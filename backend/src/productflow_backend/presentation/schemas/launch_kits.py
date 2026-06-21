from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel, Field

from productflow_backend.application.launch_kit.status import latest_generation_task
from productflow_backend.domain.enums import JobStatus
from productflow_backend.domain.launch_kits import LaunchKitPlatform, LaunchKitProgressStage, LaunchKitStatus
from productflow_backend.infrastructure.db.models import LaunchKit, LaunchKitGenerationTask


class SourceReferenceRequest(BaseModel):
    pasted_reference_text: str | None = Field(default=None, max_length=20_000)
    reference_urls: list[str] = Field(default_factory=list, max_length=10)
    notes: str | None = Field(default=None, max_length=4_000)


class LaunchKitCreateRequest(BaseModel):
    product_name: str = Field(min_length=1, max_length=255)
    category_key: str = Field(min_length=1, max_length=80)
    target_platforms: list[LaunchKitPlatform] = Field(min_length=1, max_length=2)
    source_references: SourceReferenceRequest | None = None


class LaunchKitFeedbackRequest(BaseModel):
    used: bool | None = None
    edited: bool | None = None
    would_reuse: bool | None = None
    would_pay: bool | None = None
    notes: str | None = Field(default=None, max_length=4_000)
    metrics: dict[str, Any] = Field(default_factory=dict)


class LaunchKitTaskStatusResponse(BaseModel):
    id: str
    status: JobStatus
    progress_stage: LaunchKitProgressStage | None = None
    attempt_count: int
    failure_category: str | None = None
    failure_detail: str | None = None
    is_retryable: bool
    is_cancelable: bool
    started_at: datetime | None = None
    progress_updated_at: datetime | None = None
    finished_at: datetime | None = None
    created_at: datetime
    updated_at: datetime


class LaunchKitSummaryResponse(BaseModel):
    id: str
    product_id: str
    product_name: str
    category_key: str
    target_platforms: list[str]
    status: LaunchKitStatus
    latest_task: LaunchKitTaskStatusResponse | None = None
    quality_score_summary: dict[str, Any] | None = None
    created_at: datetime
    updated_at: datetime


class LaunchKitListResponse(BaseModel):
    items: list[LaunchKitSummaryResponse]
    total: int
    page: int
    page_size: int


class LaunchKitDetailResponse(LaunchKitSummaryResponse):
    buyer_angle_key: str | None = None
    source_references: dict[str, Any]
    generated_summary: dict[str, Any] | None = None
    selected_angle: dict[str, Any] | None = None
    export_snapshot: dict[str, Any] | None = None
    seller_feedback: dict[str, Any] | None = None
    variants: list[dict[str, Any]]
    exports: list[dict[str, Any]]


class LaunchKitStatusResponse(BaseModel):
    id: str
    status: LaunchKitStatus
    latest_task: LaunchKitTaskStatusResponse | None = None
    updated_at: datetime


def serialize_launch_kit_task(task: LaunchKitGenerationTask) -> LaunchKitTaskStatusResponse:
    return LaunchKitTaskStatusResponse(
        id=task.id,
        status=task.status,
        progress_stage=task.progress_stage,
        attempt_count=task.attempt_count,
        failure_category=task.failure_category,
        failure_detail=task.failure_detail,
        is_retryable=task.is_retryable,
        is_cancelable=task.is_cancelable,
        started_at=task.started_at,
        progress_updated_at=task.progress_updated_at,
        finished_at=task.finished_at,
        created_at=task.created_at,
        updated_at=task.updated_at,
    )


def _latest_quality_score_json(launch_kit: LaunchKit) -> dict[str, Any] | None:
    latest = max(launch_kit.quality_scores, key=lambda item: item.created_at, default=None)
    return latest.score_json if latest else None


def serialize_launch_kit_summary(launch_kit: LaunchKit) -> LaunchKitSummaryResponse:
    latest_task = latest_generation_task(launch_kit)
    return LaunchKitSummaryResponse(
        id=launch_kit.id,
        product_id=launch_kit.product_id,
        product_name=launch_kit.product.name,
        category_key=launch_kit.category_key,
        target_platforms=launch_kit.target_platforms_json,
        status=launch_kit.status,
        latest_task=serialize_launch_kit_task(latest_task) if latest_task else None,
        quality_score_summary=_latest_quality_score_json(launch_kit),
        created_at=launch_kit.created_at,
        updated_at=launch_kit.updated_at,
    )


def serialize_launch_kit_detail(launch_kit: LaunchKit) -> LaunchKitDetailResponse:
    summary = serialize_launch_kit_summary(launch_kit)
    return LaunchKitDetailResponse(
        **summary.model_dump(),
        buyer_angle_key=launch_kit.buyer_angle_key,
        source_references=launch_kit.source_references_json,
        generated_summary=launch_kit.generated_summary_json,
        selected_angle=launch_kit.selected_angle_json,
        export_snapshot=launch_kit.export_snapshot_json,
        seller_feedback=launch_kit.seller_feedback_json,
        variants=[
            {
                "id": item.id,
                "kind": item.kind,
                "platform": item.platform,
                "content": item.content_json,
                "score": item.score_json,
                "selected": item.selected,
                "created_at": item.created_at,
            }
            for item in launch_kit.variants
        ],
        exports=[
            {
                "id": item.id,
                "export_type": item.export_type,
                "status": item.status,
                "failure_reason": item.failure_reason,
                "created_at": item.created_at,
            }
            for item in launch_kit.exports
        ],
    )
