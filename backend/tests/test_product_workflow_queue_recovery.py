from __future__ import annotations

import time
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
    PosterKind,
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRunStatus,
)
from productflow_backend.infrastructure.db.models import (
    AppSetting,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.image.base import GeneratedImagePayload
from productflow_backend.infrastructure.queue import recover_unfinished_workflow_runs


class _SlowWorkflowImageProvider:
    provider_name = "slow-test"
    prompt_version = "slow-test-v1"

    def __init__(self, *, sleep_seconds: float = 0.2) -> None:
        self.sleep_seconds = sleep_seconds

    def generate_poster_image(self, *args, **kwargs):
        time.sleep(self.sleep_seconds)
        return (
            GeneratedImagePayload(
                kind=PosterKind.MAIN_IMAGE,
                bytes_data=_make_demo_image_bytes(),
                mime_type="image/png",
                width=800,
                height=800,
                variant_label="slow",
            ),
            "slow-model",
        )


class _FailingWorkflowImageProvider:
    provider_name = "failing-test"
    prompt_version = "failing-test-v1"

    def generate_poster_image(self, *args, **kwargs):
        raise RuntimeError("raw provider failure sk-test base_url=https://secret-provider.example prompt=full-prompt")


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
        "productflow_backend.application.product_workflow_execution.enqueue_workflow_run",
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

    monkeypatch.setattr("productflow_backend.application.product_workflow_execution.enqueue_workflow_run", fail_enqueue)

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


def test_workflow_image_generation_timeout_marks_run_node_and_queue_failed(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.admission import get_generation_queue_overview
    from productflow_backend.application.product_workflow_dependencies import WorkflowExecutionDependencies
    from productflow_backend.application.product_workflow_execution import WORKFLOW_IMAGE_GENERATION_TIMEOUT_FAILURE
    from productflow_backend.application.product_workflows import run_product_workflow

    db_session.add(AppSetting(key="poster_generation_mode", value="generated"))
    db_session.commit()
    monkeypatch.setattr(
        "productflow_backend.application.product_workflow_execution."
        "_workflow_image_generation_provider_timeout_seconds",
        lambda: 0.01,
    )

    product = create_product(
        db_session,
        name="生图超时商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow-timeout.png",
        content_type="image/png",
    )

    workflow = run_product_workflow(
        db_session,
        product_id=product.id,
        dependencies=WorkflowExecutionDependencies(
            image_provider_resolver=lambda: _SlowWorkflowImageProvider(sleep_seconds=0.2),
        ),
    )
    db_session.expire_all()

    run = (
        db_session.query(WorkflowRun)
        .filter_by(workflow_id=workflow.id)
        .order_by(WorkflowRun.started_at.desc())
        .first()
    )
    assert run is not None
    assert run.status == WorkflowRunStatus.FAILED
    assert run.finished_at is not None
    assert run.failure_reason == WORKFLOW_IMAGE_GENERATION_TIMEOUT_FAILURE

    image_node = db_session.query(WorkflowNode).filter_by(
        workflow_id=workflow.id,
        node_type=WorkflowNodeType.IMAGE_GENERATION,
    ).one()
    assert image_node.status == WorkflowNodeStatus.FAILED
    assert image_node.failure_reason == WORKFLOW_IMAGE_GENERATION_TIMEOUT_FAILURE
    assert image_node.last_run_at is not None

    node_runs = db_session.query(WorkflowNodeRun).filter_by(workflow_run_id=run.id).all()
    assert node_runs
    assert all(node_run.status not in {WorkflowNodeStatus.QUEUED, WorkflowNodeStatus.RUNNING} for node_run in node_runs)
    assert all(node_run.finished_at is not None for node_run in node_runs)
    image_node_run = next(node_run for node_run in node_runs if node_run.node_id == image_node.id)
    assert image_node_run.status == WorkflowNodeStatus.FAILED
    assert image_node_run.failure_reason == WORKFLOW_IMAGE_GENERATION_TIMEOUT_FAILURE

    overview = get_generation_queue_overview(db_session)
    assert overview.active_count == 0
    assert overview.running_count == 0


def test_workflow_image_generation_provider_failure_uses_safe_reason(
    db_session,
    configured_env: Path,
) -> None:
    from productflow_backend.application.product_workflow_dependencies import WorkflowExecutionDependencies
    from productflow_backend.application.product_workflow_execution import WORKFLOW_IMAGE_GENERATION_FAILURE
    from productflow_backend.application.product_workflows import run_product_workflow

    db_session.add(AppSetting(key="poster_generation_mode", value="generated"))
    db_session.commit()

    product = create_product(
        db_session,
        name="生图失败商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow-provider-failure.png",
        content_type="image/png",
    )

    workflow = run_product_workflow(
        db_session,
        product_id=product.id,
        dependencies=WorkflowExecutionDependencies(
            image_provider_resolver=lambda: _FailingWorkflowImageProvider(),
        ),
    )
    db_session.expire_all()

    run = (
        db_session.query(WorkflowRun)
        .filter_by(workflow_id=workflow.id)
        .order_by(WorkflowRun.started_at.desc())
        .first()
    )
    assert run is not None
    assert run.status == WorkflowRunStatus.FAILED
    assert run.finished_at is not None
    assert run.failure_reason == WORKFLOW_IMAGE_GENERATION_FAILURE
    assert "sk-test" not in run.failure_reason
    assert "secret-provider" not in run.failure_reason
    assert "full-prompt" not in run.failure_reason

    image_node = db_session.query(WorkflowNode).filter_by(
        workflow_id=workflow.id,
        node_type=WorkflowNodeType.IMAGE_GENERATION,
    ).one()
    assert image_node.status == WorkflowNodeStatus.FAILED
    assert image_node.failure_reason == WORKFLOW_IMAGE_GENERATION_FAILURE


def test_workflow_time_limit_exception_marks_running_node_failed(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from dramatiq.middleware.time_limit import TimeLimitExceeded

    from productflow_backend.application.product_workflow_execution import WORKFLOW_WORKER_TIMEOUT_FAILURE
    from productflow_backend.application.product_workflows import (
        execute_product_workflow_run,
        start_product_workflow_run,
    )

    def raise_time_limit(*args, **kwargs) -> dict:
        raise TimeLimitExceeded()

    monkeypatch.setattr("productflow_backend.application.product_workflows._execute_node", raise_time_limit)

    product = create_product(
        db_session,
        name="worker 超时商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow-worker-timeout.png",
        content_type="image/png",
    )
    kickoff = start_product_workflow_run(db_session, product_id=product.id)

    execute_product_workflow_run(kickoff.run_id)
    db_session.expire_all()

    run = db_session.get(WorkflowRun, kickoff.run_id)
    assert run is not None
    assert run.status == WorkflowRunStatus.FAILED
    assert run.finished_at is not None
    assert run.failure_reason == WORKFLOW_WORKER_TIMEOUT_FAILURE

    node_runs = db_session.query(WorkflowNodeRun).filter_by(workflow_run_id=run.id).all()
    assert node_runs
    assert all(node_run.status != WorkflowNodeStatus.RUNNING for node_run in node_runs)
    failed_node_runs = [
        node_run for node_run in node_runs if node_run.failure_reason == WORKFLOW_WORKER_TIMEOUT_FAILURE
    ]
    assert len(failed_node_runs) == 1
    assert failed_node_runs[0].finished_at is not None

    failed_node = db_session.get(WorkflowNode, failed_node_runs[0].node_id)
    assert failed_node is not None
    assert failed_node.status == WorkflowNodeStatus.FAILED
    assert failed_node.failure_reason == WORKFLOW_WORKER_TIMEOUT_FAILURE


def test_workflow_worker_actor_uses_internal_failsafe_time_limit(configured_env: Path) -> None:
    from productflow_backend.workers import (
        IMAGE_SESSION_WORKER_FAILSAFE_TIME_LIMIT_MS,
        PRODUCT_WORKFLOW_WORKER_FAILSAFE_TIME_LIMIT_MS,
        get_product_workflow_worker_failsafe_time_limit_ms,
        run_product_workflow_run,
    )

    assert get_product_workflow_worker_failsafe_time_limit_ms() == IMAGE_SESSION_WORKER_FAILSAFE_TIME_LIMIT_MS
    assert PRODUCT_WORKFLOW_WORKER_FAILSAFE_TIME_LIMIT_MS == IMAGE_SESSION_WORKER_FAILSAFE_TIME_LIMIT_MS
    assert run_product_workflow_run.options["time_limit"] == PRODUCT_WORKFLOW_WORKER_FAILSAFE_TIME_LIMIT_MS
