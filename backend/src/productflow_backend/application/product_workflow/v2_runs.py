from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

from pydantic import ValidationError
from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.admission import ensure_generation_capacity
from productflow_backend.application.product_workflow.run_state import (
    WORKFLOW_CANCELLED_REASON,
    mark_workflow_run_cancelled,
    mark_workflow_run_failed,
    workflow_run_failure_progress_metadata,
)
from productflow_backend.application.product_workflow.v2_staleness import (
    ensure_image_prompt_references_current,
    get_image_prompt_node,
)
from productflow_backend.application.queue_submission import enqueue_or_mark_failed
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.contracts import GenerationSpec, ImagePromptPayloadV1
from productflow_backend.domain.durable_generation_tasks import WORKFLOW_RUN_GENERATION_TASK_CONTRACT
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowNodeType, WorkflowRunStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.domain.workflow_rules import WorkflowRuleEdge, WorkflowRuleNode, topological_node_ids
from productflow_backend.infrastructure.db.models import (
    ImagePromptArtifactVersion,
    ProductWorkflow,
    WorkflowImageGenerationRecord,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)
from productflow_backend.infrastructure.queue import enqueue_workflow_run

V2_WORKFLOW_SCHEMA_VERSION = 2
V2_RUNNABLE_NODE_TYPES = {WorkflowNodeType.PROMPT_GENERATION, WorkflowNodeType.IMAGE_GENERATION}
V2_RUN_SCOPE_NODE = "node"
V2_RUN_SCOPE_WORKFLOW = "workflow"


@dataclass(frozen=True, slots=True)
class V2WorkflowNodeRunSubmission:
    node_run: WorkflowNodeRun
    created: bool


@dataclass(frozen=True, slots=True)
class V2WorkflowRunSubmission:
    run: WorkflowRun
    created: bool


@dataclass(frozen=True, slots=True)
class V2WorkflowRunList:
    workflow: ProductWorkflow
    runs: tuple[WorkflowRun, ...]


def submit_v2_workflow_node_run(
    session: Session,
    *,
    node_id: str,
    enqueue: Callable[[str], None] | None = None,
) -> V2WorkflowNodeRunSubmission:
    node = _get_v2_runnable_node(session, node_id=node_id)
    active_node_run = _find_active_node_run(session, node_id=node.id)
    if active_node_run is not None:
        return V2WorkflowNodeRunSubmission(node_run=active_node_run, created=False)

    submission = _submit_v2_run(
        session,
        workflow=node.workflow,
        ordered_node_ids=(node.id,),
        progress_metadata={"run_scope": V2_RUN_SCOPE_NODE, "requested_node_id": node.id},
        enqueue=enqueue,
        enqueue_failure_node_id=node.id,
        matches_idempotent_active=lambda run: _run_scope(run) == V2_RUN_SCOPE_NODE,
    )
    node_run = next(item for item in submission.run.node_runs if item.node_id == node.id)
    return V2WorkflowNodeRunSubmission(node_run=node_run, created=submission.created)


def submit_v2_workflow_run(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    enqueue: Callable[[str], None] | None = None,
) -> V2WorkflowRunSubmission:
    workflow = _get_v2_workflow(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        require_active=True,
        lock=True,
    )
    ordered_node_ids = _validated_v2_run_node_ids(session, workflow=workflow)
    return _submit_v2_run(
        session,
        workflow=workflow,
        ordered_node_ids=ordered_node_ids,
        progress_metadata={"run_scope": V2_RUN_SCOPE_WORKFLOW},
        enqueue=enqueue,
        enqueue_failure_node_id=None,
        matches_idempotent_active=lambda run: _run_scope(run) == V2_RUN_SCOPE_WORKFLOW,
    )


def get_v2_workflow_run(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    run_id: str,
) -> WorkflowRun:
    run = session.scalar(
        _v2_workflow_run_query().where(
            WorkflowRun.id == run_id,
            WorkflowRun.workflow_id == workflow_id,
            ProductWorkflow.product_id == product_id,
        )
    )
    if run is None:
        raise NotFoundError("工作流运行不存在")
    _ensure_v2_workflow_run(run)
    return run


def list_v2_workflow_runs(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    limit: int = 20,
) -> V2WorkflowRunList:
    workflow = _get_v2_workflow(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        require_active=False,
        lock=False,
    )
    bounded_limit = min(max(limit, 1), 50)
    runs = tuple(
        session.scalars(
            _v2_workflow_run_query()
            .where(WorkflowRun.workflow_id == workflow_id, ProductWorkflow.product_id == product_id)
            .order_by(WorkflowRun.started_at.desc(), WorkflowRun.id.desc())
            .limit(bounded_limit)
        )
    )
    return V2WorkflowRunList(workflow=workflow, runs=runs)


def cancel_v2_workflow_run(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    run_id: str,
) -> WorkflowRun:
    run = get_v2_workflow_run(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        run_id=run_id,
    )
    if run.status == WorkflowRunStatus.CANCELLED:
        return run
    if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_terminal(run.status):
        raise ConflictError("已结束的工作流运行不能取消")
    mark_workflow_run_cancelled(session, run_id=run.id)
    session.expire_all()
    return get_v2_workflow_run(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        run_id=run_id,
    )


def retry_v2_workflow_run(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    run_id: str,
    enqueue: Callable[[str], None] | None = None,
) -> V2WorkflowRunSubmission:
    source_run = get_v2_workflow_run(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        run_id=run_id,
    )
    if source_run.status != WorkflowRunStatus.FAILED:
        raise BusinessValidationError("只有失败的工作流运行可以重试")
    if not source_run.is_retryable:
        raise BusinessValidationError("该工作流运行不可重试")
    retry_node_ids = {
        node_run.node_id
        for node_run in source_run.node_runs
        if node_run.status == WorkflowNodeStatus.FAILED
        and node_run.failure_reason != WORKFLOW_CANCELLED_REASON
    }
    if not retry_node_ids:
        raise BusinessValidationError("工作流运行没有可重试节点")

    workflow = _get_v2_workflow(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        require_active=True,
        lock=True,
    )
    ordered_node_ids = _validated_v2_run_node_ids(session, workflow=workflow, selected_node_ids=retry_node_ids)
    metadata = _retry_progress_metadata(source_run)
    return _submit_v2_run(
        session,
        workflow=workflow,
        ordered_node_ids=ordered_node_ids,
        progress_metadata=metadata,
        enqueue=enqueue,
        enqueue_failure_node_id=None,
        matches_idempotent_active=lambda run: (
            isinstance(run.progress_metadata, dict)
            and run.progress_metadata.get("source_run_id") == source_run.id
            and run.progress_metadata.get("manual_retry") is True
        ),
    )


def get_v2_workflow_node_run(session: Session, *, node_run_id: str) -> WorkflowNodeRun:
    node_run = session.scalar(_v2_node_run_query().where(WorkflowNodeRun.id == node_run_id))
    if node_run is None:
        raise NotFoundError("工作流节点运行不存在")
    workflow = node_run.workflow_run.workflow
    if (
        workflow.schema_version != V2_WORKFLOW_SCHEMA_VERSION
        or node_run.node.schema_version != V2_WORKFLOW_SCHEMA_VERSION
    ):
        raise ConflictError("v2 节点运行接口拒绝读取不支持的运行版本")
    return node_run


def list_v2_workflow_node_runs(
    session: Session,
    *,
    node_id: str,
    limit: int = 20,
) -> tuple[WorkflowNodeRun, ...]:
    _get_v2_node(session, node_id=node_id, lock=False)
    bounded_limit = min(max(limit, 1), 50)
    return tuple(
        session.scalars(
            _v2_node_run_query()
            .where(WorkflowNodeRun.node_id == node_id)
            .order_by(WorkflowNodeRun.started_at.desc(), WorkflowNodeRun.id.desc())
            .limit(bounded_limit)
        )
    )


def cancel_v2_workflow_node_run(session: Session, *, node_run_id: str) -> WorkflowNodeRun:
    node_run = get_v2_workflow_node_run(session, node_run_id=node_run_id)
    run = node_run.workflow_run
    if run.status == WorkflowRunStatus.CANCELLED:
        return node_run
    if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_terminal(run.status):
        raise ConflictError("已结束的工作流节点运行不能取消")
    mark_workflow_run_cancelled(session, run_id=run.id)
    session.expire_all()
    return get_v2_workflow_node_run(session, node_run_id=node_run_id)


def _submit_v2_run(
    session: Session,
    *,
    workflow: ProductWorkflow,
    ordered_node_ids: tuple[str, ...],
    progress_metadata: dict[str, Any],
    enqueue: Callable[[str], None] | None,
    enqueue_failure_node_id: str | None,
    matches_idempotent_active: Callable[[WorkflowRun], bool],
) -> V2WorkflowRunSubmission:
    node_id_set = set(ordered_node_ids)
    active_run = _find_active_run_for_nodes(session, workflow_id=workflow.id, node_ids=node_id_set)
    if active_run is not None:
        if _run_node_ids(active_run) == node_id_set and matches_idempotent_active(active_run):
            return V2WorkflowRunSubmission(run=active_run, created=False)
        raise ConflictError("相关节点已有运行中的任务")

    ensure_generation_capacity(session)
    now = now_utc()
    nodes_by_id = {node.id: node for node in workflow.nodes}
    run = WorkflowRun(
        workflow_id=workflow.id,
        status=WorkflowRunStatus.RUNNING,
        progress_metadata=progress_metadata,
    )
    session.add(run)
    session.flush()
    for node_id in ordered_node_ids:
        node = nodes_by_id[node_id]
        node.status = WorkflowNodeStatus.QUEUED
        node.failure_reason = None
        node.last_run_at = now
        session.add(
            WorkflowNodeRun(
                workflow_run_id=run.id,
                node_id=node.id,
                status=WorkflowNodeStatus.QUEUED,
            )
        )
    workflow.updated_at = now
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        active_run = _find_active_run_for_nodes(session, workflow_id=workflow.id, node_ids=node_id_set)
        if (
            active_run is not None
            and _run_node_ids(active_run) == node_id_set
            and matches_idempotent_active(active_run)
        ):
            return V2WorkflowRunSubmission(run=active_run, created=False)
        if active_run is not None:
            raise ConflictError("相关节点已有运行中的任务") from None
        raise

    enqueue_or_mark_failed(
        run.id,
        enqueue=enqueue or enqueue_workflow_run,
        mark_failed=lambda run_id, reason: mark_workflow_run_failed(
            session,
            run_id=run_id,
            failed_node_id=enqueue_failure_node_id,
            reason=reason,
        ),
    )
    session.expire_all()
    return V2WorkflowRunSubmission(
        run=_get_v2_workflow_run_by_id(session, run_id=run.id),
        created=True,
    )


def _get_v2_workflow(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    require_active: bool,
    lock: bool,
) -> ProductWorkflow:
    statement = (
        select(ProductWorkflow)
        .options(
            selectinload(ProductWorkflow.nodes).selectinload(WorkflowNode.current_prompt_artifact_version),
            selectinload(ProductWorkflow.edges),
        )
        .where(ProductWorkflow.id == workflow_id, ProductWorkflow.product_id == product_id)
    )
    if lock:
        statement = statement.with_for_update()
    workflow = session.scalar(statement)
    if workflow is None:
        raise NotFoundError("工作流不存在")
    if workflow.schema_version != V2_WORKFLOW_SCHEMA_VERSION:
        raise ConflictError("v2 工作流运行接口拒绝处理不支持的工作流版本")
    if require_active and not workflow.active:
        raise ConflictError("只能运行 active schema-v2 工作流")
    return workflow


def _validated_v2_run_node_ids(
    session: Session,
    *,
    workflow: ProductWorkflow,
    selected_node_ids: set[str] | None = None,
) -> tuple[str, ...]:
    if any(node.schema_version != V2_WORKFLOW_SCHEMA_VERSION for node in workflow.nodes):
        raise ConflictError("schema-v2 工作流包含其他 schema version 节点")
    rule_nodes = [
        WorkflowRuleNode(
            id=node.id,
            node_type=node.node_type,
            position_x=node.position_x,
            config_json=node.config_json,
        )
        for node in workflow.nodes
    ]
    rule_edges = [
        WorkflowRuleEdge(source_node_id=edge.source_node_id, target_node_id=edge.target_node_id)
        for edge in workflow.edges
    ]
    ordered_ids = topological_node_ids(rule_nodes, rule_edges)
    nodes_by_id = {node.id: node for node in workflow.nodes}
    runnable_ids = {
        node.id for node in workflow.nodes if node.node_type in V2_RUNNABLE_NODE_TYPES
    }
    requested_ids = runnable_ids if selected_node_ids is None else set(selected_node_ids)
    if not requested_ids:
        raise BusinessValidationError("工作流没有可运行节点")
    if not requested_ids.issubset(runnable_ids):
        raise ConflictError("schema-v2 工作流运行包含不支持的节点")

    for node_id in ordered_ids:
        if node_id not in requested_ids:
            continue
        node = nodes_by_id[node_id]
        if node.node_type == WorkflowNodeType.PROMPT_GENERATION:
            _validate_prompt_node(node)
        else:
            _validate_image_node(session, image_node=node, selected_node_ids=requested_ids)
    return tuple(node_id for node_id in ordered_ids if node_id in requested_ids)


def _validate_prompt_node(node: WorkflowNode) -> None:
    image_type_key = node.config_json.get("image_type_key")
    prompt_plan_key = node.config_json.get("prompt_plan_key")
    if not all(isinstance(value, str) and value for value in (image_type_key, prompt_plan_key)):
        raise ConflictError("提示词节点缺少 image type 或 prompt plan key")
    version = node.current_prompt_artifact_version
    if version is None:
        raise ConflictError("提示词节点缺少 current Prompt Artifact version")
    artifact = version.artifact
    if artifact.workflow_id != node.workflow_id or artifact.image_type_key != image_type_key:
        raise ConflictError("提示词节点与 Prompt Artifact lineage 不一致")
    try:
        ImagePromptPayloadV1.model_validate(version.payload_json)
    except ValidationError as exc:
        raise ConflictError("Prompt Artifact version payload 不符合 schema version 1") from exc


def _validate_image_node(
    session: Session,
    *,
    image_node: WorkflowNode,
    selected_node_ids: set[str],
) -> None:
    image_type_key = image_node.config_json.get("image_type_key")
    image_plan_key = image_node.config_json.get("image_plan_key")
    prompt_plan_key = image_node.config_json.get("prompt_plan_key")
    if not all(isinstance(value, str) and value for value in (image_type_key, image_plan_key, prompt_plan_key)):
        raise ConflictError("图片节点缺少 image type、image plan 或 prompt plan key")
    try:
        GenerationSpec.model_validate(image_node.config_json.get("generation_spec"))
    except ValidationError as exc:
        raise ConflictError("图片节点 GenerationSpec 不符合 schema") from exc
    prompt_node = get_image_prompt_node(session, image_node=image_node)
    if prompt_node.id not in selected_node_ids:
        ensure_image_prompt_references_current(session, image_node=image_node)
    _validate_prompt_node(prompt_node)
    prompt_payload = ImagePromptPayloadV1.model_validate(
        prompt_node.current_prompt_artifact_version.payload_json
    )
    if not any(image.image_plan_key == image_plan_key for image in prompt_payload.images):
        raise ConflictError("Prompt Artifact 缺少当前图片节点的逐图计划")


def _get_v2_node(session: Session, *, node_id: str, lock: bool = True) -> WorkflowNode:
    statement = (
        select(WorkflowNode)
        .options(selectinload(WorkflowNode.workflow))
        .where(WorkflowNode.id == node_id)
    )
    if lock:
        statement = statement.with_for_update()
    node = session.scalar(statement)
    if node is None:
        raise NotFoundError("工作流节点不存在")
    workflow = node.workflow
    if workflow.schema_version != V2_WORKFLOW_SCHEMA_VERSION or node.schema_version != V2_WORKFLOW_SCHEMA_VERSION:
        raise ConflictError("v2 节点运行接口拒绝提交不支持的节点版本")
    if not workflow.active:
        raise ConflictError("只能运行 active schema-v2 工作流")
    return node


def _get_v2_runnable_node(session: Session, *, node_id: str) -> WorkflowNode:
    node = _get_v2_node(session, node_id=node_id)
    if node.node_type not in V2_RUNNABLE_NODE_TYPES:
        raise ConflictError("schema-v2 只允许运行提示词或图片生成节点")
    if node.node_type == WorkflowNodeType.IMAGE_GENERATION:
        ensure_image_prompt_references_current(session, image_node=node)
    return node


def _find_active_node_run(session: Session, *, node_id: str) -> WorkflowNodeRun | None:
    return session.scalar(
        _v2_node_run_query()
        .join(WorkflowRun, WorkflowRun.id == WorkflowNodeRun.workflow_run_id)
        .where(
            WorkflowNodeRun.node_id == node_id,
            WorkflowNodeRun.status.in_((WorkflowNodeStatus.QUEUED, WorkflowNodeStatus.RUNNING)),
            WorkflowRun.status.in_(WORKFLOW_RUN_GENERATION_TASK_CONTRACT.active_statuses),
        )
        .order_by(WorkflowNodeRun.started_at.desc(), WorkflowNodeRun.id.desc())
    )


def _find_active_run_for_nodes(
    session: Session,
    *,
    workflow_id: str,
    node_ids: set[str],
) -> WorkflowRun | None:
    return session.scalar(
        _v2_workflow_run_query()
        .join(WorkflowNodeRun, WorkflowNodeRun.workflow_run_id == WorkflowRun.id)
        .where(
            WorkflowRun.workflow_id == workflow_id,
            WorkflowRun.status.in_(WORKFLOW_RUN_GENERATION_TASK_CONTRACT.active_statuses),
            WorkflowNodeRun.node_id.in_(node_ids),
        )
        .order_by(WorkflowRun.started_at.desc(), WorkflowRun.id.desc())
    )


def _get_v2_workflow_run_by_id(session: Session, *, run_id: str) -> WorkflowRun:
    run = session.scalar(_v2_workflow_run_query().where(WorkflowRun.id == run_id))
    if run is None:
        raise NotFoundError("工作流运行不存在")
    _ensure_v2_workflow_run(run)
    return run


def _ensure_v2_workflow_run(run: WorkflowRun) -> None:
    if run.workflow.schema_version != V2_WORKFLOW_SCHEMA_VERSION:
        raise ConflictError("v2 工作流运行接口拒绝读取不支持的运行版本")
    if any(node_run.node.schema_version != V2_WORKFLOW_SCHEMA_VERSION for node_run in run.node_runs):
        raise ConflictError("v2 工作流运行包含不支持的节点版本")


def _run_node_ids(run: WorkflowRun) -> set[str]:
    return {node_run.node_id for node_run in run.node_runs}


def _run_scope(run: WorkflowRun) -> str | None:
    metadata = run.progress_metadata if isinstance(run.progress_metadata, dict) else {}
    scope = metadata.get("run_scope")
    return scope if isinstance(scope, str) else None


def _retry_progress_metadata(run: WorkflowRun) -> dict[str, Any]:
    metadata = workflow_run_failure_progress_metadata(
        reason=run.failure_reason or "工作流部分节点失败",
        retryable=run.is_retryable,
    )
    previous = run.progress_metadata if isinstance(run.progress_metadata, dict) else {}
    for key in ("last_failure_category", "retry_hint"):
        if isinstance(previous.get(key), str):
            metadata[key] = previous[key]
    metadata.update(
        {
            "run_scope": V2_RUN_SCOPE_WORKFLOW,
            "source_run_id": run.id,
            "manual_retry": True,
        }
    )
    return metadata


def _v2_workflow_run_query():
    return (
        select(WorkflowRun)
        .join(ProductWorkflow, ProductWorkflow.id == WorkflowRun.workflow_id)
        .options(
            selectinload(WorkflowRun.workflow).selectinload(ProductWorkflow.folders),
            selectinload(WorkflowRun.workflow).selectinload(ProductWorkflow.nodes),
            selectinload(WorkflowRun.workflow).selectinload(ProductWorkflow.edges),
            selectinload(WorkflowRun.workflow).selectinload(ProductWorkflow.materialization),
            selectinload(WorkflowRun.node_runs).selectinload(WorkflowNodeRun.node),
            selectinload(WorkflowRun.node_runs)
            .selectinload(WorkflowNodeRun.node)
            .selectinload(WorkflowNode.current_prompt_artifact_version),
            selectinload(WorkflowRun.node_runs)
            .selectinload(WorkflowNodeRun.prompt_artifact_version)
            .selectinload(ImagePromptArtifactVersion.references),
            selectinload(WorkflowRun.node_runs)
            .selectinload(WorkflowNodeRun.image_generation_record)
            .selectinload(WorkflowImageGenerationRecord.references),
        )
    )


def _v2_node_run_query():
    return select(WorkflowNodeRun).options(
        selectinload(WorkflowNodeRun.node),
        selectinload(WorkflowNodeRun.node).selectinload(WorkflowNode.current_prompt_artifact_version),
        selectinload(WorkflowNodeRun.workflow_run).selectinload(WorkflowRun.workflow),
        selectinload(WorkflowNodeRun.prompt_artifact_version),
        selectinload(WorkflowNodeRun.prompt_artifact_version).selectinload(ImagePromptArtifactVersion.references),
        selectinload(WorkflowNodeRun.image_generation_record).selectinload(
            WorkflowImageGenerationRecord.references
        ),
    )


__all__ = [
    "V2WorkflowNodeRunSubmission",
    "V2WorkflowRunList",
    "V2WorkflowRunSubmission",
    "cancel_v2_workflow_node_run",
    "cancel_v2_workflow_run",
    "get_v2_workflow_node_run",
    "get_v2_workflow_run",
    "list_v2_workflow_node_runs",
    "list_v2_workflow_runs",
    "retry_v2_workflow_run",
    "submit_v2_workflow_node_run",
    "submit_v2_workflow_run",
]
