from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from pathlib import Path

from productflow_backend.application.legacy_retirement.backfill import (
    backfill_legacy_archive_page,
    render_archive_backfill_csv,
)
from productflow_backend.application.legacy_retirement.contracts import LegacyArchiveExportPage
from productflow_backend.infrastructure.db.session import get_session_factory


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="校验并回填版本化旧工作流归档快照")
    parser.add_argument("--input", type=Path, required=True, help="export_legacy_archives 生成的 page JSON")
    parser.add_argument(
        "--expected-source-report-sha256",
        required=True,
        help="已经人工批准的只读 source audit report SHA-256",
    )
    parser.add_argument("--apply", action="store_true", help="通过全部门槛后写入；默认只做目标映射 dry-run")
    parser.add_argument(
        "--allow-legacy-bridge",
        action="store_true",
        help="显式允许已批准的旧 lineage page 进入目标映射；不会绕过其它 blocker",
    )
    parser.add_argument("--output-json", type=Path, help="backfill 结果 JSON")
    parser.add_argument("--output-csv", type=Path, help="backfill 对账 CSV")
    parser.add_argument("--compact", action="store_true", help="标准输出使用紧凑 JSON")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    page = LegacyArchiveExportPage.model_validate_json(args.input.read_text(encoding="utf-8"))
    session = get_session_factory()()
    try:
        report = backfill_legacy_archive_page(
            session,
            page=page,
            expected_source_report_sha256=args.expected_source_report_sha256,
            apply=args.apply,
            allow_legacy_bridge=args.allow_legacy_bridge,
        )
    finally:
        session.close()

    rendered = json.dumps(
        report.model_dump(mode="json"),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":") if args.compact else None,
        indent=None if args.compact else 2,
    )
    if args.output_json is not None:
        args.output_json.parent.mkdir(parents=True, exist_ok=True)
        args.output_json.write_text(f"{rendered}\n", encoding="utf-8")
    if args.output_csv is not None:
        args.output_csv.parent.mkdir(parents=True, exist_ok=True)
        args.output_csv.write_text(render_archive_backfill_csv(report), encoding="utf-8")
    print(rendered)
    dry_run_ready = not args.apply and not report.global_blocking_issue_codes and not report.blocked_count
    return 0 if report.applied or dry_run_ready else 2


if __name__ == "__main__":
    raise SystemExit(main())
