from __future__ import annotations

from productflow_backend.config import get_runtime_settings
from productflow_backend.infrastructure.text.base import TextProvider
from productflow_backend.infrastructure.text.mock_provider import MockTextProvider
from productflow_backend.infrastructure.text.openai_provider import OpenAITextProvider


def get_text_provider() -> TextProvider:
    """根据运行时配置选择文本生成供应商。"""
    settings = get_runtime_settings()
    if settings.text_provider_kind == "openai":
        return OpenAITextProvider()
    return MockTextProvider()
