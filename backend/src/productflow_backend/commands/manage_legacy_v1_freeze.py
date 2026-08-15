from __future__ import annotations

import argparse
import json
from collections.abc import Sequence

from productflow_backend.application.legacy_retirement.freeze import (
    get_legacy_v1_write_freeze_state,
    set_legacy_v1_write_freeze_state,
)
from productflow_backend.infrastructure.db.session import get_session_factory

ENABLE_CONFIRMATION = "FREEZE_V1_WRITES"
DISABLE_CONFIRMATION = "UNFREEZE_V1_WRITES"


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="查看或切换旧 schema-v1 工作流的数据库写入冻结状态")
    subparsers = parser.add_subparsers(dest="action", required=True)
    subparsers.add_parser("status", help="只读查看当前冻结状态")
    enable = subparsers.add_parser("enable", help="阻止新的 v1 写入、运行、重试和取消请求")
    enable.add_argument("--confirm", required=True, help=f"必须明确传入 {ENABLE_CONFIRMATION}")
    disable = subparsers.add_parser("disable", help="在最终切换前解除冻结")
    disable.add_argument("--confirm", required=True, help=f"必须明确传入 {DISABLE_CONFIRMATION}")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    session = get_session_factory()()
    try:
        if args.action == "status":
            state = get_legacy_v1_write_freeze_state(session)
        elif args.action == "enable":
            if args.confirm != ENABLE_CONFIRMATION:
                raise SystemExit(f"enable 需要 --confirm {ENABLE_CONFIRMATION}")
            state = set_legacy_v1_write_freeze_state(session, frozen=True)
        else:
            if args.confirm != DISABLE_CONFIRMATION:
                raise SystemExit(f"disable 需要 --confirm {DISABLE_CONFIRMATION}")
            state = set_legacy_v1_write_freeze_state(session, frozen=False)
    finally:
        session.close()

    print(
        json.dumps(
            {
                "schema_version": 1,
                "configured": state.configured,
                "frozen": state.frozen,
                "valid": state.valid,
                "updated_at": state.updated_at.isoformat() if state.updated_at is not None else None,
            },
            ensure_ascii=False,
            sort_keys=True,
        )
    )
    return 0 if state.valid else 2


if __name__ == "__main__":
    raise SystemExit(main())
