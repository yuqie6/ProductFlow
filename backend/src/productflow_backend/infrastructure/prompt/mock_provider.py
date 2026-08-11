from __future__ import annotations

from productflow_backend.infrastructure.prompt.base import (
    PromptGenerationProvider,
    PromptGenerationRequest,
    PromptGenerationResult,
)


class MockPromptGenerationProvider(PromptGenerationProvider):
    provider_name = "mock"
    model_name = "mock-prompt-v1"

    def generate_prompt(self, request: PromptGenerationRequest) -> PromptGenerationResult:
        return PromptGenerationResult(payload=request.current_prompt, model=self.model_name)


__all__ = ["MockPromptGenerationProvider"]
