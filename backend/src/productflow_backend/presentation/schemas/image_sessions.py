from __future__ import annotations

from datetime import datetime
from typing import Literal

from pydantic import BaseModel, Field, field_validator

from productflow_backend.domain.enums import ImageSessionAssetKind
from productflow_backend.infrastructure.db.models import ImageSession, ImageSessionAsset, ImageSessionRound
from productflow_backend.presentation.image_variants import build_image_urls
from productflow_backend.presentation.schemas.validators import validate_image_generation_size


class ImageSessionAssetResponse(BaseModel):
    id: str
    kind: ImageSessionAssetKind
    original_filename: str
    mime_type: str
    download_url: str
    preview_url: str
    thumbnail_url: str
    created_at: datetime


class ImageSessionRoundResponse(BaseModel):
    id: str
    prompt: str
    assistant_message: str
    size: str
    model_name: str
    provider_name: str
    prompt_version: str
    provider_response_id: str | None = None
    previous_response_id: str | None = None
    image_generation_call_id: str | None = None
    generated_asset: ImageSessionAssetResponse
    created_at: datetime


class ImageSessionSummaryResponse(BaseModel):
    id: str
    product_id: str | None = None
    title: str
    rounds_count: int
    latest_generated_asset: ImageSessionAssetResponse | None = None
    created_at: datetime
    updated_at: datetime


class ImageSessionDetailResponse(BaseModel):
    id: str
    product_id: str | None = None
    title: str
    assets: list[ImageSessionAssetResponse]
    rounds: list[ImageSessionRoundResponse]
    created_at: datetime
    updated_at: datetime


class ImageSessionListResponse(BaseModel):
    items: list[ImageSessionSummaryResponse]


class CreateImageSessionRequest(BaseModel):
    product_id: str | None = None
    title: str | None = Field(default=None, max_length=255)


class UpdateImageSessionRequest(BaseModel):
    title: str = Field(min_length=1, max_length=255)


class GenerateImageSessionRoundRequest(BaseModel):
    prompt: str = Field(min_length=1, max_length=4000)
    size: str = Field(default="1024x1024")

    @field_validator("size")
    @classmethod
    def validate_size(cls, size: str) -> str:
        return validate_image_generation_size(size)


class AttachImageSessionAssetRequest(BaseModel):
    product_id: str | None = None
    target: Literal["reference", "main_source"]


class ProductWritebackResponse(BaseModel):
    product_id: str
    message: str


def serialize_image_session_asset(asset: ImageSessionAsset) -> ImageSessionAssetResponse:
    urls = build_image_urls(f"/api/image-session-assets/{asset.id}/download")
    return ImageSessionAssetResponse(
        id=asset.id,
        kind=asset.kind,
        original_filename=asset.original_filename,
        mime_type=asset.mime_type,
        **urls,
        created_at=asset.created_at,
    )


def serialize_image_session_round(round_item: ImageSessionRound) -> ImageSessionRoundResponse:
    return ImageSessionRoundResponse(
        id=round_item.id,
        prompt=round_item.prompt,
        assistant_message=round_item.assistant_message,
        size=round_item.size,
        model_name=round_item.model_name,
        provider_name=round_item.provider_name,
        prompt_version=round_item.prompt_version,
        provider_response_id=round_item.provider_response_id,
        previous_response_id=round_item.previous_response_id,
        image_generation_call_id=round_item.image_generation_call_id,
        generated_asset=serialize_image_session_asset(round_item.generated_asset),
        created_at=round_item.created_at,
    )


def serialize_image_session_summary(image_session: ImageSession) -> ImageSessionSummaryResponse:
    latest_round = max(image_session.rounds, key=lambda item: item.created_at, default=None)
    return ImageSessionSummaryResponse(
        id=image_session.id,
        product_id=image_session.product_id,
        title=image_session.title,
        rounds_count=len(image_session.rounds),
        latest_generated_asset=(serialize_image_session_asset(latest_round.generated_asset) if latest_round else None),
        created_at=image_session.created_at,
        updated_at=image_session.updated_at,
    )


def serialize_image_session_detail(image_session: ImageSession) -> ImageSessionDetailResponse:
    rounds = sorted(image_session.rounds, key=lambda item: item.created_at)
    assets = sorted(image_session.assets, key=lambda item: item.created_at, reverse=True)
    return ImageSessionDetailResponse(
        id=image_session.id,
        product_id=image_session.product_id,
        title=image_session.title,
        assets=[serialize_image_session_asset(item) for item in assets],
        rounds=[serialize_image_session_round(item) for item in rounds],
        created_at=image_session.created_at,
        updated_at=image_session.updated_at,
    )
