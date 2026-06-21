from __future__ import annotations

import re
from typing import Any

from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.infrastructure.db.models import LaunchKit


def launch_kit_export_filename(launch_kit: LaunchKit) -> str:
    base = re.sub(r"[^a-zA-Z0-9_-]+", "-", launch_kit.product.name).strip("-").lower()
    return f"{base or 'launch-kit'}-{launch_kit.id[:8]}.md"


def render_launch_kit_markdown(launch_kit: LaunchKit) -> str:
    snapshot = launch_kit.export_snapshot_json or {}
    manual_export = _dict_value(snapshot.get("manual_export"))
    if not manual_export:
        raise BusinessValidationError("LaunchKit has no manual export yet. Generate it first.")

    lines: list[str] = [
        f"# {launch_kit.product.name} LaunchKit",
        "",
        f"Category: `{launch_kit.category_key}`",
        f"Status: `{launch_kit.status}`",
        "",
    ]
    selected_angle = _dict_value(launch_kit.selected_angle_json)
    if selected_angle:
        lines.extend(
            [
                "## Buyer angle",
                "",
                f"**{selected_angle.get('label') or 'Selected angle'}**",
                "",
                str(selected_angle.get("why_it_might_work") or "").strip(),
                "",
                f"Buyer emotion: {selected_angle.get('buyer_emotion') or '—'}",
                "",
                f"Risk note: {selected_angle.get('risk') or '—'}",
                "",
            ]
        )

    platform_blocks = _list_of_dicts(manual_export.get("platform_blocks"))
    if platform_blocks:
        lines.extend(["## Platform copy blocks", ""])
    for block in platform_blocks:
        platform = block.get("platform") or "platform"
        lines.extend(
            [
                f"### {platform}",
                "",
                "**Title**",
                "",
                str(block.get("title") or "").strip(),
                "",
                "**Hook**",
                "",
                str(block.get("hook") or "").strip(),
                "",
                "**Description**",
                "",
                str(block.get("description") or "").strip(),
                "",
            ]
        )
        hashtags = _list_of_strings(block.get("hashtags"))
        if hashtags:
            lines.extend(["**Hashtags**", "", " ".join(hashtags), ""])

    image_plan = _image_plan(launch_kit)
    if image_plan:
        lines.extend(["## Image proof plan", ""])
        for item in _list_of_dicts(image_plan.get("image_sequence")):
            proof = item.get("proof_required") or "confirm proof"
            lines.append(f"- Slot {item.get('slot')}: {item.get('purpose')} — {proof}")
        if image_plan.get("cover_guidance"):
            lines.extend(["", f"Cover guidance: {image_plan['cover_guidance']}"])
        lines.append("")

    checklist = _list_of_strings(manual_export.get("checklist") or snapshot.get("checklist_items"))
    if checklist:
        lines.extend(["## Manual export checklist", ""])
        lines.extend(f"- {item}" for item in checklist)
        lines.append("")

    latest_score = max(launch_kit.quality_scores, key=lambda item: item.created_at, default=None)
    score = _dict_value(latest_score.score_json) if latest_score else {}
    if score:
        lines.extend(["## Readiness score", "", f"Overall: **{score.get('overall', 0)} / 100**", ""])
        warnings = _list_of_strings(score.get("warnings"))
        if warnings:
            lines.extend(["Warnings:", ""])
            lines.extend(f"- {warning}" for warning in warnings)
            lines.append("")

    return "\n".join(line.rstrip() for line in lines).strip() + "\n"


def _dict_value(value: Any) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def _list_of_dicts(value: Any) -> list[dict[str, Any]]:
    return [item for item in value if isinstance(item, dict)] if isinstance(value, list) else []


def _list_of_strings(value: Any) -> list[str]:
    return [item for item in value if isinstance(item, str)] if isinstance(value, list) else []


def _image_plan(launch_kit: LaunchKit) -> dict[str, Any]:
    for variant in launch_kit.variants:
        content = _dict_value(variant.content_json.get("content") if isinstance(variant.content_json, dict) else None)
        if "image_sequence" in content:
            return content
    return {}
