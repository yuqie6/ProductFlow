from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel, ConfigDict, Field

from productflow_backend.domain.enums import LibraryOrganizationDraftStatus
from productflow_backend.infrastructure.db.models import (
    LibraryOrganizationDraft,
    LibraryOrganizationDraftRevision,
)


class ConfirmLibraryOrganizationDraftRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    expected_draft_version: int = Field(ge=1)
    idempotency_key: str = Field(min_length=1, max_length=200)


class LibraryOrganizationDraftRevisionResponse(BaseModel):
    id: str
    version: int
    schema_version: int
    payload: dict[str, Any]
    payload_hash: str
    source_turn_id: str | None
    source_artifact_step_id: str | None
    confirmed_at: datetime | None
    created_at: datetime


class LibraryOrganizationDraftResponse(BaseModel):
    id: str
    conversation_id: str
    status: LibraryOrganizationDraftStatus
    current_revision: LibraryOrganizationDraftRevisionResponse | None
    confirmed_revision_id: str | None
    confirmation_result: dict[str, Any] | None
    confirmed_at: datetime | None
    created_at: datetime
    updated_at: datetime


def serialize_library_organization_draft_revision(
    revision: LibraryOrganizationDraftRevision | None,
) -> LibraryOrganizationDraftRevisionResponse | None:
    if revision is None:
        return None
    return LibraryOrganizationDraftRevisionResponse(
        id=revision.id,
        version=revision.version,
        schema_version=revision.schema_version,
        payload=dict(revision.payload_json),
        payload_hash=revision.payload_hash,
        source_turn_id=revision.source_turn_id,
        source_artifact_step_id=revision.source_artifact_step_id,
        confirmed_at=revision.confirmed_at,
        created_at=revision.created_at,
    )


def serialize_library_organization_draft(
    draft: LibraryOrganizationDraft,
) -> LibraryOrganizationDraftResponse:
    return LibraryOrganizationDraftResponse(
        id=draft.id,
        conversation_id=draft.conversation_id,
        status=draft.status,
        current_revision=serialize_library_organization_draft_revision(draft.current_revision),
        confirmed_revision_id=draft.confirmed_revision_id,
        confirmation_result=(
            dict(draft.confirmation_result_json) if draft.confirmation_result_json is not None else None
        ),
        confirmed_at=draft.confirmed_at,
        created_at=draft.created_at,
        updated_at=draft.updated_at,
    )
