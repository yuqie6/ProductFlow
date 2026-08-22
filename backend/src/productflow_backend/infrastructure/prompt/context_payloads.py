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
    goal="做成能点击的商业套图：商品是主角，层次清楚，卖点好读。不要极简空棚，也不要爆炸贴墙。",
    design_goals=["商品一眼能认并占主体", "构图、布光和排版按图种设计", "卖点写成用户利益，条目克制好读"],
    required_copy=[],
    prohibitions=[
        "不要编造参考图和资料里没有的认证、Logo、价格或结构",
        "不要复制参考图构图后只加一行字",
        "不要极简大留白、浅灰空棚、杂志静物",
        "不要爆炸贴、满屏色块、牛皮癣标签",
    ],
)

DEFAULT_VISUAL_OVERLAY = GeneratedVisualOverlay(
    style=["商业套图", "商品是主角", "层次清楚"],
    colors=[
        GeneratedVisualColor(role="background", value="#F3EFE8", label="暖白底"),
        GeneratedVisualColor(role="headline", value="#1C1917", label="标题色"),
        GeneratedVisualColor(role="accent", value="#6B7C6A", label="克制点缀"),
    ],
    prohibitions=[
        "不要改变商品结构、颜色或材质",
        "不要把参考图构图当完成稿",
        "不要浅灰空棚或极简大留白",
        "不要爆炸贴或满屏色块",
    ],
)
