from __future__ import annotations

import re
from collections.abc import Mapping
from typing import Any

from productflow_backend.application.contracts import PosterGenerationInput
from productflow_backend.application.language_policy import image_visible_text_requirements
from productflow_backend.domain.enums import PosterKind

_PROMPT_PLACEHOLDER_RE = re.compile(r"{([A-Za-z_][A-Za-z0-9_]*)}")


def render_prompt_template(template: str, values: Mapping[str, Any]) -> str:
    """Render an operator-editable prompt template.

    Prompt values may contain user-authored product/copy text, so callers should not log the rendered result.
    """

    def replace(match: re.Match[str]) -> str:
        key = match.group(1)
        if key not in values:
            return match.group(0)
        value = values[key]
        return "" if value is None else str(value)

    rendered = _PROMPT_PLACEHOLDER_RE.sub(replace, template)
    lines = [line.rstrip() for line in rendered.splitlines()]
    return "\n".join(line for line in lines if line.strip()).strip()


def render_poster_image_prompt(
    poster: PosterGenerationInput,
    kind: PosterKind,
    size: str,
    *,
    image_template: str,
    edit_template: str,
    reference_policy: str,
) -> str:
    template = image_template if poster.copy_prompt_mode == "copy" else edit_template
    return render_prompt_template(
        template,
        {
            "product_name": poster.product_name,
            "category": poster.category or "",
            "price": poster.price or "",
            "source_note": poster.source_note or "",
            "instruction": poster.instruction or "Free image generation.",
            "context_block": _poster_context_block(poster),
            "reference_policy": reference_policy if _poster_has_reference_input(poster) else "",
            "visible_text_language_hint": poster.visible_text_language_hint or "",
            "size": size,
            "kind": kind.value,
            "kind_label": "main image" if kind == PosterKind.MAIN_IMAGE else "promotional poster",
            "kind_requirements": image_visible_text_requirements(
                kind,
                visible_text_language_hint=poster.visible_text_language_hint,
            ),
        },
    )


def _poster_context_block(poster: PosterGenerationInput) -> str:
    lines: list[str] = []
    if poster.product_name:
        lines.append(f"- Subject: {poster.product_name}")
    if poster.category:
        lines.append(f"- Category/type: {poster.category}")
    if poster.price:
        lines.append(f"- Price: {poster.price}")
    if poster.source_note:
        lines.append(f"- Additional notes: {poster.source_note}")
    if poster.copy_prompt_mode == "copy" and poster.structured_copy_context:
        lines.append(
            "- Available copy text (use only when visible text is requested or clearly useful; "
            "do not render field names, labels, or context notes):\n"
            f"{poster.structured_copy_context}"
        )
    if _poster_has_reference_input(poster):
        reference_paths = {str(reference.path.resolve()) for reference in poster.reference_images}
        if poster.source_image is not None:
            reference_paths.add(str(poster.source_image.resolve()))
        lines.append(f"- Reference image count: {len(reference_paths)}")
        if poster.source_image is not None:
            lines.append("- Source product image: input image 1")
        reference_labels = [
            f"{reference.label or reference.filename} (role: {reference.role or 'reference'})"
            for reference in poster.reference_images
        ]
        if reference_labels:
            lines.append(f"- Reference images: {'; '.join(reference_labels)}")
    return "\n".join(lines) if lines else "- No explicit upstream context."


def _poster_has_reference_input(poster: PosterGenerationInput) -> bool:
    return poster.source_image is not None or bool(poster.reference_images)


def text_or_default(value: str | None, default: str) -> str:
    normalized = (value or "").strip()
    return normalized or default
