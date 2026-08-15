from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from datetime import datetime

from productflow_backend.application.legacy_retirement.gate import (
    approve_legacy_cutover_gate,
    assert_legacy_cutover_cleanup_ready,
    read_legacy_cutover_gate,
)
from productflow_backend.domain.errors import BusinessError
from productflow_backend.infrastructure.db.session import get_engine


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="读取或批准旧工作流切换清理门槛")
    subparsers = parser.add_subparsers(dest="action", required=True)
    subparsers.add_parser("status", help="读取当前持久化门槛状态")
    approve = subparsers.add_parser("approve", help="写入外部审计、归档、canonical 和备份证据")
    approve.add_argument("--source-profile", required=True)
    approve.add_argument("--source-report-sha256", required=True)
    approve.add_argument("--archive-report-sha256", required=True)
    approve.add_argument("--canonical-report-sha256", required=True)
    approve.add_argument("--backup-restore-verified-at", required=True)
    subparsers.add_parser("assert-cleanup-ready", help="检查未来 destructive cleanup 是否有资格执行")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    try:
        with get_engine().begin() as connection:
            if args.action == "status":
                state = read_legacy_cutover_gate(connection)
            elif args.action == "approve":
                state = approve_legacy_cutover_gate(
                    connection,
                    source_profile=args.source_profile,
                    source_report_sha256=args.source_report_sha256,
                    archive_report_sha256=args.archive_report_sha256,
                    canonical_report_sha256=args.canonical_report_sha256,
                    backup_restore_verified_at=_parse_datetime(args.backup_restore_verified_at),
                )
            else:
                state = assert_legacy_cutover_cleanup_ready(connection)
    except BusinessError as exc:
        print(json.dumps({"detail": exc.message}, ensure_ascii=False, sort_keys=True))
        return 2

    print(
        json.dumps(
            _state_dict(state),
            ensure_ascii=False,
            sort_keys=True,
            default=_json_default,
        )
    )
    return 0


def _state_dict(state: object) -> dict[str, object]:
    return {
        field_name: getattr(state, field_name)
        for field_name in (
            "id",
            "schema_version",
            "phase",
            "source_profile",
            "source_report_sha256",
            "archive_report_sha256",
            "canonical_report_sha256",
            "backup_restore_verified_at",
            "active_run_count",
            "created_at",
            "updated_at",
        )
    }


def _parse_datetime(value: str) -> datetime:
    normalized = value[:-1] + "+00:00" if value.endswith("Z") else value
    return datetime.fromisoformat(normalized)


def _json_default(value: object) -> str:
    if isinstance(value, datetime):
        return value.isoformat()
    raise TypeError(f"unsupported JSON value: {type(value).__name__}")


if __name__ == "__main__":
    raise SystemExit(main())
