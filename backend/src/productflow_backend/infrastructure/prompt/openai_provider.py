from __future__ import annotations

import base64
import json
from typing import Any

from openai import OpenAI
from pydantic import ValidationError

from productflow_backend.application.workflow_drafts.contracts import ImagePromptPayloadV1
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
    "You write one high-quality ImagePromptPayloadV1 for a single ecommerce listing image. "
    "The attached product photos and confirmed facts are the source of product identity: shape, materials, color, "
    "structure, and visible features. Use image_type_key, image_type_title, and image_type_description to decide "
    "the shot (hero, scene, detail, and so on). If visual_system or visual_exceptions are present, obey them. "
    "When generate_from_context is true, current_prompt is a schema seed, not a confirmed creative plan. "
    "Observe the photos and write composition, background, lighting, focus, and selling points for that image type. "
    "Do not keep placeholder phrases such as 干净背景, 正面, 均匀照明, or 根据参考图、商品资料与图片类型生成 "
    "as the final look. When generate_from_context is false, refine current_prompt and keep user-authored fields. "
    "Obey text_policy. none means no on-image letters, digits, prices, logos, or watermarks; keep text.headline, "
    "subtitle, and body null and copy_regions empty. allow means on-image copy is optional. required means write "
    "text fields in text_language. Product facts such as price may describe the product; they are not on-image copy "
    "unless text_policy is allow or required. Keep exactly the supplied image_plan_keys. Do not invent logos, "
    "certifications, prices, product features, or source images that are not in the facts or visible in the photos."
)

CREATIVE_BRIEF_INSTRUCTIONS = (
    "You write one ecommerce creative brief from the attached product photos and confirmed facts. "
    "goal is the shoot purpose. design_goals are concrete visual aims. prohibitions block invented features. "
    "Obey text_policy: none means required_copy must be empty; allow or required may include short on-image copy "
    "only when the photos or facts actually need it. Do not invent logos, certifications, prices as on-image copy, "
    "or product features that are not in the facts or visible in the photos."
)

VISUAL_OVERLAY_INSTRUCTIONS = (
    "You write a compact visual overlay for ecommerce product photography. "
    "style is 2 to 6 keywords describing look. colors must include a background swatch sampled from the photos "
    "or a clean studio fallback, with hex values like #F4F4F5. prohibitions block changes to product identity. "
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
            text_format=ImagePromptPayloadV1,
            instructions=PROMPT_GENERATION_INSTRUCTIONS,
            request=request,
            expected_type=ImagePromptPayloadV1,
        )
        return PromptGenerationResult(payload=parsed, model=self.model, response_id=response_id)

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
        }
    else:
        context = {
            "task": "generate_ecommerce_image_prompt_artifact",
            "generate_from_context": request.generate_from_context,
            "image_type_key": request.image_type_key,
            "image_type_title": request.image_type_title,
            "image_type_description": request.image_type_description,
            "image_plan_keys": list(request.image_plan_keys),
            "confirmed_facts": list(request.facts),
            "visual_system": (
                request.visual_system.model_dump(mode="json") if request.visual_system is not None else None
            ),
            "visual_exceptions": list(request.visual_exceptions),
            "current_prompt": request.current_prompt.model_dump(mode="json"),
            "text_policy": request.text_policy,
            "text_languages": list(request.text_languages),
            "reference_images": reference_metadata,
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
