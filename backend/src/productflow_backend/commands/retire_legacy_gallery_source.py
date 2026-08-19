from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from dataclasses import asdict
from pathlib import Path

import sqlalchemy as sa

from productflow_backend.application.legacy_retirement.gallery_bridge import (
    inspect_legacy_gallery_source_retirement,
    retire_legacy_gallery_source,
)
from productflow_backend.application.legacy_retirement.gallery_bridge_contracts import (
    LegacyGalleryBridgeApproval,
    LegacyGalleryBridgeManifest,
)
from productflow_backend.config import get_settings


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="在独立批准证据通过后退休旧库 image_gallery_entries 表")
    parser.add_argument("--database-url", help="旧库 DATABASE_URL；未提供时使用当前环境值")
    parser.add_argument("--source-storage-root", type=Path, required=True, help="旧库媒体文件根目录")
    parser.add_argument("--manifest", type=Path, required=True, help="Gallery bridge manifest JSON")
    parser.add_argument("--approval", type=Path, required=True, help="approve_legacy_gallery_bridge 生成的批准 JSON")
    parser.add_argument("--apply", action="store_true", help="实际删除旧 Gallery 表")
    parser.add_argument("--confirm", default="", help="实际删除时必须是 RETIRE_LEGACY_GALLERY")
    parser.add_argument("--output", type=Path, help="退休结果 JSON")
    parser.add_argument("--compact", action="store_true", help="标准输出使用紧凑 JSON")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    settings = get_settings()
    database_url = args.database_url or settings.database_url
    manifest = LegacyGalleryBridgeManifest.model_validate_json(args.manifest.read_text(encoding="utf-8"))
    approval = LegacyGalleryBridgeApproval.model_validate_json(args.approval.read_text(encoding="utf-8"))
    connect_args = {"check_same_thread": False} if database_url.startswith("sqlite") else {}
    engine = sa.create_engine(database_url, future=True, connect_args=connect_args, pool_pre_ping=True)
    try:
        if args.apply:
            report = retire_legacy_gallery_source(
                engine,
                manifest=manifest,
                approval=approval,
                storage_root=args.source_storage_root,
                confirmation=args.confirm,
            )
        else:
            report = inspect_legacy_gallery_source_retirement(
                engine,
                manifest=manifest,
                approval=approval,
                storage_root=args.source_storage_root,
            )
    except (RuntimeError, ValueError) as exc:
        payload = {"detail": str(exc)}
        rendered = json.dumps(payload, ensure_ascii=False, sort_keys=True)
        if args.output is not None:
            args.output.parent.mkdir(parents=True, exist_ok=True)
            args.output.write_text(f"{rendered}\n", encoding="utf-8")
        print(rendered)
        return 2
    finally:
        engine.dispose()

    rendered = json.dumps(
        asdict(report),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":") if args.compact else None,
        indent=None if args.compact else 2,
    )
    if args.output is not None:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(f"{rendered}\n", encoding="utf-8")
    print(rendered)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
