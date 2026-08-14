from __future__ import annotations

from datetime import datetime

from pydantic import BaseModel, ConfigDict

from productflow_backend.application.delivery_renditions.contracts import DeliveryRenditionStatus
from productflow_backend.application.workflow_drafts.contracts import DeliverySpec
from productflow_backend.infrastructure.db.models import DeliveryRenditionJob
from productflow_backend.presentation.schemas.products import (
    ProductImageAssetResponse,
    serialize_product_image_asset,
)


class CreateDeliveryRenditionRequest(DeliverySpec):
    model_config = ConfigDict(extra="forbid", frozen=True)


class DeliveryRenditionJobResponse(BaseModel):
    id: str
    product_id: str
    source_asset_id: str
    result_asset: ProductImageAssetResponse | None = None
    delivery_spec: DeliverySpec
    status: DeliveryRenditionStatus
    attempts: int
    is_retryable: bool
    failure_reason: str | None = None
    created_at: datetime
    started_at: datetime | None = None
    finished_at: datetime | None = None
    updated_at: datetime


class DeliveryRenditionJobListResponse(BaseModel):
    items: list[DeliveryRenditionJobResponse]


def serialize_delivery_rendition_job(job: DeliveryRenditionJob) -> DeliveryRenditionJobResponse:
    return DeliveryRenditionJobResponse(
        id=job.id,
        product_id=job.product_id,
        source_asset_id=job.source_asset_id,
        result_asset=(
            serialize_product_image_asset(job.result_asset)
            if job.result_asset is not None
            else None
        ),
        delivery_spec=DeliverySpec.model_validate(job.spec_json),
        status=job.status,
        attempts=job.attempts,
        is_retryable=job.is_retryable,
        failure_reason=job.failure_reason,
        created_at=job.created_at,
        started_at=job.started_at,
        finished_at=job.finished_at,
        updated_at=job.updated_at,
    )
