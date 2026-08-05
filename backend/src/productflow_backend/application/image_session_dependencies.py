from __future__ import annotations

from base64 import b64encode
from collections.abc import Callable
from dataclasses import dataclass
from datetime import datetime
from typing import Any, Literal, Protocol

from productflow_backend.application.image_generation_failures import ImageGenerationFailureDecision

IMAGE_SESSION_TEXT_OUTPUT_FAILURE_REASON = "图片供应商已完成请求，但返回的是文字回复，没有返回图片结果"


@dataclass(slots=True)
class ImageChatTurn:
    """A single user or assistant turn used to build image-session context."""

    role: Literal["user", "assistant"]
    content: str
    image_data_url: str | None = None


@dataclass(slots=True)
class GeneratedChatImage:
    """The provider-neutral image result consumed by the ImageSession use case."""

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


class ImageSessionChatService(Protocol):
    provider_kind: str

    def generate(
        self,
        prompt: str,
        size: str,
        history: list[ImageChatTurn],
        manual_reference_images: list[str],
        previous_response_id: str | None = None,
        tool_options: dict | None = None,
        progress_callback: Callable[[dict[str, Any]], None] | None = None,
    ) -> GeneratedChatImage: ...

    def generate_many(
        self,
        prompt: str,
        size: str,
        history: list[ImageChatTurn],
        manual_reference_images: list[str],
        *,
        candidate_count: int,
        tool_options: dict | None = None,
    ) -> list[GeneratedChatImage]: ...


ImageSessionChatServiceFactory = Callable[[], ImageSessionChatService]


class ImageSessionProviderFailure(RuntimeError):
    """A provider output failure already normalized at the chat-service boundary."""

    def __init__(
        self,
        safe_reason: str,
        *,
        failure_decision: ImageGenerationFailureDecision | None = None,
    ) -> None:
        super().__init__(safe_reason)
        self.safe_reason = safe_reason
        self.failure_decision = failure_decision


def default_image_session_chat_service_factory() -> ImageSessionChatService:
    """Keep legacy direct application callers on the existing production adapter."""

    from productflow_backend.infrastructure.image.chat_service import ImageChatService

    return ImageChatService()
