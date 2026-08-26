"""与 provider 无关的 image-session 聊天 DTO，以及规范化后的 provider 失败。"""

from __future__ import annotations

from base64 import b64encode
from dataclasses import dataclass
from datetime import datetime
from typing import Any, Literal

from productflow_backend.infrastructure.image.failures import ImageGenerationFailureDecision

IMAGE_SESSION_TEXT_OUTPUT_FAILURE_REASON = "图片供应商已完成请求，但返回的是文字回复，没有返回图片结果"


@dataclass(slots=True)
class ImageChatTurn:
    """用于构建 image-session 上下文的一条 user 或 assistant turn。"""

    role: Literal["user", "assistant"]
    content: str
    image_data_url: str | None = None


@dataclass(slots=True)
class GeneratedChatImage:
    """ImageSession 用例消费的、与 provider 无关的图片结果。"""

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
    provider_request_json: dict[str, Any] | None = None
    provider_output_json: dict[str, Any] | None = None

    @property
    def data_url(self) -> str:
        encoded = b64encode(self.bytes_data).decode("utf-8")
        return f"data:{self.mime_type};base64,{encoded}"


class ImageSessionProviderFailure(RuntimeError):
    """已在 chat-service 边界规范化的 provider 输出失败。"""

    def __init__(
        self,
        safe_reason: str,
        *,
        failure_decision: ImageGenerationFailureDecision | None = None,
    ) -> None:
        super().__init__(safe_reason)
        self.safe_reason = safe_reason
        self.failure_decision = failure_decision


__all__ = [
    "IMAGE_SESSION_TEXT_OUTPUT_FAILURE_REASON",
    "GeneratedChatImage",
    "ImageChatTurn",
    "ImageSessionProviderFailure",
]
