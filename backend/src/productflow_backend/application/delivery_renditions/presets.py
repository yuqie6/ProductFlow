"""交付目录对外暴露的确定性 DeliverySpec 预设。"""

from __future__ import annotations

from dataclasses import dataclass
from math import gcd

from productflow_backend.application.delivery_renditions.contracts import normalize_delivery_spec
from productflow_backend.domain.errors import NotFoundError
from productflow_backend.domain.image_specs import DeliverySpec

DELIVERY_PRESET_REVIEWED_AT = "2026-08-24"
DELIVERY_PRESET_SOURCE = "docs/specs/productflow-studio-requirements.md §18"
DELIVERY_PRESET_DISCLAIMER = (
    "内置模板仅提供便捷默认值，不构成平台审核或合规保证；平台规则可能变化，请在使用前自行确认。"
)


def _reduced_aspect_ratio(width: int, height: int) -> str:
    divisor = gcd(width, height)
    return f"{width // divisor}:{height // divisor}"


@dataclass(frozen=True, slots=True)
class DeliveryPreset:
    """一份不可变、与 provider 无关的 DeliverySpec 模板。"""

    key: str
    title: str
    aspect_ratio: str
    applicable_image_type: str
    reviewed_at: str
    source: str
    disclaimer: str
    spec: DeliverySpec

    def __post_init__(self) -> None:
        expected_ratio = _reduced_aspect_ratio(self.spec.width, self.spec.height)
        if self.aspect_ratio != expected_ratio:
            raise ValueError(f"DeliverySpec 预设 {self.key} 的画幅 {self.aspect_ratio} 与尺寸 {expected_ratio} 不一致")


def _make_preset(
    *,
    key: str,
    title: str,
    aspect_ratio: str,
    applicable_image_type: str,
    width: int,
    height: int,
) -> DeliveryPreset:
    # normalize_delivery_spec 与 rendition job 共用同一校验边界。
    normalized = normalize_delivery_spec(
        DeliverySpec(
            width=width,
            height=height,
            format="png",
            max_byte_size=None,
            fit="contain",
            background_color=None,
            crop_anchor=None,
        )
    )
    return DeliveryPreset(
        key=key,
        title=title,
        aspect_ratio=aspect_ratio,
        applicable_image_type=applicable_image_type,
        reviewed_at=DELIVERY_PRESET_REVIEWED_AT,
        source=DELIVERY_PRESET_SOURCE,
        disclaimer=DELIVERY_PRESET_DISCLAIMER,
        spec=normalized.spec,
    )


# 元组顺序属于只读 API 合同。
DELIVERY_PRESETS: tuple[DeliveryPreset, ...] = (
    _make_preset(
        key="taobao_tmall_hero",
        title="淘宝/天猫首屏",
        aspect_ratio="3:4",
        applicable_image_type="hero",
        width=1200,
        height=1600,
    ),
    _make_preset(
        key="jd_hero",
        title="京东主图",
        aspect_ratio="1:1",
        applicable_image_type="hero",
        width=1200,
        height=1200,
    ),
    _make_preset(
        key="amazon_hero",
        title="Amazon 主图",
        aspect_ratio="1:1",
        applicable_image_type="hero",
        width=1200,
        height=1200,
    ),
    _make_preset(
        key="detail_portrait",
        title="详情竖图",
        aspect_ratio="3:4",
        applicable_image_type="detail",
        width=1200,
        height=1600,
    ),
    _make_preset(
        key="scene_landscape",
        title="场景横图",
        aspect_ratio="4:3",
        applicable_image_type="scene",
        width=1600,
        height=1200,
    ),
)


def list_delivery_presets() -> tuple[DeliveryPreset, ...]:
    """按稳定的 wire 顺序返回目录。"""

    return DELIVERY_PRESETS


def get_delivery_preset(key: str) -> DeliveryPreset:
    for preset in DELIVERY_PRESETS:
        if preset.key == key:
            return preset
    raise NotFoundError(f"未知 DeliverySpec 预设: {key}")


__all__ = [
    "DELIVERY_PRESET_DISCLAIMER",
    "DELIVERY_PRESET_REVIEWED_AT",
    "DELIVERY_PRESET_SOURCE",
    "DELIVERY_PRESETS",
    "DeliveryPreset",
    "get_delivery_preset",
    "list_delivery_presets",
]
