from __future__ import annotations

from datetime import datetime
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from productflow_backend.application.legacy_retirement.contracts import canonical_sha256

LEGACY_GALLERY_BRIDGE_SCHEMA_VERSION = 1
LEGACY_GALLERY_BRIDGE_SOURCE_PROFILE = "legacy_canvas_agent_20260518_0032"

LegacyGalleryBridgeBlockerCode = Literal[
    "gallery_created_at_missing",
    "source_asset_missing",
    "source_file_missing",
    "source_path_invalid",
    "source_image_invalid",
    "source_file_changed",
    "source_mime_mismatch",
    "target_mapping_conflict",
    "target_media_conflict",
]
LegacyGalleryBridgeItemStatus = Literal["would_create", "created", "unchanged", "blocked"]


class _FrozenContract(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)


class LegacyGalleryBridgeRecord(_FrozenContract):
    gallery_entry_id: str = Field(min_length=1, max_length=80)
    image_session_asset_id: str | None = Field(default=None, max_length=80)
    image_session_round_id: str | None = Field(default=None, max_length=80)
    gallery_created_at: datetime | None = None
    asset_created_at: datetime | None = None
    original_filename: str = Field(min_length=1, max_length=255)
    mime_type: str = Field(min_length=1, max_length=100)
    storage_path: str = Field(max_length=500)
    source_file_sha256: str | None = Field(default=None, pattern=r"^[0-9a-f]{64}$")
    source_byte_size: int | None = Field(default=None, ge=1)
    width: int | None = Field(default=None, ge=1)
    height: int | None = Field(default=None, ge=1)
    blocker_code: LegacyGalleryBridgeBlockerCode | None = None


class LegacyGalleryBridgeManifest(_FrozenContract):
    schema_version: Literal[1] = LEGACY_GALLERY_BRIDGE_SCHEMA_VERSION
    generated_at: datetime
    source_profile: Literal["legacy_canvas_agent_20260518_0032"] = LEGACY_GALLERY_BRIDGE_SOURCE_PROFILE
    source_revisions: list[str] = Field(min_length=1)
    source_report_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")
    source_snapshot_token: str = Field(pattern=r"^[0-9a-f]{64}$")
    records: list[LegacyGalleryBridgeRecord]
    manifest_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")


class LegacyGalleryBridgeItemResult(_FrozenContract):
    gallery_entry_id: str
    status: LegacyGalleryBridgeItemStatus
    target_media_object_id: str | None = None
    target_library_asset_id: str | None = None
    diagnostic_codes: list[str] = Field(default_factory=list)


class LegacyGalleryBridgeReport(_FrozenContract):
    schema_version: Literal[1] = LEGACY_GALLERY_BRIDGE_SCHEMA_VERSION
    generated_at: datetime
    apply_requested: bool
    applied: bool
    source_manifest_sha256: str
    source_report_sha256: str
    source_snapshot_token: str
    target_reconciliation_sha256: str | None = Field(default=None, pattern=r"^[0-9a-f]{64}$")
    scanned: int = Field(ge=0)
    created_count: int = Field(ge=0)
    unchanged_count: int = Field(ge=0)
    blocked_count: int = Field(ge=0)
    items: list[LegacyGalleryBridgeItemResult]
    report_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")


class LegacyGalleryBridgeApproval(_FrozenContract):
    schema_version: Literal[1] = LEGACY_GALLERY_BRIDGE_SCHEMA_VERSION
    approved_at: datetime
    source_manifest_sha256: str
    source_report_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")
    source_snapshot_token: str = Field(pattern=r"^[0-9a-f]{64}$")
    target_reconciliation_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")
    backup_restore_verified_at: datetime
    zero_delta_observed_at: datetime
    approval_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")


def gallery_bridge_manifest_sha256(manifest: LegacyGalleryBridgeManifest) -> str:
    payload = manifest.model_dump(mode="json", exclude={"generated_at", "manifest_sha256"})
    return canonical_sha256(payload)


def gallery_bridge_report_sha256(report: LegacyGalleryBridgeReport) -> str:
    payload = report.model_dump(mode="json", exclude={"generated_at", "report_sha256"})
    return canonical_sha256(payload)


def gallery_bridge_approval_sha256(approval: LegacyGalleryBridgeApproval) -> str:
    payload: dict[str, Any] = approval.model_dump(mode="json", exclude={"approved_at", "approval_sha256"})
    return canonical_sha256(payload)


__all__ = [
    "LEGACY_GALLERY_BRIDGE_SCHEMA_VERSION",
    "LEGACY_GALLERY_BRIDGE_SOURCE_PROFILE",
    "LegacyGalleryBridgeApproval",
    "LegacyGalleryBridgeBlockerCode",
    "LegacyGalleryBridgeItemResult",
    "LegacyGalleryBridgeManifest",
    "LegacyGalleryBridgeRecord",
    "LegacyGalleryBridgeReport",
    "gallery_bridge_approval_sha256",
    "gallery_bridge_manifest_sha256",
    "gallery_bridge_report_sha256",
]
