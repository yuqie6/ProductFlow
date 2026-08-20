from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from pydantic import ValidationError
from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent.conversations import (
    get_agent_conversation_or_raise,
    lock_agent_turn_or_raise,
    mark_agent_conversation_completed_for_draft,
)
from productflow_backend.application.agent.global_draft_contracts import (
    GLOBAL_AGENT_DRAFT_ARTIFACT_NAME,
    GlobalAgentDraftPayloadV1,
)
from productflow_backend.application.agent.tasks import update_agent_task_from_turn
from productflow_backend.application.agent.tools import get_agent_global_workflow_target
from productflow_backend.application.media_library.drafts import (
    validate_library_organization_draft,
)
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.service import (
    append_workflow_draft_revision,
    confirm_workflow_draft_revision,
    get_workflow_draft_or_raise,
    validate_workflow_draft_for_confirmation,
)
from productflow_backend.domain.enums import (
    AgentConversationScope,
    AgentConversationStatus,
    AgentTurnStatus,
    WorkflowDraftStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentTurnProjection,
    Product,
    WorkflowDraft,
    WorkflowDraftRevision,
)


@dataclass(frozen=True, slots=True)
class GlobalWorkflowDraftReview:
    conversation_id: str
    product_id: str
    product_name: str
    product_conversation_id: str
    workflow_draft_id: str
    draft: WorkflowDraft


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
    conversation = get_agent_conversation_or_raise(
        session,
        product_id=None,
        conversation_id=conversation_id,
    )
    artifact = parse_global_agent_draft_payload_or_raise(value)
    if artifact.draft_kind == "library_organization":
        if artifact.library_payload is None:
            raise BusinessValidationError("素材整理 Draft 缺少 library_payload")
        validate_library_organization_draft(
            session,
            conversation_id=conversation.id,
            value=artifact.library_payload.model_dump(mode="json"),
        )
        return artifact

    target = get_agent_global_workflow_target(
        session,
        product_id=artifact.product_id or "",
        workflow_draft_id=artifact.workflow_draft_id,
    )
    if target.scope_type != AgentConversationScope.PRODUCT_WORKFLOW:
        raise ConflictError("全局 WorkflowDraft 目标必须属于商品工作区")
    draft = _get_target_draft(
        session,
        product_id=artifact.product_id or "",
        workflow_draft_id=artifact.workflow_draft_id or "",
    )
    current_version = draft.current_revision.version if draft.current_revision is not None else 0
    if current_version != artifact.expected_draft_version:
        raise ConflictError("目标 WorkflowDraft version 已变化，请重新读取目标上下文")
    if artifact.workflow_payload is None:
        raise BusinessValidationError("工作流 Draft 缺少 workflow_payload")
    validate_workflow_draft_for_confirmation(
        session,
        product_id=artifact.product_id or "",
        artifact=artifact.workflow_payload,
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
    if artifact_name != GLOBAL_AGENT_DRAFT_ARTIFACT_NAME:
        raise BusinessValidationError("Agent 返回了不受支持的全局 Draft artifact")
    normalized_step_id = artifact_step_id.strip()
    if not normalized_step_id or len(normalized_step_id) > 120:
        raise BusinessValidationError("Agent artifact step ID 无效")
    conversation = get_agent_conversation_or_raise(
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

    if artifact.draft_kind == "library_organization":
        # Keep the established library Draft implementation as the owner of
        # media observations and confirmation side effects.
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

    if artifact.product_id is None or artifact.workflow_draft_id is None or artifact.workflow_payload is None:
        raise BusinessValidationError("工作流 Draft 缺少目标作用域或 payload")
    draft = _get_target_draft(
        session,
        product_id=artifact.product_id,
        workflow_draft_id=artifact.workflow_draft_id,
        for_update=True,
    )
    current_version = draft.current_revision.version if draft.current_revision is not None else 0
    if current_version != artifact.expected_draft_version:
        raise ConflictError("目标 WorkflowDraft version 已变化，请重新读取目标上下文")
    validate_workflow_draft_for_confirmation(
        session,
        product_id=artifact.product_id,
        artifact=artifact.workflow_payload,
    )
    append_workflow_draft_revision(
        session,
        product_id=artifact.product_id,
        draft_id=artifact.workflow_draft_id,
        expected_draft_version=artifact.expected_draft_version,
        payload=artifact.workflow_payload,
        ready_for_confirmation=True,
        source_turn_id=harness_turn_id,
        source_artifact_step_id=normalized_step_id,
        commit=False,
    )
    session.flush()
    session.expire(draft)
    revision = session.scalar(
        select(WorkflowDraftRevision).where(
            WorkflowDraftRevision.draft_id == artifact.workflow_draft_id,
            WorkflowDraftRevision.source_turn_id == harness_turn_id,
            WorkflowDraftRevision.source_artifact_step_id == normalized_step_id,
        )
    )
    if revision is None:
        raise ConflictError("全局 Agent artifact 未能同步为 WorkflowDraft revision")
    projection = lock_agent_turn_or_raise(
        session,
        product_id=None,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if projection.workflow_draft_revision_id not in {None, revision.id}:
        raise ConflictError("Agent turn projection 已关联其他 WorkflowDraft revision")
    if projection.library_organization_draft_revision_id is not None:
        raise ConflictError("全局 Agent Turn 不能同时绑定素材整理 Draft")
    projection.harness_turn_id = harness_turn_id
    projection.artifact_name = GLOBAL_AGENT_DRAFT_ARTIFACT_NAME
    projection.artifact_step_id = normalized_step_id
    projection.workflow_draft_revision_id = revision.id
    projection.sync_error = None
    projection.updated_at = now_utc()
    conversation.status = AgentConversationStatus.AWAITING_CONFIRMATION
    conversation.updated_at = projection.updated_at
    if commit:
        session.commit()
        session.refresh(projection)
    return projection


def get_global_workflow_draft_review(
    session: Session,
    *,
    conversation_id: str,
    revision_id: str,
) -> GlobalWorkflowDraftReview:
    projection = session.scalar(
        select(AgentTurnProjection)
        .join(AgentConversation, AgentConversation.id == AgentTurnProjection.conversation_id)
        .where(
            AgentTurnProjection.workflow_draft_revision_id == revision_id,
            AgentTurnProjection.conversation_id == conversation_id,
            AgentConversation.scope_type == AgentConversationScope.GLOBAL,
        )
    )
    if projection is None:
        get_agent_conversation_or_raise(session, product_id=None, conversation_id=conversation_id)
        raise NotFoundError("全局会话没有关联这个 WorkflowDraft revision")
    revision = session.scalar(select(WorkflowDraftRevision).where(WorkflowDraftRevision.id == revision_id))
    if revision is None:
        raise NotFoundError("WorkflowDraft revision 不存在")
    draft = get_workflow_draft_or_raise(
        session,
        product_id=revision.draft.product_id,
        draft_id=revision.draft_id,
    )
    session.expire(draft, ["current_revision_id", "current_revision"])
    if draft.current_revision_id != revision.id:
        raise ConflictError("WorkflowDraft 已产生更新版本，请重新读取后审核")
    target = get_agent_global_workflow_target(
        session,
        product_id=draft.product_id,
        workflow_draft_id=draft.id,
    )
    product = session.get(Product, draft.product_id)
    if product is None:
        raise NotFoundError("商品不存在")
    return GlobalWorkflowDraftReview(
        conversation_id=conversation_id,
        product_id=product.id,
        product_name=product.name,
        product_conversation_id=target.id,
        workflow_draft_id=draft.id,
        draft=draft,
    )


def confirm_global_workflow_draft_review(
    session: Session,
    *,
    conversation_id: str,
    revision_id: str,
    expected_draft_version: int,
) -> GlobalWorkflowDraftReview:
    review = get_global_workflow_draft_review(
        session,
        conversation_id=conversation_id,
        revision_id=revision_id,
    )
    try:
        draft = confirm_workflow_draft_revision(
            session,
            product_id=review.product_id,
            draft_id=review.workflow_draft_id,
            expected_draft_version=expected_draft_version,
            commit=False,
        )
        mark_agent_conversation_completed_for_draft(
            session,
            product_id=review.product_id,
            workflow_draft_id=draft.id,
            commit=False,
        )
        projection = session.scalar(
            select(AgentTurnProjection)
            .where(AgentTurnProjection.workflow_draft_revision_id == revision_id)
            .with_for_update()
        )
        if projection is not None:
            finished_at = now_utc()
            projection.status = AgentTurnStatus.SUCCEEDED
            projection.finished_at = finished_at
            projection.updated_at = finished_at
            update_agent_task_from_turn(
                session,
                projection=projection,
                status=AgentTurnStatus.SUCCEEDED,
                error_text=None,
                finished_at=finished_at,
            )
        global_conversation = session.scalar(
            select(AgentConversation).where(AgentConversation.id == conversation_id).with_for_update()
        )
        if global_conversation is not None:
            global_conversation.status = AgentConversationStatus.COMPLETED
            global_conversation.updated_at = now_utc()
        session.commit()
    except Exception:
        session.rollback()
        raise
    session.expire_all()
    return get_global_workflow_draft_review(
        session,
        conversation_id=conversation_id,
        revision_id=revision_id,
    )


def _get_target_draft(
    session: Session,
    *,
    product_id: str,
    workflow_draft_id: str,
    for_update: bool = False,
) -> WorkflowDraft:
    statement = (
        select(WorkflowDraft)
        .options(selectinload(WorkflowDraft.current_revision))
        .where(
            WorkflowDraft.id == workflow_draft_id,
            WorkflowDraft.product_id == product_id,
            WorkflowDraft.status != WorkflowDraftStatus.CANCELLED,
        )
    )
    if for_update:
        statement = statement.with_for_update()
    draft = session.scalar(statement)
    if draft is None:
        raise NotFoundError("目标 WorkflowDraft 不存在")
    return draft


__all__ = [
    "GLOBAL_AGENT_DRAFT_ARTIFACT_NAME",
    "GlobalWorkflowDraftReview",
    "attach_agent_global_draft_artifact",
    "confirm_global_workflow_draft_review",
    "get_global_workflow_draft_review",
    "parse_global_agent_draft_payload_or_raise",
    "validate_global_agent_draft",
]
