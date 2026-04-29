from __future__ import annotations

from decimal import Decimal, InvalidOperation
from typing import Any

from sqlalchemy import desc, func, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import (
    CopyStatus,
    ProductWorkflowState,
    SourceAssetKind,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import NotFoundError
from productflow_backend.infrastructure.db.models import (
    CopySet,
    Product,
    ProductWorkflow,
    SourceAsset,
    WorkflowRun,
)
from productflow_backend.infrastructure.storage import LocalStorage


def _normalize_required_text(value: str, *, field_name: str, max_length: int) -> str:
    normalized = value.strip()
    if not normalized:
        raise ValueError(f"{field_name}不能为空")
    if len(normalized) > max_length:
        raise ValueError(f"{field_name}不能超过 {max_length} 个字符")
    return normalized


def _normalize_optional_text(value: str | None, *, field_name: str, max_length: int) -> str | None:
    if value is None:
        return None
    normalized = value.strip()
    if not normalized:
        return None
    if len(normalized) > max_length:
        raise ValueError(f"{field_name}不能超过 {max_length} 个字符")
    return normalized


def _normalize_price(value: str | None) -> Decimal | None:
    if value is None or not value.strip():
        return None
    try:
        price = Decimal(value.strip())
    except InvalidOperation as exc:
        raise ValueError("价格格式不正确") from exc
    if not price.is_finite() or price < 0:
        raise ValueError("价格必须是非负数字")
    if abs(price.as_tuple().exponent) > 2:
        raise ValueError("价格最多保留两位小数")
    return price


def _product_query():
    return (
        select(Product)
        .options(
            selectinload(Product.source_assets),
            selectinload(Product.creative_briefs),
            selectinload(Product.copy_sets),
            selectinload(Product.poster_variants),
            selectinload(Product.confirmed_copy_set),
        )
        .order_by(desc(Product.updated_at))
    )


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
    if product.current_confirmed_copy_set_id:
        return ProductWorkflowState.COPY_READY
    return ProductWorkflowState.DRAFT


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
    storage: LocalStorage | None = None,
) -> Product:
    """创建商品，保存原始图和参考图到本地存储。"""
    storage = storage or LocalStorage()
    product = Product(
        name=_normalize_required_text(name, field_name="商品名", max_length=255),
        category=_normalize_optional_text(category, field_name="类目", max_length=120),
        price=_normalize_price(price),
        source_note=_normalize_optional_text(source_note, field_name="备注", max_length=4000),
    )
    session.add(product)
    session.flush()

    relative_path = storage.save_product_upload(product.id, filename, image_bytes)
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
        reference_path = storage.save_reference_upload(product.id, reference_filename, reference_bytes)
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


def add_reference_images(
    session: Session,
    *,
    product_id: str,
    reference_image_uploads: list[tuple[bytes, str, str]],
    storage: LocalStorage | None = None,
) -> Product:
    product = _get_product_or_raise(session, product_id)
    storage = storage or LocalStorage()
    for reference_bytes, reference_filename, reference_content_type in reference_image_uploads:
        reference_path = storage.save_reference_upload(product.id, reference_filename, reference_bytes)
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
        raise ValueError("只能删除商品参考图")

    product_id = asset.product_id
    storage_path = asset.storage_path
    storage = storage or LocalStorage()
    product = _get_product_or_raise(session, product_id)
    product.updated_at = now_utc()
    session.delete(asset)
    session.commit()
    storage.delete_image_with_variants(storage_path)
    session.expire_all()
    return _get_product_or_raise(session, product_id)


def list_products(
    session: Session,
    *,
    status: ProductWorkflowState | None,
    page: int,
    page_size: int,
) -> tuple[list[Product], int]:
    page = max(page, 1)
    page_size = min(max(page_size, 1), 100)
    start = (page - 1) * page_size
    if status is None:
        total = session.scalar(select(func.count()).select_from(Product)) or 0
        products = session.scalars(_product_query().offset(start).limit(page_size)).all()
        return list(products), total

    products = session.scalars(_product_query()).all()
    products = [item for item in products if derive_product_state(item) == status]
    total = len(products)

    end = start + page_size
    return products[start:end], total


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
        .where(ProductWorkflow.product_id == product_id, WorkflowRun.status == WorkflowRunStatus.RUNNING)
    )
    if active_workflow_run is not None:
        raise ValueError("商品工作流运行中，稍后删除")
    storage = storage or LocalStorage()
    session.delete(product)
    session.commit()
    storage.delete_product_tree(product_id)


def update_copy_set(
    session: Session,
    *,
    copy_set_id: str,
    title: str | None,
    selling_points: list[str] | None,
    poster_headline: str | None,
    cta: str | None,
) -> CopySet:
    copy_set = _get_copy_set_or_raise(session, copy_set_id)
    if title is not None:
        copy_set.title = _normalize_required_text(title, field_name="标题", max_length=500)
    if selling_points is not None:
        copy_set.selling_points = selling_points
    if poster_headline is not None:
        copy_set.poster_headline = _normalize_required_text(poster_headline, field_name="海报标题", max_length=500)
    if cta is not None:
        copy_set.cta = _normalize_required_text(cta, field_name="CTA", max_length=300)
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
