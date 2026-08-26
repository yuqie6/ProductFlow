"""Agent graph ChangeSet 工具。mutation ledger 保证幂等。"""

from __future__ import annotations

from typing import Any

from sqlalchemy.orm import Session

from productflow_backend.application.agent.agent_context import _require_product_conversation
from productflow_backend.application.agent.tool_ledger import (
    AgentToolMutationStage,
    AgentToolReconcileResult,
    apply_tool_mutation,
    reconcile_tool_mutation,
)
from productflow_backend.application.product_workflow.graph_proposals import (
    apply_agent_graph_change_set,
    parse_agent_change_set,
    propose_graph_change_set,
)
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.infrastructure.db.models import AgentConversation

APPLY_GRAPH_TOOL_NAME = "apply_graph_change_set_v1"
PROPOSE_GRAPH_TOOL_NAME = "propose_graph_change_set_v1"


def apply_agent_graph_change_set_tool(
    session: Session,
    *,
    conversation_id: str,
    change_set: dict[str, Any],
    idempotency_key: str,
) -> dict[str, Any]:
    """应用一条可逆 graph ChangeSet。已有 applied ledger 行则回放。"""
    parsed = parse_agent_change_set(change_set)

    def stage(session: Session, conversation: AgentConversation) -> AgentToolMutationStage:
        graph = apply_agent_graph_change_set(
            session,
            conversation_id=conversation.id,
            change_set=parsed,
            commit=False,
        )
        return AgentToolMutationStage(
            result={
                "accepted": True,
                "applied": True,
                "graph_id": graph.id,
                "revision": graph.revision,
                "summary": parsed.summary,
            }
        )

    return apply_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=APPLY_GRAPH_TOOL_NAME,
        operation=APPLY_GRAPH_TOOL_NAME,
        before={"base_graph_revision": parsed.base_graph_revision},
        target=parsed.model_dump(mode="json"),
        stage=stage,
        validate_conversation=_require_product_conversation,
    )


def propose_agent_graph_change_set_tool(
    session: Session,
    *,
    conversation_id: str,
    change_set: dict[str, Any],
    idempotency_key: str,
) -> dict[str, Any]:
    """把未应用的 graph 预览写入 mutation ledger。"""
    parsed = parse_agent_change_set(change_set)

    def stage(session: Session, conversation: AgentConversation) -> AgentToolMutationStage:
        proposal = propose_graph_change_set(
            session,
            conversation_id=conversation.id,
            change_set=parsed,
            commit=False,
        )
        return AgentToolMutationStage(
            result={
                "accepted": True,
                "applied": False,
                "pending_confirmation": True,
                "proposal_id": proposal.id,
                "graph_id": proposal.graph_id,
                "base_graph_revision": proposal.base_graph_revision,
                "summary": proposal.summary,
            }
        )

    return apply_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=PROPOSE_GRAPH_TOOL_NAME,
        operation=PROPOSE_GRAPH_TOOL_NAME,
        before={"base_graph_revision": parsed.base_graph_revision},
        target=parsed.model_dump(mode="json"),
        stage=stage,
        validate_conversation=_require_product_conversation,
    )


def reconcile_agent_graph_change_set_tool(
    session: Session,
    *,
    conversation_id: str,
    change_set: dict[str, Any],
    idempotency_key: str,
    tool_name: str,
) -> AgentToolReconcileResult:
    """对账 graph apply/propose，不重放任一命令。"""
    if tool_name not in {APPLY_GRAPH_TOOL_NAME, PROPOSE_GRAPH_TOOL_NAME}:
        raise BusinessValidationError("不支持的图变更对账工具")
    parsed = parse_agent_change_set(change_set)

    def fallback(_session: Session, _conversation: AgentConversation) -> AgentToolReconcileResult:
        return AgentToolReconcileResult(state="not_applied", detail="图变更副作用尚未提交")

    return reconcile_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=tool_name,
        operation=tool_name,
        before={"base_graph_revision": parsed.base_graph_revision},
        target=parsed.model_dump(mode="json"),
        fallback=fallback,
        validate_conversation=_require_product_conversation,
    )


__all__ = [
    "APPLY_GRAPH_TOOL_NAME",
    "PROPOSE_GRAPH_TOOL_NAME",
    "AgentToolReconcileResult",
    "apply_agent_graph_change_set_tool",
    "propose_agent_graph_change_set_tool",
    "reconcile_agent_graph_change_set_tool",
]
