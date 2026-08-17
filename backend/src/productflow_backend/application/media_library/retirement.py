from __future__ import annotations

import re
from collections.abc import Mapping
from dataclasses import dataclass
from datetime import UTC, datetime

import sqlalchemy as sa
from sqlalchemy.engine import Connection
from sqlalchemy.orm import Session

from productflow_backend.application.media_library.backfill import (
    capture_gallery_snapshot,
    gallery_reconciliation_hash,
)
from productflow_backend.application.time import now_utc
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.infrastructure.db.models import MediaLibraryAsset
from productflow_backend.infrastructure.storage import LocalStorage

MEDIA_LIBRARY_CUTOVER_GATE_TABLE = "media_library_cutover_gates"
MEDIA_LIBRARY_CUTOVER_GATE_ID = "singleton"
MEDIA_LIBRARY_CUTOVER_GATE_SCHEMA_VERSION = 1
MEDIA_LIBRARY_CUTOVER_PHASE_PENDING = "pending"
MEDIA_LIBRARY_CUTOVER_PHASE_READY = "ready_for_cleanup"
MEDIA_LIBRARY_CUTOVER_PHASE_CLEANED = "cleaned"
LEGACY_GALLERY_RETIRE_CONFIRMATION = "RETIRE_LEGACY_GALLERY"

_HEX_SHA256 = re.compile(r"^[0-9a-f]{64}$")
_LEGACY_GALLERY_TABLE = "image_gallery_entries"


@dataclass(frozen=True, slots=True)
class MediaLibraryCutoverGateState:
    id: str
    schema_version: int
    phase: str
    source_snapshot_token: str | None
    source_report_sha256: str | None
    reconciliation_report_sha256: str | None
    backup_restore_verified_at: datetime | None
    zero_delta_observed_at: datetime | None
    created_at: datetime
    updated_at: datetime


@dataclass(frozen=True, slots=True)
class LegacyGalleryRetirementReport:
    schema_version: int
    gate_phase: str
    source_snapshot_token: str
    source_report_sha256: str
    reconciliation_report_sha256: str
    source_row_count: int
    library_asset_count: int
    dropped: bool


def read_media_library_cutover_gate(connection: Connection) -> MediaLibraryCutoverGateState:
    return _state_from_row(_load_gate_row(connection))


def approve_media_library_cutover_gate(
    connection: Connection,
    *,
    source_snapshot_token: str,
    source_report_sha256: str,
    reconciliation_report_sha256: str,
    backup_restore_verified_at: datetime,
    zero_delta_observed_at: datetime,
    updated_at: datetime | None = None,
) -> MediaLibraryCutoverGateState:
    _validate_approval(
        source_snapshot_token=source_snapshot_token,
        source_report_sha256=source_report_sha256,
        reconciliation_report_sha256=reconciliation_report_sha256,
        backup_restore_verified_at=backup_restore_verified_at,
        zero_delta_observed_at=zero_delta_observed_at,
    )
    gate = _state_from_row(_load_gate_row(connection, for_update=True))
    if gate.phase == MEDIA_LIBRARY_CUTOVER_PHASE_CLEANED:
        raise ConflictError("素材库旧 Gallery 已经标记为 cleaned，不能重新打开清理闸门")
    if zero_delta_observed_at < backup_restore_verified_at:
        raise BusinessValidationError("zero-delta 观察时间不能早于备份恢复验证时间")
    timestamp = updated_at or now_utc()
    connection.execute(
        sa.text(
            "UPDATE media_library_cutover_gates SET "
            "phase = :phase, source_snapshot_token = :source_snapshot_token, "
            "source_report_sha256 = :source_report_sha256, "
            "reconciliation_report_sha256 = :reconciliation_report_sha256, "
            "backup_restore_verified_at = :backup_restore_verified_at, "
            "zero_delta_observed_at = :zero_delta_observed_at, updated_at = :updated_at "
            "WHERE id = :id"
        ),
        {
            "id": MEDIA_LIBRARY_CUTOVER_GATE_ID,
            "phase": MEDIA_LIBRARY_CUTOVER_PHASE_READY,
            "source_snapshot_token": source_snapshot_token.strip(),
            "source_report_sha256": source_report_sha256,
            "reconciliation_report_sha256": reconciliation_report_sha256,
            "backup_restore_verified_at": backup_restore_verified_at,
            "zero_delta_observed_at": zero_delta_observed_at,
            "updated_at": timestamp,
        },
    )
    return read_media_library_cutover_gate(connection)


def assert_media_library_cutover_cleanup_ready(connection: Connection) -> MediaLibraryCutoverGateState:
    state = _state_from_row(_load_gate_row(connection, for_update=True))
    missing = [
        field_name
        for field_name in (
            "source_snapshot_token",
            "source_report_sha256",
            "reconciliation_report_sha256",
            "backup_restore_verified_at",
            "zero_delta_observed_at",
        )
        if getattr(state, field_name) is None
    ]
    if state.phase != MEDIA_LIBRARY_CUTOVER_PHASE_READY or missing:
        details = ", ".join(missing) if missing else state.phase
        raise ConflictError(f"素材库旧 Gallery 清理闸门未通过: {details}")
    return state


def mark_media_library_cutover_cleaned(
    connection: Connection,
    *,
    updated_at: datetime | None = None,
) -> MediaLibraryCutoverGateState:
    state = assert_media_library_cutover_cleanup_ready(connection)
    if state.phase != MEDIA_LIBRARY_CUTOVER_PHASE_READY:
        raise ConflictError("素材库旧 Gallery 清理闸门状态不允许标记 cleaned")
    connection.execute(
        sa.text(
            "UPDATE media_library_cutover_gates SET phase = :phase, updated_at = :updated_at WHERE id = :id"
        ),
        {
            "id": MEDIA_LIBRARY_CUTOVER_GATE_ID,
            "phase": MEDIA_LIBRARY_CUTOVER_PHASE_CLEANED,
            "updated_at": updated_at or now_utc(),
        },
    )
    return read_media_library_cutover_gate(connection)


def inspect_legacy_gallery_retirement(
    session: Session,
    *,
    storage: LocalStorage,
) -> LegacyGalleryRetirementReport:
    snapshot = capture_gallery_snapshot(session)
    reconciliation_hash = gallery_reconciliation_hash(session, storage=storage, snapshot=snapshot)
    library_asset_count = int(
        session.scalar(
            sa.select(sa.func.count())
            .select_from(MediaLibraryAsset)
            .where(MediaLibraryAsset.source_type == "legacy_gallery")
        )
        or 0
    )
    gate = read_media_library_cutover_gate(session.connection())
    return LegacyGalleryRetirementReport(
        schema_version=1,
        gate_phase=gate.phase,
        source_snapshot_token=snapshot.snapshot_token,
        source_report_sha256=snapshot.source_hash,
        reconciliation_report_sha256=reconciliation_hash,
        source_row_count=snapshot.gallery_count,
        library_asset_count=library_asset_count,
        dropped=False,
    )


def retire_legacy_gallery(
    session: Session,
    *,
    storage: LocalStorage,
    confirmation: str,
) -> LegacyGalleryRetirementReport:
    if confirmation != LEGACY_GALLERY_RETIRE_CONFIRMATION:
        raise BusinessValidationError(
            f"旧 Gallery 清理需要明确确认字符串 {LEGACY_GALLERY_RETIRE_CONFIRMATION}"
        )
    connection = session.connection()
    gate = assert_media_library_cutover_cleanup_ready(connection)
    if not _table_exists(connection, _LEGACY_GALLERY_TABLE):
        raise ConflictError("旧 Gallery 表不存在，不能重复执行清理")

    _lock_legacy_gallery_table(connection)
    snapshot = capture_gallery_snapshot(session)
    if snapshot.source_hash != gate.source_report_sha256:
        raise ConflictError("旧 Gallery source snapshot 在清理前发生变化")
    reconciliation_hash = gallery_reconciliation_hash(session, storage=storage, snapshot=snapshot)
    if reconciliation_hash != gate.reconciliation_report_sha256:
        raise ConflictError("旧 Gallery canonical 映射在清理前发生变化")

    library_asset_count = int(
        session.scalar(
            sa.select(sa.func.count())
            .select_from(MediaLibraryAsset)
            .where(MediaLibraryAsset.source_type == "legacy_gallery")
        )
        or 0
    )
    session.execute(sa.text(f"DROP TABLE {_LEGACY_GALLERY_TABLE}"))
    mark_media_library_cutover_cleaned(connection)
    session.commit()
    return LegacyGalleryRetirementReport(
        schema_version=1,
        gate_phase=MEDIA_LIBRARY_CUTOVER_PHASE_CLEANED,
        source_snapshot_token=snapshot.snapshot_token,
        source_report_sha256=snapshot.source_hash,
        reconciliation_report_sha256=reconciliation_hash,
        source_row_count=snapshot.gallery_count,
        library_asset_count=library_asset_count,
        dropped=True,
    )


def _validate_approval(
    *,
    source_snapshot_token: str,
    source_report_sha256: str,
    reconciliation_report_sha256: str,
    backup_restore_verified_at: datetime,
    zero_delta_observed_at: datetime,
) -> None:
    normalized_token = source_snapshot_token.strip()
    if not normalized_token or len(normalized_token) > 255:
        raise BusinessValidationError("source snapshot token 不能为空且不能超过 255 个字符")
    for field_name, value in (
        ("source_report_sha256", source_report_sha256),
        ("reconciliation_report_sha256", reconciliation_report_sha256),
    ):
        if not _HEX_SHA256.fullmatch(value):
            raise BusinessValidationError(f"{field_name} 必须是 64 位小写 SHA-256")
    if backup_restore_verified_at.tzinfo is None or zero_delta_observed_at.tzinfo is None:
        raise BusinessValidationError("迁移证据时间必须带时区")


def _load_gate_row(connection: Connection, *, for_update: bool = False) -> Mapping[str, object]:
    statement = "SELECT * FROM media_library_cutover_gates WHERE id = :id"
    if for_update and connection.dialect.name == "postgresql":
        statement += " FOR UPDATE"
    row = connection.execute(sa.text(statement), {"id": MEDIA_LIBRARY_CUTOVER_GATE_ID}).mappings().first()
    if row is None:
        raise BusinessValidationError("数据库缺少 media library cutover gate，不能退休旧 Gallery")
    return row


def _state_from_row(row: Mapping[str, object]) -> MediaLibraryCutoverGateState:
    return MediaLibraryCutoverGateState(
        id=str(row["id"]),
        schema_version=int(row["schema_version"]),
        phase=str(row["phase"]),
        source_snapshot_token=_optional_text(row.get("source_snapshot_token")),
        source_report_sha256=_optional_text(row.get("source_report_sha256")),
        reconciliation_report_sha256=_optional_text(row.get("reconciliation_report_sha256")),
        backup_restore_verified_at=_as_datetime(row.get("backup_restore_verified_at")),
        zero_delta_observed_at=_as_datetime(row.get("zero_delta_observed_at")),
        created_at=_as_datetime(row.get("created_at")) or datetime.min.replace(tzinfo=UTC),
        updated_at=_as_datetime(row.get("updated_at")) or datetime.min.replace(tzinfo=UTC),
    )


def _table_exists(connection: Connection, table_name: str) -> bool:
    return table_name in sa.inspect(connection).get_table_names()


def _lock_legacy_gallery_table(connection: Connection) -> None:
    if connection.dialect.name == "postgresql":
        connection.exec_driver_sql(f"LOCK TABLE {_LEGACY_GALLERY_TABLE} IN ACCESS EXCLUSIVE MODE")


def _optional_text(value: object) -> str | None:
    text = str(value).strip() if value is not None else ""
    return text or None


def _as_datetime(value: object) -> datetime | None:
    if value is None:
        return None
    if isinstance(value, datetime):
        return value if value.tzinfo is not None else value.replace(tzinfo=UTC)
    parsed = datetime.fromisoformat(str(value))
    return parsed if parsed.tzinfo is not None else parsed.replace(tzinfo=UTC)


__all__ = [
    "LEGACY_GALLERY_RETIRE_CONFIRMATION",
    "MEDIA_LIBRARY_CUTOVER_GATE_ID",
    "MEDIA_LIBRARY_CUTOVER_GATE_SCHEMA_VERSION",
    "MEDIA_LIBRARY_CUTOVER_GATE_TABLE",
    "MEDIA_LIBRARY_CUTOVER_PHASE_CLEANED",
    "MEDIA_LIBRARY_CUTOVER_PHASE_PENDING",
    "MEDIA_LIBRARY_CUTOVER_PHASE_READY",
    "LegacyGalleryRetirementReport",
    "MediaLibraryCutoverGateState",
    "approve_media_library_cutover_gate",
    "assert_media_library_cutover_cleanup_ready",
    "inspect_legacy_gallery_retirement",
    "mark_media_library_cutover_cleaned",
    "read_media_library_cutover_gate",
    "retire_legacy_gallery",
]
