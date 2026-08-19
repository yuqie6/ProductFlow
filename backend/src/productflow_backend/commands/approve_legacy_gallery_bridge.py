from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from datetime import datetime
from pathlib import Path

from productflow_backend.application.legacy_retirement.gallery_bridge import approve_legacy_gallery_bridge
from productflow_backend.application.legacy_retirement.gallery_bridge_contracts import LegacyGalleryBridgeManifest


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="生成旧 Gallery bridge 的独立物理退休批准证据")
    parser.add_argument("--manifest", type=Path, required=True, help="source Gallery bridge manifest JSON")
    parser.add_argument("--target-reconciliation-sha256", required=True, help="目标素材库对账 SHA-256")
    parser.add_argument("--backup-restore-verified-at", required=True, help="带时区的备份恢复验证时间")
    parser.add_argument("--zero-delta-observed-at", required=True, help="带时区的零增量观察时间")
    parser.add_argument("--output", type=Path, required=True, help="批准证据 JSON")
    parser.add_argument("--compact", action="store_true", help="标准输出使用紧凑 JSON")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    manifest = LegacyGalleryBridgeManifest.model_validate_json(args.manifest.read_text(encoding="utf-8"))
    approval = approve_legacy_gallery_bridge(
        manifest,
        target_reconciliation_sha256=args.target_reconciliation_sha256,
        backup_restore_verified_at=datetime.fromisoformat(args.backup_restore_verified_at),
        zero_delta_observed_at=datetime.fromisoformat(args.zero_delta_observed_at),
    )
    rendered = json.dumps(
        approval.model_dump(mode="json"),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":") if args.compact else None,
        indent=None if args.compact else 2,
    )
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(f"{rendered}\n", encoding="utf-8")
    print(rendered)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
