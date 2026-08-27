"""全局素材的保存、收录、归档与直接上传。

保存和收录共享 MediaObject，不复制 bytes。归档改变可见性，不删除文件。
上传走 StorageWriteCompensation：DB 失败只收回本次写入。
"""

from __future__ import annotations

import json
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import datetime

from sqlalchemy import select, update
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from productflow_backend.application.media_library.contracts import (
    MediaLibrarySourceType,
    canonical_provenance_hash,
    media_library_collection_request_hash,
    media_library_upload_request_hash,
    normalize_media_library_collection_idempotency_key,
    normalize_media_library_upload_idempotency_key,
    parse_provenance_v1,
)
from productflow_backend.application.media_library.queries import (
    get_media_library_asset,
    list_media_library_assets,
)
from productflow_backend.application.media_objects import stage_verified_media_object
from productflow_backend.application.product_images.assets import stage_product_image_identity
from productflow_backend.application.storage_compensation import compensate_storage_writes
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import (
    ImageSessionAssetKind,
    MediaVerificationStatus,
    ProductImageOriginType,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    ImageSessionAsset,
    MediaLibraryAsset,
    MediaLibraryCollectionKey,
    MediaLibraryFolder,
    MediaLibraryUploadKey,
    MediaObject,
    Product,
    ProductImageAsset,
    WorkflowMediaLibraryAsset,
)
from productflow_backend.infrastructure.storage import LocalStorage


@dataclass(frozen=True, slots=True)
class MediaLibrarySaveResult:
    asset: MediaLibraryAsset
    created: bool


@dataclass(frozen=True, slots=True)
class MediaLibraryCollectionResult:
    asset: ProductImageAsset
    created: bool


def _verified_media(media: MediaObject | None) -> MediaObject:
    if media is None:
        raise ConflictError("素材来源缺少 MediaObject")
    if media.verification_status != MediaVerificationStatus.VERIFIED:
        raise BusinessValidationError("素材媒体尚未通过核验")
    if (
        media.sha256 is None
        or media.byte_size is None
        or media.width is None
        or media.height is None
        or media.byte_size <= 0
        or media.width <= 0
        or media.height <= 0
    ):
        raise ConflictError("素材媒体缺少完整核验元数据")
    return media


def _provenance_from_media_object(
    *,
    source_type: MediaLibrarySourceType,
    source_id: str,
    media: MediaObject,
    original_filename: str,
    captured_at: datetime,
    origin_type: str | None = None,
) -> dict[str, object]:
    verified_media = _verified_media(media)
    payload: dict[str, object] = {
        "schema_version": 1,
        "source_type": source_type,
        "source_id": source_id,
        "sha256": verified_media.sha256,
        "mime_type": verified_media.mime_type,
        "byte_size": verified_media.byte_size,
        "width": verified_media.width,
        "height": verified_media.height,
        "original_filename": original_filename,
        "captured_at": captured_at.isoformat(),
    }
    if origin_type is not None:
        payload["origin_type"] = origin_type
    return payload


def _find_by_source(
    session: Session,
    *,
    source_type: MediaLibrarySourceType,
    source_id: str,
) -> MediaLibraryAsset | None:
    return session.scalar(
        select(MediaLibraryAsset).where(
            MediaLibraryAsset.source_type == source_type,
            MediaLibraryAsset.source_id == source_id,
        )
    )


def save_media_library_asset_from_session(
    session: Session,
    *,
    image_session_asset_id: str,
) -> MediaLibrarySaveResult:
    """把会话生成图登记为全局素材，复用同一 MediaObject。"""

    asset = session.scalar(
        select(ImageSessionAsset)
        .where(ImageSessionAsset.id == image_session_asset_id)
        .with_for_update()
    )
    if asset is None:
        raise NotFoundError("会话图片不存在")
    if asset.kind != ImageSessionAssetKind.GENERATED_IMAGE:
        raise BusinessValidationError("只有生成结果可以保存到素材库")
    media = _verified_media(asset.media_object)
    existing = _find_by_source(session, source_type="image_session_generated", source_id=asset.id)
    if existing is not None:
        return MediaLibrarySaveResult(asset=existing, created=False)
    provenance = _provenance_from_media_object(
        source_type="image_session_generated",
        source_id=asset.id,
        media=media,
        original_filename=asset.original_filename,
        captured_at=asset.created_at,
    )
    library_asset = MediaLibraryAsset(
        media_object_id=media.id,
        source_type="image_session_generated",
        source_id=asset.id,
        source_image_session_asset_id=asset.id,
        provenance_json=provenance,
        provenance_hash=canonical_provenance_hash(parse_provenance_v1(provenance).model_dump(mode="json")),
        display_name=asset.original_filename,
        original_filename=asset.original_filename,
    )
    session.add(library_asset)
    try:
        with session.begin_nested():
            session.flush()
    except IntegrityError:
        existing = _find_by_source(session, source_type="image_session_generated", source_id=asset.id)
        if existing is None:
            raise
        session.expire_all()
        return MediaLibrarySaveResult(
            asset=get_media_library_asset(session, asset_id=existing.id),
            created=False,
        )
    session.commit()
    session.expire_all()
    return MediaLibrarySaveResult(
        asset=get_media_library_asset(session, asset_id=library_asset.id),
        created=True,
    )


def save_media_library_asset_from_product(
    session: Session,
    *,
    product_image_asset_id: str,
) -> MediaLibrarySaveResult:
    """把商品图片登记为全局素材，复用同一 MediaObject。"""

    asset = session.scalar(
        select(ProductImageAsset)
        .where(ProductImageAsset.id == product_image_asset_id)
        .with_for_update()
    )
    if asset is None:
        raise NotFoundError("商品图片不存在")
    media = _verified_media(asset.media_object)
    existing = _find_by_source(session, source_type="product_asset", source_id=asset.id)
    if existing is not None:
        return MediaLibrarySaveResult(asset=existing, created=False)
    source_origin_type = asset.origin_type.value if hasattr(asset.origin_type, "value") else asset.origin_type
    provenance = _provenance_from_media_object(
        source_type="product_asset",
        source_id=asset.id,
        media=media,
        original_filename=asset.original_filename,
        captured_at=asset.created_at,
        origin_type=source_origin_type,
    )
    library_asset = MediaLibraryAsset(
        media_object_id=media.id,
        source_type="product_asset",
        source_id=asset.id,
        source_product_asset_id=asset.id,
        provenance_json=provenance,
        provenance_hash=canonical_provenance_hash(parse_provenance_v1(provenance).model_dump(mode="json")),
        display_name=asset.display_name,
        original_filename=asset.original_filename,
    )
    session.add(library_asset)
    try:
        with session.begin_nested():
            session.flush()
    except IntegrityError:
        existing = _find_by_source(session, source_type="product_asset", source_id=asset.id)
        if existing is None:
            raise
        session.expire_all()
        return MediaLibrarySaveResult(
            asset=get_media_library_asset(session, asset_id=existing.id),
            created=False,
        )
    session.commit()
    session.expire_all()
    return MediaLibrarySaveResult(
        asset=get_media_library_asset(session, asset_id=library_asset.id),
        created=True,
    )


MAX_COLLECTION_ASSETS = 100
MAX_COLLECTION_REQUEST_BYTES = 256 * 1024


def _assert_library_asset_coherent(
    library_asset: MediaLibraryAsset,
    *,
    media: MediaObject,
) -> None:
    try:
        provenance = parse_provenance_v1(library_asset.provenance_json)
        expected_hash = canonical_provenance_hash(provenance.model_dump(mode="json"))
    except (TypeError, ValueError) as exc:
        raise ConflictError("素材库 provenance 无法核验") from exc
    if (
        library_asset.provenance_hash != expected_hash
        or provenance.source_type != library_asset.source_type
        or provenance.source_id != library_asset.source_id
        or provenance.sha256 != media.sha256
        or provenance.mime_type != media.mime_type
        or provenance.byte_size != media.byte_size
        or provenance.width != media.width
        or provenance.height != media.height
        or provenance.original_filename != library_asset.original_filename
    ):
        raise ConflictError("素材库来源与媒体元数据不一致")
    if library_asset.source_product_asset is not None:
        if library_asset.source_product_asset.media_object_id != media.id:
            raise ConflictError("素材库商品来源与媒体不一致")
    if library_asset.source_image_session_asset is not None:
        if library_asset.source_image_session_asset.media_object_id != media.id:
            raise ConflictError("素材库会话来源与媒体不一致")


def validate_media_library_asset_integrity(library_asset: MediaLibraryAsset) -> MediaObject:
    """核验不可变媒体与 provenance；不解释归档策略。"""
    media = _verified_media(library_asset.media_object)
    _assert_library_asset_coherent(library_asset, media=media)
    return media


def validate_media_library_asset_for_use(library_asset: MediaLibraryAsset) -> MediaObject:
    """使用前核验媒体与 provenance；归档素材不能收录到商品。"""
    if library_asset.is_archived:
        raise ConflictError("归档素材不能收录到商品")
    return validate_media_library_asset_integrity(library_asset)


def _origin_type_for_library_asset(library_asset: MediaLibraryAsset) -> ProductImageOriginType:
    """映射进入商品命名空间的来源；不是自动 reject/draft。"""

    if library_asset.source_type == "image_session_generated":
        return ProductImageOriginType.IMAGE_SESSION_ATTACH
    if library_asset.source_type == "product_asset":
        if library_asset.source_product_asset is not None:
            return library_asset.source_product_asset.origin_type
        provenance_origin = (library_asset.provenance_json or {}).get("origin_type")
        if provenance_origin:
            try:
                return ProductImageOriginType(provenance_origin)
            except ValueError:
                pass
    return ProductImageOriginType.UPLOAD


def collect_media_library_assets_to_product(
    session: Session,
    *,
    product_id: str,
    library_asset_ids: list[str],
    idempotency_key: str | None = None,
    commit: bool = True,
) -> list[MediaLibraryCollectionResult]:
    """把最多 100 个未归档全局素材收录进一个商品。

    创建共享 MediaObject 的 ProductImageAsset，不复制 bytes。
    按稳定 id 顺序先锁 Product 再锁 MediaLibraryAsset。唯一键冲突按幂等回查。
    调用方拥有更大事务时传 commit=False。
    """
    if len(library_asset_ids) > MAX_COLLECTION_ASSETS:
        raise BusinessValidationError(f"一次最多收录 {MAX_COLLECTION_ASSETS} 个素材")
    if (
        not product_id
        or len(product_id) > 36
        or any(not asset_id or len(asset_id) > 36 for asset_id in library_asset_ids)
    ):
        raise BusinessValidationError("商品或素材库资产 ID 无效")
    request_bytes = len(product_id.encode("utf-8")) + sum(
        len(asset_id.encode("utf-8")) for asset_id in library_asset_ids
    )
    if request_bytes > MAX_COLLECTION_REQUEST_BYTES:
        raise BusinessValidationError("素材收录请求过大")
    unique_ids = list(dict.fromkeys(library_asset_ids))
    if len(unique_ids) != len(library_asset_ids):
        raise BusinessValidationError("素材收录请求包含重复 ID")
    normalized_idempotency_key = None
    request_hash = None
    if idempotency_key is not None:
        try:
            normalized_idempotency_key = normalize_media_library_collection_idempotency_key(idempotency_key)
        except ValueError as exc:
            raise BusinessValidationError(str(exc)) from exc
        request_hash = media_library_collection_request_hash(
            product_id=product_id,
            library_asset_ids=library_asset_ids,
        )

    product = session.scalar(
        select(Product).where(Product.id == product_id).with_for_update()
    )
    if product is None:
        raise NotFoundError("商品不存在")

    if normalized_idempotency_key is not None:
        prior_request = session.scalar(
            select(MediaLibraryCollectionKey)
            .where(
                MediaLibraryCollectionKey.product_id == product_id,
                MediaLibraryCollectionKey.idempotency_key == normalized_idempotency_key,
            )
            .with_for_update()
        )
        if prior_request is not None:
            if prior_request.request_hash != request_hash:
                raise ConflictError("相同 idempotency key 不能用于不同的素材收录参数")
            replayed_assets = list(
                session.scalars(
                    select(ProductImageAsset).where(
                        ProductImageAsset.product_id == product_id,
                        ProductImageAsset.source_library_asset_id.in_(unique_ids),
                    )
                ).all()
            )
            replayed_by_source_id = {
                asset.source_library_asset_id: asset
                for asset in replayed_assets
                if asset.source_library_asset_id is not None
            }
            if len(replayed_by_source_id) != len(unique_ids):
                raise ConflictError("素材收录幂等记录与商品图片不一致")
            return [
                MediaLibraryCollectionResult(asset=replayed_by_source_id[asset_id], created=False)
                for asset_id in unique_ids
            ]

    library_assets = list(
        session.scalars(
            select(MediaLibraryAsset)
            .where(MediaLibraryAsset.id.in_(unique_ids))
            .order_by(MediaLibraryAsset.id)
            .with_for_update()
        ).all()
    )
    if len(library_assets) != len(unique_ids):
        raise NotFoundError("素材库资产不存在")

    existing_by_library_id: dict[str, ProductImageAsset] = {}
    new_by_library_id: dict[str, ProductImageAsset] = {}
    for library_asset in library_assets:
        media = validate_media_library_asset_for_use(library_asset)
        existing = session.scalar(
            select(ProductImageAsset).where(
                ProductImageAsset.product_id == product_id,
                ProductImageAsset.source_library_asset_id == library_asset.id,
            )
        )
        if existing is not None:
            existing_by_library_id[library_asset.id] = existing
            continue
        # 商品侧新身份指向同一 MediaObject；工作流绑定用 ProductImageAsset id。
        product_asset = stage_product_image_identity(
            session,
            product=product,
            media_object=media,
            origin_type=_origin_type_for_library_asset(library_asset),
            display_name=library_asset.display_name,
            original_filename=library_asset.original_filename,
            source_library_asset_id=library_asset.id,
        )
        new_by_library_id[library_asset.id] = product_asset

    if new_by_library_id:
        product.updated_at = now_utc()
        try:
            with session.begin_nested():
                session.flush()
        except IntegrityError:
            reloaded: list[MediaLibraryCollectionResult] = []
            for library_asset_id in unique_ids:
                existing = session.scalar(
                    select(ProductImageAsset).where(
                        ProductImageAsset.product_id == product_id,
                        ProductImageAsset.source_library_asset_id == library_asset_id,
                    )
                )
                if existing is None:
                    raise
                reloaded.append(MediaLibraryCollectionResult(asset=existing, created=False))
            if normalized_idempotency_key is not None:
                session.add(
                    MediaLibraryCollectionKey(
                        product_id=product_id,
                        idempotency_key=normalized_idempotency_key,
                        request_hash=request_hash,
                    )
                )
            if commit:
                session.commit()
            return reloaded
    if normalized_idempotency_key is not None:
        session.add(
            MediaLibraryCollectionKey(
                product_id=product_id,
                idempotency_key=normalized_idempotency_key,
                request_hash=request_hash,
            )
        )
    if commit and (new_by_library_id or normalized_idempotency_key is not None):
        session.commit()
        session.expire_all()
    results: list[MediaLibraryCollectionResult] = []
    for library_asset_id in unique_ids:
        asset = existing_by_library_id.get(library_asset_id)
        if asset is None:
            pending_asset = new_by_library_id[library_asset_id]
            asset = session.get(ProductImageAsset, pending_asset.id)
        if asset is None:
            raise RuntimeError("商品图片收录后无法读取")
        results.append(
            MediaLibraryCollectionResult(
                asset=asset,
                created=library_asset_id in new_by_library_id,
            )
        )
    return results


def collect_media_library_asset_to_product(
    session: Session,
    *,
    product_id: str,
    library_asset_id: str,
    idempotency_key: str | None = None,
) -> MediaLibraryCollectionResult:
    results = collect_media_library_assets_to_product(
        session,
        product_id=product_id,
        library_asset_ids=[library_asset_id],
        idempotency_key=idempotency_key,
    )
    return results[0]


def _set_media_library_archive_state(
    session: Session,
    *,
    asset_id: str,
    archived: bool,
    expected_revision: int | None,
) -> MediaLibraryAsset:
    asset = get_media_library_asset(session, asset_id=asset_id)
    if expected_revision is not None and asset.revision != expected_revision:
        raise ConflictError("素材库资产 revision 已变化")
    if archived:
        linked_workflow_id = session.scalar(
            select(WorkflowMediaLibraryAsset.workflow_id)
            .where(WorkflowMediaLibraryAsset.media_library_asset_id == asset.id)
            .limit(1)
        )
        if linked_workflow_id is not None:
            raise ConflictError("素材仍被工作流素材库使用，解除关联后才能归档")
    current_revision = asset.revision
    if asset.is_archived == archived:
        return asset

    now = now_utc()
    result = session.execute(
        update(MediaLibraryAsset)
        .where(
            MediaLibraryAsset.id == asset_id,
            MediaLibraryAsset.revision == current_revision,
            MediaLibraryAsset.is_archived == (not archived),
        )
        .values(
            is_archived=archived,
            archived_at=now if archived else None,
            revision=current_revision + 1,
            updated_at=now,
        )
        .execution_options(synchronize_session=False)
    )
    if result.rowcount != 1:
        raise ConflictError("素材库资产 revision 已变化")
    session.commit()
    session.expire_all()
    return get_media_library_asset(session, asset_id=asset_id)


def archive_media_library_asset(
    session: Session,
    *,
    asset_id: str,
    expected_revision: int | None = None,
) -> MediaLibraryAsset:
    """归档只改可见性；仍被工作流关联时拒绝。"""

    return _set_media_library_archive_state(
        session,
        asset_id=asset_id,
        archived=True,
        expected_revision=expected_revision,
    )


def restore_media_library_asset(
    session: Session,
    *,
    asset_id: str,
    expected_revision: int | None = None,
) -> MediaLibraryAsset:
    """恢复可见性；不新建 MediaObject。"""

    return _set_media_library_archive_state(
        session,
        asset_id=asset_id,
        archived=False,
        expected_revision=expected_revision,
    )


MEDIA_LIBRARY_FILENAME_MAX_LENGTH = 255


def _normalize_upload_names(filename: str, *, display_name: str | None = None) -> tuple[str, str]:
    """规范化并截断上传文件名，使 provenance 与列值一致。

    截断发生在构造 provenance/列（因此也在任何存储写入）之前，
    避免超过 255 字符的名字生成无法通过校验、或与存库列值漂移的 provenance。
    """

    normalized = (filename or "").strip() or "upload.png"
    bounded_display = (display_name or normalized).strip() or normalized
    return (
        normalized[:MEDIA_LIBRARY_FILENAME_MAX_LENGTH],
        bounded_display[:MEDIA_LIBRARY_FILENAME_MAX_LENGTH] or normalized[:MEDIA_LIBRARY_FILENAME_MAX_LENGTH],
    )


def save_media_library_assets_from_upload(
    session: Session,
    *,
    items: Sequence[tuple[bytes, str, str | None]],
    folder_id: str | None = None,
    display_names: Sequence[str | None] | None = None,
    idempotency_key: str | None = None,
    storage: LocalStorage | None = None,
) -> list[MediaLibrarySaveResult]:
    """原子持久化一批直传素材库资产。

    所有文件在同一事务、同一 storage-compensation 范围内暂存，然后一次性 commit。
    任何失败都会整批回滚，并只删除本次尝试创建的文件（见 AGENTS.md 存储补偿规则），
    客户端不会在错误响应里看到半提交批次。

    提供 ``idempotency_key`` 时，用相同上传参数重用该键会返回先前创建的资产
    （与 ``collect_media_library_assets_to_product`` 一致）；参数不同则冲突。
    """

    storage = storage or LocalStorage()
    if folder_id is not None:
        folder = session.get(MediaLibraryFolder, folder_id)
        if folder is None:
            raise NotFoundError("文件夹不存在")

    normalized_idempotency_key: str | None = None
    request_hash: str | None = None
    if idempotency_key is not None:
        try:
            normalized_idempotency_key = normalize_media_library_upload_idempotency_key(idempotency_key)
        except ValueError as exc:
            raise BusinessValidationError(str(exc)) from exc
        request_hash = media_library_upload_request_hash(
            folder_id=folder_id,
            files=[(filename, content, mime_type) for content, filename, mime_type in items],
        )
        existing_key = session.scalar(
            select(MediaLibraryUploadKey).where(
                MediaLibraryUploadKey.idempotency_key == normalized_idempotency_key,
            )
        )
        if existing_key is not None:
            if existing_key.request_hash != request_hash:
                raise ConflictError("相同 idempotency key 不能用于不同的上传参数")
            return [
                MediaLibrarySaveResult(
                    asset=get_media_library_asset(session, asset_id=asset_id),
                    created=False,
                )
                for asset_id in json.loads(existing_key.asset_ids_json)
            ]

    normalized_items: list[tuple[bytes, str, str, str | None]] = []
    for index, (content, filename, expected_mime_type) in enumerate(items):
        display_name = display_names[index] if display_names is not None else None
        bounded_filename, bounded_display = _normalize_upload_names(filename, display_name=display_name)
        normalized_items.append((content, bounded_filename, bounded_display, expected_mime_type))

    results: list[MediaLibrarySaveResult] = []
    with compensate_storage_writes(session) as storage_writes:
        for content, bounded_filename, bounded_display, expected_mime_type in normalized_items:
            media = stage_verified_media_object(
                session,
                content=content,
                filename=bounded_filename,
                expected_mime_type=expected_mime_type,
                storage=storage,
                storage_writes=storage_writes,
            )
            captured_at = now_utc()
            provenance = _provenance_from_media_object(
                source_type="direct_upload",
                source_id=media.id,
                media=media,
                original_filename=bounded_filename,
                captured_at=captured_at,
            )
            library_asset = MediaLibraryAsset(
                media_object_id=media.id,
                source_type="direct_upload",
                source_id=media.id,
                provenance_json=provenance,
                provenance_hash=canonical_provenance_hash(parse_provenance_v1(provenance).model_dump(mode="json")),
                display_name=bounded_display,
                original_filename=bounded_filename,
                folder_id=folder_id,
            )
            session.add(library_asset)
            session.flush()
            results.append(MediaLibrarySaveResult(asset=library_asset, created=True))
        if normalized_idempotency_key is not None:
            session.add(
                MediaLibraryUploadKey(
                    idempotency_key=normalized_idempotency_key,
                    request_hash=request_hash or "",
                    asset_ids_json=json.dumps([result.asset.id for result in results]),
                )
            )
        session.commit()
    session.expire_all()
    return [
        MediaLibrarySaveResult(asset=get_media_library_asset(session, asset_id=result.asset.id), created=True)
        for result in results
    ]


def save_media_library_asset_from_upload(
    session: Session,
    *,
    content: bytes,
    filename: str,
    expected_mime_type: str | None,
    folder_id: str | None = None,
    display_name: str | None = None,
    storage: LocalStorage | None = None,
) -> MediaLibrarySaveResult:
    saved = save_media_library_assets_from_upload(
        session,
        items=[(content, filename, expected_mime_type)],
        folder_id=folder_id,
        display_names=[display_name],
        storage=storage,
    )
    return saved[0]


__all__ = [
    "MediaLibraryCollectionResult",
    "MediaLibrarySaveResult",
    "archive_media_library_asset",
    "collect_media_library_asset_to_product",
    "collect_media_library_assets_to_product",
    "list_media_library_assets",
    "restore_media_library_asset",
    "save_media_library_asset_from_product",
    "save_media_library_asset_from_session",
    "save_media_library_asset_from_upload",
    "save_media_library_assets_from_upload",
    "validate_media_library_asset_for_use",
]
