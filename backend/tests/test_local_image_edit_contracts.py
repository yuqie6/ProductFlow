from __future__ import annotations

from io import BytesIO
from math import nan

import pytest
from PIL import Image
from pydantic import ValidationError

from productflow_backend.application.local_image_edits import (
    LocalEditMaskGeometry,
    LocalImageEditDraft,
    LocalImageEditOperation,
    NormalizedLocalEditMask,
    validate_and_normalize_local_edit_mask,
)
from productflow_backend.infrastructure.image.base import LocalEditImage, LocalEditMask, LocalEditRequest


def _geometry(
    *,
    source_width: int = 4,
    source_height: int = 3,
    viewport_width: int = 400,
    viewport_height: int = 300,
    transform: tuple[float, float, float, float, float, float] = (1, 0, 0, 1, 0, 0),
) -> LocalEditMaskGeometry:
    return LocalEditMaskGeometry(
        source_width=source_width,
        source_height=source_height,
        viewport_width=viewport_width,
        viewport_height=viewport_height,
        viewport_to_source=transform,
    )


def _mask_png(
    *,
    width: int = 4,
    height: int = 3,
    selected: set[tuple[int, int]] | None = None,
    protected_alpha: int = 255,
    format: str = "PNG",
) -> bytes:
    selected = selected or set()
    mode = "RGBA" if format == "PNG" else "RGB"
    image = Image.new(mode, (width, height), (255, 255, 255, protected_alpha) if mode == "RGBA" else (255, 255, 255))
    if mode == "RGBA":
        for x, y in selected:
            image.putpixel((x, y), (255, 255, 255, 0))
    output = BytesIO()
    image.save(output, format=format)
    return output.getvalue()


def _draft(**overrides: object) -> LocalImageEditDraft:
    values: dict[str, object] = {
        "operation": LocalImageEditOperation.REMOVE,
        "instruction": "移除选区对象",
        "mask_geometry": _geometry(),
    }
    values.update(overrides)
    return LocalImageEditDraft(**values)


def test_operation_enum_is_single_authority_and_crop_is_not_provider_operation() -> None:
    assert tuple(operation.value for operation in LocalImageEditOperation) == (
        "remove",
        "replace_text",
        "inpaint",
    )
    with pytest.raises(ValidationError):
        _draft(operation="crop")
    with pytest.raises(ValidationError):
        LocalEditRequest(
            source_image=LocalEditImage(bytes_data=b"source", mime_type="image/png"),
            instruction="裁切",
            operation="crop",  # type: ignore[arg-type]
            mask=LocalEditMask(bytes_data=b"mask", mime_type="image/png"),
            size="1024x1024",
        )


@pytest.mark.parametrize(
    "transform",
    [
        (1, 0, 0, 0, 0, 0),
        (1, 2, 2, 4, 0, 0),
        (1, 0, 0, 1, nan, 0),
        (1, 0, 0, 1, 0, float("inf")),
    ],
)
def test_geometry_rejects_singular_or_non_finite_transform(transform: tuple[float, ...]) -> None:
    with pytest.raises(ValidationError):
        _geometry(transform=transform)  # type: ignore[arg-type]


def test_geometry_records_viewport_to_source_direction_and_dimensions() -> None:
    geometry = _geometry(transform=(2, 0, 0, 3, 10, -4))
    assert geometry.transform_direction == "viewport_to_source"
    assert geometry.viewport_to_source == (2, 0, 0, 3, 10, -4)
    assert (geometry.source_width, geometry.source_height) == (4, 3)
    assert (geometry.viewport_width, geometry.viewport_height) == (400, 300)


def test_mask_rejects_source_dimension_drift() -> None:
    with pytest.raises(ValueError, match="source 尺寸"):
        validate_and_normalize_local_edit_mask(
            source_width=5,
            source_height=3,
            mask_png_bytes=_mask_png(selected={(0, 0)}),
            geometry=_geometry(),
        )
    with pytest.raises(ValueError, match="像素尺寸"):
        validate_and_normalize_local_edit_mask(
            source_width=4,
            source_height=3,
            mask_png_bytes=_mask_png(width=5, selected={(0, 0)}),
            geometry=_geometry(),
        )


@pytest.mark.parametrize("image_format", ["JPEG", "WEBP"])
def test_mask_rejects_non_png_formats(image_format: str) -> None:
    with pytest.raises(ValueError, match="PNG"):
        validate_and_normalize_local_edit_mask(
            source_width=4,
            source_height=3,
            mask_png_bytes=_mask_png(selected={(0, 0)}, format=image_format),
            geometry=_geometry(),
        )


def test_mask_rejects_empty_selection_and_full_image_selection() -> None:
    with pytest.raises(ValueError, match="没有可编辑像素"):
        validate_and_normalize_local_edit_mask(
            source_width=4,
            source_height=3,
            mask_png_bytes=_mask_png(),
            geometry=_geometry(),
        )
    with pytest.raises(ValueError, match="整张图"):
        validate_and_normalize_local_edit_mask(
            source_width=4,
            source_height=3,
            mask_png_bytes=_mask_png(selected={(x, y) for x in range(4) for y in range(3)}),
            geometry=_geometry(),
        )


def test_mask_returns_source_sized_canonical_png_with_fixed_alpha_semantics() -> None:
    selected = {(0, 0), (1, 1)}
    normalized = validate_and_normalize_local_edit_mask(
        source_width=4,
        source_height=3,
        mask_png_bytes=_mask_png(selected=selected),
        geometry=_geometry(transform=(2, 0, 0, 2, 5, 6)),
    )

    assert isinstance(normalized, NormalizedLocalEditMask)
    assert normalized.mime_type == "image/png"
    assert normalized.mask_bytes == normalized.bytes_data
    assert (normalized.width, normalized.height) == (4, 3)
    assert normalized.selected_pixel_count == 2
    assert normalized.protected_pixel_count == 10
    assert normalized.selection_semantics == "alpha_zero_edit"
    assert normalized.geometry.viewport_to_source == (2, 0, 0, 2, 5, 6)
    with Image.open(BytesIO(normalized.bytes_data)) as output:
        assert output.format == "PNG"
        assert output.mode == "RGBA"
        assert output.size == (4, 3)
        alpha = output.getchannel("A")
        assert alpha.getpixel((0, 0)) == 0
        assert alpha.getpixel((1, 1)) == 0
        assert alpha.getpixel((2, 2)) == 255


def test_partial_alpha_is_preserved_as_brush_feathering() -> None:
    image = Image.new("RGBA", (4, 3), (255, 255, 255, 255))
    image.putpixel((0, 0), (255, 255, 255, 128))
    image.putpixel((1, 1), (255, 255, 255, 0))
    raw = BytesIO()
    image.save(raw, format="PNG")

    normalized = validate_and_normalize_local_edit_mask(
        source_width=4,
        source_height=3,
        mask_png_bytes=raw.getvalue(),
        geometry=_geometry(),
    )
    with Image.open(BytesIO(normalized.bytes_data)) as output:
        assert output.getchannel("A").getpixel((0, 0)) == 128
        assert output.getchannel("A").getpixel((1, 1)) == 0


def test_partial_alpha_without_fully_transparent_pixels_is_still_a_non_empty_selection() -> None:
    image = Image.new("RGBA", (4, 3), (255, 255, 255, 255))
    image.putpixel((0, 0), (255, 255, 255, 128))
    raw = BytesIO()
    image.save(raw, format="PNG")

    normalized = validate_and_normalize_local_edit_mask(
        source_width=4,
        source_height=3,
        mask_png_bytes=raw.getvalue(),
        geometry=_geometry(),
    )

    assert normalized.selected_pixel_count == 1
    assert normalized.protected_pixel_count == 12


@pytest.mark.parametrize("operation", [LocalImageEditOperation.REMOVE, LocalImageEditOperation.INPAINT])
def test_remove_and_inpaint_require_non_empty_instruction(operation: LocalImageEditOperation) -> None:
    with pytest.raises(ValidationError):
        _draft(operation=operation, instruction=" ")


def test_replace_text_requires_both_texts_and_generates_provider_instruction() -> None:
    with pytest.raises(ValidationError):
        _draft(operation=LocalImageEditOperation.REPLACE_TEXT, instruction=None)
    with pytest.raises(ValidationError):
        _draft(operation=LocalImageEditOperation.REPLACE_TEXT, source_text="原文")

    draft = _draft(
        operation=LocalImageEditOperation.REPLACE_TEXT,
        source_text="旧字",
        replacement_text="新字",
        instruction="保持原有字体",
    )
    assert "旧字" in draft.provider_instruction
    assert "新字" in draft.provider_instruction
    assert "mask" in draft.provider_instruction
    assert "保持原有字体" in draft.provider_instruction


def test_reference_asset_ids_are_trimmed_rejected_when_duplicate_or_over_limit() -> None:
    draft = _draft(reference_asset_ids=(" asset-1 ", "asset-2"))
    assert draft.reference_asset_ids == ("asset-1", "asset-2")
    with pytest.raises(ValidationError):
        _draft(reference_asset_ids=("asset-1", "asset-1"))
    with pytest.raises(ValidationError):
        _draft(reference_asset_ids=tuple(f"asset-{index}" for index in range(7)))
    with pytest.raises(ValidationError):
        _draft(reference_asset_ids=(" ",))


def test_provider_mask_requires_exact_png_mime_and_request_requires_mask() -> None:
    with pytest.raises(ValidationError):
        LocalEditMask(bytes_data=b"mask", mime_type="image/jpeg")  # type: ignore[arg-type]
    with pytest.raises(ValidationError):
        LocalEditRequest(
            source_image=LocalEditImage(bytes_data=b"source", mime_type="image/png"),
            instruction="移除选区",
            operation=LocalImageEditOperation.REMOVE,
            size="1024x1024",
        )
