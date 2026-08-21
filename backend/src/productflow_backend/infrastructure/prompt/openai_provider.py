from __future__ import annotations

import base64
import json
from typing import Any

from openai import OpenAI
from pydantic import ValidationError

from productflow_backend.application.workflow_drafts.contracts import ImagePromptPayloadV1
from productflow_backend.infrastructure.prompt.base import (
    PromptGenerationProvider,
    PromptGenerationRequest,
    PromptGenerationResult,
)
from productflow_backend.infrastructure.provider_config import (
    ResolvedPromptProviderConfig,
    resolve_prompt_provider_config,
)
from productflow_backend.infrastructure.provider_effects import ProviderEffectQueryResult

PROMPT_GENERATION_INSTRUCTIONS = (
    "You create a structured ecommerce image prompt plan. Use only the supplied confirmed facts and image evidence. "
    "Preserve product identity, obey the selected visual system and confirmed exceptions, keep exactly the supplied "
    "image_plan_keys, and return an ImagePromptPayloadV1 object. Do not invent logos, certifications, prices, product "
    "features, or source images."
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
        parse = getattr(self.client.responses, "parse", None)
        if not callable(parse):
            raise RuntimeError("提示词 provider 不支持 Responses structured outputs，请更换或升级 provider")
        try:
            response = parse(
                model=self.model,
                text_format=ImagePromptPayloadV1,
                instructions=PROMPT_GENERATION_INSTRUCTIONS,
                input=[{"role": "user", "content": _request_content(request)}],
            )
        except ValidationError:
            raise
        except Exception as exc:
            raise RuntimeError("提示词 provider 结构化输出请求失败") from exc
        parsed = getattr(response, "output_parsed", None)
        if isinstance(parsed, dict):
            parsed = ImagePromptPayloadV1.model_validate(parsed)
        if not isinstance(parsed, ImagePromptPayloadV1):
            raise RuntimeError("提示词 provider 未返回结构化 ImagePromptPayloadV1")
        response_id = getattr(response, "id", None)
        return PromptGenerationResult(
            payload=parsed,
            model=self.model,
            response_id=response_id if isinstance(response_id, str) else None,
        )

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


def _request_content(request: PromptGenerationRequest) -> list[dict[str, Any]]:
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
    context = {
        "task": "generate_ecommerce_image_prompt_artifact",
        "image_type_key": request.image_type_key,
        "image_plan_keys": list(request.image_plan_keys),
        "confirmed_facts": list(request.facts),
        "visual_system": request.visual_system.model_dump(mode="json") if request.visual_system is not None else None,
        "visual_exceptions": list(request.visual_exceptions),
        "current_prompt": request.current_prompt.model_dump(mode="json"),
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
