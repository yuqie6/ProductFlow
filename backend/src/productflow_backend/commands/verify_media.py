from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from dataclasses import asdict

from productflow_backend.application.media_assets import verify_pending_media_objects
from productflow_backend.infrastructure.db.session import get_session_factory


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="核验 legacy_pending MediaObject 的实际图片文件")
    parser.add_argument("--batch-size", type=int, default=100)
    parser.add_argument("--after", dest="after_id")
    parser.add_argument("--dry-run", action="store_true")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    session = get_session_factory()()
    try:
        result = verify_pending_media_objects(
            session,
            batch_size=args.batch_size,
            after_id=args.after_id,
            dry_run=args.dry_run,
        )
    finally:
        session.close()
    print(json.dumps(asdict(result), ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
