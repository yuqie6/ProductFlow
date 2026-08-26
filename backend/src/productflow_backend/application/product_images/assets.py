"""商品图片身份的创建、封面展示绑定与删除守卫。

上传、工作流结果、会话附加和交付派生都进入 ProductImageAsset；成功图不自动标 reject/draft。
封面只改展示元数据。删除前必须确认节点、封面、lineage、交付和保真检查不再引用该 id。
"""

from __future__ import annotations

from sqlalchemy import select, update
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.media_objects import (
    prune_unreferenced_media_objects,
    stage_verified_media_object,
)
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
    LegacyWorkflowArchiveAsset,
    LocalImageEditProviderAttempt,
    LocalImageEditTask,
    LocalImageEditTaskReference,
    MediaObject,
    Product,
    ProductImageAsset,
    ProductImageFidelityCheck,
    VisualSystemVersionReference,
    WorkflowGraphArtifact,
    WorkflowGraphNode,
)
from productflow_backend.infrastructure.storage import LocalStorage


def _validate_product_image_parent(
    session: Session,
    *,
    product: Product,
    parent_asset_id: str | None,
) -> None:
    if parent_asset_id is None:
        return
    parent_asset = session.get(ProductImageAsset, parent_asset_id)
    if parent_asset is None or parent_asset.product_id != product.id:
        raise BusinessValidationError("父图片资产不属于当前商品")


def stage_product_image_identity(
    session: Session,
    *,
    product: Product,
    media_object: MediaObject,
    origin_type: ProductImageOriginType,
    display_name: str | None,
    original_filename: str,
    parent_asset_id: str | None = None,
    image_type_key: str | None = None,
    source_library_asset_id: str | None = None,
    source_image_session_asset_id: str | None = None,
) -> ProductImageAsset:
    """从已核验的 MediaObject 暂存商品图片身份，不写入媒体 bytes。"""

    _validate_product_image_parent(session, product=product, parent_asset_id=parent_asset_id)
    normalized_filename = original_filename.strip() or "image"
    normalized_display_name = (display_name or normalized_filename).strip() or normalized_filename
    asset = ProductImageAsset(
        product_id=product.id,
        media_object=media_object,
        origin_type=origin_type,
        display_name=normalized_display_name[:255],
        original_filename=normalized_filename[:255],
        parent_asset_id=parent_asset_id,
        image_type_key=image_type_key,
        source_library_asset_id=source_library_asset_id,
        source_image_session_asset_id=source_image_session_asset_id,
    )
    session.add(asset)
    return asset


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
    """核验写入 MediaObject，并在商品命名空间创建一张逻辑图片。"""

    _validate_product_image_parent(session, product=product, parent_asset_id=parent_asset_id)
    media = stage_verified_media_object(
        session,
        content=content,
        filename=filename,
        expected_mime_type=expected_mime_type,
        storage=storage,
        storage_writes=storage_writes,
    )
    return stage_product_image_identity(
        session,
        product=product,
        media_object=media,
        origin_type=origin_type,
        display_name=display_name,
        original_filename=filename,
        parent_asset_id=parent_asset_id,
        image_type_key=image_type_key,
    )


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
    """提交一张商品图片；DB 失败时补偿本次存储写入。"""

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
    """工作流生成结果进入商品库；不自动标 reject/draft。"""

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
    """按商品图片身份读取；路径不作为查找键。"""

    asset = session.scalar(_product_image_asset_query().where(ProductImageAsset.id == asset_id))
    if asset is None:
        raise NotFoundError("商品图片不存在")
    return asset


def list_product_image_assets(session: Session, product_id: str) -> list[ProductImageAsset]:
    """列出该商品当前与历史图片，不含自动 reject/draft 过滤。"""

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
    """把封面指向一张商品图片；不改商品事实或工作流参考绑定。"""

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
    """仅在尚无封面时写入展示图，仍不改事实或参考绑定。"""

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
    """清除展示封面，不删除图片资产或工作流绑定。"""

    product = session.get(Product, product_id)
    if product is None:
        raise NotFoundError("商品不存在")
    product.cover_image_asset_id = None
    product.updated_at = now_utc()
    session.commit()
    session.refresh(product)
    return product


def delete_product_image_asset(
    session: Session,
    *,
    asset_id: str,
    storage: LocalStorage | None = None,
) -> str:
    """删除商品图片身份；commit 后才清理已无引用的 MediaObject 文件。"""

    asset = get_product_image_asset(session, asset_id)
    ensure_product_image_asset_not_referenced(session, asset_id=asset_id)

    product_id = asset.product_id
    media_id = asset.media_object_id
    storage_path = asset.media_object.storage_path
    session.delete(asset)
    deleted_media = prune_unreferenced_media_objects(session, {media_id})
    product = session.get(Product, product_id)
    if product is not None:
        product.updated_at = now_utc()
    session.commit()
    # 业务行已提交；共享媒体不作为通用回滚删除。
    if deleted_media:
        storage = storage or LocalStorage()
        best_effort_storage_delete(
            lambda: storage.delete_image_with_variants(storage_path),
            target=f"media_object_id={media_id} path={storage_path}",
        )
    return product_id


def ensure_product_image_asset_not_referenced(
    session: Session,
    *,
    asset_id: str,
) -> None:
    """封面、节点绑定、生成历史、交付和保真检查仍持有该 id 时拒绝删除。

    用户文件夹不是引用：删文件夹只清空 user_folder_id。
    """

    if session.scalar(select(Product.id).where(Product.cover_image_asset_id == asset_id).limit(1)) is not None:
        raise ConflictError("商品图片仍被设为封面，不能删除")
    if session.scalar(select(ProductImageAsset.id).where(ProductImageAsset.parent_asset_id == asset_id).limit(1)):
        raise ConflictError("商品图片仍有派生图片，不能删除")
    if session.scalar(
        select(WorkflowGraphNode.id).where(WorkflowGraphNode.bound_image_asset_id == asset_id).limit(1)
    ):
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
        select(WorkflowGraphArtifact.id)
        .where(WorkflowGraphArtifact.product_image_asset_id == asset_id)
        .limit(1)
    ):
        raise ConflictError("商品图片仍被工作流生成历史作为结果引用，不能删除")
    if session.scalar(
        select(DeliveryRenditionJob.id)
        .where(
            (DeliveryRenditionJob.source_asset_id == asset_id)
            | (DeliveryRenditionJob.result_asset_id == asset_id)
        )
        .limit(1)
    ):
        raise ConflictError("商品图片仍被交付派生任务引用，不能删除")
    if session.scalar(
        select(LocalImageEditTask.id)
        .where(
            (LocalImageEditTask.source_asset_id == asset_id)
            | (LocalImageEditTask.source_artifact_asset_id == asset_id)
            | (LocalImageEditTask.result_asset_id == asset_id)
        )
        .limit(1)
    ):
        raise ConflictError("商品图片仍被局部编辑任务的源图或结果引用，不能删除")
    if session.scalar(
        select(LocalImageEditTaskReference.task_id)
        .where(LocalImageEditTaskReference.asset_id == asset_id)
        .limit(1)
    ):
        raise ConflictError("商品图片仍被局部编辑任务作为参考图引用，不能删除")
    if session.scalar(
        select(LocalImageEditProviderAttempt.id)
        .where(LocalImageEditProviderAttempt.late_result_asset_id == asset_id)
        .limit(1)
    ):
        raise ConflictError("商品图片仍被局部编辑迟到结果审计引用，不能删除")
    if session.scalar(
        select(ProductImageFidelityCheck.id)
        .where(ProductImageFidelityCheck.asset_id == asset_id)
        .limit(1)
    ):
        raise ConflictError("商品图片仍有人工保真检查历史，不能删除")
