from __future__ import annotations

from dataclasses import dataclass
from decimal import Decimal, InvalidOperation
from typing import Literal

from sqlalchemy import delete, desc, func, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.media_objects import (
    prune_unreferenced_media_objects,
)
from productflow_backend.application.product_images.assets import (
    get_product_image_assets_by_ids,
    stage_product_image_asset,
)
from productflow_backend.application.storage_compensation import (
    StorageWriteCompensation,
    best_effort_storage_delete,
    compensate_storage_writes,
)
from productflow_backend.application.time import now_utc
from productflow_backend.domain.durable_generation_tasks import WORKFLOW_RUN_GENERATION_TASK_CONTRACT
from productflow_backend.domain.enums import ProductImageOriginType
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    Product,
    ProductImageAsset,
    ProductWorkflow,
    VisualSystem,
    VisualSystemVersion,
    VisualSystemVersionReference,
    WorkflowDraft,
    WorkflowDraftRevision,
    WorkflowImageGenerationRecord,
    WorkflowRecipeVersion,
    WorkflowRun,
)
from productflow_backend.infrastructure.storage import LocalStorage

ProductListSort = Literal["updated_desc", "created_desc", "name_asc"]
DEFAULT_PRODUCT_LIST_SORT: ProductListSort = "updated_desc"


@dataclass(frozen=True, slots=True)
class CanonicalProductCreation:
    product: Product
    created_assets: list[ProductImageAsset]


def _normalize_required_text(value: str, *, field_name: str, max_length: int) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError(f"{field_name}不能为空")
    if len(normalized) > max_length:
        raise BusinessValidationError(f"{field_name}不能超过 {max_length} 个字符")
    return normalized


def normalize_product_name(value: str) -> str:
    """Normalize a product name for canonical creation and idempotency hashing."""
    return _normalize_required_text(value, field_name="商品名", max_length=255)


def _normalize_optional_text(value: str | None, *, field_name: str, max_length: int) -> str | None:
    if value is None:
        return None
    normalized = value.strip()
    if not normalized:
        return None
    if len(normalized) > max_length:
        raise BusinessValidationError(f"{field_name}不能超过 {max_length} 个字符")
    return normalized


def _normalize_price(value: str | None) -> Decimal | None:
    if value is None or not value.strip():
        return None
    try:
        price = Decimal(value.strip())
    except InvalidOperation as exc:
        raise BusinessValidationError("价格格式不正确") from exc
    if not price.is_finite() or price < 0:
        raise BusinessValidationError("价格必须是非负数字")
    if abs(price.as_tuple().exponent) > 2:
        raise BusinessValidationError("价格最多保留两位小数")
    return price


def _product_query():
    return (
        select(Product)
        .options(
            selectinload(Product.image_assets).selectinload(ProductImageAsset.media_object),
            selectinload(Product.cover_image_asset).selectinload(ProductImageAsset.media_object),
        )
        .order_by(desc(Product.updated_at))
    )


def _product_list_query():
    return select(Product).options(
        selectinload(Product.cover_image_asset).selectinload(ProductImageAsset.media_object),
    )


def _product_sort_order(sort: ProductListSort):
    if sort == "created_desc":
        return Product.created_at.desc(), Product.id.desc()
    if sort == "name_asc":
        return func.lower(Product.name).asc(), Product.name.asc(), Product.id.asc()
    return Product.updated_at.desc(), Product.id.desc()


def _get_product_or_raise(session: Session, product_id: str) -> Product:
    product = session.scalar(_product_query().where(Product.id == product_id))
    if product is None:
        raise NotFoundError("商品不存在")
    return product


def create_canonical_product(
    session: Session,
    *,
    name: str,
    category: str | None,
    price: str | None,
    source_note: str | None,
    image_uploads: list[tuple[bytes, str, str]],
    storage: LocalStorage | None = None,
) -> Product:
    """创建只使用 MediaObject/ProductImageAsset 的 v2 商品。"""
    return create_canonical_product_with_assets(
        session,
        name=name,
        category=category,
        price=price,
        source_note=source_note,
        image_uploads=image_uploads,
        storage=storage,
    ).product


def create_canonical_product_with_assets(
    session: Session,
    *,
    name: str,
    category: str | None,
    price: str | None,
    source_note: str | None,
    image_uploads: list[tuple[bytes, str, str]],
    storage: LocalStorage | None = None,
) -> CanonicalProductCreation:
    """创建 canonical 商品并仅返回本次创建的有界图片集合。"""
    storage = storage or LocalStorage()
    with compensate_storage_writes(session) as storage_writes:
        creation = stage_canonical_product_with_assets(
            session,
            name=name,
            category=category,
            price=price,
            source_note=source_note,
            image_uploads=image_uploads,
            storage=storage,
            storage_writes=storage_writes,
        )
        creation.product.cover_image_asset_id = creation.created_assets[0].id
        product_id = creation.product.id
        asset_ids = [asset.id for asset in creation.created_assets]
        session.commit()
    session.expire_all()
    return CanonicalProductCreation(
        product=_get_product_or_raise(session, product_id),
        created_assets=get_product_image_assets_by_ids(
            session,
            product_id=product_id,
            asset_ids=asset_ids,
        ),
    )


def stage_canonical_product_with_assets(
    session: Session,
    *,
    name: str,
    category: str | None,
    price: str | None,
    source_note: str | None,
    image_uploads: list[tuple[bytes, str, str]],
    storage: LocalStorage,
    storage_writes: StorageWriteCompensation,
) -> CanonicalProductCreation:
    """Stage one canonical Product and its verified uploads without committing."""
    product = stage_canonical_product(
        session,
        name=name,
        category=category,
        price=price,
        source_note=source_note,
    )
    image_assets = stage_canonical_product_assets(
        session,
        product=product,
        image_uploads=image_uploads,
        storage=storage,
        storage_writes=storage_writes,
    )
    return CanonicalProductCreation(product=product, created_assets=image_assets)


def stage_canonical_product(
    session: Session,
    *,
    name: str,
    category: str | None,
    price: str | None,
    source_note: str | None,
) -> Product:
    """Stage canonical product identity without committing or creating media."""
    product = Product(
        name=normalize_product_name(name),
        category=_normalize_optional_text(category, field_name="类目", max_length=120),
        price=_normalize_price(price),
        source_note=_normalize_optional_text(source_note, field_name="备注", max_length=4000),
    )
    session.add(product)
    session.flush()
    return product


def stage_canonical_product_assets(
    session: Session,
    *,
    product: Product,
    image_uploads: list[tuple[bytes, str, str]],
    storage: LocalStorage,
    storage_writes: StorageWriteCompensation,
) -> list[ProductImageAsset]:
    """Stage one bounded set of equal-reference canonical assets without committing."""
    if not image_uploads:
        raise BusinessValidationError("至少上传一张商品参考图")
    if len(image_uploads) > 6:
        raise BusinessValidationError("商品参考图最多上传 6 张")
    image_assets = [
        stage_product_image_asset(
            session,
            product=product,
            content=image_bytes,
            filename=filename,
            expected_mime_type=mime_type,
            display_name=filename,
            origin_type=ProductImageOriginType.UPLOAD,
            storage=storage,
            storage_writes=storage_writes,
        )
        for image_bytes, filename, mime_type in image_uploads
    ]
    session.flush()
    return image_assets


def add_canonical_product_images(
    session: Session,
    *,
    product_id: str,
    image_uploads: list[tuple[bytes, str, str]],
    storage: LocalStorage | None = None,
) -> list[ProductImageAsset]:
    if not image_uploads:
        raise BusinessValidationError("至少上传一张商品图片")
    if len(image_uploads) > 6:
        raise BusinessValidationError("单次最多上传 6 张商品图片")
    product = _get_product_or_raise(session, product_id)
    storage = storage or LocalStorage()
    with compensate_storage_writes(session) as storage_writes:
        assets = [
            stage_product_image_asset(
                session,
                product=product,
                content=image_bytes,
                filename=filename,
                expected_mime_type=mime_type,
                display_name=filename,
                origin_type=ProductImageOriginType.UPLOAD,
                storage=storage,
                storage_writes=storage_writes,
            )
            for image_bytes, filename, mime_type in image_uploads
        ]
        session.flush()
        if product.cover_image_asset_id is None:
            product.cover_image_asset_id = assets[0].id
        product.updated_at = now_utc()
        asset_ids = [asset.id for asset in assets]
        session.commit()
    session.expire_all()
    return get_product_image_assets_by_ids(
        session,
        product_id=product_id,
        asset_ids=asset_ids,
    )


def list_products(
    session: Session,
    *,
    page: int,
    page_size: int,
    q: str | None = None,
    sort: ProductListSort = DEFAULT_PRODUCT_LIST_SORT,
) -> tuple[list[Product], int]:
    page = max(page, 1)
    page_size = min(max(page_size, 1), 100)
    start = (page - 1) * page_size
    filters = []
    normalized_q = q.strip() if q else ""
    if normalized_q:
        filters.append(Product.name.icontains(normalized_q, autoescape=True))

    count_query = select(func.count()).select_from(Product)
    product_query = _product_list_query().order_by(*_product_sort_order(sort))
    if filters:
        count_query = count_query.where(*filters)
        product_query = product_query.where(*filters)

    total = session.scalar(count_query) or 0
    products = session.scalars(product_query.offset(start).limit(page_size)).all()
    return list(products), total


def get_product_detail(session: Session, product_id: str) -> Product:
    return _get_product_or_raise(session, product_id)


def delete_product(
    session: Session,
    *,
    product_id: str,
    storage: LocalStorage | None = None,
) -> None:
    product = _get_product_or_raise(session, product_id)
    active_workflow_run = session.scalar(
        select(WorkflowRun)
        .join(ProductWorkflow, WorkflowRun.workflow_id == ProductWorkflow.id)
        .where(
            ProductWorkflow.product_id == product_id,
            WorkflowRun.status.in_(WORKFLOW_RUN_GENERATION_TASK_CONTRACT.active_statuses),
        )
    )
    if active_workflow_run is not None:
        raise BusinessValidationError("商品工作流运行中，稍后删除")
    storage = storage or LocalStorage()
    media_ids = set(
        session.scalars(
            select(ProductImageAsset.media_object_id).where(ProductImageAsset.product_id == product_id)
        )
    )
    removable_visual_versions = _prepare_visual_system_cleanup_for_product(
        session,
        product_id=product_id,
    )
    session.delete(product)
    session.flush()
    _delete_owned_visual_system_versions(session, removable_visual_versions)
    deleted_media = prune_unreferenced_media_objects(session, media_ids)
    session.commit()
    cleanup_paths = {storage_path for _, storage_path in deleted_media}
    for storage_path in sorted(cleanup_paths):
        best_effort_storage_delete(
            lambda path=storage_path: storage.delete_image_with_variants(path),
            target=f"product_id={product_id} path={storage_path}",
        )
    best_effort_storage_delete(
        lambda: storage.remove_empty_product_directories(product_id),
        target=f"product_id={product_id} empty_directories",
    )


def _prepare_visual_system_cleanup_for_product(
    session: Session,
    *,
    product_id: str,
) -> list[tuple[str, str]]:
    source_versions = list(
        session.scalars(
            select(VisualSystemVersion)
            .join(
                WorkflowDraftRevision,
                WorkflowDraftRevision.id == VisualSystemVersion.source_draft_revision_id,
            )
            .join(WorkflowDraft, WorkflowDraft.id == WorkflowDraftRevision.draft_id)
            .options(selectinload(VisualSystemVersion.references))
            .where(WorkflowDraft.product_id == product_id)
        )
    )
    source_version_ids = {version.id for version in source_versions}
    referenced_version_ids = set(
        session.scalars(
            select(VisualSystemVersionReference.visual_system_version_id)
            .join(ProductImageAsset, ProductImageAsset.id == VisualSystemVersionReference.asset_id)
            .where(ProductImageAsset.product_id == product_id)
        )
    )
    if referenced_version_ids - source_version_ids:
        raise ConflictError("商品图片仍被其他视觉体系版本引用，不能删除商品")

    removable: list[tuple[str, str]] = []
    for version in source_versions:
        has_external_consumer = any(
            (
                session.scalar(
                    select(ProductWorkflow.id)
                    .where(
                        ProductWorkflow.visual_system_version_id == version.id,
                        ProductWorkflow.product_id != product_id,
                    )
                    .limit(1)
                ),
                session.scalar(
                    select(WorkflowDraftRevision.id)
                    .join(WorkflowDraft, WorkflowDraft.id == WorkflowDraftRevision.draft_id)
                    .where(
                        WorkflowDraftRevision.visual_system_version_id == version.id,
                        WorkflowDraft.product_id != product_id,
                    )
                    .limit(1)
                ),
                session.scalar(
                    select(WorkflowImageGenerationRecord.id)
                    .where(
                        WorkflowImageGenerationRecord.visual_system_version_id == version.id,
                        WorkflowImageGenerationRecord.product_id != product_id,
                    )
                    .limit(1)
                ),
                session.scalar(
                    select(WorkflowRecipeVersion.id)
                    .where(WorkflowRecipeVersion.preferred_visual_system_version_id == version.id)
                    .limit(1)
                ),
            )
        )
        if has_external_consumer:
            if version.id in referenced_version_ids:
                raise ConflictError("商品视觉体系仍被其他商品使用，不能删除其参考图片")
            continue
        removable.append((version.id, version.visual_system_id))
    removable_version_ids = {version_id for version_id, _ in removable}
    for version in source_versions:
        if version.id not in removable_version_ids:
            continue
        for reference in version.references:
            session.delete(reference)
    session.flush()
    return removable


def _delete_owned_visual_system_versions(
    session: Session,
    versions: list[tuple[str, str]],
) -> None:
    if not versions:
        return
    version_ids = {version_id for version_id, _ in versions}
    visual_system_ids = {visual_system_id for _, visual_system_id in versions}
    session.execute(delete(VisualSystemVersion).where(VisualSystemVersion.id.in_(version_ids)))
    for visual_system_id in visual_system_ids:
        remaining_version_id = session.scalar(
            select(VisualSystemVersion.id)
            .where(VisualSystemVersion.visual_system_id == visual_system_id)
            .limit(1)
        )
        if remaining_version_id is None:
            session.execute(delete(VisualSystem).where(VisualSystem.id == visual_system_id))
    session.flush()
