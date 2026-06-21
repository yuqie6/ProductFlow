from __future__ import annotations

from enum import StrEnum


class LaunchKitStatus(StrEnum):
    """Seller-facing LaunchKit lifecycle, separate from durable worker task state."""

    DRAFT = "draft"
    GENERATING = "generating"
    READY = "ready"
    FAILED = "failed"
    ARCHIVED = "archived"


class LaunchKitProgressStage(StrEnum):
    """Detailed generation progress. Task lifecycle status stays boring."""

    EXTRACTING_FACTS = "extracting_facts"
    APPLYING_PLAYBOOK = "applying_playbook"
    APPLYING_STORE_PROFILE = "applying_store_profile"
    GENERATING_ANGLES = "generating_angles"
    GENERATING_COPY = "generating_copy"
    PLANNING_IMAGES = "planning_images"
    SCORING = "scoring"
    EXPORTING_OPTIONAL_SNAPSHOT = "exporting_optional_snapshot"


class LaunchKitPlatform(StrEnum):
    SHOPEE = "shopee"
    TIKTOK_SHOP = "tiktok_shop"
    BOTH = "both"


class LaunchKitVariantKind(StrEnum):
    TITLE = "title"
    DESCRIPTION = "description"
    IMAGE_PLAN = "image_plan"
    HASHTAG = "hashtag"
    HOOK = "hook"
    FULL_KIT = "full_kit"


class LaunchKitExportType(StrEnum):
    MARKDOWN = "markdown"
    IMAGES_ZIP = "images_zip"
    PLATFORM_TEXT = "platform_text"
    CHECKLIST = "checklist"


class LaunchKitExportStatus(StrEnum):
    READY = "ready"
    FAILED = "failed"


class LaunchKitFailureCategory(StrEnum):
    EMPTY_RESPONSE = "empty_response"
    MALFORMED_JSON = "malformed_json"
    MISSING_SECTIONS = "missing_sections"
    UNSAFE_CLAIM = "unsafe_claim"
    REFUSAL = "refusal"
    RATE_LIMIT = "rate_limit"
    TIMEOUT = "timeout"
    QUEUE_UNAVAILABLE = "queue_unavailable"
    UNKNOWN = "unknown"
