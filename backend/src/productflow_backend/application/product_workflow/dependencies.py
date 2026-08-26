from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass

from productflow_backend.infrastructure.image.base import ImageProvider
from productflow_backend.infrastructure.image.factory import get_image_provider
from productflow_backend.infrastructure.prompt.base import PromptGenerationProvider
from productflow_backend.infrastructure.prompt.factory import get_prompt_generation_provider

ImageProviderResolver = Callable[[], ImageProvider]
PromptGenerationProviderResolver = Callable[[], PromptGenerationProvider]


def _default_image_provider() -> ImageProvider:
    return get_image_provider()


def _default_prompt_generation_provider() -> PromptGenerationProvider:
    return get_prompt_generation_provider()


@dataclass(frozen=True, slots=True)
class WorkflowExecutionDependencies:
    """图执行所需的显式 provider 依赖。"""

    image_provider_resolver: ImageProviderResolver = _default_image_provider
    prompt_generation_provider_resolver: PromptGenerationProviderResolver = _default_prompt_generation_provider

    def image_provider(self) -> ImageProvider:
        return self.image_provider_resolver()

    def prompt_generation_provider(self) -> PromptGenerationProvider:
        return self.prompt_generation_provider_resolver()


def default_workflow_execution_dependencies() -> WorkflowExecutionDependencies:
    return WorkflowExecutionDependencies()
