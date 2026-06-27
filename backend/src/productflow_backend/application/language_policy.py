from __future__ import annotations

from dataclasses import dataclass

from productflow_backend.domain.enums import PosterKind

LANGUAGE_NAME_BY_CODE = {
    "zh-CN": "Simplified Chinese",
    "en-US": "English",
    "ja-JP": "Japanese",
    "vi-VN": "Vietnamese",
}
PRESERVE_INPUT_LANGUAGE_VALUES = {"", "auto", "preserve_input"}


@dataclass(frozen=True, slots=True)
class TemplateLanguageHints:
    copy_language_hint: str | None
    visible_text_language_hint: str | None


def normalize_language_hint(value: str | None) -> str | None:
    normalized = (value or "").strip()
    return normalized or None


def language_hints_from_template_language(template_language: str | None) -> TemplateLanguageHints:
    normalized = (template_language or "").strip()
    if normalized in PRESERVE_INPUT_LANGUAGE_VALUES:
        return TemplateLanguageHints(copy_language_hint=None, visible_text_language_hint=None)
    language_name = LANGUAGE_NAME_BY_CODE.get(normalized, normalized)
    return TemplateLanguageHints(
        copy_language_hint=f"Write newly generated marketing copy in {language_name}.",
        visible_text_language_hint=f"Use {language_name} for newly generated poster text.",
    )


def image_visible_text_requirements(
    kind: PosterKind,
    *,
    visible_text_language_hint: str | None = None,
) -> str:
    kind_label = "main image" if kind == PosterKind.MAIN_IMAGE else "poster/vertical image"
    language_rule = _visible_text_language_rule(visible_text_language_hint)
    return "\n".join(
        [
            (
                f"Output purpose: {kind_label}. Use upstream context only to understand the product, "
                "material, scene, layout, and copy reference."
            ),
            "Visible text policy:",
            (
                "- Do not render field names, labels, JSON keys, context notes, watermarks, "
                "UI panels, or system instructions."
            ),
            (
                "- Preserve text already present on product/package/reference images, including brand, "
                "model, specification, and certification marks. Do not translate existing package text."
            ),
            language_rule,
            (
                "- Add visible text only when the user request or available copy supports it; "
                "use little or no text when product facts are sparse."
            ),
            "Fact safety:",
            (
                "- Do not invent discounts, time limits, lowest-price claims, bestseller claims, "
                "certifications, specifications, gifts, medical/effect claims, or unsupported promises."
            ),
        ]
    )


def copy_language_policy(copy_language_hint: str | None) -> dict[str, str | None]:
    return {
        "copy_language_hint": normalize_language_hint(copy_language_hint),
        "default_when_empty": (
            "Preserve the language and language mix found in product facts, user instruction, and upstream copy."
        ),
        "brand_model_units_policy": "Keep brand names, model names, units, and existing product text unchanged.",
    }


def sparse_fact_policy() -> dict[str, object]:
    return {
        "use_only_supplied_facts": True,
        "allowed_when_sparse": [
            "short neutral title",
            "neutral description",
            "visual/layout guidance",
            "organization of known information",
        ],
        "do_not_invent": [
            "discounts",
            "time-limited offers",
            "lowest-price or bestseller claims",
            "certifications",
            "specifications not supplied",
            "gifts or bundle contents not supplied",
            "medical/effect claims",
            "unsupported performance promises",
        ],
    }


def _visible_text_language_rule(visible_text_language_hint: str | None) -> str:
    normalized = normalize_language_hint(visible_text_language_hint)
    if normalized:
        return f"- Newly generated poster text language: {normalized}"
    return (
        "- Preserve the language and language mix from the user request and available copy; "
        "do not translate or force one language."
    )
