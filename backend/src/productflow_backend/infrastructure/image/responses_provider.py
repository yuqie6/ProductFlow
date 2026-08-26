from __future__ import annotations

import logging
from base64 import b64encode
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from time import sleep
from typing import Any
from urllib.parse import urlsplit, urlunsplit

from openai import OpenAI

from productflow_backend.config import (
    IMAGE_TOOL_FIELD_KEYS,
    filter_image_tool_options,
    parse_image_tool_allowed_fields,
)
from productflow_backend.domain.image_type_catalog import image_type_family
from productflow_backend.infrastructure.image.base import (
    ImageProvider,
    WorkflowGeneratedImage,
    WorkflowImageReference,
    WorkflowImageRequest,
    WorkflowImageResult,
    decode_b64_image,
    map_generation_spec_to_openai_size,
)
from productflow_backend.infrastructure.provider_config import (
    ResolvedImageProviderConfig,
    resolve_image_provider_config,
)
from productflow_backend.infrastructure.provider_effects import ProviderEffectQueryResult
from productflow_backend.infrastructure.runtime_settings import get_runtime_settings

IMAGE_TOOL_OPTIONAL_FIELD_KEYS = IMAGE_TOOL_FIELD_KEYS
RESPONSES_BACKGROUND_POLL_INTERVAL_SECONDS = 2.0
RESPONSES_IN_PROGRESS_STATUSES = {"queued", "in_progress"}
RESPONSES_TERMINAL_FAILURE_STATUSES = {"failed", "cancelled", "canceled", "incomplete", "expired"}
PROVIDER_REQUEST_FAILURE_MESSAGE = "图片供应商请求失败，请检查供应商配置后重试"
PROVIDER_BACKGROUND_INCOMPLETE_MESSAGE = "图片供应商后台生成未完成，请稍后重试"
PROVIDER_MISSING_OUTPUT_MESSAGE = "图片供应商没有返回图片结果，请稍后重试"
PROVIDER_TEXT_OUTPUT_MESSAGE = "图片供应商已完成请求，但返回的是文字回复，没有返回图片结果"

logger = logging.getLogger(__name__)


@dataclass(slots=True)
class ResponsesReferenceImage:
    bytes_data: bytes
    mime_type: str
    filename: str | None = None

    @property
    def data_url(self) -> str:
        encoded = b64encode(self.bytes_data).decode("utf-8")
        return f"data:{self.mime_type};base64,{encoded}"


@dataclass(slots=True)
class ResponsesImageResult:
    bytes_data: bytes
    mime_type: str
    model_name: str
    provider_name: str
    prompt_version: str
    size: str
    generated_at: datetime
    provider_response_id: str | None
    previous_response_id: str | None
    image_generation_call_id: str | None
    provider_request_json: dict[str, Any]
    provider_output_json: dict[str, Any]


def _get_value(item: Any, key: str, default: Any = None) -> Any:
    if isinstance(item, dict):
        return item.get(key, default)
    return getattr(item, key, default)


def _jsonable(item: Any) -> Any:
    if item is None or isinstance(item, str | int | float | bool):
        return item
    if isinstance(item, list | tuple):
        return [_jsonable(value) for value in item]
    if isinstance(item, dict):
        return {str(key): _jsonable(value) for key, value in item.items()}
    if hasattr(item, "model_dump"):
        return item.model_dump(mode="json", exclude_none=True)
    if hasattr(item, "__dict__"):
        return {key: _jsonable(value) for key, value in vars(item).items() if not key.startswith("_")}
    return str(item)


def _sanitize_base64_images(item: Any) -> Any:
    if isinstance(item, list):
        return [_sanitize_base64_images(value) for value in item]
    if isinstance(item, dict):
        sanitized: dict[str, Any] = {}
        for key, value in item.items():
            if key in {"image_url", "result"} and isinstance(value, str):
                if value.startswith("data:image/"):
                    prefix = value.split(",", maxsplit=1)[0]
                    sanitized[key] = f"{prefix},<base64 omitted {len(value)} chars>"
                    continue
                if key == "result":
                    sanitized[key] = f"<base64 omitted {len(value)} chars>"
                    continue
            sanitized[key] = _sanitize_base64_images(value)
        return sanitized
    return item


def decode_reference_data_url(data_url: str) -> ResponsesReferenceImage:
    if not data_url.startswith("data:") or ";base64," not in data_url:
        raise RuntimeError("对话中的参考图不是合法 data URL")
    header, encoded = data_url.split(",", maxsplit=1)
    mime_type = header[5:].split(";", maxsplit=1)[0] or "image/png"
    return ResponsesReferenceImage(bytes_data=decode_b64_image(encoded), mime_type=mime_type)


def build_responses_reference_images_from_data_urls(
    data_urls: list[str],
    *,
    limit: int,
) -> list[ResponsesReferenceImage]:
    return [decode_reference_data_url(data_url) for data_url in data_urls[:limit]]


def _mime_type_from_output_format(value: Any) -> str | None:
    normalized = "" if value is None else str(value).strip().lower()
    return {
        "png": "image/png",
        "jpeg": "image/jpeg",
        "jpg": "image/jpeg",
        "webp": "image/webp",
    }.get(normalized)


def _mime_type_from_image_bytes(bytes_data: bytes) -> str | None:
    if bytes_data.startswith(b"\x89PNG\r\n\x1a\n"):
        return "image/png"
    if bytes_data.startswith(b"\xff\xd8\xff"):
        return "image/jpeg"
    if bytes_data.startswith(b"RIFF") and bytes_data[8:12] == b"WEBP":
        return "image/webp"
    return None


def _infer_generated_mime_type(output_call: Any, bytes_data: bytes) -> str:
    detected = _mime_type_from_image_bytes(bytes_data)
    if detected:
        return detected

    output_mime = _get_value(output_call, "mime_type") or _get_value(output_call, "content_type")
    if isinstance(output_mime, str) and output_mime.startswith("image/"):
        return output_mime

    output_format = _get_value(output_call, "output_format") or _get_value(output_call, "format")
    return _mime_type_from_output_format(output_format) or "image/png"


class OpenAIResponsesImageClient:
    provider_name = "openai-responses"
    prompt_version = "responses-image-generation-v1"

    def __init__(self, provider_config: ResolvedImageProviderConfig | None = None) -> None:
        settings = get_runtime_settings()
        resolved_config = provider_config or resolve_image_provider_config()
        self.api_key = resolved_config.api_key
        self.base_url = resolved_config.base_url
        self.model = resolved_config.model
        self.background_enabled = resolved_config.responses_background_enabled
        self.tool_model = settings.image_tool_model
        self.tool_quality = settings.image_tool_quality
        self.tool_output_format = settings.image_tool_output_format
        self.tool_output_compression = settings.image_tool_output_compression
        self.tool_background = settings.image_tool_background
        self.tool_moderation = settings.image_tool_moderation
        self.tool_action = settings.image_tool_action
        self.tool_input_fidelity = settings.image_tool_input_fidelity
        self.tool_partial_images = settings.image_tool_partial_images
        self.tool_allowed_fields = parse_image_tool_allowed_fields(settings.image_tool_allowed_fields)

    def generate_image(
        self,
        *,
        prompt: str,
        size: str,
        reference_images: list[ResponsesReferenceImage] | None = None,
        previous_response_id: str | None = None,
        tool_options: dict[str, Any] | None = None,
        progress_callback: Callable[[dict[str, Any]], None] | None = None,
    ) -> ResponsesImageResult:
        if not self.api_key:
            raise RuntimeError("图片供应商档案缺少 API Key")

        reference_images = reference_images or []
        tool = self._build_image_generation_tool(size, tool_options=tool_options)
        input_payload = self._build_input(prompt=prompt, reference_images=reference_images)
        task_context = self._progress_task_context(progress_callback)
        request_payload: dict[str, Any] = {
            "model": self.model,
            "input": input_payload,
            "tools": [tool],
        }
        if self.background_enabled:
            request_payload["background"] = True
        if previous_response_id:
            request_payload["previous_response_id"] = previous_response_id

        client_kwargs: dict[str, Any] = {"api_key": self.api_key}
        if self.base_url:
            client_kwargs["base_url"] = self.base_url
        fallback_used = False
        requested_tool = dict(tool)
        try:
            client = OpenAI(**client_kwargs)
        except Exception as exc:  # noqa: BLE001
            self._log_provider_exception(
                "初始化 Responses 图片供应商失败",
                exc=exc,
                phase="client_init",
                request_payload=request_payload,
                task_context=task_context,
            )
            raise RuntimeError(PROVIDER_REQUEST_FAILURE_MESSAGE) from exc

        try:
            response, request_payload, fallback_used = self._create_response_with_fallback(
                client=client,
                request_payload=request_payload,
                requested_tool=tool,
                size=size,
                task_context=task_context,
            )
            response = self._poll_background_response(
                client,
                response,
                request_payload=request_payload,
                progress_callback=progress_callback,
                task_context=task_context,
            )

            output_call = self._extract_image_generation_call(response)
            image_b64 = _get_value(output_call, "result")
            if not image_b64:
                raise RuntimeError(PROVIDER_MISSING_OUTPUT_MESSAGE)

            image_bytes = decode_b64_image(image_b64)
        except RuntimeError as exc:
            if str(exc) in {
                PROVIDER_REQUEST_FAILURE_MESSAGE,
                PROVIDER_BACKGROUND_INCOMPLETE_MESSAGE,
                PROVIDER_MISSING_OUTPUT_MESSAGE,
                PROVIDER_TEXT_OUTPUT_MESSAGE,
            }:
                raise
            self._log_provider_exception(
                "解析 Responses 图片供应商结果失败",
                exc=exc,
                phase="parse_response",
                request_payload=request_payload,
                response=response if "response" in locals() else None,
                task_context=task_context,
            )
            raise RuntimeError(PROVIDER_REQUEST_FAILURE_MESSAGE) from exc
        except Exception as exc:  # noqa: BLE001
            self._log_provider_exception(
                "解析 Responses 图片供应商结果失败",
                exc=exc,
                phase="parse_response",
                request_payload=request_payload,
                response=response if "response" in locals() else None,
                task_context=task_context,
            )
            raise RuntimeError(PROVIDER_REQUEST_FAILURE_MESSAGE) from exc
        response_json = _jsonable(response)
        request_json = _sanitize_base64_images(request_payload)
        output_json = _sanitize_base64_images(response_json)
        productflow_metadata = self._build_productflow_metadata(
            requested_tool=requested_tool,
            effective_response=response_json,
            output_call=output_call,
            fallback_used=fallback_used,
        )
        if productflow_metadata:
            request_json["_productflow"] = {
                "requested_image_tool": requested_tool,
                "fallback_used": fallback_used,
            }
            output_json["_productflow"] = productflow_metadata
        response_id = str(_get_value(response, "id", "") or "") or None
        call_id = str(_get_value(output_call, "id", "") or "") or None
        return ResponsesImageResult(
            bytes_data=image_bytes,
            mime_type=_infer_generated_mime_type(output_call, image_bytes),
            model_name=self.model,
            provider_name=self.provider_name,
            prompt_version=self.prompt_version,
            size=size,
            generated_at=datetime.now(UTC),
            provider_response_id=response_id,
            previous_response_id=previous_response_id,
            image_generation_call_id=call_id,
            provider_request_json=request_json,
            provider_output_json=output_json,
        )

    def reconcile_response(self, provider_response_id: str) -> ProviderEffectQueryResult:
        """Read a Responses record without creating another image request."""

        if not self.api_key:
            return ProviderEffectQueryResult(
                effect_result="unknown",
                reconciliation_state="unknown",
                provider_response_id=provider_response_id,
                detail="图片 provider 档案缺少 API Key，无法查询原请求",
            )
        client_kwargs: dict[str, Any] = {"api_key": self.api_key}
        if self.base_url:
            client_kwargs["base_url"] = self.base_url
        try:
            client = OpenAI(**client_kwargs)
            retrieve = getattr(client.responses, "retrieve", None)
            if not callable(retrieve):
                return ProviderEffectQueryResult.unsupported("当前 Responses client 不支持查询 response")
            response = retrieve(provider_response_id)
        except Exception as exc:  # noqa: BLE001
            return ProviderEffectQueryResult(
                effect_result="unknown",
                reconciliation_state="unknown",
                provider_response_id=provider_response_id,
                detail=f"查询图片 provider response 失败: {type(exc).__name__}",
            )

        response_id = str(_get_value(response, "id", "") or "") or provider_response_id
        status = str(_get_value(response, "status", "") or "").lower() or None
        output_items = _get_value(response, "output", []) or []
        image_call = next(
            (item for item in output_items if _get_value(item, "type", "") == "image_generation_call"),
            None,
        )
        has_image_result = bool(image_call is not None and _get_value(image_call, "result", None))
        result_json = {
            "provider_response_id": response_id,
            "provider_status": status,
            "has_image_generation_call": image_call is not None,
            "has_image_result": has_image_result,
        }
        if status in RESPONSES_TERMINAL_FAILURE_STATUSES:
            return ProviderEffectQueryResult(
                effect_result="failed",
                reconciliation_state="not_applied",
                provider_response_id=response_id,
                provider_status=status,
                result_json=result_json,
                detail="供应商记录显示图片请求未完成",
            )
        if status in RESPONSES_IN_PROGRESS_STATUSES:
            return ProviderEffectQueryResult(
                effect_result="unknown",
                reconciliation_state="unknown",
                provider_response_id=response_id,
                provider_status=status,
                result_json=result_json,
                detail="图片 provider 请求仍在处理中",
            )
        if has_image_result:
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
            detail="供应商 response 没有足够的图片结果证据",
        )

    def _create_response_with_fallback(
        self,
        *,
        client: Any,
        request_payload: dict[str, Any],
        requested_tool: dict[str, Any],
        size: str,
        task_context: dict[str, Any],
    ) -> tuple[Any, dict[str, Any], bool]:
        fallback_used = False
        current_payload = request_payload
        while True:
            try:
                return client.responses.create(**current_payload), current_payload, fallback_used
            except Exception as exc:  # noqa: BLE001
                if current_payload.get("background") is True and self._is_background_unsupported_error(exc):
                    self._log_provider_exception(
                        "Responses 图片供应商不支持 background，回退同步请求",
                        exc=exc,
                        phase="create_background_fallback",
                        request_payload=current_payload,
                        task_context=task_context,
                        level=logging.INFO,
                    )
                    current_payload = dict(current_payload)
                    current_payload.pop("background", None)
                    continue
                if self._has_optional_tool_fields(current_payload["tools"][0]):
                    self._log_provider_exception(
                        "Responses 图片供应商拒绝高级 tool 字段，回退基础 tool",
                        exc=exc,
                        phase="create_tool_fallback",
                        request_payload=current_payload,
                        task_context=task_context,
                        level=logging.INFO,
                    )
                    current_payload = dict(current_payload)
                    current_payload["tools"] = [self._build_image_generation_tool(size, include_optional=False)]
                    fallback_used = current_payload["tools"][0] != requested_tool
                    continue
                self._log_provider_exception(
                    "Responses 图片供应商创建请求失败",
                    exc=exc,
                    phase="create",
                    request_payload=current_payload,
                    task_context=task_context,
                )
                raise RuntimeError(PROVIDER_REQUEST_FAILURE_MESSAGE) from exc

    def _is_background_unsupported_error(self, exc: Exception) -> bool:
        message = str(exc).lower()
        return "background" in message and any(
            marker in message
            for marker in (
                "unknown",
                "unsupported",
                "unexpected",
                "extra",
                "unrecognized",
                "not support",
                "not_supported",
            )
        )

    def _poll_background_response(
        self,
        client: Any,
        response: Any,
        *,
        request_payload: dict[str, Any],
        progress_callback: Callable[[dict[str, Any]], None] | None,
        task_context: dict[str, Any],
    ) -> Any:
        self._emit_response_progress(response, progress_callback)
        response_id = str(_get_value(response, "id", "") or "")
        status = str(_get_value(response, "status", "") or "").lower()
        while response_id and status in RESPONSES_IN_PROGRESS_STATUSES and hasattr(client.responses, "retrieve"):
            sleep(RESPONSES_BACKGROUND_POLL_INTERVAL_SECONDS)
            try:
                response = client.responses.retrieve(response_id)
            except Exception as exc:  # noqa: BLE001
                self._log_provider_exception(
                    "Responses 图片供应商轮询失败",
                    exc=exc,
                    phase="retrieve",
                    request_payload=request_payload,
                    response=response,
                    task_context=task_context,
                )
                raise RuntimeError(PROVIDER_REQUEST_FAILURE_MESSAGE) from exc
            self._emit_response_progress(response, progress_callback)
            status = str(_get_value(response, "status", "") or "").lower()
        if status in RESPONSES_TERMINAL_FAILURE_STATUSES:
            self._log_provider_failure(
                "Responses 图片供应商后台生成失败",
                phase="terminal_status",
                request_payload=request_payload,
                response=response,
                task_context=task_context,
            )
            raise RuntimeError(PROVIDER_BACKGROUND_INCOMPLETE_MESSAGE)
        return response

    def _emit_response_progress(
        self,
        response: Any,
        progress_callback: Callable[[dict[str, Any]], None] | None,
    ) -> None:
        if progress_callback is None:
            return
        response_json = _sanitize_base64_images(_jsonable(response))
        progress_callback(
            {
                "provider_response_id": str(_get_value(response, "id", "") or "") or None,
                "provider_response_status": str(_get_value(response, "status", "") or "") or None,
                "provider_response": response_json,
            }
        )

    def _progress_task_context(
        self,
        progress_callback: Callable[[dict[str, Any]], None] | None,
    ) -> dict[str, Any]:
        if progress_callback is None:
            return {}
        context = getattr(progress_callback, "productflow_context", None)
        return dict(context) if isinstance(context, dict) else {}

    def _request_diagnostic_fields(self, request_payload: dict[str, Any]) -> dict[str, Any]:
        return {
            "model": request_payload.get("model") or self.model,
            "base_url": self._safe_base_url(),
            "background": request_payload.get("background", False),
            "tool_fields": sorted(
                key
                for tool in request_payload.get("tools", [])
                if isinstance(tool, dict)
                for key in tool
                if key != "type"
            ),
        }

    def _safe_base_url(self) -> str | None:
        if not self.base_url:
            return None
        split = urlsplit(self.base_url)
        if split.username or split.password:
            hostname = split.hostname or ""
            netloc = hostname
            if split.port is not None:
                netloc = f"{netloc}:{split.port}"
            return urlunsplit((split.scheme, netloc, split.path, "", ""))
        return urlunsplit((split.scheme, split.netloc, split.path, "", ""))

    def _response_diagnostic_fields(self, response: Any | None) -> dict[str, Any]:
        if response is None:
            return {"provider_response_id": None, "provider_response_status": None}
        return {
            "provider_response_id": str(_get_value(response, "id", "") or "") or None,
            "provider_response_status": str(_get_value(response, "status", "") or "") or None,
        }

    def _log_provider_exception(
        self,
        message: str,
        *,
        exc: Exception,
        phase: str,
        request_payload: dict[str, Any],
        task_context: dict[str, Any],
        response: Any | None = None,
        level: int = logging.WARNING,
    ) -> None:
        fields = {
            **task_context,
            **self._request_diagnostic_fields(request_payload),
            **self._response_diagnostic_fields(response),
            "phase": phase,
            "exception_class": type(exc).__name__,
        }
        logger.log(
            level,
            (
                "%s: phase=%s task_id=%s session_id=%s candidate_index=%s candidate_count=%s "
                "model=%s base_url=%s background=%s status=%s response_id=%s exception_class=%s tool_fields=%s"
            ),
            message,
            fields["phase"],
            fields.get("task_id"),
            fields.get("session_id"),
            fields.get("candidate_index"),
            fields.get("candidate_count"),
            fields["model"],
            fields["base_url"],
            fields["background"],
            fields["provider_response_status"],
            fields["provider_response_id"],
            fields["exception_class"],
            fields["tool_fields"],
        )

    def _log_provider_failure(
        self,
        message: str,
        *,
        phase: str,
        request_payload: dict[str, Any],
        task_context: dict[str, Any],
        response: Any | None = None,
    ) -> None:
        fields = {
            **task_context,
            **self._request_diagnostic_fields(request_payload),
            **self._response_diagnostic_fields(response),
            "phase": phase,
        }
        logger.warning(
            (
                "%s: phase=%s task_id=%s session_id=%s candidate_index=%s candidate_count=%s "
                "model=%s base_url=%s background=%s status=%s response_id=%s tool_fields=%s"
            ),
            message,
            fields["phase"],
            fields.get("task_id"),
            fields.get("session_id"),
            fields.get("candidate_index"),
            fields.get("candidate_count"),
            fields["model"],
            fields["base_url"],
            fields["background"],
            fields["provider_response_status"],
            fields["provider_response_id"],
            fields["tool_fields"],
        )

    def _build_image_generation_tool(
        self,
        size: str,
        *,
        tool_options: dict[str, Any] | None = None,
        include_optional: bool = True,
    ) -> dict[str, Any]:
        tool: dict[str, Any] = {"type": "image_generation", "size": size}
        if not include_optional:
            return tool
        runtime_options = {
            "model": self.tool_model,
            "quality": self.tool_quality,
            "output_format": self.tool_output_format,
            "output_compression": self.tool_output_compression,
            "background": self.tool_background,
            "moderation": self.tool_moderation,
            "action": self.tool_action,
            "input_fidelity": self.tool_input_fidelity,
            "partial_images": self.tool_partial_images,
        }
        merged_options = filter_image_tool_options(
            {**runtime_options, **(tool_options or {})},
            allowed_fields=self.tool_allowed_fields,
        )
        for key in IMAGE_TOOL_OPTIONAL_FIELD_KEYS:
            value = (merged_options or {}).get(key)
            if value is None:
                continue
            if isinstance(value, str) and not value.strip():
                continue
            tool[key] = value
        return tool

    def _has_optional_tool_fields(self, tool: dict[str, Any]) -> bool:
        return any(key in tool for key in IMAGE_TOOL_OPTIONAL_FIELD_KEYS)

    def _build_productflow_metadata(
        self,
        *,
        requested_tool: dict[str, Any],
        effective_response: dict[str, Any],
        output_call: Any,
        fallback_used: bool,
    ) -> dict[str, Any] | None:
        notes: list[dict[str, Any]] = []
        effective_tool = self._extract_effective_tool_metadata(effective_response, output_call)
        requested_relevant = {
            key: value
            for key, value in requested_tool.items()
            if key == "size" or key in IMAGE_TOOL_OPTIONAL_FIELD_KEYS
        }
        adjusted_fields = [
            key
            for key, requested_value in requested_relevant.items()
            if key in effective_tool and effective_tool[key] != requested_value
        ]
        if fallback_used:
            notes.append(
                {
                    "kind": "fallback",
                    "message": "供应商不支持部分参数，已按基础参数完成。",
                }
            )
        if adjusted_fields and not fallback_used:
            notes.append(
                {
                    "kind": "provider_adjusted",
                    "message": f"供应商调整了 {', '.join(adjusted_fields)}。",
                    "fields": adjusted_fields,
                }
            )
        if not notes and not effective_tool:
            return None
        return {
            "requested_image_tool": requested_relevant,
            "effective_image_tool": effective_tool,
            "notes": notes,
        }

    def _extract_effective_tool_metadata(self, response_json: dict[str, Any], output_call: Any) -> dict[str, Any]:
        effective: dict[str, Any] = {}
        output_keys = ("size", "quality", "output_format", "background", "action", "input_fidelity")
        for key in output_keys:
            value = _get_value(output_call, key)
            if value is not None:
                effective[key] = value
        response_tools = response_json.get("tools")
        if isinstance(response_tools, list):
            for tool in response_tools:
                if isinstance(tool, dict) and tool.get("type") == "image_generation":
                    for key in ("size", *IMAGE_TOOL_OPTIONAL_FIELD_KEYS):
                        if key not in effective and tool.get(key) is not None:
                            effective[key] = tool[key]
                    break
        return effective

    def _build_input(
        self,
        *,
        prompt: str,
        reference_images: list[ResponsesReferenceImage],
    ) -> str | list[dict[str, Any]]:
        if not reference_images:
            return prompt

        content: list[dict[str, Any]] = [{"type": "input_text", "text": prompt}]
        for image in reference_images:
            content.append({"type": "input_image", "image_url": image.data_url})
        return [{"role": "user", "content": content}]

    def _extract_image_generation_call(self, response: Any) -> Any:
        output_items = _get_value(response, "output", []) or []
        for output in output_items:
            if _get_value(output, "type") == "image_generation_call":
                return output
        if self._has_text_output(output_items):
            raise RuntimeError(PROVIDER_TEXT_OUTPUT_MESSAGE)
        raise RuntimeError(PROVIDER_MISSING_OUTPUT_MESSAGE)

    def _has_text_output(self, output_items: list[Any]) -> bool:
        for output in output_items:
            if _get_value(output, "type") == "message":
                content_items = _get_value(output, "content", []) or []
                for content in content_items:
                    text = str(_get_value(content, "text", "") or "").strip()
                    if _get_value(content, "type") == "output_text" and text:
                        return True
        return False


class OpenAIResponsesImageProvider(ImageProvider):
    provider_name = OpenAIResponsesImageClient.provider_name
    prompt_version = OpenAIResponsesImageClient.prompt_version

    def __init__(self, provider_config: ResolvedImageProviderConfig | None = None) -> None:
        self.provider_config = provider_config or resolve_image_provider_config()
        self.client = OpenAIResponsesImageClient(self.provider_config)

    def generate_workflow_image(self, request: WorkflowImageRequest) -> WorkflowImageResult:
        size = map_generation_spec_to_openai_size(request.generation_spec)
        requested_options = _workflow_responses_tool_options(request)
        result = self.client.generate_image(
            prompt=request.compiled_prompt,
            size=size,
            reference_images=[_responses_reference(reference) for reference in request.references],
            tool_options=requested_options,
        )
        output_metadata = result.provider_output_json.get("_productflow")
        output_metadata = output_metadata if isinstance(output_metadata, dict) else {}
        effective_tool = output_metadata.get("effective_image_tool")
        if not isinstance(effective_tool, dict) or not effective_tool:
            request_tools = result.provider_request_json.get("tools")
            effective_tool = (
                dict(request_tools[0])
                if isinstance(request_tools, list) and request_tools and isinstance(request_tools[0], dict)
                else {"size": size}
            )
        notes = [dict(note) for note in output_metadata.get("notes", []) if isinstance(note, dict)]
        for key, value in requested_options.items():
            if key not in effective_tool:
                notes.append(
                    {
                        "kind": "parameter_not_sent",
                        "field": key,
                        "requested": value,
                        "message": f"Responses adapter 未发送 {key}，当前 provider 配置未开放该字段。",
                    }
                )
        if (
            request.generation_spec.reference_fidelity == "medium"
            and request.references
            and image_type_family(request.image_type_key or "") != "infographic"
        ):
            notes.append(
                {
                    "kind": "reference_fidelity_mapped",
                    "requested": "medium",
                    "effective": "high",
                    "message": "Responses image tool 不支持 medium input_fidelity，已映射为 high。",
                }
            )
        if request.references and image_type_family(request.image_type_key or "") == "infographic":
            notes.append(
                {
                    "kind": "reference_fidelity_mapped",
                    "requested": request.generation_spec.reference_fidelity,
                    "effective": "low",
                    "message": "卖点/信息图需要重新构图，参考保真按 low 发送，避免原图贴字。",
                }
            )
        if image_type_family(request.image_type_key or "") == "infographic" and request.references:
            notes.append(
                {
                    "kind": "reference_fidelity_mapped",
                    "requested": request.generation_spec.reference_fidelity,
                    "effective": "low",
                    "message": "卖点或信息图需要重新构图，参考保真按 low 发送，避免原图贴字。",
                }
            )
        effective_parameters = {
            "adapter": "openai_responses",
            "model": result.model_name,
            "reference_image_count": len(request.references),
            **{key: value for key, value in effective_tool.items() if key != "type"},
            "notes": notes,
        }
        status = str(result.provider_output_json.get("status", "") or "completed")
        return WorkflowImageResult(
            images=(
                WorkflowGeneratedImage(
                    bytes_data=result.bytes_data,
                    mime_type=result.mime_type,
                ),
            ),
            model=result.model_name,
            provider_response_id=result.provider_response_id,
            provider_status=status,
            effective_parameters=effective_parameters,
            provider_request_json=result.provider_request_json,
            provider_output_json=result.provider_output_json,
        )

    def reconcile_workflow_image_effect(
        self,
        *,
        operation_key: str,
        request_hash: str,
        provider_response_id: str | None,
    ) -> ProviderEffectQueryResult:
        del operation_key, request_hash
        if not provider_response_id:
            return ProviderEffectQueryResult.unsupported("图片 provider 没有可查询的 response id")
        return self.client.reconcile_response(provider_response_id)

def _responses_reference(reference: WorkflowImageReference) -> ResponsesReferenceImage:
    return ResponsesReferenceImage(
        bytes_data=reference.bytes_data,
        mime_type=reference.mime_type,
        filename=reference.filename,
    )


def _workflow_responses_tool_options(request: WorkflowImageRequest) -> dict[str, Any]:
    spec = request.generation_spec
    options: dict[str, Any] = {
        "action": "generate",
        "quality": {"draft": "low", "standard": "medium", "high": "high"}[spec.quality_intent],
        "output_format": "png",
        "background": spec.background_intent,
    }
    if request.references:
        family = image_type_family(request.image_type_key or "")
        if family == "infographic":
            options["input_fidelity"] = "low"
        else:
            options["input_fidelity"] = {
                "low": "low",
                "medium": "high",
                "high": "high",
            }[spec.reference_fidelity]
    return options
