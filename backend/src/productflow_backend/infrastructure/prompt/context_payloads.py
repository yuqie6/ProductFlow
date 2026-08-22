from __future__ import annotations

from typing import Annotated

from pydantic import BaseModel, ConfigDict, Field, StringConstraints

NonEmptyText = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=4000)]


class GeneratedCreativeBrief(BaseModel):
    model_config = ConfigDict(extra="forbid")

    goal: NonEmptyText
    design_goals: list[NonEmptyText] = Field(default_factory=list)
    required_copy: list[NonEmptyText] = Field(default_factory=list)
    prohibitions: list[NonEmptyText] = Field(default_factory=list)


class GeneratedVisualColor(BaseModel):
    model_config = ConfigDict(extra="forbid")

    role: Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=80)]
    value: Annotated[
        str,
        StringConstraints(strip_whitespace=True, min_length=7, max_length=7, pattern=r"^#[0-9A-Fa-f]{6}$"),
    ]
    label: Annotated[str, StringConstraints(strip_whitespace=True, max_length=255)] = ""


class GeneratedVisualOverlay(BaseModel):
    model_config = ConfigDict(extra="forbid")

    style: list[NonEmptyText] = Field(min_length=1)
    colors: list[GeneratedVisualColor] = Field(min_length=1)
    prohibitions: list[NonEmptyText] = Field(default_factory=list)


DEFAULT_CREATIVE_BRIEF = GeneratedCreativeBrief(
    goal="根据参考图和商品资料制定电商拍摄要求",
    design_goals=["还原商品真实形态", "突出可辨认的卖点"],
    required_copy=[],
    prohibitions=["不得编造参考图或商品资料中未出现的特征"],
)

DEFAULT_VISUAL_OVERLAY = GeneratedVisualOverlay(
    style=["干净商业摄影", "还原商品材质"],
    colors=[GeneratedVisualColor(role="background", value="#F4F4F5", label="浅灰背景")],
    prohibitions=["不要改变商品结构、颜色或材质"],
)
