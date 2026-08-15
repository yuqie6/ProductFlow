from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from pathlib import Path

from productflow_backend.application.legacy_retirement.media_verification import verify_legacy_media
from productflow_backend.config import get_settings
from productflow_backend.infrastructure.db.session import get_session_factory


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="补齐可读取旧媒体的真实元数据并生成核验报告")
    parser.add_argument("--apply", action="store_true", help="将可解析文件的元数据和 verified 状态写入数据库")
    parser.add_argument("--output-json", type=Path, help="同时把 JSON 报告写入指定文件")
    parser.add_argument("--compact", action="store_true", help="标准输出使用紧凑 JSON")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    settings = get_settings()
    session = get_session_factory()()
    try:
        report = verify_legacy_media(
            session,
            storage_root=settings.storage_root,
            apply=args.apply,
        )
        if args.apply:
            session.commit()
    except Exception:
        session.rollback()
        raise
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
    print(rendered)
    return 2 if report.missing_count or report.invalid_count or report.path_error_count else 0


if __name__ == "__main__":
    raise SystemExit(main())
