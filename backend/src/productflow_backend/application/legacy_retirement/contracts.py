from __future__ import annotations

import hashlib
import json
from collections.abc import Mapping
from datetime import date, datetime
from decimal import Decimal
from enum import Enum
from pathlib import Path
from typing import Any, Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field

LEGACY_RETIREMENT_REPORT_SCHEMA_VERSION = 1
SchemaProfile = Literal[
    "legacy_canvas_agent_20260518_0032",
    "current_canonical_20260814_0038",
    "current_with_legacy_archives_20260815_0039",
    "unknown",
]
AuditIssueSeverity = Literal["warning", "blocking"]


class _FrozenContract(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)


class AuditIssue(_FrozenContract):
    code: str
    severity: AuditIssueSeverity
    count: int = Field(ge=1)
    detail: str


class DatabaseSourceSummary(_FrozenContract):
    dialect: str
    read_only_enforced: bool
    alembic_revisions: list[str]
    schema_profile: SchemaProfile
    profile_diagnostics: list[str]
    key_table_columns: dict[str, list[str]]
    table_counts: dict[str, int]


class WorkflowSourceSummary(_FrozenContract):
    workflow_id: str
    product_id: str
    schema_version: int | None = None
    active: bool
    archive_candidate: bool
    node_count: int = Field(ge=0)
    edge_count: int = Field(ge=0)
    run_count: int = Field(ge=0)
    node_run_count: int = Field(ge=0)
    legacy_asset_count: int = Field(ge=0)
    canonical_asset_count: int = Field(ge=0)
    node_status_counts: dict[str, int]
    run_status_counts: dict[str, int]
    node_run_status_counts: dict[str, int]
    source_fingerprint_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")


class WorkflowAuditSummary(_FrozenContract):
    workflow_count: int = Field(ge=0)
    archive_candidate_count: int = Field(ge=0)
    active_workflow_count: int = Field(ge=0)
    node_type_counts: dict[str, int]
    node_status_counts: dict[str, int]
    run_status_counts: dict[str, int]
    node_run_status_counts: dict[str, int]
    items: list[WorkflowSourceSummary]


class UserTemplateSourceSummary(_FrozenContract):
    template_id: str
    key: str
    schema_version: int | None = None
    archived: bool
    source_fingerprint_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")


class UserTemplateAuditSummary(_FrozenContract):
    template_count: int = Field(ge=0)
    archived_count: int = Field(ge=0)
    items: list[UserTemplateSourceSummary]


class CanvasAgentThreadSourceSummary(_FrozenContract):
    thread_id: str
    product_id: str
    status: str
    message_count: int = Field(ge=0)
    run_count: int = Field(ge=0)
    tool_event_count: int = Field(ge=0)
    plan_count: int = Field(ge=0)
    task_plan_count: int = Field(ge=0)
    timeline_event_count: int = Field(ge=0)
    visible_event_count: int = Field(ge=0)
    technical_event_count: int = Field(ge=0)
    runs_without_terminal_evidence: int = Field(ge=0)
    run_status_counts: dict[str, int]
    event_type_counts: dict[str, int]
    source_fingerprint_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")


class CanvasAgentAuditSummary(_FrozenContract):
    present: bool
    thread_count: int = Field(ge=0)
    message_count: int = Field(ge=0)
    run_count: int = Field(ge=0)
    tool_event_count: int = Field(ge=0)
    plan_count: int = Field(ge=0)
    task_plan_count: int = Field(ge=0)
    timeline_event_count: int = Field(ge=0)
    thread_status_counts: dict[str, int]
    run_status_counts: dict[str, int]
    tool_status_counts: dict[str, int]
    plan_status_counts: dict[str, int]
    task_plan_status_counts: dict[str, int]
    visible_event_type_counts: dict[str, int]
    technical_event_type_counts: dict[str, int]
    source_bytes_by_table: dict[str, int]
    items: list[CanvasAgentThreadSourceSummary]


class MediaPathProblem(_FrozenContract):
    record_type: str
    record_id: str
    storage_path: str
    reason: Literal["missing", "invalid_path"]


class MediaAuditSummary(_FrozenContract):
    storage_root: str
    storage_root_exists: bool
    declared_record_count: int = Field(ge=0)
    unique_path_count: int = Field(ge=0)
    present_record_count: int = Field(ge=0)
    missing_record_count: int = Field(ge=0)
    invalid_path_record_count: int = Field(ge=0)
    verification_status_counts: dict[str, int]
    problems: list[MediaPathProblem]


class LegacyRetirementAuditReport(_FrozenContract):
    schema_version: Literal[1] = LEGACY_RETIREMENT_REPORT_SCHEMA_VERSION
    generated_at: datetime
    source: DatabaseSourceSummary
    workflows: WorkflowAuditSummary
    user_templates: UserTemplateAuditSummary
    canvas_agent: CanvasAgentAuditSummary
    media: MediaAuditSummary
    integrity_counts: dict[str, int]
    issues: list[AuditIssue]
    ready_for_archive: bool
    source_fingerprint_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")
    report_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")


def canonical_json_bytes(value: Any) -> bytes:
    return json.dumps(
        _canonical_json_value(value),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
        allow_nan=False,
    ).encode("utf-8")


def canonical_sha256(value: Any) -> str:
    return hashlib.sha256(canonical_json_bytes(value)).hexdigest()


def report_sha256(report: LegacyRetirementAuditReport) -> str:
    payload = report.model_dump(
        mode="json",
        exclude={"generated_at", "report_sha256"},
    )
    return canonical_sha256(payload)


def _canonical_json_value(value: Any) -> Any:
    if value is None or isinstance(value, (str, int, bool)):
        return value
    if isinstance(value, float):
        return value
    if isinstance(value, Decimal):
        return format(value, "f")
    if isinstance(value, datetime):
        return value.isoformat()
    if isinstance(value, date):
        return value.isoformat()
    if isinstance(value, (UUID, Path)):
        return str(value)
    if isinstance(value, Enum):
        return _canonical_json_value(value.value)
    if isinstance(value, bytes):
        return {"byte_size": len(value), "sha256": hashlib.sha256(value).hexdigest()}
    if isinstance(value, memoryview):
        content = value.tobytes()
        return {"byte_size": len(content), "sha256": hashlib.sha256(content).hexdigest()}
    if isinstance(value, Mapping):
        return {
            str(key): _canonical_json_value(item)
            for key, item in sorted(value.items(), key=lambda pair: str(pair[0]))
        }
    if isinstance(value, (list, tuple)):
        return [_canonical_json_value(item) for item in value]
    raise TypeError(f"unsupported canonical JSON value: {type(value).__name__}")


__all__ = [
    "LEGACY_RETIREMENT_REPORT_SCHEMA_VERSION",
    "AuditIssue",
    "CanvasAgentAuditSummary",
    "CanvasAgentThreadSourceSummary",
    "DatabaseSourceSummary",
    "LegacyRetirementAuditReport",
    "MediaAuditSummary",
    "MediaPathProblem",
    "SchemaProfile",
    "UserTemplateAuditSummary",
    "UserTemplateSourceSummary",
    "WorkflowAuditSummary",
    "WorkflowSourceSummary",
    "canonical_json_bytes",
    "canonical_sha256",
    "report_sha256",
]
