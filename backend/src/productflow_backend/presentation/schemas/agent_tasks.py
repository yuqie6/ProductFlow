from __future__ import annotations

from datetime import datetime

from pydantic import BaseModel, Field

from productflow_backend.application.agent.tasks import (
    AGENT_TASK_GOAL_MAX_LENGTH,
    AGENT_TASK_LIST_MAX_LIMIT,
    AGENT_TASK_TITLE_MAX_LENGTH,
)
from productflow_backend.domain.enums import AgentTaskStatus
from productflow_backend.infrastructure.db.models import AgentTask
from productflow_backend.presentation.schemas.agent_conversations import StrictAgentRequest


class CreateAgentTaskRequest(StrictAgentRequest):
    session_id: str = Field(min_length=1, max_length=64)
    title: str = Field(min_length=1, max_length=AGENT_TASK_TITLE_MAX_LENGTH)
    goal: str = Field(min_length=1, max_length=AGENT_TASK_GOAL_MAX_LENGTH)
    conversation_id: str | None = Field(default=None, max_length=64)


class RenameAgentTaskRequest(StrictAgentRequest):
    title: str = Field(min_length=1, max_length=AGENT_TASK_TITLE_MAX_LENGTH)


class AgentTaskResponse(BaseModel):
    id: str
    session_id: str
    conversation_id: str | None
    product_id: str | None
    workflow_id: str | None
    workflow_draft_id: str | None
    title: str
    goal: str
    summary: str | None
    status: AgentTaskStatus
    waiting_reason: str | None
    failure_reason: str | None
    current_turn_id: str | None
    created_at: datetime
    updated_at: datetime
    started_at: datetime | None
    finished_at: datetime | None
    canceled_at: datetime | None


class AgentTaskListResponse(BaseModel):
    items: list[AgentTaskResponse]
    next_cursor: str | None = None


def serialize_agent_task(task: AgentTask) -> AgentTaskResponse:
    return AgentTaskResponse(
        id=task.id,
        session_id=task.session_id,
        conversation_id=task.conversation_id,
        product_id=task.product_id,
        workflow_id=task.workflow_id,
        workflow_draft_id=task.workflow_draft_id,
        title=task.title,
        goal=task.goal,
        summary=task.summary,
        status=task.status,
        waiting_reason=task.waiting_reason,
        failure_reason=task.failure_reason,
        current_turn_id=task.current_turn_id,
        created_at=task.created_at,
        updated_at=task.updated_at,
        started_at=task.started_at,
        finished_at=task.finished_at,
        canceled_at=task.canceled_at,
    )


__all__ = [
    "AGENT_TASK_LIST_MAX_LIMIT",
    "AgentTaskListResponse",
    "AgentTaskResponse",
    "CreateAgentTaskRequest",
    "RenameAgentTaskRequest",
    "serialize_agent_task",
]
