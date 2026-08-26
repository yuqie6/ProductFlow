"""unknown Turn 的副作用对账。不能证明 applied/failed 时保持 unknown，禁止重放 mutation。"""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any

from pydantic import ValidationError
from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from productflow_backend.application.agent.product_workspaces import (
    reconcile_agent_product_draft_workspace_from_global_conversation,
    reconcile_agent_product_intake_from_assets,
)
from productflow_backend.application.agent.turn_projection import get_agent_turn_or_raise
from productflow_backend.application.agent.workflow_run_requests import (
    reconcile_agent_global_workflow_run_request,
    reconcile_agent_workflow_run_request,
)
from productflow_backend.application.product_intake import AgentProductSelectionV1
from productflow_backend.domain.enums import AgentCheckpointKind, AgentConversationScope, AgentTurnStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.infrastructure.db.models import (
    AgentTurnCheckpoint,
    AgentTurnEffectReconciliation,
    AgentTurnProjection,
)

MAX_RECONCILIATION_RESULT_BYTES = 64 * 1024
_SUPPORTED_EFFECT_TOOLS = {
    "request_workflow_run_v1",
    "create_product_workspace_v1",
    "finalize_product_intake_v1",
}
_RECONCILIATION_STATES = {"applied", "not_applied", "conflict", "unknown"}
_EFFECT_RESULTS = {"applied", "failed", "unknown"}


@dataclass(frozen=True, slots=True)
class AgentTurnEffectReconciliationResult:
    id: str
    projection_id: str
    tool_call_id: str
    tool_name: str
    idempotency_key: str
    effect_result: str
    reconciliation_state: str
    result_json: dict[str, Any] | None
    detail: str | None
    created_at: Any
    updated_at: Any


def reconcile_agent_turn_effect(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    tool_call_id: str,
) -> AgentTurnEffectReconciliationResult:
    """对账一条 unknown 工具副作用，不重放 mutation 命令。本函数 commit。"""
    normalized_tool_call_id = _normalize_identifier(tool_call_id, "tool_call_id", 120)
    projection = get_agent_turn_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if projection.status != AgentTurnStatus.UNKNOWN:
        raise ConflictError("只有 unknown Agent Turn 才能执行副作用对账")

    locked_projection = session.scalar(
        select(AgentTurnProjection)
        .where(AgentTurnProjection.id == projection.id)
        .with_for_update()
    )
    if locked_projection is None:
        raise ConflictError("Agent Turn 在对账前已被删除")

    existing = session.scalar(
        select(AgentTurnEffectReconciliation)
        .where(
            AgentTurnEffectReconciliation.turn_projection_id == locked_projection.id,
            AgentTurnEffectReconciliation.tool_call_id == normalized_tool_call_id,
        )
        .with_for_update()
    )
    if existing is not None and existing.effect_result != "unknown":
        return _reconciliation_result(existing)

    intent = _latest_effect_checkpoint(
        session,
        projection_id=locked_projection.id,
        tool_call_id=normalized_tool_call_id,
        kind=AgentCheckpointKind.TOOL_EFFECT_INTENT,
    )
    if intent is None:
        raise ConflictError("找不到该 tool call 的副作用 intent checkpoint")
    payload = _validated_intent_payload(intent.payload_json, normalized_tool_call_id)
    tool_name = payload["tool_name"]
    idempotency_key = _required_identifier(payload, "idempotency_key", 200)

    if existing is None:
        known_result = _latest_effect_checkpoint(
            session,
            projection_id=locked_projection.id,
            tool_call_id=normalized_tool_call_id,
            kind=AgentCheckpointKind.TOOL_EFFECT_RESULT,
        )
        if known_result is not None:
            known = _known_checkpoint_verdict(known_result.payload_json)
            if known is not None:
                return _save_reconciliation(
                    session,
                    projection=locked_projection,
                    tool_call_id=normalized_tool_call_id,
                    tool_name=tool_name,
                    idempotency_key=idempotency_key,
                    effect_result=known[0],
                    reconciliation_state=known[1],
                    result_json=None,
                    detail=None,
                )

    state, result_json, detail = _reconcile_business_effect(
        session,
        projection=locked_projection,
        product_id=product_id,
        tool_name=tool_name,
        payload=payload,
        tool_call_id=normalized_tool_call_id,
        idempotency_key=idempotency_key,
    )
    # not_applied/conflict 证明没落地；其余保持 unknown，不能猜 failed。
    effect_result = "applied" if state == "applied" else "failed" if state in {"not_applied", "conflict"} else "unknown"
    return _save_reconciliation(
        session,
        projection=locked_projection,
        tool_call_id=normalized_tool_call_id,
        tool_name=tool_name,
        idempotency_key=idempotency_key,
        effect_result=effect_result,
        reconciliation_state=state,
        result_json=result_json,
        detail=detail,
        existing=existing,
    )


def _reconcile_business_effect(
    session: Session,
    *,
    projection: AgentTurnProjection,
    product_id: str | None,
    tool_name: str,
    payload: dict[str, Any],
    tool_call_id: str,
    idempotency_key: str,
) -> tuple[str, dict[str, Any] | None, str | None]:
    conversation = projection.conversation
    if tool_name == "request_workflow_run_v1":
        expected_revision = _required_int(payload, "workflow_revision")
        workflow_id = _required_identifier(payload, "workflow_id", 64)
        task_id = _optional_identifier(payload, "task_id", 64)
        source_run_id = _optional_identifier(payload, "source_run_id", 64)
        if conversation.scope_type == AgentConversationScope.GLOBAL:
            intent_product_id = _required_identifier(payload, "product_id", 64)
            if product_id is not None:
                raise ConflictError("全局 Agent Turn 不能带 product scope")
            reconciled = reconcile_agent_global_workflow_run_request(
                session,
                conversation_id=conversation.id,
                product_id=intent_product_id,
                expected_workflow_revision=expected_revision,
                workflow_id=workflow_id,
                source_step_id=tool_call_id,
                idempotency_key=idempotency_key,
                task_id=task_id,
                source_run_id=source_run_id,
            )
        else:
            if product_id is None:
                raise ConflictError("商品 Agent Turn 缺少 product scope")
            reconciled = reconcile_agent_workflow_run_request(
                session,
                conversation_id=conversation.id,
                expected_workflow_revision=expected_revision,
                workflow_id=workflow_id,
                source_step_id=tool_call_id,
                idempotency_key=idempotency_key,
                task_id=task_id,
                source_run_id=source_run_id,
            )
        result_json = _workflow_request_summary(reconciled.request) if reconciled.request is not None else None
        return reconciled.state, result_json, reconciled.detail

    if tool_name == "create_product_workspace_v1":
        if conversation.scope_type != AgentConversationScope.GLOBAL or product_id is not None:
            raise ConflictError("商品 Agent Turn 不允许创建全局商品工作区")
        name = _required_text(payload, "product_name", 255)
        reconciled = reconcile_agent_product_draft_workspace_from_global_conversation(
            session,
            global_conversation_id=conversation.id,
            name=name,
            idempotency_key=idempotency_key,
        )
        result_json = _workspace_summary(reconciled.creation, conversation.id)
        return reconciled.state, result_json, reconciled.detail

    if tool_name == "finalize_product_intake_v1":
        if conversation.scope_type != AgentConversationScope.PRODUCT_WORKFLOW or product_id is None:
            raise ConflictError("全局 Agent Turn 不能提交商品创建输入")
        if conversation.product_id != product_id:
            raise ConflictError("商品 Agent Turn 与当前商品不匹配")
        selection_payload = payload.get("selection")
        if not isinstance(selection_payload, dict):
            raise BusinessValidationError("商品输入对账缺少 selection")
        try:
            selection = AgentProductSelectionV1.model_validate(selection_payload)
        except ValidationError as exc:
            raise BusinessValidationError("商品输入对账的图片类型选择无效") from exc
        raw_ids = payload.get("reference_asset_ids")
        if not isinstance(raw_ids, list) or not all(isinstance(item, str) for item in raw_ids):
            raise BusinessValidationError("商品输入对账缺少 reference_asset_ids")
        reconciled = reconcile_agent_product_intake_from_assets(
            session,
            conversation_id=conversation.id,
            selection=selection,
            reference_asset_ids=raw_ids,
            idempotency_key=idempotency_key,
        )
        result_json = _intake_summary(reconciled.creation)
        return reconciled.state, result_json, reconciled.detail

    raise BusinessValidationError("不支持该 Agent 副作用工具的对账")


def _save_reconciliation(
    session: Session,
    *,
    projection: AgentTurnProjection,
    tool_call_id: str,
    tool_name: str,
    idempotency_key: str,
    effect_result: str,
    reconciliation_state: str,
    result_json: dict[str, Any] | None,
    detail: str | None,
    existing: AgentTurnEffectReconciliation | None = None,
) -> AgentTurnEffectReconciliationResult:
    if effect_result not in _EFFECT_RESULTS:
        raise BusinessValidationError("Agent effect result 无效")
    if reconciliation_state not in _RECONCILIATION_STATES:
        raise BusinessValidationError("Agent reconciliation state 无效")
    _validate_result_json(result_json)
    row = existing or AgentTurnEffectReconciliation(
        turn_projection_id=projection.id,
        tool_call_id=tool_call_id,
        tool_name=tool_name,
        idempotency_key=idempotency_key,
    )
    if existing is not None and (
        existing.tool_name != tool_name or existing.idempotency_key != idempotency_key
    ):
        raise ConflictError("同一 tool call 已绑定不同的副作用身份")
    row.tool_name = tool_name
    row.idempotency_key = idempotency_key
    row.effect_result = effect_result
    row.reconciliation_state = reconciliation_state
    row.result_json = dict(result_json) if result_json is not None else None
    row.detail = detail
    session.add(row)
    try:
        session.commit()
    except IntegrityError as exc:
        session.rollback()
        replay = session.scalar(
            select(AgentTurnEffectReconciliation).where(
                AgentTurnEffectReconciliation.turn_projection_id == projection.id,
                AgentTurnEffectReconciliation.tool_call_id == tool_call_id,
            )
        )
        if replay is None:
            raise ConflictError("Agent effect reconciliation 与其他 writer 冲突") from exc
        return _reconciliation_result(replay)
    session.refresh(row)
    return _reconciliation_result(row)


def _latest_effect_checkpoint(
    session: Session,
    *,
    projection_id: str,
    tool_call_id: str,
    kind: AgentCheckpointKind,
) -> AgentTurnCheckpoint | None:
    checkpoints = list(
        session.scalars(
            select(AgentTurnCheckpoint)
            .where(
                AgentTurnCheckpoint.turn_projection_id == projection_id,
                AgentTurnCheckpoint.kind == kind,
            )
            .order_by(AgentTurnCheckpoint.sequence.desc())
        )
    )
    for checkpoint in checkpoints:
        payload = checkpoint.payload_json
        if isinstance(payload, dict) and payload.get("tool_call_id") == tool_call_id:
            return checkpoint
    return None


def _validated_intent_payload(payload: Any, tool_call_id: str) -> dict[str, Any]:
    if not isinstance(payload, dict):
        raise BusinessValidationError("副作用 intent checkpoint payload 无效")
    tool_name = payload.get("tool_name")
    if not isinstance(tool_name, str) or tool_name not in _SUPPORTED_EFFECT_TOOLS:
        raise BusinessValidationError("副作用 intent checkpoint 的工具不受支持")
    if payload.get("tool_call_id") != tool_call_id:
        raise ConflictError("副作用 intent checkpoint 与请求的 tool call 不匹配")
    _required_identifier(payload, "idempotency_key", 200)
    return payload


def _known_checkpoint_verdict(payload: Any) -> tuple[str, str] | None:
    if not isinstance(payload, dict):
        return None
    result = payload.get("result")
    if result == "applied":
        return "applied", "applied"
    if result == "failed":
        state = payload.get("reconciliation_state")
        return "failed", state if state in _RECONCILIATION_STATES else "not_applied"
    # unknown 或缺失仍要读业务对象，不能把 checkpoint 当失败。
    return None


def _workflow_request_summary(request: Any) -> dict[str, Any]:
    return {
        "kind": "workflow_run_request",
        "id": request.id,
        "conversation_id": request.conversation_id,
        "product_id": request.product_id,
        "workflow_id": request.workflow_id,
        "status": request.status.value,
        "workflow_run_id": request.workflow_run_id,
    }


def _workspace_summary(creation: Any, global_conversation_id: str) -> dict[str, Any] | None:
    if creation is None:
        return None
    return {
        "kind": "product_workspace",
        "created": creation.created,
        "session_id": creation.conversation.session_id,
        "global_conversation_id": global_conversation_id,
        "product_conversation_id": creation.conversation.id,
        "product_id": creation.product.id,
        "product_name": creation.product.name,
        "workflow_draft_id": creation.conversation.workflow_draft_id,
        "task_id": None,
        "intake_finalized": creation.intake_finalized,
    }


def _intake_summary(creation: Any) -> dict[str, Any] | None:
    if creation is None:
        return None
    return {
        "kind": "product_intake",
        "product_id": creation.product.id,
        "workflow_draft_id": creation.conversation.workflow_draft_id,
        "intake_finalized": creation.intake_finalized,
    }


def _reconciliation_result(row: AgentTurnEffectReconciliation) -> AgentTurnEffectReconciliationResult:
    return AgentTurnEffectReconciliationResult(
        id=row.id,
        projection_id=row.turn_projection_id,
        tool_call_id=row.tool_call_id,
        tool_name=row.tool_name,
        idempotency_key=row.idempotency_key,
        effect_result=row.effect_result,
        reconciliation_state=row.reconciliation_state,
        result_json=dict(row.result_json) if row.result_json is not None else None,
        detail=row.detail,
        created_at=row.created_at,
        updated_at=row.updated_at,
    )


def _validate_result_json(result_json: dict[str, Any] | None) -> None:
    if result_json is None:
        return
    try:
        encoded = json.dumps(
            result_json,
            ensure_ascii=False,
            sort_keys=True,
            separators=(",", ":"),
            allow_nan=False,
        ).encode()
    except (TypeError, ValueError) as exc:
        raise BusinessValidationError("Agent reconciliation result 不是有效 JSON") from exc
    if len(encoded) > MAX_RECONCILIATION_RESULT_BYTES:
        raise BusinessValidationError("Agent reconciliation result 超过大小限制")


def _required_identifier(payload: dict[str, Any], key: str, maximum: int) -> str:
    value = payload.get(key)
    if not isinstance(value, str) or not value.strip() or len(value.strip()) > maximum:
        raise BusinessValidationError(f"副作用 intent 缺少有效的 {key}")
    return value.strip()


def _optional_identifier(payload: dict[str, Any], key: str, maximum: int) -> str | None:
    value = payload.get(key)
    if value is None:
        return None
    return _required_identifier(payload, key, maximum)


def _required_text(payload: dict[str, Any], key: str, maximum: int) -> str:
    return _required_identifier(payload, key, maximum)


def _required_int(payload: dict[str, Any], key: str) -> int:
    value = payload.get(key)
    if isinstance(value, bool) or not isinstance(value, int) or value < 1:
        raise BusinessValidationError(f"副作用 intent 缺少有效的 {key}")
    return value


def _normalize_identifier(value: str, name: str, maximum: int) -> str:
    if not isinstance(value, str) or not value.strip() or len(value.strip()) > maximum:
        raise BusinessValidationError(f"{name} 无效")
    return value.strip()
