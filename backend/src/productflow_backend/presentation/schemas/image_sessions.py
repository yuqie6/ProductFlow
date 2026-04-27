from __future__ import annotations

from datetime import datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator

from productflow_backend.domain.enums import ImageSessionAssetKind, JobStatus
from productflow_backend.infrastructure.db.models import (
    ImageSession,
    ImageSessionAsset,
    ImageSessionGenerationTask,
    ImageSessionRound,
)
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
    generation_group_id: str | None = None
    candidate_index: int = 1
    candidate_count: int = 1
    base_asset_id: str | None = None
    selected_reference_asset_ids: list[str] = Field(default_factory=list)
    provider_notes: list[str] = Field(default_factory=list)
    generated_asset: ImageSessionAssetResponse
    created_at: datetime


class ImageSessionGenerationTaskResponse(BaseModel):
    id: str
    session_id: str
    status: JobStatus
    prompt: str
    size: str
    base_asset_id: str | None = None
    selected_reference_asset_ids: list[str] = Field(default_factory=list)
    generation_count: int
    failure_reason: str | None = None
    result_generation_group_id: str | None = None
    tool_options: dict | None = None
    provider_notes: list[str] = Field(default_factory=list)
    created_at: datetime
    started_at: datetime | None = None
    finished_at: datetime | None = None
    queue_active_count: int
    queue_running_count: int
    queue_queued_count: int
    queue_max_concurrent_tasks: int
    queued_ahead_count: int | None = None
    queue_position: int | None = None


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
    generation_tasks: list[ImageSessionGenerationTaskResponse] = Field(default_factory=list)
    created_at: datetime
    updated_at: datetime


class ImageSessionListResponse(BaseModel):
    items: list[ImageSessionSummaryResponse]


class CreateImageSessionRequest(BaseModel):
    product_id: str | None = None
    title: str | None = Field(default=None, max_length=255)


class UpdateImageSessionRequest(BaseModel):
    title: str = Field(min_length=1, max_length=255)


class ImageToolOptionsRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    model: str | None = Field(default=None, min_length=1, max_length=100)
    quality: Literal["auto", "low", "medium", "high"] | None = None
    output_format: Literal["png", "jpeg", "webp"] | None = None
    output_compression: int | None = Field(default=None, ge=0, le=100)
    background: Literal["auto", "opaque", "transparent"] | None = None
    moderation: Literal["auto", "low"] | None = None
    action: Literal["auto", "generate", "edit"] | None = None
    input_fidelity: Literal["low", "high"] | None = None
    partial_images: int | None = Field(default=None, ge=0, le=3)
    n: int | None = Field(default=None, ge=1, le=10)

    @field_validator("model", mode="before")
    @classmethod
    def normalize_model(cls, value: object) -> str | None:
        normalized = "" if value is None else str(value).strip()
        return normalized or None


class GenerateImageSessionRoundRequest(BaseModel):
    prompt: str = Field(min_length=1, max_length=4000)
    size: str = Field(default="1024x1024")
    base_asset_id: str | None = None
    selected_reference_asset_ids: list[str] = Field(default_factory=list, max_length=6)
    generation_count: int = Field(default=1, ge=1, le=4)
    tool_options: ImageToolOptionsRequest | None = None

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


def extract_provider_notes(provider_output_json: dict | None) -> list[str]:
    if not isinstance(provider_output_json, dict):
        return []
    metadata = provider_output_json.get("_productflow")
    if not isinstance(metadata, dict):
        return []
    notes = metadata.get("notes")
    if not isinstance(notes, list):
        return []
    messages: list[str] = []
    for note in notes:
        if not isinstance(note, dict):
            continue
        message = note.get("message")
        if isinstance(message, str) and message.strip():
            messages.append(message.strip())
    return messages[:3]


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
        generation_group_id=round_item.generation_group_id,
        candidate_index=round_item.candidate_index,
        candidate_count=round_item.candidate_count,
        base_asset_id=round_item.base_asset_id,
        selected_reference_asset_ids=round_item.selected_reference_asset_ids or [],
        provider_notes=extract_provider_notes(round_item.provider_output_json),
        generated_asset=serialize_image_session_asset(round_item.generated_asset),
        created_at=round_item.created_at,
    )


def serialize_image_session_generation_task(
    task: ImageSessionGenerationTask,
    *,
    provider_notes: list[str] | None = None,
) -> ImageSessionGenerationTaskResponse:
    queue_metadata = getattr(task, "_queue_metadata", None)
    queue_overview = getattr(queue_metadata, "overview", None)
    return ImageSessionGenerationTaskResponse(
        id=task.id,
        session_id=task.session_id,
        status=task.status,
        prompt=task.prompt,
        size=task.size,
        base_asset_id=task.base_asset_id,
        selected_reference_asset_ids=task.selected_reference_asset_ids or [],
        generation_count=task.generation_count,
        failure_reason=task.failure_reason,
        result_generation_group_id=task.result_generation_group_id,
        tool_options=task.tool_options,
        provider_notes=provider_notes or [],
        created_at=task.created_at,
        started_at=task.started_at,
        finished_at=task.finished_at,
        queue_active_count=getattr(queue_overview, "active_count", 0),
        queue_running_count=getattr(queue_overview, "running_count", 0),
        queue_queued_count=getattr(queue_overview, "queued_count", 0),
        queue_max_concurrent_tasks=getattr(queue_overview, "max_concurrent_tasks", 0),
        queued_ahead_count=getattr(queue_metadata, "queued_ahead_count", None),
        queue_position=getattr(queue_metadata, "queue_position", None),
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
    generation_tasks = sorted(image_session.generation_tasks, key=lambda item: item.created_at, reverse=True)
    notes_by_group = {
        round_item.generation_group_id: extract_provider_notes(round_item.provider_output_json)
        for round_item in rounds
        if round_item.generation_group_id and extract_provider_notes(round_item.provider_output_json)
    }
    return ImageSessionDetailResponse(
        id=image_session.id,
        product_id=image_session.product_id,
        title=image_session.title,
        assets=[serialize_image_session_asset(item) for item in assets],
        rounds=[serialize_image_session_round(item) for item in rounds],
        generation_tasks=[
            serialize_image_session_generation_task(
                item,
                provider_notes=notes_by_group.get(item.result_generation_group_id or ""),
            )
            for item in generation_tasks
        ],
        created_at=image_session.created_at,
        updated_at=image_session.updated_at,
    )
