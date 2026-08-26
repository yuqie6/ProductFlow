from __future__ import annotations

from fastapi import APIRouter, Depends

from productflow_backend.application.delivery_renditions.presets import list_delivery_presets
from productflow_backend.presentation.deps import require_admin
from productflow_backend.presentation.schemas.delivery_presets import (
    DeliveryPresetCatalogResponse,
    serialize_delivery_preset_catalog,
)

router = APIRouter(
    prefix="/api/v3",
    tags=["delivery-presets"],
    dependencies=[Depends(require_admin)],
)


@router.get("/delivery-presets", response_model=DeliveryPresetCatalogResponse)
def list_delivery_presets_endpoint() -> DeliveryPresetCatalogResponse:
    return serialize_delivery_preset_catalog(list_delivery_presets())


__all__ = ["router"]
