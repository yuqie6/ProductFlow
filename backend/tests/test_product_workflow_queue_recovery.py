from __future__ import annotations

import time
from datetime import UTC, datetime, timedelta
from pathlib import Path

import pytest
import sqlalchemy as sa
from fastapi.testclient import TestClient
from helpers import (
    _execute_workflow_queue_inline,
    _login,
    _make_demo_image_bytes,
)

from productflow_backend.application.durable_recovery import recover_unfinished_workflow_runs
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
    WorkflowEdge,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.image.base import GeneratedImagePayload


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


class _SafeFailingWorkflowImageProvider:
    provider_name = "safe-failing-test"
    prompt_version = "safe-failing-test-v1"

    def generate_poster_image(self, *args, **kwargs):
        raise RuntimeError("image2 不支持 64x64，最小尺寸为 512x512")


class _RateLimitedWorkflowImageProvider:
    provider_name = "rate-limited-test"
    prompt_version = "rate-limited-test-v1"

    def generate_poster_image(self, *args, **kwargs):
        cause = RuntimeError("429 Too many requests")
        wrapped = RuntimeError("图片供应商请求失败，请检查供应商配置后重试")
        raise wrapped from cause


class _PolicyRejectedWorkflowImageProvider:
    provider_name = "policy-rejected-test"
    prompt_version = "policy-rejected-test-v1"

    def generate_poster_image(self, *args, **kwargs):
        raise RuntimeError("Request blocked by content policy")


def test_workflow_run_kickoff_reuses_overlapping_active_node_runs(db_session, configured_env: Path) -> None:
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

    duplicated_node_id = first.workflow.nodes[0].id
    duplicate_run = WorkflowRun(workflow_id=first.workflow.id, status=WorkflowRunStatus.RUNNING)
    db_session.add(duplicate_run)
    db_session.flush()
    db_session.add(
        WorkflowNodeRun(
            workflow_run_id=duplicate_run.id,
            node_id=duplicated_node_id,
            status=WorkflowNodeStatus.QUEUED,
        )
    )
    with pytest.raises(sa.exc.IntegrityError):
        db_session.commit()
    db_session.rollback()


def test_workflow_run_kickoff_allows_disjoint_active_node_runs(db_session, configured_env: Path) -> None:
    from productflow_backend.application.product_workflows import start_product_workflow_run

    product = create_product(
        db_session,
        name="独立节点并发商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="product.png",
        content_type="image/png",
    )
    workflow = start_product_workflow_run(db_session, product_id=product.id).workflow
    copy_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.COPY_GENERATION)
    image_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION)

    db_session.query(WorkflowNodeRun).delete()
    db_session.query(WorkflowRun).delete()
    db_session.query(WorkflowEdge).filter(
        WorkflowEdge.workflow_id == workflow.id,
        WorkflowEdge.target_node_id == image_node.id,
    ).delete(synchronize_session=False)
    db_session.commit()
    db_session.expire_all()

    first = start_product_workflow_run(db_session, product_id=product.id, start_node_id=copy_node.id)
    second = start_product_workflow_run(db_session, product_id=product.id, start_node_id=image_node.id)
    duplicate = start_product_workflow_run(db_session, product_id=product.id, start_node_id=copy_node.id)

    assert first.created is True
    assert second.created is True
    assert second.run_id != first.run_id
    assert duplicate.created is False
    assert duplicate.run_id == first.run_id

    runs = db_session.query(WorkflowRun).filter_by(workflow_id=workflow.id, status=WorkflowRunStatus.RUNNING).all()
    assert {run.id for run in runs} == {first.run_id, second.run_id}
    active_node_runs = (
        db_session.query(WorkflowNodeRun)
        .filter(WorkflowNodeRun.workflow_run_id.in_([first.run_id, second.run_id]))
        .all()
    )
    assert {(node_run.workflow_run_id, node_run.node_id) for node_run in active_node_runs} == {
        (first.run_id, copy_node.id),
        (second.run_id, image_node.id),
    }


def test_workflow_run_endpoint_enqueues_durable_actor_and_reuses_active_run(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    sent_run_ids: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.execution.enqueue_workflow_run",
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


def test_workflow_status_exposes_queue_metadata_and_action_flags(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.execution.enqueue_workflow_run",
        lambda run_id: None,
    )
    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "工作流队列元数据"},
        files={"image": ("workflow.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]
    submitted = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert submitted.status_code == 200
    run_payload = submitted.json()["runs"][0]
    assert run_payload["is_cancelable"] is True
    assert run_payload["is_retryable"] is False

    status = client.get(f"/api/products/{product_id}/workflow/status")
    assert status.status_code == 200
    payload = status.json()
    assert payload["has_active_workflow"] is True
    assert payload["runs"][0]["id"] == run_payload["id"]
    assert payload["runs"][0]["status"] == "running"
    assert payload["runs"][0]["is_cancelable"] is True
    assert payload["runs"][0]["is_retryable"] is False
    assert payload["runs"][0]["queue_active_count"] == 1
    assert payload["runs"][0]["queue_running_count"] == 0
    assert payload["runs"][0]["queue_queued_count"] == 1
    assert payload["runs"][0]["queued_ahead_count"] == 0
    assert payload["runs"][0]["queue_position"] == 1

def test_workflow_run_enqueue_failure_marks_run_failed(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    def fail_enqueue(_: str) -> None:
        raise RuntimeError("redis unavailable")

    monkeypatch.setattr("productflow_backend.application.product_workflow.execution.enqueue_workflow_run", fail_enqueue)

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
    assert payload["runs"][0]["is_retryable"] is True
    assert payload["runs"][0]["is_cancelable"] is False
    assert all(node["status"] not in {"queued", "running"} for node in payload["nodes"])


def test_workflow_run_cancel_marks_active_run_cancelled_and_worker_noops(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.product_workflows import (
        cancel_product_workflow_run,
        execute_product_workflow_run,
        start_product_workflow_run,
    )
    from productflow_backend.presentation.schemas.product_workflows import serialize_product_workflow

    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.execution._execute_node",
        lambda *args, **kwargs: pytest.fail("cancelled workflow run must no-op"),
    )

    product = create_product(
        db_session,
        name="取消工作流商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow.png",
        content_type="image/png",
    )
    kickoff = start_product_workflow_run(db_session, product_id=product.id)
    run_id = kickoff.run_id

    cancelled_workflow = cancel_product_workflow_run(db_session, product_id=product.id, run_id=run_id)
    cancelled_payload = serialize_product_workflow(cancelled_workflow).model_dump(mode="json")
    run_payload = cancelled_payload["runs"][0]
    assert run_payload["id"] == run_id
    assert run_payload["status"] == "cancelled"
    assert run_payload["failure_reason"] == "已取消"
    assert run_payload["is_cancelable"] is False
    assert run_payload["is_retryable"] is False
    assert run_payload["node_runs"]
    assert {node_run["status"] for node_run in run_payload["node_runs"]} == {"cancelled"}
    assert {node_run["failure_reason"] for node_run in run_payload["node_runs"]} == {"已取消"}
    assert all(node["status"] not in {"queued", "running"} for node in cancelled_payload["nodes"])
    assert all(node["is_retryable"] is False for node in cancelled_payload["nodes"])

    execute_product_workflow_run(run_id)
    db_session.expire_all()
    run = db_session.get(WorkflowRun, run_id)
    assert run is not None
    assert run.status == WorkflowRunStatus.CANCELLED


def test_workflow_run_execution_fails_when_queued_nodes_have_no_ready_upstream(
    configured_env: Path,
    db_session,
) -> None:
    from productflow_backend.application.product_workflows import (
        execute_product_workflow_run,
        start_product_workflow_run,
    )

    product = create_product(
        db_session,
        name="调度无就绪节点商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow.png",
        content_type="image/png",
    )
    kickoff = start_product_workflow_run(db_session, product_id=product.id)
    workflow = kickoff.workflow
    blocking_edge = workflow.edges[0]
    run = db_session.get(WorkflowRun, kickoff.run_id)
    assert run is not None
    for node_run in run.node_runs:
        if node_run.node_id == blocking_edge.source_node_id:
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.failure_reason = "上游测试失败"
        elif node_run.node_id == blocking_edge.target_node_id:
            node_run.status = WorkflowNodeStatus.QUEUED
        else:
            node_run.status = WorkflowNodeStatus.SUCCEEDED
    db_session.commit()

    execute_product_workflow_run(kickoff.run_id)

    db_session.expire_all()
    persisted_run = db_session.get(WorkflowRun, kickoff.run_id)
    assert persisted_run is not None
    assert persisted_run.status == WorkflowRunStatus.FAILED
    assert persisted_run.failure_reason == "上游测试失败"
    target_node_run = next(
        node_run for node_run in persisted_run.node_runs if node_run.node_id == blocking_edge.target_node_id
    )
    assert target_node_run.status == WorkflowNodeStatus.FAILED
    assert target_node_run.failure_reason == "上游节点失败"


def test_workflow_run_retry_creates_new_run_from_failed_run_without_duplicate_active_run(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    sent_run_ids: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.execution.enqueue_workflow_run",
        lambda run_id: sent_run_ids.append(run_id),
    )
    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "重试工作流商品"},
        files={"image": ("workflow.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]
    submitted = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert submitted.status_code == 200
    failed_run_id = submitted.json()["runs"][0]["id"]

    session = get_session_factory()()
    try:
        run = session.get(WorkflowRun, failed_run_id)
        assert run is not None
        run.status = WorkflowRunStatus.FAILED
        run.failure_reason = "图片生成失败，请稍后重试"
        run.is_retryable = True
        run.finished_at = datetime.now(UTC)
        for node_run in run.node_runs:
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.failure_reason = "图片生成失败，请稍后重试"
            node_run.finished_at = datetime.now(UTC)
        session.commit()
    finally:
        session.close()

    sent_run_ids.clear()
    retried = client.post(f"/api/products/{product_id}/workflow/runs/{failed_run_id}/retry")
    assert retried.status_code == 202
    runs = retried.json()["runs"]
    assert runs[0]["id"] != failed_run_id
    assert runs[0]["status"] == "running"
    assert runs[0]["is_cancelable"] is True
    assert runs[0]["progress_metadata"] == {
        "last_failure_reason": "图片生成失败，请稍后重试",
        "last_failure_retryable": True,
        "retry_hint": "retry_later",
        "source_run_id": failed_run_id,
        "manual_retry": True,
    }
    assert runs[1]["id"] == failed_run_id
    assert runs[1]["is_retryable"] is True
    assert sent_run_ids == [runs[0]["id"]]

    duplicate_retry = client.post(f"/api/products/{product_id}/workflow/runs/{failed_run_id}/retry")
    assert duplicate_retry.status_code == 400
    assert duplicate_retry.json()["detail"] == "相关节点运行中，不能重试"


def test_workflow_run_retry_rejects_non_retryable_failed_run(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    sent_run_ids: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.execution.enqueue_workflow_run",
        lambda run_id: sent_run_ids.append(run_id),
    )
    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "不可重试工作流商品"},
        files={"image": ("workflow.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]
    submitted = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert submitted.status_code == 200
    failed_run_id = submitted.json()["runs"][0]["id"]
    assert sent_run_ids == [failed_run_id]

    session = get_session_factory()()
    try:
        run = session.get(WorkflowRun, failed_run_id)
        assert run is not None
        run.status = WorkflowRunStatus.FAILED
        run.failure_reason = "图片供应商拒绝了本次内容或安全策略，请调整提示词或参考图后重试"
        run.is_retryable = False
        run.finished_at = datetime.now(UTC)
        for node_run in run.node_runs:
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.failure_reason = run.failure_reason
            node_run.finished_at = datetime.now(UTC)
        session.commit()
    finally:
        session.close()

    sent_run_ids.clear()
    retried = client.post(f"/api/products/{product_id}/workflow/runs/{failed_run_id}/retry")

    assert retried.status_code == 400
    assert retried.json()["detail"] == "该工作流运行不可重试"
    assert sent_run_ids == []


def test_recover_unfinished_workflow_runs_requeues_queued_runs(
    db_session,
    configured_env: Path,
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
    summary = recover_unfinished_workflow_runs(enqueue=sent_run_ids.append)

    assert summary.queued_runs == 1
    assert summary.stale_running_runs == 0
    assert summary.enqueued_runs == 1
    assert sent_run_ids == [kickoff.run_id]


def test_recover_unfinished_workflow_runs_requeues_terminal_nodes_for_finalization(
    db_session,
    configured_env: Path,
) -> None:
    from productflow_backend.application.product_workflows import start_product_workflow_run

    product = create_product(
        db_session,
        name="恢复待终结工作流",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow.png",
        content_type="image/png",
    )
    kickoff = start_product_workflow_run(db_session, product_id=product.id)
    node_runs = list(
        db_session.scalars(
            sa.select(WorkflowNodeRun).where(WorkflowNodeRun.workflow_run_id == kickoff.run_id)
        )
    )
    assert node_runs
    for index, node_run in enumerate(node_runs):
        node_run.status = WorkflowNodeStatus.SUCCEEDED if index == 0 else WorkflowNodeStatus.FAILED
    db_session.commit()

    sent_run_ids: list[str] = []
    summary = recover_unfinished_workflow_runs(enqueue=sent_run_ids.append)

    assert summary.queued_runs == 1
    assert summary.stale_running_runs == 0
    assert summary.enqueued_runs == 1
    assert sent_run_ids == [kickoff.run_id]


def test_recover_unfinished_workflow_runs_resets_stale_running_node_runs(
    db_session,
    configured_env: Path,
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
    summary = recover_unfinished_workflow_runs(
        enqueue=sent_run_ids.append,
        reset_stale_running=True,
        stale_running_after=timedelta(minutes=30),
    )
    db_session.refresh(node_run)
    db_session.refresh(node)

    assert summary.queued_runs == 0
    assert summary.stale_running_runs == 1
    assert summary.enqueued_runs == 1
    assert sent_run_ids == [kickoff.run_id]
    assert node_run.status == WorkflowNodeStatus.QUEUED
    assert node.status == WorkflowNodeStatus.QUEUED


def test_product_workflow_worker_defers_queued_run_when_global_running_capacity_full(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.product_workflow.run_state import (
        PRODUCT_WORKFLOW_CAPACITY_RETRY_DELAY_MS,
    )
    from productflow_backend.application.product_workflows import (
        cancel_product_workflow_run,
        execute_product_workflow_node_run,
        execute_product_workflow_run,
        start_product_workflow_run,
    )

    occupying_product = create_product(
        db_session,
        name="占用运行容量商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="occupying.png",
        content_type="image/png",
    )
    occupying = start_product_workflow_run(db_session, product_id=occupying_product.id)
    occupying_node_run = db_session.query(WorkflowNodeRun).filter_by(workflow_run_id=occupying.run_id).first()
    assert occupying_node_run is not None
    occupying_node = db_session.get(WorkflowNode, occupying_node_run.node_id)
    assert occupying_node is not None
    occupying_node_run.status = WorkflowNodeStatus.RUNNING
    occupying_node.status = WorkflowNodeStatus.RUNNING

    queued_product = create_product(
        db_session,
        name="等待运行容量商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="queued.png",
        content_type="image/png",
    )
    queued = start_product_workflow_run(db_session, product_id=queued_product.id)
    queued_node_run = db_session.query(WorkflowNodeRun).filter_by(workflow_run_id=queued.run_id).first()
    assert queued_node_run is not None
    db_session.add(AppSetting(key="generation_max_concurrent_tasks", value="1"))
    db_session.commit()

    delayed_requeues: list[tuple[str, int]] = []
    dispatched_node_run_ids: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.execution.enqueue_workflow_node_run",
        lambda node_run_id: dispatched_node_run_ids.append(node_run_id),
    )
    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.run_state.enqueue_workflow_node_run_later",
        lambda node_run_id, *, delay_ms: delayed_requeues.append((node_run_id, delay_ms)),
    )
    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.execution._execute_node",
        lambda *args, **kwargs: pytest.fail("capacity-blocked workflow run must not call provider"),
    )

    execute_product_workflow_run(queued.run_id)
    assert len(dispatched_node_run_ids) == 1
    dispatched_node_run_id = dispatched_node_run_ids[0]
    execute_product_workflow_node_run(dispatched_node_run_id)

    db_session.expire_all()
    persisted_run = db_session.get(WorkflowRun, queued.run_id)
    persisted_node_run = db_session.get(WorkflowNodeRun, dispatched_node_run_id)
    assert persisted_node_run is not None
    persisted_node = db_session.get(WorkflowNode, persisted_node_run.node_id)

    assert persisted_run is not None
    assert persisted_run.status == WorkflowRunStatus.RUNNING
    assert persisted_node_run.status == WorkflowNodeStatus.QUEUED
    assert persisted_node_run.finished_at is None
    assert persisted_node is not None
    assert persisted_node.status == WorkflowNodeStatus.QUEUED
    assert delayed_requeues == [(dispatched_node_run_id, PRODUCT_WORKFLOW_CAPACITY_RETRY_DELAY_MS)]

    cancel_product_workflow_run(db_session, product_id=queued_product.id, run_id=queued.run_id)
    execute_product_workflow_node_run(dispatched_node_run_id)

    db_session.expire_all()
    cancelled_run = db_session.get(WorkflowRun, queued.run_id)
    cancelled_node_run = db_session.get(WorkflowNodeRun, dispatched_node_run_id)
    assert cancelled_run is not None
    assert cancelled_run.status == WorkflowRunStatus.CANCELLED
    assert cancelled_node_run is not None
    assert cancelled_node_run.status == WorkflowNodeStatus.FAILED
    assert cancelled_node_run.failure_reason == "已取消"
    assert delayed_requeues == [(dispatched_node_run_id, PRODUCT_WORKFLOW_CAPACITY_RETRY_DELAY_MS)]


def test_duplicate_workflow_messages_noop_for_terminal_or_running_runs(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.product_workflows import (
        execute_product_workflow_node_run,
        execute_product_workflow_run,
        start_product_workflow_run,
    )

    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.execution._execute_node",
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

    execute_product_workflow_node_run(running_node_run.id)
    db_session.refresh(running_node_run)
    assert running_node_run.status == WorkflowNodeStatus.RUNNING


def test_workflow_scheduler_dispatches_every_ready_node_run(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.product_workflows import (
        execute_product_workflow_run,
        get_or_create_product_workflow,
        start_product_workflow_run,
    )

    product = create_product(
        db_session,
        name="ready wave 工作流",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="ready-wave.png",
        content_type="image/png",
    )
    workflow = get_or_create_product_workflow(db_session, product.id)
    isolated_node = WorkflowNode(
        workflow_id=workflow.id,
        node_type=WorkflowNodeType.REFERENCE_IMAGE,
        title="独立参考",
        position_x=1200,
        position_y=120,
        config_json={"role": "reference", "label": "独立参考"},
    )
    db_session.add(isolated_node)
    db_session.commit()
    db_session.expire_all()

    kickoff = start_product_workflow_run(db_session, product_id=product.id)
    dispatched_node_run_ids: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.execution.enqueue_workflow_node_run",
        lambda node_run_id: dispatched_node_run_ids.append(node_run_id),
    )

    execute_product_workflow_run(kickoff.run_id)

    run = db_session.get(WorkflowRun, kickoff.run_id)
    assert run is not None
    ready_node_ids = {
        node.id
        for node in run.workflow.nodes
        if not any(edge.target_node_id == node.id for edge in run.workflow.edges)
    }
    dispatched_node_ids = {
        node_run.node_id
        for node_run in run.node_runs
        if node_run.id in dispatched_node_run_ids
    }
    assert dispatched_node_ids == ready_node_ids
    assert isolated_node.id in dispatched_node_ids


def test_workflow_node_run_message_executes_queued_node_once(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.product_workflows import (
        execute_product_workflow_node_run,
        start_product_workflow_run,
    )

    product = create_product(
        db_session,
        name="节点幂等商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="node-idempotency.png",
        content_type="image/png",
    )
    kickoff = start_product_workflow_run(db_session, product_id=product.id)
    node_run = db_session.query(WorkflowNodeRun).filter_by(workflow_run_id=kickoff.run_id).first()
    assert node_run is not None

    executed_node_ids: list[str] = []

    def execute_node(*args, node: WorkflowNode, **kwargs) -> dict:
        executed_node_ids.append(node.id)
        return {"node_id": node.id}

    monkeypatch.setattr("productflow_backend.application.product_workflow.execution._execute_node", execute_node)
    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.execution.enqueue_workflow_run",
        lambda run_id: None,
    )

    execute_product_workflow_node_run(node_run.id)
    execute_product_workflow_node_run(node_run.id)

    db_session.expire_all()
    persisted_node_run = db_session.get(WorkflowNodeRun, node_run.id)
    assert persisted_node_run is not None
    assert persisted_node_run.status == WorkflowNodeStatus.SUCCEEDED
    assert executed_node_ids == [node_run.node_id]


def test_workflow_node_run_failure_does_not_block_independent_ready_branch(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.product_workflows import (
        execute_product_workflow_node_run,
        execute_product_workflow_run,
        get_or_create_product_workflow,
        start_product_workflow_run,
    )

    product = create_product(
        db_session,
        name="分支局部失败商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="branch-local-failure.png",
        content_type="image/png",
    )
    workflow = get_or_create_product_workflow(db_session, product.id)
    failing_node = WorkflowNode(
        workflow_id=workflow.id,
        node_type=WorkflowNodeType.REFERENCE_IMAGE,
        title="会失败的独立参考",
        position_x=1200,
        position_y=80,
        config_json={"role": "reference", "label": "失败参考"},
    )
    succeeding_node = WorkflowNode(
        workflow_id=workflow.id,
        node_type=WorkflowNodeType.REFERENCE_IMAGE,
        title="会成功的独立参考",
        position_x=1200,
        position_y=240,
        config_json={"role": "reference", "label": "成功参考"},
    )
    db_session.add_all([failing_node, succeeding_node])
    db_session.commit()
    db_session.expire_all()

    kickoff = start_product_workflow_run(db_session, product_id=product.id)
    run = db_session.get(WorkflowRun, kickoff.run_id)
    assert run is not None
    isolated_node_ids = {failing_node.id, succeeding_node.id}
    isolated_node_runs = {
        node_run.node_id: node_run
        for node_run in run.node_runs
        if node_run.node_id in isolated_node_ids
    }
    assert set(isolated_node_runs) == isolated_node_ids
    for node_run in run.node_runs:
        if node_run.node_id not in isolated_node_ids:
            node_run.status = WorkflowNodeStatus.SUCCEEDED
            node_run.finished_at = datetime.now(UTC)
    db_session.commit()

    def execute_node(*args, node: WorkflowNode, **kwargs) -> dict:
        if node.id == failing_node.id:
            raise RuntimeError("独立分支失败")
        return {"node_id": node.id}

    monkeypatch.setattr("productflow_backend.application.product_workflow.execution._execute_node", execute_node)
    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.execution.enqueue_workflow_run",
        lambda run_id: None,
    )

    execute_product_workflow_node_run(isolated_node_runs[failing_node.id].id)
    execute_product_workflow_node_run(isolated_node_runs[succeeding_node.id].id)
    execute_product_workflow_run(kickoff.run_id)

    db_session.expire_all()
    persisted_run = db_session.get(WorkflowRun, kickoff.run_id)
    failed_node_run = db_session.get(WorkflowNodeRun, isolated_node_runs[failing_node.id].id)
    succeeded_node_run = db_session.get(WorkflowNodeRun, isolated_node_runs[succeeding_node.id].id)
    assert persisted_run is not None
    assert persisted_run.status == WorkflowRunStatus.FAILED
    assert persisted_run.failure_reason == "独立分支失败"
    assert failed_node_run is not None
    assert failed_node_run.status == WorkflowNodeStatus.FAILED
    assert succeeded_node_run is not None
    assert succeeded_node_run.status == WorkflowNodeStatus.SUCCEEDED


def test_workflow_retry_requeues_failed_and_blocked_node_runs_only(
    db_session,
    configured_env: Path,
) -> None:
    from productflow_backend.application.product_workflows import (
        retry_product_workflow_run,
        start_product_workflow_run,
    )

    product = create_product(
        db_session,
        name="局部重试商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="partial-retry.png",
        content_type="image/png",
    )
    kickoff = start_product_workflow_run(db_session, product_id=product.id)
    workflow = kickoff.workflow
    edge = workflow.edges[0]
    run = db_session.get(WorkflowRun, kickoff.run_id)
    assert run is not None
    run.status = WorkflowRunStatus.FAILED
    run.failure_reason = "上游测试失败"
    run.is_retryable = True
    run.finished_at = datetime.now(UTC)
    expected_retry_node_ids = {edge.source_node_id, edge.target_node_id}
    for node_run in run.node_runs:
        if node_run.node_id == edge.source_node_id:
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.failure_reason = "上游测试失败"
            node_run.finished_at = datetime.now(UTC)
        elif node_run.node_id == edge.target_node_id:
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.failure_reason = "上游节点失败"
            node_run.finished_at = datetime.now(UTC)
        else:
            node_run.status = WorkflowNodeStatus.SUCCEEDED
            node_run.finished_at = datetime.now(UTC)
    db_session.commit()

    sent_run_ids: list[str] = []
    retried_workflow = retry_product_workflow_run(
        db_session,
        product_id=product.id,
        run_id=kickoff.run_id,
        enqueue=lambda run_id: sent_run_ids.append(run_id),
    )

    latest_run = max(retried_workflow.runs, key=lambda item: item.started_at)
    assert latest_run.id != kickoff.run_id
    assert sent_run_ids == [latest_run.id]
    assert {node_run.node_id for node_run in latest_run.node_runs} == expected_retry_node_ids


def test_workflow_image_generation_timeout_marks_run_node_and_queue_failed(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.admission import get_generation_queue_overview
    from productflow_backend.application.product_workflow.image_generation import (
        WORKFLOW_IMAGE_GENERATION_TIMEOUT_FAILURE,
    )
    from productflow_backend.application.product_workflow_dependencies import WorkflowExecutionDependencies
    from productflow_backend.application.product_workflows import run_product_workflow

    db_session.add(AppSetting(key="poster_generation_mode", value="generated"))
    db_session.commit()
    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.image_generation."
        "workflow_image_generation_provider_timeout_seconds",
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
    from productflow_backend.application.product_workflow.image_generation import WORKFLOW_IMAGE_GENERATION_FAILURE
    from productflow_backend.application.product_workflow_dependencies import WorkflowExecutionDependencies
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


def test_workflow_image_generation_provider_failure_exposes_safe_detail(
    db_session,
    configured_env: Path,
) -> None:
    from productflow_backend.application.product_workflow_dependencies import WorkflowExecutionDependencies
    from productflow_backend.application.product_workflows import run_product_workflow
    from productflow_backend.presentation.schemas.product_workflows import serialize_product_workflow

    db_session.add(AppSetting(key="poster_generation_mode", value="generated"))
    db_session.commit()

    product = create_product(
        db_session,
        name="生图安全失败详情商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow-provider-safe-failure.png",
        content_type="image/png",
    )

    workflow = run_product_workflow(
        db_session,
        product_id=product.id,
        dependencies=WorkflowExecutionDependencies(
            image_provider_resolver=lambda: _SafeFailingWorkflowImageProvider(),
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
    assert run.failure_reason == "图片生成失败：image2 不支持 64x64，最小尺寸为 512x512"
    assert run.progress_metadata == {
        "last_failure_reason": "图片生成失败：image2 不支持 64x64，最小尺寸为 512x512",
        "last_failure_retryable": False,
        "retry_hint": "check_settings",
        "last_failure_category": "unsupported_parameters",
    }

    image_node = db_session.query(WorkflowNode).filter_by(
        workflow_id=workflow.id,
        node_type=WorkflowNodeType.IMAGE_GENERATION,
    ).one()
    assert image_node.status == WorkflowNodeStatus.FAILED
    assert image_node.failure_reason == run.failure_reason
    payload = serialize_product_workflow(workflow).model_dump(mode="json")
    image_node_payload = next(node for node in payload["nodes"] if node["id"] == image_node.id)
    assert image_node_payload["retry_hint"] == "check_settings"
    assert image_node_payload["non_retryable_reason"] == run.failure_reason


def test_workflow_image_generation_provider_failure_categorizes_wrapped_rate_limit(
    db_session,
    configured_env: Path,
) -> None:
    from productflow_backend.application.product_workflow_dependencies import WorkflowExecutionDependencies
    from productflow_backend.application.product_workflows import run_product_workflow

    db_session.add(AppSetting(key="poster_generation_mode", value="generated"))
    db_session.commit()

    product = create_product(
        db_session,
        name="生图限流失败商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow-provider-rate-limit.png",
        content_type="image/png",
    )

    workflow = run_product_workflow(
        db_session,
        product_id=product.id,
        dependencies=WorkflowExecutionDependencies(
            image_provider_resolver=lambda: _RateLimitedWorkflowImageProvider(),
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
    assert run.failure_reason == "图片供应商限流或配额不足，请稍后重试或降低并发后再试"
    assert run.is_retryable is True


def test_workflow_image_generation_policy_reject_is_not_retryable(
    db_session,
    configured_env: Path,
) -> None:
    from productflow_backend.application.product_workflow_dependencies import WorkflowExecutionDependencies
    from productflow_backend.application.product_workflows import (
        run_product_workflow,
        start_product_workflow_run,
        update_workflow_node,
    )
    from productflow_backend.domain.errors import BusinessValidationError
    from productflow_backend.presentation.schemas.product_workflows import serialize_product_workflow

    db_session.add(AppSetting(key="poster_generation_mode", value="generated"))
    db_session.commit()

    product = create_product(
        db_session,
        name="生图策略拒绝商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow-provider-policy-reject.png",
        content_type="image/png",
    )

    workflow = run_product_workflow(
        db_session,
        product_id=product.id,
        dependencies=WorkflowExecutionDependencies(
            image_provider_resolver=lambda: _PolicyRejectedWorkflowImageProvider(),
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
    assert run.failure_reason == "图片供应商拒绝了本次内容或安全策略，请调整提示词或参考图后重试"
    assert run.is_retryable is False
    assert run.progress_metadata == {
        "last_failure_reason": "图片供应商拒绝了本次内容或安全策略，请调整提示词或参考图后重试",
        "last_failure_retryable": False,
        "retry_hint": "revise_input",
        "last_failure_category": "content_policy",
    }

    failed_node_run = next(
        node_run
        for node_run in run.node_runs
        if node_run.status == WorkflowNodeStatus.FAILED and node_run.failure_reason == run.failure_reason
    )

    workflow_payload = serialize_product_workflow(workflow).model_dump(mode="json")
    failed_node_payload = next(node for node in workflow_payload["nodes"] if node["id"] == failed_node_run.node_id)
    assert failed_node_payload["status"] == "failed"
    assert failed_node_payload["is_retryable"] is False
    assert failed_node_payload["attempt_count"] == 1
    assert failed_node_payload["retry_count"] == 0
    assert failed_node_payload["non_retryable_reason"] == run.failure_reason
    assert failed_node_payload["retry_hint"] == "revise_input"

    with pytest.raises(BusinessValidationError, match="该工作流节点不可重试"):
        start_product_workflow_run(db_session, product_id=product.id, start_node_id=failed_node_run.node_id)

    failed_node = db_session.get(WorkflowNode, failed_node_run.node_id)
    assert failed_node is not None
    next_config = dict(failed_node.config_json or {})
    next_config["instruction"] = "改成安全的商品主图提示词"
    updated_workflow = update_workflow_node(
        db_session,
        node_id=failed_node.id,
        title=None,
        position_x=None,
        position_y=None,
        config_json=next_config,
    )
    updated_payload = serialize_product_workflow(updated_workflow).model_dump(mode="json")
    updated_node_payload = next(node for node in updated_payload["nodes"] if node["id"] == failed_node.id)
    assert updated_node_payload["status"] == "idle"
    assert updated_node_payload["failure_reason"] is None
    assert updated_node_payload["attempt_count"] == 1
    assert updated_node_payload["retry_count"] == 0
    assert updated_node_payload["non_retryable_reason"] is None
    assert updated_node_payload["retry_hint"] is None

    unlocked_kickoff = start_product_workflow_run(db_session, product_id=product.id, start_node_id=failed_node.id)
    assert unlocked_kickoff.created is True


@pytest.mark.parametrize(
    "malformed_requested_slots",
    [
        [123],
        "主标题",
        [{"key": "", "label": "主标题"}],
    ],
)
def test_workflow_copy_node_invalid_config_is_non_retryable(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
    malformed_requested_slots: object,
) -> None:
    from productflow_backend.application.product_workflows import (
        execute_product_workflow_run,
        get_or_create_product_workflow,
        start_product_workflow_run,
    )

    _execute_workflow_queue_inline(monkeypatch)

    product = create_product(
        db_session,
        name="文案配置错误商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow-copy-invalid-config.png",
        content_type="image/png",
    )
    workflow = get_or_create_product_workflow(db_session, product.id)
    copy_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.COPY_GENERATION)
    copy_node.config_json = {
        **dict(copy_node.config_json or {}),
        "requested_slots": malformed_requested_slots,
    }
    db_session.commit()

    kickoff = start_product_workflow_run(db_session, product_id=product.id)
    execute_product_workflow_run(kickoff.run_id)
    db_session.expire_all()

    run = db_session.get(WorkflowRun, kickoff.run_id)
    failed_copy_node = db_session.get(WorkflowNode, copy_node.id)
    assert run is not None
    assert run.status == WorkflowRunStatus.FAILED
    assert run.failure_reason == "文案节点配置无效，请调整节点设置后重试"
    assert run.is_retryable is False
    assert run.progress_metadata == {
        "last_failure_reason": "文案节点配置无效，请调整节点设置后重试",
        "last_failure_retryable": False,
        "retry_hint": "revise_input",
        "last_failure_category": "invalid_node_config",
    }
    assert failed_copy_node is not None
    assert failed_copy_node.status == WorkflowNodeStatus.FAILED
    assert failed_copy_node.failure_reason == run.failure_reason


def test_workflow_time_limit_exception_marks_running_node_failed(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from dramatiq.middleware.time_limit import TimeLimitExceeded

    from productflow_backend.application.product_workflow.run_state import WORKFLOW_WORKER_TIMEOUT_FAILURE
    from productflow_backend.application.product_workflows import (
        execute_product_workflow_run,
        start_product_workflow_run,
    )

    def raise_time_limit(*args, **kwargs) -> dict:
        raise TimeLimitExceeded()

    monkeypatch.setattr("productflow_backend.application.product_workflow.execution._execute_node", raise_time_limit)
    _execute_workflow_queue_inline(monkeypatch)

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
