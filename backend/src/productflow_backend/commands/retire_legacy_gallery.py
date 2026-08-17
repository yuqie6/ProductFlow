from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from dataclasses import asdict

from productflow_backend.application.media_library.retirement import (
    LEGACY_GALLERY_RETIRE_CONFIRMATION,
    inspect_legacy_gallery_retirement,
    retire_legacy_gallery,
)
from productflow_backend.config import get_settings
from productflow_backend.domain.errors import BusinessError
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.storage import LocalStorage


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="在独立素材库证据闸门通过后退休旧 image_gallery_entries 表"
    )
    parser.add_argument(
        "--apply",
        action="store_true",
        help="在同一事务中重新对账、删除旧表并标记 gate cleaned",
    )
    parser.add_argument("--confirm", help=f"实际清理必须传入 {LEGACY_GALLERY_RETIRE_CONFIRMATION}")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    session = get_session_factory()()
    try:
        if args.apply:
            if args.confirm != LEGACY_GALLERY_RETIRE_CONFIRMATION:
                raise ValueError(f"--apply 必须同时传入 {LEGACY_GALLERY_RETIRE_CONFIRMATION}")
            report = retire_legacy_gallery(
                session,
                storage=LocalStorage(root=get_settings().storage_root),
                confirmation=args.confirm,
            )
        else:
            report = inspect_legacy_gallery_retirement(
                session,
                storage=LocalStorage(root=get_settings().storage_root),
            )
    except (BusinessError, RuntimeError, FileNotFoundError, ValueError) as exc:
        session.rollback()
        print(json.dumps({"detail": str(exc)}, ensure_ascii=False, sort_keys=True))
        return 2
    finally:
        session.close()

    print(json.dumps(asdict(report), ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
