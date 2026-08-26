"""商品 WorkflowDraft 确认：revision 与 Agent conversation 在同一事务内确认。"""

from __future__ import annotations

from sqlalchemy.orm import Session

from productflow_backend.application.agent.conversations import mark_agent_conversation_completed_for_draft
from productflow_backend.application.workflow_drafts.service import (
    confirm_workflow_draft_revision,
    get_workflow_draft_or_raise,
)
from productflow_backend.infrastructure.db.models import WorkflowDraft


def confirm_product_workflow_draft(
    session: Session,
    *,
    product_id: str,
    draft_id: str,
    expected_draft_version: int,
) -> WorkflowDraft:
    """在一次应用事务内确认商品草稿及其 Agent 会话。"""
    try:
        confirm_workflow_draft_revision(
            session,
            product_id=product_id,
            draft_id=draft_id,
            expected_draft_version=expected_draft_version,
            commit=False,
        )
        mark_agent_conversation_completed_for_draft(
            session,
            product_id=product_id,
            workflow_draft_id=draft_id,
            commit=False,
        )
        session.commit()
    except Exception:
        session.rollback()
        raise
    session.expire_all()
    return get_workflow_draft_or_raise(session, product_id=product_id, draft_id=draft_id)


__all__ = ["confirm_product_workflow_draft"]
