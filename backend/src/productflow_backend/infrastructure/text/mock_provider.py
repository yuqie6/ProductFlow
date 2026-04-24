from __future__ import annotations

from productflow_backend.application.contracts import (
    CopyPayload,
    CreativeBriefPayload,
    ProductInput,
    ReferenceImageInput,
)
from productflow_backend.infrastructure.text.base import TextProvider


class MockTextProvider(TextProvider):
    provider_name = "mock"

    def generate_brief(self, product: ProductInput) -> tuple[CreativeBriefPayload, str]:
        category = product.category or "通用电商"
        note_hint = f"，重点参考：{product.source_note[:48]}" if product.source_note else ""
        brief = CreativeBriefPayload(
            positioning=f"{category}场景下的实用型商品{note_hint}",
            audience="追求性价比、希望快速了解卖点的电商消费者",
            selling_angles=[
                "突出核心用途，先让人知道买来能解决什么问题",
                "强调到手直观收益，不堆空泛形容词",
                "语言更接近淘宝主图与促销海报风格",
            ],
            taboo_phrases=["全网最低", "包治百病", "绝对有效"],
            poster_style_hint="白底主图 + 强调主卖点的红色促销信息",
        )
        return brief, "mock-brief-v1"

    def generate_copy(
        self,
        product: ProductInput,
        brief: CreativeBriefPayload,
        instruction: str | None = None,
        reference_images: list[ReferenceImageInput] | None = None,
    ) -> tuple[CopyPayload, str]:
        category_prefix = f"{product.category} " if product.category else ""
        price_line = f" 参考价 {product.price}" if product.price else ""
        note_line = f"，结合描述：{product.source_note[:36]}" if product.source_note else ""
        instruction_line = f"，本轮方向：{instruction[:32]}" if instruction else ""
        reference_images = reference_images or []
        reference_hint = ""
        if reference_images:
            first_reference = reference_images[0]
            label = first_reference.label or first_reference.filename
            role = first_reference.role or "参考图"
            reference_hint = f"，参考{role}：{label}"
        copy = CopyPayload(
            title=f"{category_prefix}{product.name}｜实用好上手，店铺主推更省心",
            selling_points=[
                f"核心用途更清楚：{product.name}一眼看懂卖点{note_line}{reference_hint}",
                "展示更直接，适合主图和促销素材快速上架",
                f"语言偏转化型，适合淘宝电商场景{price_line}{instruction_line}".strip(),
            ],
            poster_headline=f"{product.name} 现在入手更划算",
            cta="下单前先收藏，对比后更容易做决定",
        )
        return copy, "mock-copy-v1"
