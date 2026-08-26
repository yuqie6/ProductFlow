from __future__ import annotations

from datetime import timedelta

from helpers import _make_demo_image_bytes
from sqlalchemy import func, select
from test_graph_execution import RecordingImageProvider, RecordingPromptProvider

import productflow_backend.application.product_workflow.graph_execution as graph_execution_module
from productflow_backend.application.async_delivery import run_async_dispatcher_once
from productflow_backend.application.durable_recovery import recover_unfinished_workflow_runs
from productflow_backend.application.product_workflow.dependencies import WorkflowExecutionDependencies
from productflow_backend.application.product_workflow.graph_commands import (
    apply_graph_change_set,
    get_workflow_graph,
    load_applied_graph,
)
from productflow_backend.application.product_workflow.graph_contracts import (
    DeleteNodeOp,
    DisconnectEdgeOp,
    RenameNodeOp,
    WorkflowChangeSet,
)
from productflow_backend.application.product_workflow.graph_direct_create import create_product_with_direct_graph
from productflow_backend.application.product_workflow.graph_execution import execute_graph_run
from productflow_backend.application.product_workflow.graph_provider_effects import GraphRunEffectCrash
from productflow_backend.application.product_workflow.graph_runs import cancel_graph_run, submit_graph_run
from productflow_backend.application.product_workflow.graph_template import DirectCreateImageType
from productflow_backend.domain.durable_generation_tasks import (
    GRAPH_RUN_GENERATION_TASK_CONTRACT,
    QUEUE_UNAVAILABLE_DETAIL,
    WORKFLOW_PROVIDER_EFFECT_UNKNOWN_DETAIL,
)
from productflow_backend.domain.enums import (
    AsyncDispatchStatus,
    GraphActorType,
    GraphNodeType,
    GraphRunScope,
    ProductImageOriginType,
    WorkflowNodeStatus,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import QueueUnavailableError
from productflow_backend.infrastructure.db.models import (
    AsyncDispatch,
    ProductImageAsset,
    WorkflowGraphArtifact,
    WorkflowGraphNode,
    WorkflowGraphNodeRun,
    WorkflowGraphRun,
)
from productflow_backend.infrastructure.image.base import WorkflowImageRequest, WorkflowImageResult
from productflow_backend.infrastructure.storage import LocalStorage
from productflow_backend.presentation.schemas.graphs import serialize_graph_run


def _created_graph(db_session, name: str = "耐久运行商品"):
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name=name,
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    image_node = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    return created, image_bytes, image_node


def _submit_without_execute(db_session, created):
    return submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.GRAPH,
        enqueue=lambda _run_id: None,
    )


def _image_node_run(db_session, run_id: str, image_node_id: str) -> WorkflowGraphNodeRun:
    node_run = db_session.scalar(
        select(WorkflowGraphNodeRun).where(
            WorkflowGraphNodeRun.graph_run_id == run_id,
            WorkflowGraphNodeRun.node_id == image_node_id,
        )
    )
    assert node_run is not None
    return node_run


def test_crash_before_provider_safe_requeues_without_calling_provider(db_session, monkeypatch) -> None:
    created, image_bytes, image_node = _created_graph(db_session, "调用前崩溃")
    prompt_provider = RecordingPromptProvider()
    image_provider = RecordingImageProvider(image_bytes)
    dependencies = WorkflowExecutionDependencies(
        prompt_generation_provider_resolver=lambda: prompt_provider,
        image_provider_resolver=lambda: image_provider,
    )
    submission = _submit_without_execute(db_session, created)

    def crash(phase: str, node_run: WorkflowGraphNodeRun) -> None:
        if phase == "prepared" and node_run.node_id == image_node.id:
            raise GraphRunEffectCrash(phase, node_run.id)

    monkeypatch.setattr(graph_execution_module, "_effect_phase_hook", crash)
    execute_graph_run(submission.run.id, dependencies=dependencies)
    db_session.expire_all()
    node_run = _image_node_run(db_session, submission.run.id, image_node.id)
    assert node_run.status == WorkflowNodeStatus.RUNNING
    assert node_run.progress_phase == "prepared"
    assert image_provider.requests == []

    sent: list[str] = []
    summary = recover_unfinished_workflow_runs(
        enqueue=sent.append,
        reset_stale_running=True,
        stale_running_after=timedelta(0),
    )
    assert summary.unknown_runs == 0
    assert summary.stale_running_runs == 1
    db_session.expire_all()
    assert node_run.status == WorkflowNodeStatus.QUEUED
    assert sent == [submission.run.id]
    db_session.commit()

    monkeypatch.setattr(graph_execution_module, "_effect_phase_hook", None)
    execute_graph_run(submission.run.id, dependencies=dependencies)
    db_session.expire_all()
    recovered_run = db_session.get(WorkflowGraphRun, submission.run.id)
    recovered_node = _image_node_run(db_session, submission.run.id, image_node.id)
    assert recovered_node.status == WorkflowNodeStatus.SUCCEEDED, (
        recovered_run.status,
        recovered_node.status,
        recovered_node.failure_reason,
        recovered_node.progress_phase,
        recovered_node.active_attempt_id,
    )
    assert len(image_provider.requests) == 1
    assert recovered_run.status == WorkflowRunStatus.SUCCEEDED


def test_crash_after_provider_call_marks_unknown_and_does_not_recall(db_session, monkeypatch) -> None:
    created, image_bytes, image_node = _created_graph(db_session, "调用后崩溃")

    class CrashingImageProvider(RecordingImageProvider):
        def generate_workflow_image(self, request: WorkflowImageRequest) -> WorkflowImageResult:
            self.requests.append(request)
            raise GraphRunEffectCrash("provider_call")

    prompt_provider = RecordingPromptProvider()
    image_provider = CrashingImageProvider(image_bytes)
    dependencies = WorkflowExecutionDependencies(
        prompt_generation_provider_resolver=lambda: prompt_provider,
        image_provider_resolver=lambda: image_provider,
    )
    submission = _submit_without_execute(db_session, created)
    execute_graph_run(submission.run.id, dependencies=dependencies)
    db_session.expire_all()
    node_run = _image_node_run(db_session, submission.run.id, image_node.id)
    assert node_run.progress_phase == "provider_call"
    assert len(image_provider.requests) == 1

    summary = recover_unfinished_workflow_runs(
        enqueue=lambda _run_id: None,
        reset_stale_running=True,
        stale_running_after=timedelta(0),
    )
    assert summary.unknown_runs == 1
    assert summary.stale_running_runs == 0
    db_session.expire_all()
    run = db_session.get(WorkflowGraphRun, submission.run.id)
    assert run is not None
    assert run.status == WorkflowRunStatus.UNKNOWN
    assert run.failure_reason == WORKFLOW_PROVIDER_EFFECT_UNKNOWN_DETAIL
    assert node_run.status == WorkflowNodeStatus.UNKNOWN
    execute_graph_run(submission.run.id, dependencies=dependencies)
    assert len(image_provider.requests) == 1


def test_crash_after_provider_return_marks_unknown_without_second_generate(db_session, monkeypatch) -> None:
    created, image_bytes, image_node = _created_graph(db_session, "返回后崩溃")
    prompt_provider = RecordingPromptProvider()
    image_provider = RecordingImageProvider(image_bytes)
    dependencies = WorkflowExecutionDependencies(
        prompt_generation_provider_resolver=lambda: prompt_provider,
        image_provider_resolver=lambda: image_provider,
    )
    submission = _submit_without_execute(db_session, created)

    def crash(phase: str, node_run: WorkflowGraphNodeRun) -> None:
        if phase == "provider_returned" and node_run.node_id == image_node.id:
            raise GraphRunEffectCrash(phase, node_run.id)

    monkeypatch.setattr(graph_execution_module, "_effect_phase_hook", crash)
    execute_graph_run(submission.run.id, dependencies=dependencies)
    db_session.expire_all()
    node_run = _image_node_run(db_session, submission.run.id, image_node.id)
    assert node_run.progress_phase == "provider_call"
    assert len(image_provider.requests) == 1

    summary = recover_unfinished_workflow_runs(
        enqueue=lambda _run_id: None,
        reset_stale_running=True,
        stale_running_after=timedelta(0),
    )
    assert summary.unknown_runs == 1
    db_session.expire_all()
    run = db_session.get(WorkflowGraphRun, submission.run.id)
    assert run is not None
    assert run.status == WorkflowRunStatus.UNKNOWN
    execute_graph_run(submission.run.id, dependencies=dependencies)
    assert len(image_provider.requests) == 1


def test_crash_before_artifact_commit_marks_unknown_without_second_generate(db_session, monkeypatch) -> None:
    created, image_bytes, image_node = _created_graph(db_session, "提交前崩溃")
    prompt_provider = RecordingPromptProvider()
    image_provider = RecordingImageProvider(image_bytes)
    dependencies = WorkflowExecutionDependencies(
        prompt_generation_provider_resolver=lambda: prompt_provider,
        image_provider_resolver=lambda: image_provider,
    )
    submission = _submit_without_execute(db_session, created)

    def crash(phase: str, node_run: WorkflowGraphNodeRun) -> None:
        if phase == "provider_result_received" and node_run.node_id == image_node.id:
            raise GraphRunEffectCrash(phase, node_run.id)

    monkeypatch.setattr(graph_execution_module, "_effect_phase_hook", crash)
    execute_graph_run(submission.run.id, dependencies=dependencies)
    db_session.expire_all()
    node_run = _image_node_run(db_session, submission.run.id, image_node.id)
    assert node_run.progress_phase == "provider_result_received"
    assert len(image_provider.requests) == 1

    summary = recover_unfinished_workflow_runs(
        enqueue=lambda _run_id: None,
        reset_stale_running=True,
        stale_running_after=timedelta(0),
    )
    assert summary.unknown_runs == 1
    db_session.expire_all()
    run = db_session.get(WorkflowGraphRun, submission.run.id)
    assert run is not None
    assert run.status == WorkflowRunStatus.UNKNOWN
    live_node = db_session.get(WorkflowGraphNode, image_node.id)
    assert live_node is not None
    assert live_node.current_artifact_id is None
    execute_graph_run(submission.run.id, dependencies=dependencies)
    assert len(image_provider.requests) == 1


def test_late_provider_result_after_cancel_is_not_current_artifact(db_session, monkeypatch) -> None:
    created, image_bytes, image_node = _created_graph(db_session, "取消竞态")
    prompt_provider = RecordingPromptProvider()
    image_provider = RecordingImageProvider(image_bytes)
    dependencies = WorkflowExecutionDependencies(
        prompt_generation_provider_resolver=lambda: prompt_provider,
        image_provider_resolver=lambda: image_provider,
    )
    submission = _submit_without_execute(db_session, created)

    def cancel_after_result(phase: str, node_run: WorkflowGraphNodeRun) -> None:
        if phase == "provider_result_received" and node_run.node_id == image_node.id:
            cancel_graph_run(
                db_session,
                product_id=created.product.id,
                graph_id=created.graph.id,
                run_id=submission.run.id,
            )

    monkeypatch.setattr(graph_execution_module, "_effect_phase_hook", cancel_after_result)
    execute_graph_run(submission.run.id, dependencies=dependencies)
    db_session.expire_all()
    run = db_session.get(WorkflowGraphRun, submission.run.id)
    assert run is not None
    assert run.status == WorkflowRunStatus.CANCELLED
    live_node = db_session.get(WorkflowGraphNode, image_node.id)
    assert live_node is not None
    assert live_node.current_artifact_id is None
    node_run = _image_node_run(db_session, submission.run.id, image_node.id)
    assert node_run.status == WorkflowNodeStatus.FAILED
    assert node_run.status != WorkflowNodeStatus.SUCCEEDED


def test_submit_graph_run_stages_dispatch_when_broker_unavailable(db_session, monkeypatch) -> None:
    created, _image_bytes, _image_node = _created_graph(db_session, "队列不可用")
    direct: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.infrastructure.queue.enqueue_graph_run",
        lambda run_id: direct.append(run_id),
    )
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.GRAPH,
    )
    assert direct == []
    assert submission.created is True
    assert submission.run.status == WorkflowRunStatus.RUNNING
    dispatch = db_session.scalar(
        select(AsyncDispatch).where(AsyncDispatch.aggregate_id == submission.run.id)
    )
    assert dispatch is not None
    assert dispatch.status == AsyncDispatchStatus.PENDING
    assert dispatch.actor_name == GRAPH_RUN_GENERATION_TASK_CONTRACT.actor_name

    retried = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.GRAPH,
    )
    assert retried.created is False
    assert retried.run.id == submission.run.id
    run_count = db_session.scalar(select(func.count()).select_from(WorkflowGraphRun))
    assert run_count == 1
    dispatch_count = db_session.scalar(
        select(func.count()).select_from(AsyncDispatch).where(
            AsyncDispatch.aggregate_id == submission.run.id
        )
    )
    assert dispatch_count == 1

    published: list[tuple[str, str]] = []
    summary = run_async_dispatcher_once(
        enqueue=lambda dispatch_id, aggregate_id: published.append((dispatch_id, aggregate_id)),
    )
    assert summary.sent == 1
    assert published == [(dispatch.id, submission.run.id)]


def test_submit_graph_run_explicit_enqueue_failure_is_queue_unavailable(db_session) -> None:
    created, _image_bytes, _image_node = _created_graph(db_session, "显式入队失败")

    def boom(_run_id: str) -> None:
        raise RuntimeError("broker down")

    try:
        submit_graph_run(
            db_session,
            product_id=created.product.id,
            graph_id=created.graph.id,
            scope=GraphRunScope.GRAPH,
            enqueue=boom,
        )
    except QueueUnavailableError as exc:
        assert str(exc) == QUEUE_UNAVAILABLE_DETAIL
    else:
        raise AssertionError("expected QueueUnavailableError")
    persisted = list(db_session.scalars(select(WorkflowGraphRun)))
    assert len(persisted) == 1
    assert persisted[0].status == WorkflowRunStatus.FAILED
    assert persisted[0].failure_reason == QUEUE_UNAVAILABLE_DETAIL
    node_runs = list(
        db_session.scalars(
            select(WorkflowGraphNodeRun).where(WorkflowGraphNodeRun.graph_run_id == persisted[0].id)
        )
    )
    assert node_runs
    assert {node_run.status for node_run in node_runs} == {WorkflowNodeStatus.FAILED}


def test_default_submit_dispatcher_consumes_once_and_duplicate_delivery_skips_provider(
    db_session, monkeypatch
) -> None:
    created, image_bytes, _image_node = _created_graph(db_session, "单次投递")
    prompt_provider = RecordingPromptProvider()
    image_provider = RecordingImageProvider(image_bytes)
    dependencies = WorkflowExecutionDependencies(
        prompt_generation_provider_resolver=lambda: prompt_provider,
        image_provider_resolver=lambda: image_provider,
    )
    direct: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.infrastructure.queue.enqueue_graph_run",
        lambda run_id: direct.append(run_id),
    )
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.GRAPH,
    )
    assert direct == []
    dispatches = list(
        db_session.scalars(select(AsyncDispatch).where(AsyncDispatch.aggregate_id == submission.run.id))
    )
    assert len(dispatches) == 1
    db_session.commit()
    summary = run_async_dispatcher_once(
        enqueue=lambda _dispatch_id, aggregate_id: execute_graph_run(
            aggregate_id,
            dependencies=dependencies,
        )
    )
    assert summary.sent == 1
    db_session.expire_all()
    run = db_session.get(WorkflowGraphRun, submission.run.id)
    assert run is not None
    assert run.status == WorkflowRunStatus.SUCCEEDED
    first_image_calls = len(image_provider.requests)
    assert first_image_calls == 1
    execute_graph_run(submission.run.id, dependencies=dependencies)
    assert len(image_provider.requests) == first_image_calls


def test_image_persist_commit_failure_deletes_file_and_marks_unknown(
    db_session, configured_env, monkeypatch
) -> None:
    created, image_bytes, image_node = _created_graph(db_session, "提交失败补偿")
    prompt_provider = RecordingPromptProvider()
    image_provider = RecordingImageProvider(image_bytes)
    dependencies = WorkflowExecutionDependencies(
        prompt_generation_provider_resolver=lambda: prompt_provider,
        image_provider_resolver=lambda: image_provider,
    )
    submission = _submit_without_execute(db_session, created)
    tracked: list[str] = []
    storage = LocalStorage(configured_env)

    def boom(_session, storage_writes) -> None:
        tracked.extend(path for _item, path in storage_writes._writes)
        assert tracked
        for relative in tracked:
            assert storage.resolve(relative).exists()
        raise RuntimeError("db commit failed")

    monkeypatch.setattr(graph_execution_module, "_storage_bound_commit_hook", boom)
    execute_graph_run(submission.run.id, dependencies=dependencies)
    db_session.expire_all()
    run = db_session.get(WorkflowGraphRun, submission.run.id)
    assert run is not None
    assert run.status == WorkflowRunStatus.UNKNOWN
    assert run.failure_reason == WORKFLOW_PROVIDER_EFFECT_UNKNOWN_DETAIL
    node_run = _image_node_run(db_session, submission.run.id, image_node.id)
    assert node_run.status == WorkflowNodeStatus.UNKNOWN
    generated = list(
        db_session.scalars(
            select(ProductImageAsset).where(
                ProductImageAsset.product_id == created.product.id,
                ProductImageAsset.origin_type == ProductImageOriginType.WORKFLOW_GENERATION,
            )
        )
    )
    assert generated == []
    artifacts = list(
        db_session.scalars(
            select(WorkflowGraphArtifact).where(WorkflowGraphArtifact.node_run_id == node_run.id)
        )
    )
    assert artifacts == []
    for relative in tracked:
        assert not storage.resolve(relative).exists()
    assert len(image_provider.requests) == 1


def test_late_result_commit_failure_deletes_file_and_keeps_cancelled(
    db_session, configured_env, monkeypatch
) -> None:
    created, image_bytes, image_node = _created_graph(db_session, "取消后提交失败")
    prompt_provider = RecordingPromptProvider()
    image_provider = RecordingImageProvider(image_bytes)
    dependencies = WorkflowExecutionDependencies(
        prompt_generation_provider_resolver=lambda: prompt_provider,
        image_provider_resolver=lambda: image_provider,
    )
    submission = _submit_without_execute(db_session, created)
    tracked: list[str] = []
    storage = LocalStorage(configured_env)

    def cancel_after_result(phase: str, node_run: WorkflowGraphNodeRun) -> None:
        if phase == "provider_result_received" and node_run.node_id == image_node.id:
            cancel_graph_run(
                db_session,
                product_id=created.product.id,
                graph_id=created.graph.id,
                run_id=submission.run.id,
            )

    def boom(_session, storage_writes) -> None:
        tracked.extend(path for _item, path in storage_writes._writes)
        assert tracked
        for relative in tracked:
            assert storage.resolve(relative).exists()
        raise RuntimeError("db commit failed")

    monkeypatch.setattr(graph_execution_module, "_effect_phase_hook", cancel_after_result)
    monkeypatch.setattr(graph_execution_module, "_storage_bound_commit_hook", boom)
    execute_graph_run(submission.run.id, dependencies=dependencies)
    db_session.expire_all()
    run = db_session.get(WorkflowGraphRun, submission.run.id)
    assert run is not None
    assert run.status == WorkflowRunStatus.CANCELLED
    node_run = _image_node_run(db_session, submission.run.id, image_node.id)
    assert node_run.status == WorkflowNodeStatus.FAILED
    generated = list(
        db_session.scalars(
            select(ProductImageAsset).where(
                ProductImageAsset.product_id == created.product.id,
                ProductImageAsset.origin_type == ProductImageOriginType.WORKFLOW_GENERATION,
            )
        )
    )
    assert generated == []
    artifacts = list(
        db_session.scalars(
            select(WorkflowGraphArtifact).where(WorkflowGraphArtifact.node_run_id == node_run.id)
        )
    )
    assert artifacts == []
    for relative in tracked:
        assert not storage.resolve(relative).exists()
    live_node = db_session.get(WorkflowGraphNode, image_node.id)
    assert live_node is not None
    assert live_node.current_artifact_id is None
    assert len(image_provider.requests) == 1


def test_run_history_keeps_rev_n_input_titles_after_rename_disconnect_delete(db_session) -> None:
    created, image_bytes, image_node = _created_graph(db_session, "历史血缘")
    prompt_provider = RecordingPromptProvider()
    image_provider = RecordingImageProvider(image_bytes)
    dependencies = WorkflowExecutionDependencies(
        prompt_generation_provider_resolver=lambda: prompt_provider,
        image_provider_resolver=lambda: image_provider,
    )
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.GRAPH,
        enqueue=lambda run_id: execute_graph_run(run_id, dependencies=dependencies),
    )
    assert submission.run.status == WorkflowRunStatus.SUCCEEDED
    before = serialize_graph_run(submission.run)
    image_history = next(item for item in before.node_runs if item.node_id == image_node.id)
    original_titles = {entry.source_title for entry in image_history.input_trace}
    assert original_titles
    prompt = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    original_prompt_title = prompt.title
    assert original_prompt_title in original_titles

    graph = get_workflow_graph(db_session, product_id=created.product.id, graph_id=created.graph.id)
    apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary="改名提示词",
            actor_type=GraphActorType.USER,
            operations=[RenameNodeOp(node_ref=prompt.id, title="改名后的提示词")],
        ),
    )
    graph = get_workflow_graph(db_session, product_id=created.product.id, graph_id=created.graph.id)
    applied = load_applied_graph(db_session, graph)
    prompt_edge = next(
        edge for edge in applied.edges if edge.source_node_id == prompt.id and edge.target_node_id == image_node.id
    )
    apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary="断开提示词",
            actor_type=GraphActorType.USER,
            operations=[DisconnectEdgeOp(edge_ref=prompt_edge.id)],
        ),
    )
    graph = get_workflow_graph(db_session, product_id=created.product.id, graph_id=created.graph.id)
    apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary="删除提示词",
            actor_type=GraphActorType.USER,
            operations=[DeleteNodeOp(node_ref=prompt.id)],
        ),
    )
    db_session.expire_all()
    run = db_session.get(WorkflowGraphRun, submission.run.id)
    assert run is not None
    after = serialize_graph_run(run)
    image_after = next(item for item in after.node_runs if item.node_id == image_node.id)
    assert {entry.source_title for entry in image_after.input_trace} == original_titles
    assert original_prompt_title in {entry.source_title for entry in image_after.input_trace}
    assert all(entry.source_title != "改名后的提示词" for entry in image_after.input_trace)
