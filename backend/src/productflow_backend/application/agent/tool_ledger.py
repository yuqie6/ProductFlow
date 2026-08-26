"""Durable Agent tool mutation ledger and request-hash helpers."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from productflow_backend.application.agent.idempotency import (
    canonical_json_request_hash,
    normalize_idempotency_key,
)
from productflow_backend.domain.enums import AgentToolMutationStatus
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import AgentConversation, AgentToolMutation


@dataclass(frozen=True, slots=True)
class AgentToolReconcileResult:
    state: str
    result: dict[str, Any] | None = None
    detail: str | None = None


AgentAssetRenameReconcileResult = AgentToolReconcileResult


def _prepared_document(
    conversation: AgentConversation,
    *,
    operation: str,
    before: dict[str, Any],
    target: dict[str, Any],
) -> dict[str, Any]:
    return {
        "schema_version": 1,
        "operation": operation,
        "scope": {
            "conversation_id": conversation.id,
            "product_id": conversation.product_id,
        },
        "before": before,
        "target": target,
    }


def _tool_request_hash(*, tool_name: str, prepared_json: dict[str, Any]) -> str:
    return canonical_json_request_hash({"tool_name": tool_name, "prepared": prepared_json})


def _get_conversation_for_update(session: Session, conversation_id: str) -> AgentConversation:
    conversation = session.scalar(
        select(AgentConversation).where(AgentConversation.id == conversation_id).with_for_update()
    )
    if conversation is None:
        raise NotFoundError("Agent conversation 不存在")
    return conversation


def _get_tool_mutation(
    session: Session,
    *,
    conversation_id: str,
    tool_name: str,
    idempotency_key: str,
) -> AgentToolMutation | None:
    return session.scalar(
        select(AgentToolMutation).where(
            AgentToolMutation.conversation_id == conversation_id,
            AgentToolMutation.tool_name == tool_name,
            AgentToolMutation.idempotency_key == idempotency_key,
        )
    )


def _replay_existing_mutation(
    session: Session,
    *,
    conversation_id: str,
    tool_name: str,
    idempotency_key: str,
    request_hash: str,
) -> dict[str, Any] | None:
    """Replay an applied ledger row or reject an idempotency mismatch."""
    mutation = _get_tool_mutation(
        session,
        conversation_id=conversation_id,
        tool_name=tool_name,
        idempotency_key=idempotency_key,
    )
    if mutation is None:
        return None
    if mutation.request_hash != request_hash:
        raise ConflictError("同一工具 idempotency key 不能提交不同请求")
    if mutation.status != AgentToolMutationStatus.APPLIED or mutation.result_json is None:
        raise ConflictError("工具副作用账本未处于 applied 状态")
    return dict(mutation.result_json)


def _reconcile_from_ledger(
    session: Session,
    *,
    conversation_id: str,
    tool_name: str,
    idempotency_key: str,
    request_hash: str,
) -> AgentToolReconcileResult | None:
    """Keep UNKNOWN or incomplete ledger rows unknown during reconciliation."""
    mutation = _get_tool_mutation(
        session,
        conversation_id=conversation_id,
        tool_name=tool_name,
        idempotency_key=idempotency_key,
    )
    if mutation is None:
        return None
    if mutation.request_hash != request_hash:
        return AgentToolReconcileResult(state="conflict", detail="工具幂等键已绑定其他请求")
    if mutation.status == AgentToolMutationStatus.APPLIED and mutation.result_json is not None:
        return AgentToolReconcileResult(
            state="applied",
            result=dict(mutation.result_json),
            detail="工具副作用已提交",
        )
    if mutation.status == AgentToolMutationStatus.UNKNOWN:
        return AgentToolReconcileResult(state="unknown", detail="副作用结果仍不明确")
    return AgentToolReconcileResult(state="unknown", detail="副作用账本尚未形成终态")


def _commit_tool_mutation(
    session: Session,
    *,
    conversation_id: str,
    tool_name: str,
    idempotency_key: str,
    request_hash: str,
    prepared_json: dict[str, Any],
    result: dict[str, Any],
    asset_id: str | None = None,
    expected_display_name: str | None = None,
    target_display_name: str | None = None,
) -> dict[str, Any]:
    """Write an applied mutation and commit, replaying a concurrent winner."""
    normalized_key = normalize_idempotency_key(idempotency_key)
    session.add(
        AgentToolMutation(
            conversation_id=conversation_id,
            tool_name=tool_name,
            idempotency_key=normalized_key,
            request_hash=request_hash,
            asset_id=asset_id,
            expected_display_name=expected_display_name,
            target_display_name=target_display_name,
            prepared_json=prepared_json,
            status=AgentToolMutationStatus.APPLIED,
            result_json=result,
        )
    )
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        existing = _get_tool_mutation(
            session,
            conversation_id=conversation_id,
            tool_name=tool_name,
            idempotency_key=normalized_key,
        )
        if (
            existing is not None
            and existing.request_hash == request_hash
            and existing.status == AgentToolMutationStatus.APPLIED
            and existing.result_json is not None
        ):
            return dict(existing.result_json)
        raise ConflictError("工具请求与现有对象或副作用账本冲突") from None
    return result


__all__ = [
    "AgentAssetRenameReconcileResult",
    "AgentToolReconcileResult",
    "_commit_tool_mutation",
    "_get_conversation_for_update",
    "_prepared_document",
    "_reconcile_from_ledger",
    "_replay_existing_mutation",
    "_tool_request_hash",
]
