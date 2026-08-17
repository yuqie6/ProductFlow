from __future__ import annotations

from dataclasses import dataclass
from hashlib import sha256
from io import BytesIO

from PIL import Image, UnidentifiedImageError
from sqlalchemy import select, update
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.storage_compensation import (
    StorageWriteCompensation,
    best_effort_storage_delete,
    compensate_storage_writes,
)
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import MediaVerificationStatus, ProductImageOriginType
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    DeliveryRenditionJob,
    ImagePromptArtifactVersionReference,
    ImageSessionAsset,
    LegacyWorkflowArchiveAsset,
    MediaLibraryAsset,
    MediaObject,
    Product,
    ProductImageAsset,
    VisualSystemVersionReference,
    WorkflowImageGenerationRecord,
    WorkflowImageGenerationReference,
    WorkflowNode,
    new_id,
)
from productflow_backend.infrastructure.storage import LocalStorage

_IMAGE_FORMAT_MIME_TYPES = {
    "PNG": "image/png",
    "JPEG": "image/jpeg",
    "WEBP": "image/webp",
}


@dataclass(frozen=True, slots=True)
class VerifiedImageMetadata:
    mime_type: str
    byte_size: int
    width: int
    height: int
    sha256: str


def inspect_image_bytes(content: bytes, *, expected_mime_type: str | None = None) -> VerifiedImageMetadata:
    if not content:
        raise BusinessValidationError("图片内容不能为空")
    try:
        with Image.open(BytesIO(content)) as image:
            image.verify()
        with Image.open(BytesIO(content)) as image:
            width, height = image.size
            mime_type = _IMAGE_FORMAT_MIME_TYPES.get(image.format or "")
    except (OSError, UnidentifiedImageError) as exc:
        raise BusinessValidationError("图片内容不是可解码的 PNG、JPEG 或 WEBP") from exc
    if mime_type is None:
        raise BusinessValidationError("图片格式仅支持 PNG、JPEG 或 WEBP")
    if width <= 0 or height <= 0:
        raise BusinessValidationError("图片尺寸无效")
    normalized_expected = expected_mime_type.split(";", maxsplit=1)[0].strip().lower() if expected_mime_type else None
    if normalized_expected and normalized_expected != mime_type:
        raise BusinessValidationError("图片内容格式与声明媒体类型不一致")
    return VerifiedImageMetadata(
        mime_type=mime_type,
        byte_size=len(content),
        width=width,
        height=height,
        sha256=sha256(content).hexdigest(),
    )


def stage_verified_media_object(
    session: Session,
    *,
    content: bytes,
    filename: str,
    expected_mime_type: str | None,
    storage: LocalStorage,
    storage_writes: StorageWriteCompensation,
) -> MediaObject:
    metadata = inspect_image_bytes(content, expected_mime_type=expected_mime_type)
    media_id = new_id()
    storage_path = storage_writes.track(
        storage,
        storage.save_media_image(media_id, filename, content),
    )
    media = MediaObject(
        id=media_id,
        storage_path=storage_path,
        mime_type=metadata.mime_type,
        byte_size=metadata.byte_size,
        width=metadata.width,
        height=metadata.height,
        sha256=metadata.sha256,
        verification_status=MediaVerificationStatus.VERIFIED,
        verified_at=now_utc(),
    )
    session.add(media)
    return media


def stage_product_image_asset(
    session: Session,
    *,
    product: Product,
    content: bytes,
    filename: str,
    expected_mime_type: str | None,
    display_name: str | None,
    origin_type: ProductImageOriginType,
    storage: LocalStorage,
    storage_writes: StorageWriteCompensation,
    parent_asset_id: str | None = None,
    image_type_key: str | None = None,
) -> ProductImageAsset:
    if parent_asset_id is not None:
        parent_asset = session.get(ProductImageAsset, parent_asset_id)
        if parent_asset is None or parent_asset.product_id != product.id:
            raise BusinessValidationError("父图片资产不属于当前商品")
    media = stage_verified_media_object(
        session,
        content=content,
        filename=filename,
        expected_mime_type=expected_mime_type,
        storage=storage,
        storage_writes=storage_writes,
    )
    normalized_filename = filename.strip() or "image"
    normalized_display_name = (display_name or normalized_filename).strip() or normalized_filename
    asset = ProductImageAsset(
        product_id=product.id,
        media_object=media,
        origin_type=origin_type,
        display_name=normalized_display_name[:255],
        original_filename=normalized_filename[:255],
        parent_asset_id=parent_asset_id,
        image_type_key=image_type_key,
    )
    session.add(asset)
    return asset


def create_product_image_asset(
    session: Session,
    *,
    product_id: str,
    content: bytes,
    filename: str,
    expected_mime_type: str | None,
    display_name: str | None = None,
    origin_type: ProductImageOriginType = ProductImageOriginType.UPLOAD,
    parent_asset_id: str | None = None,
    storage: LocalStorage | None = None,
) -> ProductImageAsset:
    product = session.get(Product, product_id)
    if product is None:
        raise NotFoundError("商品不存在")
    storage = storage or LocalStorage()
    with compensate_storage_writes(session) as storage_writes:
        asset = stage_product_image_asset(
            session,
            product=product,
            content=content,
            filename=filename,
            expected_mime_type=expected_mime_type,
            display_name=display_name,
            origin_type=origin_type,
            storage=storage,
            storage_writes=storage_writes,
            parent_asset_id=parent_asset_id,
        )
        product.updated_at = now_utc()
        session.commit()
    session.expire_all()
    return get_product_image_asset(session, asset.id)


def create_generated_product_image_asset(
    session: Session,
    *,
    product_id: str,
    content: bytes,
    filename: str,
    expected_mime_type: str,
    display_name: str,
    parent_asset_id: str | None = None,
    storage: LocalStorage | None = None,
) -> ProductImageAsset:
    return create_product_image_asset(
        session,
        product_id=product_id,
        content=content,
        filename=filename,
        expected_mime_type=expected_mime_type,
        display_name=display_name,
        origin_type=ProductImageOriginType.WORKFLOW_GENERATION,
        parent_asset_id=parent_asset_id,
        storage=storage,
    )


def _product_image_asset_query():
    return select(ProductImageAsset).options(selectinload(ProductImageAsset.media_object))


def get_product_image_asset(session: Session, asset_id: str) -> ProductImageAsset:
    asset = session.scalar(_product_image_asset_query().where(ProductImageAsset.id == asset_id))
    if asset is None:
        raise NotFoundError("商品图片不存在")
    return asset


def list_product_image_assets(session: Session, product_id: str) -> list[ProductImageAsset]:
    if session.get(Product, product_id) is None:
        raise NotFoundError("商品不存在")
    return list(
        session.scalars(
            _product_image_asset_query()
            .where(ProductImageAsset.product_id == product_id)
            .order_by(ProductImageAsset.created_at.asc(), ProductImageAsset.id.asc())
        ).all()
    )


def get_product_image_assets_by_ids(
    session: Session,
    *,
    product_id: str,
    asset_ids: list[str],
) -> list[ProductImageAsset]:
    if not asset_ids:
        return []
    assets = list(
        session.scalars(
            _product_image_asset_query().where(
                ProductImageAsset.product_id == product_id,
                ProductImageAsset.id.in_(asset_ids),
            )
        ).all()
    )
    assets_by_id = {asset.id: asset for asset in assets}
    if len(assets_by_id) != len(set(asset_ids)):
        raise NotFoundError("商品图片不存在")
    return [assets_by_id[asset_id] for asset_id in asset_ids]


def set_product_cover(session: Session, *, product_id: str, asset_id: str) -> Product:
    product = session.get(Product, product_id)
    if product is None:
        raise NotFoundError("商品不存在")
    asset = get_product_image_asset(session, asset_id)
    if asset.product_id != product_id:
        raise BusinessValidationError("封面图片不属于当前商品")
    if asset.media_object.verification_status == MediaVerificationStatus.MISSING:
        raise BusinessValidationError("缺失的媒体文件不能设为封面")
    product.cover_image_asset_id = asset.id
    product.updated_at = now_utc()
    session.commit()
    session.refresh(product)
    return product


def set_product_cover_if_empty(session: Session, *, product_id: str, asset_id: str) -> bool:
    product_exists = session.scalar(select(Product.id).where(Product.id == product_id).with_for_update())
    if product_exists is None:
        raise NotFoundError("商品不存在")
    asset = get_product_image_asset(session, asset_id)
    if asset.product_id != product_id:
        raise BusinessValidationError("封面图片不属于当前商品")
    if asset.media_object.verification_status == MediaVerificationStatus.MISSING:
        raise BusinessValidationError("缺失的媒体文件不能设为封面")
    result = session.execute(
        update(Product)
        .where(Product.id == product_id, Product.cover_image_asset_id.is_(None))
        .values(cover_image_asset_id=asset_id, updated_at=now_utc())
    )
    session.commit()
    return result.rowcount == 1


def clear_product_cover(session: Session, *, product_id: str) -> Product:
    product = session.get(Product, product_id)
    if product is None:
        raise NotFoundError("商品不存在")
    product.cover_image_asset_id = None
    product.updated_at = now_utc()
    session.commit()
    session.refresh(product)
    return product


def _media_has_references(session: Session, media_object_id: str) -> bool:
    return any(
        session.scalar(select(model.id).where(column == media_object_id).limit(1)) is not None
        for model, column in (
            (ProductImageAsset, ProductImageAsset.media_object_id),
            (ImageSessionAsset, ImageSessionAsset.media_object_id),
            (MediaLibraryAsset, MediaLibraryAsset.media_object_id),
        )
    )


def prune_unreferenced_media_objects(
    session: Session,
    media_object_ids: set[str],
) -> list[tuple[str, str]]:
    """在调用方事务内删除已无逻辑引用的媒体行，并返回待清理文件。"""
    if not media_object_ids:
        return []
    session.flush()
    deleted: list[tuple[str, str]] = []
    for media_id in sorted(media_object_ids):
        media = session.scalar(select(MediaObject).where(MediaObject.id == media_id).with_for_update())
        if media is None or _media_has_references(session, media_id):
            continue
        deleted.append((media.id, media.storage_path))
        session.delete(media)
    return deleted


def delete_product_image_asset(
    session: Session,
    *,
    asset_id: str,
    storage: LocalStorage | None = None,
) -> str:
    asset = get_product_image_asset(session, asset_id)
    ensure_product_image_asset_not_referenced(session, asset_id=asset_id)

    product_id = asset.product_id
    media = asset.media_object
    storage_path = media.storage_path
    session.delete(asset)
    session.flush()
    delete_media = not _media_has_references(session, media.id)
    if delete_media:
        session.delete(media)
    product = session.get(Product, product_id)
    if product is not None:
        product.updated_at = now_utc()
    session.commit()
    if delete_media:
        storage = storage or LocalStorage()
        best_effort_storage_delete(
            lambda: storage.delete_image_with_variants(storage_path),
            target=f"media_object_id={media.id} path={storage_path}",
        )
    return product_id


def ensure_product_image_asset_not_referenced(
    session: Session,
    *,
    asset_id: str,
) -> None:
    if session.scalar(select(Product.id).where(Product.cover_image_asset_id == asset_id).limit(1)) is not None:
        raise ConflictError("商品图片仍被设为封面，不能删除")
    if session.scalar(select(ProductImageAsset.id).where(ProductImageAsset.parent_asset_id == asset_id).limit(1)):
        raise ConflictError("商品图片仍有派生图片，不能删除")
    if session.scalar(select(WorkflowNode.id).where(WorkflowNode.bound_image_asset_id == asset_id).limit(1)):
        raise ConflictError("商品图片仍被工作流节点绑定，不能删除")
    if session.scalar(
        select(LegacyWorkflowArchiveAsset.id)
        .where(LegacyWorkflowArchiveAsset.product_image_asset_id == asset_id)
        .limit(1)
    ):
        raise ConflictError("商品图片仍被旧工作流归档引用，不能删除")
    if session.scalar(
        select(VisualSystemVersionReference.id)
        .where(VisualSystemVersionReference.asset_id == asset_id)
        .limit(1)
    ):
        raise ConflictError("商品图片仍被视觉体系版本引用，不能删除")
    if session.scalar(
        select(ImagePromptArtifactVersionReference.id)
        .where(ImagePromptArtifactVersionReference.asset_id == asset_id)
        .limit(1)
    ):
        raise ConflictError("商品图片仍被提示词版本作为证据引用，不能删除")
    if session.scalar(
        select(WorkflowImageGenerationRecord.id)
        .where(WorkflowImageGenerationRecord.result_asset_id == asset_id)
        .limit(1)
    ):
        raise ConflictError("商品图片仍被工作流生成历史作为结果引用，不能删除")
    if session.scalar(
        select(WorkflowImageGenerationReference.id)
        .where(WorkflowImageGenerationReference.asset_id == asset_id)
        .limit(1)
    ):
        raise ConflictError("商品图片仍被工作流生成历史作为参考图引用，不能删除")
    if session.scalar(
        select(DeliveryRenditionJob.id)
        .where(
            (DeliveryRenditionJob.source_asset_id == asset_id)
            | (DeliveryRenditionJob.result_asset_id == asset_id)
        )
        .limit(1)
    ):
        raise ConflictError("商品图片仍被交付派生任务引用，不能删除")
