from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from pathlib import Path

from productflow_backend.application.legacy_retirement.snapshots import (
    DEFAULT_ARCHIVE_EXPORT_PAGE_SIZE,
    MAX_ARCHIVE_EXPORT_PAGE_SIZE,
    export_legacy_archive_page,
    render_archive_export_csv,
)
from productflow_backend.config import get_settings
from productflow_backend.infrastructure.db.session import get_engine


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="只读导出旧工作流、用户模板或 Canvas Agent 归档快照")
    parser.add_argument(
        "--kind",
        required=True,
        choices=("workflow", "user_template", "canvas_agent_thread"),
    )
    parser.add_argument(
        "--limit",
        type=int,
        default=DEFAULT_ARCHIVE_EXPORT_PAGE_SIZE,
        help=f"单页数量，范围 1-{MAX_ARCHIVE_EXPORT_PAGE_SIZE}",
    )
    parser.add_argument("--after", default="", help="上一页返回的 resume cursor")
    parser.add_argument(
        "--source-id",
        "--workflow-id",
        dest="source_id",
        help="只导出一个源记录；--workflow-id 是 workflow 场景别名",
    )
    parser.add_argument("--output-json", type=Path, help="完整版本化快照 page JSON")
    parser.add_argument("--output-csv", type=Path, help="不含 payload 内容的对账 CSV")
    parser.add_argument("--compact", action="store_true", help="标准输出使用紧凑 JSON")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    settings = get_settings()
    page = export_legacy_archive_page(
        get_engine(),
        storage_root=settings.storage_root,
        kind=args.kind,
        limit=args.limit,
        after=args.after,
        source_id=args.source_id,
    )
    rendered = json.dumps(
        page.model_dump(mode="json"),
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
        args.output_csv.write_text(render_archive_export_csv(page), encoding="utf-8")
    print(rendered)
    return 0 if not page.blocking_issue_codes else 2


if __name__ == "__main__":
    raise SystemExit(main())
