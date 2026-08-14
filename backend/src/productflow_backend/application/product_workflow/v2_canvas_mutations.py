from __future__ import annotations

from collections.abc import Callable, Iterable
from dataclasses import dataclass

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.materialization import v2_workflow_query
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowRunStatus
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import ProductWorkflow, WorkflowNodeRun, WorkflowRun

V2_WORKFLOW_SCHEMA_VERSION = 2
ACTIVE_V2_NODE_RUN_STATUSES = {WorkflowNodeStatus.QUEUED, WorkflowNodeStatus.RUNNING}


@dataclass(frozen=True, slots=True)
class WorkflowCanvasMutationResult:
    workflow: ProductWorkflow
    changed: bool
    dissolved_folder_ids: tuple[str, ...]


V2CanvasMutation = Callable[[ProductWorkflow], tuple[bool, Iterable[str]]]


def run_v2_canvas_mutation(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    expected_edit_version: int,
    mutate: V2CanvasMutation,
) -> WorkflowCanvasMutationResult:
    try:
        workflow = session.scalar(
            select(ProductWorkflow)
            .where(
                ProductWorkflow.id == workflow_id,
                ProductWorkflow.product_id == product_id,
            )
            .with_for_update()
        )
        if workflow is None:
            raise NotFoundError("商品工作流不存在")
        if workflow.schema_version != V2_WORKFLOW_SCHEMA_VERSION:
            raise ConflictError("画布修改只支持 schema-v2 工作流")
        if not workflow.active:
            raise ConflictError("只能修改 active schema-v2 工作流")
        if workflow.edit_version != expected_edit_version:
            raise ConflictError("工作流 edit version 已变化，请刷新后重试")

        changed, dissolved_folder_ids = mutate(workflow)
        if changed:
            workflow.edit_version += 1
            workflow.updated_at = now_utc()
        session.commit()
        session.expire_all()
        return WorkflowCanvasMutationResult(
            workflow=reload_v2_workflow(session, workflow.id),
            changed=changed,
            dissolved_folder_ids=tuple(sorted(set(dissolved_folder_ids))),
        )
    except Exception:
        session.rollback()
        raise


def reject_active_v2_node_runs(session: Session, node_ids: Iterable[str]) -> None:
    normalized_ids = sorted(set(node_ids))
    if not normalized_ids:
        return
    active_run_id = session.scalar(
        select(WorkflowNodeRun.id)
        .where(
            WorkflowNodeRun.node_id.in_(normalized_ids),
            WorkflowNodeRun.status.in_(ACTIVE_V2_NODE_RUN_STATUSES),
        )
        .order_by(WorkflowNodeRun.id)
        .with_for_update()
    )
    if active_run_id is not None:
        raise ConflictError("受影响的工作流节点正在运行")


def reject_active_v2_workflow_runs(session: Session, workflow_id: str) -> None:
    active_run_id = session.scalar(
        select(WorkflowRun.id)
        .where(
            WorkflowRun.workflow_id == workflow_id,
            WorkflowRun.status == WorkflowRunStatus.RUNNING,
        )
        .order_by(WorkflowRun.id)
        .with_for_update()
    )
    if active_run_id is not None:
        raise ConflictError("工作流正在运行，暂不能修改图结构")


def reload_v2_workflow(session: Session, workflow_id: str) -> ProductWorkflow:
    workflow = session.scalar(v2_workflow_query().where(ProductWorkflow.id == workflow_id))
    if workflow is None:
        raise NotFoundError("商品工作流不存在")
    return workflow


__all__ = [
    "ACTIVE_V2_NODE_RUN_STATUSES",
    "V2_WORKFLOW_SCHEMA_VERSION",
    "WorkflowCanvasMutationResult",
    "reject_active_v2_node_runs",
    "reject_active_v2_workflow_runs",
    "reload_v2_workflow",
    "run_v2_canvas_mutation",
]
