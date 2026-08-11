from __future__ import annotations

from productflow_backend.infrastructure.prompt.base import PromptGenerationProvider
from productflow_backend.infrastructure.prompt.mock_provider import MockPromptGenerationProvider
from productflow_backend.infrastructure.prompt.openai_provider import OpenAIPromptGenerationProvider
from productflow_backend.infrastructure.provider_config import resolve_text_provider_config


def get_prompt_generation_provider() -> PromptGenerationProvider:
    provider_config = resolve_text_provider_config()
    if provider_config.provider_kind == "openai":
        return OpenAIPromptGenerationProvider(provider_config)
    return MockPromptGenerationProvider()


__all__ = ["get_prompt_generation_provider"]
