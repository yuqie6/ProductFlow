from __future__ import annotations

from dataclasses import dataclass
from decimal import Decimal, InvalidOperation
from pathlib import Path
from typing import Any

import httpx
from openai import APIConnectionError, APITimeoutError, InternalServerError, RateLimitError
from pydantic import ValidationError
from sqlalchemy import desc, func, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.contracts import (
    PosterGenerationInput,
    ProductInput,
    ReferenceImageInput,
)
from productflow_backend.application.time import now_utc
from productflow_backend.config import get_runtime_settings
from productflow_backend.domain.enums import (
    CopyStatus,
    JobKind,
    JobStatus,
    PosterKind,
    ProductWorkflowState,
    SourceAssetKind,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import NotFoundError
from productflow_backend.infrastructure.db.models import (
    CopySet,
    CreativeBrief,
    JobRun,
    PosterVariant,
    Product,
    ProductWorkflow,
    SourceAsset,
    WorkflowRun,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.image.base import infer_extension
from productflow_backend.infrastructure.image.factory import get_image_provider
from productflow_backend.infrastructure.poster.renderer import PosterRenderer
from productflow_backend.infrastructure.storage import LocalStorage
from productflow_backend.infrastructure.text.factory import get_text_provider


@dataclass(frozen=True, slots=True)
class JobCreationResult:
    job: JobRun
    created: bool


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
            selectinload(Product.job_runs),
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


def _get_poster_or_raise(session: Session, poster_id: str) -> PosterVariant:
    poster = session.get(PosterVariant, poster_id)
    if poster is None:
        raise NotFoundError("海报不存在")
    return poster


def derive_product_state(product: Product) -> ProductWorkflowState:
    """从商品关联数据推导流程状态，用于列表过滤。"""
    if product.poster_variants:
        return ProductWorkflowState.POSTER_READY
    if product.current_confirmed_copy_set_id:
        return ProductWorkflowState.COPY_READY
    latest_job = max(product.job_runs, key=lambda item: item.created_at, default=None)
    if latest_job and latest_job.status == JobStatus.FAILED:
        return ProductWorkflowState.FAILED
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
    if any(job.status in {JobStatus.QUEUED, JobStatus.RUNNING} for job in product.job_runs):
        raise ValueError("商品任务运行中，稍后删除")
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


def _get_existing_active_job(session: Session, *, product_id: str, kind: JobKind) -> JobRun | None:
    """查找同一商品同一类型下仍在运行（排队或执行中）的任务，用于幂等去重。"""
    stmt = (
        select(JobRun)
        .where(
            JobRun.product_id == product_id,
            JobRun.kind == kind,
            JobRun.status.in_([JobStatus.QUEUED, JobStatus.RUNNING]),
        )
        .order_by(desc(JobRun.created_at))
    )
    return session.scalar(stmt)


def create_copy_job(session: Session, *, product_id: str) -> JobCreationResult:
    _get_product_or_raise(session, product_id)
    existing = _get_existing_active_job(session, product_id=product_id, kind=JobKind.COPY_GENERATION)
    if existing:
        return JobCreationResult(job=existing, created=False)

    job = JobRun(product_id=product_id, kind=JobKind.COPY_GENERATION, status=JobStatus.QUEUED)
    session.add(job)
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        existing = _get_existing_active_job(session, product_id=product_id, kind=JobKind.COPY_GENERATION)
        if existing:
            return JobCreationResult(job=existing, created=False)
        raise
    session.refresh(job)
    return JobCreationResult(job=job, created=True)


def create_poster_job(
    session: Session,
    *,
    product_id: str,
    target_poster_kind: PosterKind | None = None,
) -> JobCreationResult:
    product = _get_product_or_raise(session, product_id)
    if not product.current_confirmed_copy_set_id:
        raise ValueError("请先确认一版文案，再生成海报")

    existing = _get_existing_active_job(session, product_id=product_id, kind=JobKind.POSTER_GENERATION)
    if existing:
        return JobCreationResult(job=existing, created=False)

    job = JobRun(
        product_id=product_id,
        kind=JobKind.POSTER_GENERATION,
        status=JobStatus.QUEUED,
        target_poster_kind=target_poster_kind,
    )
    session.add(job)
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        existing = _get_existing_active_job(session, product_id=product_id, kind=JobKind.POSTER_GENERATION)
        if existing:
            return JobCreationResult(job=existing, created=False)
        raise
    session.refresh(job)
    return JobCreationResult(job=job, created=True)


def create_regenerate_poster_job(session: Session, *, poster_id: str) -> JobCreationResult:
    poster = _get_poster_or_raise(session, poster_id)
    return create_poster_job(session, product_id=poster.product_id, target_poster_kind=poster.kind)


def get_job(session: Session, job_id: str) -> JobRun:
    job = session.get(JobRun, job_id)
    if job is None:
        raise NotFoundError("任务不存在")
    return job


def get_product_history(session: Session, product_id: str) -> dict[str, Any]:
    product = _get_product_or_raise(session, product_id)
    return {
        "copy_sets": sorted(product.copy_sets, key=lambda item: item.created_at, reverse=True),
        "poster_variants": sorted(product.poster_variants, key=lambda item: item.created_at, reverse=True),
        "jobs": sorted(product.job_runs, key=lambda item: item.created_at, reverse=True),
    }


def mark_job_enqueue_failed(session: Session, *, job_id: str, reason: str) -> None:
    job = get_job(session, job_id)
    job.status = JobStatus.FAILED
    job.failure_reason = f"任务入队失败: {reason}"[:1000]
    job.is_retryable = True
    job.finished_at = now_utc()
    session.commit()


def _mark_job_running(session: Session, job: JobRun) -> bool:
    if job.status != JobStatus.QUEUED:
        return False
    job.status = JobStatus.RUNNING
    job.started_at = now_utc()
    job.finished_at = None
    job.attempts += 1
    job.failure_reason = None
    session.commit()
    return True


def _mark_job_failed(session: Session, job: JobRun, reason: str, *, retryable: bool) -> None:
    job.status = JobStatus.QUEUED if retryable else JobStatus.FAILED
    job.failure_reason = reason[:1000]
    job.is_retryable = retryable
    job.finished_at = None if retryable else now_utc()
    session.commit()


def _mark_job_succeeded(session: Session, job: JobRun) -> None:
    job.status = JobStatus.SUCCEEDED
    job.is_retryable = False
    job.finished_at = now_utc()
    session.commit()


def _is_retryable_exception(exc: Exception) -> bool:
    if isinstance(exc, (APIConnectionError, APITimeoutError, InternalServerError, RateLimitError)):
        return True
    if isinstance(exc, (httpx.TimeoutException, httpx.TransportError)):
        return True
    if isinstance(exc, httpx.HTTPStatusError):
        status_code = exc.response.status_code
        return status_code in {408, 409, 425, 429} or status_code >= 500
    if isinstance(exc, (RuntimeError, ValueError, ValidationError, InvalidOperation)):
        return False
    return False


def _handle_job_exception(session: Session, *, job_id: str, exc: Exception) -> bool:
    """统一任务异常处理：判断是否可重试并更新任务状态。返回 True 表示需要重试。"""
    job = get_job(session, job_id)
    settings = get_runtime_settings()
    retryable = _is_retryable_exception(exc) and job.attempts < settings.job_max_attempts
    _mark_job_failed(session, job, str(exc), retryable=retryable)
    return retryable


def execute_copy_job(job_id: str) -> bool:
    session_factory = get_session_factory()
    session = session_factory()
    try:
        job = get_job(session, job_id)
        if not _mark_job_running(session, job):
            session.close()
            return False
        product = _get_product_or_raise(session, job.product_id)
        source_asset = next(
            (asset for asset in product.source_assets if asset.kind == SourceAssetKind.ORIGINAL_IMAGE),
            None,
        )
        if source_asset is None:
            raise RuntimeError("商品缺少原始图片")

        storage = LocalStorage()
        provider = get_text_provider()
        product_input = ProductInput(
            name=product.name,
            category=product.category,
            price=str(product.price) if product.price is not None else None,
            source_note=product.source_note,
            image_path=str(storage.resolve(source_asset.storage_path)),
        )

        brief_payload, brief_model = provider.generate_brief(product_input)
        brief = CreativeBrief(
            product_id=product.id,
            payload=brief_payload.model_dump(),
            provider_name=provider.provider_name,
            model_name=brief_model,
            prompt_version=provider.prompt_version,
        )
        session.add(brief)
        session.flush()

        copy_payload, copy_model = provider.generate_copy(product_input, brief_payload)
        copy_set = CopySet(
            product_id=product.id,
            creative_brief_id=brief.id,
            title=copy_payload.title,
            selling_points=copy_payload.selling_points,
            poster_headline=copy_payload.poster_headline,
            cta=copy_payload.cta,
            model_title=copy_payload.title,
            model_selling_points=copy_payload.selling_points,
            model_poster_headline=copy_payload.poster_headline,
            model_cta=copy_payload.cta,
            provider_name=provider.provider_name,
            model_name=copy_model,
            prompt_version=provider.prompt_version,
        )
        session.add(copy_set)
        session.flush()
        job.copy_set_id = copy_set.id
        _mark_job_succeeded(session, job)
    except Exception as exc:  # noqa: BLE001
        session.rollback()
        try:
            return _handle_job_exception(session, job_id=job_id, exc=exc)
        finally:
            session.close()
    else:
        session.close()
        return False


def execute_poster_job(job_id: str) -> bool:
    session_factory = get_session_factory()
    session = session_factory()
    try:
        job = get_job(session, job_id)
        if not _mark_job_running(session, job):
            session.close()
            return False
        product = _get_product_or_raise(session, job.product_id)
        copy_set = product.confirmed_copy_set
        if copy_set is None:
            raise RuntimeError("当前没有已确认文案")

        source_asset = next(
            (asset for asset in product.source_assets if asset.kind == SourceAssetKind.ORIGINAL_IMAGE),
            None,
        )
        if source_asset is None:
            raise RuntimeError("商品缺少原始图片")

        storage = LocalStorage()
        render_input = PosterGenerationInput(
            product_name=product.name,
            category=product.category,
            price=str(product.price) if product.price is not None else None,
            source_note=product.source_note,
            title=copy_set.title,
            selling_points=copy_set.selling_points,
            poster_headline=copy_set.poster_headline,
            cta=copy_set.cta,
            source_image=Path(storage.resolve(source_asset.storage_path)),
            reference_images=[
                ReferenceImageInput(
                    path=Path(storage.resolve(asset.storage_path)),
                    mime_type=asset.mime_type,
                    filename=asset.original_filename,
                )
                for asset in product.source_assets
                if asset.kind in [SourceAssetKind.ORIGINAL_IMAGE, SourceAssetKind.REFERENCE_IMAGE]
            ],
        )

        kinds = [job.target_poster_kind] if job.target_poster_kind else [PosterKind.MAIN_IMAGE, PosterKind.PROMO_POSTER]
        last_poster_id: str | None = None
        settings = get_runtime_settings()
        renderer = PosterRenderer()
        for kind in kinds:
            if kind is None:
                continue
            if settings.poster_generation_mode == "generated":
                image_provider = get_image_provider()
                generated_image, image_model = image_provider.generate_poster_image(render_input, kind)
                content = generated_image.bytes_data
                width = generated_image.width
                height = generated_image.height
                template_name = f"{image_provider.provider_name}:{generated_image.variant_label}"
                mime_type = generated_image.mime_type
            else:
                content = renderer.render(render_input, kind)
                image_model = None
                width = 1080
                height = 1080 if kind == PosterKind.MAIN_IMAGE else 1440
                template_name = "default-main" if kind == PosterKind.MAIN_IMAGE else "default-promo"
                mime_type = "image/png"
            relative_path = storage.save_generated_image(
                product.id,
                kind.value,
                content,
                suffix=infer_extension(mime_type),
            )
            poster = PosterVariant(
                product_id=product.id,
                copy_set_id=copy_set.id,
                kind=kind,
                template_name=template_name if image_model is None else f"{template_name}:{image_model}",
                storage_path=relative_path,
                mime_type=mime_type,
                width=width,
                height=height,
            )
            session.add(poster)
            session.flush()
            last_poster_id = poster.id

        job.copy_set_id = copy_set.id
        job.poster_variant_id = last_poster_id
        _mark_job_succeeded(session, job)
    except Exception as exc:  # noqa: BLE001
        session.rollback()
        try:
            return _handle_job_exception(session, job_id=job_id, exc=exc)
        finally:
            session.close()
    else:
        session.close()
        return False
