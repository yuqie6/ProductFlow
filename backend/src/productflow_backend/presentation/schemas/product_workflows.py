from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel, Field

from productflow_backend.application.product_workflows import latest_workflow_runs
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowNodeType, WorkflowRunStatus
from productflow_backend.infrastructure.db.models import (
    ProductWorkflow,
    WorkflowEdge,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)


class WorkflowNodeResponse(BaseModel):
    id: str
    workflow_id: str
    node_type: WorkflowNodeType
    title: str
    position_x: int
    position_y: int
    config_json: dict[str, Any]
    status: WorkflowNodeStatus
    output_json: dict[str, Any] | None = None
    failure_reason: str | None = None
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
    status: WorkflowNodeStatus
    output_json: dict[str, Any] | None = None
    failure_reason: str | None = None
    copy_set_id: str | None = None
    poster_variant_id: str | None = None
    image_session_asset_id: str | None = None
    started_at: datetime
    finished_at: datetime | None = None


class WorkflowRunResponse(BaseModel):
    id: str
    workflow_id: str
    status: WorkflowRunStatus
    started_at: datetime
    finished_at: datetime | None = None
    failure_reason: str | None = None
    node_runs: list[WorkflowNodeRunResponse]


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
    title: str | None = Field(default=None, min_length=1, max_length=500)
    selling_points: list[str] | None = None
    poster_headline: str | None = Field(default=None, min_length=1, max_length=500)
    cta: str | None = Field(default=None, min_length=1, max_length=300)


class CreateWorkflowEdgeRequest(BaseModel):
    source_node_id: str
    target_node_id: str
    source_handle: str | None = Field(default=None, max_length=80)
    target_handle: str | None = Field(default=None, max_length=80)


class RunWorkflowRequest(BaseModel):
    start_node_id: str | None = None


def serialize_workflow_node(node: WorkflowNode) -> WorkflowNodeResponse:
    return WorkflowNodeResponse(
        id=node.id,
        workflow_id=node.workflow_id,
        node_type=node.node_type,
        title=node.title,
        position_x=node.position_x,
        position_y=node.position_y,
        config_json=node.config_json,
        status=node.status,
        output_json=node.output_json,
        failure_reason=node.failure_reason,
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
        status=node_run.status,
        output_json=node_run.output_json,
        failure_reason=node_run.failure_reason,
        copy_set_id=node_run.copy_set_id,
        poster_variant_id=node_run.poster_variant_id,
        image_session_asset_id=node_run.image_session_asset_id,
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
        node_runs=[serialize_workflow_node_run(item) for item in node_runs],
    )


def serialize_product_workflow(workflow: ProductWorkflow) -> ProductWorkflowResponse:
    nodes = sorted(workflow.nodes, key=lambda item: (item.position_x, item.position_y, item.created_at))
    edges = sorted(workflow.edges, key=lambda item: item.created_at)
    return ProductWorkflowResponse(
        id=workflow.id,
        product_id=workflow.product_id,
        title=workflow.title,
        active=workflow.active,
        nodes=[serialize_workflow_node(item) for item in nodes],
        edges=[serialize_workflow_edge(item) for item in edges],
        runs=[serialize_workflow_run(item) for item in latest_workflow_runs(workflow)],
        created_at=workflow.created_at,
        updated_at=workflow.updated_at,
    )
