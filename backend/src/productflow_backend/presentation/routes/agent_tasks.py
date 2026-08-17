from __future__ import annotations

from fastapi import APIRouter, Depends, Query, status
from sqlalchemy.orm import Session

from productflow_backend.application.agent_control import control_agent_turn
from productflow_backend.application.agent_conversations import get_agent_turn_or_raise
from productflow_backend.application.agent_tasks import (
    AGENT_TASK_LIST_DEFAULT_LIMIT,
    AGENT_TASK_LIST_MAX_LIMIT,
    cancel_agent_task,
    create_agent_task,
    get_agent_task_or_raise,
    list_agent_tasks,
    rename_agent_task,
)
from productflow_backend.application.agent_workflow_run_requests import (
    cancel_agent_workflow_run_request,
)
from productflow_backend.application.async_delivery import stage_async_dispatch_for_actor
from productflow_backend.domain.enums import AgentTaskStatus, AgentTurnStatus
from productflow_backend.infrastructure.agent_service import get_agent_service_client
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.agent_tasks import (
    AgentTaskListResponse,
    AgentTaskResponse,
    CreateAgentTaskRequest,
    RenameAgentTaskRequest,
    serialize_agent_task,
)

router = APIRouter(
    prefix="/api/v2/agent-tasks",
    tags=["agent-tasks"],
    dependencies=[Depends(require_admin)],
)


def enqueue_agent_turn_sync(session: Session, projection_id: str) -> None:
    stage_async_dispatch_for_actor(session, "run_agent_turn_sync", projection_id)


@router.get("", response_model=AgentTaskListResponse)
def list_agent_tasks_endpoint(
    session_id: str | None = Query(default=None, max_length=64),
    include_terminal: bool = Query(default=True),
    limit: int = Query(default=AGENT_TASK_LIST_DEFAULT_LIMIT, ge=1, le=AGENT_TASK_LIST_MAX_LIMIT),
    session: Session = Depends(get_session),
) -> AgentTaskListResponse:
    page = list_agent_tasks(
        session,
        session_id=session_id,
        include_terminal=include_terminal,
        limit=limit,
    )
    return AgentTaskListResponse(items=[serialize_agent_task(item) for item in page.items])


@router.post("", response_model=AgentTaskResponse, status_code=status.HTTP_201_CREATED)
def create_agent_task_endpoint(
    payload: CreateAgentTaskRequest,
    session: Session = Depends(get_session),
) -> AgentTaskResponse:
    return serialize_agent_task(
        create_agent_task(
            session,
            session_id=payload.session_id,
            title=payload.title,
            goal=payload.goal,
            conversation_id=payload.conversation_id,
        )
    )


@router.get("/{task_id}", response_model=AgentTaskResponse)
def get_agent_task_endpoint(
    task_id: str,
    session: Session = Depends(get_session),
) -> AgentTaskResponse:
    return serialize_agent_task(get_agent_task_or_raise(session, task_id))


@router.patch("/{task_id}", response_model=AgentTaskResponse)
def rename_agent_task_endpoint(
    task_id: str,
    payload: RenameAgentTaskRequest,
    session: Session = Depends(get_session),
) -> AgentTaskResponse:
    return serialize_agent_task(rename_agent_task(session, task_id=task_id, title=payload.title))


@router.post("/{task_id}/cancel", response_model=AgentTaskResponse)
def cancel_agent_task_endpoint(
    task_id: str,
    session: Session = Depends(get_session),
) -> AgentTaskResponse:
    task = get_agent_task_or_raise(session, task_id)
    if task.status in {
        AgentTaskStatus.SUCCEEDED,
        AgentTaskStatus.FAILED,
        AgentTaskStatus.CANCELED,
        AgentTaskStatus.UNKNOWN,
    }:
        return serialize_agent_task(task)
    projection = None
    if task.current_turn_id is not None and task.conversation_id is not None:
        projection = get_agent_turn_or_raise(
            session,
            product_id=task.product_id,
            conversation_id=task.conversation_id,
            projection_id=task.current_turn_id,
        )
    if (
        projection is not None
        and projection.workflow_run_request_id is not None
        and task.product_id is not None
        and task.conversation_id is not None
    ):
        cancel_agent_workflow_run_request(
            session,
            product_id=task.product_id,
            conversation_id=task.conversation_id,
            request_id=projection.workflow_run_request_id,
        )
        return serialize_agent_task(get_agent_task_or_raise(session, task_id))
    if (
        projection is not None
        and projection.harness_turn_id is not None
        and projection.status in {
            AgentTurnStatus.QUEUED,
            AgentTurnStatus.RUNNING,
            AgentTurnStatus.REQUIRES_INPUT,
            AgentTurnStatus.AWAITING_CONFIRMATION,
            AgentTurnStatus.CANCEL_REQUESTED,
        }
    ):
        control_agent_turn(
            session,
            product_id=task.product_id,
            conversation_id=task.conversation_id,
            projection_id=projection.id,
            command="cancel",
            gateway=get_agent_service_client(),
            enqueue_sync=enqueue_agent_turn_sync,
        )
        task = get_agent_task_or_raise(session, task_id)
    else:
        task = cancel_agent_task(session, task_id=task_id)
    return serialize_agent_task(task)


__all__ = ["router"]
