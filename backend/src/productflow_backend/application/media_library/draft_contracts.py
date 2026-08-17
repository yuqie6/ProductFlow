from __future__ import annotations

import json
import re
from hashlib import sha256
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator

from productflow_backend.application.media_library.organization import normalize_media_library_name

LIBRARY_ORGANIZATION_DRAFT_SCHEMA_VERSION = 1
MAX_LIBRARY_ORGANIZATION_ASSETS = 100
MAX_LIBRARY_ORGANIZATION_OPERATIONS = 256
MAX_LIBRARY_ORGANIZATION_PAYLOAD_BYTES = 256 * 1024
MAX_LIBRARY_ORGANIZATION_REASON_LENGTH = 500
MAX_LIBRARY_ORGANIZATION_SUMMARY_LENGTH = 500
_WHITESPACE = re.compile(r"\s+")


def _single_line_text(value: str, *, label: str, maximum: int) -> str:
    normalized = _WHITESPACE.sub(" ", value.strip())
    if not normalized:
        raise ValueError(f"{label}不能为空")
    if len(normalized) > maximum:
        raise ValueError(f"{label}不能超过 {maximum} 个字符")
    return normalized


def _display_name(value: str) -> str:
    return _single_line_text(value, label="素材名称", maximum=255)


def _tag_names(values: list[str]) -> list[str]:
    normalized: list[str] = []
    seen: set[str] = set()
    for value in values:
        try:
            display_name = normalize_media_library_name(value, kind="tag")
        except Exception as exc:
            raise ValueError(str(exc)) from exc
        key = display_name.casefold()
        if key not in seen:
            seen.add(key)
            normalized.append(display_name)
    return normalized


class LibraryAssetBeforeV1(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    revision: int = Field(ge=1)
    display_name: str = Field(min_length=1, max_length=255)
    folder_id: str | None = Field(default=None, max_length=36)
    tag_names: list[str] = Field(default_factory=list, max_length=40)
    is_archived: bool

    _normalize_display_name = field_validator("display_name")(_display_name)
    _normalize_tag_names = field_validator("tag_names")(_tag_names)


class LibraryRenameTargetV1(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    display_name: str = Field(min_length=1, max_length=255)

    _normalize_display_name = field_validator("display_name")(_display_name)


class LibraryMoveTargetV1(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    folder_id: str | None = Field(default=None, max_length=36)


class LibraryTagsTargetV1(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    tag_names: list[str] = Field(default_factory=list, max_length=40)

    _normalize_tag_names = field_validator("tag_names")(_tag_names)


class LibraryArchiveTargetV1(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    is_archived: Literal[True] = True


class LibraryRestoreTargetV1(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    is_archived: Literal[False] = False


class _LibraryOrganizationOperationBase(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    asset_id: str = Field(min_length=1, max_length=36)
    expected_revision: int = Field(ge=1)
    before: LibraryAssetBeforeV1
    reason: str = Field(min_length=1, max_length=MAX_LIBRARY_ORGANIZATION_REASON_LENGTH)

    _normalize_reason = field_validator("reason")(
        lambda value: _single_line_text(
            value,
            label="整理原因",
            maximum=MAX_LIBRARY_ORGANIZATION_REASON_LENGTH,
        )
    )

    @model_validator(mode="after")
    def revision_matches_before(self):
        if self.before.revision != self.expected_revision:
            raise ValueError("before.revision 必须等于 expected_revision")
        return self


class LibraryRenameOperationV1(_LibraryOrganizationOperationBase):
    operation: Literal["rename"]
    target: LibraryRenameTargetV1


class LibraryMoveOperationV1(_LibraryOrganizationOperationBase):
    operation: Literal["move"]
    target: LibraryMoveTargetV1


class LibrarySetTagsOperationV1(_LibraryOrganizationOperationBase):
    operation: Literal["set_tags"]
    target: LibraryTagsTargetV1


class LibraryArchiveOperationV1(_LibraryOrganizationOperationBase):
    operation: Literal["archive"]
    target: LibraryArchiveTargetV1


class LibraryRestoreOperationV1(_LibraryOrganizationOperationBase):
    operation: Literal["restore"]
    target: LibraryRestoreTargetV1


LibraryOrganizationOperationV1 = Annotated[
    LibraryRenameOperationV1
    | LibraryMoveOperationV1
    | LibrarySetTagsOperationV1
    | LibraryArchiveOperationV1
    | LibraryRestoreOperationV1,
    Field(discriminator="operation"),
]


class LibraryOrganizationDraftPayloadV1(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    schema_version: Literal[1] = LIBRARY_ORGANIZATION_DRAFT_SCHEMA_VERSION
    confirmation_summary: str = Field(min_length=1, max_length=MAX_LIBRARY_ORGANIZATION_SUMMARY_LENGTH)
    operations: list[LibraryOrganizationOperationV1] = Field(
        min_length=1,
        max_length=MAX_LIBRARY_ORGANIZATION_OPERATIONS,
    )

    _normalize_summary = field_validator("confirmation_summary")(
        lambda value: _single_line_text(
            value,
            label="整理确认摘要",
            maximum=MAX_LIBRARY_ORGANIZATION_SUMMARY_LENGTH,
        )
    )

    @model_validator(mode="after")
    def validate_limits_and_unique_assets(self):
        asset_ids = [operation.asset_id for operation in self.operations]
        if len(set(asset_ids)) != len(asset_ids):
            raise ValueError("同一个素材在一个整理 Draft 中只能出现一次")
        if len(asset_ids) > MAX_LIBRARY_ORGANIZATION_ASSETS:
            raise ValueError(f"一次整理最多包含 {MAX_LIBRARY_ORGANIZATION_ASSETS} 个素材")
        payload_size = len(
            json.dumps(
                self.model_dump(mode="json"),
                ensure_ascii=False,
                sort_keys=True,
                separators=(",", ":"),
            ).encode("utf-8")
        )
        if payload_size > MAX_LIBRARY_ORGANIZATION_PAYLOAD_BYTES:
            raise ValueError("素材整理 Draft 过大")
        return self


def parse_library_organization_draft_payload(payload: dict[str, object]) -> LibraryOrganizationDraftPayloadV1:
    return LibraryOrganizationDraftPayloadV1.model_validate(payload)


def library_organization_draft_payload_hash(payload: LibraryOrganizationDraftPayloadV1) -> str:
    encoded = json.dumps(
        payload.model_dump(mode="json"),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    return sha256(encoded).hexdigest()


def library_organization_draft_schema() -> dict[str, object]:
    return LibraryOrganizationDraftPayloadV1.model_json_schema()


__all__ = [
    "LIBRARY_ORGANIZATION_DRAFT_SCHEMA_VERSION",
    "MAX_LIBRARY_ORGANIZATION_ASSETS",
    "MAX_LIBRARY_ORGANIZATION_OPERATIONS",
    "MAX_LIBRARY_ORGANIZATION_PAYLOAD_BYTES",
    "LibraryAssetBeforeV1",
    "LibraryOrganizationDraftPayloadV1",
    "LibraryOrganizationOperationV1",
    "parse_library_organization_draft_payload",
    "library_organization_draft_payload_hash",
    "library_organization_draft_schema",
]
