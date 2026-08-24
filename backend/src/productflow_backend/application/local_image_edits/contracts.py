"""Strict, DB-free contracts for masked local image edits.

The browser is expected to map its viewport selection into source-image pixel
coordinates before calling :func:`validate_and_normalize_local_edit_mask`.
This module records the viewport-to-source transform for auditability, but it
does not guess or apply a second coordinate transform.
"""

from __future__ import annotations

from io import BytesIO
from math import isfinite
from typing import Literal

from PIL import Image, UnidentifiedImageError
from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator

from productflow_backend.domain.local_image_edits import LocalImageEditOperation

LOCAL_EDIT_MASK_MIME_TYPE = "image/png"
MASK_SELECTION_SEMANTICS = "alpha_zero_edit"
MAX_LOCAL_EDIT_REFERENCE_ASSETS = 6
_AFFINE_DETERMINANT_EPSILON = 1e-12


class LocalEditMaskGeometry(BaseModel):
    """Auditable browser geometry for a source-pixel mask.

    ``viewport_to_source`` is the affine tuple ``(a, b, c, d, e, f)`` with
    ``source_x = a * viewport_x + c * viewport_y + e`` and
    ``source_y = b * viewport_x + d * viewport_y + f``. The mask bytes passed
    to the validator are already in source-pixel coordinates; this transform
    is retained to explain how the browser produced those pixels.
    """

    model_config = ConfigDict(extra="forbid", frozen=True)

    source_width: int = Field(gt=0)
    source_height: int = Field(gt=0)
    viewport_width: float = Field(gt=0)
    viewport_height: float = Field(gt=0)
    viewport_to_source: tuple[float, float, float, float, float, float]
    transform_direction: Literal["viewport_to_source"] = "viewport_to_source"

    @field_validator("viewport_to_source")
    @classmethod
    def validate_affine_transform(
        cls,
        transform: tuple[float, float, float, float, float, float],
    ) -> tuple[float, float, float, float, float, float]:
        if any(not isfinite(value) for value in transform):
            raise ValueError("viewport_to_source 的六个变换参数必须是有限数值")
        a, b, c, d, _e, _f = transform
        determinant = a * d - b * c
        if not isfinite(determinant) or abs(determinant) <= _AFFINE_DETERMINANT_EPSILON:
            raise ValueError("viewport_to_source 必须是可逆的二维仿射变换")
        return transform


class LocalImageEditDraft(BaseModel):
    """Application-side intent before source/mask bytes cross the provider boundary."""

    model_config = ConfigDict(extra="forbid", frozen=True)

    operation: LocalImageEditOperation
    instruction: str | None = None
    mask_geometry: LocalEditMaskGeometry
    source_text: str | None = None
    replacement_text: str | None = None
    reference_asset_ids: tuple[str, ...] = Field(
        default_factory=tuple,
        max_length=MAX_LOCAL_EDIT_REFERENCE_ASSETS,
    )

    @field_validator("instruction", "source_text", "replacement_text")
    @classmethod
    def normalize_optional_text(cls, value: str | None) -> str | None:
        if value is None:
            return None
        normalized = value.strip()
        return normalized or None

    @field_validator("reference_asset_ids")
    @classmethod
    def validate_reference_asset_ids(cls, values: tuple[str, ...]) -> tuple[str, ...]:
        normalized = tuple(value.strip() for value in values)
        if any(not value for value in normalized):
            raise ValueError("局部编辑 reference asset id 不能为空")
        if len(set(normalized)) != len(normalized):
            raise ValueError("局部编辑 reference asset id 不能重复")
        return normalized

    @model_validator(mode="after")
    def validate_operation_inputs(self) -> LocalImageEditDraft:
        if self.operation in {
            LocalImageEditOperation.REMOVE,
            LocalImageEditOperation.INPAINT,
        } and not self.instruction:
            raise ValueError(f"{self.operation.value} 操作必须提供非空 instruction")
        if self.operation == LocalImageEditOperation.REPLACE_TEXT:
            if not self.source_text or not self.replacement_text:
                raise ValueError("replace_text 必须同时提供非空 source_text 和 replacement_text")
        return self

    @property
    def provider_instruction(self) -> str:
        """Return an explicit instruction safe to pass to a masked provider."""

        if self.operation == LocalImageEditOperation.REPLACE_TEXT:
            context = f"；补充要求：{self.instruction}" if self.instruction else ""
            return (
                f"将图中原文字“{self.source_text}”替换为“{self.replacement_text}”，"
                f"只修改 mask 指定区域，不改变其他内容{context}。"
            )
        return self.instruction or ""


class NormalizedLocalEditMask(BaseModel):
    """Canonical source-pixel PNG and metadata accepted by the provider adapter."""

    model_config = ConfigDict(extra="forbid", frozen=True)

    bytes_data: bytes = Field(min_length=1)
    mime_type: Literal["image/png"] = LOCAL_EDIT_MASK_MIME_TYPE
    width: int = Field(gt=0)
    height: int = Field(gt=0)
    selected_pixel_count: int = Field(gt=0)
    protected_pixel_count: int = Field(gt=0)
    selection_semantics: Literal["alpha_zero_edit"] = MASK_SELECTION_SEMANTICS
    geometry: LocalEditMaskGeometry

    @property
    def mask_bytes(self) -> bytes:
        """Alias used by callers that name the normalized payload as a mask."""

        return self.bytes_data


def validate_and_normalize_local_edit_mask(
    *,
    source_width: int,
    source_height: int,
    mask_png_bytes: bytes,
    geometry: LocalEditMaskGeometry,
) -> NormalizedLocalEditMask:
    """Validate a source-sized PNG mask and emit deterministic canonical PNG bytes.

    Mask semantics are fixed to ``alpha=0`` for fully editable pixels and
    ``alpha=255`` for fully protected pixels. Partial alpha is preserved as
    brush feathering. The function deliberately does not transform viewport
    coordinates.
    """

    if source_width <= 0 or source_height <= 0:
        raise ValueError("source image 尺寸必须为正数")
    if (geometry.source_width, geometry.source_height) != (source_width, source_height):
        raise ValueError("mask geometry 的 source 尺寸必须匹配已核验的源图尺寸")
    if not isinstance(mask_png_bytes, bytes) or not mask_png_bytes:
        raise ValueError("局部编辑 mask 必须是非空 PNG bytes")

    try:
        with Image.open(BytesIO(mask_png_bytes)) as image:
            if image.format != "PNG":
                raise ValueError("局部编辑 mask 必须是真实 PNG，不能使用 JPEG 或 WEBP")
            if image.size != (source_width, source_height):
                raise ValueError("局部编辑 mask 像素尺寸必须匹配源图尺寸")
            rgba = image.convert("RGBA")
            alpha_values = rgba.getchannel("A").tobytes()
    except (OSError, UnidentifiedImageError) as exc:
        raise ValueError("局部编辑 mask 不是可读取的 PNG") from exc

    selected_pixel_count = sum(alpha < 255 for alpha in alpha_values)
    protected_pixel_count = sum(alpha > 0 for alpha in alpha_values)
    if selected_pixel_count == 0:
        raise ValueError("局部编辑 mask 不能没有可编辑像素")
    if protected_pixel_count == 0:
        raise ValueError("局部编辑 mask 不能覆盖整张图，必须保留保护像素")

    canonical_alpha = Image.frombytes("L", (source_width, source_height), alpha_values)
    canonical = Image.new("RGBA", (source_width, source_height), (255, 255, 255, 255))
    canonical.putalpha(canonical_alpha)
    output = BytesIO()
    canonical.save(output, format="PNG", optimize=True)

    return NormalizedLocalEditMask(
        bytes_data=output.getvalue(),
        width=source_width,
        height=source_height,
        selected_pixel_count=selected_pixel_count,
        protected_pixel_count=protected_pixel_count,
        geometry=geometry,
    )
