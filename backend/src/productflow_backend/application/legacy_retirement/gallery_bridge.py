from __future__ import annotations

from dataclasses import dataclass
from datetime import UTC, datetime
from hashlib import sha256
from pathlib import Path, PurePosixPath
from typing import Any
from uuid import NAMESPACE_URL, uuid5

import sqlalchemy as sa
from sqlalchemy.engine import Connection, Engine
from sqlalchemy.orm import Session

from productflow_backend.application.legacy_retirement.contracts import canonical_sha256
from productflow_backend.application.legacy_retirement.gallery_bridge_contracts import (
    LEGACY_GALLERY_BRIDGE_SOURCE_PROFILE,
    LegacyGalleryBridgeApproval,
    LegacyGalleryBridgeItemResult,
    LegacyGalleryBridgeManifest,
    LegacyGalleryBridgeRecord,
    LegacyGalleryBridgeReport,
    gallery_bridge_approval_sha256,
    gallery_bridge_manifest_sha256,
    gallery_bridge_report_sha256,
)
from productflow_backend.application.media_assets import VerifiedImageMetadata, inspect_image_bytes
from productflow_backend.application.storage_compensation import compensate_storage_writes
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import MediaVerificationStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.infrastructure.db.models import MediaLibraryAsset, MediaObject
from productflow_backend.infrastructure.storage import LocalStorage

LEGACY_GALLERY_TABLE = "image_gallery_entries"
LEGACY_SESSION_ASSET_TABLE = "image_session_assets"
LEGACY_GALLERY_SOURCE_REVISION = "20260518_0032"


@dataclass(frozen=True, slots=True)
class LegacyGallerySourceRetirementReport:
    schema_version: int
    source_manifest_sha256: str
    source_report_sha256: str
    source_snapshot_token: str
    target_reconciliation_sha256: str
    source_row_count: int
    dropped: bool


@dataclass(frozen=True, slots=True)
class _TargetPlan:
    record: LegacyGalleryBridgeRecord
    metadata: VerifiedImageMetadata | None
    status: str
    diagnostic_codes: tuple[str, ...]
    target_media_object_id: str | None = None
    target_library_asset_id: str | None = None
    provenance_hash: str | None = None


def export_legacy_gallery_bridge_manifest(
    engine: Engine,
    *,
    storage_root: Path,
    generated_at: datetime | None = None,
) -> LegacyGalleryBridgeManifest:
    """Create a read-only, Gallery-only source manifest for the old Canvas schema."""

    from productflow_backend.application.legacy_retirement.source import open_legacy_read_only_connection

    timestamp = generated_at or now_utc()
    with open_legacy_read_only_connection(engine) as connection:
        return capture_legacy_gallery_bridge_manifest_connection(
            connection,
            storage_root=storage_root,
            generated_at=timestamp,
        )


def capture_legacy_gallery_bridge_manifest_connection(
    connection: Connection,
    *,
    storage_root: Path,
    generated_at: datetime,
) -> LegacyGalleryBridgeManifest:
    revisions, tables = _ensure_legacy_gallery_source_schema(connection)
    gallery = tables[LEGACY_GALLERY_TABLE]
    session_assets = tables[LEGACY_SESSION_ASSET_TABLE]
    asset_rows = {
        str(row["id"]): dict(row)
        for row in connection.execute(sa.select(session_assets)).mappings()
    }
    gallery_rows = connection.execute(sa.select(gallery).order_by(gallery.c.id)).mappings()
    records = [
        _build_bridge_record(
            dict(row),
            asset_rows.get(str(row.get("image_session_asset_id")))
            if row.get("image_session_asset_id") is not None
            else None,
            storage_root=storage_root,
        )
        for row in gallery_rows
    ]
    source_report_sha256 = canonical_sha256(
        {
            "schema_version": 1,
            "source_profile": LEGACY_GALLERY_BRIDGE_SOURCE_PROFILE,
            "source_revisions": revisions,
            "records": [
                record.model_dump(
                    mode="json",
                    exclude={"source_file_sha256", "source_byte_size", "width", "height"},
                )
                for record in records
            ],
        }
    )
    source_snapshot_token = canonical_sha256(
        {
            "schema_version": 1,
            "source_report_sha256": source_report_sha256,
            "files": [
                {
                    "gallery_entry_id": record.gallery_entry_id,
                    "storage_path": record.storage_path,
                    "source_file_sha256": record.source_file_sha256,
                    "source_byte_size": record.source_byte_size,
                    "width": record.width,
                    "height": record.height,
                    "blocker_code": record.blocker_code,
                }
                for record in records
            ],
        }
    )
    provisional = LegacyGalleryBridgeManifest(
        generated_at=generated_at,
        source_revisions=revisions,
        source_report_sha256=source_report_sha256,
        source_snapshot_token=source_snapshot_token,
        records=records,
        manifest_sha256="0" * 64,
    )
    return provisional.model_copy(update={"manifest_sha256": gallery_bridge_manifest_sha256(provisional)})


def run_legacy_gallery_bridge(
    session: Session,
    *,
    manifest: LegacyGalleryBridgeManifest,
    source_storage_root: Path,
    target_storage: LocalStorage,
    expected_source_report_sha256: str,
    apply: bool = False,
    generated_at: datetime | None = None,
) -> LegacyGalleryBridgeReport:
    _validate_manifest(manifest, expected_source_report_sha256=expected_source_report_sha256)
    _ensure_target_schema(session)
    plans = [
        _plan_target_record(
            session,
            record=record,
            source_storage_root=source_storage_root,
            target_storage=target_storage,
        )
        for record in manifest.records
    ]
    blocked = sum(plan.status == "blocked" for plan in plans)
    can_apply = blocked == 0
    applied = False
    if apply and can_apply:
        with compensate_storage_writes(session) as storage_writes:
            for plan in plans:
                if plan.status != "would_create":
                    continue
                _create_target_record(
                    session,
                    plan=plan,
                    source_storage_root=source_storage_root,
                    target_storage=target_storage,
                    storage_writes=storage_writes,
                )
            session.commit()
            storage_writes.release()
        applied = True

    item_results = [
        LegacyGalleryBridgeItemResult(
            gallery_entry_id=plan.record.gallery_entry_id,
            status=("created" if applied and plan.status == "would_create" else plan.status),
            target_media_object_id=plan.target_media_object_id,
            target_library_asset_id=plan.target_library_asset_id,
            diagnostic_codes=list(plan.diagnostic_codes),
        )
        for plan in plans
    ]
    created_count = sum(plan.status == "would_create" for plan in plans)
    unchanged_count = sum(plan.status == "unchanged" for plan in plans)
    timestamp = generated_at or now_utc()
    provisional = LegacyGalleryBridgeReport(
        generated_at=timestamp,
        apply_requested=apply,
        applied=applied,
        source_manifest_sha256=manifest.manifest_sha256,
        source_report_sha256=manifest.source_report_sha256,
        source_snapshot_token=manifest.source_snapshot_token,
        target_reconciliation_sha256=None,
        scanned=len(plans),
        created_count=created_count,
        unchanged_count=unchanged_count,
        blocked_count=blocked,
        items=item_results,
        report_sha256="0" * 64,
    )
    return provisional.model_copy(update={"report_sha256": gallery_bridge_report_sha256(provisional)})


def verify_legacy_gallery_bridge(
    session: Session,
    *,
    manifest: LegacyGalleryBridgeManifest,
    source_storage_root: Path,
    target_storage: LocalStorage,
    expected_source_report_sha256: str,
) -> str:
    _validate_manifest(manifest, expected_source_report_sha256=expected_source_report_sha256)
    _ensure_target_schema(session)
    if any(record.blocker_code is not None for record in manifest.records):
        raise ConflictError("旧 Gallery bridge manifest 包含 blocker，不能生成目标对账 hash")

    rows: list[dict[str, object]] = []
    for record in manifest.records:
        metadata, source_error = _read_source_metadata(record, source_storage_root=source_storage_root)
        if source_error is not None or metadata is None:
            raise ConflictError(f"旧 Gallery source 在对账前发生变化: {record.gallery_entry_id}")
        library_asset = session.scalar(
            sa.select(MediaLibraryAsset).where(
                MediaLibraryAsset.source_type == "legacy_gallery",
                MediaLibraryAsset.source_id == record.gallery_entry_id,
            )
        )
        if library_asset is None:
            raise ConflictError(f"目标素材库缺少旧 Gallery 映射: {record.gallery_entry_id}")
        media = session.get(MediaObject, library_asset.media_object_id)
        if media is None:
            raise ConflictError(f"目标素材库缺少 MediaObject: {record.gallery_entry_id}")
        expected_provenance_hash = _provenance_hash(record, metadata)
        if library_asset.provenance_hash != expected_provenance_hash:
            raise ConflictError(f"目标素材库 provenance 不一致: {record.gallery_entry_id}")
        _assert_target_media(media, target_storage=target_storage, metadata=metadata)
        rows.append(
            {
                "gallery_entry_id": record.gallery_entry_id,
                "image_session_asset_id": record.image_session_asset_id,
                "target_library_asset_id": library_asset.id,
                "target_media_object_id": media.id,
                "target_storage_path": media.storage_path,
                "source_file_sha256": record.source_file_sha256,
                "target_file_sha256": media.sha256,
                "provenance_hash": library_asset.provenance_hash,
            }
        )
    return canonical_sha256(
        {
            "schema_version": 1,
            "source_manifest_sha256": manifest.manifest_sha256,
            "source_snapshot_token": manifest.source_snapshot_token,
            "rows": rows,
        }
    )


def approve_legacy_gallery_bridge(
    manifest: LegacyGalleryBridgeManifest,
    *,
    target_reconciliation_sha256: str,
    backup_restore_verified_at: datetime,
    zero_delta_observed_at: datetime,
    approved_at: datetime | None = None,
) -> LegacyGalleryBridgeApproval:
    _validate_manifest(manifest, expected_source_report_sha256=manifest.source_report_sha256)
    if any(record.blocker_code is not None for record in manifest.records):
        raise ConflictError("旧 Gallery bridge manifest 包含 blocker，不能批准物理退休")
    _validate_sha256("target_reconciliation_sha256", target_reconciliation_sha256)
    if backup_restore_verified_at.tzinfo is None or zero_delta_observed_at.tzinfo is None:
        raise BusinessValidationError("Gallery bridge 迁移证据时间必须带时区")
    if zero_delta_observed_at < backup_restore_verified_at:
        raise BusinessValidationError("zero-delta 观察时间不能早于备份恢复验证时间")
    provisional = LegacyGalleryBridgeApproval(
        approved_at=approved_at or now_utc(),
        source_manifest_sha256=manifest.manifest_sha256,
        source_report_sha256=manifest.source_report_sha256,
        source_snapshot_token=manifest.source_snapshot_token,
        target_reconciliation_sha256=target_reconciliation_sha256,
        backup_restore_verified_at=backup_restore_verified_at,
        zero_delta_observed_at=zero_delta_observed_at,
        approval_sha256="0" * 64,
    )
    return provisional.model_copy(update={"approval_sha256": gallery_bridge_approval_sha256(provisional)})


def retire_legacy_gallery_source(
    engine: Engine,
    *,
    manifest: LegacyGalleryBridgeManifest,
    approval: LegacyGalleryBridgeApproval,
    storage_root: Path,
    confirmation: str,
) -> LegacyGallerySourceRetirementReport:
    if confirmation != "RETIRE_LEGACY_GALLERY":
        raise BusinessValidationError("旧 Gallery source 清理需要明确确认字符串 RETIRE_LEGACY_GALLERY")
    _validate_source_retirement_inputs(manifest, approval)

    with engine.begin() as connection:
        _ensure_legacy_gallery_source_schema(connection)
        _lock_legacy_gallery_source_tables(connection)
        _assert_current_source_manifest(
            connection,
            manifest=manifest,
            storage_root=storage_root,
        )
        connection.execute(sa.text(f"DROP TABLE {LEGACY_GALLERY_TABLE}"))
    return LegacyGallerySourceRetirementReport(
        schema_version=1,
        source_manifest_sha256=manifest.manifest_sha256,
        source_report_sha256=manifest.source_report_sha256,
        source_snapshot_token=manifest.source_snapshot_token,
        target_reconciliation_sha256=approval.target_reconciliation_sha256,
        source_row_count=len(manifest.records),
        dropped=True,
    )


def inspect_legacy_gallery_source_retirement(
    engine: Engine,
    *,
    manifest: LegacyGalleryBridgeManifest,
    approval: LegacyGalleryBridgeApproval,
    storage_root: Path,
) -> LegacyGallerySourceRetirementReport:
    """Recheck the source under a lock without dropping its table."""

    _validate_source_retirement_inputs(manifest, approval)
    with engine.begin() as connection:
        _ensure_legacy_gallery_source_schema(connection)
        _lock_legacy_gallery_source_tables(connection)
        current = _assert_current_source_manifest(
            connection,
            manifest=manifest,
            storage_root=storage_root,
        )
    return LegacyGallerySourceRetirementReport(
        schema_version=1,
        source_manifest_sha256=current.manifest_sha256,
        source_report_sha256=current.source_report_sha256,
        source_snapshot_token=current.source_snapshot_token,
        target_reconciliation_sha256=approval.target_reconciliation_sha256,
        source_row_count=len(current.records),
        dropped=False,
    )


def _validate_source_retirement_inputs(
    manifest: LegacyGalleryBridgeManifest,
    approval: LegacyGalleryBridgeApproval,
) -> None:
    _validate_manifest(manifest, expected_source_report_sha256=manifest.source_report_sha256)
    if gallery_bridge_approval_sha256(approval) != approval.approval_sha256:
        raise BusinessValidationError("Gallery bridge approval hash 校验失败")
    if (
        approval.source_manifest_sha256 != manifest.manifest_sha256
        or approval.source_report_sha256 != manifest.source_report_sha256
        or approval.source_snapshot_token != manifest.source_snapshot_token
    ):
        raise ConflictError("Gallery bridge approval 与 source manifest 不一致")
    _validate_sha256("target_reconciliation_sha256", approval.target_reconciliation_sha256)
    if any(record.blocker_code is not None for record in manifest.records):
        raise ConflictError("旧 Gallery source 仍有 blocker，不能物理退休")


def _assert_current_source_manifest(
    connection: Connection,
    *,
    manifest: LegacyGalleryBridgeManifest,
    storage_root: Path,
) -> LegacyGalleryBridgeManifest:
    _ensure_legacy_gallery_source_schema(connection)
    current = capture_legacy_gallery_bridge_manifest_connection(
        connection,
        storage_root=storage_root,
        generated_at=now_utc(),
    )
    if (
        current.manifest_sha256 != manifest.manifest_sha256
        or current.source_report_sha256 != manifest.source_report_sha256
        or current.source_snapshot_token != manifest.source_snapshot_token
    ):
        raise ConflictError("旧 Gallery source manifest 在清理前发生变化")
    return current


def _ensure_legacy_gallery_source_schema(connection: Connection) -> tuple[list[str], dict[str, sa.Table]]:
    inspector = sa.inspect(connection)
    table_names = frozenset(inspector.get_table_names())
    if "alembic_version" not in table_names:
        raise BusinessValidationError("旧 Gallery source 缺少 alembic_version，拒绝桥接")
    revisions = [
        str(value)
        for value in connection.execute(
            sa.text("SELECT version_num FROM alembic_version ORDER BY version_num")
        ).scalars()
    ]
    if frozenset(revisions) != frozenset({LEGACY_GALLERY_SOURCE_REVISION}):
        raise BusinessValidationError(
            "旧 Gallery bridge 只支持 source revision 20260518_0032；"
            f"当前 revisions 为 {','.join(revisions) or '<missing>'}，不能直接套用"
        )
    required_columns = {
        LEGACY_GALLERY_TABLE: {"id", "image_session_asset_id", "created_at"},
        LEGACY_SESSION_ASSET_TABLE: {"id", "storage_path"},
    }
    missing_tables = sorted(set(required_columns) - table_names)
    missing_columns = {
        table_name: sorted(columns - {str(column["name"]) for column in inspector.get_columns(table_name)})
        for table_name, columns in required_columns.items()
        if table_name in table_names
        and columns - {str(column["name"]) for column in inspector.get_columns(table_name)}
    }
    if missing_tables or missing_columns:
        details = []
        if missing_tables:
            details.append(f"缺少表: {', '.join(missing_tables)}")
        if missing_columns:
            details.append(
                "缺少列: "
                + ", ".join(f"{table}: {', '.join(columns)}" for table, columns in sorted(missing_columns.items()))
            )
        raise BusinessValidationError("旧 Gallery bridge source schema 不完整: " + "; ".join(details))
    metadata = sa.MetaData()
    return revisions, {
        table_name: sa.Table(table_name, metadata, autoload_with=connection)
        for table_name in required_columns
    }


def _build_bridge_record(
    gallery_row: dict[str, Any],
    asset_row: dict[str, Any] | None,
    *,
    storage_root: Path,
) -> LegacyGalleryBridgeRecord:
    entry_id = str(gallery_row.get("id"))
    asset_id = str(asset_row["id"]) if asset_row is not None and asset_row.get("id") is not None else None
    storage_path = str(asset_row.get("storage_path") or "") if asset_row is not None else ""
    original_filename = _filename_from_source(asset_row, storage_path)
    declared_mime_type = (
        str(asset_row.get("mime_type") or "application/octet-stream")
        if asset_row
        else "application/octet-stream"
    )
    blocker_code: str | None = None
    metadata: VerifiedImageMetadata | None = None
    source_file_sha256: str | None = None
    source_byte_size: int | None = None
    width: int | None = None
    height: int | None = None
    gallery_created_at = _as_datetime(gallery_row.get("created_at"))
    if gallery_created_at is None:
        blocker_code = "gallery_created_at_missing"
    elif asset_row is None:
        blocker_code = "source_asset_missing"
    else:
        resolved = _resolve_source_path(storage_root, storage_path)
        if resolved is None:
            blocker_code = "source_path_invalid"
        elif not resolved.is_file():
            blocker_code = "source_file_missing"
        else:
            try:
                content = resolved.read_bytes()
            except OSError:
                blocker_code = "source_file_missing"
            else:
                source_file_sha256 = sha256(content).hexdigest()
                try:
                    metadata = inspect_image_bytes(content)
                except BusinessValidationError:
                    blocker_code = "source_image_invalid"
                else:
                    if (
                        declared_mime_type != "application/octet-stream"
                        and _normalized_mime(declared_mime_type) != metadata.mime_type
                    ):
                        blocker_code = "source_mime_mismatch"
                    source_byte_size = metadata.byte_size
                    width = metadata.width
                    height = metadata.height
                    declared_mime_type = metadata.mime_type
    return LegacyGalleryBridgeRecord(
        gallery_entry_id=entry_id,
        image_session_asset_id=asset_id,
        image_session_round_id=(
            str(gallery_row["image_session_round_id"])
            if gallery_row.get("image_session_round_id") is not None
            else None
        ),
        gallery_created_at=gallery_created_at,
        asset_created_at=_as_datetime(asset_row.get("created_at")) if asset_row else None,
        original_filename=original_filename,
        mime_type=declared_mime_type,
        storage_path=storage_path,
        source_file_sha256=source_file_sha256,
        source_byte_size=source_byte_size,
        width=width,
        height=height,
        blocker_code=blocker_code,
    )


def _plan_target_record(
    session: Session,
    *,
    record: LegacyGalleryBridgeRecord,
    source_storage_root: Path,
    target_storage: LocalStorage,
) -> _TargetPlan:
    if record.blocker_code is not None:
        return _TargetPlan(record=record, metadata=None, status="blocked", diagnostic_codes=(record.blocker_code,))
    metadata, source_error = _read_source_metadata(record, source_storage_root=source_storage_root)
    if source_error is not None or metadata is None:
        return _TargetPlan(
            record=record,
            metadata=None,
            status="blocked",
            diagnostic_codes=(source_error or "source_file_changed",),
        )
    expected_provenance_hash = _provenance_hash(record, metadata)
    existing = session.scalar(
        sa.select(MediaLibraryAsset).where(
            MediaLibraryAsset.source_type == "legacy_gallery",
            MediaLibraryAsset.source_id == record.gallery_entry_id,
        )
    )
    if existing is not None:
        media = session.get(MediaObject, existing.media_object_id)
        if media is None or existing.provenance_hash != expected_provenance_hash:
            return _TargetPlan(
                record=record,
                metadata=metadata,
                status="blocked",
                diagnostic_codes=("target_mapping_conflict",),
                target_media_object_id=existing.media_object_id,
                target_library_asset_id=existing.id,
                provenance_hash=expected_provenance_hash,
            )
        try:
            _assert_target_media(media, target_storage=target_storage, metadata=metadata)
        except ConflictError:
            return _TargetPlan(
                record=record,
                metadata=metadata,
                status="blocked",
                diagnostic_codes=("target_mapping_conflict",),
                target_media_object_id=media.id,
                target_library_asset_id=existing.id,
                provenance_hash=expected_provenance_hash,
            )
        return _TargetPlan(
            record=record,
            metadata=metadata,
            status="unchanged",
            diagnostic_codes=(),
            target_media_object_id=media.id,
            target_library_asset_id=existing.id,
            provenance_hash=expected_provenance_hash,
        )

    media_id = _stable_target_id("media", record.gallery_entry_id)
    library_asset_id = _stable_target_id("library", record.gallery_entry_id)
    if session.get(MediaLibraryAsset, library_asset_id) is not None:
        return _TargetPlan(
            record=record,
            metadata=metadata,
            status="blocked",
            diagnostic_codes=("target_mapping_conflict",),
            target_media_object_id=media_id,
            target_library_asset_id=library_asset_id,
            provenance_hash=expected_provenance_hash,
        )
    existing_media = session.get(MediaObject, media_id)
    if existing_media is not None:
        try:
            _assert_target_media(existing_media, target_storage=target_storage, metadata=metadata)
        except ConflictError:
            return _TargetPlan(
                record=record,
                metadata=metadata,
                status="blocked",
                diagnostic_codes=("target_media_conflict",),
                target_media_object_id=media_id,
                target_library_asset_id=library_asset_id,
                provenance_hash=expected_provenance_hash,
            )
    return _TargetPlan(
        record=record,
        metadata=metadata,
        status="would_create",
        diagnostic_codes=(),
        target_media_object_id=media_id,
        target_library_asset_id=library_asset_id,
        provenance_hash=expected_provenance_hash,
    )


def _create_target_record(
    session: Session,
    *,
    plan: _TargetPlan,
    source_storage_root: Path,
    target_storage: LocalStorage,
    storage_writes,
) -> None:
    if plan.metadata is None or plan.target_media_object_id is None or plan.target_library_asset_id is None:
        raise ConflictError(f"旧 Gallery bridge plan 不完整: {plan.record.gallery_entry_id}")
    content, metadata, source_error = _read_verified_source_content(
        plan.record,
        source_storage_root=source_storage_root,
    )
    if source_error is not None or content is None or metadata is None:
        raise ConflictError(f"旧 Gallery source 在写入前发生变化: {plan.record.gallery_entry_id}")
    media = session.get(MediaObject, plan.target_media_object_id)
    if media is None:
        storage_path = storage_writes.track(
            target_storage,
            target_storage.save_media_image(plan.target_media_object_id, plan.record.original_filename, content),
        )
        media = MediaObject(
            id=plan.target_media_object_id,
            storage_path=storage_path,
            mime_type=metadata.mime_type,
            byte_size=metadata.byte_size,
            width=metadata.width,
            height=metadata.height,
            sha256=metadata.sha256,
            verification_status=MediaVerificationStatus.VERIFIED,
            verified_at=now_utc(),
        )
        session.add(media)
        session.flush()
    else:
        _assert_target_media(media, target_storage=target_storage, metadata=metadata)
    provenance = _build_provenance(plan.record, metadata)
    session.add(
        MediaLibraryAsset(
            id=plan.target_library_asset_id,
            media_object_id=media.id,
            source_type="legacy_gallery",
            source_id=plan.record.gallery_entry_id,
            provenance_json=provenance,
            provenance_hash=canonical_sha256(provenance),
            display_name=plan.record.original_filename,
            original_filename=plan.record.original_filename,
        )
    )
    session.flush()


def _read_source_metadata(
    record: LegacyGalleryBridgeRecord,
    *,
    source_storage_root: Path,
) -> tuple[VerifiedImageMetadata | None, str | None]:
    _, metadata, error = _read_verified_source_content(
        record,
        source_storage_root=source_storage_root,
    )
    return metadata, error


def _read_verified_source_content(
    record: LegacyGalleryBridgeRecord,
    *,
    source_storage_root: Path,
) -> tuple[bytes | None, VerifiedImageMetadata | None, str | None]:
    if record.blocker_code is not None:
        return None, None, record.blocker_code
    try:
        content = _read_source_content(record, source_storage_root=source_storage_root)
    except ConflictError as exc:
        return None, None, str(exc)
    actual_sha256 = sha256(content).hexdigest()
    if record.source_file_sha256 != actual_sha256 or record.source_byte_size != len(content):
        return None, None, "source_file_changed"
    try:
        metadata = inspect_image_bytes(content)
    except BusinessValidationError:
        return None, None, "source_image_invalid"
    if record.width != metadata.width or record.height != metadata.height or record.mime_type != metadata.mime_type:
        return None, None, "source_file_changed"
    return content, metadata, None


def _read_source_content(record: LegacyGalleryBridgeRecord, *, source_storage_root: Path) -> bytes:
    resolved = _resolve_source_path(source_storage_root, record.storage_path)
    if resolved is None:
        raise ConflictError("source_path_invalid")
    if not resolved.is_file():
        raise ConflictError("source_file_missing")
    try:
        return resolved.read_bytes()
    except OSError as exc:
        raise ConflictError("source_file_missing") from exc


def _assert_target_media(
    media: MediaObject,
    *,
    target_storage: LocalStorage,
    metadata: VerifiedImageMetadata,
) -> None:
    if (
        media.verification_status != MediaVerificationStatus.VERIFIED
        or media.sha256 != metadata.sha256
        or media.byte_size != metadata.byte_size
        or media.width != metadata.width
        or media.height != metadata.height
        or media.mime_type != metadata.mime_type
    ):
        raise ConflictError("target_media_metadata_mismatch")
    try:
        path = target_storage.resolve(media.storage_path)
    except ValueError as exc:
        raise ConflictError("target_media_path_invalid") from exc
    try:
        actual_sha256 = sha256(path.read_bytes()).hexdigest() if path.is_file() else None
    except OSError:
        actual_sha256 = None
    if actual_sha256 != metadata.sha256:
        raise ConflictError("target_media_file_mismatch")


def _build_provenance(record: LegacyGalleryBridgeRecord, metadata: VerifiedImageMetadata) -> dict[str, object]:
    if record.gallery_created_at is None:
        raise ConflictError("gallery_created_at_missing")
    return {
        "schema_version": 1,
        "source_type": "legacy_gallery",
        "source_id": record.gallery_entry_id,
        "sha256": metadata.sha256,
        "mime_type": metadata.mime_type,
        "byte_size": metadata.byte_size,
        "width": metadata.width,
        "height": metadata.height,
        "original_filename": record.original_filename,
        "captured_at": record.gallery_created_at.isoformat(),
    }


def _provenance_hash(record: LegacyGalleryBridgeRecord, metadata: VerifiedImageMetadata) -> str:
    return canonical_sha256(_build_provenance(record, metadata))


def _validate_manifest(manifest: LegacyGalleryBridgeManifest, *, expected_source_report_sha256: str) -> None:
    if manifest.source_profile != LEGACY_GALLERY_BRIDGE_SOURCE_PROFILE:
        raise BusinessValidationError("旧 Gallery bridge source profile 不受支持")
    if gallery_bridge_manifest_sha256(manifest) != manifest.manifest_sha256:
        raise BusinessValidationError("旧 Gallery bridge manifest hash 校验失败")
    if expected_source_report_sha256 != manifest.source_report_sha256:
        raise BusinessValidationError("旧 Gallery bridge manifest 与批准的 source report hash 不一致")
    source_ids = [record.gallery_entry_id for record in manifest.records]
    if len(source_ids) != len(set(source_ids)):
        raise BusinessValidationError("旧 Gallery bridge manifest 存在重复 gallery entry")


def _ensure_target_schema(session: Session) -> None:
    inspector = sa.inspect(session.connection())
    table_names = frozenset(inspector.get_table_names())
    required = {"media_objects", "media_library_assets"}
    missing = sorted(required - table_names)
    if missing:
        raise BusinessValidationError(
            "旧 Gallery bridge target 不是当前素材库 schema，缺少表: " + ", ".join(missing)
        )


def _lock_legacy_gallery_source_tables(connection: Connection) -> None:
    if connection.dialect.name == "postgresql":
        connection.exec_driver_sql(
            f"LOCK TABLE {LEGACY_GALLERY_TABLE}, {LEGACY_SESSION_ASSET_TABLE} IN ACCESS EXCLUSIVE MODE"
        )


def _validate_sha256(field_name: str, value: str) -> None:
    if len(value) != 64 or any(character not in "0123456789abcdef" for character in value):
        raise BusinessValidationError(f"{field_name} 必须是 64 位小写 SHA-256")


def _stable_target_id(kind: str, source_id: str) -> str:
    return str(uuid5(NAMESPACE_URL, f"productflow:legacy-gallery:{kind}:{source_id}"))


def _filename_from_source(asset_row: dict[str, Any] | None, storage_path: str) -> str:
    declared = str(asset_row.get("original_filename") or "") if asset_row else ""
    if declared.strip():
        return declared.strip()[:255]
    name = PurePosixPath(storage_path.replace("\\", "/")).name
    return (name or "legacy-gallery-image")[:255]


def _resolve_source_path(root: Path, storage_path: str) -> Path | None:
    normalized = storage_path.replace("\\", "/")
    relative = PurePosixPath(normalized)
    if not normalized or relative.is_absolute() or ".." in relative.parts:
        return None
    resolved_root = root.expanduser().resolve()
    resolved = (resolved_root / Path(*relative.parts)).resolve()
    try:
        resolved.relative_to(resolved_root)
    except ValueError:
        return None
    return resolved


def _normalized_mime(value: str) -> str:
    return value.split(";", maxsplit=1)[0].strip().lower()


def _as_datetime(value: Any) -> datetime | None:
    if value is None:
        return None
    if isinstance(value, datetime):
        return value if value.tzinfo is not None else value.replace(tzinfo=UTC)
    try:
        parsed = datetime.fromisoformat(str(value))
    except ValueError:
        return None
    return parsed if parsed.tzinfo is not None else parsed.replace(tzinfo=UTC)


__all__ = [
    "LEGACY_GALLERY_TABLE",
    "LegacyGallerySourceRetirementReport",
    "approve_legacy_gallery_bridge",
    "capture_legacy_gallery_bridge_manifest_connection",
    "export_legacy_gallery_bridge_manifest",
    "inspect_legacy_gallery_source_retirement",
    "retire_legacy_gallery_source",
    "run_legacy_gallery_bridge",
    "verify_legacy_gallery_bridge",
]
