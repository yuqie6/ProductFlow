from __future__ import annotations

import json
from datetime import datetime
from typing import Any

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from productflow_backend.application.local_image_edits.contracts import (
    LocalEditMaskGeometry,
    LocalImageEditDraft,
)
from productflow_backend.domain.enums import LocalImageEditTaskStatus
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.local_image_edits import LocalImageEditOperation
from productflow_backend.infrastructure.image.base import LocalEditCapability
from productflow_backend.presentation.schemas.products import (
    ProductImageAssetResponse,
    serialize_product_image_asset,
)


class SubmitLocalImageEditRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    idempotency_key: str = Field(min_length=1, max_length=120)


class LocalImageEditRevisionRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    expected_revision: int | None = Field(default=None, ge=1)


class AdoptLocalImageEditRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    expected_current_artifact_id: str = Field(min_length=1, max_length=36)


class LocalImageEditProviderAttemptResponse(BaseModel):
    id: str
    attempt_id: str
    attempt_number: int
    operation_key: str
    phase: str
    effect_result: str
    provider_name: str
    provider_model: str | None = None
    provider_response_id: str | None = None
    provider_status: str | None = None
    late_result_asset: ProductImageAssetResponse | None = None
    detail: str | None = None
    created_at: datetime
    updated_at: datetime


class LocalImageEditAdoptionEventResponse(BaseModel):
    id: str
    task_id: str
    graph_id: str
    node_id: str
    event_type: str
    from_artifact_id: str
    to_artifact_id: str
    related_event_id: str | None = None
    created_at: datetime


class LocalImageEditTaskResponse(BaseModel):
    id: str
    product_id: str
    status: LocalImageEditTaskStatus
    revision: int
    operation: LocalImageEditOperation
    instruction: str | None = None
    source_text: str | None = None
    replacement_text: str | None = None
    mask_geometry: LocalEditMaskGeometry
    source_media_sha256: str
    source_asset: ProductImageAssetResponse
    result_asset: ProductImageAssetResponse | None = None
    references: list[ProductImageAssetResponse] = Field(default_factory=list)
    reference_asset_ids: list[str] = Field(default_factory=list)
    target_graph_id: str | None = None
    target_node_id: str | None = None
    target_graph_revision: int | None = None
    source_artifact_id: str | None = None
    source_artifact_asset_id: str | None = None
    source_artifact_input_digest: str | None = None
    idempotency_key: str | None = None
    request_hash: str | None = None
    requested_provider_name: str | None = None
    requested_local_edit_mode: str | None = None
    attempts: int
    active_attempt_id: str | None = None
    progress_phase: str | None = None
    failure_reason: str | None = None
    is_retryable: bool
    is_cancelable: bool
    provider_name: str | None = None
    provider_model: str | None = None
    provider_response_id: str | None = None
    provider_status: str | None = None
    provider_attempts: list[LocalImageEditProviderAttemptResponse] = Field(default_factory=list)
    adoption_events: list[LocalImageEditAdoptionEventResponse] = Field(default_factory=list)
    created_at: datetime
    updated_at: datetime
    queued_at: datetime | None = None
    started_at: datetime | None = None
    finished_at: datetime | None = None


class LocalImageEditTaskListResponse(BaseModel):
    items: list[LocalImageEditTaskResponse]


class LocalImageEditCapabilityResponse(BaseModel):
    provider_name: str
    supported: bool
    mode: str | None = None
    operations: list[LocalImageEditOperation] = Field(default_factory=list)
    requires_mask: bool
    max_reference_images: int
    reason: str | None = None


def parse_local_image_edit_draft(
    *,
    operation: str,
    instruction: str | None,
    source_text: str | None,
    replacement_text: str | None,
    mask_geometry_json: str | None,
    reference_asset_ids_json: str | None,
) -> LocalImageEditDraft:
    """在应用合同运行之前解析结构化 JSON 表单字段。"""

    if not mask_geometry_json:
        raise BusinessValidationError("局部编辑 mask_geometry JSON 不能为空")
    try:
        mask_geometry = json.loads(mask_geometry_json)
        reference_asset_ids = json.loads(reference_asset_ids_json or "[]")
        if not isinstance(reference_asset_ids, list):
            raise ValueError("reference_asset_ids 必须是 JSON 数组")
        return LocalImageEditDraft.model_validate(
            {
                "operation": operation,
                "instruction": instruction,
                "source_text": source_text,
                "replacement_text": replacement_text,
                "mask_geometry": mask_geometry,
                "reference_asset_ids": reference_asset_ids,
            }
        )
    except (json.JSONDecodeError, TypeError, ValueError, ValidationError) as exc:
        raise BusinessValidationError("局部编辑 draft 参数无效") from exc


def serialize_local_image_edit_task(
    task: Any,
    *,
    include_audit: bool = True,
) -> LocalImageEditTaskResponse:
    references = sorted(task.references, key=lambda item: (item.sort_order, item.asset_id))
    provider_attempts = (
        getattr(task, "_local_image_edit_provider_attempts_projection", [])
        if include_audit
        else []
    )
    adoption_events = (
        getattr(task, "_local_image_edit_adoption_events_projection", [])
        if include_audit
        else []
    )
    return LocalImageEditTaskResponse(
        id=task.id,
        product_id=task.product_id,
        status=task.status,
        revision=task.revision,
        operation=LocalImageEditOperation(task.operation),
        instruction=task.instruction,
        source_text=task.source_text,
        replacement_text=task.replacement_text,
        mask_geometry=LocalEditMaskGeometry.model_validate(task.mask_geometry_json),
        source_media_sha256=task.source_media_sha256,
        source_asset=serialize_product_image_asset(task.source_asset),
        result_asset=(serialize_product_image_asset(task.result_asset) if task.result_asset is not None else None),
        references=[serialize_product_image_asset(item.asset) for item in references],
        reference_asset_ids=[item.asset_id for item in references],
        target_graph_id=task.target_graph_id,
        target_node_id=task.target_node_id,
        target_graph_revision=task.target_graph_revision,
        source_artifact_id=task.source_artifact_id,
        source_artifact_asset_id=task.source_artifact_asset_id,
        source_artifact_input_digest=task.source_artifact_input_digest,
        idempotency_key=task.idempotency_key,
        request_hash=task.request_hash,
        requested_provider_name=task.requested_provider_name,
        requested_local_edit_mode=task.requested_local_edit_mode,
        attempts=task.attempts,
        active_attempt_id=task.active_attempt_id,
        progress_phase=task.progress_phase,
        failure_reason=task.failure_reason,
        is_retryable=task.is_retryable,
        is_cancelable=task.status
        in {LocalImageEditTaskStatus.DRAFT, LocalImageEditTaskStatus.QUEUED, LocalImageEditTaskStatus.RUNNING},
        provider_name=task.provider_name,
        provider_model=task.provider_model,
        provider_response_id=task.provider_response_id,
        provider_status=task.provider_status,
        provider_attempts=[serialize_local_image_edit_attempt(item) for item in provider_attempts],
        adoption_events=[serialize_local_image_edit_adoption_event(item) for item in adoption_events],
        created_at=task.created_at,
        updated_at=task.updated_at,
        queued_at=task.queued_at,
        started_at=task.started_at,
        finished_at=task.finished_at,
    )


def serialize_local_image_edit_attempt(
    attempt: Any,
) -> LocalImageEditProviderAttemptResponse:
    return LocalImageEditProviderAttemptResponse(
        id=attempt.id,
        attempt_id=attempt.attempt_id,
        attempt_number=attempt.attempt_number,
        operation_key=attempt.operation_key,
        phase=attempt.phase,
        effect_result=attempt.effect_result,
        provider_name=attempt.provider_name,
        provider_model=attempt.provider_model,
        provider_response_id=attempt.provider_response_id,
        provider_status=attempt.provider_status,
        late_result_asset=(
            serialize_product_image_asset(attempt.late_result_asset)
            if attempt.late_result_asset is not None
            else None
        ),
        detail=attempt.detail,
        created_at=attempt.created_at,
        updated_at=attempt.updated_at,
    )


def serialize_local_image_edit_adoption_event(
    event: Any,
) -> LocalImageEditAdoptionEventResponse:
    return LocalImageEditAdoptionEventResponse(
        id=event.id,
        task_id=event.task_id,
        graph_id=event.graph_id,
        node_id=event.node_id,
        event_type=event.event_type,
        from_artifact_id=event.from_artifact_id,
        to_artifact_id=event.to_artifact_id,
        related_event_id=event.related_event_id,
        created_at=event.created_at,
    )


def serialize_local_image_edit_capability(
    capability: LocalEditCapability,
) -> LocalImageEditCapabilityResponse:
    return LocalImageEditCapabilityResponse(
        provider_name=capability.provider_name,
        supported=capability.supported,
        mode=capability.mode,
        operations=list(capability.operations),
        requires_mask=capability.requires_mask,
        max_reference_images=capability.max_reference_images,
        reason=capability.reason,
    )


__all__ = [
    "AdoptLocalImageEditRequest",
    "LocalImageEditCapabilityResponse",
    "LocalImageEditRevisionRequest",
    "LocalImageEditTaskListResponse",
    "LocalImageEditTaskResponse",
    "SubmitLocalImageEditRequest",
    "parse_local_image_edit_draft",
    "serialize_local_image_edit_capability",
    "serialize_local_image_edit_task",
]
