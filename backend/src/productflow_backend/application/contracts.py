from __future__ import annotations

from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel, Field, ValidationInfo, field_validator

from productflow_backend.domain.enums import PosterKind


def _normalize_ai_scalar_text(value: Any, *, field_name: str) -> Any:
    """Normalize provider JSON scalar text fields without hiding malformed structures."""

    if not isinstance(value, list):
        return value
    if not value:
        raise ValueError(f"{field_name}不能为空")
    parts: list[str] = []
    for item in value:
        if not isinstance(item, str):
            raise ValueError(f"{field_name}数组项必须是文本")
        text = item.strip()
        if not text:
            raise ValueError(f"{field_name}数组项不能为空")
        parts.append(text)
    return "、".join(parts)


class ProductInput(BaseModel):
    """文本生成器需要的最小商品信息。"""

    name: str
    category: str | None = None
    price: str | None = None
    source_note: str | None = None
    image_path: str


class CreativeBriefPayload(BaseModel):
    """商品理解结果：定位/受众/卖点/禁忌词/海报风格。"""

    positioning: str
    audience: str
    selling_angles: list[str] = Field(min_length=3, max_length=5)
    taboo_phrases: list[str] = Field(default_factory=list)
    poster_style_hint: str

    @field_validator("positioning", "audience", "poster_style_hint", mode="before")
    @classmethod
    def normalize_scalar_text(cls, value: Any, info: ValidationInfo) -> Any:
        """模型偶发把短文本字段输出成字符串数组；只接受纯文本数组并拼成展示字符串。"""

        return _normalize_ai_scalar_text(value, field_name=info.field_name)


class CopyPayload(BaseModel):
    """文案生成结果：标题/卖点/headline/CTA。"""

    title: str
    selling_points: list[str] = Field(min_length=3, max_length=5)
    poster_headline: str
    cta: str

    @field_validator("title", "poster_headline", "cta", mode="before")
    @classmethod
    def normalize_scalar_text(cls, value: Any, info: ValidationInfo) -> Any:
        """模型偶发把短文本字段输出成字符串数组；只接受纯文本数组并拼成展示字符串。"""

        return _normalize_ai_scalar_text(value, field_name=info.field_name)


class PosterRenderResult(BaseModel):
    """海报渲染结果元信息。"""

    kind: PosterKind
    template_name: str
    storage_path: str
    width: int
    height: int
    mime_type: str = "image/png"


class ReferenceImageInput(BaseModel):
    """海报渲染/生成需要的参考图信息。"""

    path: Path
    mime_type: str
    filename: str
    role: str | None = None
    label: str | None = None


class PosterGenerationInput(BaseModel):
    """海报/改图生成入参：聚合商品、可选文案与图片上下文。"""

    copy_prompt_mode: Literal["copy", "image_edit"] = "copy"
    product_name: str
    category: str | None = None
    price: str | None = None
    source_note: str | None = None
    instruction: str | None = None
    image_size: str | None = None
    tool_options: dict[str, Any] | None = None
    title: str = ""
    selling_points: list[str] = Field(default_factory=list)
    poster_headline: str = ""
    cta: str = ""
    source_image: Path | None = None
    reference_images: list[ReferenceImageInput] = Field(default_factory=list)
