"""商品创建 intake 合同与幂等 request hash。intake 一旦写入即不可变。"""

from __future__ import annotations

import hashlib
import json
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, StringConstraints, ValidationError, model_validator

from productflow_backend.application.delivery_renditions.presets import get_delivery_preset
from productflow_backend.domain.artifact_contracts import (
    PRODUCT_INTAKE_MAX_IMAGES_PER_TYPE,
    PRODUCT_INTAKE_MAX_REFERENCE_ASSETS,
    PRODUCT_INTAKE_MAX_TOTAL_IMAGES,
    PRODUCT_INTAKE_MIN_IMAGE_TYPES,
    PRODUCT_INTAKE_MIN_IMAGES_PER_TYPE,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.domain.image_specs import DeliverySpec
from productflow_backend.domain.image_type_catalog import (
    AGENT_PRODUCT_IMAGE_TYPE_CATALOG,
    AgentProductImageTypeKey,
)

AGENT_PRODUCT_SELECTION_SCHEMA_VERSION = 1
WORKFLOW_INTAKE_SCHEMA_VERSION = 1
AGENT_PRODUCT_DEFAULT_IMAGE_QUANTITY = 2
AGENT_PRODUCT_MAX_IDEMPOTENCY_KEY_BYTES = 200
AGENT_PRODUCT_ALLOWED_IMAGE_MIME_TYPES = (
    "image/png",
    "image/jpeg",
    "image/webp",
)

ReferenceAssetId = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=36)]
DeliveryPresetKey = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=80)]


class StrictIntakeModel(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)


class AgentProductImageTypeSelection(StrictIntakeModel):
    key: AgentProductImageTypeKey
    quantity: int = Field(
        ge=PRODUCT_INTAKE_MIN_IMAGES_PER_TYPE,
        le=PRODUCT_INTAKE_MAX_IMAGES_PER_TYPE,
    )
    order: int = Field(ge=0)


class AgentProductSelectionV1(StrictIntakeModel):
    schema_version: Literal[1]
    image_types: list[AgentProductImageTypeSelection] = Field(
        min_length=PRODUCT_INTAKE_MIN_IMAGE_TYPES,
        max_length=len(AGENT_PRODUCT_IMAGE_TYPE_CATALOG),
    )
    delivery_preset_key: DeliveryPresetKey | None = Field(default=None, exclude_if=lambda value: value is None)

    @model_validator(mode="after")
    def validate_image_types(self) -> AgentProductSelectionV1:
        keys = [item.key for item in self.image_types]
        if len(keys) != len(set(keys)):
            raise ValueError("图片类型不能重复")
        if [item.order for item in self.image_types] != list(range(len(self.image_types))):
            raise ValueError("图片类型 order 必须按数组顺序从 0 连续递增")
        if sum(item.quantity for item in self.image_types) > PRODUCT_INTAKE_MAX_TOTAL_IMAGES:
            raise ValueError(f"图片生成总数不能超过 {PRODUCT_INTAKE_MAX_TOTAL_IMAGES}")
        if self.delivery_preset_key is not None:
            _delivery_preset_spec_or_value_error(self.delivery_preset_key)
        return self


class WorkflowIntakeV1(StrictIntakeModel):
    schema_version: Literal[1]
    image_types: list[AgentProductImageTypeSelection] = Field(
        min_length=PRODUCT_INTAKE_MIN_IMAGE_TYPES,
        max_length=len(AGENT_PRODUCT_IMAGE_TYPE_CATALOG),
    )
    reference_asset_ids: list[ReferenceAssetId] = Field(
        min_length=1,
        max_length=PRODUCT_INTAKE_MAX_REFERENCE_ASSETS,
    )
    delivery_preset_key: DeliveryPresetKey | None = Field(default=None, exclude_if=lambda value: value is None)
    delivery_spec: DeliverySpec | None = Field(default=None, exclude_if=lambda value: value is None)

    @model_validator(mode="after")
    def validate_intake(self) -> WorkflowIntakeV1:
        AgentProductSelectionV1(
            schema_version=AGENT_PRODUCT_SELECTION_SCHEMA_VERSION,
            image_types=self.image_types,
        )
        if len(self.reference_asset_ids) != len(set(self.reference_asset_ids)):
            raise ValueError("参考图资产不能重复")
        if (self.delivery_preset_key is None) != (self.delivery_spec is None):
            raise ValueError("delivery_preset_key 与 delivery_spec 必须同时存在或同时省略")
        return self


def _delivery_preset_spec_or_value_error(key: str) -> DeliverySpec:
    try:
        return get_delivery_preset(key).spec
    except NotFoundError as exc:
        raise ValueError(f"未知 DeliverySpec 预设: {key}") from exc


def delivery_preset_spec_for_key(key: str | None) -> DeliverySpec | None:
    """Resolve a current platform preset for a new intake or direct-create command."""
    if key is None:
        return None
    try:
        return get_delivery_preset(key).spec
    except NotFoundError as exc:
        raise BusinessValidationError(f"未知 DeliverySpec 预设: {key}") from exc


def workflow_intake_from_selection(
    selection: AgentProductSelectionV1,
    *,
    reference_asset_ids: list[str],
) -> WorkflowIntakeV1:
    return WorkflowIntakeV1(
        schema_version=WORKFLOW_INTAKE_SCHEMA_VERSION,
        image_types=selection.image_types,
        reference_asset_ids=list(reference_asset_ids),
        delivery_preset_key=selection.delivery_preset_key,
        delivery_spec=delivery_preset_spec_for_key(selection.delivery_preset_key),
    )


def workflow_intake_payload(intake: WorkflowIntakeV1) -> dict[str, object]:
    """Serialize intake compatibly while retaining null-valued fields inside a selected spec snapshot."""
    payload = intake.model_dump(mode="json", exclude_none=True)
    if intake.delivery_spec is not None:
        payload["delivery_spec"] = intake.delivery_spec.model_dump(mode="json")
    return payload


def parse_agent_product_selection(raw_json: str) -> AgentProductSelectionV1:
    try:
        return AgentProductSelectionV1.model_validate_json(raw_json)
    except ValidationError as exc:
        raise BusinessValidationError("图片类型选择不符合 AgentProductSelectionV1") from exc


def parse_workflow_intake(
    *,
    schema_version: int | None,
    payload: dict[str, object] | None,
) -> WorkflowIntakeV1 | None:
    if schema_version is None and payload is None:
        return None
    if schema_version != WORKFLOW_INTAKE_SCHEMA_VERSION or payload is None:
        raise ConflictError("商品 intake 缺失或版本不受支持")
    try:
        return WorkflowIntakeV1.model_validate(payload)
    except ValidationError as exc:
        raise ConflictError("商品 intake 不符合 schema version 1") from exc


def parse_product_intake(product) -> WorkflowIntakeV1 | None:
    return parse_workflow_intake(
        schema_version=product.intake_schema_version,
        payload=product.intake_json,
    )


def write_product_intake(product, intake: WorkflowIntakeV1) -> None:
    product.intake_schema_version = WORKFLOW_INTAKE_SCHEMA_VERSION
    product.intake_json = workflow_intake_payload(intake)


def normalize_agent_product_idempotency_key(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("Idempotency-Key 不能为空")
    if len(normalized.encode("utf-8")) > AGENT_PRODUCT_MAX_IDEMPOTENCY_KEY_BYTES:
        raise BusinessValidationError(
            f"Idempotency-Key 不能超过 {AGENT_PRODUCT_MAX_IDEMPOTENCY_KEY_BYTES} bytes"
        )
    return normalized


def agent_product_workspace_request_hash(
    *,
    normalized_product_name: str,
    selection: AgentProductSelectionV1,
    image_uploads: list[tuple[bytes, str, str]],
    agent_session_id: str | None = None,
) -> str:
    """一次性创建商品工作区的 request hash；与 idempotency key 共同定义身份。"""
    payload = {
        "product_name": normalized_product_name,
        "selection": selection.model_dump(mode="json", exclude_none=True),
        "images": [
            {
                "order": order,
                "filename": filename,
                "mime_type": mime_type,
                "sha256": hashlib.sha256(content).hexdigest(),
            }
            for order, (content, filename, mime_type) in enumerate(image_uploads)
        ],
    }
    if agent_session_id is not None:
        payload["agent_session_id"] = agent_session_id
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def agent_product_draft_workspace_request_hash(
    *,
    normalized_product_name: str,
    agent_session_id: str | None = None,
) -> str:
    """草稿工作区 hash。key 复用但 hash 不同必须 conflict。"""
    payload = {
        "request_kind": "agent_product_draft_workspace_v1",
        "product_name": normalized_product_name,
    }
    if agent_session_id is not None:
        payload["agent_session_id"] = agent_session_id
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def agent_workbench_attach_request_hash(
    *,
    product_id: str,
    agent_session_id: str | None = None,
) -> str:
    """给已有 live graph 商品挂工作区的 hash。"""
    payload: dict[str, object] = {
        "request_kind": "ensure_agent_workbench_v1",
        "product_id": product_id,
    }
    if agent_session_id is not None:
        payload["agent_session_id"] = agent_session_id
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def agent_product_intake_request_hash(
    *,
    selection: AgentProductSelectionV1,
    image_uploads: list[tuple[bytes, str, str]],
) -> str:
    """finalize intake 的 hash。intake 写入后不可变。"""
    payload = {
        "request_kind": "agent_product_intake_finalization_v1",
        "selection": selection.model_dump(mode="json", exclude_none=True),
        "images": [
            {
                "order": order,
                "filename": filename,
                "mime_type": mime_type,
                "sha256": hashlib.sha256(content).hexdigest(),
            }
            for order, (content, filename, mime_type) in enumerate(image_uploads)
        ],
    }
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def agent_product_intake_from_assets_request_hash(
    *,
    selection: AgentProductSelectionV1,
    reference_asset_ids: list[str],
) -> str:
    """用已有资产 finalize intake 的 hash。"""
    payload = {
        "request_kind": "agent_product_intake_from_assets_v1",
        "selection": selection.model_dump(mode="json", exclude_none=True),
        "reference_asset_ids": list(reference_asset_ids),
    }
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def agent_product_image_type_catalog_json() -> list[dict[str, object]]:
    return [
        {
            "key": item.key,
            "title": item.title,
            "description": item.description,
            "order": item.order,
        }
        for item in AGENT_PRODUCT_IMAGE_TYPE_CATALOG
    ]


__all__ = [
    "AGENT_PRODUCT_ALLOWED_IMAGE_MIME_TYPES",
    "AGENT_PRODUCT_DEFAULT_IMAGE_QUANTITY",
    "AGENT_PRODUCT_MAX_IDEMPOTENCY_KEY_BYTES",
    "AGENT_PRODUCT_SELECTION_SCHEMA_VERSION",
    "AgentProductImageTypeSelection",
    "AgentProductSelectionV1",
    "DeliveryPresetKey",
    "WORKFLOW_INTAKE_SCHEMA_VERSION",
    "WorkflowIntakeV1",
    "agent_product_image_type_catalog_json",
    "agent_product_draft_workspace_request_hash",
    "agent_product_intake_from_assets_request_hash",
    "agent_product_intake_request_hash",
    "agent_product_workspace_request_hash",
    "agent_workbench_attach_request_hash",
    "delivery_preset_spec_for_key",
    "normalize_agent_product_idempotency_key",
    "parse_agent_product_selection",
    "parse_product_intake",
    "parse_workflow_intake",
    "workflow_intake_from_selection",
    "workflow_intake_payload",
    "write_product_intake",
]
