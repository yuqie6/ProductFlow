from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict

from productflow_backend.application.delivery_renditions.presets import DeliveryPreset
from productflow_backend.application.workflow_drafts.contracts import DeliverySpec


class DeliveryPresetResponse(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    key: str
    title: str
    aspect_ratio: str
    applicable_image_type: str
    reviewed_at: str
    source: str
    disclaimer: str
    delivery_spec: DeliverySpec


class DeliveryPresetCatalogResponse(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    supports_custom: Literal[True] = True
    items: list[DeliveryPresetResponse]


def serialize_delivery_preset(preset: DeliveryPreset) -> DeliveryPresetResponse:
    return DeliveryPresetResponse(
        key=preset.key,
        title=preset.title,
        aspect_ratio=preset.aspect_ratio,
        applicable_image_type=preset.applicable_image_type,
        reviewed_at=preset.reviewed_at,
        source=preset.source,
        disclaimer=preset.disclaimer,
        delivery_spec=preset.spec,
    )


def serialize_delivery_preset_catalog(
    presets: tuple[DeliveryPreset, ...],
) -> DeliveryPresetCatalogResponse:
    return DeliveryPresetCatalogResponse(
        items=[serialize_delivery_preset(preset) for preset in presets],
    )


__all__ = [
    "DeliveryPresetCatalogResponse",
    "DeliveryPresetResponse",
    "serialize_delivery_preset",
    "serialize_delivery_preset_catalog",
]
