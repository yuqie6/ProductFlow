from __future__ import annotations

from datetime import UTC, datetime
from typing import Any

from sqlalchemy.orm import Session

from productflow_backend.application.launch_kit.query import get_launch_kit
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.infrastructure.db.models import LaunchKit

MAX_TITLE_CHARS = 255
MAX_HOOK_CHARS = 500
MAX_DESCRIPTION_CHARS = 8_000
MAX_HASHTAGS = 30
MAX_HASHTAG_CHARS = 80


def save_launch_kit_manual_edits(
    session: Session,
    *,
    launch_kit_id: str,
    platform_blocks: list[dict[str, Any]],
) -> LaunchKit:
    """Persist seller edits into the manual export snapshot while preserving generated originals."""

    launch_kit = get_launch_kit(session, launch_kit_id)
    snapshot = dict(launch_kit.export_snapshot_json or {})
    manual_export = dict(snapshot.get("manual_export") or {})
    if not manual_export:
        raise BusinessValidationError("LaunchKit has no manual export yet. Generate it before editing.")

    original_blocks = _list_of_dicts(manual_export.get("original_platform_blocks"))
    current_blocks = _list_of_dicts(manual_export.get("platform_blocks"))
    if not current_blocks:
        raise BusinessValidationError("LaunchKit has no editable platform blocks yet. Generate it first.")
    if len(platform_blocks) != len(current_blocks):
        raise BusinessValidationError("Edited platform block count must match the generated export blocks.")

    validated_blocks = [
        _validated_platform_block(input_block, current_blocks[index])
        for index, input_block in enumerate(platform_blocks)
    ]
    now = datetime.now(UTC)
    manual_export["original_platform_blocks"] = original_blocks or current_blocks
    manual_export["platform_blocks"] = validated_blocks
    manual_export["edited"] = True
    manual_export["edited_at"] = now.isoformat()
    manual_export["edit_count"] = int(manual_export.get("edit_count") or 0) + 1
    snapshot["manual_export"] = manual_export
    launch_kit.export_snapshot_json = snapshot

    feedback = dict(launch_kit.seller_feedback_json or {})
    metrics = dict(feedback.get("metrics") or {})
    metrics["manual_content_edits"] = int(metrics.get("manual_content_edits") or 0) + 1
    metrics["last_manual_content_edit_at"] = now.isoformat()
    feedback["edited"] = True
    feedback["metrics"] = metrics
    launch_kit.seller_feedback_json = feedback
    launch_kit.updated_at = now
    session.commit()
    session.expire_all()
    return get_launch_kit(session, launch_kit_id)


def _validated_platform_block(input_block: dict[str, Any], existing_block: dict[str, Any]) -> dict[str, Any]:
    platform = str(existing_block.get("platform") or input_block.get("platform") or "").strip()
    if platform and input_block.get("platform") not in (None, platform):
        raise BusinessValidationError("Edited platform cannot change from the generated block.")
    title = _required_string(input_block.get("title"), "title", MAX_TITLE_CHARS)
    hook = _optional_string(input_block.get("hook"), "hook", MAX_HOOK_CHARS)
    description = _required_string(input_block.get("description"), "description", MAX_DESCRIPTION_CHARS)
    hashtags = _hashtags(input_block.get("hashtags"))
    return {
        **existing_block,
        "platform": platform,
        "title": title,
        "hook": hook,
        "description": description,
        "bullet_points": _list_of_strings(existing_block.get("bullet_points")),
        "hashtags": hashtags,
    }


def _required_string(value: Any, field_name: str, max_chars: int) -> str:
    text = _optional_string(value, field_name, max_chars)
    if not text:
        raise BusinessValidationError(f"{field_name} is required")
    return text


def _optional_string(value: Any, field_name: str, max_chars: int) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise BusinessValidationError(f"{field_name} must be a string")
    text = value.strip()
    if len(text) > max_chars:
        raise BusinessValidationError(f"{field_name} must be at most {max_chars} characters")
    return text


def _hashtags(value: Any) -> list[str]:
    if not isinstance(value, list):
        raise BusinessValidationError("hashtags must be a list")
    if len(value) > MAX_HASHTAGS:
        raise BusinessValidationError(f"hashtags must contain at most {MAX_HASHTAGS} items")
    tags: list[str] = []
    for item in value:
        tag = _optional_string(item, "hashtag", MAX_HASHTAG_CHARS)
        if not tag:
            continue
        tags.append(tag if tag.startswith("#") else f"#{tag}")
    return tags


def _list_of_dicts(value: Any) -> list[dict[str, Any]]:
    return [item for item in value if isinstance(item, dict)] if isinstance(value, list) else []


def _list_of_strings(value: Any) -> list[str]:
    return [item for item in value if isinstance(item, str)] if isinstance(value, list) else []
