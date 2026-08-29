#!/usr/bin/env python3
"""Dump live FastAPI OpenAPI and route table into contracts/. Requires dummy env only."""

from __future__ import annotations

import json
import os
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
BACKEND = ROOT / "backend"
OUT = ROOT / "contracts"


def _ensure_export_env() -> None:
    os.environ.setdefault("ADMIN_ACCESS_KEY", "contract-export-admin-key")
    os.environ.setdefault("SESSION_SECRET", "contract-export-session-secret")
    os.environ.setdefault("DATABASE_URL", "postgresql+psycopg://productflow:export@127.0.0.1:15432/productflow")
    os.environ.setdefault("REDIS_URL", "redis://127.0.0.1:16379/0")
    os.environ.setdefault("STORAGE_ROOT", str(ROOT / "backend" / "storage"))


def main() -> int:
    _ensure_export_env()
    sys.path.insert(0, str(BACKEND / "src"))
    from productflow_backend.presentation.api import create_app

    app = create_app()
    schema = app.openapi()
    routes: list[dict[str, str]] = []
    for route in app.routes:
        methods = sorted(getattr(route, "methods", None) or [])
        path = getattr(route, "path", None)
        if path is None or not methods:
            continue
        for method in methods:
            if method in {"HEAD", "OPTIONS"}:
                continue
            routes.append(
                {
                    "method": method,
                    "path": path,
                    "name": getattr(route, "name", "") or "",
                }
            )
    routes.sort(key=lambda item: (item["path"], item["method"]))

    OUT.mkdir(parents=True, exist_ok=True)
    (OUT / "openapi.json").write_text(json.dumps(schema, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    (OUT / "http-routes.json").write_text(json.dumps(routes, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"wrote {len(routes)} routes to {OUT.relative_to(ROOT)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
