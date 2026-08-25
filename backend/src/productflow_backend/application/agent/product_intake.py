"""商品创建 intake 合同与幂等 request hash。intake 一旦写入即不可变。"""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, StringConstraints, ValidationError, model_validator

from productflow_backend.application.delivery_renditions.presets import get_delivery_preset
from productflow_backend.application.workflow_drafts.contracts import (
    WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE,
    WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS,
    WORKFLOW_DRAFT_MAX_TOTAL_IMAGES,
    WORKFLOW_DRAFT_MIN_IMAGE_TYPES,
    WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE,
    DeliverySpec,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError

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
DeliveryPresetKey = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=80)]


@dataclass(frozen=True, slots=True)
class AgentProductImageTypeOption:
    key: AgentProductImageTypeKey
    title: str
    description: str
    order: int


AGENT_PRODUCT_IMAGE_TYPE_CATALOG = (
    AgentProductImageTypeOption("hero", "首屏海报图", "搜索列表首图，商品够大能认", 0),
    AgentProductImageTypeOption("selling_point", "核心卖点图", "详情卖点图，层次清楚，不要空棚贴字", 1),
    AgentProductImageTypeOption("scene", "场景展示图", "使用场景里拍，商品是主角", 2),
    AgentProductImageTypeOption("detail", "细节展示图", "材质和工艺特写", 3),
    AgentProductImageTypeOption("sku", "SKU 展示图", "白底规格对照，方便选款", 4),
    AgentProductImageTypeOption("dimensions", "尺寸图", "尺寸线清晰的信息图", 5),
    AgentProductImageTypeOption("specifications", "规格参数图", "参数对照信息图", 6),
    AgentProductImageTypeOption("after_sales", "售后保障图", "质保退换说明图", 7),
    AgentProductImageTypeOption("brand_story", "品牌故事图", "品牌故事海报", 8),
    AgentProductImageTypeOption("precautions", "注意事项图", "使用保养说明图", 9),
    AgentProductImageTypeOption("certification", "资质认证图", "只用已提供的资质", 10),
    AgentProductImageTypeOption("faq", "常见问题图", "问答说明图", 11),
    AgentProductImageTypeOption("factory", "工厂实力图", "只用已提供的工厂画面", 12),
    AgentProductImageTypeOption("packaging", "包装展示图", "包装全貌", 13),
    AgentProductImageTypeOption("shipping", "发货物流图", "发货物流说明图", 14),
)
LISTING_LOOK_RULE = (
    "做成能点击的商业套图：商品是主角，层次清楚，卖点好读。"
    "不要极简大留白、浅灰空棚、杂志静物；也不要爆炸贴、满屏色块、牛皮癣标签。"
)
LISTING_LOOK_CONTEXT: dict[str, object] = {
    "rule": LISTING_LOOK_RULE,
    "product_share_percent": "55-75",
    "benefit_count": "2-4",
    "source_note_is_product_fact": True,
    "ignore_as_art_direction": ["极简", "浅灰", "静物", "干净", "留白", "高级", "苹果风"],
    "do_not_invert_into": ["爆炸贴", "满屏色块", "牛皮癣标签", "过饱和撞色"],
}
IMAGE_TYPE_GENERATION_JOBS: dict[str, str] = {
    "hero": (
        "搜索列表首图。商品约占画面 55%–75%，一眼能认出货，有类别合适的底和光影。"
        "最多一句超短主利益点。不要浅灰大海把商品挤到角落，也不要贴满角标。"
    ),
    "selling_point": (
        "详情卖点图，不是照片加字幕。抠出商品重新构图。"
        "一个主标题加 2 到 4 条对齐好读的短利益点，色块克制。商品仍是主角。"
        "不要原图贴字，不要大面积留白，也不要爆炸贴墙。"
    ),
    "scene": (
        "使用场景。把商品放进会用到的环境，环境为人服务、商品清晰可辨。"
        "不要空棚静物，也不要把场景堆满杂物，更不要编造资料里没有的生活道具品牌。"
    ),
    "detail": "材质或工艺特写。镜头贴近关键结构，光线强调质感，不要整件商品的远景棚拍。",
    "sku": "规格/颜色/款式对照图。纯色或白底，商品摆正、边缘干净，方便选款，不要装饰性大标题。",
    "dimensions": (
        "尺寸标注信息图。商品在画面中，尺寸线清楚。数字只能来自商品资料；没有数据就画结构关系，不要编造毫米数。"
    ),
    "specifications": "规格参数信息图。用短标签和对照模块呈现资料里已有的参数，不要编造参数。",
    "after_sales": "售后保障说明图。只写资料里有的质保、退换、运费政策，排版清楚，不要编造承诺。",
    "brand_story": "品牌故事海报。只使用资料或参考图里出现的品牌信息，做成可上详情的设计稿，不要空洞鸡汤。",
    "precautions": "使用与保养说明图。条目短、可读，内容来自资料，不要恐吓式极限词。",
    "certification": "资质认证图。只能使用用户提供的证书或标志照片，没有素材就留缺口，不要手绘公章。",
    "faq": "常见问题说明图。问句短、答句短，内容来自资料，不要编造售后话术。",
    "factory": "工厂实力图。只能使用用户提供的产线或厂房照片，没有素材就留缺口，不要生成假车间。",
    "packaging": "包装展示图。看清包装结构与内容物，商品可辨认，不要只拍一个模糊纸箱。",
    "shipping": "发货物流说明图。只写资料里有的发货与时效信息，排版清楚，不要编造快递品牌。",
}
AGENT_PRODUCT_IMAGE_TYPE_KEYS = frozenset(option.key for option in AGENT_PRODUCT_IMAGE_TYPE_CATALOG)
_IMAGE_TYPE_BY_KEY = {option.key: option for option in AGENT_PRODUCT_IMAGE_TYPE_CATALOG}
PHOTOGRAPHY_IMAGE_TYPE_KEYS = frozenset({"hero", "scene", "detail", "sku", "packaging"})
INFOGRAPHIC_IMAGE_TYPE_KEYS = frozenset(
    {
        "selling_point",
        "dimensions",
        "specifications",
        "after_sales",
        "precautions",
        "faq",
        "shipping",
        "brand_story",
    }
)
EVIDENCE_IMAGE_TYPE_KEYS = frozenset({"certification", "factory"})


def image_type_family(key: str) -> str:
    if key in EVIDENCE_IMAGE_TYPE_KEYS:
        return "evidence"
    if key in INFOGRAPHIC_IMAGE_TYPE_KEYS:
        return "infographic"
    return "photography"


def image_type_generation_job(key: str) -> str:
    option = _IMAGE_TYPE_BY_KEY.get(key)
    return IMAGE_TYPE_GENERATION_JOBS.get(key) or (option.description if option else "")


def image_type_prompt_goal(key: str) -> str:
    option = _IMAGE_TYPE_BY_KEY.get(key)
    title = option.title if option else key
    job = image_type_generation_job(key)
    return f"{title}：{job}" if job else title


def agent_product_image_type_option(key: str) -> AgentProductImageTypeOption | None:
    return _IMAGE_TYPE_BY_KEY.get(key)


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
    delivery_preset_key: DeliveryPresetKey | None = Field(default=None, exclude_if=lambda value: value is None)

    @model_validator(mode="after")
    def validate_image_types(self) -> AgentProductSelectionV1:
        keys = [item.key for item in self.image_types]
        if len(keys) != len(set(keys)):
            raise ValueError("图片类型不能重复")
        if [item.order for item in self.image_types] != list(range(len(self.image_types))):
            raise ValueError("图片类型 order 必须按数组顺序从 0 连续递增")
        if sum(item.quantity for item in self.image_types) > WORKFLOW_DRAFT_MAX_TOTAL_IMAGES:
            raise ValueError(f"图片生成总数不能超过 {WORKFLOW_DRAFT_MAX_TOTAL_IMAGES}")
        if self.delivery_preset_key is not None:
            _delivery_preset_spec_or_value_error(self.delivery_preset_key)
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
    "AGENT_PRODUCT_IMAGE_TYPE_CATALOG",
    "AGENT_PRODUCT_MAX_IDEMPOTENCY_KEY_BYTES",
    "AGENT_PRODUCT_SELECTION_SCHEMA_VERSION",
    "AgentProductImageTypeOption",
    "AgentProductImageTypeSelection",
    "AgentProductSelectionV1",
    "DeliveryPresetKey",
    "WORKFLOW_INTAKE_SCHEMA_VERSION",
    "WorkflowIntakeV1",
    "IMAGE_TYPE_GENERATION_JOBS",
    "LISTING_LOOK_CONTEXT",
    "LISTING_LOOK_RULE",
    "agent_product_image_type_catalog_json",
    "agent_product_image_type_option",
    "image_type_family",
    "image_type_generation_job",
    "image_type_prompt_goal",
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
