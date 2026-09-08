#!/usr/bin/env python3
"""Freeze and verify the source/tool identity for a capacity run."""

from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
import sys
from pathlib import Path
from typing import Any


FIXTURE_PATH = Path("go/internal/auth/saas_capacity_fixture_test.go")
ALLOWED_UNTRACKED_PREFIXES = ("scripts/saas_capacity/", str(FIXTURE_PATH))


def _source_files(repo_root: Path) -> list[Path]:
    files = sorted((repo_root / "scripts/saas_capacity").glob("*.py"))
    files.extend(sorted((repo_root / "scripts/saas_capacity").glob("*.sh")))
    files.append(repo_root / FIXTURE_PATH)
    return [path for path in files if path.is_file()]


def _git(repo_root: Path, *args: str) -> str:
    # Preserve the leading status column in porcelain output; only remove line
    # terminators so a modified path remains parseable as ``XY path``.
    return subprocess.check_output(["git", *args], cwd=repo_root, text=True, stderr=subprocess.STDOUT).rstrip("\r\n")


def _tracked_status(repo_root: Path) -> list[str]:
    output = _git(repo_root, "status", "--porcelain", "--untracked-files=all")
    unexpected: list[str] = []
    for line in output.splitlines():
        if not line:
            continue
        path = line[3:]
        if any(path == prefix or path.startswith(prefix) for prefix in ALLOWED_UNTRACKED_PREFIXES):
            continue
        unexpected.append(line)
    return unexpected


def _source_payload(repo_root: Path) -> dict[str, Any]:
    unexpected = _tracked_status(repo_root)
    if unexpected:
        raise RuntimeError(f"unexpected checkout changes: {'; '.join(unexpected)}")
    files = {
        str(path.relative_to(repo_root)): hashlib.sha256(path.read_bytes()).hexdigest()
        for path in _source_files(repo_root)
    }
    if str(FIXTURE_PATH) not in files:
        raise RuntimeError(f"capacity fixture is missing: {repo_root / FIXTURE_PATH}")
    return {
        "git_commit": _git(repo_root, "rev-parse", "HEAD"),
        "python_version": sys.version.split()[0],
        "files": files,
    }


def source_identity(repo_root: Path) -> tuple[dict[str, Any], str]:
    payload = _source_payload(repo_root)
    encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return payload, hashlib.sha256(encoded).hexdigest()


def write_identity(repo_root: Path, path: Path, image_name: str) -> dict[str, Any]:
    payload, source_hash = source_identity(repo_root)
    image = {"name": image_name, "id": None, "repo_digests": []}
    if path.exists():
        existing = json.loads(path.read_text(encoding="utf-8"))
        if existing.get("source_identity_sha256") != source_hash:
            raise RuntimeError(f"source identity changed since {path} was created")
        image = existing.get("image") or image
        if image.get("name") != image_name:
            raise RuntimeError(f"image name changed since {path} was created")
    result = {
        **payload,
        "source_identity_sha256": source_hash,
        "image": image,
    }
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return result


def verify_identity(repo_root: Path, path: Path) -> dict[str, Any]:
    if not path.exists():
        raise RuntimeError(f"capacity source identity is missing: {path}")
    recorded = json.loads(path.read_text(encoding="utf-8"))
    payload, source_hash = source_identity(repo_root)
    if recorded.get("source_identity_sha256") != source_hash:
        raise RuntimeError(
            f"capacity source identity mismatch: recorded={recorded.get('source_identity_sha256')} current={source_hash}"
        )
    if recorded.get("git_commit") != payload["git_commit"] or recorded.get("files") != payload["files"]:
        raise RuntimeError("capacity source identity content differs from the recorded candidate")
    image = recorded.get("image") or {}
    if not image.get("name") or not image.get("id"):
        raise RuntimeError("capacity image identity is incomplete")
    return recorded


def attach_image(path: Path, image_name: str, image_id: str, repo_digests: list[str]) -> dict[str, Any]:
    recorded = json.loads(path.read_text(encoding="utf-8"))
    image = recorded.setdefault("image", {})
    if image.get("name") != image_name:
        raise RuntimeError(f"image name changed while attaching identity: {image_name}")
    old_id = image.get("id")
    if old_id and old_id != image_id:
        raise RuntimeError(f"image identity changed: recorded={old_id} current={image_id}")
    image.update({"name": image_name, "id": image_id, "repo_digests": repo_digests})
    path.write_text(json.dumps(recorded, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return recorded


def field_value(result: dict[str, Any], field: str) -> Any:
    value: Any = result
    for part in field.split("."):
        if not isinstance(value, dict):
            return ""
        value = value.get(part, "")
    return value


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo-root", type=Path, required=True)
    parser.add_argument("--write", type=Path)
    parser.add_argument("--verify", type=Path)
    parser.add_argument("--source-hash", action="store_true")
    parser.add_argument("--field")
    parser.add_argument("--attach-image", type=Path)
    parser.add_argument("--image-name")
    parser.add_argument("--image-id")
    parser.add_argument("--repo-digests", default="")
    args = parser.parse_args()
    if args.source_hash:
        _payload, source_hash = source_identity(args.repo_root)
        print(source_hash)
        return 0
    if args.write:
        result = write_identity(args.repo_root, args.write, args.image_name or "")
        if args.field:
            print(field_value(result, args.field))
        return 0
    if args.verify:
        result = verify_identity(args.repo_root, args.verify)
        if args.field:
            print(field_value(result, args.field))
        return 0
    if args.attach_image:
        if not args.image_name or not args.image_id:
            raise SystemExit("--attach-image requires --image-name and --image-id")
        attach_image(args.attach_image, args.image_name, args.image_id, [item for item in args.repo_digests.split(",") if item])
        return 0
    parser.error("one of --write, --verify, --source-hash, or --attach-image is required")
    return 2


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, RuntimeError, json.JSONDecodeError, subprocess.CalledProcessError) as error:
        print(f"capacity identity: {error}", file=sys.stderr)
        raise SystemExit(1) from error
