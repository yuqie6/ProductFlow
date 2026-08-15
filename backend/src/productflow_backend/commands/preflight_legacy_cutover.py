from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from pathlib import Path

from productflow_backend.application.legacy_retirement.preflight import audit_legacy_cutover_preflight
from productflow_backend.config import get_settings
from productflow_backend.infrastructure.db.session import get_engine


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="只读执行旧工作流切换、运行状态和 provider 配置预检")
    parser.add_argument("--output", type=Path, help="同时把脱敏 JSON 报告写入指定文件")
    parser.add_argument("--compact", action="store_true", help="标准输出使用紧凑 JSON")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    settings = get_settings()
    report = audit_legacy_cutover_preflight(
        get_engine(),
        storage_root=settings.storage_root,
        base_settings=settings,
    )
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
    return 0 if report.ready_for_cutover else 2


if __name__ == "__main__":
    raise SystemExit(main())
