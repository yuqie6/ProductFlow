from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from datetime import datetime

from productflow_backend.application.media_library.retirement import (
    approve_media_library_cutover_gate,
    assert_media_library_cutover_cleanup_ready,
    read_media_library_cutover_gate,
)
from productflow_backend.domain.errors import BusinessError
from productflow_backend.infrastructure.db.session import get_engine


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="读取或批准旧 Gallery 退休的素材库证据闸门")
    subparsers = parser.add_subparsers(dest="action", required=True)
    subparsers.add_parser("status", help="读取当前素材库闸门状态")
    approve = subparsers.add_parser("approve", help="写入冻结快照、对账、备份和 zero-delta 观察证据")
    approve.add_argument("--source-snapshot-token", required=True)
    approve.add_argument("--source-report-sha256", required=True)
    approve.add_argument("--reconciliation-report-sha256", required=True)
    approve.add_argument("--backup-restore-verified-at", required=True)
    approve.add_argument("--zero-delta-observed-at", required=True)
    subparsers.add_parser("assert-cleanup-ready", help="检查旧 Gallery 是否具备退休资格")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    try:
        with get_engine().begin() as connection:
            if args.action == "status":
                state = read_media_library_cutover_gate(connection)
            elif args.action == "approve":
                state = approve_media_library_cutover_gate(
                    connection,
                    source_snapshot_token=args.source_snapshot_token,
                    source_report_sha256=args.source_report_sha256,
                    reconciliation_report_sha256=args.reconciliation_report_sha256,
                    backup_restore_verified_at=_parse_datetime(args.backup_restore_verified_at),
                    zero_delta_observed_at=_parse_datetime(args.zero_delta_observed_at),
                )
            else:
                state = assert_media_library_cutover_cleanup_ready(connection)
    except BusinessError as exc:
        print(json.dumps({"detail": exc.message}, ensure_ascii=False, sort_keys=True))
        return 2

    print(json.dumps(_state_dict(state), ensure_ascii=False, sort_keys=True, default=_json_default))
    return 0


def _state_dict(state: object) -> dict[str, object]:
    return {
        field_name: getattr(state, field_name)
        for field_name in (
            "id",
            "schema_version",
            "phase",
            "source_snapshot_token",
            "source_report_sha256",
            "reconciliation_report_sha256",
            "backup_restore_verified_at",
            "zero_delta_observed_at",
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
