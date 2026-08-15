from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

PRIMARY_DOCS = (
    ROOT / "README.md",
    ROOT / "README.en.md",
    ROOT / "CONTEXT.md",
    ROOT / "AGENTS.md",
    ROOT / "backend/AGENTS.md",
    ROOT / "web/AGENTS.md",
    ROOT / "docs/README.md",
    ROOT / "docs/PRD.md",
    ROOT / "docs/PRD.en.md",
    ROOT / "docs/ARCHITECTURE.md",
    ROOT / "docs/ARCHITECTURE.en.md",
    ROOT / "docs/USER_GUIDE.md",
    ROOT / "docs/USER_GUIDE.en.md",
    ROOT / "docs/ROADMAP.md",
    ROOT / "docs/ROADMAP.en.md",
)

LINK_DOCS = tuple(
    sorted(
        {
            *PRIMARY_DOCS,
            ROOT / "CONTRIBUTING.md",
            ROOT / "CONTRIBUTING.en.md",
            ROOT / "SECURITY.md",
            ROOT / "SECURITY.en.md",
            *(ROOT / "docs").rglob("*.md"),
        }
    )
)

CODE_OWNERS = (
    "backend/src/productflow_backend/application/agent_product_workspaces.py",
    "backend/src/productflow_backend/application/agent_conversations.py",
    "backend/src/productflow_backend/application/agent_control.py",
    "backend/src/productflow_backend/application/agent_sync.py",
    "backend/src/productflow_backend/application/workflow_drafts/contracts.py",
    "backend/src/productflow_backend/application/workflow_drafts/materialization.py",
    "backend/src/productflow_backend/application/product_workflow/execution.py",
    "backend/src/productflow_backend/application/gallery.py",
    "backend/src/productflow_backend/application/gallery_assets.py",
    "backend/src/productflow_backend/application/gallery_mutations.py",
    "backend/src/productflow_backend/application/gallery_archives.py",
    "backend/src/productflow_backend/domain/workflow_rules.py",
    "backend/src/productflow_backend/presentation/routes/workflow_drafts.py",
    "backend/src/productflow_backend/infrastructure/logging.py",
    "web/src/App.tsx",
    "web/src/lib/api.ts",
    "web/src/lib/types.ts",
    "web/src/pages/AgentProductCreatePage.tsx",
    "web/src/pages/agent-workbench",
    "web/src/pages/product-workflow-v2",
    "web/src/pages/product-detail/image-explorer",
)

CANONICAL_ROUTE_EXCLUSIONS = {
    "/login",
    "/products/new/agent",
}


def main() -> int:
    errors: list[str] = []
    _check_required_paths(errors)
    _check_routes(errors)
    _check_markdown_links(errors)
    _check_stale_ownership(errors)
    if errors:
        print("Documentation contract check failed:")
        for error in errors:
            print(f"- {error}")
        return 1
    print("Documentation contract check passed")
    return 0


def _check_required_paths(errors: list[str]) -> None:
    for path in (*PRIMARY_DOCS, *(ROOT / path for path in CODE_OWNERS)):
        if not path.exists():
            errors.append(f"required path is missing: {path.relative_to(ROOT)}")


def _check_routes(errors: list[str]) -> None:
    app_source = (ROOT / "web/src/App.tsx").read_text(encoding="utf-8")
    routes = set(re.findall(r'\bpath="([^"]+)"', app_source)) - {"*"}
    architecture_docs = (
        ROOT / "docs/ARCHITECTURE.md",
        ROOT / "docs/ARCHITECTURE.en.md",
    )
    for doc in architecture_docs:
        content = doc.read_text(encoding="utf-8")
        for route in sorted(routes):
            if f"`{route}`" not in content:
                errors.append(f"{doc.relative_to(ROOT)} does not document route {route}")

    canonical_routes = {
        route
        for route in routes
        if route not in CANONICAL_ROUTE_EXCLUSIONS and ":" not in route
    }
    user_entry_docs = (
        ROOT / "README.md",
        ROOT / "README.en.md",
        ROOT / "docs/PRD.md",
        ROOT / "docs/PRD.en.md",
    )
    for doc in user_entry_docs:
        content = doc.read_text(encoding="utf-8")
        for route in sorted(canonical_routes):
            if f"`{route}`" not in content:
                errors.append(f"{doc.relative_to(ROOT)} does not list user entry {route}")


def _check_markdown_links(errors: list[str]) -> None:
    for doc in LINK_DOCS:
        content = doc.read_text(encoding="utf-8")
        for target in re.findall(r"\[[^\]]+\]\(([^)]+)\)", content):
            if target.startswith(("http://", "https://", "mailto:", "#")):
                continue
            path_text = target.split("#", maxsplit=1)[0]
            target_path = (doc.parent / path_text).resolve()
            if not target_path.exists():
                errors.append(f"{doc.relative_to(ROOT)} links to missing path {target}")


def _check_stale_ownership(errors: list[str]) -> None:
    stable_docs = tuple(path for path in PRIMARY_DOCS if path.name != "ROADMAP.md")
    for doc in stable_docs:
        content = doc.read_text(encoding="utf-8")
        for stale_name in ("gallery_queries.py", "product_gallery.py"):
            if stale_name in content:
                errors.append(f"{doc.relative_to(ROOT)} references retired owner {stale_name}")


if __name__ == "__main__":
    raise SystemExit(main())
