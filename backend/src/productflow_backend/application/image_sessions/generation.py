"""连续生图请求规范化与实测输出。

requested size 是生成意图；actual_image_size 是测得输出，两者分开记录。
"""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass
from typing import Any

from productflow_backend.config import filter_image_tool_options, parse_image_tool_allowed_fields
from productflow_backend.infrastructure.image.base import image_dimensions_from_bytes
from productflow_backend.infrastructure.runtime_settings import get_runtime_settings


@dataclass(frozen=True, slots=True)
class ImageGenerationProviderMetadata:
    actual_image_size: str | None = None
    notes: tuple[str, ...] = ()


def extract_image_generation_provider_metadata(
    provider_output_json: Mapping[str, Any] | None,
) -> ImageGenerationProviderMetadata:
    if not isinstance(provider_output_json, Mapping):
        return ImageGenerationProviderMetadata()
    metadata = provider_output_json.get("_productflow")
    if not isinstance(metadata, Mapping):
        return ImageGenerationProviderMetadata()

    raw_actual_size = metadata.get("actual_image_size")
    actual_image_size = raw_actual_size.strip() if isinstance(raw_actual_size, str) else None
    if not actual_image_size:
        actual_image_size = None

    raw_notes = metadata.get("notes")
    messages: list[str] = []
    if isinstance(raw_notes, list):
        for note in raw_notes:
            if not isinstance(note, Mapping):
                continue
            raw_message = note.get("message")
            message = raw_message.strip() if isinstance(raw_message, str) else ""
            if message:
                messages.append(message)
    return ImageGenerationProviderMetadata(actual_image_size=actual_image_size, notes=tuple(messages))


def unique_image_generation_ids(ids: list[str] | None) -> list[str]:
    seen: set[str] = set()
    values: list[str] = []
    for item in ids or []:
        if item in seen:
            continue
        seen.add(item)
        values.append(item)
    return values


def normalize_image_generation_tool_options(
    tool_options: dict[str, Any] | None,
    *,
    allowed_fields: tuple[str, ...] | None = None,
) -> dict[str, Any] | None:
    resolved_allowed_fields = allowed_fields
    if resolved_allowed_fields is None:
        resolved_allowed_fields = parse_image_tool_allowed_fields(get_runtime_settings().image_tool_allowed_fields)
    normalized = filter_image_tool_options(tool_options, allowed_fields=resolved_allowed_fields)
    return normalized or None


def provider_output_with_actual_image_size(
    provider_output_json: dict[str, Any] | None,
    *,
    requested_size: str,
    image_bytes: bytes,
) -> dict[str, Any]:
    """把测得像素尺寸写入输出元数据，不回写请求尺寸。"""

    output = dict(provider_output_json or {})
    dimensions = image_dimensions_from_bytes(image_bytes)
    if dimensions is None:
        return output

    actual_size = f"{dimensions[0]}x{dimensions[1]}"
    metadata = output.get("_productflow")
    metadata = dict(metadata) if isinstance(metadata, dict) else {}
    metadata["actual_image_size"] = actual_size
    if actual_size != requested_size:
        raw_notes = metadata.get("notes")
        notes = [note for note in raw_notes if isinstance(note, dict)] if isinstance(raw_notes, list) else []
        if not any(note.get("kind") == "actual_size_mismatch" for note in notes):
            notes.append(
                {
                    "kind": "actual_size_mismatch",
                    "message": f"供应商实际返回 {actual_size}，请求尺寸为 {requested_size}。",
                    "requested_size": requested_size,
                    "actual_size": actual_size,
                }
            )
        metadata["notes"] = notes
    output["_productflow"] = metadata
    return output
