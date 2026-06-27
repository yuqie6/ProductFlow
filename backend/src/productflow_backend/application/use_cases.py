from __future__ import annotations

from decimal import Decimal, InvalidOperation
from typing import Any

from sqlalchemy import desc, exists, func, literal, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.copy_payloads import validate_copy_payload
from productflow_backend.application.product_workflow.templates import (
    materialize_product_workflow_from_template,
    resolve_product_creation_canvas_template,
)
from productflow_backend.application.time import now_utc
from productflow_backend.domain.durable_generation_tasks import WORKFLOW_RUN_GENERATION_TASK_CONTRACT
from productflow_backend.domain.enums import (
    CopyStatus,
    ProductWorkflowState,
    SourceAssetKind,
    WorkflowNodeStatus,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    CopySet,
    PosterVariant,
    Product,
    ProductWorkflow,
    SourceAsset,
    WorkflowNode,
    WorkflowRun,
)
from productflow_backend.infrastructure.storage import LocalStorage


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
            selectinload(Product.creative_briefs),
            selectinload(Product.copy_sets),
            selectinload(Product.poster_variants),
            selectinload(Product.confirmed_copy_set),
            selectinload(Product.workflows).selectinload(ProductWorkflow.nodes),
            selectinload(Product.workflows).selectinload(ProductWorkflow.runs),
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
    storage: LocalStorage | None = None,
) -> Product:
    """创建商品，保存原始图和参考图到本地存储。"""
    canvas_template = resolve_product_creation_canvas_template(canvas_template_key)
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
    if canvas_template is not None:
        materialize_product_workflow_from_template(
            session,
            product_id=product.id,
            template=canvas_template,
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
        raise BusinessValidationError("只能删除商品参考图")

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

    status_filter = _product_status_filter(status)
    total = session.scalar(select(func.count()).select_from(Product).where(status_filter)) or 0
    products = session.scalars(_product_query().where(status_filter).offset(start).limit(page_size)).all()
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
    session.delete(product)
    session.commit()
    storage.delete_product_tree(product_id)


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
