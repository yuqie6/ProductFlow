from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from pathlib import Path

from productflow_backend.application.legacy_retirement import audit_legacy_retirement
from productflow_backend.config import get_settings
from productflow_backend.infrastructure.db.session import get_engine


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="只读审计旧工作流、Canvas Agent 历史和媒体文件")
    parser.add_argument("--output", type=Path, help="同时把完整 JSON 报告写入指定文件")
    parser.add_argument("--compact", action="store_true", help="标准输出使用紧凑 JSON")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    settings = get_settings()
    report = audit_legacy_retirement(get_engine(), storage_root=settings.storage_root)
    payload = report.model_dump(mode="json")
    rendered = json.dumps(
        payload,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":") if args.compact else None,
        indent=None if args.compact else 2,
    )
    if args.output is not None:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(f"{rendered}\n", encoding="utf-8")
    print(rendered)
    return 0 if report.ready_for_archive else 2


if __name__ == "__main__":
    raise SystemExit(main())
