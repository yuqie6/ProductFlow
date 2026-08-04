from __future__ import annotations

from collections.abc import Callable, Mapping
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Protocol

from productflow_backend.application.contracts import ReferenceImageInput
from productflow_backend.config import filter_image_tool_options
from productflow_backend.infrastructure.image.base import image_dimensions_from_bytes


class StoredImageReference(Protocol):
    id: str
    storage_path: str
    mime_type: str
    original_filename: str


@dataclass(frozen=True, slots=True)
class ImageGenerationReferencePayload:
    source_image: Path | None
    reference_images: list[ReferenceImageInput]


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


def normalize_image_generation_tool_options(tool_options: dict[str, Any] | None) -> dict[str, Any] | None:
    normalized = filter_image_tool_options(tool_options)
    if not normalized:
        return None
    normalized.pop("n", None)
    return normalized or None


def unique_image_generation_references[T: StoredImageReference](references: list[T]) -> list[T]:
    unique_by_path: dict[str, T] = {}
    for reference in references:
        unique_by_path.setdefault(reference.storage_path, reference)
    return list(unique_by_path.values())


def build_stored_image_reference_payload(
    references: list[StoredImageReference],
    *,
    resolve_storage_path: Callable[[str], str | Path],
) -> ImageGenerationReferencePayload:
    unique_references = unique_image_generation_references(references)
    reference_inputs = [
        ReferenceImageInput(
            path=Path(resolve_storage_path(reference.storage_path)),
            mime_type=reference.mime_type,
            filename=reference.original_filename,
        )
        for reference in unique_references
    ]
    return ImageGenerationReferencePayload(
        source_image=reference_inputs[0].path if reference_inputs else None,
        reference_images=reference_inputs,
    )


def provider_output_with_actual_image_size(
    provider_output_json: dict[str, Any] | None,
    *,
    requested_size: str,
    image_bytes: bytes,
) -> dict[str, Any]:
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
