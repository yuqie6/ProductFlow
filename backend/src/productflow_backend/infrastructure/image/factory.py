from __future__ import annotations

from productflow_backend.config import get_runtime_settings
from productflow_backend.infrastructure.image.base import ImageProvider
from productflow_backend.infrastructure.image.mock_provider import MockImageProvider
from productflow_backend.infrastructure.image.responses_provider import OpenAIResponsesImageProvider


def get_image_provider() -> ImageProvider:
    """根据运行时配置选择图片生成供应商。"""
    settings = get_runtime_settings()
    if settings.image_provider_kind == "mock":
        return MockImageProvider()
    if settings.image_provider_kind == "openai_responses":
        return OpenAIResponsesImageProvider()
    raise RuntimeError(f"暂不支持的图片 provider: {settings.image_provider_kind}")
