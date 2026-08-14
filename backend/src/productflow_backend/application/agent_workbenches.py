from __future__ import annotations

from dataclasses import dataclass
from typing import Literal

from sqlalchemy import exists, select
from sqlalchemy.orm import Session

from productflow_backend.application.agent_conversations import agent_conversation_query
from productflow_backend.application.workflow_drafts.materialization import (
    ActiveV2WorkflowSnapshot,
    get_active_v2_workflow_snapshot,
)
from productflow_backend.application.workflow_drafts.service import workflow_draft_query
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    Product,
    ProductWorkflow,
    WorkflowDraft,
)


@dataclass(frozen=True, slots=True)
class AgentV2WorkbenchBootstrap:
    mode: Literal["agent_v2"]
    product: Product
    conversation: AgentConversation
    workflow_draft: WorkflowDraft
    active_workflow: ActiveV2WorkflowSnapshot


@dataclass(frozen=True, slots=True)
class LegacyV1WorkbenchBootstrap:
    mode: Literal["legacy_v1"]
    product: Product
    has_existing_v1_workflow: bool


AgentWorkbenchBootstrap = AgentV2WorkbenchBootstrap | LegacyV1WorkbenchBootstrap


def get_agent_workbench_bootstrap(
    session: Session,
    *,
    product_id: str,
) -> AgentWorkbenchBootstrap:
    product = session.get(Product, product_id)
    if product is None:
        raise NotFoundError("商品不存在")

    conversation = session.scalar(
        agent_conversation_query()
        .where(AgentConversation.product_id == product_id)
        .order_by(AgentConversation.created_at.desc(), AgentConversation.id.desc())
        .limit(1)
    )
    if conversation is None:
        has_existing_v1_workflow = bool(
            session.scalar(
                select(
                    exists().where(
                        ProductWorkflow.product_id == product_id,
                        ProductWorkflow.schema_version == 1,
                    )
                )
            )
        )
        return LegacyV1WorkbenchBootstrap(
            mode="legacy_v1",
            product=product,
            has_existing_v1_workflow=has_existing_v1_workflow,
        )

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
    "LegacyV1WorkbenchBootstrap",
    "get_agent_workbench_bootstrap",
]
