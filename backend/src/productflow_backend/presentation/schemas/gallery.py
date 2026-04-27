from __future__ import annotations

from datetime import datetime

from pydantic import BaseModel

from productflow_backend.infrastructure.db.models import ImageGalleryEntry
from productflow_backend.presentation.schemas.image_sessions import (
    ImageSessionAssetResponse,
    extract_actual_image_size,
    extract_provider_notes,
    serialize_image_session_asset,
)


class SaveGalleryEntryRequest(BaseModel):
    image_session_asset_id: str


class GalleryEntryResponse(BaseModel):
    id: str
    image_session_asset_id: str
    image_session_round_id: str | None = None
    image_session_id: str
    image_session_title: str
    product_id: str | None = None
    product_name: str | None = None
    image: ImageSessionAssetResponse
    prompt: str | None = None
    size: str | None = None
    actual_size: str | None = None
    model_name: str | None = None
    provider_name: str | None = None
    prompt_version: str | None = None
    provider_response_id: str | None = None
    image_generation_call_id: str | None = None
    generation_group_id: str | None = None
    candidate_index: int | None = None
    candidate_count: int | None = None
    base_asset_id: str | None = None
    selected_reference_asset_ids: list[str]
    provider_notes: list[str]
    created_at: datetime


class GalleryEntryListResponse(BaseModel):
    items: list[GalleryEntryResponse]


def serialize_gallery_entry(entry: ImageGalleryEntry) -> GalleryEntryResponse:
    round_item = entry.round
    image_session = entry.asset.session
    product = image_session.product
    return GalleryEntryResponse(
        id=entry.id,
        image_session_asset_id=entry.image_session_asset_id,
        image_session_round_id=entry.image_session_round_id,
        image_session_id=image_session.id,
        image_session_title=image_session.title,
        product_id=image_session.product_id,
        product_name=product.name if product else None,
        image=serialize_image_session_asset(entry.asset),
        prompt=round_item.prompt if round_item else None,
        size=round_item.size if round_item else None,
        actual_size=extract_actual_image_size(round_item.provider_output_json) if round_item else None,
        model_name=round_item.model_name if round_item else None,
        provider_name=round_item.provider_name if round_item else None,
        prompt_version=round_item.prompt_version if round_item else None,
        provider_response_id=round_item.provider_response_id if round_item else None,
        image_generation_call_id=round_item.image_generation_call_id if round_item else None,
        generation_group_id=round_item.generation_group_id if round_item else None,
        candidate_index=round_item.candidate_index if round_item else None,
        candidate_count=round_item.candidate_count if round_item else None,
        base_asset_id=round_item.base_asset_id if round_item else None,
        selected_reference_asset_ids=round_item.selected_reference_asset_ids or [] if round_item else [],
        provider_notes=extract_provider_notes(round_item.provider_output_json) if round_item else [],
        created_at=entry.created_at,
    )
