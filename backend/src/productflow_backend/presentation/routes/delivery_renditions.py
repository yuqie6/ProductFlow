from __future__ import annotations

from fastapi import APIRouter, Depends, Response, status
from sqlalchemy.orm import Session

from productflow_backend.application.delivery_renditions import (
    get_delivery_rendition_job,
    list_delivery_rendition_jobs,
    retry_delivery_rendition_job,
    submit_delivery_rendition_job,
)
from productflow_backend.domain.enums import JobStatus
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.delivery_renditions import (
    CreateDeliveryRenditionRequest,
    DeliveryRenditionJobListResponse,
    DeliveryRenditionJobResponse,
    serialize_delivery_rendition_job,
)

router = APIRouter(
    prefix="/api/v2",
    tags=["delivery-renditions"],
    dependencies=[Depends(require_admin)],
)


@router.post(
    "/product-image-assets/{source_asset_id}/renditions",
    response_model=DeliveryRenditionJobResponse,
)
def create_delivery_rendition_endpoint(
    source_asset_id: str,
    payload: CreateDeliveryRenditionRequest,
    response: Response,
    session: Session = Depends(get_session),
) -> DeliveryRenditionJobResponse:
    job = submit_delivery_rendition_job(
        session,
        source_asset_id=source_asset_id,
        delivery_spec=payload,
    )
    response.status_code = (
        status.HTTP_202_ACCEPTED
        if job.status in {JobStatus.QUEUED, JobStatus.RUNNING}
        else status.HTTP_200_OK
    )
    return serialize_delivery_rendition_job(job)


@router.get(
    "/product-image-assets/{source_asset_id}/renditions",
    response_model=DeliveryRenditionJobListResponse,
)
def list_delivery_renditions_endpoint(
    source_asset_id: str,
    session: Session = Depends(get_session),
) -> DeliveryRenditionJobListResponse:
    jobs = list_delivery_rendition_jobs(session, source_asset_id=source_asset_id)
    return DeliveryRenditionJobListResponse(
        items=[serialize_delivery_rendition_job(job) for job in jobs]
    )


@router.get(
    "/delivery-rendition-jobs/{job_id}",
    response_model=DeliveryRenditionJobResponse,
)
def get_delivery_rendition_endpoint(
    job_id: str,
    session: Session = Depends(get_session),
) -> DeliveryRenditionJobResponse:
    return serialize_delivery_rendition_job(get_delivery_rendition_job(session, job_id))


@router.post(
    "/delivery-rendition-jobs/{job_id}/retry",
    response_model=DeliveryRenditionJobResponse,
    status_code=status.HTTP_202_ACCEPTED,
)
def retry_delivery_rendition_endpoint(
    job_id: str,
    session: Session = Depends(get_session),
) -> DeliveryRenditionJobResponse:
    return serialize_delivery_rendition_job(retry_delivery_rendition_job(session, job_id=job_id))
