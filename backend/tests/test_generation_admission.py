from __future__ import annotations

from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes

from productflow_backend.application.image_sessions import create_image_session, create_image_session_generation_task
from productflow_backend.application.product_workflows import start_product_workflow_run
from productflow_backend.application.use_cases import create_product
from productflow_backend.domain.enums import JobStatus
from productflow_backend.infrastructure.db.models import AppSetting


def _set_generation_cap(db_session, value: int) -> None:
    db_session.add(AppSetting(key="generation_max_concurrent_tasks", value=str(value)))
    db_session.commit()


def _create_product(db_session, name: str):
    return create_product(
        db_session,
        name=name,
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename=f"{name}.png",
        content_type="image/png",
    )


def test_generation_cap_rejects_workflow_run_creation(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    monkeypatch.setattr(
        "productflow_backend.application.product_workflow_execution.enqueue_workflow_run",
        lambda run_id: pytest.fail(f"busy workflow run must not enqueue: {run_id}"),
    )

    busy_product = _create_product(db_session, "占用并发商品")
    start_product_workflow_run(db_session, product_id=busy_product.id)
    workflow_target = _create_product(db_session, "工作流限流商品")
    _set_generation_cap(db_session, 1)

    app = create_app()
    client = TestClient(app)
    _login(client)

    workflow_response = client.post(f"/api/products/{workflow_target.id}/workflow/run", json={})
    assert workflow_response.status_code == 429
    assert workflow_response.json()["detail"] == "当前生成任务较多，请稍后再试"


def test_generation_cap_rejects_image_session_generation_task_creation(
    configured_env: Path,
    db_session,
) -> None:
    from productflow_backend.presentation.api import create_app

    busy_product = _create_product(db_session, "同步占用商品")
    start_product_workflow_run(db_session, product_id=busy_product.id)
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

    assert generated.status_code == 429
    assert generated.json()["detail"] == "当前生成任务较多，请稍后再试"


def test_active_generation_task_count_includes_image_session_generation_tasks(
    configured_env: Path,
    db_session,
) -> None:
    from productflow_backend.application.admission import active_generation_task_count

    assert active_generation_task_count(db_session) == 0
    image_session = create_image_session(db_session, product_id=None, title="并发计数")
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

    image_session = create_image_session(db_session, product_id=None, title="队列会话")
    second_image_session = create_image_session(db_session, product_id=None, title="队列会话 2")
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

    image_session = create_image_session(db_session, product_id=None, title="队列 API 会话")
    running = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="运行中的连续生图任务",
        size="1024x1024",
    ).task
    running.status = JobStatus.RUNNING
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
