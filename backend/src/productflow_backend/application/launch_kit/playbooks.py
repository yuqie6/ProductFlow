from __future__ import annotations

from collections.abc import Iterable
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.launch_kit.payloads import CategoryPlaybookPayload
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import CategoryPlaybook

STARTER_CATEGORY_PLAYBOOKS: dict[str, dict[str, Any]] = {
    "fashion": {
        "schema_version": 1,
        "buyer_objections": ["size fit", "fabric quality", "real color versus photo"],
        "required_visual_proof": ["front/back view", "fabric close-up", "size/fit note"],
        "risky_claims": ["guaranteed slimming", "luxury brand implication"],
        "suggested_image_sequence": ["main outfit image", "detail image", "size guidance", "styling/use case"],
        "content_tone": ["trendy", "clear sizing", "practical"],
        "platform_notes": {
            "shopee": "Lead with searchable material/style words and size confidence.",
            "tiktok_shop": "Lead with use case, movement, and quick styling hook.",
        },
    },
    "beauty": {
        "schema_version": 1,
        "buyer_objections": ["skin suitability", "ingredient safety", "authenticity"],
        "required_visual_proof": ["ingredient/label view", "texture/result context", "usage step"],
        "risky_claims": ["medical cure", "guaranteed whitening", "permanent result"],
        "suggested_image_sequence": ["main product", "ingredient proof", "how to use", "expected result context"],
        "content_tone": ["trustworthy", "gentle", "specific"],
        "platform_notes": {
            "shopee": "Make capacity, ingredient, and suitability clear.",
            "tiktok_shop": "Use a problem/solution hook without medical guarantees.",
        },
    },
    "electronics_accessories": {
        "schema_version": 1,
        "buyer_objections": ["compatibility", "durability", "charging/speed claim"],
        "required_visual_proof": ["compatibility list", "port/detail close-up", "usage scenario"],
        "risky_claims": ["unsafe fast-charging guarantee", "unverified certification"],
        "suggested_image_sequence": ["main accessory", "compatibility", "feature proof", "use case"],
        "content_tone": ["precise", "spec-led", "reassuring"],
        "platform_notes": {
            "shopee": "Put model compatibility and specs early.",
            "tiktok_shop": "Show the before/after convenience quickly.",
        },
    },
    "home_goods": {
        "schema_version": 1,
        "buyer_objections": ["size", "material", "cleaning/maintenance"],
        "required_visual_proof": ["scale in room", "material/detail", "before-after organization"],
        "risky_claims": ["unverified safety claim", "unrealistic capacity"],
        "suggested_image_sequence": ["room/use main image", "dimensions", "material", "storage/result"],
        "content_tone": ["practical", "warm", "space-saving"],
        "platform_notes": {
            "shopee": "Prioritize dimensions and material to reduce returns.",
            "tiktok_shop": "Show the satisfying use/result moment.",
        },
    },
    "food": {
        "schema_version": 1,
        "buyer_objections": ["expiry", "ingredients", "origin", "shipping freshness"],
        "required_visual_proof": ["packaging/expiry", "ingredient label", "serving/use suggestion"],
        "risky_claims": ["medical health claim", "weight-loss guarantee"],
        "suggested_image_sequence": ["main pack", "label/expiry", "serving idea", "shipping/storage note"],
        "content_tone": ["clear", "safe", "appetizing"],
        "platform_notes": {
            "shopee": "Make expiry, origin, and quantity obvious.",
            "tiktok_shop": "Lead with taste/use moment while keeping safety claims grounded.",
        },
    },
    "other": {
        "schema_version": 1,
        "buyer_objections": ["what it is", "who it is for", "why trust this listing"],
        "required_visual_proof": ["clear main image", "feature/detail", "use case"],
        "risky_claims": ["unsupported performance claim"],
        "suggested_image_sequence": ["main product", "benefit", "proof/detail", "use case"],
        "content_tone": ["clear", "seller-practical", "specific"],
        "platform_notes": {
            "shopee": "Make searchable nouns and concrete attributes clear.",
            "tiktok_shop": "Lead with a concrete buyer problem or use case.",
        },
    },
}

CATEGORY_DISPLAY_NAMES = {
    "fashion": "Fashion",
    "beauty": "Cosmetics / Beauty",
    "electronics_accessories": "Electronics Accessories",
    "home_goods": "Home Goods",
    "food": "Food",
    "other": "Other / Custom",
}


def validate_category_playbook_payload(payload: dict[str, Any]) -> CategoryPlaybookPayload:
    try:
        return CategoryPlaybookPayload.model_validate(payload)
    except ValueError as exc:
        raise BusinessValidationError("Category playbook payload is invalid") from exc


def starter_playbook_rows() -> Iterable[CategoryPlaybook]:
    for key, payload in STARTER_CATEGORY_PLAYBOOKS.items():
        yield CategoryPlaybook(
            key=key,
            display_name=CATEGORY_DISPLAY_NAMES[key],
            schema_version=payload["schema_version"],
            playbook_json=validate_category_playbook_payload(payload).model_dump(mode="json"),
            active=True,
        )


def ensure_starter_category_playbooks(session: Session) -> None:
    existing_keys = set(session.scalars(select(CategoryPlaybook.key)).all())
    for row in starter_playbook_rows():
        if row.key not in existing_keys:
            session.add(row)
    session.commit()


def get_active_category_playbook(session: Session, key: str) -> CategoryPlaybook:
    playbook = session.scalar(
        select(CategoryPlaybook).where(CategoryPlaybook.key == key, CategoryPlaybook.active.is_(True))
    )
    if playbook is None:
        raise NotFoundError("Category playbook does not exist")
    validate_category_playbook_payload(playbook.playbook_json)
    return playbook
