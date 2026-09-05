#!/usr/bin/env python3
"""Check the production Web entry and workbench route chunks for regressions."""

from __future__ import annotations

import gzip
import json
import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DIST = ROOT / "web" / "dist"
ASSETS = DIST / "assets"
BUDGETS = {
    "app entry": ("entry", 1_000_000, 300_000),
    "workbench loader": ("ProductWorkbenchPage-*.js", 100_000, 40_000),
    "workbench shell": ("ProductWorkbenchSurface-*.js", 550_000, 170_000),
}


def format_bytes(value: int) -> str:
    return f"{value / 1024:.1f} KiB"


def one_match(pattern: str) -> Path:
    matches = sorted(ASSETS.glob(pattern))
    if len(matches) != 1:
        raise RuntimeError(f"expected one asset matching {pattern!r}, found {len(matches)}")
    return matches[0]


def entry_asset() -> Path:
    html = (DIST / "index.html").read_text(encoding="utf-8")
    match = re.search(r'<script[^>]+src="/assets/(index-[^"]+\.js)"', html)
    if not match:
        raise RuntimeError("index.html does not reference a JavaScript entry asset")
    asset = ASSETS / match.group(1)
    if not asset.is_file():
        raise RuntimeError(f"entry asset does not exist: {asset}")
    return asset


def check(name: str, asset: Path, raw_limit: int, gzip_limit: int) -> dict[str, object]:
    raw = asset.read_bytes()
    compressed = gzip.compress(raw, compresslevel=9)
    result = {
        "name": name,
        "asset": str(asset.relative_to(ROOT)),
        "bytes": len(raw),
        "gzip_bytes": len(compressed),
        "raw_budget": raw_limit,
        "gzip_budget": gzip_limit,
    }
    if len(raw) > raw_limit or len(compressed) > gzip_limit:
        raise RuntimeError(
            f"{name} exceeds budget: raw={format_bytes(len(raw))}/{format_bytes(raw_limit)}, "
            f"gzip={format_bytes(len(compressed))}/{format_bytes(gzip_limit)}"
        )
    return result


def main() -> int:
    try:
        checks = []
        for name, (pattern, raw_limit, gzip_limit) in BUDGETS.items():
            asset = entry_asset() if pattern == "entry" else one_match(pattern)
            checks.append(check(name, asset, raw_limit, gzip_limit))
    except (OSError, RuntimeError) as exc:
        print(f"web bundle budget failed: {exc}", file=sys.stderr)
        return 1
    print(json.dumps({"checks": checks}, ensure_ascii=True, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
