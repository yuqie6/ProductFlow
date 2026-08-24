from __future__ import annotations

from fastapi import APIRouter, Depends, Query, status
from sqlalchemy.orm import Session

from productflow_backend.application.product_images.fidelity_checks import (
    FIDELITY_CHECK_MAX_LIMIT,
    create_product_image_fidelity_check,
    list_product_image_fidelity_checks,
)
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.image_fidelity_checks import (
    CreateProductImageFidelityCheckRequest,
    ProductImageFidelityCheckListResponse,
    ProductImageFidelityCheckResponse,
    serialize_product_image_fidelity_check,
    serialize_product_image_fidelity_check_list,
)

router = APIRouter(prefix="/api", tags=["image-fidelity-checks"], dependencies=[Depends(require_admin)])


@router.get(
    "/v3/products/{product_id}/image-assets/{asset_id}/fidelity-checks",
    response_model=ProductImageFidelityCheckListResponse,
)
def list_product_image_fidelity_checks_endpoint(
    product_id: str,
    asset_id: str,
    limit: int = Query(default=FIDELITY_CHECK_MAX_LIMIT, ge=1, le=FIDELITY_CHECK_MAX_LIMIT),
    session: Session = Depends(get_session),
) -> ProductImageFidelityCheckListResponse:
    result = list_product_image_fidelity_checks(
        session,
        product_id=product_id,
        asset_id=asset_id,
        limit=limit,
    )
    return serialize_product_image_fidelity_check_list(result)


@router.post(
    "/v3/products/{product_id}/image-assets/{asset_id}/fidelity-checks",
    response_model=ProductImageFidelityCheckResponse,
    status_code=status.HTTP_201_CREATED,
)
def create_product_image_fidelity_check_endpoint(
    product_id: str,
    asset_id: str,
    payload: CreateProductImageFidelityCheckRequest,
    session: Session = Depends(get_session),
) -> ProductImageFidelityCheckResponse:
    check = create_product_image_fidelity_check(
        session,
        product_id=product_id,
        asset_id=asset_id,
        expected_latest_version=payload.expected_latest_version,
        idempotency_key=payload.idempotency_key,
        shape_fidelity=payload.shape_fidelity,
        color_material_fidelity=payload.color_material_fidelity,
        logo_text_legibility=payload.logo_text_legibility,
        text_policy_compliance=payload.text_policy_compliance,
        notes=payload.notes,
    )
    return serialize_product_image_fidelity_check(check)
