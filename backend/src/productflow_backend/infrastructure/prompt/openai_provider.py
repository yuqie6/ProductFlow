from __future__ import annotations

import base64
import json
from typing import Any

from openai import OpenAI
from pydantic import ValidationError

from productflow_backend.domain.artifact_contracts import ImagePromptPayloadV1, ListingPromptPayload
from productflow_backend.domain.image_type_catalog import LISTING_LOOK_CONTEXT, LISTING_LOOK_RULE
from productflow_backend.infrastructure.prompt.base import (
    ContextGenerationRequest,
    CreativeBriefGenerationResult,
    PromptGenerationProvider,
    PromptGenerationRequest,
    PromptGenerationResult,
    VisualOverlayGenerationResult,
)
from productflow_backend.infrastructure.prompt.context_payloads import GeneratedCreativeBrief, GeneratedVisualOverlay
from productflow_backend.infrastructure.provider_config import (
    ResolvedPromptProviderConfig,
    resolve_prompt_provider_config,
)
from productflow_backend.infrastructure.provider_effects import ProviderEffectQueryResult

PROMPT_GENERATION_INSTRUCTIONS = (
    "You write one ListingPromptPayload for a clickable commercial listing image. "
    "Attached photos lock product identity only: shape, materials, color, structure, visible parts. "
    "The photo's crop, empty background, camera distance, and layout are not the finished frame. "
    "Follow listing_look: product is the hero, hierarchy is clear, benefits are readable. "
    "Forbidden both extremes: empty gray still-life / huge whitespace; and sticker-bomb / neon carnival layouts. "
    "Treat leftover brief text such as 极简, 浅灰, 静物, or 干净 as product-photo notes, not the listing look. "
    "Do not invert those notes into busy sticker spam. "
    "Use image_type_key, image_type_family, image_type_job, image_type_title, "
    "and image_type_description as the job. "
    "photography: commercial product photography; product occupies about 55-75% of the frame; real lighting; "
    "category-appropriate background; never shrink the product into a corner of empty canvas; "
    "never cover it with badges. "
    "infographic: cut the product out and redesign the layout. One clear headline plus 2-4 short aligned benefits. "
    "Restrained color blocks, readable type. The product stays the visual hero. "
    "Never paste a caption onto the original photo. "
    "evidence: only user-supplied certificates or factory photos; if missing, leave the gap; "
    "never invent seals or plants. "
    "Do not invent logos, certifications, prices, spec numbers, or structures absent from facts and photos. "
    "When generate_from_context is true, current_prompt is a schema seed. Observe the photos and write composition, "
    "background, lighting, focus, and selling points for that image type. "
    "Do not keep placeholder phrases such as 干净背景, 正面, 均匀照明, or 根据参考图、商品资料与图片类型生成. "
    "When generate_from_context is false, refine current_prompt and keep user-authored fields. "
    "Obey text_policy. none means no on-image letters, digits, prices, logos, or watermarks; keep text.headline, "
    "subtitle, and body null and copy_regions empty. "
    "required means short benefit copy in text_language, not a spec sheet. "
    "Do not emit images, image_plan_key, fact_keys, or evidence_asset_ids."
)

CREATIVE_BRIEF_INSTRUCTIONS = (
    "You write one ecommerce listing brief from the attached product photos, confirmed facts, and image_types. "
    "Follow listing_look: a shopper would click, the product is the hero, hierarchy is clear. "
    "goal is the listing job, not a restatement of studio notes. "
    "current_brief.goal and source notes are product facts (what it is, who it is for). "
    "Ignore leftover art-direction words such as 极简, 浅灰, 静物, 干净, 留白. "
    "Do not invert them into sticker-bomb or oversaturated layouts either. "
    "design_goals are concrete layout and photography aims for the planned image_types. "
    "If image_types include infographic keys such as selling_point, require cutout, recomposed layout, "
    "and 2-4 short aligned benefits with restrained color. If they include photography keys such as hero or scene, "
    "require a large product and commercial lighting, not empty-canvas still life and not badge spam. "
    "prohibitions must include both extremes: 极简大留白/浅灰空棚/杂志静物, and 爆炸贴/满屏色块/牛皮癣标签. "
    "Also block invented logos, certificates, prices, and product structures. "
    "Do not prohibit new composition, lighting, scene, or type layout. "
    "Obey text_policy: none means required_copy must be empty; allow or required may include "
    "short on-image benefit copy in text_language. "
    "Do not invent facts that are not in the photos or confirmed facts."
)

VISUAL_OVERLAY_INSTRUCTIONS = (
    "You write a compact visual overlay for a commercial listing set. "
    "style is 2 to 6 keywords for a balanced sellable look "
    "(product hero, clear hierarchy, category-appropriate color), "
    "not 极简静物, not 干净商业摄影, not 花里胡哨. "
    "Do not copy the reference photo's empty background or crop as the brand system; "
    "only lock product material colors. "
    "colors must include a background with presence (warm off-white or a category color, never empty zinc-gray studio) "
    "plus one muted accent for headlines or modules. Hex values like #F3EFE8. Never neon carnival palettes. "
    "prohibitions block product-identity changes and both listing extremes "
    "(empty gray still-life, sticker-bomb layouts). Do not block layout changes. "
    "Do not invent a brand system that is not visible in the photos or facts."
)


class OpenAIPromptGenerationProvider(PromptGenerationProvider):
    provider_name = "openai"

    def __init__(self, provider_config: ResolvedPromptProviderConfig | None = None) -> None:
        resolved_config = provider_config or resolve_prompt_provider_config()
        client_kwargs: dict[str, Any] = {"api_key": resolved_config.api_key}
        if resolved_config.base_url:
            client_kwargs["base_url"] = resolved_config.base_url
        self.client = OpenAI(**client_kwargs)
        self.model = resolved_config.model

    def generate_prompt(self, request: PromptGenerationRequest) -> PromptGenerationResult:
        parsed, response_id = self._parse_structured(
            text_format=ListingPromptPayload,
            instructions=PROMPT_GENERATION_INSTRUCTIONS,
            request=request,
            expected_type=ListingPromptPayload,
        )
        return PromptGenerationResult(
            payload=ImagePromptPayloadV1.model_validate(parsed.model_dump(mode="json")),
            model=self.model,
            response_id=response_id,
        )

    def generate_creative_brief(self, request: ContextGenerationRequest) -> CreativeBriefGenerationResult:
        parsed, response_id = self._parse_structured(
            text_format=GeneratedCreativeBrief,
            instructions=CREATIVE_BRIEF_INSTRUCTIONS,
            request=request,
            expected_type=GeneratedCreativeBrief,
        )
        if request.text_policy == "none":
            parsed = parsed.model_copy(update={"required_copy": []})
        return CreativeBriefGenerationResult(payload=parsed, model=self.model, response_id=response_id)

    def generate_visual_overlay(self, request: ContextGenerationRequest) -> VisualOverlayGenerationResult:
        parsed, response_id = self._parse_structured(
            text_format=GeneratedVisualOverlay,
            instructions=VISUAL_OVERLAY_INSTRUCTIONS,
            request=request,
            expected_type=GeneratedVisualOverlay,
        )
        return VisualOverlayGenerationResult(payload=parsed, model=self.model, response_id=response_id)

    def _parse_structured(
        self,
        *,
        text_format: type[Any],
        instructions: str,
        request: PromptGenerationRequest | ContextGenerationRequest,
        expected_type: type[Any],
    ) -> tuple[Any, str | None]:
        parse = getattr(self.client.responses, "parse", None)
        if not callable(parse):
            raise RuntimeError("提示词 provider 不支持 Responses structured outputs，请更换或升级 provider")
        try:
            response = parse(
                model=self.model,
                text_format=text_format,
                instructions=instructions,
                input=[{"role": "user", "content": _request_content(request)}],
            )
        except ValidationError:
            raise
        except Exception as exc:
            raise RuntimeError("提示词 provider 结构化输出请求失败") from exc
        parsed = getattr(response, "output_parsed", None)
        if isinstance(parsed, dict):
            parsed = expected_type.model_validate(parsed)
        if not isinstance(parsed, expected_type):
            raise RuntimeError("提示词 provider 未返回结构化输出")
        response_id = getattr(response, "id", None)
        return parsed, response_id if isinstance(response_id, str) else None

    def reconcile_prompt_effect(
        self,
        *,
        operation_key: str,
        request_hash: str,
        provider_response_id: str | None,
    ) -> ProviderEffectQueryResult:
        del operation_key, request_hash
        if not provider_response_id:
            return ProviderEffectQueryResult.unsupported("提示词 provider 没有可查询的 response id")
        retrieve = getattr(self.client.responses, "retrieve", None)
        if not callable(retrieve):
            return ProviderEffectQueryResult.unsupported("当前提示词 provider client 不支持查询 response")
        try:
            response = retrieve(provider_response_id)
        except Exception as exc:  # noqa: BLE001
            return ProviderEffectQueryResult(
                effect_result="unknown",
                reconciliation_state="unknown",
                provider_response_id=provider_response_id,
                detail=f"查询提示词 provider response 失败: {type(exc).__name__}",
            )

        status = str(getattr(response, "status", "") or "").lower() or None
        response_id = str(getattr(response, "id", "") or "") or provider_response_id
        result_json = {
            "provider_response_id": response_id,
            "provider_status": status,
            "has_output": bool(getattr(response, "output", None)),
            "has_output_parsed": getattr(response, "output_parsed", None) is not None,
        }
        if status in {"failed", "cancelled", "canceled", "incomplete", "expired"}:
            return ProviderEffectQueryResult(
                effect_result="failed",
                reconciliation_state="not_applied",
                provider_response_id=response_id,
                provider_status=status,
                result_json=result_json,
                detail="供应商记录显示提示词请求未完成",
            )
        if status in {"queued", "in_progress"}:
            return ProviderEffectQueryResult(
                effect_result="unknown",
                reconciliation_state="unknown",
                provider_response_id=response_id,
                provider_status=status,
                result_json=result_json,
                detail="提示词 provider 请求仍在处理中",
            )
        if getattr(response, "output_parsed", None) is not None or getattr(response, "output", None):
            return ProviderEffectQueryResult(
                effect_result="applied",
                reconciliation_state="applied",
                provider_response_id=response_id,
                provider_status=status,
                result_json=result_json,
            )
        return ProviderEffectQueryResult(
            effect_result="unknown",
            reconciliation_state="unknown",
            provider_response_id=response_id,
            provider_status=status,
            result_json=result_json,
            detail="供应商 response 没有足够的输出证据",
        )


def _request_content(request: PromptGenerationRequest | ContextGenerationRequest) -> list[dict[str, Any]]:
    reference_metadata = [
        {
            "asset_id": reference.asset_id,
            "role": reference.role,
            "label": reference.label,
            "filename": reference.filename,
            "mime_type": reference.mime_type,
        }
        for reference in request.reference_images
    ]
    if isinstance(request, ContextGenerationRequest):
        context = {
            "task": "generate_ecommerce_context_node",
            "node_title": request.node_title,
            "confirmed_facts": list(request.facts),
            "current_brief": request.current_brief,
            "current_overlay": request.current_overlay,
            "text_policy": request.text_policy,
            "text_language": request.text_language,
            "reference_images": reference_metadata,
            "image_types": list(request.image_types),
            "listing_look": LISTING_LOOK_CONTEXT,
            "listing_look_rule": LISTING_LOOK_RULE,
        }
    else:
        context = {
            "task": "generate_ecommerce_image_prompt_artifact",
            "generate_from_context": request.generate_from_context,
            "image_type_key": request.image_type_key,
            "image_type_title": request.image_type_title,
            "image_type_description": request.image_type_description,
            "image_type_family": request.image_type_family,
            "image_type_job": request.image_type_job,
            "confirmed_facts": list(request.facts),
            "visual_system": (
                request.visual_system.model_dump(mode="json") if request.visual_system is not None else None
            ),
            "visual_exceptions": list(request.visual_exceptions),
            "current_prompt": request.current_prompt.model_dump(
                mode="json",
                exclude={"images", "fact_keys", "evidence_asset_ids"},
            ),
            "text_policy": request.text_policy,
            "text_languages": list(request.text_languages),
            "reference_images": reference_metadata,
            "listing_look": LISTING_LOOK_CONTEXT,
            "listing_look_rule": LISTING_LOOK_RULE,
        }
    content: list[dict[str, Any]] = [
        {
            "type": "input_text",
            "text": json.dumps(context, ensure_ascii=False, sort_keys=True, separators=(",", ":")),
        }
    ]
    for reference in request.reference_images:
        content.append(
            {
                "type": "input_text",
                "text": json.dumps(
                    {
                        "asset_id": reference.asset_id,
                        "role": reference.role,
                        "label": reference.label,
                    },
                    ensure_ascii=False,
                    sort_keys=True,
                    separators=(",", ":"),
                ),
            }
        )
        encoded = base64.b64encode(reference.image_bytes).decode("ascii")
        content.append(
            {
                "type": "input_image",
                "image_url": f"data:{reference.mime_type};base64,{encoded}",
            }
        )
    return content


__all__ = ["OpenAIPromptGenerationProvider", "PROMPT_GENERATION_INSTRUCTIONS"]
