from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass

from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.admission import ensure_generation_capacity
from productflow_backend.application.product_workflow.run_state import mark_workflow_run_failed
from productflow_backend.application.queue_submission import enqueue_or_mark_failed
from productflow_backend.application.time import now_utc
from productflow_backend.domain.durable_generation_tasks import WORKFLOW_RUN_GENERATION_TASK_CONTRACT
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowNodeType, WorkflowRunStatus
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    ImagePromptArtifactVersion,
    WorkflowImageGenerationRecord,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)
from productflow_backend.infrastructure.queue import enqueue_workflow_node_run

V2_WORKFLOW_SCHEMA_VERSION = 2
V2_RUNNABLE_NODE_TYPES = {WorkflowNodeType.PROMPT_GENERATION, WorkflowNodeType.IMAGE_GENERATION}


@dataclass(frozen=True, slots=True)
class V2WorkflowNodeRunSubmission:
    node_run: WorkflowNodeRun
    created: bool


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

    ensure_generation_capacity(session)
    now = now_utc()
    run = WorkflowRun(workflow_id=node.workflow_id, status=WorkflowRunStatus.RUNNING)
    session.add(run)
    session.flush()
    node.status = WorkflowNodeStatus.QUEUED
    node.failure_reason = None
    node.last_run_at = now
    node.workflow.updated_at = now
    node_run = WorkflowNodeRun(
        workflow_run_id=run.id,
        node_id=node.id,
        status=WorkflowNodeStatus.QUEUED,
    )
    session.add(node_run)
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        active_node_run = _find_active_node_run(session, node_id=node.id)
        if active_node_run is None:
            raise
        return V2WorkflowNodeRunSubmission(node_run=active_node_run, created=False)

    enqueue_or_mark_failed(
        node_run.id,
        enqueue=enqueue or enqueue_workflow_node_run,
        mark_failed=lambda _node_run_id, reason: mark_workflow_run_failed(
            session,
            run_id=run.id,
            failed_node_id=node.id,
            reason=reason,
        ),
    )
    session.expire_all()
    return V2WorkflowNodeRunSubmission(
        node_run=get_v2_workflow_node_run(session, node_run_id=node_run.id),
        created=True,
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
        raise ConflictError("v2 节点运行接口拒绝读取 schema-v1 运行")
    return node_run


def _get_v2_runnable_node(session: Session, *, node_id: str) -> WorkflowNode:
    node = session.scalar(
        select(WorkflowNode)
        .options(selectinload(WorkflowNode.workflow))
        .where(WorkflowNode.id == node_id)
    )
    if node is None:
        raise NotFoundError("工作流节点不存在")
    workflow = node.workflow
    if workflow.schema_version != V2_WORKFLOW_SCHEMA_VERSION or node.schema_version != V2_WORKFLOW_SCHEMA_VERSION:
        raise ConflictError("v2 节点运行接口拒绝提交 schema-v1 节点")
    if not workflow.active:
        raise ConflictError("只能运行 active schema-v2 工作流")
    if node.node_type not in V2_RUNNABLE_NODE_TYPES:
        raise ConflictError("schema-v2 只允许运行提示词或图片生成节点")
    return node


def _find_active_node_run(session: Session, *, node_id: str) -> WorkflowNodeRun | None:
    return session.scalar(
        _v2_node_run_query()
        .join(WorkflowRun, WorkflowRun.id == WorkflowNodeRun.workflow_run_id)
        .where(
            WorkflowNodeRun.node_id == node_id,
            WorkflowNodeRun.status.in_(
                (
                    WorkflowNodeStatus.QUEUED,
                    WorkflowNodeStatus.RUNNING,
                )
            ),
            WorkflowRun.status.in_(WORKFLOW_RUN_GENERATION_TASK_CONTRACT.active_statuses),
        )
        .order_by(WorkflowNodeRun.started_at.desc(), WorkflowNodeRun.id.desc())
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
    "get_v2_workflow_node_run",
    "submit_v2_workflow_node_run",
]
