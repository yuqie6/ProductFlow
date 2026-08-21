from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from helpers import _login

from productflow_backend.application.image_sessions.service import (
    create_image_session,
    create_image_session_generation_task,
)
from productflow_backend.domain.enums import JobStatus
from productflow_backend.infrastructure.db.models import (
    AppSetting,
)


def _set_generation_cap(db_session, value: int) -> None:
    db_session.add(AppSetting(key="generation_max_concurrent_tasks", value=str(value)))
    db_session.commit()


def test_generation_cap_accepts_and_queues_image_session_generation_task_creation(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    sent_task_ids: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.application.image_sessions.service.enqueue_image_session_generation_task",
        lambda task_id: sent_task_ids.append(task_id),
    )

    image_session = create_image_session(db_session, title="同步占用会话")
    running = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="第一张正在跑",
        size="1024x1024",
    ).task
    running.status = JobStatus.RUNNING
    running.active_attempt_id = "admission-running-attempt"
    running.started_at = datetime.now(UTC)
    db_session.commit()
    _set_generation_cap(db_session, 1)

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "同步限流"})
    assert created.status_code == 201

    generated = client.post(
        f"/api/image-sessions/{created.json()['id']}/generate",
        json={"prompt": "这次应该被并发上限拦截", "size": "1024x1024"},
    )

    assert generated.status_code == 202
    tasks = generated.json()["generation_tasks"]
    assert len(tasks) == 1
    assert tasks[0]["status"] == "queued"
    assert tasks[0]["queue_active_count"] == 2
    assert tasks[0]["queue_running_count"] == 1
    assert tasks[0]["queue_queued_count"] == 1
    assert sent_task_ids == [tasks[0]["id"]]


def test_active_generation_task_count_includes_image_session_generation_tasks(
    configured_env: Path,
    db_session,
) -> None:
    from productflow_backend.application.admission import active_generation_task_count

    assert active_generation_task_count(db_session) == 0
    image_session = create_image_session(db_session, title="并发计数")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="占用连续生图任务",
        size="1024x1024",
    )

    assert active_generation_task_count(db_session) == 1
    result.task.status = JobStatus.SUCCEEDED
    db_session.commit()
    assert active_generation_task_count(db_session) == 0


def test_generation_queue_overview_and_positions_include_durable_tasks(
    configured_env: Path,
    db_session,
) -> None:
    from productflow_backend.application.admission import (
        get_generation_queue_overview,
        get_generation_task_queue_metadata,
        get_queued_generation_positions,
    )

    image_session = create_image_session(db_session, title="队列会话")
    second_image_session = create_image_session(db_session, title="队列会话 2")
    first = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="第一个连续生图任务",
        size="1024x1024",
    ).task
    second = create_image_session_generation_task(
        db_session,
        image_session_id=second_image_session.id,
        prompt="第二个连续生图任务",
        size="1024x1024",
    ).task
    second.status = JobStatus.RUNNING
    second.active_attempt_id = "queue-overview-running-attempt"
    second.started_at = datetime.now(UTC)
    db_session.commit()

    overview = get_generation_queue_overview(db_session)
    positions = get_queued_generation_positions(db_session)
    first_metadata = get_generation_task_queue_metadata(
        db_session,
        first,
        overview=overview,
        queued_positions=positions,
    )
    second_metadata = get_generation_task_queue_metadata(
        db_session,
        second,
        overview=overview,
        queued_positions=positions,
    )

    assert overview.active_count == 2
    assert overview.running_count == 1
    assert overview.queued_count == 1
    assert positions[first.id] == 1
    assert first_metadata.queue_position == 1
    assert first_metadata.queued_ahead_count == 0
    assert second_metadata.queue_position is None
    assert second_metadata.queued_ahead_count is None


def test_generation_queue_overview_endpoint_returns_public_snapshot(
    configured_env: Path,
    db_session,
) -> None:
    from productflow_backend.presentation.api import create_app

    image_session = create_image_session(db_session, title="队列 API 会话")
    running = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="运行中的连续生图任务",
        size="1024x1024",
    ).task
    running.status = JobStatus.RUNNING
    running.active_attempt_id = "admission-running-attempt"
    running.started_at = datetime.now(UTC)
    db_session.commit()

    app = create_app()
    client = TestClient(app)
    _login(client)

    response = client.get("/api/generation-queue")

    assert response.status_code == 200
    assert response.json() == {
        "active_count": 1,
        "running_count": 1,
        "queued_count": 0,
        "max_concurrent_tasks": 3,
    }
