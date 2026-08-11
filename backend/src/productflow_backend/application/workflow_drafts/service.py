from __future__ import annotations

import hashlib
import json
from typing import Any

from pydantic import ValidationError
from sqlalchemy import func, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.contracts import (
    WorkflowDraftPayloadV1,
    parse_workflow_draft_payload,
    workflow_draft_payload_hash,
)
from productflow_backend.domain.enums import ProductFactStatus, WorkflowDraftStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    Product,
    ProductFactSetVersion,
    WorkflowDraft,
    WorkflowDraftRevision,
)


def workflow_draft_query():
    return select(WorkflowDraft).options(
        selectinload(WorkflowDraft.revisions).selectinload(WorkflowDraftRevision.fact_set_version),
        selectinload(WorkflowDraft.current_revision),
        selectinload(WorkflowDraft.final_workflow),
    )


def get_workflow_draft_or_raise(session: Session, *, product_id: str, draft_id: str) -> WorkflowDraft:
    draft = session.scalar(
        workflow_draft_query().where(WorkflowDraft.id == draft_id, WorkflowDraft.product_id == product_id)
    )
    if draft is None:
        _get_product_or_raise(session, product_id)
        raise NotFoundError("WorkflowDraft 不存在")
    return draft


def create_workflow_draft(
    session: Session,
    *,
    product_id: str,
    payload: WorkflowDraftPayloadV1 | dict[str, Any],
    ready_for_confirmation: bool,
    source_turn_id: str | None = None,
    source_artifact_step_id: str | None = None,
) -> WorkflowDraft:
    artifact = parse_workflow_draft_payload_or_raise(payload)
    _validate_artifact_origin(source_turn_id, source_artifact_step_id)
    try:
        _get_product_or_raise(session, product_id, for_update=True)
        draft = WorkflowDraft(
            product_id=product_id,
            status=(
                WorkflowDraftStatus.AWAITING_CONFIRMATION
                if ready_for_confirmation
                else WorkflowDraftStatus.COLLECTING
            ),
        )
        session.add(draft)
        session.flush()
        revision = WorkflowDraftRevision(
            draft_id=draft.id,
            version=1,
            schema_version=artifact.schema_version,
            payload_json=artifact.model_dump(mode="json"),
            payload_hash=workflow_draft_payload_hash(artifact),
            source_turn_id=_normalize_optional_origin(source_turn_id),
            source_artifact_step_id=_normalize_optional_origin(source_artifact_step_id),
        )
        session.add(revision)
        session.flush()
        draft.current_revision_id = revision.id
        session.commit()
    except Exception:
        session.rollback()
        raise
    session.expire_all()
    return get_workflow_draft_or_raise(session, product_id=product_id, draft_id=draft.id)


def append_workflow_draft_revision(
    session: Session,
    *,
    product_id: str,
    draft_id: str,
    expected_draft_version: int,
    payload: WorkflowDraftPayloadV1 | dict[str, Any],
    ready_for_confirmation: bool,
    source_turn_id: str | None = None,
    source_artifact_step_id: str | None = None,
) -> WorkflowDraft:
    artifact = parse_workflow_draft_payload_or_raise(payload)
    payload_json = artifact.model_dump(mode="json")
    payload_hash = workflow_draft_payload_hash(artifact)
    _validate_artifact_origin(source_turn_id, source_artifact_step_id)
    normalized_turn_id = _normalize_optional_origin(source_turn_id)
    normalized_step_id = _normalize_optional_origin(source_artifact_step_id)
    try:
        _get_product_or_raise(session, product_id, for_update=True)
        draft = _get_draft_for_update(session, product_id=product_id, draft_id=draft_id)
        existing_origin = _revision_for_artifact_origin(
            session,
            draft_id=draft.id,
            source_turn_id=normalized_turn_id,
            source_artifact_step_id=normalized_step_id,
        )
        if existing_origin is not None:
            if existing_origin.payload_hash != payload_hash:
                raise ConflictError("同一 Agent artifact 来源不能写入不同 WorkflowDraft 内容")
            session.commit()
            return get_workflow_draft_or_raise(session, product_id=product_id, draft_id=draft_id)

        current_revision = _current_revision_or_raise(draft)
        if current_revision.version != expected_draft_version:
            raise ConflictError("WorkflowDraft version 已变化，请基于最新 revision 重试")
        if draft.status in {WorkflowDraftStatus.CANCELLED, WorkflowDraftStatus.MATERIALIZING}:
            raise ConflictError("当前 WorkflowDraft 状态不允许追加 revision")

        revision = WorkflowDraftRevision(
            draft_id=draft.id,
            version=current_revision.version + 1,
            schema_version=artifact.schema_version,
            payload_json=payload_json,
            payload_hash=payload_hash,
            source_turn_id=normalized_turn_id,
            source_artifact_step_id=normalized_step_id,
        )
        session.add(revision)
        session.flush()
        draft.current_revision_id = revision.id
        draft.final_workflow_id = None
        draft.status = (
            WorkflowDraftStatus.AWAITING_CONFIRMATION
            if ready_for_confirmation
            else WorkflowDraftStatus.COLLECTING
        )
        draft.updated_at = now_utc()
        session.commit()
    except Exception:
        session.rollback()
        raise
    session.expire_all()
    return get_workflow_draft_or_raise(session, product_id=product_id, draft_id=draft_id)


def confirm_workflow_draft_revision(
    session: Session,
    *,
    product_id: str,
    draft_id: str,
    expected_draft_version: int,
) -> WorkflowDraft:
    try:
        product = _get_product_or_raise(session, product_id, for_update=True)
        draft = _get_draft_for_update(session, product_id=product_id, draft_id=draft_id)
        revision = _current_revision_or_raise(draft)
        if revision.version != expected_draft_version:
            raise ConflictError("WorkflowDraft version 已变化，请确认最新 revision")
        if revision.confirmed_at is not None:
            if revision.fact_set_version is None:
                raise ConflictError("已确认 WorkflowDraft 缺少商品事实版本")
            session.commit()
            return get_workflow_draft_or_raise(session, product_id=product_id, draft_id=draft_id)
        if draft.status != WorkflowDraftStatus.AWAITING_CONFIRMATION:
            raise ConflictError("WorkflowDraft 尚未进入待确认状态")

        artifact = parse_workflow_draft_payload_or_raise(revision.payload_json)
        if workflow_draft_payload_hash(artifact) != revision.payload_hash:
            raise ConflictError("WorkflowDraft revision payload hash 不一致")
        if artifact.missing_fact_keys:
            raise BusinessValidationError("WorkflowDraft 仍有缺失的必要商品事实")
        if any(fact.status == ProductFactStatus.CONFLICTED for fact in artifact.facts):
            raise BusinessValidationError("WorkflowDraft 仍有未解决的商品事实冲突")

        next_fact_version = (
            session.scalar(
                select(func.max(ProductFactSetVersion.version)).where(
                    ProductFactSetVersion.product_id == product_id
                )
            )
            or 0
        ) + 1
        fact_payload = {
            "schema_version": 1,
            "source_draft_revision_id": revision.id,
            "facts": [
                {
                    **fact.model_dump(mode="json"),
                    "status": ProductFactStatus.CONFIRMED.value,
                }
                for fact in artifact.facts
            ],
        }
        fact_set = ProductFactSetVersion(
            product_id=product_id,
            version=next_fact_version,
            payload_json=fact_payload,
            payload_hash=_json_hash(fact_payload),
            source_draft_revision_id=revision.id,
        )
        session.add(fact_set)
        session.flush()
        revision.confirmed_at = now_utc()
        draft.status = WorkflowDraftStatus.CONFIRMED
        draft.updated_at = now_utc()
        product.current_fact_set_version_id = fact_set.id
        session.commit()
    except Exception:
        session.rollback()
        raise
    session.expire_all()
    return get_workflow_draft_or_raise(session, product_id=product_id, draft_id=draft_id)


def parse_workflow_draft_payload_or_raise(
    payload: WorkflowDraftPayloadV1 | dict[str, Any],
) -> WorkflowDraftPayloadV1:
    try:
        return parse_workflow_draft_payload(payload)
    except ValidationError as exc:
        raise BusinessValidationError("WorkflowDraft payload 不符合 schema version 1") from exc


def _get_product_or_raise(session: Session, product_id: str, *, for_update: bool = False) -> Product:
    statement = select(Product).where(Product.id == product_id)
    if for_update:
        statement = statement.with_for_update()
    product = session.scalar(statement)
    if product is None:
        raise NotFoundError("商品不存在")
    return product


def _get_draft_for_update(session: Session, *, product_id: str, draft_id: str) -> WorkflowDraft:
    draft = session.scalar(
        select(WorkflowDraft)
        .options(
            selectinload(WorkflowDraft.current_revision).selectinload(WorkflowDraftRevision.fact_set_version),
        )
        .where(WorkflowDraft.id == draft_id, WorkflowDraft.product_id == product_id)
        .with_for_update()
    )
    if draft is None:
        raise NotFoundError("WorkflowDraft 不存在")
    return draft


def _current_revision_or_raise(draft: WorkflowDraft) -> WorkflowDraftRevision:
    if draft.current_revision is None:
        raise ConflictError("WorkflowDraft 缺少 current revision")
    return draft.current_revision


def _revision_for_artifact_origin(
    session: Session,
    *,
    draft_id: str,
    source_turn_id: str | None,
    source_artifact_step_id: str | None,
) -> WorkflowDraftRevision | None:
    if source_turn_id is None or source_artifact_step_id is None:
        return None
    return session.scalar(
        select(WorkflowDraftRevision).where(
            WorkflowDraftRevision.draft_id == draft_id,
            WorkflowDraftRevision.source_turn_id == source_turn_id,
            WorkflowDraftRevision.source_artifact_step_id == source_artifact_step_id,
        )
    )


def _validate_artifact_origin(source_turn_id: str | None, source_artifact_step_id: str | None) -> None:
    if (source_turn_id is None) != (source_artifact_step_id is None):
        raise BusinessValidationError("source_turn_id 和 source_artifact_step_id 必须同时提供")
    for value in (source_turn_id, source_artifact_step_id):
        if value is not None and not value.strip():
            raise BusinessValidationError("Agent artifact 来源 ID 不能为空")
        if value is not None and len(value.strip()) > 120:
            raise BusinessValidationError("Agent artifact 来源 ID 不能超过 120 个字符")


def _normalize_optional_origin(value: str | None) -> str | None:
    return value.strip() if value is not None else None


def _json_hash(payload: dict[str, Any]) -> str:
    canonical = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(canonical).hexdigest()


__all__ = [
    "append_workflow_draft_revision",
    "confirm_workflow_draft_revision",
    "create_workflow_draft",
    "get_workflow_draft_or_raise",
    "parse_workflow_draft_payload_or_raise",
    "workflow_draft_query",
]
