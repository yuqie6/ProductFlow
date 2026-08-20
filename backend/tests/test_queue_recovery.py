from __future__ import annotations

from datetime import UTC, datetime, timedelta
from pathlib import Path

from sqlalchemy import select

from productflow_backend.application.async_delivery import delivery_key_for_actor, stage_async_dispatch
from productflow_backend.application.durable_recovery import recover_unfinished_image_session_generation_tasks
from productflow_backend.application.image_sessions.service import (
    create_image_session,
    create_image_session_generation_task,
)
from productflow_backend.domain.durable_generation_tasks import (
    IMAGE_SESSION_GENERATION_TASK_CONTRACT,
    IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_DETAIL,
    IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_PHASE,
)
from productflow_backend.domain.enums import JobStatus
from productflow_backend.infrastructure.db.models import AppSetting, AsyncDispatch, ImageSessionProviderEffect


def test_recover_unfinished_image_session_generation_tasks_requeues_queued_tasks(
    db_session,
    configured_env: Path,
) -> None:
    image_session = create_image_session(db_session, title="queued 恢复")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="queued 任务应补发",
        size="1024x1024",
    )
    sent: list[str] = []
    summary = recover_unfinished_image_session_generation_tasks(enqueue=sent.append)

    assert summary.queued_tasks == 1
    assert summary.stale_running_tasks == 0
    assert summary.enqueued_tasks == 1
    assert sent == [result.task.id]


def test_recovery_can_stage_dispatch_in_same_transaction(
    db_session,
    configured_env: Path,
) -> None:
    image_session = create_image_session(db_session, title="同事务恢复")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="恢复必须和 intent 同提交",
        size="1024x1024",
    )

    summary = recover_unfinished_image_session_generation_tasks(
        stage_dispatch=lambda session, task_id: stage_async_dispatch(
            session,
            delivery_key=delivery_key_for_actor(
                IMAGE_SESSION_GENERATION_TASK_CONTRACT.actor_name,
                task_id,
            ),
            actor_name=IMAGE_SESSION_GENERATION_TASK_CONTRACT.actor_name,
            aggregate_id=task_id,
        )
    )

    dispatch = db_session.scalar(
        select(AsyncDispatch).where(
            AsyncDispatch.aggregate_id == result.task.id,
        )
    )
    assert summary.enqueued_tasks == 1
    assert dispatch is not None
    assert dispatch.status.value == "pending"


def test_recover_unfinished_image_session_generation_tasks_resets_stale_running_tasks(
    db_session,
    configured_env: Path,
) -> None:
    image_session = create_image_session(db_session, title="running 恢复")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="stale running 任务应重置",
        size="1024x1024",
    )
    result.task.status = JobStatus.RUNNING
    result.task.active_attempt_id = "stale-running-attempt"
    result.task.started_at = datetime.now(UTC) - timedelta(hours=2)
    result.task.progress_updated_at = datetime.now(UTC) - timedelta(hours=2)
    result.task.progress_phase = "running"
    db_session.commit()
    sent: list[str] = []
    summary = recover_unfinished_image_session_generation_tasks(
        enqueue=sent.append,
        reset_stale_running=True,
        stale_running_after=timedelta(minutes=30),
    )
    db_session.refresh(result.task)

    assert summary.queued_tasks == 0
    assert summary.stale_running_tasks == 1
    assert summary.enqueued_tasks == 1
    assert sent == [result.task.id]
    assert result.task.status == JobStatus.QUEUED
    assert result.task.active_attempt_id is None
    assert result.task.started_at is None
    assert result.task.progress_phase == "requeued_after_idle"


def test_recover_unfinished_image_session_generation_tasks_uses_progress_heartbeat_for_stale_running(
    db_session,
    configured_env: Path,
) -> None:
    image_session = create_image_session(db_session, title="heartbeat 恢复")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="started_at 旧，但 progress 新，不应重置",
        size="1024x1024",
    )
    result.task.status = JobStatus.RUNNING
    result.task.active_attempt_id = "fresh-heartbeat-attempt"
    result.task.started_at = datetime.now(UTC) - timedelta(hours=2)
    result.task.progress_updated_at = datetime.now(UTC) - timedelta(minutes=5)
    db_session.commit()
    sent: list[str] = []
    summary = recover_unfinished_image_session_generation_tasks(
        enqueue=sent.append,
        reset_stale_running=True,
        stale_running_after=timedelta(minutes=30),
    )
    db_session.refresh(result.task)

    assert summary.stale_running_tasks == 0
    assert summary.enqueued_tasks == 0
    assert sent == []
    assert result.task.status == JobStatus.RUNNING
    assert result.task.active_attempt_id == "fresh-heartbeat-attempt"


def test_recover_unfinished_image_session_generation_tasks_marks_provider_effect_unknown(
    db_session,
    configured_env: Path,
) -> None:
    image_session = create_image_session(db_session, title="partial heartbeat 恢复")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="已生成一张后闲置",
        size="1024x1024",
        generation_count=2,
    )
    result.task.status = JobStatus.RUNNING
    result.task.active_attempt_id = "stale-running-attempt"
    result.task.started_at = datetime.now(UTC) - timedelta(hours=2)
    result.task.progress_updated_at = datetime.now(UTC) - timedelta(hours=2)
    result.task.completed_candidates = 1
    result.task.result_generation_group_id = "group-partial"
    result.task.progress_phase = "provider_polling"
    result.task.active_candidate_index = 2
    db_session.commit()
    sent: list[str] = []
    summary = recover_unfinished_image_session_generation_tasks(
        enqueue=sent.append,
        reset_stale_running=True,
        stale_running_after=timedelta(minutes=30),
    )
    db_session.refresh(result.task)

    assert summary.queued_tasks == 0
    assert summary.stale_running_tasks == 0
    assert summary.enqueued_tasks == 0
    assert summary.unknown_tasks == 1
    assert sent == []
    assert result.task.status == JobStatus.UNKNOWN
    assert result.task.active_attempt_id is None
    assert result.task.is_retryable is False
    assert result.task.failure_reason == IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_DETAIL
    assert result.task.progress_phase == IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_PHASE
    assert result.task.progress_metadata == {
        "unknown_provider_effect": {
            "attempt_id": "stale-running-attempt",
            "active_candidate_index": 2,
            "observed_phase": "provider_polling",
            "next_candidate_index": 2,
            "has_unmaterialized_provider_effect": False,
        }
    }


def test_recovery_does_not_replay_batch_provider_effect_after_partial_materialization(
    db_session,
    configured_env: Path,
) -> None:
    image_session = create_image_session(db_session, title="批量 provider effect 恢复")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="批量请求只落下一张也不能重复提交",
        size="1024x1024",
        generation_count=3,
    )
    result.task.status = JobStatus.RUNNING
    result.task.active_attempt_id = "batch-attempt"
    result.task.started_at = datetime.now(UTC) - timedelta(hours=2)
    result.task.progress_updated_at = datetime.now(UTC) - timedelta(hours=2)
    result.task.completed_candidates = 1
    result.task.progress_phase = "candidate_saved"
    db_session.add(
        ImageSessionProviderEffect(
            generation_task_id=result.task.id,
            candidate_start_index=1,
            candidate_count=3,
            operation_key=f"image-session-task:{result.task.id}:candidates:1-3",
            request_hash="a" * 64,
            provider_name="openai_images",
            attempt_id="batch-attempt",
            effect_result="applied",
            reconciliation_state="applied",
            provider_response_id="resp-batch",
        )
    )
    db_session.commit()
    sent: list[str] = []

    summary = recover_unfinished_image_session_generation_tasks(
        enqueue=sent.append,
        reset_stale_running=True,
        stale_running_after=timedelta(minutes=30),
    )
    db_session.refresh(result.task)

    assert summary.queued_tasks == 0
    assert summary.stale_running_tasks == 0
    assert summary.enqueued_tasks == 0
    assert summary.unknown_tasks == 1
    assert sent == []
    assert result.task.status == JobStatus.UNKNOWN
    assert result.task.active_attempt_id is None
    assert result.task.is_retryable is False
    assert result.task.failure_reason == IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_DETAIL
    assert result.task.progress_phase == IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_PHASE
    assert result.task.progress_metadata == {
        "unknown_provider_effect": {
            "attempt_id": "batch-attempt",
            "active_candidate_index": None,
            "observed_phase": "candidate_saved",
            "next_candidate_index": 2,
            "has_unmaterialized_provider_effect": True,
        }
    }


def test_recover_unfinished_image_session_generation_tasks_uses_runtime_stale_cutoff_by_default(
    db_session,
    configured_env: Path,
) -> None:
    image_session = create_image_session(db_session, title="runtime cutoff 恢复")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="默认 90 分钟内不应重置",
        size="1024x1024",
    )
    result.task.status = JobStatus.RUNNING
    result.task.active_attempt_id = "runtime-cutoff-attempt"
    result.task.started_at = datetime.now(UTC) - timedelta(minutes=60)
    result.task.progress_phase = "running"
    db_session.commit()
    sent: list[str] = []
    default_summary = recover_unfinished_image_session_generation_tasks(
        enqueue=sent.append,
        reset_stale_running=True,
    )
    db_session.refresh(result.task)

    assert default_summary.stale_running_tasks == 0
    assert default_summary.enqueued_tasks == 0
    assert sent == []
    assert result.task.status == JobStatus.RUNNING

    db_session.add(AppSetting(key="image_session_stale_running_after_minutes", value="30"))
    db_session.commit()

    override_summary = recover_unfinished_image_session_generation_tasks(
        enqueue=sent.append,
        reset_stale_running=True,
    )
    db_session.refresh(result.task)

    assert override_summary.stale_running_tasks == 1
    assert override_summary.enqueued_tasks == 1
    assert sent == [result.task.id]
    assert result.task.status == JobStatus.QUEUED
    assert result.task.active_attempt_id is None
    assert result.task.started_at is None


def test_recover_unfinished_image_session_generation_tasks_counts_delivery_failure_without_faking_success(
    db_session,
    configured_env: Path,
) -> None:
    image_session = create_image_session(db_session, title="delivery 失败恢复")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="delivery 失败不应虚报成功",
        size="1024x1024",
    )

    def fail_enqueue(_: str) -> None:
        raise RuntimeError("redis unavailable")

    summary = recover_unfinished_image_session_generation_tasks(enqueue=fail_enqueue)

    assert summary.queued_tasks == 1
    assert summary.stale_running_tasks == 0
    assert summary.enqueued_tasks == 0
    db_session.refresh(result.task)
    assert result.task.status == JobStatus.QUEUED
