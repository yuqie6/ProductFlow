from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from helpers import _make_demo_image_bytes_with_size

from productflow_backend.application.image_generation_core import (
    build_stored_image_reference_payload,
    normalize_image_generation_tool_options,
    provider_output_with_actual_image_size,
    unique_image_generation_ids,
    unique_image_generation_references,
)


@dataclass(frozen=True, slots=True)
class StoredReference:
    id: str
    storage_path: str
    mime_type: str = "image/png"
    original_filename: str = "reference.png"


def test_image_generation_core_normalizes_ids_tool_options_and_reference_payload(configured_env: Path) -> None:
    assert unique_image_generation_ids(["ref-1", "ref-2", "ref-1"]) == ["ref-1", "ref-2"]
    assert normalize_image_generation_tool_options(
        {
            "quality": "high",
            "background": "transparent",
            "output_compression": 75,
        }
    ) == {
        "quality": "high",
        "output_compression": 75,
    }

    references = [
        StoredReference(id="asset-1", storage_path="products/p/ref.png", original_filename="ref-a.png"),
        StoredReference(id="poster-1", storage_path="products/p/ref.png", original_filename="poster.png"),
        StoredReference(id="asset-2", storage_path="products/p/other.png", original_filename="ref-b.png"),
    ]

    assert [reference.id for reference in unique_image_generation_references(references)] == ["asset-1", "asset-2"]
    payload = build_stored_image_reference_payload(
        references,
        resolve_storage_path=lambda storage_path: Path("/storage") / storage_path,
    )
    assert payload.source_image == Path("/storage/products/p/ref.png")
    assert [reference.path for reference in payload.reference_images] == [
        Path("/storage/products/p/ref.png"),
        Path("/storage/products/p/other.png"),
    ]


def test_image_generation_core_merges_actual_size_metadata_without_dropping_provider_notes() -> None:
    provider_output = provider_output_with_actual_image_size(
        {"_productflow": {"notes": [{"kind": "fallback", "message": "fallback used"}]}},
        requested_size="2048x2048",
        image_bytes=_make_demo_image_bytes_with_size(1024, 1024),
    )

    assert provider_output["_productflow"]["actual_image_size"] == "1024x1024"
    assert provider_output["_productflow"]["notes"] == [
        {"kind": "fallback", "message": "fallback used"},
        {
            "kind": "actual_size_mismatch",
            "message": "供应商实际返回 1024x1024，请求尺寸为 2048x2048。",
            "requested_size": "2048x2048",
            "actual_size": "1024x1024",
        },
    ]
