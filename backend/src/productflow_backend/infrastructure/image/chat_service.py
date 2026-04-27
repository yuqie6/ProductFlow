from __future__ import annotations

from base64 import b64decode, b64encode
from dataclasses import dataclass
from datetime import UTC, datetime
from io import BytesIO
from textwrap import shorten
from typing import Literal

from PIL import Image, ImageDraw

from productflow_backend.config import get_runtime_settings
from productflow_backend.infrastructure.image.base import parse_size
from productflow_backend.infrastructure.image.responses_provider import (
    OpenAIResponsesImageClient,
    ResponsesReferenceImage,
)
from productflow_backend.infrastructure.prompts import render_prompt_template


@dataclass(slots=True)
class ImageChatTurn:
    """生图对话中的一轮：用户输入或 AI 回复（含历史图片）。"""

    role: Literal["user", "assistant"]
    content: str
    image_data_url: str | None = None


@dataclass(slots=True)
class GeneratedChatImage:
    """AI 生成的图片结果。"""

    bytes_data: bytes
    mime_type: str
    model_name: str
    provider_name: str
    prompt_version: str
    size: str
    generated_at: datetime
    provider_response_id: str | None = None
    previous_response_id: str | None = None
    image_generation_call_id: str | None = None
    provider_request_json: dict | None = None
    provider_output_json: dict | None = None

    @property
    def data_url(self) -> str:
        encoded = b64encode(self.bytes_data).decode("utf-8")
        return f"data:{self.mime_type};base64,{encoded}"


class ImageChatService:
    provider_name = "image-session"
    prompt_version = "responses-image-session-v1"

    def __init__(self) -> None:
        settings = get_runtime_settings()
        self.provider_kind = settings.image_provider_kind
        self.prompt_template = settings.prompt_image_chat_template

    def generate(
        self,
        prompt: str,
        size: str,
        history: list[ImageChatTurn],
        manual_reference_images: list[str],
        previous_response_id: str | None = None,
        tool_options: dict | None = None,
    ) -> GeneratedChatImage:
        if self.provider_kind == "mock":
            return self._generate_mock(
                prompt=prompt,
                size=size,
                history=history,
                manual_reference_images=manual_reference_images,
            )
        if self.provider_kind == "openai_responses":
            return self._generate_openai_responses(
                prompt=prompt,
                size=size,
                history=history,
                manual_reference_images=manual_reference_images,
                previous_response_id=previous_response_id,
                tool_options=tool_options,
            )
        raise RuntimeError(f"暂不支持的图片 provider: {self.provider_kind}")

    def _generate_mock(
        self,
        prompt: str,
        size: str,
        history: list[ImageChatTurn],
        manual_reference_images: list[str],
    ) -> GeneratedChatImage:
        width, height = parse_size(size)
        background = (246, 238, 255) if history else (240, 244, 255)
        accent = (99, 91, 255)
        image = Image.new("RGB", (width, height), background)
        draw = ImageDraw.Draw(image)
        history_image_count = len([turn for turn in history if turn.role == "assistant" and turn.image_data_url])

        lines = [
            "ProductFlow Image Chat",
            f"Turn: {len([turn for turn in history if turn.role == 'user']) + 1}",
            f"Refs: {len(manual_reference_images)} upload / {history_image_count} history",
            shorten(prompt, width=100, placeholder="..."),
        ]
        y = max(40, height // 10)
        for index, line in enumerate(lines):
            draw.text((48, y + index * 42), line, fill=accent if index == 0 else (47, 47, 61))
        draw.rounded_rectangle(
            (48, y + 190, width - 48, height - 48),
            radius=28,
            outline=(184, 184, 201),
            width=3,
            fill=(255, 255, 255),
        )
        draw.text(
            (72, y + 230),
            "Mock 模式：这里会显示真实图片供应商返回的图片。",
            fill=(82, 82, 91),
        )
        draw.text(
            (72, y + 280),
            "继续分支时，只会传入选择的基图和勾选参考图。",
            fill=(82, 82, 91),
        )

        buffer = BytesIO()
        image.save(buffer, format="PNG")
        return GeneratedChatImage(
            bytes_data=buffer.getvalue(),
            mime_type="image/png",
            model_name="mock-image-chat-v1",
            provider_name="mock",
            prompt_version=self.prompt_version,
            size=size,
            generated_at=datetime.now(UTC),
            previous_response_id=None,
            provider_request_json={
                "prompt": prompt,
                "size": size,
                "history_count": len(history),
                "manual_reference_count": len(manual_reference_images),
                "previous_response_id": None,
            },
        )

    def _generate_openai_responses(
        self,
        prompt: str,
        size: str,
        history: list[ImageChatTurn],
        manual_reference_images: list[str],
        previous_response_id: str | None,
        tool_options: dict | None,
    ) -> GeneratedChatImage:
        client = OpenAIResponsesImageClient()
        history_for_prompt = [] if previous_response_id else history
        history_for_images = [] if previous_response_id else history
        result = client.generate_image(
            prompt=self._build_prompt(prompt=prompt, history=history_for_prompt, size=size),
            size=size,
            reference_images=self._collect_reference_images(history_for_images, manual_reference_images),
            previous_response_id=previous_response_id,
            tool_options=tool_options,
        )
        return GeneratedChatImage(
            bytes_data=result.bytes_data,
            mime_type=result.mime_type,
            model_name=result.model_name,
            provider_name=result.provider_name,
            prompt_version=self.prompt_version,
            size=size,
            generated_at=result.generated_at,
            provider_response_id=result.provider_response_id,
            previous_response_id=result.previous_response_id,
            image_generation_call_id=result.image_generation_call_id,
            provider_request_json=result.provider_request_json,
            provider_output_json=result.provider_output_json,
        )

    def _build_prompt(self, prompt: str, history: list[ImageChatTurn], size: str) -> str:
        recent_turns = history[-8:]
        history_lines = []
        for index, turn in enumerate(recent_turns, start=1):
            role = "用户" if turn.role == "user" else "助手"
            history_lines.append(f"{index}. {role}：{turn.content.strip()}")

        history_block = ""
        if history_lines:
            history_block = "\n".join(["以下是之前对话中已经确认的上下文：", *history_lines])
        return render_prompt_template(
            self.prompt_template,
            {
                "prompt": prompt.strip(),
                "size": size,
                "history_block": history_block,
            },
        )

    def _collect_reference_images(
        self,
        history: list[ImageChatTurn],
        manual_reference_images: list[str],
    ) -> list[ResponsesReferenceImage]:
        references: list[ResponsesReferenceImage] = []

        for data_url in manual_reference_images[:6]:
            references.append(self._decode_reference_image(data_url))

        history_references: list[ResponsesReferenceImage] = []
        for turn in reversed(history):
            if turn.role != "assistant" or not turn.image_data_url:
                continue
            history_references.append(self._decode_reference_image(turn.image_data_url))
            if len(history_references) >= 3:
                break
        history_references.reverse()
        references.extend(history_references)
        return references[:6]

    def _decode_reference_image(self, data_url: str) -> ResponsesReferenceImage:
        if not data_url.startswith("data:") or ";base64," not in data_url:
            raise RuntimeError("对话中的参考图不是合法 data URL")
        header, encoded = data_url.split(",", maxsplit=1)
        mime_type = header[5:].split(";", maxsplit=1)[0] or "image/png"
        return ResponsesReferenceImage(bytes_data=b64decode(encoded), mime_type=mime_type)
