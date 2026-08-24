from __future__ import annotations

from datetime import datetime

from pydantic import BaseModel, ConfigDict, Field

from productflow_backend.application.product_images.fidelity_checks import (
    ProductImageFidelityCheckList,
)
from productflow_backend.domain.enums import ProductImageFidelityOutcome
from productflow_backend.infrastructure.db.models import ProductImageFidelityCheck


class CreateProductImageFidelityCheckRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    expected_latest_version: int = Field(ge=0)
    idempotency_key: str = Field(min_length=1, max_length=120)
    shape_fidelity: ProductImageFidelityOutcome
    color_material_fidelity: ProductImageFidelityOutcome
    logo_text_legibility: ProductImageFidelityOutcome
    text_policy_compliance: ProductImageFidelityOutcome
    notes: str | None = Field(default=None, max_length=4000)


class ProductImageFidelityCheckResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    product_id: str
    asset_id: str
    version: int
    shape_fidelity: ProductImageFidelityOutcome
    color_material_fidelity: ProductImageFidelityOutcome
    logo_text_legibility: ProductImageFidelityOutcome
    text_policy_compliance: ProductImageFidelityOutcome
    notes: str | None = None
    checked_by: str
    idempotency_key: str
    request_hash: str
    created_at: datetime


class ProductImageFidelityCheckListResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    product_id: str
    asset_id: str
    latest_version: int
    items: list[ProductImageFidelityCheckResponse]


def serialize_product_image_fidelity_check(
    check: ProductImageFidelityCheck,
) -> ProductImageFidelityCheckResponse:
    return ProductImageFidelityCheckResponse(
        id=check.id,
        product_id=check.product_id,
        asset_id=check.asset_id,
        version=check.version,
        shape_fidelity=ProductImageFidelityOutcome(check.shape_fidelity),
        color_material_fidelity=ProductImageFidelityOutcome(check.color_material_fidelity),
        logo_text_legibility=ProductImageFidelityOutcome(check.logo_text_legibility),
        text_policy_compliance=ProductImageFidelityOutcome(check.text_policy_compliance),
        notes=check.notes,
        checked_by=check.checked_by,
        idempotency_key=check.idempotency_key,
        request_hash=check.request_hash,
        created_at=check.created_at,
    )


def serialize_product_image_fidelity_check_list(
    result: ProductImageFidelityCheckList,
) -> ProductImageFidelityCheckListResponse:
    return ProductImageFidelityCheckListResponse(
        product_id=result.product_id,
        asset_id=result.asset_id,
        latest_version=result.latest_version,
        items=[serialize_product_image_fidelity_check(item) for item in result.items],
    )
