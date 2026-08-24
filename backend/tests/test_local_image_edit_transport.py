from __future__ import annotations

from datetime import timedelta

from test_local_image_edits import _context, _new_task

from productflow_backend.application.async_delivery import stage_async_dispatch_for_actor
from productflow_backend.application.durable_recovery import recover_unfinished_local_image_edit_tasks
from productflow_backend.application.local_image_edits.service import (
    LOCAL_EDIT_ACTOR_NAME,
    claim_local_image_edit_task,
    submit_local_image_edit_task,
)
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import LocalImageEditTaskStatus
from productflow_backend.infrastructure.db.models import LocalImageEditTask


def test_local_edit_recovery_scanner_requeues_queued_and_marks_stale_provider_unknown(
    db_session,
    configured_env,
) -> None:
    context = _context(db_session)
    queued_task = _new_task(db_session, context)
    submit_local_image_edit_task(
        db_session,
        task_id=queued_task.id,
        idempotency_key="queued-recovery",
        requested_provider_name="fixture-image-provider",
        requested_local_edit_mode="masked_edit",
    )

    stale_task = _new_task(db_session, context)
    submit_local_image_edit_task(
        db_session,
        task_id=stale_task.id,
        idempotency_key="stale-recovery",
        requested_provider_name="fixture-image-provider",
        requested_local_edit_mode="masked_edit",
    )
    claim_local_image_edit_task(db_session, task_id=stale_task.id)
    db_session.commit()
    stale_row = db_session.get(LocalImageEditTask, stale_task.id)
    assert stale_row is not None
    stale_row.progress_phase = "provider_call"
    stale_row.started_at = now_utc() - timedelta(hours=1)
    db_session.commit()

    staged: list[str] = []

    def stage(session, task_id: str) -> None:
        stage_async_dispatch_for_actor(session, LOCAL_EDIT_ACTOR_NAME, task_id)
        staged.append(task_id)

    summary = recover_unfinished_local_image_edit_tasks(
        stage_dispatch=stage,
        reset_stale_running=True,
        stale_running_after=timedelta(minutes=10),
    )
    assert summary.queued_tasks == 1
    assert summary.unknown_tasks == 1
    assert summary.enqueued_tasks == 1
    assert staged == [queued_task.id]
    db_session.expire_all()
    queued_row = db_session.get(LocalImageEditTask, queued_task.id)
    stale_row = db_session.get(LocalImageEditTask, stale_task.id)
    assert queued_row is not None and queued_row.status == LocalImageEditTaskStatus.QUEUED
    assert stale_row is not None and stale_row.status == LocalImageEditTaskStatus.UNKNOWN


def test_local_edit_async_dispatch_target_uses_injectable_service(monkeypatch, configured_env) -> None:
    import productflow_backend.workers as workers

    captured: list[dict[str, object]] = []

    def execute(**kwargs) -> None:
        captured.append(kwargs)

    monkeypatch.setattr(workers, "execute_local_image_edit_task", execute)
    monkeypatch.setattr(workers, "get_image_provider", lambda: "provider")
    monkeypatch.setattr(workers, "LocalStorage", lambda: "storage")
    workers._execute_async_dispatch_target(LOCAL_EDIT_ACTOR_NAME, "task-transport")

    assert captured == [
        {
            "session_factory": workers.get_session_factory(),
            "task_id": "task-transport",
            "provider": "provider",
            "storage": "storage",
        }
    ]
