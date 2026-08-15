from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, StringConstraints, ValidationError, model_validator

from productflow_backend.application.workflow_drafts.contracts import (
    WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE,
    WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS,
    WORKFLOW_DRAFT_MAX_TOTAL_IMAGES,
    WORKFLOW_DRAFT_MIN_IMAGE_TYPES,
    WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError

AGENT_PRODUCT_SELECTION_SCHEMA_VERSION = 1
WORKFLOW_INTAKE_SCHEMA_VERSION = 1
AGENT_PRODUCT_DEFAULT_IMAGE_QUANTITY = 2
AGENT_PRODUCT_MAX_IDEMPOTENCY_KEY_BYTES = 200
AGENT_PRODUCT_ALLOWED_IMAGE_MIME_TYPES = (
    "image/png",
    "image/jpeg",
    "image/webp",
)

AgentProductImageTypeKey = Literal[
    "hero",
    "selling_point",
    "scene",
    "detail",
    "sku",
    "dimensions",
    "specifications",
    "after_sales",
    "brand_story",
    "precautions",
    "certification",
    "faq",
    "factory",
    "packaging",
    "shipping",
]
ReferenceAssetId = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=36)]


@dataclass(frozen=True, slots=True)
class AgentProductImageTypeOption:
    key: AgentProductImageTypeKey
    title: str
    description: str
    order: int


AGENT_PRODUCT_IMAGE_TYPE_CATALOG = (
    AgentProductImageTypeOption("hero", "首屏海报图", "快速抓住用户注意力，传递产品核心定位", 0),
    AgentProductImageTypeOption("selling_point", "核心卖点图", "直击产品核心优势，突出亮点", 1),
    AgentProductImageTypeOption("scene", "场景展示图", "展示真实使用场景，直观感受产品价值", 2),
    AgentProductImageTypeOption("detail", "细节展示图", "放大核心细节，呈现工艺品质与精致做工", 3),
    AgentProductImageTypeOption("sku", "SKU 展示图", "清晰展示规格、颜色、款式和型号", 4),
    AgentProductImageTypeOption("dimensions", "尺寸图", "标注产品尺寸数据", 5),
    AgentProductImageTypeOption("specifications", "规格参数图", "呈现规格参数，清晰展示产品硬核信息", 6),
    AgentProductImageTypeOption("after_sales", "售后保障图", "展示售后政策、质保、退换与运费说明", 7),
    AgentProductImageTypeOption("brand_story", "品牌故事图", "讲述品牌理念与历程", 8),
    AgentProductImageTypeOption("precautions", "注意事项图", "明确使用与保养注意事项", 9),
    AgentProductImageTypeOption("certification", "资质认证图", "展示用户已提供的权威资质认证", 10),
    AgentProductImageTypeOption("faq", "常见问题图", "解答常见疑问", 11),
    AgentProductImageTypeOption("factory", "工厂实力图", "展示用户已提供的生产实力与工艺信息", 12),
    AgentProductImageTypeOption("packaging", "包装展示图", "展示产品包装全貌", 13),
    AgentProductImageTypeOption("shipping", "发货物流图", "展示发货流程与物流时效", 14),
)
AGENT_PRODUCT_IMAGE_TYPE_KEYS = frozenset(option.key for option in AGENT_PRODUCT_IMAGE_TYPE_CATALOG)


class StrictIntakeModel(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)


class AgentProductImageTypeSelection(StrictIntakeModel):
    key: AgentProductImageTypeKey
    quantity: int = Field(
        ge=WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE,
        le=WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE,
    )
    order: int = Field(ge=0)


class AgentProductSelectionV1(StrictIntakeModel):
    schema_version: Literal[1]
    image_types: list[AgentProductImageTypeSelection] = Field(
        min_length=WORKFLOW_DRAFT_MIN_IMAGE_TYPES,
        max_length=len(AGENT_PRODUCT_IMAGE_TYPE_CATALOG),
    )

    @model_validator(mode="after")
    def validate_image_types(self) -> AgentProductSelectionV1:
        keys = [item.key for item in self.image_types]
        if len(keys) != len(set(keys)):
            raise ValueError("图片类型不能重复")
        if [item.order for item in self.image_types] != list(range(len(self.image_types))):
            raise ValueError("图片类型 order 必须按数组顺序从 0 连续递增")
        if sum(item.quantity for item in self.image_types) > WORKFLOW_DRAFT_MAX_TOTAL_IMAGES:
            raise ValueError(f"图片生成总数不能超过 {WORKFLOW_DRAFT_MAX_TOTAL_IMAGES}")
        return self


class WorkflowIntakeV1(StrictIntakeModel):
    schema_version: Literal[1]
    image_types: list[AgentProductImageTypeSelection] = Field(
        min_length=WORKFLOW_DRAFT_MIN_IMAGE_TYPES,
        max_length=len(AGENT_PRODUCT_IMAGE_TYPE_CATALOG),
    )
    reference_asset_ids: list[ReferenceAssetId] = Field(
        min_length=1,
        max_length=WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS,
    )

    @model_validator(mode="after")
    def validate_intake(self) -> WorkflowIntakeV1:
        AgentProductSelectionV1(
            schema_version=AGENT_PRODUCT_SELECTION_SCHEMA_VERSION,
            image_types=self.image_types,
        )
        if len(self.reference_asset_ids) != len(set(self.reference_asset_ids)):
            raise ValueError("参考图资产不能重复")
        return self


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
        raise ConflictError("WorkflowDraft intake 缺失或版本不受支持")
    try:
        return WorkflowIntakeV1.model_validate(payload)
    except ValidationError as exc:
        raise ConflictError("WorkflowDraft intake 不符合 schema version 1") from exc


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
) -> str:
    payload = {
        "product_name": normalized_product_name,
        "selection": selection.model_dump(mode="json"),
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


def agent_product_draft_workspace_request_hash(*, normalized_product_name: str) -> str:
    payload = {
        "request_kind": "agent_product_draft_workspace_v1",
        "product_name": normalized_product_name,
    }
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def agent_product_intake_request_hash(
    *,
    selection: AgentProductSelectionV1,
    image_uploads: list[tuple[bytes, str, str]],
) -> str:
    payload = {
        "request_kind": "agent_product_intake_finalization_v1",
        "selection": selection.model_dump(mode="json"),
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


__all__ = [
    "AGENT_PRODUCT_ALLOWED_IMAGE_MIME_TYPES",
    "AGENT_PRODUCT_DEFAULT_IMAGE_QUANTITY",
    "AGENT_PRODUCT_IMAGE_TYPE_CATALOG",
    "AGENT_PRODUCT_MAX_IDEMPOTENCY_KEY_BYTES",
    "AGENT_PRODUCT_SELECTION_SCHEMA_VERSION",
    "AgentProductImageTypeOption",
    "AgentProductImageTypeSelection",
    "AgentProductSelectionV1",
    "WORKFLOW_INTAKE_SCHEMA_VERSION",
    "WorkflowIntakeV1",
    "agent_product_draft_workspace_request_hash",
    "agent_product_intake_request_hash",
    "agent_product_workspace_request_hash",
    "normalize_agent_product_idempotency_key",
    "parse_agent_product_selection",
    "parse_workflow_intake",
]
