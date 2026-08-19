from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from pathlib import Path

from productflow_backend.application.legacy_retirement.gallery_bridge import (
    run_legacy_gallery_bridge,
    verify_legacy_gallery_bridge,
)
from productflow_backend.application.legacy_retirement.gallery_bridge_contracts import (
    LegacyGalleryBridgeManifest,
    gallery_bridge_report_sha256,
)
from productflow_backend.config import get_settings
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.storage import LocalStorage


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="把旧 Canvas schema 的 Gallery manifest 导入当前全局素材库")
    parser.add_argument("--input", type=Path, required=True, help="export_legacy_gallery_bridge 生成的 manifest JSON")
    parser.add_argument("--source-storage-root", type=Path, required=True, help="旧库媒体文件根目录")
    parser.add_argument(
        "--expected-source-report-sha256",
        required=True,
        help="已经批准的旧 Gallery source report SHA-256",
    )
    parser.add_argument("--target-storage-root", type=Path, help="当前库媒体文件根目录；默认使用 STORAGE_ROOT")
    parser.add_argument("--apply", action="store_true", help="通过全部 source blocker 后写入当前素材库")
    parser.add_argument("--verify", action="store_true", help="导入后重新核验 source 文件和目标映射")
    parser.add_argument("--output", type=Path, help="bridge 结果 JSON")
    parser.add_argument("--compact", action="store_true", help="标准输出使用紧凑 JSON")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    settings = get_settings()
    manifest = LegacyGalleryBridgeManifest.model_validate_json(args.input.read_text(encoding="utf-8"))
    target_storage = LocalStorage(root=args.target_storage_root or settings.storage_root)
    session = get_session_factory()()
    try:
        report = run_legacy_gallery_bridge(
            session,
            manifest=manifest,
            source_storage_root=args.source_storage_root,
            target_storage=target_storage,
            expected_source_report_sha256=args.expected_source_report_sha256,
            apply=args.apply,
        )
        if args.verify:
            reconciliation_sha256 = verify_legacy_gallery_bridge(
                session,
                manifest=manifest,
                source_storage_root=args.source_storage_root,
                target_storage=target_storage,
                expected_source_report_sha256=args.expected_source_report_sha256,
            )
            report = report.model_copy(
                update={
                    "target_reconciliation_sha256": reconciliation_sha256,
                    "report_sha256": "0" * 64,
                }
            )
            report = report.model_copy(update={"report_sha256": gallery_bridge_report_sha256(report)})
    except (RuntimeError, ValueError) as exc:
        session.rollback()
        payload = {"detail": str(exc)}
        rendered = json.dumps(payload, ensure_ascii=False, sort_keys=True)
        if args.output is not None:
            args.output.parent.mkdir(parents=True, exist_ok=True)
            args.output.write_text(f"{rendered}\n", encoding="utf-8")
        print(rendered)
        return 2
    finally:
        session.close()

    rendered = json.dumps(
        report.model_dump(mode="json"),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":") if args.compact else None,
        indent=None if args.compact else 2,
    )
    if args.output is not None:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(f"{rendered}\n", encoding="utf-8")
    print(rendered)
    return 0 if report.blocked_count == 0 and (not args.apply or report.applied) else 2


if __name__ == "__main__":
    raise SystemExit(main())
