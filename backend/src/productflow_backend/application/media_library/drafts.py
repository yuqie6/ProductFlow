from __future__ import annotations

import json
from hashlib import sha256
from typing import Any

from pydantic import ValidationError
from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent.conversations import (
    GLOBAL_SCOPE,
    get_agent_conversation_or_raise,
)
from productflow_backend.application.media_library.draft_contracts import (
    LibraryArchiveOperationV1,
    LibraryAssetBeforeV1,
    LibraryLinkWorkflowOperationV1,
    LibraryMoveOperationV1,
    LibraryOrganizationDraftPayloadV1,
    LibraryRenameOperationV1,
    LibraryRestoreOperationV1,
    LibrarySetTagsOperationV1,
    library_organization_draft_payload_hash,
    parse_library_organization_draft_payload,
)
from productflow_backend.application.media_library.service import (
    validate_media_library_asset_for_use,
    validate_media_library_asset_integrity,
)
from productflow_backend.application.media_library.workflow import sync_workflow_media_library_assets
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import (
    AgentConversationStatus,
    AgentTaskStatus,
    LibraryOrganizationDraftStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentTask,
    AgentTurnProjection,
    LibraryOrganizationDraft,
    LibraryOrganizationDraftRevision,
    MediaLibraryAsset,
    MediaLibraryAssetTag,
    MediaLibraryFolder,
    MediaLibraryTag,
    WorkflowGraph,
    WorkflowMediaLibraryAsset,
)

LIBRARY_ORGANIZATION_DRAFT_ARTIFACT_NAME = "propose_library_organization_draft"
MAX_CONFIRMATION_IDEMPOTENCY_KEY_BYTES = 200


def library_organization_draft_query():
    return select(LibraryOrganizationDraft).options(
        selectinload(LibraryOrganizationDraft.conversation),
        selectinload(LibraryOrganizationDraft.revisions),
        selectinload(LibraryOrganizationDraft.current_revision),
        selectinload(LibraryOrganizationDraft.confirmed_revision),
    )


def get_library_organization_draft_or_raise(
    session: Session,
    *,
    conversation_id: str,
) -> LibraryOrganizationDraft:
    _get_global_conversation(session, conversation_id)
    draft = session.scalar(
        library_organization_draft_query().where(
            LibraryOrganizationDraft.conversation_id == conversation_id,
        )
    )
    if draft is None:
        raise NotFoundError("全局素材整理 Draft 不存在")
    return draft


def validate_library_organization_draft(
    session: Session,
    *,
    conversation_id: str,
    value: dict[str, Any],
) -> LibraryOrganizationDraftPayloadV1:
    _get_global_conversation(session, conversation_id)
    artifact = parse_library_organization_draft_payload_or_raise(value)
    _observe_operations(session, artifact)
    return artifact


def append_library_organization_draft_revision(
    session: Session,
    *,
    conversation_id: str,
    expected_draft_version: int,
    payload: LibraryOrganizationDraftPayloadV1 | dict[str, Any],
    source_turn_id: str,
    source_artifact_step_id: str,
    commit: bool = True,
) -> LibraryOrganizationDraft:
    conversation = _get_global_conversation(session, conversation_id)
    artifact = parse_library_organization_draft_payload_or_raise(payload)
    source_turn_id, source_artifact_step_id = _normalize_artifact_origin(
        source_turn_id,
        source_artifact_step_id,
    )
    payload_json = artifact.model_dump(mode="json")
    payload_hash = library_organization_draft_payload_hash(artifact)
    try:
        draft = _get_or_create_draft_for_update(session, conversation)
        existing = session.scalar(
            select(LibraryOrganizationDraftRevision).where(
                LibraryOrganizationDraftRevision.draft_id == draft.id,
                LibraryOrganizationDraftRevision.source_turn_id == source_turn_id,
                LibraryOrganizationDraftRevision.source_artifact_step_id == source_artifact_step_id,
            )
        )
        if existing is not None:
            if existing.payload_hash != payload_hash:
                raise ConflictError("同一 Agent artifact 来源不能写入不同素材整理 Draft")
            if commit:
                session.commit()
            return get_library_organization_draft_or_raise(session, conversation_id=conversation_id)

        current_revision = draft.current_revision
        current_version = current_revision.version if current_revision is not None else 0
        if current_version != expected_draft_version:
            raise ConflictError("素材整理 Draft version 已变化，请基于最新 revision 重试")
        if draft.status == LibraryOrganizationDraftStatus.CANCELLED:
            raise ConflictError("当前素材整理 Draft 已取消")

        _observe_operations(session, artifact)
        revision = LibraryOrganizationDraftRevision(
            draft_id=draft.id,
            version=current_version + 1,
            schema_version=artifact.schema_version,
            payload_json=payload_json,
            payload_hash=payload_hash,
            source_turn_id=source_turn_id,
            source_artifact_step_id=source_artifact_step_id,
        )
        session.add(revision)
        session.flush()
        draft.current_revision_id = revision.id
        draft.status = LibraryOrganizationDraftStatus.AWAITING_CONFIRMATION
        draft.confirmed_revision_id = None
        draft.confirmation_idempotency_key = None
        draft.confirmation_request_hash = None
        draft.confirmation_result_json = None
        draft.confirmed_at = None
        draft.updated_at = now_utc()
        conversation.status = AgentConversationStatus.AWAITING_CONFIRMATION
        conversation.updated_at = now_utc()
        if commit:
            session.commit()
        if commit:
            session.expire_all()
        return get_library_organization_draft_or_raise(session, conversation_id=conversation_id)
    except Exception:
        if commit:
            session.rollback()
        raise


def confirm_library_organization_draft_revision(
    session: Session,
    *,
    draft_id: str,
    expected_draft_version: int,
    idempotency_key: str,
) -> LibraryOrganizationDraft:
    normalized_key = _normalize_confirmation_idempotency_key(idempotency_key)
    request_hash = _confirmation_request_hash(draft_id, expected_draft_version)
    try:
        draft = _get_draft_for_update(session, draft_id)
        revision = draft.current_revision
        if revision is None:
            raise ConflictError("素材整理 Draft 缺少 current revision")
        if draft.status == LibraryOrganizationDraftStatus.CONFIRMED:
            if (
                draft.confirmed_revision_id != revision.id
                or draft.confirmation_idempotency_key != normalized_key
                or draft.confirmation_request_hash != request_hash
            ):
                raise ConflictError("素材整理 Draft 已使用其他确认请求完成")
            session.commit()
            return get_library_organization_draft_or_raise(
                session,
                conversation_id=draft.conversation_id,
            )
        if draft.status != LibraryOrganizationDraftStatus.AWAITING_CONFIRMATION:
            raise ConflictError("当前素材整理 Draft 不在待确认状态")
        if revision.version != expected_draft_version:
            raise ConflictError("素材整理 Draft version 已变化，请确认最新 revision")
        artifact = parse_library_organization_draft_payload_or_raise(revision.payload_json)
        if library_organization_draft_payload_hash(artifact) != revision.payload_hash:
            raise ConflictError("素材整理 Draft revision payload hash 不一致")

        assets, folders, workflows = _observe_operations(session, artifact)
        tags = _load_or_create_tags(session, artifact)
        result = _apply_operations(
            session=session,
            artifact=artifact,
            assets=assets,
            folders=folders,
            workflows=workflows,
            tags=tags,
            draft_id=draft.id,
            revision_version=revision.version,
        )
        confirmed_at = now_utc()
        revision.confirmed_at = confirmed_at
        draft.status = LibraryOrganizationDraftStatus.CONFIRMED
        draft.confirmed_revision_id = revision.id
        draft.confirmation_idempotency_key = normalized_key
        draft.confirmation_request_hash = request_hash
        draft.confirmation_result_json = result
        draft.confirmed_at = confirmed_at
        draft.updated_at = confirmed_at
        conversation = draft.conversation
        conversation.status = AgentConversationStatus.COMPLETED
        conversation.updated_at = confirmed_at
        projection = session.scalar(
            select(AgentTurnProjection).where(
                AgentTurnProjection.library_organization_draft_revision_id == revision.id,
            )
        )
        if projection is not None and projection.task_id is not None:
            task = session.get(AgentTask, projection.task_id, with_for_update=True)
            if task is None:
                raise ConflictError("素材整理 Draft 绑定的 Agent Task 不存在")
            if task.status == AgentTaskStatus.AWAITING_CONFIRMATION:
                task.status = AgentTaskStatus.SUCCEEDED
                task.waiting_reason = None
                task.failure_reason = None
                task.finished_at = confirmed_at
                task.updated_at = confirmed_at
        session.commit()
    except Exception:
        session.rollback()
        raise
    session.expire_all()
    return get_library_organization_draft_or_raise(
        session,
        conversation_id=draft.conversation_id,
    )


def parse_library_organization_draft_payload_or_raise(
    payload: LibraryOrganizationDraftPayloadV1 | dict[str, Any],
) -> LibraryOrganizationDraftPayloadV1:
    try:
        return (
            payload
            if isinstance(payload, LibraryOrganizationDraftPayloadV1)
            else parse_library_organization_draft_payload(payload)
        )
    except ValidationError as exc:
        raise BusinessValidationError("素材整理 Draft payload 不符合 schema version 1") from exc


def _get_global_conversation(session: Session, conversation_id: str) -> AgentConversation:
    conversation = get_agent_conversation_or_raise(
        session,
        product_id=None,
        conversation_id=conversation_id,
    )
    if conversation.scope_type != GLOBAL_SCOPE:
        raise ConflictError("素材整理 Draft 只能绑定全局 Agent conversation")
    return conversation


def _get_or_create_draft_for_update(
    session: Session,
    conversation: AgentConversation,
) -> LibraryOrganizationDraft:
    draft = _get_draft_for_update(session, conversation_id=conversation.id, allow_missing=True)
    if draft is not None:
        return draft
    draft = LibraryOrganizationDraft(conversation_id=conversation.id)
    session.add(draft)
    session.flush()
    return _get_draft_for_update(session, draft_id=draft.id)


def _get_draft_for_update(
    session: Session,
    draft_id: str | None = None,
    *,
    conversation_id: str | None = None,
    allow_missing: bool = False,
) -> LibraryOrganizationDraft | None:
    statement = library_organization_draft_query()
    if draft_id is not None:
        statement = statement.where(LibraryOrganizationDraft.id == draft_id)
    elif conversation_id is not None:
        statement = statement.where(LibraryOrganizationDraft.conversation_id == conversation_id)
    else:
        raise ValueError("draft_id or conversation_id is required")
    draft = session.scalar(statement.with_for_update())
    if draft is None and not allow_missing:
        raise NotFoundError("素材整理 Draft 不存在")
    return draft


def _normalize_artifact_origin(source_turn_id: str, source_artifact_step_id: str) -> tuple[str, str]:
    normalized_turn_id = source_turn_id.strip()
    normalized_step_id = source_artifact_step_id.strip()
    if not normalized_turn_id or not normalized_step_id:
        raise BusinessValidationError("素材整理 Draft 必须绑定 Agent Turn 和 artifact step")
    if len(normalized_turn_id) > 120 or len(normalized_step_id) > 120:
        raise BusinessValidationError("素材整理 Draft artifact 来源 ID 无效")
    return normalized_turn_id, normalized_step_id


def _normalize_confirmation_idempotency_key(value: str) -> str:
    normalized = value.strip()
    if not normalized or len(normalized.encode("utf-8")) > MAX_CONFIRMATION_IDEMPOTENCY_KEY_BYTES:
        raise BusinessValidationError("素材整理确认 idempotency key 无效")
    return normalized


def _confirmation_request_hash(draft_id: str, expected_draft_version: int) -> str:
    payload = {"draft_id": draft_id, "expected_draft_version": expected_draft_version}
    return sha256(json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def _observe_operations(
    session: Session,
    artifact: LibraryOrganizationDraftPayloadV1,
) -> tuple[
    dict[str, MediaLibraryAsset],
    dict[str, MediaLibraryFolder],
    dict[str, WorkflowGraph],
]:
    asset_ids = sorted({operation.asset_id for operation in artifact.operations})
    assets = list(
        session.scalars(
            select(MediaLibraryAsset)
            .where(MediaLibraryAsset.id.in_(asset_ids))
            .order_by(MediaLibraryAsset.id)
            .options(
                selectinload(MediaLibraryAsset.media_object),
                selectinload(MediaLibraryAsset.folder),
                selectinload(MediaLibraryAsset.tag_assignments).selectinload(MediaLibraryAssetTag.tag),
                selectinload(MediaLibraryAsset.source_product_asset),
                selectinload(MediaLibraryAsset.source_image_session_asset),
            )
            .with_for_update()
        ).all()
    )
    if len(assets) != len(asset_ids):
        raise NotFoundError("素材整理 Draft 引用了不存在的素材")
    assets_by_id = {asset.id: asset for asset in assets}

    folder_ids = sorted(
        {
            operation.target.folder_id
            for operation in artifact.operations
            if isinstance(operation, LibraryMoveOperationV1) and operation.target.folder_id is not None
        }
    )
    folders = list(
        session.scalars(
            select(MediaLibraryFolder)
            .where(MediaLibraryFolder.id.in_(folder_ids))
            .order_by(MediaLibraryFolder.id)
            .with_for_update()
        ).all()
        if folder_ids
        else []
    )
    folders_by_id = {folder.id: folder for folder in folders}
    if len(folders_by_id) != len(folder_ids):
        raise NotFoundError("素材整理 Draft 引用了不存在的文件夹")

    workflow_ids = sorted(
        {
            operation.target.workflow_id
            for operation in artifact.operations
            if isinstance(operation, LibraryLinkWorkflowOperationV1)
        }
    )
    workflows = list(
        session.scalars(
            select(WorkflowGraph)
            .where(WorkflowGraph.id.in_(workflow_ids))
            .order_by(WorkflowGraph.id)
            .with_for_update()
        ).all()
        if workflow_ids
        else []
    )
    workflows_by_id = {workflow.id: workflow for workflow in workflows}
    if len(workflows_by_id) != len(workflow_ids):
        raise NotFoundError("素材整理 Draft 引用了不存在的工作流")

    existing_workflow_links = {
        (workflow_id, asset_id)
        for workflow_id, asset_id in session.execute(
            select(
                WorkflowMediaLibraryAsset.workflow_id,
                WorkflowMediaLibraryAsset.media_library_asset_id,
            ).where(
                WorkflowMediaLibraryAsset.workflow_id.in_(workflow_ids),
                WorkflowMediaLibraryAsset.media_library_asset_id.in_(asset_ids),
            )
        ).all()
    }

    for operation in artifact.operations:
        asset = assets_by_id[operation.asset_id]
        validate_media_library_asset_integrity(asset)
        _validate_before_summary(asset, operation.before, operation.expected_revision)
        if isinstance(operation, LibraryLinkWorkflowOperationV1):
            workflow = workflows_by_id[operation.target.workflow_id]
            if (
                workflow.title != operation.target.workflow_title
                or workflow.revision != operation.target.expected_workflow_revision
            ):
                raise ConflictError(f"工作流 {workflow.id} 当前状态已变化，请重新生成素材关联 Draft")
            currently_linked = (workflow.id, asset.id) in existing_workflow_links
            if currently_linked != operation.target.expected_linked:
                raise ConflictError(f"素材 {asset.id} 与工作流 {workflow.id} 的关联状态已变化")
            validate_media_library_asset_for_use(asset)
        elif isinstance(operation, LibraryArchiveOperationV1):
            if asset.is_archived:
                raise ConflictError("只有 active 素材才能执行归档")
            _ensure_not_linked_to_workflow(session, asset.id)
        elif isinstance(operation, LibraryRestoreOperationV1):
            if not asset.is_archived:
                raise ConflictError("只有已归档素材才能执行恢复")
        elif asset.is_archived:
            raise ConflictError("归档素材只能先恢复，不能直接整理")
    return assets_by_id, folders_by_id, workflows_by_id


def _validate_before_summary(
    asset: MediaLibraryAsset,
    before: LibraryAssetBeforeV1,
    expected_revision: int,
) -> None:
    current_tag_names = sorted(
        (assignment.tag.name for assignment in asset.tag_assignments if assignment.tag is not None),
        key=str.casefold,
    )
    expected_tag_names = sorted(before.tag_names, key=str.casefold)
    if (
        asset.revision != expected_revision
        or before.revision != expected_revision
        or asset.display_name != before.display_name
        or asset.folder_id != before.folder_id
        or current_tag_names != expected_tag_names
        or asset.is_archived != before.is_archived
    ):
        raise ConflictError(f"素材 {asset.id} 当前状态已变化，请重新生成整理 Draft")


def _load_or_create_tags(
    session: Session,
    artifact: LibraryOrganizationDraftPayloadV1,
) -> dict[str, MediaLibraryTag]:
    tag_names_by_key = {
        name.casefold(): name
        for operation in artifact.operations
        if isinstance(operation, LibrarySetTagsOperationV1)
        for name in operation.target.tag_names
    }
    if not tag_names_by_key:
        return {}
    keys = sorted(tag_names_by_key)
    tags = list(
        session.scalars(
            select(MediaLibraryTag)
            .where(MediaLibraryTag.normalized_name.in_(keys))
            .order_by(MediaLibraryTag.id)
            .with_for_update()
        ).all()
    )
    tags_by_key = {tag.normalized_name: tag for tag in tags}
    for key in keys:
        if key in tags_by_key:
            continue
        tag = MediaLibraryTag(name=tag_names_by_key[key], normalized_name=key)
        session.add(tag)
        try:
            session.flush()
        except IntegrityError as exc:
            raise ConflictError("素材整理 Draft 创建标签时发生并发冲突，请重试") from exc
        tags_by_key[key] = tag
    return tags_by_key


def _apply_operations(
    *,
    session: Session,
    artifact: LibraryOrganizationDraftPayloadV1,
    assets: dict[str, MediaLibraryAsset],
    folders: dict[str, MediaLibraryFolder],
    workflows: dict[str, WorkflowGraph],
    tags: dict[str, MediaLibraryTag],
    draft_id: str,
    revision_version: int,
) -> dict[str, Any]:
    result_assets: list[dict[str, Any]] = []
    result_workflow_links: list[dict[str, Any]] = []
    for operation in artifact.operations:
        asset = assets[operation.asset_id]
        changed = False
        if isinstance(operation, LibraryLinkWorkflowOperationV1):
            workflow = workflows[operation.target.workflow_id]
            sync_workflow_media_library_assets(
                session,
                product_id=workflow.product_id,
                workflow_id=workflow.id,
                media_library_asset_ids=[asset.id],
                commit=False,
            )
            result_workflow_links.append(
                {
                    "asset_id": asset.id,
                    "product_id": workflow.product_id,
                    "workflow_id": workflow.id,
                    "workflow_title": workflow.title,
                    "linked": True,
                    "changed": not operation.target.expected_linked,
                }
            )
        elif isinstance(operation, LibraryRenameOperationV1):
            target_name = operation.target.display_name
            changed = asset.display_name != target_name
            asset.display_name = target_name
        elif isinstance(operation, LibraryMoveOperationV1):
            target_folder_id = operation.target.folder_id
            changed = asset.folder_id != target_folder_id
            asset.folder_id = target_folder_id
            asset.folder = folders.get(target_folder_id)
        elif isinstance(operation, LibrarySetTagsOperationV1):
            target_tag_names = list(operation.target.tag_names)
            current_tag_names = sorted(
                (assignment.tag.name for assignment in asset.tag_assignments if assignment.tag is not None),
                key=str.casefold,
            )
            changed = current_tag_names != sorted(target_tag_names, key=str.casefold)
            if changed:
                asset.tag_assignments.clear()
                asset.tag_assignments.extend(
                    MediaLibraryAssetTag(tag=tags[tag_name.casefold()]) for tag_name in target_tag_names
                )
        elif isinstance(operation, LibraryArchiveOperationV1):
            changed = not asset.is_archived
            asset.is_archived = True
            asset.archived_at = now_utc()
        elif isinstance(operation, LibraryRestoreOperationV1):
            changed = asset.is_archived
            asset.is_archived = False
            asset.archived_at = None
        if changed:
            asset.revision += 1
            asset.updated_at = now_utc()
        result_assets.append(_result_asset(asset))
    result = {
        "schema_version": 1,
        "draft_id": draft_id,
        "revision_version": revision_version,
        "assets": result_assets,
    }
    if result_workflow_links:
        result["workflow_links"] = result_workflow_links
    return result


def _result_asset(asset: MediaLibraryAsset) -> dict[str, Any]:
    return {
        "asset_id": asset.id,
        "display_name": asset.display_name,
        "folder_id": asset.folder_id,
        "tag_names": sorted(
            (assignment.tag.name for assignment in asset.tag_assignments if assignment.tag is not None),
            key=str.casefold,
        ),
        "revision": asset.revision,
        "is_archived": asset.is_archived,
    }


def _ensure_not_linked_to_workflow(session: Session, asset_id: str) -> None:
    linked = session.scalar(
        select(WorkflowMediaLibraryAsset.workflow_id)
        .where(WorkflowMediaLibraryAsset.media_library_asset_id == asset_id)
        .limit(1)
    )
    if linked is not None:
        raise ConflictError("素材仍被工作流素材库使用，解除关联后才能归档")


__all__ = [
    "LIBRARY_ORGANIZATION_DRAFT_ARTIFACT_NAME",
    "append_library_organization_draft_revision",
    "confirm_library_organization_draft_revision",
    "get_library_organization_draft_or_raise",
    "library_organization_draft_query",
    "parse_library_organization_draft_payload_or_raise",
    "validate_library_organization_draft",
]
