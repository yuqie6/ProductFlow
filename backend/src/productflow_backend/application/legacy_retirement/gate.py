from __future__ import annotations

import re
from collections.abc import Mapping
from dataclasses import dataclass
from datetime import UTC, datetime

import sqlalchemy as sa
from sqlalchemy.engine import Connection

from productflow_backend.domain.errors import BusinessValidationError, ConflictError

LEGACY_CUTOVER_GATE_TABLE = "legacy_cutover_gates"
LEGACY_CUTOVER_GATE_ID = "singleton"
LEGACY_CUTOVER_GATE_SCHEMA_VERSION = 1
LEGACY_CUTOVER_PHASE_PENDING = "pending"
LEGACY_CUTOVER_PHASE_READY = "ready_for_cleanup"
LEGACY_CUTOVER_PHASE_CLEANED = "cleaned"

_GATE_HASH_FIELDS = (
    "source_report_sha256",
    "archive_report_sha256",
    "canonical_report_sha256",
)
_HEX_SHA256 = re.compile(r"^[0-9a-f]{64}$")
_TERMINAL_WORKFLOW_RUN_STATUSES = ("succeeded", "failed", "cancelled")
_TERMINAL_CANVAS_RUN_STATUSES = ("completed", "failed", "cancelled")


@dataclass(frozen=True, slots=True)
class LegacyCutoverGateState:
    id: str
    schema_version: int
    phase: str
    source_profile: str | None
    source_report_sha256: str | None
    archive_report_sha256: str | None
    canonical_report_sha256: str | None
    backup_restore_verified_at: datetime | None
    active_run_count: int
    created_at: datetime
    updated_at: datetime


def read_legacy_cutover_gate(connection: Connection) -> LegacyCutoverGateState:
    row = _load_gate_row(connection)
    return _state_from_row(row)


def approve_legacy_cutover_gate(
    connection: Connection,
    *,
    source_profile: str,
    source_report_sha256: str,
    archive_report_sha256: str,
    canonical_report_sha256: str,
    backup_restore_verified_at: datetime,
    updated_at: datetime | None = None,
) -> LegacyCutoverGateState:
    """Persist external migration evidence after rechecking legacy execution state.

    The caller owns the transaction. This function only updates the singleton gate;
    it does not call providers, inspect storage bytes, or delete legacy rows.
    """
    _validate_evidence(
        source_profile=source_profile,
        source_report_sha256=source_report_sha256,
        archive_report_sha256=archive_report_sha256,
        canonical_report_sha256=canonical_report_sha256,
        backup_restore_verified_at=backup_restore_verified_at,
    )
    gate = _load_gate_row(connection, for_update=True)
    state = _state_from_row(gate)
    if state.phase == LEGACY_CUTOVER_PHASE_CLEANED:
        raise ConflictError("旧工作流切换证据已经标记为 cleaned，不能重新打开清理门槛")

    active_run_count = count_legacy_active_runs(connection)
    if active_run_count:
        raise ConflictError(f"仍有 {active_run_count} 条旧工作流执行记录未到终态，不能批准清理")

    timestamp = updated_at or datetime.now(UTC)
    connection.execute(
        sa.text(
            "UPDATE legacy_cutover_gates SET "
            "phase = :phase, source_profile = :source_profile, "
            "source_report_sha256 = :source_report_sha256, "
            "archive_report_sha256 = :archive_report_sha256, "
            "canonical_report_sha256 = :canonical_report_sha256, "
            "backup_restore_verified_at = :backup_restore_verified_at, "
            "active_run_count = 0, updated_at = :updated_at "
            "WHERE id = :id"
        ),
        {
            "id": LEGACY_CUTOVER_GATE_ID,
            "phase": LEGACY_CUTOVER_PHASE_READY,
            "source_profile": source_profile,
            "source_report_sha256": source_report_sha256,
            "archive_report_sha256": archive_report_sha256,
            "canonical_report_sha256": canonical_report_sha256,
            "backup_restore_verified_at": backup_restore_verified_at,
            "updated_at": timestamp,
        },
    )
    return read_legacy_cutover_gate(connection)


def assert_legacy_cutover_cleanup_ready(connection: Connection) -> LegacyCutoverGateState:
    """Guard any future destructive cleanup entrypoint with the persisted gate."""
    state = _state_from_row(_load_gate_row(connection, for_update=True))
    missing_evidence = [
        field_name
        for field_name in (
            "source_profile",
            "source_report_sha256",
            "archive_report_sha256",
            "canonical_report_sha256",
            "backup_restore_verified_at",
        )
        if getattr(state, field_name) is None
    ]
    if state.phase != LEGACY_CUTOVER_PHASE_READY or state.active_run_count != 0 or missing_evidence:
        details = ", ".join(missing_evidence) if missing_evidence else state.phase
        raise ConflictError(f"旧工作流清理门槛未通过: {details}")
    active_run_count = count_legacy_active_runs(connection)
    if active_run_count:
        raise ConflictError(f"旧工作流清理门槛批准后又出现 {active_run_count} 条未到终态的执行记录")
    return state


def mark_legacy_cutover_cleaned(
    connection: Connection,
    *,
    updated_at: datetime | None = None,
) -> LegacyCutoverGateState:
    """Record completion after a separately approved cleanup transaction."""
    assert_legacy_cutover_cleanup_ready(connection)
    connection.execute(
        sa.text(
            "UPDATE legacy_cutover_gates SET phase = :phase, updated_at = :updated_at WHERE id = :id"
        ),
        {
            "id": LEGACY_CUTOVER_GATE_ID,
            "phase": LEGACY_CUTOVER_PHASE_CLEANED,
            "updated_at": updated_at or datetime.now(UTC),
        },
    )
    return read_legacy_cutover_gate(connection)


def count_legacy_active_runs(connection: Connection) -> int:
    """Count active or unknown execution rows in retained V1 tables."""
    table_names = frozenset(sa.inspect(connection).get_table_names())
    count = 0
    if {"product_workflows", "workflow_runs"} <= table_names:
        count += int(
            connection.scalar(
                sa.text(
                    "SELECT COUNT(*) FROM workflow_runs AS runs "
                    "JOIN product_workflows AS workflows ON workflows.id = runs.workflow_id "
                    "WHERE workflows.schema_version = 1 "
                    "AND runs.status NOT IN ('succeeded', 'failed', 'cancelled')"
                )
            )
            or 0
        )
    if {"workflow_runs", "workflow_node_runs", "product_workflows"} <= table_names:
        count += int(
            connection.scalar(
                sa.text(
                    "SELECT COUNT(*) FROM workflow_node_runs AS node_runs "
                    "JOIN workflow_runs AS runs ON runs.id = node_runs.workflow_run_id "
                    "JOIN product_workflows AS workflows ON workflows.id = runs.workflow_id "
                    "WHERE workflows.schema_version = 1 "
                    "AND node_runs.status NOT IN ('idle', 'succeeded', 'failed', 'cancelled')"
                )
            )
            or 0
        )
    if {"canvas_agent_threads", "canvas_agent_runs"} <= table_names:
        count += int(
            connection.scalar(
                sa.text(
                    "SELECT COUNT(*) FROM canvas_agent_runs AS runs "
                    "JOIN canvas_agent_threads AS threads ON threads.id = runs.thread_id "
                    "WHERE runs.status NOT IN ('completed', 'failed', 'cancelled')"
                )
            )
            or 0
        )
    return count


def _validate_evidence(
    *,
    source_profile: str,
    source_report_sha256: str,
    archive_report_sha256: str,
    canonical_report_sha256: str,
    backup_restore_verified_at: datetime,
) -> None:
    if not source_profile.strip():
        raise BusinessValidationError("source profile 不能为空")
    for field_name, value in (
        ("source_report_sha256", source_report_sha256),
        ("archive_report_sha256", archive_report_sha256),
        ("canonical_report_sha256", canonical_report_sha256),
    ):
        if not _HEX_SHA256.fullmatch(value):
            raise BusinessValidationError(f"{field_name} 必须是 64 位小写 SHA-256")
    if backup_restore_verified_at.tzinfo is None:
        raise BusinessValidationError("backup restore 验证时间必须带时区")


def _load_gate_row(connection: Connection, *, for_update: bool = False) -> Mapping[str, object]:
    statement = "SELECT * FROM legacy_cutover_gates WHERE id = :id"
    if for_update and connection.dialect.name == "postgresql":
        statement += " FOR UPDATE"
    row = connection.execute(sa.text(statement), {"id": LEGACY_CUTOVER_GATE_ID}).mappings().first()
    if row is None:
        raise BusinessValidationError("数据库缺少 legacy cutover gate，不能执行旧工作流切换")
    return row


def _state_from_row(row: Mapping[str, object]) -> LegacyCutoverGateState:
    return LegacyCutoverGateState(
        id=str(row["id"]),
        schema_version=int(row["schema_version"]),
        phase=str(row["phase"]),
        source_profile=_optional_text(row.get("source_profile")),
        source_report_sha256=_optional_text(row.get("source_report_sha256")),
        archive_report_sha256=_optional_text(row.get("archive_report_sha256")),
        canonical_report_sha256=_optional_text(row.get("canonical_report_sha256")),
        backup_restore_verified_at=_as_datetime(row.get("backup_restore_verified_at")),
        active_run_count=int(row["active_run_count"]),
        created_at=_as_datetime(row["created_at"]) or datetime.min.replace(tzinfo=UTC),
        updated_at=_as_datetime(row["updated_at"]) or datetime.min.replace(tzinfo=UTC),
    )


def _optional_text(value: object) -> str | None:
    text = str(value).strip() if value is not None else ""
    return text or None


def _as_datetime(value: object) -> datetime | None:
    if value is None:
        return None
    if isinstance(value, datetime):
        return value if value.tzinfo is not None else value.replace(tzinfo=UTC)
    return datetime.fromisoformat(str(value)).replace(tzinfo=UTC)


__all__ = [
    "LEGACY_CUTOVER_GATE_ID",
    "LEGACY_CUTOVER_GATE_SCHEMA_VERSION",
    "LEGACY_CUTOVER_GATE_TABLE",
    "LEGACY_CUTOVER_PHASE_CLEANED",
    "LEGACY_CUTOVER_PHASE_PENDING",
    "LEGACY_CUTOVER_PHASE_READY",
    "LegacyCutoverGateState",
    "approve_legacy_cutover_gate",
    "assert_legacy_cutover_cleanup_ready",
    "count_legacy_active_runs",
    "mark_legacy_cutover_cleaned",
    "read_legacy_cutover_gate",
]
