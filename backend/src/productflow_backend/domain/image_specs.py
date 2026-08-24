"""GenerationSpec 是模型出图意图；DeliverySpec 是确定性交付派生，互不替代。"""

from __future__ import annotations

from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, StringConstraints, model_validator

DELIVERY_SPEC_MAX_TOTAL_PIXELS = 64 * 1024 * 1024

HexColor = Annotated[str, StringConstraints(pattern=r"^#[0-9A-Fa-f]{6}$")]


class _StrictImageSpec(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)


class GenerationSpec(_StrictImageSpec):
    """模型生成意图；改这些字段才会调用图片模型。"""

    aspect_ratio: Annotated[str, StringConstraints(pattern=r"^[1-9][0-9]{0,2}:[1-9][0-9]{0,2}$")]
    resolution_tier: Literal["standard", "high", "ultra"] = "high"
    quality_intent: Literal["draft", "standard", "high"] = "high"
    reference_fidelity: Literal["low", "medium", "high"] = "high"
    background_intent: Literal["auto", "opaque", "transparent"] = "auto"
    text_policy: Literal["none", "allow", "required"] = "none"
    text_language: Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=80)] | None = None

    @model_validator(mode="after")
    def validate_text_language(self) -> GenerationSpec:
        if self.text_policy == "required" and self.text_language is None:
            raise ValueError("要求图片文字时必须指定 text_language")
        if self.text_policy == "none" and self.text_language is not None:
            raise ValueError("禁止图片文字时不能指定 text_language")
        return self


class DeliverySpec(_StrictImageSpec):
    """确定性 rendition；改宽高或格式不得调用图片模型或替换生成源。"""

    width: int = Field(ge=1, le=16384)
    height: int = Field(ge=1, le=16384)
    format: Literal["png", "jpeg", "webp"]
    max_byte_size: int | None = Field(default=None, ge=1)
    fit: Literal["contain", "cover"]
    background_color: HexColor | None = None
    crop_anchor: Literal["center", "top", "bottom", "left", "right"] | None = None

    @model_validator(mode="after")
    def validate_fit_options(self) -> DeliverySpec:
        if self.width * self.height > DELIVERY_SPEC_MAX_TOTAL_PIXELS:
            raise ValueError(f"交付规格总像素不能超过 {DELIVERY_SPEC_MAX_TOTAL_PIXELS}")
        if self.fit == "contain" and self.crop_anchor is not None:
            raise ValueError("contain 交付规格不能指定 crop_anchor")
        if self.fit == "cover" and self.background_color is not None:
            raise ValueError("cover 交付规格不能指定 background_color")
        return self


__all__ = ["DELIVERY_SPEC_MAX_TOTAL_PIXELS", "DeliverySpec", "GenerationSpec"]
