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
        if kind == PosterKind.MAIN_IMAGE:
            kind_requirements = "\n".join(
                [
                    "画面要求：1:1 电商主图，主体居中，背景干净，信息明确，可直接用于商品主图。",
                    "风格要求：白底或浅底，突出商品与卖点角标，整体简洁。",
                ]
            )
        else:
            kind_requirements = "\n".join(
                [
                    "画面要求：3:4 促销海报，层次明显，有强主标题、促销氛围和商品展示区。",
                    "风格要求：更强视觉冲击，适合活动推广页或投放素材。",
                ]
            )
        return render_prompt_template(
            self.poster_image_template,
            {
                "product_name": poster.product_name,
                "category": poster.category or "未提供",
                "price": poster.price or "未提供",
                "source_note": poster.source_note or "未提供",
                "instruction": poster.instruction or "按商品主视觉方向生成",
                "poster_headline": poster.poster_headline,
                "title": poster.title,
                "selling_points": "；".join(poster.selling_points[:3]),
                "cta": poster.cta,
                "size": size,
                "kind": kind.value,
                "kind_label": "主图" if kind == PosterKind.MAIN_IMAGE else "促销海报",
                "kind_requirements": kind_requirements,
            },
        )
