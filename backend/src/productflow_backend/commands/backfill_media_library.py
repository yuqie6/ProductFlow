from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from dataclasses import asdict
from datetime import datetime
from pathlib import Path

from productflow_backend.application.media_library.backfill import (
    capture_gallery_snapshot,
    run_gallery_backfill,
    verify_gallery_backfill,
)
from productflow_backend.application.media_library.migration_audit import MediaLibraryMigrationAudit
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.storage import LocalStorage


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
    return MediaLibraryMigrationAudit(
        snapshot_token=str(payload["snapshot_token"]),
        gallery_count=int(payload["gallery_count"]),
        session_asset_count=int(payload["session_asset_count"]),
        captured_at=datetime.fromisoformat(captured_at),
        source_hash=str(payload["source_hash"]),
        source_rows=tuple(source_rows),
    )


def _load_or_capture_snapshot(session, path: Path | None, *, require_existing: bool) -> MediaLibraryMigrationAudit:
    if path is not None and path.exists():
        payload = json.loads(path.read_text(encoding="utf-8"))
        if not isinstance(payload, dict):
            raise ValueError("snapshot JSON 根节点必须是 object")
        return _snapshot_from_payload(payload)
    if require_existing:
        raise ValueError("--apply/--verify 必须指定已存在的 --snapshot-file")
    snapshot = capture_gallery_snapshot(session)
    if path is not None:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            json.dumps(asdict(snapshot), ensure_ascii=False, sort_keys=True, default=str, indent=2) + "\n",
            encoding="utf-8",
        )
    return snapshot


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    session = get_session_factory()()
    try:
        snapshot = _load_or_capture_snapshot(
            session,
            args.snapshot_file,
            require_existing=args.apply or args.verify,
        )
        summary = run_gallery_backfill(
            session,
            storage=LocalStorage(),
            limit=args.limit,
            offset=args.offset,
            apply=args.apply,
            snapshot=snapshot,
        )
        verified = None
        if args.verify:
            verified = verify_gallery_backfill(session, storage=LocalStorage(), snapshot=snapshot)
        payload = {
            "snapshot_token": snapshot.snapshot_token,
            "gallery_count": snapshot.gallery_count,
            "session_asset_count": snapshot.session_asset_count,
            "source_hash": snapshot.source_hash,
            "summary": asdict(summary),
            "verified": verified,
        }
        print(json.dumps(payload, ensure_ascii=False, sort_keys=True, default=str))
        return 0
    finally:
        session.close()


if __name__ == "__main__":
    raise SystemExit(main())
