from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, Field, field_validator

SCHEMA_VERSION = 1


class VersionedPayload(BaseModel):
    schema_version: Literal[1] = SCHEMA_VERSION


class SourceReferencePayload(VersionedPayload):
    product_name: str | None = None
    pasted_reference_text: str | None = None
    reference_urls: list[str] = Field(default_factory=list)
    source_asset_ids: list[str] = Field(default_factory=list)
    notes: str | None = None


class GeneratedSummaryPayload(VersionedPayload):
    product_facts: dict[str, Any] = Field(default_factory=dict)
    missing_facts: list[str] = Field(default_factory=list)
    buyer_objections: list[str] = Field(default_factory=list)
    risky_claims: list[str] = Field(default_factory=list)


class SelectedAnglePayload(VersionedPayload):
    key: str | None = None
    label: str | None = None
    why_it_might_work: str | None = None
    buyer_emotion: str | None = None
    platform_fit: str | None = None
    risk: str | None = None


class ExportSnapshotPayload(VersionedPayload):
    selected_variant_ids: list[str] = Field(default_factory=list)
    generated_file_ids: list[str] = Field(default_factory=list)
    checklist_items: list[str] = Field(default_factory=list)


class SellerFeedbackPayload(VersionedPayload):
    used: bool | None = None
    edited: bool | None = None
    would_reuse: bool | None = None
    would_pay: bool | None = None
    notes: str | None = None
    metrics: dict[str, Any] = Field(default_factory=dict)


class VariantContentPayload(VersionedPayload):
    content: dict[str, Any] = Field(default_factory=dict)
    why_it_should_convert: str | None = None
    buyer_objection_addressed: str | None = None
    platform_fit: str | None = None
    risk: str | None = None


class LaunchQualityScorePayload(VersionedPayload):
    overall: int = Field(default=0, ge=0, le=100)
    missing_facts: int = Field(default=0, ge=0, le=100)
    title_strength: int = Field(default=0, ge=0, le=100)
    image_coverage: int = Field(default=0, ge=0, le=100)
    claim_risk: int = Field(default=0, ge=0, le=100)
    buyer_objection_coverage: int = Field(default=0, ge=0, le=100)
    platform_fit: int = Field(default=0, ge=0, le=100)
    generic_wording_risk: int = Field(default=0, ge=0, le=100)
    warnings: list[str] = Field(default_factory=list)


class CategoryPlaybookPayload(VersionedPayload):
    buyer_objections: list[str] = Field(default_factory=list, min_length=1)
    required_visual_proof: list[str] = Field(default_factory=list, min_length=1)
    risky_claims: list[str] = Field(default_factory=list)
    suggested_image_sequence: list[str] = Field(default_factory=list, min_length=1)
    content_tone: list[str] = Field(default_factory=list, min_length=1)
    platform_notes: dict[str, str] = Field(default_factory=dict)

    @field_validator("platform_notes")
    @classmethod
    def platform_notes_include_known_platforms(cls, value: dict[str, str]) -> dict[str, str]:
        unknown = set(value) - {"shopee", "tiktok_shop", "both"}
        if unknown:
            raise ValueError(f"unknown platform notes: {', '.join(sorted(unknown))}")
        return value


class StoreProfilePayload(VersionedPayload):
    store_tone: str | None = None
    target_buyer: str | None = None
    brand_rules: list[str] = Field(default_factory=list)
    color_logo_notes: str | None = None
    platform_preferences: dict[str, Any] = Field(default_factory=dict)
    default_shipping_promo_notes: str | None = None
    prohibited_claims: list[str] = Field(default_factory=list)
