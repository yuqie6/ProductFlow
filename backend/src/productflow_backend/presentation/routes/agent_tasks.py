from __future__ import annotations

from fastapi import APIRouter, Depends, Query, status
from sqlalchemy.orm import Session

from productflow_backend.application.agent.control import cancel_agent_task_run
from productflow_backend.application.agent.tasks import (
    AGENT_TASK_LIST_DEFAULT_LIMIT,
    AGENT_TASK_LIST_MAX_LIMIT,
    complete_agent_task,
    create_agent_task,
    get_agent_task_or_raise,
    list_agent_tasks,
    pause_agent_task,
    rename_agent_task,
    resume_agent_task,
)
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


@router.get("", response_model=AgentTaskListResponse)
def list_agent_tasks_endpoint(
    session_id: str | None = Query(default=None, max_length=64),
    include_terminal: bool = Query(default=True),
    after: str = Query(default="", max_length=4096),
    limit: int = Query(default=AGENT_TASK_LIST_DEFAULT_LIMIT, ge=1, le=AGENT_TASK_LIST_MAX_LIMIT),
    session: Session = Depends(get_session),
) -> AgentTaskListResponse:
    page = list_agent_tasks(
        session,
        session_id=session_id,
        include_terminal=include_terminal,
        limit=limit,
        after=after,
    )
    return AgentTaskListResponse(
        items=[serialize_agent_task(item) for item in page.items],
        next_cursor=page.next_cursor,
    )


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
    return serialize_agent_task(cancel_agent_task_run(session, task_id=task_id))


@router.post("/{task_id}/pause", response_model=AgentTaskResponse)
def pause_agent_task_endpoint(
    task_id: str,
    session: Session = Depends(get_session),
) -> AgentTaskResponse:
    return serialize_agent_task(pause_agent_task(session, task_id=task_id))


@router.post("/{task_id}/complete", response_model=AgentTaskResponse)
def complete_agent_task_endpoint(
    task_id: str,
    session: Session = Depends(get_session),
) -> AgentTaskResponse:
    return serialize_agent_task(complete_agent_task(session, task_id=task_id))


@router.post("/{task_id}/resume", response_model=AgentTaskResponse)
def resume_agent_task_endpoint(
    task_id: str,
    session: Session = Depends(get_session),
) -> AgentTaskResponse:
    return serialize_agent_task(resume_agent_task(session, task_id=task_id).task)


__all__ = ["router"]
