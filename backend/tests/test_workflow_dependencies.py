from __future__ import annotations

from pathlib import Path
from typing import Any

import pytest

from productflow_backend.application.contracts import PosterGenerationInput
from productflow_backend.application.product_workflow_dependencies import (
    WorkflowExecutionDependencies,
    default_workflow_execution_dependencies,
)
from productflow_backend.application.product_workflow_execution import _generate_workflow_images_concurrently
from productflow_backend.domain.enums import PosterKind


def test_workflow_execution_dependencies_use_explicit_resolvers_without_global_factories() -> None:
    text_provider = object()
    image_provider = object()
    rendered_paths: list[Path] = []

    def renderer_factory(font_path: Path) -> Any:
        rendered_paths.append(font_path)
        return {"font_path": font_path}

    dependencies = WorkflowExecutionDependencies(
        text_provider_resolver=lambda: text_provider,
        image_provider_resolver=lambda: image_provider,
        poster_renderer_factory=renderer_factory,
    )

    font_path = Path("/tmp/productflow-test-font.ttf")

    assert dependencies.text_provider() is text_provider
    assert dependencies.image_provider() is image_provider
    assert dependencies.poster_renderer(font_path) == {"font_path": font_path}
    assert rendered_paths == [font_path]


def test_default_workflow_execution_dependencies_keep_facade_monkeypatch_compatibility(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    text_provider = object()
    image_provider = object()

    monkeypatch.setattr(
        "productflow_backend.application.product_workflows.get_text_provider",
        lambda: text_provider,
    )
    monkeypatch.setattr(
        "productflow_backend.application.product_workflows.get_image_provider",
        lambda: image_provider,
    )

    dependencies = default_workflow_execution_dependencies()

    assert dependencies.text_provider() is text_provider
    assert dependencies.image_provider() is image_provider


def test_workflow_image_generation_uses_injected_renderer_factory() -> None:
    rendered_font_paths: list[Path] = []

    class FakeRenderer:
        def __init__(self, font_path: Path) -> None:
            rendered_font_paths.append(font_path)

        def render(self, render_input: PosterGenerationInput, kind: PosterKind) -> bytes:
            assert render_input.product_name == "渲染注入测试"
            assert kind == PosterKind.MAIN_IMAGE
            return b"injected-renderer-bytes"

    font_path = Path("/tmp/productflow-injected-renderer.ttf")
    source_path = Path("/tmp/productflow-injected-source.png")

    generated = _generate_workflow_images_concurrently(
        render_input=PosterGenerationInput(
            product_name="渲染注入测试",
            title="测试标题",
            selling_points=["卖点一", "卖点二", "卖点三"],
            poster_headline="测试主标题",
            cta="立即测试",
            source_image=source_path,
        ),
        kind=PosterKind.MAIN_IMAGE,
        target_count=1,
        poster_generation_mode="rendered",
        poster_font_path=font_path,
        image_providers=None,
        renderer_factory=FakeRenderer,
    )

    assert rendered_font_paths == [font_path]
    assert generated[0].content == b"injected-renderer-bytes"
