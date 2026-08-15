from __future__ import annotations

from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.image_sessions import create_image_session, create_image_session_generation_task
from productflow_backend.application.product_workflow.v2_runs import submit_v2_workflow_run
from productflow_backend.application.use_cases import create_canonical_product
from productflow_backend.application.workflow_drafts.materialization import materialize_workflow_draft
from productflow_backend.application.workflow_drafts.service import (
    confirm_workflow_draft_revision,
    create_workflow_draft,
)
from productflow_backend.domain.enums import JobStatus, WorkflowNodeStatus
from productflow_backend.infrastructure.db.models import AppSetting, WorkflowNode, WorkflowNodeRun, WorkflowRun


def _set_generation_cap(db_session, value: int) -> None:
    db_session.add(AppSetting(key="generation_max_concurrent_tasks", value=str(value)))
    db_session.commit()


def _create_materialized_workflow(db_session, name: str):
    product = create_canonical_product(
        db_session,
        name=name,
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), f"{name}.png", "image/png")],
    )
    draft = create_workflow_draft(
        db_session,
        product_id=product.id,
        payload=make_workflow_draft_payload(reference_asset_id=product.image_assets[0].id),
        ready_for_confirmation=True,
    )
    confirm_workflow_draft_revision(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
    )
    result = materialize_workflow_draft(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
        expected_workflow_revision=0,
        idempotency_key=f"admission-{product.id}",
    )
    return product, result.workflow


def test_generation_cap_accepts_and_queues_workflow_run_creation(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.admission import get_workflow_run_queue_metadata
    from productflow_backend.presentation.api import create_app

    sent_run_ids: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.v2_runs.enqueue_workflow_run",
        lambda run_id: sent_run_ids.append(run_id),
    )

    busy_product, busy_workflow = _create_materialized_workflow(db_session, "占用并发商品")
    busy = submit_v2_workflow_run(
        db_session,
        product_id=busy_product.id,
        workflow_id=busy_workflow.id,
        enqueue=lambda _: None,
    )
    busy_node_run = db_session.query(WorkflowNodeRun).filter_by(workflow_run_id=busy.run.id).first()
    assert busy_node_run is not None
    busy_node = db_session.get(WorkflowNode, busy_node_run.node_id)
    assert busy_node is not None
    busy_node_run.status = WorkflowNodeStatus.RUNNING
    busy_node.status = WorkflowNodeStatus.RUNNING
    db_session.commit()
    busy_run = db_session.get(WorkflowRun, busy.run.id)
    assert busy_run is not None
    assert any(node_run.status == WorkflowNodeStatus.QUEUED for node_run in busy_run.node_runs)

    busy_metadata = get_workflow_run_queue_metadata(db_session, busy_run)
    assert busy_metadata.overview.running_count == 1
    assert busy_metadata.overview.queued_count == 0
    assert busy_metadata.queue_position is None
    assert busy_metadata.queued_ahead_count is None

    workflow_target, target_workflow = _create_materialized_workflow(db_session, "工作流限流商品")
    _set_generation_cap(db_session, 1)

    app = create_app()
    client = TestClient(app)
    _login(client)

    workflow_response = client.post(
        f"/api/v2/products/{workflow_target.id}/workflows/{target_workflow.id}/runs"
    )
    assert workflow_response.status_code == 202
    queued_run_id = workflow_response.json()["workflow_run"]["id"]
    assert workflow_response.json()["workflow_run"]["status"] == "running"
    assert sent_run_ids == [queued_run_id]
    queued_run = db_session.get(WorkflowRun, queued_run_id)
    assert queued_run is not None
    queued_metadata = get_workflow_run_queue_metadata(db_session, queued_run)
    assert queued_metadata.overview.active_count == 2
    assert queued_metadata.overview.running_count == 1
    assert queued_metadata.overview.queued_count == 1


def test_generation_cap_accepts_and_queues_image_session_generation_task_creation(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    sent_task_ids: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.application.image_sessions.enqueue_image_session_generation_task",
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
