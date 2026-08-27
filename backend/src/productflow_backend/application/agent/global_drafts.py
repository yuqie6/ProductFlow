"""全局 Agent Draft：可审阅 artifact。当前只接受素材整理。"""

from __future__ import annotations

from typing import Any

from pydantic import ValidationError
from sqlalchemy.orm import Session

from productflow_backend.application.agent.conversations import (
    get_agent_conversation_or_raise,
)
from productflow_backend.application.agent.global_draft_contracts import (
    GLOBAL_AGENT_DRAFT_ARTIFACT_NAME,
    GlobalAgentDraftPayloadV1,
)
from productflow_backend.application.agent.turn_projection import lock_agent_turn_or_raise
from productflow_backend.application.media_library.drafts import (
    validate_library_organization_draft,
)
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import AgentTurnStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.infrastructure.db.models import AgentTurnProjection


def parse_global_agent_draft_payload_or_raise(value: dict[str, Any]) -> GlobalAgentDraftPayloadV1:
    try:
        return GlobalAgentDraftPayloadV1.model_validate(value)
    except ValidationError as exc:
        issues = []
        for error in exc.errors(include_url=False, include_context=False, include_input=False)[:8]:
            path = ".".join(str(part) for part in error["loc"]) or "$"
            issues.append(f"{path}: {error['msg']}")
        raise BusinessValidationError(f"全局 Agent Draft 无效: {'; '.join(issues)}") from exc


def validate_global_agent_draft(
    session: Session,
    *,
    conversation_id: str,
    value: dict[str, Any],
) -> GlobalAgentDraftPayloadV1:
    """校验全局 artifact。只接受素材整理。"""
    conversation = get_agent_conversation_or_raise(
        session,
        product_id=None,
        conversation_id=conversation_id,
    )
    if isinstance(value, dict) and value.get("draft_kind") not in {None, "library_organization"}:
        raise BusinessValidationError("全局 Agent 只接受素材整理 Draft")
    artifact = parse_global_agent_draft_payload_or_raise(value)
    if artifact.library_payload is None:
        raise BusinessValidationError("素材整理 Draft 缺少 library_payload")
    validate_library_organization_draft(
        session,
        conversation_id=conversation.id,
        value=artifact.library_payload.model_dump(mode="json"),
    )
    return artifact


def attach_agent_global_draft_artifact(
    session: Session,
    *,
    conversation_id: str,
    projection_id: str,
    harness_turn_id: str,
    artifact_name: str,
    artifact_step_id: str,
    artifact_value: dict[str, Any],
    commit: bool = True,
) -> AgentTurnProjection:
    """把全局 artifact 写成素材整理 Draft revision。commit=False 时由调用方持有事务。"""
    if artifact_name != GLOBAL_AGENT_DRAFT_ARTIFACT_NAME:
        raise BusinessValidationError("Agent 返回了不受支持的全局 Draft artifact")
    normalized_step_id = artifact_step_id.strip()
    if not normalized_step_id or len(normalized_step_id) > 120:
        raise BusinessValidationError("Agent artifact step ID 无效")
    get_agent_conversation_or_raise(
        session,
        product_id=None,
        conversation_id=conversation_id,
    )
    projection = lock_agent_turn_or_raise(
        session,
        product_id=None,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if projection.status != AgentTurnStatus.AWAITING_CONFIRMATION:
        raise ConflictError("Agent Turn 尚未进入待确认状态")
    if projection.harness_turn_id not in {None, harness_turn_id}:
        raise ConflictError("Agent turn projection 已绑定其他 harness Turn")
    artifact = validate_global_agent_draft(
        session,
        conversation_id=conversation_id,
        value=artifact_value,
    )

    from productflow_backend.application.agent.control import (
        attach_agent_library_organization_draft_artifact,
    )
    from productflow_backend.application.media_library.drafts import (
        LIBRARY_ORGANIZATION_DRAFT_ARTIFACT_NAME,
    )

    if artifact.library_payload is None:
        raise BusinessValidationError("素材整理 Draft 缺少 library_payload")
    projection = attach_agent_library_organization_draft_artifact(
        session,
        conversation_id=conversation_id,
        projection_id=projection_id,
        harness_turn_id=harness_turn_id,
        artifact_name=LIBRARY_ORGANIZATION_DRAFT_ARTIFACT_NAME,
        artifact_step_id=normalized_step_id,
        artifact_value=artifact.library_payload.model_dump(mode="json"),
        commit=False,
    )
    projection.artifact_name = GLOBAL_AGENT_DRAFT_ARTIFACT_NAME
    projection.updated_at = now_utc()
    if commit:
        session.commit()
        session.refresh(projection)
    return projection


__all__ = [
    "GLOBAL_AGENT_DRAFT_ARTIFACT_NAME",
    "attach_agent_global_draft_artifact",
    "parse_global_agent_draft_payload_or_raise",
    "validate_global_agent_draft",
]
