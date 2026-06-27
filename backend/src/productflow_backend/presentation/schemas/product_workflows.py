from __future__ import annotations

from datetime import datetime
from typing import Any, Literal

from pydantic import BaseModel, Field

from productflow_backend.application.canvas_templates import (
    CanvasTemplate,
    CanvasTemplateScenario,
    TemplateKind,
)
from productflow_backend.application.product_workflow.graph import ProductWorkflowStatusSnapshot
from productflow_backend.application.product_workflow.run_state import (
    WORKFLOW_CANCELLED_REASON,
    workflow_node_failed_run_is_retryable,
)
from productflow_backend.application.product_workflows import latest_workflow_runs
from productflow_backend.domain.durable_generation_tasks import WORKFLOW_RUN_GENERATION_TASK_CONTRACT
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowNodeType, WorkflowRunStatus
from productflow_backend.infrastructure.db.models import (
    ProductWorkflow,
    UserCanvasTemplate,
    WorkflowEdge,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)

WorkflowNodeDisplayStatus = WorkflowNodeStatus | Literal["cancelled"]
WorkflowRetryHint = Literal["retry_later", "revise_input", "check_settings"]


class WorkflowNodeResponse(BaseModel):
    id: str
    workflow_id: str
    node_type: WorkflowNodeType
    title: str
    position_x: int
    position_y: int
    config_json: dict[str, Any]
    status: WorkflowNodeDisplayStatus
    output_json: dict[str, Any] | None = None
    failure_reason: str | None = None
    is_retryable: bool
    attempt_count: int
    retry_count: int
    non_retryable_reason: str | None = None
    retry_hint: WorkflowRetryHint | None = None
    last_run_at: datetime | None = None
    created_at: datetime
    updated_at: datetime


class WorkflowEdgeResponse(BaseModel):
    id: str
    workflow_id: str
    source_node_id: str
    target_node_id: str
    source_handle: str | None = None
    target_handle: str | None = None
    created_at: datetime


class WorkflowNodeRunResponse(BaseModel):
    id: str
    workflow_run_id: str
    node_id: str
    status: WorkflowNodeDisplayStatus
    output_json: dict[str, Any] | None = None
    failure_reason: str | None = None
    copy_set_id: str | None = None
    poster_variant_id: str | None = None
    started_at: datetime
    finished_at: datetime | None = None


class WorkflowNodeRunStatusResponse(BaseModel):
    id: str
    workflow_run_id: str
    node_id: str
    status: WorkflowNodeDisplayStatus
    failure_reason: str | None = None
    started_at: datetime
    finished_at: datetime | None = None


class WorkflowRunResponse(BaseModel):
    id: str
    workflow_id: str
    status: WorkflowRunStatus
    started_at: datetime
    finished_at: datetime | None = None
    failure_reason: str | None = None
    progress_metadata: dict[str, Any] | None = None
    is_retryable: bool
    is_cancelable: bool
    queue_active_count: int
    queue_running_count: int
    queue_queued_count: int
    queue_max_concurrent_tasks: int
    queued_ahead_count: int | None = None
    queue_position: int | None = None
    node_runs: list[WorkflowNodeRunResponse]


class WorkflowRunStatusResponse(BaseModel):
    id: str
    workflow_id: str
    status: WorkflowRunStatus
    started_at: datetime
    finished_at: datetime | None = None
    failure_reason: str | None = None
    progress_metadata: dict[str, Any] | None = None
    is_retryable: bool
    is_cancelable: bool
    queue_active_count: int
    queue_running_count: int
    queue_queued_count: int
    queue_max_concurrent_tasks: int
    queued_ahead_count: int | None = None
    queue_position: int | None = None
    node_runs: list[WorkflowNodeRunStatusResponse]


class WorkflowNodeStatusResponse(BaseModel):
    id: str
    workflow_id: str
    status: WorkflowNodeDisplayStatus
    failure_reason: str | None = None
    is_retryable: bool
    attempt_count: int
    retry_count: int
    non_retryable_reason: str | None = None
    retry_hint: WorkflowRetryHint | None = None
    last_run_at: datetime | None = None
    updated_at: datetime


class ProductWorkflowResponse(BaseModel):
    id: str
    product_id: str
    title: str
    active: bool
    nodes: list[WorkflowNodeResponse]
    edges: list[WorkflowEdgeResponse]
    runs: list[WorkflowRunResponse]
    created_at: datetime
    updated_at: datetime


class ProductWorkflowStatusResponse(BaseModel):
    id: str
    product_id: str
    title: str
    active: bool
    has_active_workflow: bool
    nodes: list[WorkflowNodeStatusResponse]
    runs: list[WorkflowRunStatusResponse]
    created_at: datetime
    updated_at: datetime


class CanvasTemplateScenarioResponse(BaseModel):
    scenario: CanvasTemplateScenario
    title: str
    description: str
    ecommerce_stage: str
    tags: list[str]


class CanvasTemplateReferenceInputHintResponse(BaseModel):
    node_key: str
    role: str
    label: str
    required: bool
    description: str


class CanvasTemplateOutputSlotResponse(BaseModel):
    node_key: str
    label: str
    description: str


class CanvasTemplateSuggestedConnectionResponse(BaseModel):
    source_node_key: str
    target_node_key: str
    reason: str


class CanvasTemplateDefaultExternalConnectionResponse(BaseModel):
    source: str
    target_node_key: str
    label: str


class CanvasTemplatePreviewNodeResponse(BaseModel):
    key: str
    node_type: WorkflowNodeType
    title: str
    position_x: int
    position_y: int
    size: str | None = None


class CanvasTemplatePreviewEdgeResponse(BaseModel):
    source_node_key: str
    target_node_key: str


class CanvasTemplateSummaryResponse(BaseModel):
    key: str
    version: int
    kind: TemplateKind
    title: str
    description: str
    source: str
    user_template_id: str | None = None
    scenario: CanvasTemplateScenarioResponse
    preview_nodes: list[CanvasTemplatePreviewNodeResponse]
    preview_edges: list[CanvasTemplatePreviewEdgeResponse]
    output_slots: list[CanvasTemplateOutputSlotResponse]
    reference_input_hints: list[CanvasTemplateReferenceInputHintResponse]
    suggested_connections: list[CanvasTemplateSuggestedConnectionResponse]
    default_external_connections: list[CanvasTemplateDefaultExternalConnectionResponse]


class CanvasTemplateListResponse(BaseModel):
    items: list[CanvasTemplateSummaryResponse]


class CreateWorkflowNodeRequest(BaseModel):
    node_type: WorkflowNodeType
    title: str = Field(min_length=1, max_length=255)
    position_x: int = 0
    position_y: int = 0
    config_json: dict[str, Any] = Field(default_factory=dict)


class UpdateWorkflowNodeRequest(BaseModel):
    title: str | None = Field(default=None, min_length=1, max_length=255)
    position_x: int | None = None
    position_y: int | None = None
    config_json: dict[str, Any] | None = None


class UpdateWorkflowCopySetRequest(BaseModel):
    structured_payload: dict[str, Any]


class BindWorkflowNodeImageRequest(BaseModel):
    source_asset_id: str | None = None
    poster_variant_id: str | None = None


class CreateWorkflowEdgeRequest(BaseModel):
    source_node_id: str
    target_node_id: str
    source_handle: str | None = Field(default=None, max_length=80)
    target_handle: str | None = Field(default=None, max_length=80)


class ApplyWorkflowTemplateGroupRequest(BaseModel):
    template_key: str = Field(min_length=1, max_length=120)
    position_x: int = 0
    position_y: int = 0
    template_language: str | None = Field(default=None, max_length=120)


class DuplicateWorkflowNodeGroupRequest(BaseModel):
    node_ids: list[str] = Field(default_factory=list)
    position_x: int | None = None
    position_y: int | None = None
    offset_x: int = 48
    offset_y: int = 48


class CreateUserTemplateGroupRequest(BaseModel):
    title: str = Field(min_length=1, max_length=255)
    description: str | None = Field(default=None, max_length=1000)
    node_ids: list[str] = Field(default_factory=list)


class UpdateUserTemplateGroupRequest(BaseModel):
    title: str | None = Field(default=None, min_length=1, max_length=255)
    description: str | None = Field(default=None, max_length=1000)


class RunWorkflowRequest(BaseModel):
    start_node_id: str | None = None


def workflow_run_is_retryable(run: WorkflowRun) -> bool:
    return run.status == WorkflowRunStatus.FAILED and run.is_retryable


def workflow_run_is_cancelable(run: WorkflowRun) -> bool:
    return WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_active(run.status)


def workflow_run_queue_fields(run: WorkflowRun) -> dict[str, int | None]:
    queue_metadata = getattr(run, "_queue_metadata", None)
    queue_overview = getattr(queue_metadata, "overview", None)
    return {
        "queue_active_count": getattr(queue_overview, "active_count", 0),
        "queue_running_count": getattr(queue_overview, "running_count", 0),
        "queue_queued_count": getattr(queue_overview, "queued_count", 0),
        "queue_max_concurrent_tasks": getattr(queue_overview, "max_concurrent_tasks", 0),
        "queued_ahead_count": getattr(queue_metadata, "queued_ahead_count", None),
        "queue_position": getattr(queue_metadata, "queue_position", None),
    }


def workflow_node_display_status(node: WorkflowNode) -> WorkflowNodeDisplayStatus:
    if node.status == WorkflowNodeStatus.FAILED and node.failure_reason == WORKFLOW_CANCELLED_REASON:
        return "cancelled"
    return node.status


def workflow_node_run_display_status(node_run: WorkflowNodeRun) -> WorkflowNodeDisplayStatus:
    run = node_run.workflow_run
    if (
        run is not None
        and run.status == WorkflowRunStatus.CANCELLED
        and node_run.status == WorkflowNodeStatus.FAILED
        and node_run.failure_reason == WORKFLOW_CANCELLED_REASON
    ):
        return "cancelled"
    return node_run.status


def _workflow_retry_hint(value: Any) -> WorkflowRetryHint | None:
    if value in {"retry_later", "revise_input", "check_settings"}:
        return value
    return None


def _workflow_run_retry_hint(run: WorkflowRun) -> WorkflowRetryHint | None:
    metadata = run.progress_metadata if isinstance(run.progress_metadata, dict) else {}
    return _workflow_retry_hint(metadata.get("retry_hint"))


def _workflow_run_failure_reason(run: WorkflowRun) -> str | None:
    metadata = run.progress_metadata if isinstance(run.progress_metadata, dict) else {}
    reason = metadata.get("last_failure_reason")
    return reason if isinstance(reason, str) and reason.strip() else run.failure_reason


def _workflow_node_attempt_runs(
    node: WorkflowNode,
    runs: list[WorkflowRun],
) -> list[tuple[WorkflowRun, WorkflowNodeRun]]:
    ordered_runs = sorted(runs, key=lambda item: (item.started_at, item.id))
    attempts: list[tuple[WorkflowRun, WorkflowNodeRun]] = []
    for run in ordered_runs:
        for node_run in run.node_runs:
            if node_run.node_id != node.id:
                continue
            if node_run.failure_reason == "上游节点失败":
                continue
            attempts.append((run, node_run))
            break
    return attempts


def workflow_node_attempt_count(node: WorkflowNode, runs: list[WorkflowRun]) -> int:
    return len(_workflow_node_attempt_runs(node, runs))


def workflow_node_retry_count(node: WorkflowNode, runs: list[WorkflowRun]) -> int:
    return max(0, workflow_node_attempt_count(node, runs) - 1)


def workflow_node_latest_failed_run(node: WorkflowNode, runs: list[WorkflowRun]) -> WorkflowRun | None:
    attempts = _workflow_node_attempt_runs(node, runs)
    for run, node_run in reversed(attempts):
        if run.status == WorkflowRunStatus.FAILED and node_run.status == WorkflowNodeStatus.FAILED:
            return run
    return None


def workflow_node_non_retryable_reason(node: WorkflowNode, runs: list[WorkflowRun]) -> str | None:
    if node.status != WorkflowNodeStatus.FAILED:
        return None
    if workflow_node_failed_run_is_retryable(node, runs):
        return None
    failed_run = workflow_node_latest_failed_run(node, runs)
    if failed_run is None:
        return node.failure_reason
    return _workflow_run_failure_reason(failed_run)


def workflow_node_retry_hint(node: WorkflowNode, runs: list[WorkflowRun]) -> WorkflowRetryHint | None:
    if node.status != WorkflowNodeStatus.FAILED:
        return None
    failed_run = workflow_node_latest_failed_run(node, runs)
    if failed_run is None:
        return None
    return _workflow_run_retry_hint(failed_run)


def serialize_workflow_node(node: WorkflowNode, runs: list[WorkflowRun] | None = None) -> WorkflowNodeResponse:
    context_runs = runs or []
    return WorkflowNodeResponse(
        id=node.id,
        workflow_id=node.workflow_id,
        node_type=node.node_type,
        title=node.title,
        position_x=node.position_x,
        position_y=node.position_y,
        config_json=node.config_json,
        status=workflow_node_display_status(node),
        output_json=node.output_json,
        failure_reason=node.failure_reason,
        is_retryable=workflow_node_failed_run_is_retryable(node, context_runs),
        attempt_count=workflow_node_attempt_count(node, context_runs),
        retry_count=workflow_node_retry_count(node, context_runs),
        non_retryable_reason=workflow_node_non_retryable_reason(node, context_runs),
        retry_hint=workflow_node_retry_hint(node, context_runs),
        last_run_at=node.last_run_at,
        created_at=node.created_at,
        updated_at=node.updated_at,
    )


def serialize_workflow_edge(edge: WorkflowEdge) -> WorkflowEdgeResponse:
    return WorkflowEdgeResponse(
        id=edge.id,
        workflow_id=edge.workflow_id,
        source_node_id=edge.source_node_id,
        target_node_id=edge.target_node_id,
        source_handle=edge.source_handle,
        target_handle=edge.target_handle,
        created_at=edge.created_at,
    )


def serialize_workflow_node_run(node_run: WorkflowNodeRun) -> WorkflowNodeRunResponse:
    return WorkflowNodeRunResponse(
        id=node_run.id,
        workflow_run_id=node_run.workflow_run_id,
        node_id=node_run.node_id,
        status=workflow_node_run_display_status(node_run),
        output_json=node_run.output_json,
        failure_reason=node_run.failure_reason,
        copy_set_id=node_run.copy_set_id,
        poster_variant_id=node_run.poster_variant_id,
        started_at=node_run.started_at,
        finished_at=node_run.finished_at,
    )


def serialize_workflow_node_run_status(node_run: WorkflowNodeRun) -> WorkflowNodeRunStatusResponse:
    return WorkflowNodeRunStatusResponse(
        id=node_run.id,
        workflow_run_id=node_run.workflow_run_id,
        node_id=node_run.node_id,
        status=workflow_node_run_display_status(node_run),
        failure_reason=node_run.failure_reason,
        started_at=node_run.started_at,
        finished_at=node_run.finished_at,
    )


def serialize_workflow_run(run: WorkflowRun) -> WorkflowRunResponse:
    node_runs = sorted(run.node_runs, key=lambda item: item.started_at)
    return WorkflowRunResponse(
        id=run.id,
        workflow_id=run.workflow_id,
        status=run.status,
        started_at=run.started_at,
        finished_at=run.finished_at,
        failure_reason=run.failure_reason,
        progress_metadata=run.progress_metadata,
        is_retryable=workflow_run_is_retryable(run),
        is_cancelable=workflow_run_is_cancelable(run),
        **workflow_run_queue_fields(run),
        node_runs=[serialize_workflow_node_run(item) for item in node_runs],
    )


def serialize_workflow_run_status(run: WorkflowRun) -> WorkflowRunStatusResponse:
    node_runs = sorted(run.node_runs, key=lambda item: item.started_at)
    return WorkflowRunStatusResponse(
        id=run.id,
        workflow_id=run.workflow_id,
        status=run.status,
        started_at=run.started_at,
        finished_at=run.finished_at,
        failure_reason=run.failure_reason,
        progress_metadata=run.progress_metadata,
        is_retryable=workflow_run_is_retryable(run),
        is_cancelable=workflow_run_is_cancelable(run),
        **workflow_run_queue_fields(run),
        node_runs=[serialize_workflow_node_run_status(item) for item in node_runs],
    )


def serialize_product_workflow(workflow: ProductWorkflow) -> ProductWorkflowResponse:
    nodes = sorted(workflow.nodes, key=lambda item: (item.position_x, item.position_y, item.created_at))
    edges = sorted(workflow.edges, key=lambda item: item.created_at)
    runs = latest_workflow_runs(workflow)
    node_context_runs = list(workflow.runs)
    return ProductWorkflowResponse(
        id=workflow.id,
        product_id=workflow.product_id,
        title=workflow.title,
        active=workflow.active,
        nodes=[serialize_workflow_node(item, node_context_runs) for item in nodes],
        edges=[serialize_workflow_edge(item) for item in edges],
        runs=[serialize_workflow_run(item) for item in runs],
        created_at=workflow.created_at,
        updated_at=workflow.updated_at,
    )


def serialize_canvas_template_summary(template: CanvasTemplate) -> CanvasTemplateSummaryResponse:
    return CanvasTemplateSummaryResponse(
        key=template.key,
        version=template.version,
        kind=template.kind,
        title=template.title,
        description=template.description,
        source=template.source,
        user_template_id=template.user_template_id,
        scenario=CanvasTemplateScenarioResponse(
            scenario=template.scenario.scenario,
            title=template.scenario.title,
            description=template.scenario.description,
            ecommerce_stage=template.scenario.ecommerce_stage,
            tags=list(template.scenario.tags),
        ),
        preview_nodes=[
            CanvasTemplatePreviewNodeResponse(
                key=item.key,
                node_type=item.node_type,
                title=item.title,
                position_x=item.position_x,
                position_y=item.position_y,
                size=item.size,
            )
            for item in template.nodes
        ],
        preview_edges=[
            CanvasTemplatePreviewEdgeResponse(
                source_node_key=item.source_node_key,
                target_node_key=item.target_node_key,
            )
            for item in template.edges
        ],
        output_slots=[
            CanvasTemplateOutputSlotResponse(
                node_key=item.node_key,
                label=item.label,
                description=item.description,
            )
            for item in template.output_slots
        ],
        reference_input_hints=[
            CanvasTemplateReferenceInputHintResponse(
                node_key=item.node_key,
                role=item.role,
                label=item.label,
                required=item.required,
                description=item.description,
            )
            for item in template.reference_input_hints
        ],
        suggested_connections=[
            CanvasTemplateSuggestedConnectionResponse(
                source_node_key=item.source_node_key,
                target_node_key=item.target_node_key,
                reason=item.reason,
            )
            for item in template.suggested_connections
        ],
        default_external_connections=[
            CanvasTemplateDefaultExternalConnectionResponse(
                source=item.source,
                target_node_key=item.target_node_key,
                label=item.label,
            )
            for item in template.default_external_connections
        ],
    )


def serialize_user_canvas_template_summary(template: UserCanvasTemplate) -> CanvasTemplateSummaryResponse:
    from productflow_backend.application.product_workflow.user_templates import user_canvas_template_to_canvas_template

    return serialize_canvas_template_summary(user_canvas_template_to_canvas_template(template))


def serialize_product_workflow_status(snapshot: ProductWorkflowStatusSnapshot) -> ProductWorkflowStatusResponse:
    workflow = snapshot.workflow
    nodes = snapshot.nodes
    return ProductWorkflowStatusResponse(
        id=workflow.id,
        product_id=workflow.product_id,
        title=workflow.title,
        active=workflow.active,
        has_active_workflow=any(WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_active(item.status) for item in snapshot.runs)
        or any(
            WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(item.status)
            or WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_running(item.status)
            for item in nodes
        ),
        nodes=[
            WorkflowNodeStatusResponse(
                id=item.id,
                workflow_id=item.workflow_id,
                status=workflow_node_display_status(item),
                failure_reason=item.failure_reason,
                is_retryable=workflow_node_failed_run_is_retryable(item, snapshot.node_context_runs),
                attempt_count=workflow_node_attempt_count(item, snapshot.node_context_runs),
                retry_count=workflow_node_retry_count(item, snapshot.node_context_runs),
                non_retryable_reason=workflow_node_non_retryable_reason(item, snapshot.node_context_runs),
                retry_hint=workflow_node_retry_hint(item, snapshot.node_context_runs),
                last_run_at=item.last_run_at,
                updated_at=item.updated_at,
            )
            for item in nodes
        ],
        runs=[serialize_workflow_run_status(item) for item in snapshot.runs],
        created_at=workflow.created_at,
        updated_at=workflow.updated_at,
    )
