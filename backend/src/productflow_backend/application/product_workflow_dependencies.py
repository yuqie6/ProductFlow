from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path

from productflow_backend.infrastructure.image.base import ImageProvider
from productflow_backend.infrastructure.poster.renderer import PosterRenderer
from productflow_backend.infrastructure.text.base import TextProvider

TextProviderResolver = Callable[[], TextProvider]
ImageProviderResolver = Callable[[], ImageProvider]
PosterRendererFactory = Callable[[Path], PosterRenderer]


def _facade_text_provider() -> TextProvider:
    """Default resolver kept facade-routed so legacy monkeypatch targets still work."""

    from productflow_backend.application import product_workflows

    return product_workflows.get_text_provider()


def _facade_image_provider() -> ImageProvider:
    """Default resolver kept facade-routed so legacy monkeypatch targets still work."""

    from productflow_backend.application import product_workflows

    return product_workflows.get_image_provider()


@dataclass(frozen=True, slots=True)
class WorkflowExecutionDependencies:
    """Explicit dependency seam for workflow execution provider/renderer adapters."""

    text_provider_resolver: TextProviderResolver = _facade_text_provider
    image_provider_resolver: ImageProviderResolver = _facade_image_provider
    poster_renderer_factory: PosterRendererFactory = PosterRenderer

    def text_provider(self) -> TextProvider:
        return self.text_provider_resolver()

    def image_provider(self) -> ImageProvider:
        return self.image_provider_resolver()

    def poster_renderer(self, font_path: Path) -> PosterRenderer:
        return self.poster_renderer_factory(font_path)


def default_workflow_execution_dependencies() -> WorkflowExecutionDependencies:
    return WorkflowExecutionDependencies()
