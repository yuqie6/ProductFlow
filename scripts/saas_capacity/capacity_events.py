"""Small helpers for immutable provider evidence snapshots."""

from __future__ import annotations

import hashlib
import json
from pathlib import Path
from typing import Any, Iterable


REQUIRED_PHASES = frozenset({"received", "started", "finished", "responded"})
PROMPT_MARKER = "Current user request:\n"


def prompt_matches_prefix(prompt: str, prefix: str) -> bool:
    """Match raw API prompts and the Go provider's rendered prompt template."""
    value = str(prompt)
    return value.startswith(prefix) or f"{PROMPT_MARKER}{prefix}" in value


def merge_events(path: str | Path) -> dict[str, dict[str, Any]]:
    """Merge append-only event snapshots by request id.

    A live provider file may be read while its last line is being written, so
    malformed trailing lines are ignored here. Immutable round snapshots are
    validated by ``snapshot_events`` before they are consumed for scoring.
    """

    merged: dict[str, dict[str, Any]] = {}
    event_path = Path(path)
    if not event_path.exists():
        return merged
    for line in event_path.read_text(encoding="utf-8").splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        if not isinstance(event, dict) or not event.get("request_id"):
            continue
        request_id = str(event["request_id"])
        entry = merged.setdefault(request_id, {})
        phases = set(entry.get("_phases", []))
        if event.get("phase"):
            phases.add(str(event["phase"]))
        entry.update(event)
        entry["_phases"] = sorted(phases)
    return merged


def validate_event(event: dict[str, Any]) -> dict[str, Any]:
    """Require one complete, monotonic HTTP/provider lifecycle."""
    request_id = str(event.get("request_id") or "")
    if not REQUIRED_PHASES.issubset(set(event.get("_phases", []))):
        raise RuntimeError(f"provider event lacks complete lifecycle phases: {request_id}")
    values = tuple(event.get(key) for key in (
        "received_monotonic",
        "started_monotonic",
        "finished_monotonic",
        "responded_monotonic",
    ))
    if not all(isinstance(value, (int, float)) for value in values):
        raise RuntimeError(f"provider event lacks complete monotonic/HTTP timestamps: {request_id}")
    received, started, finished, responded = (float(value) for value in values)
    if started < received or finished < started or responded < finished:
        raise RuntimeError(f"provider event contains negative or non-monotonic intervals: {request_id}")
    return event


def snapshot_events(
    source: str | Path,
    destination: str | Path,
    prompt_prefixes: Iterable[str],
) -> dict[str, Any]:
    """Copy matching raw event lines into a write-once, hashed round file."""

    source_path = Path(source)
    destination_path = Path(destination)
    prefixes = tuple(prompt_prefixes)
    if not source_path.exists():
        raise RuntimeError(f"provider event source does not exist: {source_path}")
    if destination_path.exists():
        raise RuntimeError(f"immutable provider event snapshot already exists: {destination_path}")

    destination_path.parent.mkdir(parents=True, exist_ok=True)
    matched_lines: list[str] = []
    request_ids: set[str] = set()
    for line_number, raw_line in enumerate(source_path.read_text(encoding="utf-8").splitlines(), 1):
        try:
            event = json.loads(raw_line)
        except json.JSONDecodeError as error:
            raise RuntimeError(f"invalid provider event JSON at {source_path}:{line_number}") from error
        if not isinstance(event, dict):
            raise RuntimeError(f"provider event is not an object at {source_path}:{line_number}")
        prompt = str(event.get("prompt") or "")
        if not prefixes or not any(prompt_matches_prefix(prompt, prefix) for prefix in prefixes):
            continue
        request_id = str(event.get("request_id") or "")
        if not request_id:
            raise RuntimeError(f"matching provider event has no request_id at {source_path}:{line_number}")
        matched_lines.append(raw_line)
        request_ids.add(request_id)

    if not matched_lines:
        raise RuntimeError(f"no provider events matched {prefixes!r} in {source_path}")

    encoded = ("\n".join(matched_lines) + "\n").encode("utf-8")
    with destination_path.open("xb") as stream:
        stream.write(encoded)
    return {
        "path": str(destination_path),
        "sha256": hashlib.sha256(encoded).hexdigest(),
        "line_count": len(matched_lines),
        "request_count": len(request_ids),
        "prompt_prefixes": list(prefixes),
    }


def file_sha256(path: str | Path) -> str:
    digest = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()
