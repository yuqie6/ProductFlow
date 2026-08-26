"""商品图库 Agent 工具：有界读取与可逆整理 mutation。"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from pydantic import ValidationError
from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent.agent_context import _require_product_conversation
from productflow_backend.application.agent.conversations import get_agent_conversation_by_id_or_raise
from productflow_backend.application.agent.product_workspaces import (
    finalize_agent_product_workspace_intake_from_assets,
)
from productflow_backend.application.agent.tool_ledger import (
    AgentToolMutationStage,
    AgentToolReconcileResult,
    apply_tool_mutation,
    reconcile_tool_mutation,
)
from productflow_backend.application.agent.turn_projection import AGENT_MAX_INPUT_ASSETS
from productflow_backend.application.media_objects import inspect_image_bytes
from productflow_backend.application.product_images.mutations import (
    GALLERY_MOVE_MAX_ASSETS,
    GalleryAssetMove,
    normalize_gallery_display_name,
    normalize_gallery_folder_name,
    stage_create_gallery_folder,
    stage_move_gallery_assets,
    stage_rename_gallery_asset,
    stage_rename_gallery_folder,
)
from productflow_backend.application.product_images.queries import (
    GalleryAssetRecord,
    GalleryAssetSort,
    GalleryDirectoryKind,
    list_gallery_assets,
)
from productflow_backend.application.product_intake import AgentProductSelectionV1
from productflow_backend.domain.enums import MediaVerificationStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    ProductAssetFolder,
    ProductImageAsset,
    new_id,
)
from productflow_backend.infrastructure.storage import LocalStorage

AGENT_ASSET_LIST_DEFAULT_LIMIT = 50
AGENT_ASSET_LIST_MAX_LIMIT = 100
AGENT_ASSET_MAX_BYTES = 20 * 1024 * 1024

RENAME_ASSET_TOOL_NAME = "rename_product_image_asset_v1"
CREATE_FOLDER_TOOL_NAME = "create_product_image_folder_v1"
RENAME_FOLDER_TOOL_NAME = "rename_product_image_folder_v1"
MOVE_ASSETS_TOOL_NAME = "move_product_image_assets_v1"


@dataclass(frozen=True, slots=True)
class AgentAssetPage:
    items: list[dict[str, Any]]
    next_cursor: str | None


@dataclass(frozen=True, slots=True)
class AgentAssetContent:
    content: bytes
    media_type: str
    display_name: str


@dataclass(frozen=True, slots=True)
class AgentAssetRenamePrepared:
    asset_id: str
    expected_display_name: str
    target_display_name: str


@dataclass(frozen=True, slots=True)
class AgentFolderCreatePrepared:
    folder_id: str
    name: str


@dataclass(frozen=True, slots=True)
class AgentFolderRenamePrepared:
    folder_id: str
    expected_name: str
    target_name: str


@dataclass(frozen=True, slots=True)
class AgentAssetMovePrepared:
    moves: tuple[GalleryAssetMove, ...]
    target_folder_id: str | None


def finalize_agent_product_intake(
    session: Session,
    *,
    conversation_id: str,
    selection: dict[str, Any],
    reference_asset_ids: list[str],
    idempotency_key: str,
    task_id: str | None = None,
) -> dict[str, Any]:
    """把本轮参考图与图片类型写入不可变商品 intake。"""
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    try:
        parsed = AgentProductSelectionV1.model_validate(selection)
    except ValidationError as exc:
        raise BusinessValidationError("图片类型选择不符合 AgentProductSelectionV1") from exc
    if len(reference_asset_ids) != len(set(reference_asset_ids)):
        raise BusinessValidationError("参考图资产不能重复")
    creation = finalize_agent_product_workspace_intake_from_assets(
        session,
        conversation_id=conversation_id,
        selection=parsed,
        reference_asset_ids=reference_asset_ids,
        idempotency_key=idempotency_key,
        task_id=task_id,
    )
    return {
        "accepted": True,
        "intake_finalized": True,
        "product_id": creation.product.id,
        "workflow_draft_id": creation.conversation.workflow_draft_id,
        "reference_asset_ids": [asset.id for asset in creation.created_assets],
        "intake": creation.product.intake_json,
    }


def list_agent_product_assets(
    session: Session,
    *,
    conversation_id: str,
    directory_kind: GalleryDirectoryKind = GalleryDirectoryKind.ALL,
    directory_key: str | None = None,
    query: str = "",
    sort: GalleryAssetSort = GalleryAssetSort.CREATED_DESC,
    after: str = "",
    limit: int = AGENT_ASSET_LIST_DEFAULT_LIMIT,
) -> AgentAssetPage:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    page = list_gallery_assets(
        session,
        product_id=conversation.product_id,
        directory_kind=directory_kind,
        directory_key=directory_key,
        query=query,
        sort=sort,
        after=after,
        limit=limit,
    )
    return AgentAssetPage(
        items=[agent_gallery_asset_metadata(record) for record in page.items],
        next_cursor=page.next_cursor,
    )


def inspect_agent_product_assets(
    session: Session,
    *,
    conversation_id: str,
    asset_ids: list[str],
) -> list[dict[str, Any]]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    normalized_ids = _normalize_explicit_asset_ids(asset_ids)
    assets = _load_scoped_assets(
        session,
        product_id=conversation.product_id or "",
        asset_ids=normalized_ids,
    )
    by_id = {asset.id: asset for asset in assets}
    return [agent_asset_metadata(by_id[asset_id]) for asset_id in normalized_ids]


def read_agent_product_asset_content(
    session: Session,
    *,
    conversation_id: str,
    asset_id: str,
    storage: LocalStorage | None = None,
) -> AgentAssetContent:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    assets = _load_scoped_assets(
        session,
        product_id=conversation.product_id or "",
        asset_ids=[asset_id.strip()],
    )
    asset = assets[0]
    media = asset.media_object
    if media.byte_size is None or media.byte_size > AGENT_ASSET_MAX_BYTES:
        raise BusinessValidationError("图片超过 Agent 单张图片大小上限")
    resolved_storage = storage or LocalStorage()
    path = resolved_storage.resolve(media.storage_path)
    try:
        with path.open("rb") as file:
            content = file.read(AGENT_ASSET_MAX_BYTES + 1)
    except OSError as exc:
        raise NotFoundError("商品图片文件不存在") from exc
    if len(content) > AGENT_ASSET_MAX_BYTES:
        raise BusinessValidationError("图片超过 Agent 单张图片大小上限")
    actual = inspect_image_bytes(content, expected_mime_type=media.mime_type)
    if (
        actual.byte_size != media.byte_size
        or actual.width != media.width
        or actual.height != media.height
        or actual.sha256 != media.sha256
    ):
        raise ConflictError("商品图片文件与已核验媒体元数据不一致")
    return AgentAssetContent(
        content=content,
        media_type=actual.mime_type,
        display_name=asset.display_name,
    )


def prepare_agent_asset_rename(
    session: Session,
    *,
    conversation_id: str,
    asset_id: str,
    target_display_name: str,
) -> AgentAssetRenamePrepared:
    """预览重命名，不写 ledger 行。"""
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    normalized_target = normalize_gallery_display_name(target_display_name)
    asset = _get_scoped_asset(session, product_id=conversation.product_id or "", asset_id=asset_id)
    return AgentAssetRenamePrepared(
        asset_id=asset.id,
        expected_display_name=asset.display_name,
        target_display_name=normalized_target,
    )


def apply_agent_asset_rename(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    asset_id: str,
    expected_display_name: str,
    target_display_name: str,
) -> dict[str, Any]:
    """执行重命名。已有 applied ledger 行则回放，不重复 mutation。"""
    prepared = _normalize_rename_prepared(
        asset_id=asset_id,
        expected_display_name=expected_display_name,
        target_display_name=target_display_name,
    )
    before = {"asset_id": prepared.asset_id, "display_name": prepared.expected_display_name}
    target = {"asset_id": prepared.asset_id, "display_name": prepared.target_display_name}

    def stage(session: Session, conversation: AgentConversation) -> AgentToolMutationStage:
        asset = stage_rename_gallery_asset(
            session,
            product_id=conversation.product_id or "",
            asset_id=prepared.asset_id,
            expected_display_name=prepared.expected_display_name,
            display_name=prepared.target_display_name,
        )
        return AgentToolMutationStage(
            result={
                "asset_id": asset.id,
                "display_name": prepared.target_display_name,
                "applied": prepared.expected_display_name != prepared.target_display_name,
            },
            commit_kwargs={
                "asset_id": asset.id,
                "expected_display_name": prepared.expected_display_name,
                "target_display_name": prepared.target_display_name,
            },
        )

    return apply_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=RENAME_ASSET_TOOL_NAME,
        operation="rename_asset",
        before=before,
        target=target,
        stage=stage,
        validate_conversation=_require_product_conversation,
    )


def reconcile_agent_asset_rename(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    asset_id: str,
    expected_display_name: str,
    target_display_name: str,
) -> AgentToolReconcileResult:
    """对账重命名，不重放 mutation。"""
    prepared = _normalize_rename_prepared(
        asset_id=asset_id,
        expected_display_name=expected_display_name,
        target_display_name=target_display_name,
    )
    before = {"asset_id": prepared.asset_id, "display_name": prepared.expected_display_name}
    target = {"asset_id": prepared.asset_id, "display_name": prepared.target_display_name}

    def fallback(session: Session, conversation: AgentConversation) -> AgentToolReconcileResult:
        asset = _get_scoped_asset(
            session,
            product_id=conversation.product_id or "",
            asset_id=prepared.asset_id,
        )
        if asset.display_name == prepared.expected_display_name:
            return AgentToolReconcileResult(state="not_applied", detail="图片仍处于副作用执行前状态")
        return AgentToolReconcileResult(
            state="conflict",
            detail="图片名称既非预期旧值，且不存在匹配的副作用账本",
        )

    return reconcile_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=RENAME_ASSET_TOOL_NAME,
        operation="rename_asset",
        before=before,
        target=target,
        fallback=fallback,
        validate_conversation=_require_product_conversation,
    )


def prepare_agent_folder_create(
    session: Session,
    *,
    conversation_id: str,
    name: str,
) -> AgentFolderCreatePrepared:
    """为幂等 apply 预分配稳定 folder id。"""
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    normalized_name = normalize_gallery_folder_name(name)
    existing = session.scalar(
        select(ProductAssetFolder.id).where(
            ProductAssetFolder.product_id == conversation.product_id,
            ProductAssetFolder.name == normalized_name,
        )
    )
    if existing is not None:
        raise ConflictError("当前商品已存在同名文件夹")
    return AgentFolderCreatePrepared(folder_id=new_id(), name=normalized_name)


def apply_agent_folder_create(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    folder_id: str,
    name: str,
) -> dict[str, Any]:
    prepared = _normalize_folder_create_prepared(folder_id=folder_id, name=name)
    before = {"folder_id": prepared.folder_id, "exists": False}
    target = {"folder_id": prepared.folder_id, "name": prepared.name}

    def stage(session: Session, conversation: AgentConversation) -> AgentToolMutationStage:
        folder = stage_create_gallery_folder(
            session,
            product_id=conversation.product_id or "",
            folder_id=prepared.folder_id,
            name=prepared.name,
        )
        return AgentToolMutationStage(
            result={
                "folder_id": folder.id,
                "name": folder.name,
                "sort_order": folder.sort_order,
                "applied": True,
            }
        )

    return apply_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=CREATE_FOLDER_TOOL_NAME,
        operation="create_folder",
        before=before,
        target=target,
        stage=stage,
        validate_conversation=_require_product_conversation,
    )


def reconcile_agent_folder_create(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    folder_id: str,
    name: str,
) -> AgentToolReconcileResult:
    prepared = _normalize_folder_create_prepared(folder_id=folder_id, name=name)
    before = {"folder_id": prepared.folder_id, "exists": False}
    target = {"folder_id": prepared.folder_id, "name": prepared.name}

    def fallback(session: Session, conversation: AgentConversation) -> AgentToolReconcileResult:
        folder = session.scalar(
            select(ProductAssetFolder).where(
                ProductAssetFolder.id == prepared.folder_id,
                ProductAssetFolder.product_id == conversation.product_id,
            )
        )
        if folder is None:
            return AgentToolReconcileResult(state="not_applied", detail="文件夹尚未创建")
        return AgentToolReconcileResult(state="conflict", detail="稳定文件夹 ID 已存在但缺少匹配账本")

    return reconcile_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=CREATE_FOLDER_TOOL_NAME,
        operation="create_folder",
        before=before,
        target=target,
        fallback=fallback,
        validate_conversation=_require_product_conversation,
    )


def prepare_agent_folder_rename(
    session: Session,
    *,
    conversation_id: str,
    folder_id: str,
    target_name: str,
) -> AgentFolderRenamePrepared:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    folder = _get_scoped_folder(
        session,
        product_id=conversation.product_id or "",
        folder_id=folder_id,
    )
    return AgentFolderRenamePrepared(
        folder_id=folder.id,
        expected_name=folder.name,
        target_name=normalize_gallery_folder_name(target_name),
    )


def apply_agent_folder_rename(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    folder_id: str,
    expected_name: str,
    target_name: str,
) -> dict[str, Any]:
    prepared = _normalize_folder_rename_prepared(
        folder_id=folder_id,
        expected_name=expected_name,
        target_name=target_name,
    )
    before = {"folder_id": prepared.folder_id, "name": prepared.expected_name}
    target = {"folder_id": prepared.folder_id, "name": prepared.target_name}

    def stage(session: Session, conversation: AgentConversation) -> AgentToolMutationStage:
        folder = stage_rename_gallery_folder(
            session,
            product_id=conversation.product_id or "",
            folder_id=prepared.folder_id,
            expected_name=prepared.expected_name,
            name=prepared.target_name,
        )
        return AgentToolMutationStage(
            result={
                "folder_id": folder.id,
                "name": folder.name,
                "applied": prepared.expected_name != prepared.target_name,
            }
        )

    return apply_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=RENAME_FOLDER_TOOL_NAME,
        operation="rename_folder",
        before=before,
        target=target,
        stage=stage,
        validate_conversation=_require_product_conversation,
    )


def reconcile_agent_folder_rename(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    folder_id: str,
    expected_name: str,
    target_name: str,
) -> AgentToolReconcileResult:
    prepared = _normalize_folder_rename_prepared(
        folder_id=folder_id,
        expected_name=expected_name,
        target_name=target_name,
    )
    before = {"folder_id": prepared.folder_id, "name": prepared.expected_name}
    target = {"folder_id": prepared.folder_id, "name": prepared.target_name}

    def fallback(session: Session, conversation: AgentConversation) -> AgentToolReconcileResult:
        folder = _get_scoped_folder(
            session,
            product_id=conversation.product_id or "",
            folder_id=prepared.folder_id,
        )
        if folder.name == prepared.expected_name:
            return AgentToolReconcileResult(state="not_applied", detail="文件夹仍处于改名前状态")
        return AgentToolReconcileResult(state="conflict", detail="文件夹名称已变化且缺少匹配账本")

    return reconcile_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=RENAME_FOLDER_TOOL_NAME,
        operation="rename_folder",
        before=before,
        target=target,
        fallback=fallback,
        validate_conversation=_require_product_conversation,
    )


def prepare_agent_asset_move(
    session: Session,
    *,
    conversation_id: str,
    asset_ids: list[str],
    target_folder_id: str | None,
) -> AgentAssetMovePrepared:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    normalized_ids = _normalize_move_asset_ids(asset_ids)
    normalized_target = _normalize_optional_id(target_folder_id, field_name="目标文件夹 ID")
    if normalized_target is not None:
        _get_scoped_folder(
            session,
            product_id=conversation.product_id or "",
            folder_id=normalized_target,
        )
    assets = _load_scoped_asset_metadata(
        session,
        product_id=conversation.product_id or "",
        asset_ids=normalized_ids,
    )
    by_id = {asset.id: asset for asset in assets}
    return AgentAssetMovePrepared(
        moves=tuple(
            GalleryAssetMove(asset_id=asset_id, expected_folder_id=by_id[asset_id].user_folder_id)
            for asset_id in normalized_ids
        ),
        target_folder_id=normalized_target,
    )


def apply_agent_asset_move(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    moves: list[GalleryAssetMove],
    target_folder_id: str | None,
) -> dict[str, Any]:
    prepared = _normalize_asset_move_prepared(moves=moves, target_folder_id=target_folder_id)
    before = {"moves": [{"asset_id": move.asset_id, "folder_id": move.expected_folder_id} for move in prepared.moves]}
    target = {
        "asset_ids": [move.asset_id for move in prepared.moves],
        "folder_id": prepared.target_folder_id,
    }

    def stage(session: Session, conversation: AgentConversation) -> AgentToolMutationStage:
        assets = stage_move_gallery_assets(
            session,
            product_id=conversation.product_id or "",
            moves=list(prepared.moves),
            folder_id=prepared.target_folder_id,
        )
        return AgentToolMutationStage(
            result={
                "asset_ids": [asset.id for asset in assets],
                "folder_id": prepared.target_folder_id,
                "applied": any(move.expected_folder_id != prepared.target_folder_id for move in prepared.moves),
            }
        )

    return apply_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=MOVE_ASSETS_TOOL_NAME,
        operation="move_assets",
        before=before,
        target=target,
        stage=stage,
        validate_conversation=_require_product_conversation,
    )


def reconcile_agent_asset_move(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    moves: list[GalleryAssetMove],
    target_folder_id: str | None,
) -> AgentToolReconcileResult:
    prepared = _normalize_asset_move_prepared(moves=moves, target_folder_id=target_folder_id)
    before = {"moves": [{"asset_id": move.asset_id, "folder_id": move.expected_folder_id} for move in prepared.moves]}
    target = {
        "asset_ids": [move.asset_id for move in prepared.moves],
        "folder_id": prepared.target_folder_id,
    }

    def fallback(session: Session, conversation: AgentConversation) -> AgentToolReconcileResult:
        assets = _load_scoped_asset_metadata(
            session,
            product_id=conversation.product_id or "",
            asset_ids=[move.asset_id for move in prepared.moves],
        )
        current = {asset.id: asset.user_folder_id for asset in assets}
        if all(current[move.asset_id] == move.expected_folder_id for move in prepared.moves):
            return AgentToolReconcileResult(state="not_applied", detail="图片仍处于移动前目录")
        return AgentToolReconcileResult(state="conflict", detail="图片目录已变化且缺少匹配账本")

    return reconcile_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=MOVE_ASSETS_TOOL_NAME,
        operation="move_assets",
        before=before,
        target=target,
        fallback=fallback,
        validate_conversation=_require_product_conversation,
    )


def agent_asset_metadata(asset: ProductImageAsset) -> dict[str, Any]:
    media = asset.media_object
    return {
        "id": asset.id,
        "display_name": asset.display_name,
        "original_filename": asset.original_filename,
        "origin_type": asset.origin_type.value,
        "image_type_key": asset.image_type_key,
        "image_type_title": None,
        "user_folder_id": asset.user_folder_id,
        "user_folder_name": asset.user_folder.name if asset.user_folder is not None else None,
        "mime_type": media.mime_type,
        "byte_size": media.byte_size,
        "width": media.width,
        "height": media.height,
        "verification_status": media.verification_status.value,
        "parent_asset_id": asset.parent_asset_id,
        "generation": None,
        "created_at": asset.created_at.isoformat(),
    }


def agent_gallery_asset_metadata(record: GalleryAssetRecord) -> dict[str, Any]:
    metadata = agent_asset_metadata(record.asset)
    metadata["image_type_title"] = record.image_type_title
    metadata["generation"] = (
        {
            "workflow_id": record.generation.workflow_id,
            "node_id": record.generation.node_id,
            "node_run_id": record.generation.node_run_id,
            "prompt_artifact_version_id": record.generation.prompt_artifact_version_id,
            "visual_system_version_id": record.generation.visual_system_version_id,
        }
        if record.generation is not None
        else None
    )
    return metadata


def _load_scoped_assets(
    session: Session,
    *,
    product_id: str,
    asset_ids: list[str],
) -> list[ProductImageAsset]:
    normalized_ids = _normalize_explicit_asset_ids(asset_ids)
    assets = list(
        session.scalars(
            select(ProductImageAsset)
            .options(
                selectinload(ProductImageAsset.media_object),
                selectinload(ProductImageAsset.user_folder),
            )
            .where(
                ProductImageAsset.product_id == product_id,
                ProductImageAsset.id.in_(normalized_ids),
            )
        ).all()
    )
    if len(assets) != len(normalized_ids):
        raise NotFoundError("商品图片不存在")
    if any(asset.media_object.verification_status != MediaVerificationStatus.VERIFIED for asset in assets):
        raise BusinessValidationError("Agent 只能读取已通过媒体核验的商品图片")
    return assets


def _load_scoped_asset_metadata(
    session: Session,
    *,
    product_id: str,
    asset_ids: list[str],
) -> list[ProductImageAsset]:
    assets = list(
        session.scalars(
            select(ProductImageAsset)
            .options(
                selectinload(ProductImageAsset.media_object),
                selectinload(ProductImageAsset.user_folder),
            )
            .where(
                ProductImageAsset.product_id == product_id,
                ProductImageAsset.id.in_(asset_ids),
            )
        ).all()
    )
    if len(assets) != len(asset_ids):
        raise NotFoundError("商品图片不存在")
    return assets


def _get_scoped_asset(
    session: Session,
    *,
    product_id: str,
    asset_id: str,
) -> ProductImageAsset:
    normalized_id = _normalize_required_id(asset_id, field_name="商品图片 ID")
    asset = session.scalar(
        select(ProductImageAsset)
        .options(selectinload(ProductImageAsset.media_object), selectinload(ProductImageAsset.user_folder))
        .where(ProductImageAsset.id == normalized_id, ProductImageAsset.product_id == product_id)
    )
    if asset is None:
        raise NotFoundError("商品图片不存在")
    return asset


def _get_scoped_folder(
    session: Session,
    *,
    product_id: str,
    folder_id: str,
) -> ProductAssetFolder:
    normalized_id = _normalize_required_id(folder_id, field_name="文件夹 ID")
    folder = session.scalar(
        select(ProductAssetFolder).where(
            ProductAssetFolder.id == normalized_id,
            ProductAssetFolder.product_id == product_id,
        )
    )
    if folder is None:
        raise NotFoundError("商品图片文件夹不存在")
    return folder


def _normalize_explicit_asset_ids(values: list[str]) -> list[str]:
    normalized = [value.strip() for value in values]
    if not normalized or any(not value for value in normalized):
        raise BusinessValidationError("请明确提供要查看的商品图片 ID")
    if len(normalized) > AGENT_MAX_INPUT_ASSETS:
        raise BusinessValidationError(f"单次最多查看 {AGENT_MAX_INPUT_ASSETS} 张商品图片")
    if len(set(normalized)) != len(normalized):
        raise BusinessValidationError("商品图片 ID 不能重复")
    return normalized


def _normalize_required_id(value: str, *, field_name: str) -> str:
    normalized = value.strip()
    if not normalized or len(normalized) > 36:
        raise BusinessValidationError(f"{field_name}无效")
    return normalized


def _normalize_optional_id(value: str | None, *, field_name: str) -> str | None:
    if value is None:
        return None
    return _normalize_required_id(value, field_name=field_name)


def _normalize_rename_prepared(
    *,
    asset_id: str,
    expected_display_name: str,
    target_display_name: str,
) -> AgentAssetRenamePrepared:
    normalized_asset_id = asset_id.strip()
    if not normalized_asset_id:
        raise BusinessValidationError("商品图片 ID 不能为空")
    return AgentAssetRenamePrepared(
        asset_id=_normalize_required_id(normalized_asset_id, field_name="商品图片 ID"),
        expected_display_name=normalize_gallery_display_name(expected_display_name),
        target_display_name=normalize_gallery_display_name(target_display_name),
    )


def _normalize_folder_create_prepared(*, folder_id: str, name: str) -> AgentFolderCreatePrepared:
    return AgentFolderCreatePrepared(
        folder_id=_normalize_required_id(folder_id, field_name="文件夹 ID"),
        name=normalize_gallery_folder_name(name),
    )


def _normalize_folder_rename_prepared(
    *,
    folder_id: str,
    expected_name: str,
    target_name: str,
) -> AgentFolderRenamePrepared:
    return AgentFolderRenamePrepared(
        folder_id=_normalize_required_id(folder_id, field_name="文件夹 ID"),
        expected_name=normalize_gallery_folder_name(expected_name),
        target_name=normalize_gallery_folder_name(target_name),
    )


def _normalize_move_asset_ids(values: list[str]) -> list[str]:
    if not 1 <= len(values) <= GALLERY_MOVE_MAX_ASSETS:
        raise BusinessValidationError(f"单次必须移动 1 到 {GALLERY_MOVE_MAX_ASSETS} 张图片")
    normalized = [_normalize_required_id(value, field_name="商品图片 ID") for value in values]
    if len(set(normalized)) != len(normalized):
        raise BusinessValidationError("移动列表不能包含重复图片 ID")
    return normalized


def _normalize_asset_move_prepared(
    *,
    moves: list[GalleryAssetMove],
    target_folder_id: str | None,
) -> AgentAssetMovePrepared:
    normalized_ids = _normalize_move_asset_ids([move.asset_id for move in moves])
    normalized_moves = tuple(
        GalleryAssetMove(
            asset_id=asset_id,
            expected_folder_id=_normalize_optional_id(
                move.expected_folder_id,
                field_name="expected folder ID",
            ),
        )
        for asset_id, move in zip(normalized_ids, moves, strict=True)
    )
    return AgentAssetMovePrepared(
        moves=normalized_moves,
        target_folder_id=_normalize_optional_id(target_folder_id, field_name="目标文件夹 ID"),
    )


__all__ = [
    "AGENT_ASSET_LIST_DEFAULT_LIMIT",
    "AGENT_ASSET_LIST_MAX_LIMIT",
    "AGENT_ASSET_MAX_BYTES",
    "CREATE_FOLDER_TOOL_NAME",
    "MOVE_ASSETS_TOOL_NAME",
    "RENAME_ASSET_TOOL_NAME",
    "RENAME_FOLDER_TOOL_NAME",
    "AgentAssetContent",
    "AgentAssetMovePrepared",
    "AgentAssetPage",
    "AgentAssetRenamePrepared",
    "AgentFolderCreatePrepared",
    "AgentFolderRenamePrepared",
    "AgentToolReconcileResult",
    "agent_asset_metadata",
    "agent_gallery_asset_metadata",
    "apply_agent_asset_move",
    "apply_agent_asset_rename",
    "apply_agent_folder_create",
    "apply_agent_folder_rename",
    "finalize_agent_product_intake",
    "inspect_agent_product_assets",
    "list_agent_product_assets",
    "prepare_agent_asset_move",
    "prepare_agent_asset_rename",
    "prepare_agent_folder_create",
    "prepare_agent_folder_rename",
    "read_agent_product_asset_content",
    "reconcile_agent_asset_move",
    "reconcile_agent_asset_rename",
    "reconcile_agent_folder_create",
    "reconcile_agent_folder_rename",
]
