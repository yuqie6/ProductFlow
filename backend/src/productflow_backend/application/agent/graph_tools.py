"""Agent graph ChangeSet 工具。mutation ledger 保证幂等。"""

from __future__ import annotations

from typing import Any

from sqlalchemy.orm import Session

from productflow_backend.application.agent.agent_context import _require_product_conversation
from productflow_backend.application.agent.conversations import get_agent_conversation_by_id_or_raise
from productflow_backend.application.agent.idempotency import normalize_idempotency_key
from productflow_backend.application.agent.tool_ledger import (
    AgentToolReconcileResult,
    _commit_tool_mutation,
    _get_conversation_for_update,
    _prepared_document,
    _reconcile_from_ledger,
    _replay_existing_mutation,
    _tool_request_hash,
)
from productflow_backend.application.product_workflow.graph_proposals import (
    apply_agent_graph_change_set,
    parse_agent_change_set,
    propose_graph_change_set,
)
from productflow_backend.domain.errors import BusinessValidationError

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
    normalized_key = normalize_idempotency_key(idempotency_key, field_name="工具 idempotency key")
    conversation = _get_conversation_for_update(session, conversation_id)
    _require_product_conversation(conversation)
    parsed = parse_agent_change_set(change_set)
    prepared_json = _prepared_document(
        conversation,
        operation=APPLY_GRAPH_TOOL_NAME,
        before={"base_graph_revision": parsed.base_graph_revision},
        target=parsed.model_dump(mode="json"),
    )
    request_hash = _tool_request_hash(tool_name=APPLY_GRAPH_TOOL_NAME, prepared_json=prepared_json)
    replay = _replay_existing_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=APPLY_GRAPH_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
    )
    if replay is not None:
        session.commit()
        return replay
    graph = apply_agent_graph_change_set(
        session,
        conversation_id=conversation_id,
        change_set=parsed,
        commit=False,
    )
    result = {
        "accepted": True,
        "applied": True,
        "graph_id": graph.id,
        "revision": graph.revision,
        "summary": parsed.summary,
    }
    return _commit_tool_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=APPLY_GRAPH_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
        prepared_json=prepared_json,
        result=result,
    )


def propose_agent_graph_change_set_tool(
    session: Session,
    *,
    conversation_id: str,
    change_set: dict[str, Any],
    idempotency_key: str,
) -> dict[str, Any]:
    """把未应用的 graph 预览写入 mutation ledger。"""
    normalized_key = normalize_idempotency_key(idempotency_key, field_name="工具 idempotency key")
    conversation = _get_conversation_for_update(session, conversation_id)
    _require_product_conversation(conversation)
    parsed = parse_agent_change_set(change_set)
    prepared_json = _prepared_document(
        conversation,
        operation=PROPOSE_GRAPH_TOOL_NAME,
        before={"base_graph_revision": parsed.base_graph_revision},
        target=parsed.model_dump(mode="json"),
    )
    request_hash = _tool_request_hash(tool_name=PROPOSE_GRAPH_TOOL_NAME, prepared_json=prepared_json)
    replay = _replay_existing_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=PROPOSE_GRAPH_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
    )
    if replay is not None:
        session.commit()
        return replay
    proposal = propose_graph_change_set(
        session,
        conversation_id=conversation_id,
        change_set=parsed,
        commit=False,
    )
    result = {
        "accepted": True,
        "applied": False,
        "pending_confirmation": True,
        "proposal_id": proposal.id,
        "graph_id": proposal.graph_id,
        "base_graph_revision": proposal.base_graph_revision,
        "summary": proposal.summary,
    }
    return _commit_tool_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=PROPOSE_GRAPH_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
        prepared_json=prepared_json,
        result=result,
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
    normalized_key = normalize_idempotency_key(idempotency_key, field_name="工具 idempotency key")
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    parsed = parse_agent_change_set(change_set)
    prepared_json = _prepared_document(
        conversation,
        operation=tool_name,
        before={"base_graph_revision": parsed.base_graph_revision},
        target=parsed.model_dump(mode="json"),
    )
    request_hash = _tool_request_hash(tool_name=tool_name, prepared_json=prepared_json)
    ledger_result = _reconcile_from_ledger(
        session,
        conversation_id=conversation.id,
        tool_name=tool_name,
        idempotency_key=normalized_key,
        request_hash=request_hash,
    )
    if ledger_result is not None:
        return ledger_result
    return AgentToolReconcileResult(state="not_applied", detail="图变更副作用尚未提交")


__all__ = [
    "APPLY_GRAPH_TOOL_NAME",
    "PROPOSE_GRAPH_TOOL_NAME",
    "AgentToolReconcileResult",
    "apply_agent_graph_change_set_tool",
    "propose_agent_graph_change_set_tool",
    "reconcile_agent_graph_change_set_tool",
]
