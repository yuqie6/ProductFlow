from __future__ import annotations

import hashlib
import json
from typing import Any

from pydantic import ValidationError
from sqlalchemy import func, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent_product_intake import parse_workflow_intake
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.contracts import (
    WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS,
    VisualSystemDraftPayload,
    WorkflowDraftPayloadV1,
    parse_workflow_draft_payload,
    workflow_draft_payload_hash,
)
from productflow_backend.domain.enums import MediaVerificationStatus, ProductFactStatus, WorkflowDraftStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    Product,
    ProductFactSetVersion,
    ProductImageAsset,
    VisualSystem,
    VisualSystemVersion,
    VisualSystemVersionReference,
    WorkflowDraft,
    WorkflowDraftLegacyArchiveSeed,
    WorkflowDraftRecipeSeed,
    WorkflowDraftRevision,
)


def workflow_draft_query():
    return select(WorkflowDraft).options(
        selectinload(WorkflowDraft.revisions).selectinload(WorkflowDraftRevision.fact_set_version),
        selectinload(WorkflowDraft.revisions).selectinload(WorkflowDraftRevision.visual_system_version),
        selectinload(WorkflowDraft.current_revision),
        selectinload(WorkflowDraft.final_workflow),
        selectinload(WorkflowDraft.recipe_seed).selectinload(WorkflowDraftRecipeSeed.recipe_version),
        selectinload(WorkflowDraft.legacy_archive_seed).selectinload(WorkflowDraftLegacyArchiveSeed.workflow_archive),
        selectinload(WorkflowDraft.legacy_archive_seed).selectinload(
            WorkflowDraftLegacyArchiveSeed.canvas_agent_archive
        ),
        selectinload(WorkflowDraft.legacy_archive_seed).selectinload(
            WorkflowDraftLegacyArchiveSeed.user_template_archive
        ),
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
    commit: bool = True,
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
            if commit:
                session.commit()
            return get_workflow_draft_or_raise(session, product_id=product_id, draft_id=draft_id)

        current_revision = draft.current_revision
        if current_revision is None:
            if expected_draft_version != 0:
                raise ConflictError("WorkflowDraft version 已变化，请基于最新 revision 重试")
            intake = parse_workflow_intake(
                schema_version=draft.intake_schema_version,
                payload=draft.intake_json,
            )
            if draft.status != WorkflowDraftStatus.COLLECTING or (
                draft.recipe_seed is None and draft.legacy_archive_seed is None and intake is None
            ):
                raise ConflictError(
                    "只有 collecting recipe seed、legacy archive seed 或 intake Draft "
                    "可以从 version 0 追加首次 revision"
                )
            next_version = 1
        else:
            if current_revision.version != expected_draft_version:
                raise ConflictError("WorkflowDraft version 已变化，请基于最新 revision 重试")
            next_version = current_revision.version + 1
        if draft.status in {WorkflowDraftStatus.CANCELLED, WorkflowDraftStatus.MATERIALIZING}:
            raise ConflictError("当前 WorkflowDraft 状态不允许追加 revision")

        revision = WorkflowDraftRevision(
            draft_id=draft.id,
            version=next_version,
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
        if commit:
            session.commit()
    except Exception:
        if commit:
            session.rollback()
        raise
    if commit:
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
            if revision.visual_system_version is None:
                raise ConflictError("已确认 WorkflowDraft 缺少视觉体系版本")
            session.commit()
            return get_workflow_draft_or_raise(session, product_id=product_id, draft_id=draft_id)
        if draft.status != WorkflowDraftStatus.AWAITING_CONFIRMATION:
            raise ConflictError("WorkflowDraft 尚未进入待确认状态")

        artifact = parse_workflow_draft_payload_or_raise(revision.payload_json)
        if workflow_draft_payload_hash(artifact) != revision.payload_hash:
            raise ConflictError("WorkflowDraft revision payload hash 不一致")
        validate_workflow_draft_for_confirmation(session, product_id=product_id, artifact=artifact)
        visual_system_version = _resolve_visual_system_version(
            session,
            revision=revision,
            artifact=artifact,
        )

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
        revision.visual_system_version_id = visual_system_version.id
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


def validate_workflow_draft_reference_assets(
    session: Session,
    *,
    product_id: str,
    artifact: WorkflowDraftPayloadV1,
) -> None:
    asset_ids = artifact.referenced_asset_ids()
    assets = list(
        session.scalars(
            select(ProductImageAsset)
            .options(selectinload(ProductImageAsset.media_object))
            .where(ProductImageAsset.id.in_(asset_ids))
        )
    )
    assets_by_id = {asset.id: asset for asset in assets}
    if set(assets_by_id) != asset_ids:
        raise BusinessValidationError("WorkflowDraft 引用了不存在的商品图片资产")
    if any(asset.product_id != product_id for asset in assets):
        raise BusinessValidationError("WorkflowDraft 引用了其他商品的图片资产")
    if any(asset.media_object.verification_status != MediaVerificationStatus.VERIFIED for asset in assets):
        raise BusinessValidationError("WorkflowDraft 引用了未通过核验的图片资产")


def validate_workflow_draft_for_confirmation(
    session: Session,
    *,
    product_id: str,
    artifact: WorkflowDraftPayloadV1,
) -> None:
    if artifact.missing_fact_keys:
        raise BusinessValidationError("WorkflowDraft 仍有缺失的必要商品事实")
    if any(fact.status == ProductFactStatus.CONFLICTED for fact in artifact.facts):
        raise BusinessValidationError("WorkflowDraft 仍有未解决的商品事实冲突")
    validate_workflow_draft_reference_assets(session, product_id=product_id, artifact=artifact)


def _resolve_visual_system_version(
    session: Session,
    *,
    revision: WorkflowDraftRevision,
    artifact: WorkflowDraftPayloadV1,
) -> VisualSystemVersion:
    plan = artifact.visual_system
    if plan.mode == "draft":
        payload = plan.payload
        assert payload is not None
        payload_json = payload.model_dump(mode="json")
        payload_hash = _json_hash(payload_json)
        version = session.scalar(
            select(VisualSystemVersion).where(VisualSystemVersion.source_draft_revision_id == revision.id)
        )
        if version is None:
            visual_system = VisualSystem(name=payload.name)
            session.add(visual_system)
            session.flush()
            version = VisualSystemVersion(
                visual_system_id=visual_system.id,
                version=1,
                schema_version=1,
                payload_json=payload_json,
                payload_hash=payload_hash,
                source_markdown=plan.source_markdown,
                source_draft_revision_id=revision.id,
            )
            session.add(version)
            session.flush()
            for position, reference in enumerate(payload.reference_assets):
                session.add(
                    VisualSystemVersionReference(
                        visual_system_version_id=version.id,
                        asset_id=reference.asset_id,
                        role=reference.role,
                        label=reference.label,
                        position=position,
                    )
                )
        elif version.payload_hash != payload_hash:
            raise ConflictError("WorkflowDraft revision 已绑定不同的视觉体系内容")
    else:
        assert plan.version_id is not None
        version = session.get(VisualSystemVersion, plan.version_id)
        if version is None:
            raise BusinessValidationError("WorkflowDraft 引用的视觉体系版本不存在")

    visual_payload = _parse_and_verify_visual_system_version(version)
    _validate_visual_usage(
        artifact=artifact,
        visual_payload=visual_payload,
        visual_reference_asset_ids={reference.asset_id for reference in version.references},
    )
    return version


def _parse_and_verify_visual_system_version(version: VisualSystemVersion) -> VisualSystemDraftPayload:
    try:
        payload = VisualSystemDraftPayload.model_validate(version.payload_json)
    except ValidationError as exc:
        raise ConflictError("视觉体系版本 payload 不符合 schema version 1") from exc
    if _json_hash(payload.model_dump(mode="json")) != version.payload_hash:
        raise ConflictError("视觉体系版本 payload hash 不一致")
    return payload


def _validate_visual_usage(
    *,
    artifact: WorkflowDraftPayloadV1,
    visual_payload: VisualSystemDraftPayload,
    visual_reference_asset_ids: set[str],
) -> None:
    variant_keys = {variant.key for variant in visual_payload.variants}
    if any(
        prompt.payload.visual_variant_key is not None
        and prompt.payload.visual_variant_key not in variant_keys
        for prompt in artifact.prompt_plans
    ):
        raise BusinessValidationError("提示词引用了视觉体系版本中不存在的视觉变体")
    locked_fields = set(visual_payload.locked_fields)
    if any(
        override.field not in locked_fields
        for exception in artifact.visual_exceptions
        for override in exception.overrides
    ):
        raise BusinessValidationError("视觉例外只能覆盖 VisualSystem locked_fields")
    referenced_asset_ids = artifact.referenced_asset_ids() | visual_reference_asset_ids
    if len(referenced_asset_ids) > WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS:
        raise BusinessValidationError(
            f"WorkflowDraft 与视觉体系引用的不同图片资产不能超过 {WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS} 张"
        )


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
            selectinload(WorkflowDraft.current_revision).selectinload(
                WorkflowDraftRevision.visual_system_version
            ),
            selectinload(WorkflowDraft.recipe_seed),
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
    "validate_workflow_draft_for_confirmation",
    "validate_workflow_draft_reference_assets",
    "workflow_draft_query",
]
