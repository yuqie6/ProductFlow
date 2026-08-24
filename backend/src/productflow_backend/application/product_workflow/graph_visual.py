"""视觉 overlay 合并：字段集合由 Catalog 裁剪。"""

from __future__ import annotations

from typing import Any

from productflow_backend.domain.graph_catalog import catalog_visual_overlay


def merge_visual_override_items(items: list[Any]) -> dict[str, Any]:
    overlay: dict[str, Any] = {}
    for item in items:
        if not isinstance(item, dict):
            continue
        overrides = item.get("overrides")
        if not isinstance(overrides, list):
            continue
        for field_override in overrides:
            if not isinstance(field_override, dict):
                continue
            field_name = field_override.get("field")
            if isinstance(field_name, str) and field_name:
                overlay[field_name] = field_override.get("value")
    return overlay


def visual_overlay_from_config(config: dict[str, Any] | None) -> dict[str, Any] | None:
    payload = config or {}
    overlay = payload.get("visual_overlay")
    if isinstance(overlay, dict) and overlay:
        return catalog_visual_overlay(overlay)
    overrides = payload.get("visual_overrides")
    if isinstance(overrides, list):
        return catalog_visual_overlay(merge_visual_override_items(overrides))
    return None


def apply_visual_overlay(payload: dict[str, Any], overlay: dict[str, Any]) -> dict[str, Any]:
    merged = dict(payload)
    merged.update(overlay)
    return merged
