from __future__ import annotations

import json

from openai import OpenAI

from productflow_backend.application.contracts import (
    CopyPayload,
    CreativeBriefPayload,
    ProductInput,
    ReferenceImageInput,
)
from productflow_backend.config import get_runtime_settings
from productflow_backend.infrastructure.text.base import TextProvider


class OpenAITextProvider(TextProvider):
    provider_name = "openai"
    prompt_version = "responses-json-v1"

    def __init__(self) -> None:
        settings = get_runtime_settings()
        client_kwargs = {"api_key": settings.text_api_key}
        if settings.text_base_url:
            client_kwargs["base_url"] = settings.text_base_url
        self.client = OpenAI(**client_kwargs)
        self.brief_model = settings.text_brief_model
        self.copy_model = settings.text_copy_model

    def _read_output_json(self, response) -> dict:
        text = getattr(response, "output_text", "").strip()
        if text.startswith("```"):
            lines = text.splitlines()
            if lines and lines[0].startswith("```"):
                lines = lines[1:]
            if lines and lines[-1].startswith("```"):
                lines = lines[:-1]
            text = "\n".join(lines).strip()
        return json.loads(text)

    def generate_brief(self, product: ProductInput) -> tuple[CreativeBriefPayload, str]:
        response = self.client.responses.create(
            model=self.brief_model,
            input=[
                {
                    "role": "system",
                    "content": (
                        "你是电商商品理解助手。请根据商品名称、类目、价格和用途，"
                        "输出简洁、结构化的中文 JSON。不要输出 markdown。"
                    ),
                },
                {
                    "role": "user",
                    "content": (
                        f"商品名：{product.name}\n"
                        f"类目：{product.category or '未提供'}\n"
                        f"价格：{product.price or '未提供'}\n"
                        f"商品描述/补充说明：{product.source_note or '未提供'}\n"
                        "请输出字段：positioning、audience、selling_angles(3到5条)、"
                        "taboo_phrases、poster_style_hint。"
                    ),
                },
            ],
        )
        payload = CreativeBriefPayload.model_validate(self._read_output_json(response))
        return payload, self.brief_model

    def generate_copy(
        self,
        product: ProductInput,
        brief: CreativeBriefPayload,
        instruction: str | None = None,
        reference_images: list[ReferenceImageInput] | None = None,
    ) -> tuple[CopyPayload, str]:
        reference_images = reference_images or []
        reference_lines = [
            (
                f"{index}. {reference.label or reference.filename}"
                f"（角色：{reference.role or '参考图'}，类型：{reference.mime_type}，文件：{reference.filename}）"
            )
            for index, reference in enumerate(reference_images, start=1)
        ]
        reference_text = "\n".join(reference_lines) if reference_lines else "未连接"
        response = self.client.responses.create(
            model=self.copy_model,
            input=[
                {
                    "role": "system",
                    "content": (
                        "你是淘宝电商文案助手。请输出中文 JSON，不要输出 markdown，"
                        "语言要口语、直接、可用于主图和促销海报。"
                    ),
                },
                {
                    "role": "user",
                    "content": (
                        f"商品名：{product.name}\n"
                        f"类目：{product.category or '未提供'}\n"
                        f"价格：{product.price or '未提供'}\n"
                        f"商品描述/补充说明：{product.source_note or '未提供'}\n"
                        f"参考图：{reference_text}\n"
                        f"本轮文案要求：{instruction or '按默认转化型方向生成'}\n"
                        f"商品定位：{brief.positioning}\n"
                        f"目标人群：{brief.audience}\n"
                        f"卖点角度：{', '.join(brief.selling_angles)}\n"
                        f"禁忌表达：{', '.join(brief.taboo_phrases) or '无'}\n"
                        "请输出字段：title、selling_points(3到5条)、poster_headline、cta。"
                    ),
                },
            ],
        )
        payload = CopyPayload.model_validate(self._read_output_json(response))
        return payload, self.copy_model
