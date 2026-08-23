from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass

from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.async_delivery import delivery_key_for_actor, stage_async_dispatch
from productflow_backend.application.product_workflow.graph_apply import AppliedGraph
from productflow_backend.application.product_workflow.graph_commands import (
    get_workflow_graph,
    load_applied_graph,
)
from productflow_backend.application.product_workflow.graph_compiler import (
    GraphRuntimeArtifacts,
    GraphSourceRecord,
    graph_snapshot_input_trace,
    graph_snapshot_node_title,
    select_run_node_ids,
    snapshot_graph,
)
from productflow_backend.application.product_workflow.graph_visual import (
    merge_visual_override_items,
    visual_overlay_from_config,
)
from productflow_backend.application.product_workflow.product_sources import resolve_product_source
from productflow_backend.application.queue_submission import raise_queue_unavailable
from productflow_backend.application.time import now_utc
from productflow_backend.domain.durable_generation_tasks import GRAPH_RUN_GENERATION_TASK_CONTRACT
from productflow_backend.domain.enums import (
    GraphArtifactType,
    GraphNodeType,
    GraphRunScope,
    WorkflowNodeStatus,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    Product,
    ProductImageAsset,
    VisualSystemVersion,
    WorkflowGraph,
    WorkflowGraphArtifact,
    WorkflowGraphNode,
    WorkflowGraphNodeRun,
    WorkflowGraphRun,
)


def stage_graph_run_dispatch(session: Session, run_id: str):
    return stage_async_dispatch(
        session,
        delivery_key=delivery_key_for_actor(GRAPH_RUN_GENERATION_TASK_CONTRACT.actor_name, run_id),
        actor_name=GRAPH_RUN_GENERATION_TASK_CONTRACT.actor_name,
        aggregate_id=run_id,
    )

GRAPH_CANCELLED_REASON = "已取消"


@dataclass(frozen=True, slots=True)
class GraphRunSubmission:
    run: WorkflowGraphRun
    created: bool


def load_graph_sources(session: Session, graph: WorkflowGraph, applied: AppliedGraph) -> dict[str, GraphSourceRecord]:
    product = session.get(Product, graph.product_id)
    if product is None:
        raise NotFoundError("商品不存在")
    nodes = {node.id: node for node in session.scalars(
        select(WorkflowGraphNode).where(WorkflowGraphNode.graph_id == graph.id)
    )}
    artifact_ids = [node.current_artifact_id for node in nodes.values() if node.current_artifact_id]
    artifacts = {
        artifact.id: artifact
        for artifact in session.scalars(
            select(WorkflowGraphArtifact).where(WorkflowGraphArtifact.id.in_(artifact_ids))
        )
    } if artifact_ids else {}
    sources: dict[str, GraphSourceRecord] = {}
    for node in applied.nodes:
        row = nodes.get(node.id)
        artifact = artifacts.get(row.current_artifact_id) if row and row.current_artifact_id else None
        record = GraphSourceRecord()
        if node.node_type == GraphNodeType.PRODUCT_SOURCE:
            source = resolve_product_source(
                session,
                graph_product_id=graph.product_id,
                config=node.config,
            )
            record = GraphSourceRecord(facts=source.facts, product_source=source)
        elif node.node_type == GraphNodeType.CREATIVE_BRIEF:
            record = GraphSourceRecord(brief=dict(node.config))
        elif node.node_type == GraphNodeType.VISUAL_SYSTEM:
            version_id = node.config.get("visual_system_version_id")
            payload = None
            if isinstance(version_id, str) and version_id:
                version = session.get(VisualSystemVersion, version_id)
                if version is not None:
                    payload = dict(version.payload_json)
            if payload is None:
                overrides = node.config.get("visual_overrides")
                if isinstance(overrides, list):
                    payload = merge_visual_override_items(overrides) or None
                if payload is None:
                    payload = visual_overlay_from_config(node.config)
            record = GraphSourceRecord(
                visual_payload=payload,
                visual_system_version_id=version_id if isinstance(version_id, str) else None,
            )
        elif node.node_type == GraphNodeType.IMAGE_ASSET:
            asset = _asset_metadata(session, product_id=graph.product_id, asset_id=node.bound_asset_id)
            record = GraphSourceRecord(
                bound_asset_id=node.bound_asset_id,
                bound_asset_label=asset.display_name if asset else node.title,
                bound_asset_mime_type=asset.media_object.mime_type if asset and asset.media_object else None,
            )
        if artifact is not None:
            record = GraphSourceRecord(
                facts=record.facts,
                product_source=record.product_source,
                brief=record.brief,
                visual_payload=record.visual_payload,
                visual_system_version_id=record.visual_system_version_id,
                bound_asset_id=record.bound_asset_id,
                bound_asset_label=record.bound_asset_label,
                bound_asset_mime_type=record.bound_asset_mime_type,
                current_artifact_id=artifact.id,
                current_artifact_type=GraphArtifactType(artifact.artifact_type),
                current_artifact_payload=dict(artifact.payload_json),
                current_output_asset_id=artifact.product_image_asset_id,
                current_input_digest=artifact.input_digest,
            )
        sources[node.id] = record
    return sources


def submit_graph_run(
    session: Session,
    *,
    product_id: str,
    graph_id: str,
    scope: GraphRunScope,
    target_node_id: str | None = None,
    enqueue: Callable[[str], None] | None = None,
    commit: bool = True,
) -> GraphRunSubmission:
    graph = session.scalar(
        select(WorkflowGraph)
        .where(WorkflowGraph.id == graph_id, WorkflowGraph.product_id == product_id)
        .with_for_update()
    )
    if graph is None:
        raise NotFoundError("商品工作流不存在")
    if not graph.active:
        raise ConflictError("只能运行 active schema-v3 工作流")
    applied = load_applied_graph(session, graph)
    sources = load_graph_sources(session, graph, applied)
    selected = select_run_node_ids(
        applied,
        scope=scope,
        target_node_id=target_node_id,
        artifacts=GraphRuntimeArtifacts(
            payloads={
                node_id: dict(record.current_artifact_payload)
                for node_id, record in sources.items()
                if record.current_artifact_payload is not None
            },
            artifact_ids={
                node_id: record.current_artifact_id
                for node_id, record in sources.items()
                if record.current_artifact_id is not None
            },
            output_asset_ids={
                node_id: record.current_output_asset_id
                for node_id, record in sources.items()
                if record.current_output_asset_id is not None
            },
        )
        if scope == GraphRunScope.NODE
        else None,
    )
    active = session.scalar(
        select(WorkflowGraphRun).where(
            WorkflowGraphRun.graph_id == graph.id,
            WorkflowGraphRun.status == WorkflowRunStatus.RUNNING,
        )
    )
    if active is not None:
        if (
            GraphRunScope(active.run_scope) == scope
            and active.requested_node_id == target_node_id
            and active.graph_revision == graph.revision
        ):
            if enqueue is None:
                stage_graph_run_dispatch(session, active.id)
            if commit:
                session.commit()
                session.expire_all()
                active = get_graph_run(session, product_id=product_id, graph_id=graph_id, run_id=active.id)
            return GraphRunSubmission(run=active, created=False)
        raise ConflictError("工作流已有正在进行的运行")
    snapshot = snapshot_graph(applied, sources)
    run = WorkflowGraphRun(
        graph_id=graph.id,
        status=WorkflowRunStatus.RUNNING,
        run_scope=scope,
        requested_node_id=target_node_id,
        graph_revision=graph.revision,
        snapshot_json=snapshot,
        progress_metadata={"run_scope": scope.value, "requested_node_id": target_node_id},
    )
    session.add(run)
    session.flush()
    for index, node_id in enumerate(selected):
        session.add(
            WorkflowGraphNodeRun(
                graph_run_id=run.id,
                node_id=node_id,
                status=WorkflowNodeStatus.QUEUED,
                sort_order=index,
                compiled_context_json={
                    "node_title": graph_snapshot_node_title(snapshot, node_id),
                    "input_trace": graph_snapshot_input_trace(snapshot, node_id),
                },
            )
        )
    session.flush()
    run_id = run.id
    if enqueue is None:
        stage_graph_run_dispatch(session, run_id)
    if commit:
        session.commit()
        session.expire_all()
        if enqueue is not None:
            try:
                enqueue(run_id)
            except Exception as exc:  # noqa: BLE001
                raise_queue_unavailable(exc)
        run = get_graph_run(session, product_id=product_id, graph_id=graph_id, run_id=run_id)
    elif enqueue is not None:
        raise ValueError("不能在延迟提交的图运行中直接 enqueue")
    return GraphRunSubmission(run=run, created=True)


def get_graph_run(
    session: Session,
    *,
    product_id: str,
    graph_id: str,
    run_id: str,
) -> WorkflowGraphRun:
    run = session.scalar(
        select(WorkflowGraphRun)
        .options(selectinload(WorkflowGraphRun.node_runs))
        .where(WorkflowGraphRun.id == run_id, WorkflowGraphRun.graph_id == graph_id)
    )
    if run is None:
        raise NotFoundError("工作流运行不存在")
    get_workflow_graph(session, product_id=product_id, graph_id=graph_id)
    return run


def list_graph_runs(
    session: Session,
    *,
    product_id: str,
    graph_id: str,
    limit: int = 20,
) -> tuple[WorkflowGraphRun, ...]:
    get_workflow_graph(session, product_id=product_id, graph_id=graph_id)
    bounded = min(max(limit, 1), 50)
    return tuple(
        session.scalars(
            select(WorkflowGraphRun)
            .options(selectinload(WorkflowGraphRun.node_runs))
            .where(WorkflowGraphRun.graph_id == graph_id)
            .order_by(WorkflowGraphRun.started_at.desc(), WorkflowGraphRun.id.desc())
            .limit(bounded)
        )
    )


def cancel_graph_run(
    session: Session,
    *,
    product_id: str,
    graph_id: str,
    run_id: str,
) -> WorkflowGraphRun:
    get_workflow_graph(session, product_id=product_id, graph_id=graph_id)
    run = session.scalar(
        select(WorkflowGraphRun)
        .options(selectinload(WorkflowGraphRun.node_runs))
        .where(WorkflowGraphRun.id == run_id, WorkflowGraphRun.graph_id == graph_id)
        .with_for_update()
    )
    if run is None:
        raise NotFoundError("工作流运行不存在")
    if run.status == WorkflowRunStatus.CANCELLED:
        return run
    if GRAPH_RUN_GENERATION_TASK_CONTRACT.is_terminal(run.status):
        raise ConflictError("已结束的工作流运行不能取消")
    now = now_utc()
    run.status = WorkflowRunStatus.CANCELLED
    run.failure_reason = GRAPH_CANCELLED_REASON
    run.finished_at = now
    for node_run in run.node_runs:
        if node_run.status in {WorkflowNodeStatus.QUEUED, WorkflowNodeStatus.RUNNING}:
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.failure_reason = GRAPH_CANCELLED_REASON
            node_run.finished_at = now
            node_run.active_attempt_id = None
            node_run.progress_updated_at = now
    session.commit()
    session.expire_all()
    return get_graph_run(session, product_id=product_id, graph_id=graph_id, run_id=run_id)


def retry_graph_run(
    session: Session,
    *,
    product_id: str,
    graph_id: str,
    run_id: str,
    enqueue: Callable[[str], None] | None = None,
    commit: bool = True,
) -> GraphRunSubmission:
    source = get_graph_run(session, product_id=product_id, graph_id=graph_id, run_id=run_id)
    if source.status != WorkflowRunStatus.FAILED:
        raise BusinessValidationError("只有失败的工作流运行可以重试")
    if not source.is_retryable:
        raise BusinessValidationError("该工作流运行不可重试")
    return submit_graph_run(
        session,
        product_id=product_id,
        graph_id=graph_id,
        scope=GraphRunScope(source.run_scope),
        target_node_id=source.requested_node_id,
        enqueue=enqueue,
        commit=commit,
    )


def _asset_metadata(session: Session, *, product_id: str, asset_id: str | None) -> ProductImageAsset | None:
    if not asset_id:
        return None
    return session.scalar(
        select(ProductImageAsset)
        .options(selectinload(ProductImageAsset.media_object))
        .where(ProductImageAsset.id == asset_id, ProductImageAsset.product_id == product_id)
    )

