from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path

from productflow_backend.infrastructure.image.base import ImageProvider
from productflow_backend.infrastructure.image.factory import get_image_provider
from productflow_backend.infrastructure.poster.renderer import PosterRenderer
from productflow_backend.infrastructure.prompt.base import PromptGenerationProvider
from productflow_backend.infrastructure.prompt.factory import get_prompt_generation_provider
from productflow_backend.infrastructure.text.base import TextProvider
from productflow_backend.infrastructure.text.factory import get_text_provider

TextProviderResolver = Callable[[], TextProvider]
ImageProviderResolver = Callable[[], ImageProvider]
PromptGenerationProviderResolver = Callable[[], PromptGenerationProvider]
PosterRendererFactory = Callable[[Path], PosterRenderer]


def _default_text_provider() -> TextProvider:
    return get_text_provider()


def _default_image_provider() -> ImageProvider:
    return get_image_provider()


def _default_prompt_generation_provider() -> PromptGenerationProvider:
    return get_prompt_generation_provider()


@dataclass(frozen=True, slots=True)
class WorkflowExecutionDependencies:
    """Explicit dependency seam for workflow execution provider/renderer adapters."""

    text_provider_resolver: TextProviderResolver = _default_text_provider
    image_provider_resolver: ImageProviderResolver = _default_image_provider
    prompt_generation_provider_resolver: PromptGenerationProviderResolver = _default_prompt_generation_provider
    poster_renderer_factory: PosterRendererFactory = PosterRenderer

    def text_provider(self) -> TextProvider:
        return self.text_provider_resolver()

    def image_provider(self) -> ImageProvider:
        return self.image_provider_resolver()

    def prompt_generation_provider(self) -> PromptGenerationProvider:
        return self.prompt_generation_provider_resolver()

    def poster_renderer(self, font_path: Path) -> PosterRenderer:
        return self.poster_renderer_factory(font_path)


def default_workflow_execution_dependencies() -> WorkflowExecutionDependencies:
    return WorkflowExecutionDependencies()
