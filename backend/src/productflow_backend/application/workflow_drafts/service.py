from __future__ import annotations

import hashlib
import json
from typing import Any

from pydantic import ValidationError
from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.workflow_drafts.contracts import (
    WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS,
    DeliverySpec,
    VisualSystemDraftPayload,
    WorkflowDraftPayloadV1,
    parse_workflow_draft_payload,
    workflow_draft_payload_hash,
)
from productflow_backend.domain.enums import MediaVerificationStatus, ProductFactStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    Product,
    ProductImageAsset,
    VisualSystem,
    VisualSystemVersion,
    VisualSystemVersionReference,
    WorkflowDraft,
    WorkflowDraftLegacyArchiveSeed,
    WorkflowDraftRecipeSeed,
    WorkflowDraftRevision,
)

PRODUCT_WORKFLOW_DRAFT_RETIRED = "商品路径不再使用 WorkflowDraft"


def workflow_draft_query():
    return select(WorkflowDraft).options(
        selectinload(WorkflowDraft.revisions).selectinload(WorkflowDraftRevision.fact_set_version),
        selectinload(WorkflowDraft.revisions).selectinload(WorkflowDraftRevision.visual_system_version),
        selectinload(WorkflowDraft.current_revision),
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
    del session, product_id, payload, ready_for_confirmation, source_turn_id, source_artifact_step_id
    raise ConflictError(PRODUCT_WORKFLOW_DRAFT_RETIRED)


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
    del session, product_id, draft_id, expected_draft_version, payload
    del ready_for_confirmation, source_turn_id, source_artifact_step_id, commit
    raise ConflictError(PRODUCT_WORKFLOW_DRAFT_RETIRED)


def confirm_workflow_draft_revision(
    session: Session,
    *,
    product_id: str,
    draft_id: str,
    expected_draft_version: int,
    commit: bool = True,
) -> WorkflowDraft:
    del session, product_id, draft_id, expected_draft_version, commit
    raise ConflictError(PRODUCT_WORKFLOW_DRAFT_RETIRED)


def parse_workflow_draft_payload_or_raise(
    payload: WorkflowDraftPayloadV1 | dict[str, Any],
) -> WorkflowDraftPayloadV1:
    try:
        return parse_workflow_draft_payload(payload)
    except ValidationError as exc:
        raise BusinessValidationError("WorkflowDraft payload 不符合 schema version 1") from exc


def parse_and_verify_draft_revision(revision: WorkflowDraftRevision) -> WorkflowDraftPayloadV1:
    artifact = parse_workflow_draft_payload_or_raise(revision.payload_json)
    if workflow_draft_payload_hash(artifact) != revision.payload_hash:
        raise ConflictError("WorkflowDraft revision payload hash 不一致")
    return artifact


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
    required_delivery_spec: DeliverySpec | None = None,
) -> None:
    if artifact.missing_fact_keys:
        raise BusinessValidationError("WorkflowDraft 仍有缺失的必要商品事实")
    if any(fact.status == ProductFactStatus.CONFLICTED for fact in artifact.facts):
        raise BusinessValidationError("WorkflowDraft 仍有未解决的商品事实冲突")
    _validate_delivery_spec_snapshot(artifact, required_delivery_spec=required_delivery_spec)
    validate_workflow_draft_reference_assets(session, product_id=product_id, artifact=artifact)


def _validate_delivery_spec_snapshot(
    artifact: WorkflowDraftPayloadV1,
    *,
    required_delivery_spec: DeliverySpec | None,
) -> None:
    if required_delivery_spec is None:
        return
    for image_type_index, image_type in enumerate(artifact.image_types):
        for image_index, image in enumerate(image_type.images):
            if image.delivery_spec != required_delivery_spec:
                raise BusinessValidationError(
                    "WorkflowDraft 的每张 PlannedImage 必须显式复制 intake.delivery_spec "
                    f"(image_types[{image_type_index}].images[{image_index}].delivery_spec)"
                )


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
    "PRODUCT_WORKFLOW_DRAFT_RETIRED",
    "append_workflow_draft_revision",
    "confirm_workflow_draft_revision",
    "create_workflow_draft",
    "get_workflow_draft_or_raise",
    "parse_workflow_draft_payload_or_raise",
    "validate_workflow_draft_for_confirmation",
    "validate_workflow_draft_reference_assets",
    "workflow_draft_query",
]
