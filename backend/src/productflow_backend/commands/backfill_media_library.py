from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from dataclasses import asdict, replace
from datetime import datetime
from pathlib import Path

import sqlalchemy as sa

from productflow_backend.application.media_library.cutover.backfill import (
    capture_gallery_snapshot,
    collect_gallery_backfill_blockers,
    gallery_reconciliation_hash,
    run_gallery_backfill,
    verify_gallery_backfill,
)
from productflow_backend.application.media_library.cutover.migration_audit import MediaLibraryMigrationAudit
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.storage import LocalStorage

_BACKFILL_SCHEMA_REQUIREMENTS = {
    "image_gallery_entries": frozenset({"id", "image_session_asset_id", "created_at"}),
    "image_session_assets": frozenset({"id", "media_object_id"}),
    "media_objects": frozenset({"id"}),
    "media_library_assets": frozenset({"id", "source_type", "source_id"}),
}


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="预检/回填/对账旧 Gallery 到 MediaLibraryAsset")
    parser.add_argument("--apply", action="store_true", help="实际写入 MediaLibraryAsset")
    parser.add_argument("--limit", type=int, default=100, help="每批处理条数")
    parser.add_argument("--offset", type=int, default=0, help="起始偏移")
    parser.add_argument("--verify", action="store_true", help="回填后执行对账")
    parser.add_argument(
        "--snapshot-file",
        type=Path,
        help="冻结并复用 Gallery snapshot JSON；apply/verify 必须使用已有文件",
    )
    return parser


def _snapshot_from_payload(payload: dict[str, object]) -> MediaLibraryMigrationAudit:
    source_rows_raw = payload.get("source_rows", [])
    if not isinstance(source_rows_raw, list):
        raise ValueError("snapshot source_rows 无效")
    source_rows: list[tuple[str, str | None, str | None]] = []
    for row in source_rows_raw:
        if not isinstance(row, list) or len(row) != 3:
            raise ValueError("snapshot source_rows 无效")
        entry_id, asset_id, media_id = row
        if not isinstance(entry_id, str) or not isinstance(asset_id, (str, type(None))) or not isinstance(
            media_id, (str, type(None))
        ):
            raise ValueError("snapshot source_rows 无效")
        source_rows.append((entry_id, asset_id, media_id))
    captured_at = payload.get("captured_at")
    if not isinstance(captured_at, str):
        raise ValueError("snapshot captured_at 无效")
    blocker_report_raw = payload.get("blocker_report", [])
    if blocker_report_raw is None:
        blocker_report_raw = []
    if not isinstance(blocker_report_raw, list):
        raise ValueError("snapshot blocker_report 无效")
    blocker_report: list[tuple[str, str]] = []
    for item in blocker_report_raw:
        if not isinstance(item, list) or len(item) != 2:
            raise ValueError("snapshot blocker_report 无效")
        blocker_report.append((str(item[0]), str(item[1])))
    database_snapshot_token = payload.get("database_snapshot_token")
    storage_snapshot_id = payload.get("storage_snapshot_id")
    return MediaLibraryMigrationAudit(
        snapshot_token=str(payload["snapshot_token"]),
        gallery_count=int(payload["gallery_count"]),
        session_asset_count=int(payload["session_asset_count"]),
        captured_at=datetime.fromisoformat(captured_at),
        source_hash=str(payload["source_hash"]),
        source_rows=tuple(source_rows),
        database_snapshot_token=str(database_snapshot_token) if database_snapshot_token is not None else None,
        storage_snapshot_id=str(storage_snapshot_id) if storage_snapshot_id is not None else None,
        blocker_report=tuple(blocker_report),
    )


def _load_or_capture_snapshot(
    session,
    path: Path | None,
    *,
    require_existing: bool,
    storage: LocalStorage | None = None,
) -> MediaLibraryMigrationAudit:
    if path is not None and path.exists():
        payload = json.loads(path.read_text(encoding="utf-8"))
        if not isinstance(payload, dict):
            raise ValueError("snapshot JSON 根节点必须是 object")
        return _snapshot_from_payload(payload)
    if require_existing:
        raise ValueError("--apply/--verify 必须指定已存在的 --snapshot-file")
    snapshot = capture_gallery_snapshot(session, storage=storage)
    if storage is not None:
        snapshot = replace(
            snapshot,
            blocker_report=collect_gallery_backfill_blockers(session, storage=storage, snapshot=snapshot),
        )
    if path is not None:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            json.dumps(asdict(snapshot), ensure_ascii=False, sort_keys=True, default=str, indent=2) + "\n",
            encoding="utf-8",
        )
    return snapshot


def _ensure_backfill_schema(session) -> None:
    inspector = sa.inspect(session.connection())
    table_names = frozenset(inspector.get_table_names())
    missing_tables = sorted(set(_BACKFILL_SCHEMA_REQUIREMENTS) - table_names)
    missing_columns = {
        table_name: sorted(required - {str(column["name"]) for column in inspector.get_columns(table_name)})
        for table_name, required in _BACKFILL_SCHEMA_REQUIREMENTS.items()
        if table_name in table_names
        and required - {str(column["name"]) for column in inspector.get_columns(table_name)}
    }
    if not missing_tables and not missing_columns:
        return

    revision = None
    if "alembic_version" in table_names:
        revision = session.scalar(sa.text("SELECT version_num FROM alembic_version ORDER BY version_num LIMIT 1"))
    details: list[str] = []
    if missing_tables:
        details.append(f"缺少表: {', '.join(missing_tables)}")
    if missing_columns:
        details.append(
            "缺少列: "
            + ", ".join(f"{table_name}.{', '.join(columns)}" for table_name, columns in sorted(missing_columns.items()))
        )
    revision_label = str(revision) if revision is not None else "<missing>"
    raise ValueError(
        "当前数据库不是 Media Library backfill target "
        f"(Alembic revision: {revision_label}; {'; '.join(details)})。"
        "请先在支持的当前 schema 上完成迁移；若 source profile 是旧 Canvas revision，"
        "先运行 audit_legacy_retirement 和 preflight_legacy_cutover，不能直接回填或 stamp。"
    )


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    session = get_session_factory()()
    storage = LocalStorage()
    try:
        _ensure_backfill_schema(session)
        snapshot = _load_or_capture_snapshot(
            session,
            args.snapshot_file,
            require_existing=args.apply or args.verify,
            storage=storage,
        )
        summary = run_gallery_backfill(
            session,
            storage=storage,
            limit=args.limit,
            offset=args.offset,
            apply=args.apply,
            snapshot=snapshot,
        )
        verified = None
        reconciliation_hash = None
        if args.verify:
            verified = verify_gallery_backfill(session, storage=storage, snapshot=snapshot)
            reconciliation_hash = gallery_reconciliation_hash(
                session,
                storage=storage,
                snapshot=snapshot,
            )
        payload = {
            "snapshot_token": snapshot.snapshot_token,
            "gallery_count": snapshot.gallery_count,
            "session_asset_count": snapshot.session_asset_count,
            "source_hash": snapshot.source_hash,
            "database_snapshot_token": snapshot.database_snapshot_token,
            "storage_snapshot_id": snapshot.storage_snapshot_id,
            "blocker_report": list(snapshot.blocker_report),
            "summary": asdict(summary),
            "verified": verified,
            "reconciliation_report_sha256": reconciliation_hash,
        }
        print(json.dumps(payload, ensure_ascii=False, sort_keys=True, default=str))
        if summary.blocked:
            return 2
        if verified is not None and verified != snapshot.gallery_count:
            return 2
        return 0
    except (RuntimeError, ValueError) as exc:
        session.rollback()
        print(json.dumps({"detail": str(exc)}, ensure_ascii=False, sort_keys=True))
        return 2
    finally:
        session.close()


if __name__ == "__main__":
    raise SystemExit(main())
