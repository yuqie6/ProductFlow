from __future__ import annotations

from datetime import UTC, datetime, timedelta
from pathlib import Path

import pytest
import sqlalchemy as sa
from fastapi.testclient import TestClient
from helpers import (
    _login,
    _make_demo_image_bytes,
)

from productflow_backend.application.use_cases import (
    create_product,
    delete_product,
)
from productflow_backend.domain.enums import (
    WorkflowNodeStatus,
    WorkflowRunStatus,
)
from productflow_backend.infrastructure.db.models import (
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.queue import recover_unfinished_workflow_runs


def test_workflow_run_kickoff_prevents_duplicate_active_runs(db_session, configured_env: Path) -> None:
    from productflow_backend.application.product_workflows import delete_workflow_node, start_product_workflow_run

    product = create_product(
        db_session,
        name="防重复运行商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="product.png",
        content_type="image/png",
    )

    first = start_product_workflow_run(db_session, product_id=product.id)
    second = start_product_workflow_run(db_session, product_id=product.id)

    assert first.created is True
    assert first.should_enqueue is True
    assert second.created is False
    assert second.should_enqueue is True
    assert second.run_id == first.run_id
    assert [run.id for run in second.workflow.runs if run.status == WorkflowRunStatus.RUNNING] == [first.run_id]

    protected_node = first.workflow.nodes[0]
    with pytest.raises(ValueError, match="运行中，稍后删除"):
        delete_workflow_node(db_session, node_id=protected_node.id)
    with pytest.raises(ValueError, match="商品工作流运行中，稍后删除"):
        delete_product(db_session, product_id=product.id)

    db_session.add(WorkflowRun(workflow_id=first.workflow.id, status=WorkflowRunStatus.RUNNING))
    with pytest.raises(sa.exc.IntegrityError):
        db_session.commit()
    db_session.rollback()

def test_workflow_run_endpoint_enqueues_durable_actor_and_reuses_active_run(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    sent_run_ids: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.presentation.routes.product_workflows.enqueue_workflow_run",
        lambda run_id: sent_run_ids.append(run_id),
    )

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "队列工作流商品"},
        files={"image": ("workflow.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    first = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert first.status_code == 200
    first_run_id = first.json()["runs"][0]["id"]
    assert first.json()["runs"][0]["status"] == "running"
    assert sent_run_ids == [first_run_id]

    second = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert second.status_code == 200
    assert second.json()["runs"][0]["id"] == first_run_id
    assert sent_run_ids == [first_run_id, first_run_id]

    session = get_session_factory()()
    try:
        node_run = session.query(WorkflowNodeRun).filter_by(workflow_run_id=first_run_id).first()
        assert node_run is not None
        node_run.status = WorkflowNodeStatus.RUNNING
        session.commit()
    finally:
        session.close()

    third = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert third.status_code == 200
    assert third.json()["runs"][0]["id"] == first_run_id
    assert sent_run_ids == [first_run_id, first_run_id]

def test_workflow_run_enqueue_failure_marks_run_failed(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    def fail_enqueue(_: str) -> None:
        raise RuntimeError("redis unavailable")

    monkeypatch.setattr("productflow_backend.presentation.routes.product_workflows.enqueue_workflow_run", fail_enqueue)

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "入队失败商品"},
        files={"image": ("workflow.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    response = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert response.status_code == 503
    assert response.json()["detail"] == "任务队列暂不可用，请稍后重试"

    workflow = client.get(f"/api/products/{product_id}/workflow")
    assert workflow.status_code == 200
    payload = workflow.json()
    assert payload["runs"][0]["status"] == "failed"
    assert payload["runs"][0]["failure_reason"] == "任务队列暂不可用，请稍后重试"
    assert all(node["status"] not in {"queued", "running"} for node in payload["nodes"])

def test_recover_unfinished_workflow_runs_requeues_queued_runs(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.product_workflows import start_product_workflow_run

    product = create_product(
        db_session,
        name="恢复队列工作流",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow.png",
        content_type="image/png",
    )
    kickoff = start_product_workflow_run(db_session, product_id=product.id)
    sent_run_ids: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.infrastructure.queue.enqueue_workflow_run",
        lambda run_id: sent_run_ids.append(run_id),
    )

    summary = recover_unfinished_workflow_runs()

    assert summary.queued_runs == 1
    assert summary.stale_running_runs == 0
    assert summary.enqueued_runs == 1
    assert sent_run_ids == [kickoff.run_id]

def test_recover_unfinished_workflow_runs_resets_stale_running_node_runs(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.product_workflows import start_product_workflow_run

    product = create_product(
        db_session,
        name="恢复执行中工作流",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow.png",
        content_type="image/png",
    )
    kickoff = start_product_workflow_run(db_session, product_id=product.id)
    node_run = db_session.query(WorkflowNodeRun).filter_by(workflow_run_id=kickoff.run_id).first()
    assert node_run is not None
    node = db_session.get(WorkflowNode, node_run.node_id)
    assert node is not None
    node_run.status = WorkflowNodeStatus.RUNNING
    node_run.started_at = datetime.now(UTC) - timedelta(hours=2)
    node.status = WorkflowNodeStatus.RUNNING
    db_session.commit()

    sent_run_ids: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.infrastructure.queue.enqueue_workflow_run",
        lambda run_id: sent_run_ids.append(run_id),
    )

    summary = recover_unfinished_workflow_runs(reset_stale_running=True, stale_running_after=timedelta(minutes=30))
    db_session.refresh(node_run)
    db_session.refresh(node)

    assert summary.queued_runs == 0
    assert summary.stale_running_runs == 1
    assert summary.enqueued_runs == 1
    assert sent_run_ids == [kickoff.run_id]
    assert node_run.status == WorkflowNodeStatus.QUEUED
    assert node.status == WorkflowNodeStatus.QUEUED

def test_duplicate_workflow_messages_noop_for_terminal_or_running_runs(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.product_workflows import (
        execute_product_workflow_run,
        start_product_workflow_run,
    )

    monkeypatch.setattr(
        "productflow_backend.application.product_workflows._execute_node",
        lambda *args, **kwargs: pytest.fail("duplicate message must not execute providers"),
    )

    product = create_product(
        db_session,
        name="重复消息工作流",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow.png",
        content_type="image/png",
    )
    terminal = start_product_workflow_run(db_session, product_id=product.id)
    terminal_run = db_session.get(WorkflowRun, terminal.run_id)
    assert terminal_run is not None
    terminal_run.status = WorkflowRunStatus.SUCCEEDED
    terminal_run.finished_at = datetime.now(UTC)
    db_session.commit()

    execute_product_workflow_run(terminal.run_id)
    db_session.refresh(terminal_run)
    assert terminal_run.status == WorkflowRunStatus.SUCCEEDED

    product_two = create_product(
        db_session,
        name="执行中重复消息工作流",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow-two.png",
        content_type="image/png",
    )
    running = start_product_workflow_run(db_session, product_id=product_two.id)
    running_node_run = db_session.query(WorkflowNodeRun).filter_by(workflow_run_id=running.run_id).first()
    assert running_node_run is not None
    running_node_run.status = WorkflowNodeStatus.RUNNING
    running_node_run.started_at = datetime.now(UTC)
    db_session.commit()

    execute_product_workflow_run(running.run_id)
    db_session.refresh(running_node_run)
    assert running_node_run.status == WorkflowNodeStatus.RUNNING
