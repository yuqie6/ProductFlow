from __future__ import annotations

from typing import Literal

from openai import OpenAI
from pydantic import BaseModel, ValidationError

from productflow_backend.application.contracts import (
    CopyNodeConfigV2,
    CopyPayloadV2,
    CreativeBriefPayload,
    ProductInput,
    ReferenceImageInput,
)
from productflow_backend.config import get_runtime_settings
from productflow_backend.infrastructure.prompts import text_or_default
from productflow_backend.infrastructure.provider_config import (
    ResolvedTextProviderConfig,
    resolve_text_provider_config,
)
from productflow_backend.infrastructure.text.base import TextProvider


def _optional_text(value: str) -> str | None:
    return value.strip() or None


def _parsed_output[ParsedPayloadT: BaseModel](
    response: object,
    model_type: type[ParsedPayloadT],
    *,
    error_label: str,
) -> ParsedPayloadT:
    parsed = getattr(response, "output_parsed", None)
    if isinstance(parsed, model_type):
        return parsed
    if isinstance(parsed, dict):
        return model_type.model_validate(parsed)
    raise RuntimeError(f"{error_label} 未返回结构化输出，请确认文案 provider 支持 Responses structured outputs")


class _OpenAICopyBlockOutput(BaseModel):
    """Provider-facing copy block schema without nullable fields or unions."""

    id: str
    role: str
    label: str
    text: str
    note: str
    visual_hint: str
    priority: int

    def to_contract_dict(self) -> dict[str, object]:
        return {
            "id": self.id,
            "role": _optional_text(self.role),
            "label": _optional_text(self.label),
            "text": self.text,
            "note": _optional_text(self.note),
            "visual_hint": _optional_text(self.visual_hint),
            "priority": self.priority if self.priority > 0 else None,
        }


class _OpenAICopySectionOutput(BaseModel):
    """Provider-facing layout section schema without nullable fields or unions."""

    id: str
    title: str
    body: str
    items: list[_OpenAICopyBlockOutput]
    visual_hint: str

    def to_contract_dict(self) -> dict[str, object]:
        return {
            "id": self.id,
            "title": _optional_text(self.title),
            "body": _optional_text(self.body),
            "items": [item.to_contract_dict() for item in self.items],
            "visual_hint": _optional_text(self.visual_hint),
        }


class _OpenAIVisualGuidanceOutput(BaseModel):
    """Provider-facing visual guidance schema without nullable fields or unions."""

    main_message: str
    hierarchy: list[str]
    composition_hint: str
    text_density: Literal["", "none", "low", "medium", "high"]
    avoid: list[str]

    def to_contract_dict(self) -> dict[str, object] | None:
        main_message = _optional_text(self.main_message)
        composition_hint = _optional_text(self.composition_hint)
        text_density = self.text_density or None
        hierarchy = [item.strip() for item in self.hierarchy if item.strip()]
        avoid = [item.strip() for item in self.avoid if item.strip()]
        if not (main_message or composition_hint or text_density or hierarchy or avoid):
            return None
        return {
            "main_message": main_message,
            "hierarchy": hierarchy,
            "composition_hint": composition_hint,
            "text_density": text_density,
            "avoid": avoid,
        }


class OpenAICopyPayloadStructuredOutput(BaseModel):
    """OpenAI-compatible structured-output schema for copy payloads.

    `CopyPayloadV2.content` is a discriminated union. Some OpenAI-compatible providers reject union combinators in
    `text.format.schema`, so this provider-facing schema keeps the same semantic slots in a flat object and is converted
    to `CopyPayloadV2` immediately after parsing.
    """

    version: Literal[2]
    purpose: str
    summary: str
    content_kind: Literal["freeform", "blocks", "layout_brief"]
    freeform_text: str
    blocks: list[_OpenAICopyBlockOutput]
    sections: list[_OpenAICopySectionOutput]
    visual_guidance: _OpenAIVisualGuidanceOutput

    def to_copy_payload(self, *, fallback_purpose: str | None = None) -> CopyPayloadV2:
        if self.content_kind == "freeform":
            content: dict[str, object] = {"kind": "freeform", "text": self.freeform_text}
        elif self.content_kind == "blocks":
            content = {"kind": "blocks", "blocks": [block.to_contract_dict() for block in self.blocks]}
        else:
            content = {"kind": "layout_brief", "sections": [section.to_contract_dict() for section in self.sections]}
        return CopyPayloadV2.model_validate(
            {
                "version": 2,
                "purpose": _optional_text(self.purpose) or fallback_purpose,
                "summary": self.summary,
                "content": content,
                "visual_guidance": self.visual_guidance.to_contract_dict(),
            }
        )


class OpenAITextProvider(TextProvider):
    provider_name = "openai"
    prompt_version = "responses-structured-v1"

    def __init__(self, provider_config: ResolvedTextProviderConfig | None = None) -> None:
        settings = get_runtime_settings()
        resolved_config = provider_config or resolve_text_provider_config()
        client_kwargs = {"api_key": resolved_config.api_key}
        if resolved_config.base_url:
            client_kwargs["base_url"] = resolved_config.base_url
        self.client = OpenAI(**client_kwargs)
        self.brief_model = resolved_config.brief_model
        self.copy_model = resolved_config.copy_model
        self.brief_system_prompt = settings.prompt_brief_system
        self.copy_system_prompt = settings.prompt_copy_system

    def _parse_structured_response[ParsedPayloadT: BaseModel](
        self,
        *,
        model: str,
        text_format: type[ParsedPayloadT],
        instructions: str,
        input_payload: list[dict[str, str]],
        error_label: str,
    ) -> ParsedPayloadT:
        parse = getattr(self.client.responses, "parse", None)
        if not callable(parse):
            raise RuntimeError(f"{error_label} 不支持 Responses structured outputs，请更换或升级文案 provider")
        try:
            response = parse(
                model=model,
                text_format=text_format,
                instructions=instructions,
                input=input_payload,
            )
        except ValidationError:
            raise
        except Exception as exc:
            raise RuntimeError(
                f"{error_label} 结构化输出请求失败，请确认模型和供应商支持 Responses structured outputs"
            ) from exc
        return _parsed_output(response, text_format, error_label=error_label)

    def generate_brief(self, product: ProductInput) -> tuple[CreativeBriefPayload, str]:
        payload = self._parse_structured_response(
            model=self.brief_model,
            text_format=CreativeBriefPayload,
            instructions=text_or_default(
                self.brief_system_prompt,
                "你是电商商品理解助手。根据商品资料提炼定位、受众、卖点、禁忌表达和视觉风格建议。",
            ),
            input_payload=[
                {
                    "role": "user",
                    "content": (
                        f"商品名：{product.name}\n"
                        f"类目：{product.category or '未提供'}\n"
                        f"价格：{product.price or '未提供'}\n"
                        f"商品描述/补充说明：{product.source_note or '未提供'}\n"
                        "请基于真实商品信息提炼可用于后续文案和图片生成的商品理解。"
                    ),
                },
            ],
            error_label="商品理解 provider",
        )
        return payload, self.brief_model

    def generate_copy(
        self,
        product: ProductInput,
        brief: CreativeBriefPayload,
        config: CopyNodeConfigV2 | None = None,
        reference_images: list[ReferenceImageInput] | None = None,
    ) -> tuple[CopyPayloadV2, str]:
        config = config or CopyNodeConfigV2()
        reference_images = reference_images or []
        reference_lines = [
            (
                f"{index}. {reference.label or reference.filename}"
                f"（角色：{reference.role or '参考图'}，类型：{reference.mime_type}，文件：{reference.filename}）"
            )
            for index, reference in enumerate(reference_images, start=1)
        ]
        reference_text = "\n".join(reference_lines) if reference_lines else "未连接"
        structured_payload = self._parse_structured_response(
            model=self.copy_model,
            text_format=OpenAICopyPayloadStructuredOutput,
            instructions=text_or_default(
                self.copy_system_prompt,
                "你是电商文案助手。根据商品资料、商品理解、节点配置和参考图上下文生成可编辑文案。",
            ),
            input_payload=[
                {
                    "role": "user",
                    "content": (
                        f"商品名：{product.name}\n"
                        f"类目：{product.category or '未提供'}\n"
                        f"价格：{product.price or '未提供'}\n"
                        f"商品描述/补充说明：{product.source_note or '未提供'}\n"
                        f"参考图：{reference_text}\n"
                        f"文案用途：{config.purpose or '未指定'}\n"
                        f"输出模式：{config.output_mode}\n"
                        f"渠道：{config.channel or '未指定'}\n"
                        f"语气：{config.tone or '未指定'}\n"
                        f"本轮文案要求：{config.instruction or '按商品和场景自由组织文案'}\n"
                        f"可选槽位：{[slot.model_dump(mode='json') for slot in config.requested_slots]}\n"
                        f"商品定位：{brief.positioning}\n"
                        f"目标人群：{brief.audience}\n"
                        f"卖点角度：{', '.join(brief.selling_angles)}\n"
                        f"禁忌表达：{', '.join(brief.taboo_phrases) or '无'}\n"
                        "不要为了满足固定字段编造 CTA、海报标题或固定数量卖点。"
                        "如果场景适合自由正文、短标签块或布局说明，请按内容自然选择。"
                    ),
                },
            ],
            error_label="文案 provider",
        )
        payload = structured_payload.to_copy_payload(fallback_purpose=config.purpose)
        return payload, self.copy_model
