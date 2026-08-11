from __future__ import annotations

from decimal import Decimal, InvalidOperation
from typing import Any, Literal

from sqlalchemy import desc, exists, func, literal, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.copy_payloads import validate_copy_payload
from productflow_backend.application.media_assets import (
    delete_legacy_source_with_canonical_asset,
    list_product_image_assets,
    prune_unreferenced_media_objects,
    stage_product_image_asset,
)
from productflow_backend.application.product_workflow.templates import (
    materialize_product_workflow_from_template,
    resolve_product_creation_canvas_template,
)
from productflow_backend.application.storage_compensation import (
    best_effort_storage_delete,
    compensate_storage_writes,
)
from productflow_backend.application.time import now_utc
from productflow_backend.domain.durable_generation_tasks import WORKFLOW_RUN_GENERATION_TASK_CONTRACT
from productflow_backend.domain.enums import (
    CopyStatus,
    ProductImageOriginType,
    ProductWorkflowState,
    SourceAssetKind,
    WorkflowNodeStatus,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    CopySet,
    MediaObject,
    PosterVariant,
    Product,
    ProductImageAsset,
    ProductWorkflow,
    SourceAsset,
    WorkflowNode,
    WorkflowRun,
)
from productflow_backend.infrastructure.storage import LocalStorage

ProductListSort = Literal["updated_desc", "created_desc", "name_asc"]
DEFAULT_PRODUCT_LIST_SORT: ProductListSort = "updated_desc"


def _normalize_required_text(value: str, *, field_name: str, max_length: int) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError(f"{field_name}不能为空")
    if len(normalized) > max_length:
        raise BusinessValidationError(f"{field_name}不能超过 {max_length} 个字符")
    return normalized


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
            selectinload(Product.source_assets),
            selectinload(Product.image_assets).selectinload(ProductImageAsset.media_object),
            selectinload(Product.cover_image_asset).selectinload(ProductImageAsset.media_object),
            selectinload(Product.creative_briefs),
            selectinload(Product.copy_sets),
            selectinload(Product.poster_variants),
            selectinload(Product.confirmed_copy_set),
            selectinload(Product.workflows).selectinload(ProductWorkflow.nodes),
            selectinload(Product.workflows).selectinload(ProductWorkflow.runs),
        )
        .order_by(desc(Product.updated_at))
    )


def _product_list_query():
    return select(Product).options(
        selectinload(Product.source_assets),
        selectinload(Product.image_assets).selectinload(ProductImageAsset.media_object),
        selectinload(Product.cover_image_asset).selectinload(ProductImageAsset.media_object),
        selectinload(Product.copy_sets),
        selectinload(Product.poster_variants),
        selectinload(Product.workflows).selectinload(ProductWorkflow.nodes),
        selectinload(Product.workflows).selectinload(ProductWorkflow.runs),
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


def _get_copy_set_or_raise(session: Session, copy_set_id: str) -> CopySet:
    stmt = select(CopySet).options(selectinload(CopySet.product)).where(CopySet.id == copy_set_id)
    copy_set = session.scalar(stmt)
    if copy_set is None:
        raise NotFoundError("文案不存在")
    return copy_set


def derive_product_state(product: Product) -> ProductWorkflowState:
    """从商品关联数据推导流程状态，用于列表过滤。"""
    if product.poster_variants:
        return ProductWorkflowState.POSTER_READY
    if _product_has_failed_workflow(product):
        return ProductWorkflowState.FAILED
    if product.current_confirmed_copy_set_id:
        return ProductWorkflowState.COPY_READY
    return ProductWorkflowState.DRAFT


def _product_has_failed_workflow(product: Product) -> bool:
    return any(
        workflow.active
        and (
            any(run.status == WorkflowRunStatus.FAILED for run in workflow.runs)
            or any(node.status == WorkflowNodeStatus.FAILED for node in workflow.nodes)
        )
        for workflow in product.workflows
    )


def _product_failed_workflow_exists():
    has_failed_run = exists(
        select(literal(1))
        .select_from(WorkflowRun)
        .join(ProductWorkflow, WorkflowRun.workflow_id == ProductWorkflow.id)
        .where(
            ProductWorkflow.product_id == Product.id,
            ProductWorkflow.active.is_(True),
            WorkflowRun.status == WorkflowRunStatus.FAILED,
        )
    ).correlate(Product)
    has_failed_node = exists(
        select(literal(1))
        .select_from(WorkflowNode)
        .join(ProductWorkflow, WorkflowNode.workflow_id == ProductWorkflow.id)
        .where(
            ProductWorkflow.product_id == Product.id,
            ProductWorkflow.active.is_(True),
            WorkflowNode.status == WorkflowNodeStatus.FAILED,
        )
    ).correlate(Product)
    return has_failed_run | has_failed_node


def _product_status_filter(status: ProductWorkflowState):
    has_poster = exists(
        select(literal(1)).select_from(PosterVariant).where(PosterVariant.product_id == Product.id)
    ).correlate(Product)
    has_failed = _product_failed_workflow_exists()
    if status == ProductWorkflowState.POSTER_READY:
        return has_poster
    if status == ProductWorkflowState.FAILED:
        return has_failed & ~has_poster
    if status == ProductWorkflowState.COPY_READY:
        return Product.current_confirmed_copy_set_id.is_not(None) & ~has_poster & ~has_failed
    if status == ProductWorkflowState.DRAFT:
        return Product.current_confirmed_copy_set_id.is_(None) & ~has_poster & ~has_failed
    return literal(False)


def create_product(
    session: Session,
    *,
    name: str,
    category: str | None,
    price: str | None,
    source_note: str | None,
    image_bytes: bytes,
    filename: str,
    content_type: str,
    reference_image_uploads: list[tuple[bytes, str, str]] | None = None,
    canvas_template_key: str | None = None,
    template_language: str | None = None,
    storage: LocalStorage | None = None,
) -> Product:
    """创建商品，保存原始图和参考图到本地存储。"""
    canvas_template = resolve_product_creation_canvas_template(canvas_template_key)
    storage = storage or LocalStorage()
    with compensate_storage_writes(session) as storage_writes:
        product = Product(
            name=_normalize_required_text(name, field_name="商品名", max_length=255),
            category=_normalize_optional_text(category, field_name="类目", max_length=120),
            price=_normalize_price(price),
            source_note=_normalize_optional_text(source_note, field_name="备注", max_length=4000),
        )
        session.add(product)
        session.flush()

        relative_path = storage_writes.track(
            storage,
            storage.save_product_upload(product.id, filename, image_bytes),
        )
        session.add(
            SourceAsset(
                product_id=product.id,
                kind=SourceAssetKind.ORIGINAL_IMAGE,
                original_filename=filename,
                mime_type=content_type or "application/octet-stream",
                storage_path=relative_path,
            )
        )
        for reference_bytes, reference_filename, reference_content_type in reference_image_uploads or []:
            reference_path = storage_writes.track(
                storage,
                storage.save_reference_upload(product.id, reference_filename, reference_bytes),
            )
            session.add(
                SourceAsset(
                    product_id=product.id,
                    kind=SourceAssetKind.REFERENCE_IMAGE,
                    original_filename=reference_filename,
                    mime_type=reference_content_type or "application/octet-stream",
                    storage_path=reference_path,
                )
            )
        if canvas_template is not None:
            materialize_product_workflow_from_template(
                session,
                product_id=product.id,
                template=canvas_template,
                template_language=template_language,
            )
        session.commit()
    session.expire_all()
    return _get_product_or_raise(session, product.id)


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
    if not image_uploads:
        raise BusinessValidationError("至少上传一张商品参考图")
    if len(image_uploads) > 6:
        raise BusinessValidationError("商品参考图最多上传 6 张")
    storage = storage or LocalStorage()
    with compensate_storage_writes(session) as storage_writes:
        product = Product(
            name=_normalize_required_text(name, field_name="商品名", max_length=255),
            category=_normalize_optional_text(category, field_name="类目", max_length=120),
            price=_normalize_price(price),
            source_note=_normalize_optional_text(source_note, field_name="备注", max_length=4000),
        )
        session.add(product)
        session.flush()
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
        product.cover_image_asset_id = image_assets[0].id
        session.commit()
    session.expire_all()
    return _get_product_or_raise(session, product.id)


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
    assets_by_id = {asset.id: asset for asset in list_product_image_assets(session, product_id)}
    return [assets_by_id[asset_id] for asset_id in asset_ids]


def add_reference_images(
    session: Session,
    *,
    product_id: str,
    reference_image_uploads: list[tuple[bytes, str, str]],
    storage: LocalStorage | None = None,
) -> Product:
    product = _get_product_or_raise(session, product_id)
    storage = storage or LocalStorage()
    with compensate_storage_writes(session) as storage_writes:
        for reference_bytes, reference_filename, reference_content_type in reference_image_uploads:
            reference_path = storage_writes.track(
                storage,
                storage.save_reference_upload(product.id, reference_filename, reference_bytes),
            )
            session.add(
                SourceAsset(
                    product_id=product.id,
                    kind=SourceAssetKind.REFERENCE_IMAGE,
                    original_filename=reference_filename,
                    mime_type=reference_content_type or "application/octet-stream",
                    storage_path=reference_path,
                )
            )
        session.commit()
    session.expire_all()
    return _get_product_or_raise(session, product.id)


def delete_reference_image(
    session: Session,
    *,
    asset_id: str,
    storage: LocalStorage | None = None,
) -> Product:
    asset = session.get(SourceAsset, asset_id)
    if asset is None:
        raise NotFoundError("商品参考图不存在")
    if asset.kind != SourceAssetKind.REFERENCE_IMAGE:
        raise BusinessValidationError("只能删除商品参考图")

    product_id = asset.product_id
    storage_path = asset.storage_path
    storage = storage or LocalStorage()
    if delete_legacy_source_with_canonical_asset(session, source_asset=asset, storage=storage):
        session.expire_all()
        return _get_product_or_raise(session, product_id)
    product = _get_product_or_raise(session, product_id)
    product.updated_at = now_utc()
    session.delete(asset)
    session.commit()
    best_effort_storage_delete(
        lambda: storage.delete_image_with_variants(storage_path),
        target=f"source_asset_id={asset_id} path={storage_path}",
    )
    session.expire_all()
    return _get_product_or_raise(session, product_id)


def list_products(
    session: Session,
    *,
    status: ProductWorkflowState | None,
    page: int,
    page_size: int,
    q: str | None = None,
    sort: ProductListSort = DEFAULT_PRODUCT_LIST_SORT,
) -> tuple[list[Product], int]:
    page = max(page, 1)
    page_size = min(max(page_size, 1), 100)
    start = (page - 1) * page_size
    filters = []
    if status is not None:
        filters.append(_product_status_filter(status))
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
    session.expire(product, ["image_assets", "source_assets", "poster_variants"])
    media_ids = {asset.media_object_id for asset in product.image_assets}
    legacy_paths = {
        *(asset.storage_path for asset in product.source_assets),
        *(poster.storage_path for poster in product.poster_variants),
    }
    session.delete(product)
    session.flush()
    deleted_media = prune_unreferenced_media_objects(session, media_ids)
    retained_legacy_paths = set(
        session.scalars(select(MediaObject.storage_path).where(MediaObject.storage_path.in_(legacy_paths))).all()
    )
    session.commit()
    cleanup_paths = {storage_path for _, storage_path in deleted_media}
    cleanup_paths.update(legacy_paths - retained_legacy_paths)
    for storage_path in sorted(cleanup_paths):
        best_effort_storage_delete(
            lambda path=storage_path: storage.delete_image_with_variants(path),
            target=f"product_id={product_id} path={storage_path}",
        )
    best_effort_storage_delete(
        lambda: storage.remove_empty_product_directories(product_id),
        target=f"product_id={product_id} empty_directories",
    )


def update_copy_set(
    session: Session,
    *,
    copy_set_id: str,
    structured_payload: dict[str, Any],
) -> CopySet:
    copy_set = _get_copy_set_or_raise(session, copy_set_id)
    try:
        payload = validate_copy_payload(structured_payload)
    except ValueError as exc:
        raise BusinessValidationError("文案 payload 不符合 CopyPayloadV2 合同") from exc
    copy_set.structured_payload = payload.model_dump(mode="json")
    copy_set.edited_at = now_utc()
    session.commit()
    session.refresh(copy_set)
    return copy_set


def confirm_copy_set(session: Session, *, copy_set_id: str) -> CopySet:
    copy_set = _get_copy_set_or_raise(session, copy_set_id)
    product = _get_product_or_raise(session, copy_set.product_id)
    copy_set.status = CopyStatus.CONFIRMED
    copy_set.confirmed_at = now_utc()
    product.current_confirmed_copy_set_id = copy_set.id
    session.commit()
    session.refresh(copy_set)
    return copy_set


def get_product_history(session: Session, product_id: str) -> dict[str, Any]:
    product = _get_product_or_raise(session, product_id)
    return {
        "copy_sets": sorted(product.copy_sets, key=lambda item: item.created_at, reverse=True),
        "poster_variants": sorted(product.poster_variants, key=lambda item: item.created_at, reverse=True),
    }
