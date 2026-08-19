from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from pathlib import Path

import sqlalchemy as sa

from productflow_backend.application.legacy_retirement.gallery_bridge import (
    export_legacy_gallery_bridge_manifest,
)
from productflow_backend.config import get_settings


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="只读导出旧 Canvas schema 的 Gallery 兼容迁移 manifest")
    parser.add_argument("--database-url", help="旧库 DATABASE_URL；未提供时使用当前环境值")
    parser.add_argument(
        "--source-storage-root",
        type=Path,
        help="旧库媒体文件根目录；未提供时使用当前环境 STORAGE_ROOT",
    )
    parser.add_argument("--output", type=Path, help="版本化 Gallery bridge manifest JSON")
    parser.add_argument("--compact", action="store_true", help="标准输出使用紧凑 JSON")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    settings = get_settings()
    database_url = args.database_url or settings.database_url
    storage_root = (args.source_storage_root or settings.storage_root).expanduser().resolve()
    connect_args = {"check_same_thread": False} if database_url.startswith("sqlite") else {}
    engine = sa.create_engine(database_url, future=True, connect_args=connect_args, pool_pre_ping=True)
    try:
        manifest = export_legacy_gallery_bridge_manifest(engine, storage_root=storage_root)
    finally:
        engine.dispose()

    rendered = json.dumps(
        manifest.model_dump(mode="json"),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":") if args.compact else None,
        indent=None if args.compact else 2,
    )
    if args.output is not None:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(f"{rendered}\n", encoding="utf-8")
    print(rendered)
    return 0 if not any(record.blocker_code for record in manifest.records) else 2


if __name__ == "__main__":
    raise SystemExit(main())
