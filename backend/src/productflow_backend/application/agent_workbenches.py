from __future__ import annotations

from dataclasses import dataclass
from typing import Literal

from sqlalchemy.orm import Session

from productflow_backend.application.agent_conversations import agent_conversation_query
from productflow_backend.application.agent_sessions import get_agent_session_or_raise
from productflow_backend.application.agent_tasks import get_agent_task_or_raise
from productflow_backend.application.workflow_drafts.materialization import (
    ActiveV2WorkflowSnapshot,
    get_active_v2_workflow_snapshot,
)
from productflow_backend.application.workflow_drafts.service import workflow_draft_query
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    Product,
    WorkflowDraft,
)


@dataclass(frozen=True, slots=True)
class AgentV2WorkbenchBootstrap:
    mode: Literal["agent_v2"]
    product: Product
    conversation: AgentConversation
    workflow_draft: WorkflowDraft
    active_workflow: ActiveV2WorkflowSnapshot


AgentWorkbenchBootstrap = AgentV2WorkbenchBootstrap


def get_agent_workbench_bootstrap(
    session: Session,
    *,
    product_id: str,
    agent_session_id: str | None = None,
    agent_task_id: str | None = None,
) -> AgentWorkbenchBootstrap:
    product = session.get(Product, product_id)
    if product is None:
        raise NotFoundError("商品不存在")

    conversation_statement = agent_conversation_query().where(AgentConversation.product_id == product_id)
    if agent_session_id is not None:
        get_agent_session_or_raise(session, agent_session_id)
        conversation_statement = conversation_statement.where(
            AgentConversation.session_id == agent_session_id,
        )
    if agent_task_id is not None:
        task = get_agent_task_or_raise(session, agent_task_id)
        if task.product_id != product_id or task.conversation_id is None:
            raise ConflictError("当前 Agent Task 没有这个商品的工作区")
        if agent_session_id is not None and task.session_id != agent_session_id:
            raise ConflictError("Agent Task 与当前 Agent Session 不匹配")
        conversation_statement = conversation_statement.where(
            AgentConversation.id == task.conversation_id,
        )
    conversation = session.scalar(
        conversation_statement
        .order_by(AgentConversation.created_at.desc(), AgentConversation.id.desc())
        .limit(1)
    )
    if conversation is None:
        if agent_session_id is not None:
            raise ConflictError("当前 Agent Session 没有这个商品的工作区")
        raise ConflictError("商品还没有 Agent 工作区")

    draft = session.scalar(
        workflow_draft_query().where(
            WorkflowDraft.id == conversation.workflow_draft_id,
            WorkflowDraft.product_id == product_id,
        )
    )
    if draft is None:
        raise ConflictError("Agent conversation 关联的 WorkflowDraft 不可用")
    return AgentV2WorkbenchBootstrap(
        mode="agent_v2",
        product=product,
        conversation=conversation,
        workflow_draft=draft,
        active_workflow=get_active_v2_workflow_snapshot(session, product_id=product_id),
    )


__all__ = [
    "AgentV2WorkbenchBootstrap",
    "AgentWorkbenchBootstrap",
    "get_agent_workbench_bootstrap",
]
