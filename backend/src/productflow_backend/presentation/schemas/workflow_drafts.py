from __future__ import annotations

from datetime import datetime
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from productflow_backend.application.product_intake import WorkflowIntakeV1, parse_workflow_intake
from productflow_backend.application.workflow_drafts.contracts import (
    WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE,
    WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS,
    WORKFLOW_DRAFT_MAX_TOTAL_IMAGES,
    WORKFLOW_DRAFT_MIN_IMAGE_TYPES,
    WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE,
    WorkflowDraftPayloadV1,
    parse_workflow_draft_payload,
)
from productflow_backend.domain.enums import WorkflowDraftStatus


class StrictRequestModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class WorkflowDraftLimitsResponse(BaseModel):
    min_image_types: int = WORKFLOW_DRAFT_MIN_IMAGE_TYPES
    min_images_per_type: int = WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE
    max_images_per_type: int = WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE
    max_total_images: int = WORKFLOW_DRAFT_MAX_TOTAL_IMAGES
    max_reference_assets: int = WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS


class CreateWorkflowDraftRequest(StrictRequestModel):
    payload: dict[str, Any] | None = None
    ready_for_confirmation: bool = False
    source_turn_id: str | None = Field(default=None, min_length=1, max_length=120)
    source_artifact_step_id: str | None = Field(default=None, min_length=1, max_length=120)


class AppendWorkflowDraftRevisionRequest(CreateWorkflowDraftRequest):
    expected_draft_version: int = Field(ge=0)


class ConfirmWorkflowDraftRequest(StrictRequestModel):
    expected_draft_version: int = Field(ge=1)


class WorkflowDraftRevisionResponse(BaseModel):
    id: str
    draft_id: str
    version: int
    schema_version: Literal[1]
    payload: WorkflowDraftPayloadV1
    payload_hash: str
    source_turn_id: str | None
    source_artifact_step_id: str | None
    confirmed_at: datetime | None
    fact_set_version_id: str | None
    visual_system_version_id: str | None
    created_at: datetime


class WorkflowDraftResponse(BaseModel):
    id: str
    product_id: str
    status: WorkflowDraftStatus
    current_revision_id: str | None
    current_revision: WorkflowDraftRevisionResponse | None
    current_version: int
    revisions: list[WorkflowDraftRevisionResponse]
    intake: WorkflowIntakeV1 | None
    recipe_seed: WorkflowDraftRecipeSeedResponse | None
    legacy_archive_seed: WorkflowDraftLegacyArchiveSeedResponse | None
    limits: WorkflowDraftLimitsResponse
    created_at: datetime
    updated_at: datetime


class WorkflowDraftRecipeSeedResponse(BaseModel):
    id: str
    workflow_draft_id: str
    recipe_version_id: str
    recipe_id: str
    recipe_version: int
    recipe_title: str
    product_id: str
    schema_version: Literal[1]
    created_at: datetime


class WorkflowDraftLegacyArchiveSeedResponse(BaseModel):
    id: str
    workflow_draft_id: str
    product_id: str
    archive_kind: Literal["workflow", "canvas_agent_thread", "user_template"]
    archive_id: str
    archive_title: str
    archive_status: str | None
    source_product_id: str | None
    source_profile: str
    archive_schema_version: int
    payload_sha256: str
    counts: dict[str, int]
    schema_version: Literal[1]
    created_at: datetime

def serialize_workflow_draft_revision(revision: Any) -> WorkflowDraftRevisionResponse:
    return WorkflowDraftRevisionResponse(
        id=revision.id,
        draft_id=revision.draft_id,
        version=revision.version,
        schema_version=revision.schema_version,
        payload=parse_workflow_draft_payload(revision.payload_json),
        payload_hash=revision.payload_hash,
        source_turn_id=revision.source_turn_id,
        source_artifact_step_id=revision.source_artifact_step_id,
        confirmed_at=revision.confirmed_at,
        fact_set_version_id=revision.fact_set_version.id if revision.fact_set_version is not None else None,
        visual_system_version_id=revision.visual_system_version_id,
        created_at=revision.created_at,
    )


def serialize_workflow_draft(draft: Any) -> WorkflowDraftResponse:
    from productflow_backend.application.legacy_archive_rebuilds import legacy_archive_seed_summary

    current_revision = draft.current_revision
    seed = draft.recipe_seed
    archive_seed = draft.legacy_archive_seed
    return WorkflowDraftResponse(
        id=draft.id,
        product_id=draft.product_id,
        status=draft.status,
        current_revision_id=draft.current_revision_id,
        current_revision=(
            serialize_workflow_draft_revision(current_revision) if current_revision is not None else None
        ),
        current_version=current_revision.version if current_revision is not None else 0,
        revisions=[serialize_workflow_draft_revision(revision) for revision in draft.revisions],
        intake=parse_workflow_intake(
            schema_version=draft.intake_schema_version,
            payload=draft.intake_json,
        ),
        recipe_seed=(
            WorkflowDraftRecipeSeedResponse(
                id=seed.id,
                workflow_draft_id=seed.workflow_draft_id,
                recipe_version_id=seed.recipe_version_id,
                recipe_id=seed.recipe_version.recipe_id,
                recipe_version=seed.recipe_version.version,
                recipe_title=seed.recipe_version.title,
                product_id=seed.product_id,
                schema_version=seed.schema_version,
                created_at=seed.created_at,
            )
            if seed is not None
            else None
        ),
        legacy_archive_seed=(
            WorkflowDraftLegacyArchiveSeedResponse.model_validate(legacy_archive_seed_summary(archive_seed))
            if archive_seed is not None
            else None
        ),
        limits=WorkflowDraftLimitsResponse(),
        created_at=draft.created_at,
        updated_at=draft.updated_at,
    )


