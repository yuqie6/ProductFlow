from __future__ import annotations

import logging
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path

from pydantic import ValidationError
from sqlalchemy import select, update
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.async_delivery import (
    delivery_key_for_actor,
    requeue_async_dispatch,
    stage_async_dispatch,
)
from productflow_backend.application.delivery_renditions.contracts import (
    DELIVERY_FORMAT_EXTENSIONS,
    DELIVERY_RENDITION_SPEC_SCHEMA_VERSION,
    normalize_delivery_spec,
)
from productflow_backend.application.delivery_renditions.renderer import render_delivery_rendition
from productflow_backend.application.media_assets import inspect_image_bytes, stage_product_image_asset
from productflow_backend.application.queue_submission import enqueue_or_mark_failed, raise_queue_unavailable
from productflow_backend.application.storage_compensation import StorageWriteCompensation
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.contracts import DeliverySpec
from productflow_backend.domain.durable_generation_tasks import DELIVERY_RENDITION_TASK_CONTRACT
from productflow_backend.domain.enums import JobStatus, MediaVerificationStatus, ProductImageOriginType
from productflow_backend.domain.errors import (
    BusinessError,
    BusinessValidationError,
    ConflictError,
    NotFoundError,
)
from productflow_backend.infrastructure.db.models import (
    DeliveryRenditionJob,
    Product,
    ProductImageAsset,
    WorkflowImageGenerationRecord,
    new_id,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.storage import LocalStorage

logger = logging.getLogger(__name__)

DELIVERY_RENDITION_UNEXPECTED_FAILURE = "交付图处理失败，请重试"
DELIVERY_RENDITION_SOURCE_MISSING = "交付派生原图文件缺失或已变化"


@dataclass(frozen=True, slots=True)
class DeliveryRenditionJobCreation:
    job: DeliveryRenditionJob
    created: bool


@dataclass(frozen=True, slots=True)
class DeliveryRenditionClaim:
    job_id: str
    attempt_id: str
    product_id: str
    source_asset_id: str
    source_storage_path: str
    source_mime_type: str
    source_byte_size: int
    source_sha256: str
    source_display_name: str
    image_type_key: str | None
    delivery_spec: DeliverySpec


def create_delivery_rendition_job(
    session: Session,
    *,
    source_asset_id: str,
    delivery_spec: DeliverySpec | dict[str, object],
) -> DeliveryRenditionJobCreation:
    normalized = normalize_delivery_spec(delivery_spec)
    source_asset = session.scalar(
        select(ProductImageAsset)
        .options(selectinload(ProductImageAsset.media_object))
        .where(ProductImageAsset.id == source_asset_id)
    )
    if source_asset is None:
        raise NotFoundError("交付派生原图不存在")
    _validate_source_asset(session, source_asset)

    existing = session.scalar(
        select(DeliveryRenditionJob).where(
            DeliveryRenditionJob.source_asset_id == source_asset.id,
            DeliveryRenditionJob.spec_hash == normalized.spec_hash,
        )
    )
    if existing is not None:
        return DeliveryRenditionJobCreation(job=existing, created=False)

    job = DeliveryRenditionJob(
        product_id=source_asset.product_id,
        source_asset_id=source_asset.id,
        spec_schema_version=DELIVERY_RENDITION_SPEC_SCHEMA_VERSION,
        spec_json=normalized.payload,
        spec_hash=normalized.spec_hash,
        status=JobStatus.QUEUED,
        attempts=0,
        active_attempt_id=None,
        is_retryable=True,
    )
    try:
        with session.begin_nested():
            session.add(job)
            session.flush()
    except IntegrityError:
        existing = session.scalar(
            select(DeliveryRenditionJob).where(
                DeliveryRenditionJob.source_asset_id == source_asset.id,
                DeliveryRenditionJob.spec_hash == normalized.spec_hash,
            )
        )
        if existing is None:
            raise
        return DeliveryRenditionJobCreation(job=existing, created=False)
    return DeliveryRenditionJobCreation(job=job, created=True)


def submit_delivery_rendition_job(
    session: Session,
    *,
    source_asset_id: str,
    delivery_spec: DeliverySpec | dict[str, object],
    enqueue: Callable[[str], None] | None = None,
) -> DeliveryRenditionJob:
    creation = create_delivery_rendition_job(
        session,
        source_asset_id=source_asset_id,
        delivery_spec=delivery_spec,
    )
    if creation.job.status == JobStatus.QUEUED and enqueue is None:
        stage_async_dispatch(
            session,
            delivery_key=delivery_key_for_actor(DELIVERY_RENDITION_TASK_CONTRACT.actor_name, creation.job.id),
            actor_name=DELIVERY_RENDITION_TASK_CONTRACT.actor_name,
            aggregate_id=creation.job.id,
        )
        session.commit()
    else:
        session.commit()
        if creation.job.status == JobStatus.QUEUED and creation.created:
            enqueue_or_mark_failed(
                creation.job.id,
                enqueue=enqueue,
                mark_failed=lambda job_id, reason: mark_delivery_rendition_job_enqueue_failed(
                    session,
                    job_id=job_id,
                    reason=reason,
                ),
            )
        elif creation.job.status == JobStatus.QUEUED and enqueue is not None:
            try:
                enqueue(creation.job.id)
            except Exception as exc:  # noqa: BLE001
                raise_queue_unavailable(exc)
    session.expire_all()
    return get_delivery_rendition_job(session, creation.job.id)


def get_delivery_rendition_job(session: Session, job_id: str) -> DeliveryRenditionJob:
    job = session.scalar(_job_query().where(DeliveryRenditionJob.id == job_id))
    if job is None:
        raise NotFoundError("交付派生任务不存在")
    return job


def list_delivery_rendition_jobs(session: Session, *, source_asset_id: str) -> list[DeliveryRenditionJob]:
    if session.get(ProductImageAsset, source_asset_id) is None:
        raise NotFoundError("交付派生原图不存在")
    return list(
        session.scalars(
            _job_query()
            .where(DeliveryRenditionJob.source_asset_id == source_asset_id)
            .order_by(DeliveryRenditionJob.created_at.desc(), DeliveryRenditionJob.id.desc())
        ).all()
    )


def retry_delivery_rendition_job(
    session: Session,
    *,
    job_id: str,
    enqueue: Callable[[str], None] | None = None,
) -> DeliveryRenditionJob:
    job = session.scalar(select(DeliveryRenditionJob).where(DeliveryRenditionJob.id == job_id).with_for_update())
    if job is None:
        raise NotFoundError("交付派生任务不存在")
    if job.status != JobStatus.FAILED:
        raise ConflictError("只有失败的交付派生任务可以重试")
    if not job.is_retryable:
        raise ConflictError("该交付派生任务不可重试")
    job.status = JobStatus.QUEUED
    job.active_attempt_id = None
    job.failure_reason = None
    job.started_at = None
    job.finished_at = None
    job.is_retryable = True
    if enqueue is None:
        requeue_async_dispatch(
            session,
            delivery_key=delivery_key_for_actor(DELIVERY_RENDITION_TASK_CONTRACT.actor_name, job.id),
            actor_name=DELIVERY_RENDITION_TASK_CONTRACT.actor_name,
            aggregate_id=job.id,
        )
        session.commit()
    else:
        session.commit()
        enqueue_or_mark_failed(
            job.id,
            enqueue=enqueue,
            mark_failed=lambda queued_job_id, reason: mark_delivery_rendition_job_enqueue_failed(
                session,
                job_id=queued_job_id,
                reason=reason,
            ),
        )
    session.expire_all()
    return get_delivery_rendition_job(session, job.id)


def mark_delivery_rendition_job_enqueue_failed(
    session: Session,
    *,
    job_id: str,
    reason: str,
) -> None:
    now = now_utc()
    session.execute(
        update(DeliveryRenditionJob)
        .where(
            DeliveryRenditionJob.id == job_id,
            DeliveryRenditionJob.status == JobStatus.QUEUED,
        )
        .values(
            status=JobStatus.FAILED,
            active_attempt_id=None,
            failure_reason=reason[:1000],
            finished_at=now,
            is_retryable=True,
            updated_at=now,
        )
    )
    session.commit()


def claim_delivery_rendition_job(
    session: Session,
    *,
    job_id: str,
    attempt_id: str | None = None,
) -> DeliveryRenditionClaim | None:
    resolved_attempt_id = attempt_id or new_id()
    now = now_utc()
    claimed = session.execute(
        update(DeliveryRenditionJob)
        .where(
            DeliveryRenditionJob.id == job_id,
            DeliveryRenditionJob.status == JobStatus.QUEUED,
        )
        .values(
            status=JobStatus.RUNNING,
            attempts=DeliveryRenditionJob.attempts + 1,
            active_attempt_id=resolved_attempt_id,
            failure_reason=None,
            started_at=now,
            finished_at=None,
            updated_at=now,
        )
    )
    session.commit()
    if claimed.rowcount != 1:
        return None

    job = session.scalar(
        select(DeliveryRenditionJob)
        .options(
            selectinload(DeliveryRenditionJob.source_asset).selectinload(ProductImageAsset.media_object)
        )
        .where(DeliveryRenditionJob.id == job_id)
    )
    if job is None:
        raise NotFoundError("交付派生任务不存在")
    source = job.source_asset
    media = source.media_object
    if media.verification_status != MediaVerificationStatus.VERIFIED:
        raise BusinessValidationError("交付派生原图媒体尚未通过核验")
    if media.byte_size is None or media.sha256 is None:
        raise BusinessValidationError("交付派生原图缺少核验元数据")
    try:
        spec = DeliverySpec.model_validate(job.spec_json)
    except ValidationError as exc:
        raise BusinessValidationError("交付派生任务中的 DeliverySpec 已损坏") from exc
    return DeliveryRenditionClaim(
        job_id=job.id,
        attempt_id=resolved_attempt_id,
        product_id=job.product_id,
        source_asset_id=source.id,
        source_storage_path=media.storage_path,
        source_mime_type=media.mime_type,
        source_byte_size=media.byte_size,
        source_sha256=media.sha256,
        source_display_name=source.display_name,
        image_type_key=source.image_type_key,
        delivery_spec=spec,
    )


def execute_delivery_rendition_job(
    job_id: str,
    *,
    storage: LocalStorage | None = None,
) -> None:
    session = get_session_factory()()
    resolved_storage = storage or LocalStorage()
    attempt_id = new_id()
    try:
        claim = claim_delivery_rendition_job(session, job_id=job_id, attempt_id=attempt_id)
        if claim is None:
            return
        source_path = resolved_storage.resolve(claim.source_storage_path)
        source_bytes = source_path.read_bytes()
        source_metadata = inspect_image_bytes(source_bytes, expected_mime_type=claim.source_mime_type)
        if source_metadata.byte_size != claim.source_byte_size or source_metadata.sha256 != claim.source_sha256:
            raise BusinessValidationError(DELIVERY_RENDITION_SOURCE_MISSING)
        rendered = render_delivery_rendition(source_bytes, claim.delivery_spec)
        _persist_delivery_rendition_result(
            session,
            claim=claim,
            rendered_bytes=rendered.bytes_data,
            expected_mime_type=rendered.metadata.mime_type,
            storage=resolved_storage,
        )
    except FileNotFoundError:
        session.rollback()
        _fail_delivery_rendition_job(
            session,
            job_id=job_id,
            attempt_id=attempt_id,
            reason=DELIVERY_RENDITION_SOURCE_MISSING,
            retryable=False,
        )
    except BusinessError as exc:
        session.rollback()
        _fail_delivery_rendition_job(
            session,
            job_id=job_id,
            attempt_id=attempt_id,
            reason=str(exc),
            retryable=False,
        )
    except Exception:  # noqa: BLE001
        session.rollback()
        logger.exception("交付图处理失败: job_id=%s", job_id)
        _fail_delivery_rendition_job(
            session,
            job_id=job_id,
            attempt_id=attempt_id,
            reason=DELIVERY_RENDITION_UNEXPECTED_FAILURE,
            retryable=True,
        )
    finally:
        session.close()


def _persist_delivery_rendition_result(
    session: Session,
    *,
    claim: DeliveryRenditionClaim,
    rendered_bytes: bytes,
    expected_mime_type: str,
    storage: LocalStorage,
) -> bool:
    storage_writes = StorageWriteCompensation()
    try:
        job = session.scalar(
            select(DeliveryRenditionJob).where(DeliveryRenditionJob.id == claim.job_id).with_for_update()
        )
        if (
            job is None
            or job.status != JobStatus.RUNNING
            or job.active_attempt_id != claim.attempt_id
        ):
            session.rollback()
            return False
        source = session.scalar(
            select(ProductImageAsset)
            .where(ProductImageAsset.id == claim.source_asset_id)
            .with_for_update()
        )
        product = session.scalar(select(Product).where(Product.id == claim.product_id).with_for_update())
        if source is None or product is None or source.product_id != product.id:
            raise BusinessValidationError("交付派生原图状态已变化")
        extension = DELIVERY_FORMAT_EXTENSIONS[claim.delivery_spec.format]
        suffix = f"-{claim.delivery_spec.width}x{claim.delivery_spec.height}{extension}"
        stem = Path(source.original_filename).stem
        filename = f"{stem[: 255 - len(suffix)]}{suffix}"
        display_name = (
            f"{source.display_name} {claim.delivery_spec.width}x{claim.delivery_spec.height} "
            f"{claim.delivery_spec.format.upper()}"
        )
        result_asset = stage_product_image_asset(
            session,
            product=product,
            content=rendered_bytes,
            filename=filename,
            expected_mime_type=expected_mime_type,
            display_name=display_name,
            origin_type=ProductImageOriginType.WORKFLOW_GENERATION,
            storage=storage,
            storage_writes=storage_writes,
            parent_asset_id=source.id,
            image_type_key=source.image_type_key,
        )
        session.flush()
        now = now_utc()
        job.result_asset_id = result_asset.id
        job.status = JobStatus.SUCCEEDED
        job.active_attempt_id = None
        job.failure_reason = None
        job.finished_at = now
        job.is_retryable = False
        job.updated_at = now
        product.updated_at = now
        session.commit()
        return True
    except BaseException:
        session.rollback()
        storage_writes.cleanup()
        raise


def _fail_delivery_rendition_job(
    session: Session,
    *,
    job_id: str,
    attempt_id: str,
    reason: str,
    retryable: bool,
) -> bool:
    now = now_utc()
    result = session.execute(
        update(DeliveryRenditionJob)
        .where(
            DeliveryRenditionJob.id == job_id,
            DeliveryRenditionJob.status == JobStatus.RUNNING,
            DeliveryRenditionJob.active_attempt_id == attempt_id,
        )
        .values(
            status=JobStatus.FAILED,
            active_attempt_id=None,
            is_retryable=retryable,
            failure_reason=reason[:1000],
            finished_at=now,
            updated_at=now,
        )
    )
    session.commit()
    return result.rowcount == 1


def _validate_source_asset(session: Session, source_asset: ProductImageAsset) -> None:
    if source_asset.parent_asset_id is not None:
        raise BusinessValidationError("交付派生不能以已有派生图作为原图")
    if source_asset.media_object.verification_status != MediaVerificationStatus.VERIFIED:
        raise BusinessValidationError("交付派生原图媒体尚未通过核验")
    generation_record_id = session.scalar(
        select(WorkflowImageGenerationRecord.id)
        .where(WorkflowImageGenerationRecord.result_asset_id == source_asset.id)
        .limit(1)
    )
    if generation_record_id is None:
        raise BusinessValidationError("交付派生只接受成功的 schema-v2 工作流生成原图")


def _job_query():
    return select(DeliveryRenditionJob).options(
        selectinload(DeliveryRenditionJob.source_asset).selectinload(ProductImageAsset.media_object),
        selectinload(DeliveryRenditionJob.result_asset).selectinload(ProductImageAsset.media_object),
    )
