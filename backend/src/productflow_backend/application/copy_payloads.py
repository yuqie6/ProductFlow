from __future__ import annotations

from typing import Any

from productflow_backend.application.contracts import (
    BlocksCopyContent,
    CopyNodeConfigV2,
    CopyPayloadV2,
    CopySlotRequest,
    FreeformCopyContent,
    LayoutBriefCopyContent,
)
from productflow_backend.infrastructure.db.models import CopySet


def normalize_copy_node_config(raw_config: dict[str, Any] | None) -> CopyNodeConfigV2:
    config = raw_config or {}
    instruction = _string_or_empty(config.get("instruction"))
    output_mode = _string_or_none(config.get("output_mode")) or _infer_output_mode(instruction)
    requested_slots = config.get("requested_slots")
    if requested_slots is not None and not isinstance(requested_slots, list):
        raise ValueError("文案 requested_slots 必须是数组")
    return CopyNodeConfigV2.model_validate(
        {
            "version": 2,
            "instruction": instruction,
            "purpose": _string_or_none(config.get("purpose")),
            "channel": _string_or_none(config.get("channel")),
            "tone": _string_or_none(config.get("tone")),
            "output_mode": output_mode,
            "requested_slots": normalize_copy_slot_requests(requested_slots) if requested_slots is not None else [],
        }
    )


def normalize_copy_slot_requests(raw_slots: Any) -> list[dict[str, Any]]:
    if not isinstance(raw_slots, list):
        raise ValueError("文案 requested_slots 必须是数组")
    normalized: list[dict[str, Any]] = []
    for index, raw_slot in enumerate(raw_slots, start=1):
        if isinstance(raw_slot, CopySlotRequest):
            normalized.append(raw_slot.model_dump(mode="json"))
            continue
        if isinstance(raw_slot, str):
            label = raw_slot.strip()
            if not label:
                raise ValueError("文案 requested_slots 数组项不能为空")
            normalized.append({"key": f"slot_{index}", "label": label, "required": False, "hint": None})
            continue
        if isinstance(raw_slot, dict):
            normalized.append(CopySlotRequest.model_validate(raw_slot).model_dump(mode="json"))
            continue
        raise ValueError("文案 requested_slots 数组项必须是对象或文本")
    return normalized


def validate_copy_payload(raw_payload: Any, *, fallback_purpose: str | None = None) -> CopyPayloadV2:
    """Validate a copy payload without repairing model-output shapes."""

    if isinstance(raw_payload, CopyPayloadV2):
        payload = raw_payload
        if fallback_purpose and not payload.purpose:
            return payload.model_copy(update={"purpose": fallback_purpose})
        return payload
    if not isinstance(raw_payload, dict):
        raise ValueError("文案 payload 必须是对象")
    payload_dict = dict(raw_payload)
    if fallback_purpose and not payload_dict.get("purpose"):
        payload_dict["purpose"] = fallback_purpose
    return CopyPayloadV2.model_validate(payload_dict)


def copy_set_structured_payload(copy_set: CopySet) -> CopyPayloadV2:
    if isinstance(copy_set.structured_payload, dict):
        return validate_copy_payload(copy_set.structured_payload)
    raise ValueError("文案版本缺少 structured_payload")


def copy_payload_context_text(payload: CopyPayloadV2) -> str:
    parts = [f"摘要：{payload.summary}"]
    if payload.purpose:
        parts.append(f"用途：{payload.purpose}")
    if isinstance(payload.content, FreeformCopyContent):
        parts.append(f"正文：{payload.content.text}")
    elif isinstance(payload.content, BlocksCopyContent):
        for block in payload.content.blocks:
            prefix = " / ".join(part for part in (block.label, block.role) if part)
            body = block.text
            if block.note:
                body = f"{body}（{block.note}）"
            if block.visual_hint:
                body = f"{body}；视觉建议：{block.visual_hint}"
            parts.append(f"{prefix}：{body}" if prefix else body)
    elif isinstance(payload.content, LayoutBriefCopyContent):
        for section in payload.content.sections:
            title = f"{section.title}：" if section.title else ""
            body = section.body or ""
            item_text = "；".join(
                f"{item.label or item.role or '条目'}：{item.text}" for item in section.items
            )
            visual = f"；视觉建议：{section.visual_hint}" if section.visual_hint else ""
            parts.append(f"{title}{body}{('；' + item_text) if item_text else ''}{visual}")
    if payload.visual_guidance:
        guidance = payload.visual_guidance
        if guidance.main_message:
            parts.append(f"主信息：{guidance.main_message}")
        if guidance.composition_hint:
            parts.append(f"构图建议：{guidance.composition_hint}")
        if guidance.hierarchy:
            parts.append(f"信息层级：{' > '.join(guidance.hierarchy)}")
        if guidance.avoid:
            parts.append(f"避免：{'、'.join(guidance.avoid)}")
    return "\n".join(part for part in parts if part.strip())


def copy_payload_to_output(payload: CopyPayloadV2) -> dict[str, Any]:
    return {
        "structured_payload": payload.model_dump(mode="json"),
        "summary": f"文案：{payload.summary}",
    }


def _infer_output_mode(instruction: str) -> str:
    if any(keyword in instruction for keyword in ("层级", "布局", "留白", "构图", "信息图")):
        return "layout_brief"
    if any(keyword in instruction for keyword in ("步骤", "规格", "卖点", "清单", "对比", "标签")):
        return "blocks"
    return "freeform"


def _string_or_empty(value: Any) -> str:
    return value.strip() if isinstance(value, str) else ""


def _string_or_none(value: Any) -> str | None:
    value = value.strip() if isinstance(value, str) else ""
    return value or None
