from __future__ import annotations

import re
from collections.abc import Mapping
from typing import Any

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


def text_or_default(value: str | None, default: str) -> str:
    normalized = (value or "").strip()
    return normalized or default
