"""蒙版局部编辑的严格、无 DB 合同。

浏览器应在调用 :func:`validate_and_normalize_local_edit_mask` 之前，
把视口选区映射到源图像素坐标。本模块记录 viewport-to-source 变换供审计，
不再猜测或二次变换坐标。
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
    """源像素蒙版的可审计浏览器几何。

    ``viewport_to_source`` 是仿射元组 ``(a, b, c, d, e, f)``，其中
    ``source_x = a * viewport_x + c * viewport_y + e``，
    ``source_y = b * viewport_x + d * viewport_y + f``。
    交给校验器的蒙版字节已是源像素坐标；保留该变换只为说明浏览器如何得到这些像素。
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
    """源图/蒙版字节越过 provider 边界之前的应用层意图。"""

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
        """返回可安全传给蒙版 provider 的显式指令。"""

        if self.operation == LocalImageEditOperation.REPLACE_TEXT:
            context = f"；补充要求：{self.instruction}" if self.instruction else ""
            return (
                f"将图中原文字“{self.source_text}”替换为“{self.replacement_text}”，"
                f"只修改 mask 指定区域，不改变其他内容{context}。"
            )
        return self.instruction or ""


class NormalizedLocalEditMask(BaseModel):
    """provider 适配器接受的规范源像素 PNG 与元数据。"""

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
        """给把规范化载荷叫做 mask 的调用方用的别名。"""

        return self.bytes_data


def validate_and_normalize_local_edit_mask(
    *,
    source_width: int,
    source_height: int,
    mask_png_bytes: bytes,
    geometry: LocalEditMaskGeometry,
) -> NormalizedLocalEditMask:
    """校验与源图同尺寸的 PNG 蒙版，并产出确定性规范 PNG 字节。

    蒙版语义固定为 ``alpha=0`` 表示完全可编辑像素、``alpha=255`` 表示完全保护像素。
    部分 alpha 作为笔刷羽化保留。本函数不变换视口坐标。
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
