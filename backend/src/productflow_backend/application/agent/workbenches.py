"""Agent 工作台 bootstrap：加载商品 Conversation 与 live Graph 投影。

没有 Conversation 时返回 ConflictError，HTTP 映射为 409。前端把该冲突当成
「尚无 Agent 工作区」，并继续使用独立 Graph 画布；工作台不是 Graph 路由的透传层。
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Literal

from sqlalchemy.orm import Session

from productflow_backend.application.agent.conversations import agent_conversation_query
from productflow_backend.application.agent.product_workspaces import attach_agent_workspace_to_product
from productflow_backend.application.agent.sessions import get_agent_session_or_raise
from productflow_backend.application.agent.tasks import get_agent_task_or_raise
from productflow_backend.application.product_workflow.graph_commands import get_active_workflow_graph
from productflow_backend.application.product_workflow.graph_queries import GraphProjection, project_workflow_graph
from productflow_backend.application.workflow_drafts.service import workflow_draft_query
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    Product,
    WorkflowDraft,
)


@dataclass(frozen=True, slots=True)
class AgentWorkbenchBootstrap:
    mode: Literal["agent"]
    product: Product
    conversation: AgentConversation
    workflow_draft: WorkflowDraft | None
    graph: GraphProjection | None
    latest_workflow_revision: int


def get_agent_workbench_bootstrap(
    session: Session,
    *,
    product_id: str,
    agent_session_id: str | None = None,
    agent_task_id: str | None = None,
) -> AgentWorkbenchBootstrap:
    """返回商品工作台投影。graph 存在时这就是 live schema-v3，不是第二份 Draft。"""
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

    draft = None
    if conversation.workflow_draft_id is not None:
        draft = session.scalar(
            workflow_draft_query().where(
                WorkflowDraft.id == conversation.workflow_draft_id,
                WorkflowDraft.product_id == product_id,
            )
        )
    graph_row = get_active_workflow_graph(session, product_id=product_id)
    graph = project_workflow_graph(session, graph_row) if graph_row is not None else None
    return AgentWorkbenchBootstrap(
        mode="agent",
        product=product,
        conversation=conversation,
        workflow_draft=draft,
        graph=graph,
        latest_workflow_revision=graph_row.revision if graph_row is not None else 0,
    )


def ensure_agent_workbench_bootstrap(
    session: Session,
    *,
    product_id: str,
    idempotency_key: str,
    agent_session_id: str | None = None,
    agent_task_id: str | None = None,
    force_new: bool = False,
) -> AgentWorkbenchBootstrap:
    """没有 product conversation 时幂等挂工作区；已有 Task 则只读取，不另建。"""
    if agent_task_id is not None:
        return get_agent_workbench_bootstrap(
            session,
            product_id=product_id,
            agent_session_id=agent_session_id,
            agent_task_id=agent_task_id,
        )
    conversation = session.scalar(
        agent_conversation_query()
        .where(AgentConversation.product_id == product_id)
        .order_by(AgentConversation.created_at.desc(), AgentConversation.id.desc())
        .limit(1)
    )
    if conversation is None or force_new:
        attach_agent_workspace_to_product(
            session,
            product_id=product_id,
            idempotency_key=idempotency_key,
            agent_session_id=agent_session_id,
            force_new=force_new,
        )
    return get_agent_workbench_bootstrap(
        session,
        product_id=product_id,
        agent_session_id=agent_session_id,
    )


__all__ = [
    "AgentWorkbenchBootstrap",
    "ensure_agent_workbench_bootstrap",
    "get_agent_workbench_bootstrap",
]
