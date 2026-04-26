from __future__ import annotations

from base64 import b64encode
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from openai import OpenAI

from productflow_backend.application.contracts import PosterGenerationInput
from productflow_backend.config import get_runtime_settings
from productflow_backend.domain.enums import PosterKind
from productflow_backend.infrastructure.image.base import (
    GeneratedImagePayload,
    ImageProvider,
    decode_b64_image,
    parse_size,
)
from productflow_backend.infrastructure.prompts import render_prompt_template


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


def _mime_type_for_path(path: Path) -> str:
    suffix = path.suffix.lower()
    if suffix in {".jpg", ".jpeg"}:
        return "image/jpeg"
    if suffix == ".webp":
        return "image/webp"
    return "image/png"


class OpenAIResponsesImageClient:
    provider_name = "openai-responses"
    prompt_version = "responses-image-generation-v1"

    def __init__(self) -> None:
        settings = get_runtime_settings()
        self.api_key = settings.image_api_key
        self.base_url = settings.image_base_url
        self.model = settings.image_generate_model

    def generate_image(
        self,
        *,
        prompt: str,
        size: str,
        reference_images: list[ResponsesReferenceImage] | None = None,
        previous_response_id: str | None = None,
    ) -> ResponsesImageResult:
        if not self.api_key:
            raise RuntimeError("图片供应商缺少 IMAGE_API_KEY")

        reference_images = reference_images or []
        tools = [{"type": "image_generation", "size": size}]
        input_payload = self._build_input(prompt=prompt, reference_images=reference_images)
        request_payload: dict[str, Any] = {
            "model": self.model,
            "input": input_payload,
            "tools": tools,
        }
        if previous_response_id:
            request_payload["previous_response_id"] = previous_response_id

        client_kwargs: dict[str, Any] = {"api_key": self.api_key}
        if self.base_url:
            client_kwargs["base_url"] = self.base_url
        client = OpenAI(**client_kwargs)
        response = client.responses.create(**request_payload)

        output_call = self._extract_image_generation_call(response)
        image_b64 = _get_value(output_call, "result")
        if not image_b64:
            raise RuntimeError("图片供应商没有返回 image_generation_call.result")

        response_json = _jsonable(response)
        response_id = str(_get_value(response, "id", "") or "") or None
        call_id = str(_get_value(output_call, "id", "") or "") or None
        return ResponsesImageResult(
            bytes_data=decode_b64_image(image_b64),
            mime_type="image/png",
            model_name=self.model,
            provider_name=self.provider_name,
            prompt_version=self.prompt_version,
            size=size,
            generated_at=datetime.now(UTC),
            provider_response_id=response_id,
            previous_response_id=previous_response_id,
            image_generation_call_id=call_id,
            provider_request_json=_sanitize_base64_images(request_payload),
            provider_output_json=_sanitize_base64_images(response_json),
        )

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
        raise RuntimeError("图片供应商没有返回 image_generation_call")


class OpenAIResponsesImageProvider(ImageProvider):
    provider_name = OpenAIResponsesImageClient.provider_name
    prompt_version = OpenAIResponsesImageClient.prompt_version

    def __init__(self) -> None:
        settings = get_runtime_settings()
        self.model = settings.image_generate_model
        self.main_image_size = settings.image_main_image_size
        self.promo_poster_size = settings.image_promo_poster_size
        self.poster_image_template = settings.prompt_poster_image_template
        self.poster_image_edit_template = settings.prompt_poster_image_edit_template
        self.client = OpenAIResponsesImageClient()

    def generate_poster_image(
        self,
        poster: PosterGenerationInput,
        kind: PosterKind,
    ) -> tuple[GeneratedImagePayload, str]:
        size = poster.image_size or (self.main_image_size if kind == PosterKind.MAIN_IMAGE else self.promo_poster_size)
        width, height = parse_size(size)
        prompt = self._build_prompt(poster, kind, size)
        result = self.client.generate_image(
            prompt=prompt,
            size=size,
            reference_images=self._build_reference_images(poster),
        )
        variant_label = "generated-main" if kind == PosterKind.MAIN_IMAGE else "generated-promo"
        return (
            GeneratedImagePayload(
                kind=kind,
                bytes_data=result.bytes_data,
                mime_type=result.mime_type,
                width=width,
                height=height,
                variant_label=variant_label,
            ),
            self.model,
        )

    def _build_reference_images(self, poster: PosterGenerationInput) -> list[ResponsesReferenceImage]:
        references: list[ResponsesReferenceImage] = []
        seen_paths: set[str] = set()

        def add_path(path: Path, *, mime_type: str, filename: str | None = None) -> None:
            resolved = path.resolve()
            key = str(resolved)
            if key in seen_paths:
                return
            seen_paths.add(key)
            references.append(
                ResponsesReferenceImage(
                    bytes_data=resolved.read_bytes(),
                    mime_type=mime_type,
                    filename=filename or resolved.name,
                )
            )

        if poster.source_image is not None:
            add_path(
                poster.source_image,
                mime_type=_mime_type_for_path(poster.source_image),
                filename=poster.source_image.name,
            )
        for reference in poster.reference_images:
            add_path(reference.path, mime_type=reference.mime_type, filename=reference.filename)
        return references

    def _build_prompt(
        self,
        poster: PosterGenerationInput,
        kind: PosterKind,
        size: str,
    ) -> str:
        copy_mode = poster.copy_prompt_mode == "copy"
        template = self.poster_image_template if copy_mode else self.poster_image_edit_template
        kind_requirements = self._build_kind_requirements(kind)
        context_block = self._build_context_block(poster)
        return render_prompt_template(
            template,
            {
                "product_name": poster.product_name,
                "category": poster.category or "",
                "price": poster.price or "",
                "source_note": poster.source_note or "",
                "instruction": poster.instruction or "自由生成。",
                "poster_headline": poster.poster_headline,
                "title": poster.title,
                "selling_points": "；".join(poster.selling_points[:3]),
                "cta": poster.cta,
                "context_block": context_block,
                "size": size,
                "kind": kind.value,
                "kind_label": "主图" if kind == PosterKind.MAIN_IMAGE else "促销海报",
                "kind_requirements": kind_requirements,
            },
        )

    def _build_context_block(self, poster: PosterGenerationInput) -> str:
        lines: list[str] = []
        if poster.product_name:
            lines.append(f"- 商品/主体：{poster.product_name}")
        if poster.category:
            lines.append(f"- 类目/类型：{poster.category}")
        if poster.price:
            lines.append(f"- 价格：{poster.price}")
        if poster.source_note:
            lines.append(f"- 补充说明：{poster.source_note}")
        if poster.copy_prompt_mode == "copy" and (
            poster.poster_headline or poster.title or poster.selling_points or poster.cta
        ):
            copy_parts = [
                f"标题：{poster.title}" if poster.title else "",
                f"主标题：{poster.poster_headline}" if poster.poster_headline else "",
                f"要点：{'；'.join(poster.selling_points[:3])}" if poster.selling_points else "",
                f"CTA：{poster.cta}" if poster.cta else "",
            ]
            lines.append(f"- 文案：{'；'.join(part for part in copy_parts if part)}")
        if poster.reference_images or poster.source_image is not None:
            reference_paths = {str(reference.path.resolve()) for reference in poster.reference_images}
            if poster.source_image is not None:
                reference_paths.add(str(poster.source_image.resolve()))
            reference_count = len(reference_paths)
            lines.append(f"- 参考图片数量：{reference_count}")
        return "\n".join(lines) if lines else "- 无显式上游上下文。"

    def _build_kind_requirements(self, kind: PosterKind) -> str:
        kind_label = "主图" if kind == PosterKind.MAIN_IMAGE else "海报/竖图"
        return f"输出用途：{kind_label}。仅遵循用户要求和上游上下文，不额外添加未要求的文字、品牌、水印或 UI 面板。"
