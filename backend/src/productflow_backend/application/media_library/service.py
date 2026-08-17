from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime

from sqlalchemy import select, update
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from productflow_backend.application.media_library.contracts import (
    MediaLibrarySourceType,
    canonical_provenance_hash,
    parse_provenance_v1,
)
from productflow_backend.application.media_library.queries import (
    get_media_library_asset,
    list_media_library_assets,
)
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
    MediaObject,
    Product,
    ProductImageAsset,
    WorkflowMediaLibraryAsset,
)


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
    """Validate immutable media and provenance without applying archive-state policy."""
    media = _verified_media(library_asset.media_object)
    _assert_library_asset_coherent(library_asset, media=media)
    return media


def validate_media_library_asset_for_use(library_asset: MediaLibraryAsset) -> MediaObject:
    """Validate the immutable media and provenance before another feature uses an asset."""
    if library_asset.is_archived:
        raise ConflictError("归档素材不能收录到商品")
    return validate_media_library_asset_integrity(library_asset)


def _origin_type_for_library_asset(library_asset: MediaLibraryAsset) -> ProductImageOriginType:
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
    return ProductImageOriginType.LEGACY_IMPORT


def collect_media_library_assets_to_product(
    session: Session,
    *,
    product_id: str,
    library_asset_ids: list[str],
) -> list[MediaLibraryCollectionResult]:
    """Batch-collect up to 100 unique active library assets into one product.

    Locks Product then MediaLibraryAsset rows in stable id order. All product assets
    are created in one transaction; unique-race conflicts are re-queried idempotently.
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

    product = session.scalar(
        select(Product).where(Product.id == product_id).with_for_update()
    )
    if product is None:
        raise NotFoundError("商品不存在")

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
        product_asset = ProductImageAsset(
            product_id=product.id,
            media_object_id=media.id,
            origin_type=_origin_type_for_library_asset(library_asset),
            display_name=library_asset.display_name,
            original_filename=library_asset.original_filename,
            source_library_asset_id=library_asset.id,
        )
        session.add(product_asset)
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
            session.commit()
            return reloaded
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
) -> MediaLibraryCollectionResult:
    results = collect_media_library_assets_to_product(
        session,
        product_id=product_id,
        library_asset_ids=[library_asset_id],
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
    return _set_media_library_archive_state(
        session,
        asset_id=asset_id,
        archived=False,
        expected_revision=expected_revision,
    )


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
    "validate_media_library_asset_for_use",
]
