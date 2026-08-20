from __future__ import annotations

from helpers import _make_demo_image_bytes_with_size

from productflow_backend.application.image_sessions.generation import (
    extract_image_generation_provider_metadata,
    normalize_image_generation_tool_options,
    provider_output_with_actual_image_size,
    unique_image_generation_ids,
)


def test_image_generation_core_normalizes_ids_and_tool_options() -> None:
    assert unique_image_generation_ids(["ref-1", "ref-2", "ref-1"]) == ["ref-1", "ref-2"]
    assert normalize_image_generation_tool_options(
        {
            "quality": "high",
            "background": "transparent",
            "output_compression": 75,
        },
        allowed_fields=("quality", "output_compression"),
    ) == {
        "quality": "high",
        "output_compression": 75,
    }

def test_image_generation_core_merges_actual_size_metadata_without_dropping_provider_notes() -> None:
    provider_output = provider_output_with_actual_image_size(
        {
            "_productflow": {"notes": [{"kind": "fallback", "message": "fallback used"}]},
            "provider_response_id": "response-1",
        },
        requested_size="2048x2048",
        image_bytes=_make_demo_image_bytes_with_size(1024, 1024),
    )

    assert provider_output["provider_response_id"] == "response-1"
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


def test_image_generation_core_projects_safe_provider_metadata() -> None:
    metadata = extract_image_generation_provider_metadata(
        {
            "_productflow": {
                "actual_image_size": " 1024x1024 ",
                "notes": [
                    {"kind": "fallback", "message": " fallback used "},
                    {"kind": "empty", "message": "   "},
                    {"kind": "missing"},
                    "unsafe note",
                ],
            },
            "raw": {"hidden": True},
        }
    )

    assert metadata.actual_image_size == "1024x1024"
    assert metadata.notes == ("fallback used",)
    assert extract_image_generation_provider_metadata(None).actual_image_size is None
    assert extract_image_generation_provider_metadata({"_productflow": "invalid"}).notes == ()
