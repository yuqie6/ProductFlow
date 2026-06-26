from __future__ import annotations

from enum import StrEnum
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator

from productflow_backend.domain.enums import WorkflowNodeType
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.workflow_rules import WorkflowRuleEdge, WorkflowRuleNode, topological_node_ids

TemplateKind = Literal["full_canvas", "node_group"]
SUPPORTED_CANVAS_TEMPLATE_NODE_TYPES = frozenset(
    {
        WorkflowNodeType.PRODUCT_CONTEXT,
        WorkflowNodeType.REFERENCE_IMAGE,
        WorkflowNodeType.COPY_GENERATION,
        WorkflowNodeType.IMAGE_GENERATION,
    }
)
FULL_CANVAS_TEMPLATE_COLUMN_GAP = 380


class CanvasTemplateScenario(StrEnum):
    MAIN_IMAGE = "main_image"
    TAOBAO_MAIN_IMAGE = "taobao_main_image"
    XIAOHONGSHU_IMAGE = "xiaohongshu_image"
    MULTI_ANGLE = "multi_angle"
    SKU_VARIANT = "sku_variant"
    FEATURE_INFOGRAPHIC = "feature_infographic"
    SIZE_SPEC = "size_spec"
    SCALE_REFERENCE = "scale_reference"
    PACKAGE_CHECKLIST = "package_checklist"
    USAGE_STEPS = "usage_steps"
    COMPARISON = "comparison"
    MODEL_LIFESTYLE = "model_lifestyle"
    SCENE_IMAGE = "scene_image"
    DETAIL_MATERIAL = "detail_material"
    CAMPAIGN_PROMOTION = "campaign_promotion"
    SHORT_VIDEO_COVER = "short_video_cover"
    WHITE_BACKGROUND = "white_background"


class CanvasTemplateScenarioMetadata(BaseModel):
    model_config = ConfigDict(frozen=True)

    scenario: CanvasTemplateScenario
    title: str
    description: str
    ecommerce_stage: str
    tags: tuple[str, ...] = ()


class CanvasTemplateReferenceInputHint(BaseModel):
    model_config = ConfigDict(frozen=True)

    node_key: str
    role: str
    label: str
    required: bool = False
    description: str


class CanvasTemplateOutputSlot(BaseModel):
    model_config = ConfigDict(frozen=True)

    node_key: str
    label: str
    description: str


class CanvasTemplateSuggestedConnection(BaseModel):
    model_config = ConfigDict(frozen=True)

    source_node_key: str
    target_node_key: str
    reason: str


class CanvasTemplateDefaultExternalConnection(BaseModel):
    model_config = ConfigDict(frozen=True)

    source: Literal["existing_product_context"]
    target_node_key: str
    label: str
    reason: str


class CanvasTemplateNodeSpec(BaseModel):
    model_config = ConfigDict(frozen=True)

    key: str
    node_type: WorkflowNodeType
    title: str
    position_x: int = 0
    position_y: int = 0
    config_json: dict[str, Any] = Field(default_factory=dict)
    prompt_seed: str | None = None
    instruction_seed: str | None = None
    size: str | None = None
    output_slot_label: str | None = None
    reference_input_hint: str | None = None

    @field_validator("config_json")
    @classmethod
    def validate_config_json(cls, value: dict[str, Any]) -> dict[str, Any]:
        return dict(value)


class CanvasTemplateEdgeSpec(BaseModel):
    model_config = ConfigDict(frozen=True)

    source_node_key: str
    target_node_key: str
    source_handle: str | None = "output"
    target_handle: str | None = "input"


class CanvasTemplate(BaseModel):
    model_config = ConfigDict(frozen=True)

    key: str
    version: int = 1
    kind: TemplateKind
    title: str
    description: str
    source: Literal["builtin", "user"] = "builtin"
    user_template_id: str | None = None
    scenario: CanvasTemplateScenarioMetadata
    nodes: tuple[CanvasTemplateNodeSpec, ...]
    edges: tuple[CanvasTemplateEdgeSpec, ...] = ()
    prompt_seeds: tuple[str, ...] = ()
    instruction_seeds: tuple[str, ...] = ()
    output_slots: tuple[CanvasTemplateOutputSlot, ...] = ()
    reference_input_hints: tuple[CanvasTemplateReferenceInputHint, ...] = ()
    suggested_connections: tuple[CanvasTemplateSuggestedConnection, ...] = ()
    default_external_connections: tuple[CanvasTemplateDefaultExternalConnection, ...] = ()

    @model_validator(mode="after")
    def validate_contract(self) -> CanvasTemplate:
        validate_canvas_template(self)
        return self


def validate_canvas_template(template: CanvasTemplate) -> None:
    if template.version != 1:
        raise BusinessValidationError("画布模板版本必须是 v1")
    if template.kind not in ("full_canvas", "node_group"):
        raise BusinessValidationError("画布模板类型不支持")
    if not template.nodes:
        raise BusinessValidationError("画布模板至少需要一个节点")

    nodes_by_key: dict[str, CanvasTemplateNodeSpec] = {}
    for node in template.nodes:
        if node.key in nodes_by_key:
            raise BusinessValidationError("画布模板节点 key 不能重复")
        nodes_by_key[node.key] = node
        if node.node_type not in SUPPORTED_CANVAS_TEMPLATE_NODE_TYPES:
            raise BusinessValidationError("画布模板包含不支持的节点类型")
        if template.kind == "node_group" and node.node_type == WorkflowNodeType.PRODUCT_CONTEXT:
            raise BusinessValidationError("节点组模板不能包含商品资料节点")
        if node.size is not None and node.node_type != WorkflowNodeType.IMAGE_GENERATION:
            raise BusinessValidationError("只有生图节点可以声明尺寸")

    for edge in template.edges:
        if edge.source_node_key == edge.target_node_key:
            raise BusinessValidationError("画布模板连线不能连接到自身")
        if edge.source_node_key not in nodes_by_key or edge.target_node_key not in nodes_by_key:
            raise BusinessValidationError("画布模板连线引用了不存在的节点")

    _validate_node_reference_items(
        template.output_slots,
        nodes_by_key=nodes_by_key,
        expected_type=WorkflowNodeType.REFERENCE_IMAGE,
        item_name="输出槽",
    )
    _validate_node_reference_items(
        template.reference_input_hints,
        nodes_by_key=nodes_by_key,
        expected_type=WorkflowNodeType.REFERENCE_IMAGE,
        item_name="参考输入提示",
    )
    for connection in template.suggested_connections:
        if connection.source_node_key == connection.target_node_key:
            raise BusinessValidationError("画布模板连接建议不能连接到自身")
        if (
            connection.source_node_key not in nodes_by_key
            or connection.target_node_key not in nodes_by_key
        ):
            raise BusinessValidationError("画布模板连接建议引用了不存在的节点")
    for connection in template.default_external_connections:
        if template.kind != "node_group":
            raise BusinessValidationError("只有节点组模板可以声明默认外部连接")
        if connection.target_node_key not in nodes_by_key:
            raise BusinessValidationError("画布模板默认外部连接引用了不存在的节点")
        if nodes_by_key[connection.target_node_key].node_type not in {
            WorkflowNodeType.COPY_GENERATION,
            WorkflowNodeType.IMAGE_GENERATION,
        }:
            raise BusinessValidationError("画布模板默认外部连接只能接入文案或生图节点")

    topological_node_ids(
        [
            WorkflowRuleNode(
                id=node.key,
                node_type=node.node_type,
                position_x=node.position_x,
                config_json=node.config_json,
            )
            for node in template.nodes
        ],
        [
            WorkflowRuleEdge(
                source_node_id=edge.source_node_key,
                target_node_id=edge.target_node_key,
            )
            for edge in template.edges
        ],
    )


def list_builtin_canvas_templates() -> tuple[CanvasTemplate, ...]:
    return BUILTIN_CANVAS_TEMPLATES


def get_builtin_canvas_template(template_key: str) -> CanvasTemplate:
    for template in BUILTIN_CANVAS_TEMPLATES:
        if template.key == template_key:
            return template
    raise BusinessValidationError("画布模板不存在")


def _validate_node_reference_items(
    items: tuple[CanvasTemplateOutputSlot, ...] | tuple[CanvasTemplateReferenceInputHint, ...],
    *,
    nodes_by_key: dict[str, CanvasTemplateNodeSpec],
    expected_type: WorkflowNodeType,
    item_name: str,
) -> None:
    for item in items:
        node = nodes_by_key.get(item.node_key)
        if node is None:
            raise BusinessValidationError(f"画布模板{item_name}引用了不存在的节点")
        if node.node_type != expected_type:
            raise BusinessValidationError(f"画布模板{item_name}必须引用参考图节点")


def _scenario(
    scenario: CanvasTemplateScenario,
    *,
    title: str,
    description: str,
    ecommerce_stage: str,
    tags: tuple[str, ...],
) -> CanvasTemplateScenarioMetadata:
    return CanvasTemplateScenarioMetadata(
        scenario=scenario,
        title=title,
        description=description,
        ecommerce_stage=ecommerce_stage,
        tags=tags,
    )


def _node(
    key: str,
    node_type: WorkflowNodeType,
    *,
    title: str,
    x: int,
    y: int,
    config_json: dict[str, Any] | None = None,
    prompt_seed: str | None = None,
    instruction_seed: str | None = None,
    size: str | None = None,
    output_slot_label: str | None = None,
    reference_input_hint: str | None = None,
) -> CanvasTemplateNodeSpec:
    config = dict(config_json or {})
    if instruction_seed is not None and "instruction" not in config:
        config["instruction"] = instruction_seed
    if node_type == WorkflowNodeType.COPY_GENERATION:
        config = _copy_node_config(config, instruction_seed=instruction_seed)
    if size is not None and "size" not in config:
        config["size"] = size
    return CanvasTemplateNodeSpec(
        key=key,
        node_type=node_type,
        title=title,
        position_x=x,
        position_y=y,
        config_json=config,
        prompt_seed=prompt_seed,
        instruction_seed=instruction_seed,
        size=size,
        output_slot_label=output_slot_label,
        reference_input_hint=reference_input_hint,
    )


def _copy_node_config(config: dict[str, Any], *, instruction_seed: str | None) -> dict[str, Any]:
    instruction = str(config.get("instruction") or instruction_seed or "")
    output_mode = config.get("output_mode")
    if output_mode not in {"freeform", "blocks", "layout_brief"}:
        output_mode = _infer_copy_output_mode(instruction)
    next_config = {
        **config,
        "version": 2,
        "instruction": instruction,
        "output_mode": output_mode,
    }
    next_config.setdefault("purpose", _infer_copy_purpose(instruction))
    next_config.setdefault("requested_slots", [])
    return next_config


def _infer_copy_output_mode(instruction: str) -> str:
    if any(keyword in instruction for keyword in ("层级", "布局", "留白", "构图", "信息图", "视觉")):
        return "layout_brief"
    if any(keyword in instruction for keyword in ("卖点", "规格", "尺寸", "步骤", "清单", "对比", "标签", "参数")):
        return "blocks"
    return "freeform"


def _infer_copy_purpose(instruction: str) -> str:
    mapping = (
        ("白底", "white_background"),
        ("短视频", "short_video_cover"),
        ("活动", "campaign_promotion"),
        ("优惠", "campaign_promotion"),
        ("对比", "comparison"),
        ("步骤", "usage_steps"),
        ("清单", "package_checklist"),
        ("包装", "package_checklist"),
        ("尺度", "scale_reference"),
        ("尺寸", "size_spec"),
        ("规格", "size_spec"),
        ("卖点", "feature_infographic"),
        ("SKU", "sku_variant"),
        ("场景", "scene_image"),
        ("封面", "content_cover"),
    )
    for keyword, purpose in mapping:
        if keyword in instruction:
            return purpose
    return "ecommerce_copy"


def _edge(source: str, target: str) -> CanvasTemplateEdgeSpec:
    return CanvasTemplateEdgeSpec(source_node_key=source, target_node_key=target)


def _output_slot(node_key: str, label: str, description: str) -> CanvasTemplateOutputSlot:
    return CanvasTemplateOutputSlot(node_key=node_key, label=label, description=description)


def _reference_hint(
    node_key: str,
    *,
    role: str,
    label: str,
    description: str,
    required: bool = False,
) -> CanvasTemplateReferenceInputHint:
    return CanvasTemplateReferenceInputHint(
        node_key=node_key,
        role=role,
        label=label,
        required=required,
        description=description,
    )


def _suggest(source: str, target: str, reason: str) -> CanvasTemplateSuggestedConnection:
    return CanvasTemplateSuggestedConnection(source_node_key=source, target_node_key=target, reason=reason)


def _default_product_connection(target: str) -> CanvasTemplateDefaultExternalConnection:
    return CanvasTemplateDefaultExternalConnection(
        source="existing_product_context",
        target_node_key=target,
        label="自动接商品",
        reason="沿用当前画布的商品资料和商品主图。",
    )


def _instruction_seeds(nodes: tuple[CanvasTemplateNodeSpec, ...]) -> tuple[str, ...]:
    return tuple(node.instruction_seed for node in nodes if node.instruction_seed)


def _spread_full_canvas_columns(nodes: tuple[CanvasTemplateNodeSpec, ...]) -> tuple[CanvasTemplateNodeSpec, ...]:
    # 完整画布模板横向间距更大，避免创建页预览看起来像节点组模板。
    sorted_columns = sorted({node.position_x for node in nodes})
    if len(sorted_columns) <= 1:
        return nodes
    origin_x = sorted_columns[0]
    column_positions = {
        column_x: origin_x + index * FULL_CANVAS_TEMPLATE_COLUMN_GAP
        for index, column_x in enumerate(sorted_columns)
    }
    return tuple(node.model_copy(update={"position_x": column_positions[node.position_x]}) for node in nodes)


def _full_canvas_template(
    *,
    key: str,
    title: str,
    description: str,
    scenario: CanvasTemplateScenarioMetadata,
    nodes: tuple[CanvasTemplateNodeSpec, ...],
    edges: tuple[tuple[str, str], ...],
    output_slots: tuple[CanvasTemplateOutputSlot, ...],
    reference_input_hints: tuple[CanvasTemplateReferenceInputHint, ...] = (),
    suggested_connections: tuple[CanvasTemplateSuggestedConnection, ...] = (),
) -> CanvasTemplate:
    spaced_nodes = _spread_full_canvas_columns(nodes)
    seeds = _instruction_seeds(spaced_nodes)
    return CanvasTemplate(
        key=key,
        kind="full_canvas",
        title=title,
        description=description,
        scenario=scenario,
        nodes=spaced_nodes,
        edges=tuple(_edge(source, target) for source, target in edges),
        prompt_seeds=seeds,
        instruction_seeds=seeds,
        output_slots=output_slots,
        reference_input_hints=reference_input_hints,
        suggested_connections=suggested_connections,
    )


def _node_group_template(
    *,
    key: str,
    title: str,
    description: str,
    scenario: CanvasTemplateScenarioMetadata,
    copy_instruction: str,
    image_instruction: str,
    size: str,
    output_label: str,
    output_description: str,
    reference_label: str,
    reference_description: str,
    reference_role: str,
) -> CanvasTemplate:
    reference_x = 0
    reference_y = 0
    copy_x = 440
    copy_y = 40
    image_x = 880
    image_y = 120
    output_x = 1320
    output_y = 120
    nodes = (
        _node(
            "reference",
            WorkflowNodeType.REFERENCE_IMAGE,
            title=reference_label,
            x=reference_x,
            y=reference_y,
            config_json={"role": reference_role, "label": reference_label},
            reference_input_hint=reference_description,
        ),
        _node(
            "copy",
            WorkflowNodeType.COPY_GENERATION,
            title=title,
            x=copy_x,
            y=copy_y,
            instruction_seed=copy_instruction,
        ),
        _node(
            "image",
            WorkflowNodeType.IMAGE_GENERATION,
            title=f"生成{output_label}",
            x=image_x,
            y=image_y,
            instruction_seed=image_instruction,
            size=size,
        ),
        _node(
            "output",
            WorkflowNodeType.REFERENCE_IMAGE,
            title=output_label,
            x=output_x,
            y=output_y,
            config_json={"role": "output", "label": output_label},
            output_slot_label=output_label,
        ),
    )
    return CanvasTemplate(
        key=key,
        kind="node_group",
        title=title,
        description=description,
        scenario=scenario,
        nodes=nodes,
        edges=(
            _edge("reference", "copy"),
            _edge("reference", "image"),
            _edge("copy", "image"),
            _edge("image", "output"),
        ),
        prompt_seeds=(copy_instruction, image_instruction),
        instruction_seeds=(copy_instruction, image_instruction),
        output_slots=(_output_slot("output", output_label, output_description),),
        reference_input_hints=(
            _reference_hint(
                "reference",
                role=reference_role,
                label=reference_label,
                description=reference_description,
            ),
        ),
        suggested_connections=(
            _suggest("reference", "copy", "参考图为文案提供规格、材质或主体约束。"),
            _suggest("reference", "image", "参考图可作为图片生成输入，保持主体或细节一致。"),
            _suggest("copy", "image", "文案为图片生成提供标题、卖点和视觉重点。"),
            _suggest("image", "output", "图片生成结果写入下游参考图输出槽。"),
        ),
        default_external_connections=(
            _default_product_connection("copy"),
            _default_product_connection("image"),
        ),
    )


_MAIN_IMAGE_COPY = "提炼商品核心卖点，文案适合电商主图，控制为简洁标题和 3 个利益点。"
_MAIN_IMAGE_INSTRUCTION = "生成干净、聚焦商品主体的电商主图，主体清晰，卖点可被视觉表达。"

_TAOBAO_COPY = (
    "提炼适合淘宝主图的核心卖点，突出商品主体、适用人群和购买理由，控制为短标题和 3 个利益点。"
)
_TAOBAO_IMAGE = "生成淘宝主图，1:1 构图，商品主体清晰居中，背景干净，卖点视觉明确，避免复杂文字堆叠。"

_XHS_COPY = "提炼适合小红书笔记封面的内容角度，突出使用体验、场景氛围和真实感，避免硬广语气。"
_XHS_IMAGE = "生成小红书风格竖版图片，画面自然、有生活感，商品清楚可见，适合笔记封面和种草内容。"

_MULTI_ANGLE_COPY = "规划商品多角度展示顺序，覆盖正面、侧面、背面或关键结构，保持说明短而明确。"
_MULTI_ANGLE_IMAGE = "生成同一商品的多角度展示图，主体比例一致，角度清楚，适合详情页轮播和平台图册。"

_SKU_COPY = "围绕当前 SKU 的颜色、规格、容量或组合差异生成简短说明，突出用户选择时最需要比较的信息。"
_SKU_IMAGE = "生成突出 SKU 差异的商品图，保持主体一致，明确展示规格、颜色或组合差别。"

_FEATURE_INFOGRAPHIC_COPY = "提炼 3 到 5 个最影响购买决策的功能卖点，并为每个卖点生成短标签和视觉表达建议。"
_FEATURE_INFOGRAPHIC_IMAGE = "生成商品功能卖点信息图，主体清楚，卖点布局有层级，适合详情页首屏说服。"

_SIZE_SPEC_COPY = "整理商品尺寸、容量、规格、材质参数和注意事项，输出适合规格说明图的结构化短文案。"
_SIZE_SPEC_IMAGE = "生成尺寸/规格说明图，保留商品轮廓和关键标注区域，布局清楚，适合详情页参数说明。"

_SCALE_REFERENCE_COPY = "提炼商品真实尺度、手持/佩戴/桌面参照和使用距离，避免夸大比例。"
_SCALE_REFERENCE_IMAGE = "生成带真实参照物的尺度展示图，让用户直观理解商品大小、厚度或容量。"

_PACKAGE_COPY = "整理包装内容、配件清单、赠品和到手状态，形成适合清单图的短标签。"
_PACKAGE_IMAGE = "生成包装/清单平铺图，清楚展示商品、包装盒、配件和数量，适合详情页到手说明。"

_USAGE_STEPS_COPY = "拆解安装、开箱、佩戴、清洁或使用步骤，控制为 3 到 4 步，每步一句短说明。"
_USAGE_STEPS_IMAGE = "生成使用步骤说明图，步骤顺序清楚，动作真实，商品和关键部件可辨认。"

_COMPARISON_COPY = "提炼对比维度，适合和旧款、竞品、普通款或不同套餐做清晰对照，不做无法证实的绝对化承诺。"
_COMPARISON_IMAGE = "生成商品对比图，用左右或上下结构展示差异，突出可验证的规格、功能或使用体验差别。"

_LIFESTYLE_COPY = "提炼目标人群、使用场景和生活方式氛围，避免夸张承诺。"
_LIFESTYLE_IMAGE = "生成自然生活方式图，让商品在真实使用场景中清楚可见，人物或环境服务于商品。"

_SCENE_COPY = "提炼商品适合的使用场景、搭配对象和环境关键词。"
_SCENE_IMAGE = "生成商品使用场景图，环境真实可信，商品仍是画面主体。"

_DETAIL_COPY = "提炼商品材质、工艺、结构和功能细节，形成短标题和说明点。"
_DETAIL_IMAGE = "生成细节或材质特写图，强调纹理、结构和功能点，避免失真。"

_CAMPAIGN_COPY = "生成活动图文案，突出优惠、时效和商品利益点，语气明确但不过度承诺。"
_CAMPAIGN_IMAGE = "生成活动促销商品图，保留商品主体，增加促销氛围和明确视觉层级。"

_SHORT_VIDEO_COVER_COPY = "提炼适合短视频封面的 1 个强钩子和 2 个辅助信息点，语气直接但不过度标题党。"
_SHORT_VIDEO_COVER_IMAGE = "生成短视频竖版封面，主体醒目，封面钩子明确，适合站内短视频和内容投放入口。"

_WHITE_BACKGROUND_COPY = "提炼白底图需要保留的商品主体、角度和规格重点，不添加促销语。"
_WHITE_BACKGROUND_IMAGE = "生成白底商品图，纯净背景，商品边缘清晰，保持真实比例和材质。"


BUILTIN_CANVAS_TEMPLATES: tuple[CanvasTemplate, ...] = (
    _full_canvas_template(
        key="ecommerce-main-image-v1",
        title="电商主图",
        description="生成商品首图，突出主体、利益点和清晰构图。",
        scenario=_scenario(
            CanvasTemplateScenario.MAIN_IMAGE,
            title="主图",
            description="用于商品列表和详情首屏的主视觉。",
            ecommerce_stage="listing",
            tags=("main-image", "hero", "listing"),
        ),
        nodes=(
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=132),
            _node(
                "copy",
                WorkflowNodeType.COPY_GENERATION,
                title="主图卖点",
                x=320,
                y=88,
                instruction_seed=_MAIN_IMAGE_COPY,
            ),
            _node(
                "image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="生成主图",
                x=640,
                y=96,
                instruction_seed=_MAIN_IMAGE_INSTRUCTION,
                size="1024x1024",
            ),
            _node(
                "output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="主图输出",
                x=960,
                y=72,
                config_json={"role": "output", "label": "主图输出"},
                output_slot_label="主图输出",
            ),
            _node(
                "refine",
                WorkflowNodeType.IMAGE_GENERATION,
                title="细化主图",
                x=1280,
                y=180,
                instruction_seed="基于主图输出继续细化主体、光线和构图，生成一个可对比的新版本。",
                size="1024x1024",
            ),
            _node(
                "refined_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="细化输出",
                x=1600,
                y=180,
                config_json={"role": "output", "label": "细化输出"},
                output_slot_label="细化输出",
            ),
        ),
        edges=(
            ("product", "copy"),
            ("product", "image"),
            ("copy", "image"),
            ("image", "output"),
            ("output", "refine"),
            ("refine", "refined_output"),
        ),
        output_slots=(
            _output_slot("output", "主图输出", "商品列表和详情首图候选。"),
            _output_slot("refined_output", "细化输出", "主图细化版本候选。"),
        ),
        suggested_connections=(
            _suggest("product", "copy", "商品资料为主图卖点提供基础信息。"),
            _suggest("copy", "image", "主图卖点进入图片生成节点，约束视觉重点。"),
            _suggest("output", "refine", "主图输出作为下游参考，用于生成可对比的细化版本。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-taobao-main-image-v1",
        title="淘宝主图",
        description="生成适合淘宝搜索、推荐和详情首屏的 1:1 商品主图。",
        scenario=_scenario(
            CanvasTemplateScenario.TAOBAO_MAIN_IMAGE,
            title="淘宝主图",
            description="用于淘宝列表流量和详情首屏，强调主体清晰、卖点明确。",
            ecommerce_stage="listing",
            tags=("taobao", "main-image", "marketplace"),
        ),
        nodes=(
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=132),
            _node(
                "angle",
                WorkflowNodeType.COPY_GENERATION,
                title="搜索卖点",
                x=320,
                y=72,
                instruction_seed=_TAOBAO_COPY,
            ),
            _node(
                "main",
                WorkflowNodeType.IMAGE_GENERATION,
                title="主图版本",
                x=640,
                y=72,
                instruction_seed=_TAOBAO_IMAGE,
                size="1024x1024",
            ),
            _node(
                "main_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="淘宝主图",
                x=960,
                y=72,
                config_json={"role": "output", "label": "淘宝主图"},
                output_slot_label="淘宝主图",
            ),
            _node(
                "clean",
                WorkflowNodeType.IMAGE_GENERATION,
                title="干净版本",
                x=640,
                y=208,
                instruction_seed="生成更克制的淘宝主图版本，减少装饰元素，优先保证商品主体和边缘清晰。",
                size="1024x1024",
            ),
            _node(
                "clean_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="干净主图",
                x=960,
                y=208,
                config_json={"role": "output", "label": "干净主图"},
                output_slot_label="干净主图",
            ),
        ),
        edges=(
            ("product", "angle"),
            ("product", "main"),
            ("angle", "main"),
            ("main", "main_output"),
            ("product", "clean"),
            ("angle", "clean"),
            ("clean", "clean_output"),
        ),
        output_slots=(
            _output_slot("main_output", "淘宝主图", "淘宝搜索列表、推荐流和详情首屏主图候选。"),
            _output_slot("clean_output", "干净主图", "更克制的主图备用版本。"),
        ),
        suggested_connections=(
            _suggest("angle", "main", "搜索卖点为主图版本提供转化重点。"),
            _suggest("angle", "clean", "同一卖点生成更干净的备用主图。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-xiaohongshu-image-v1",
        title="小红书图",
        description="生成适合小红书笔记封面和种草内容的竖版生活方式图片。",
        scenario=_scenario(
            CanvasTemplateScenario.XIAOHONGSHU_IMAGE,
            title="小红书",
            description="用于小红书笔记封面、种草内容和生活方式展示。",
            ecommerce_stage="content",
            tags=("xiaohongshu", "cover", "lifestyle"),
        ),
        nodes=(
            _node(
                "style_reference",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="笔记风格参考",
                x=48,
                y=48,
                config_json={"role": "style", "label": "笔记风格参考"},
                reference_input_hint="上传想要接近的笔记封面、生活方式、光线或构图参考。",
            ),
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=200),
            _node(
                "angle",
                WorkflowNodeType.COPY_GENERATION,
                title="封面角度",
                x=360,
                y=112,
                instruction_seed=_XHS_COPY,
            ),
            _node(
                "cover",
                WorkflowNodeType.IMAGE_GENERATION,
                title="竖版封面",
                x=700,
                y=80,
                instruction_seed=_XHS_IMAGE,
                size="1024x1536",
            ),
            _node(
                "cover_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="封面输出",
                x=1040,
                y=80,
                config_json={"role": "output", "label": "封面输出"},
                output_slot_label="封面输出",
            ),
            _node(
                "detail",
                WorkflowNodeType.IMAGE_GENERATION,
                title="内容配图",
                x=700,
                y=232,
                instruction_seed="沿用封面角度，生成一张更适合正文承接的生活方式配图，保留真实使用氛围。",
                size="1024x1536",
            ),
            _node(
                "detail_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="配图输出",
                x=1040,
                y=232,
                config_json={"role": "output", "label": "配图输出"},
                output_slot_label="配图输出",
            ),
        ),
        edges=(
            ("style_reference", "angle"),
            ("product", "angle"),
            ("style_reference", "cover"),
            ("product", "cover"),
            ("angle", "cover"),
            ("cover", "cover_output"),
            ("cover_output", "detail"),
            ("detail", "detail_output"),
        ),
        output_slots=(
            _output_slot("cover_output", "封面输出", "小红书笔记封面候选。"),
            _output_slot("detail_output", "配图输出", "正文承接配图候选。"),
        ),
        reference_input_hints=(
            _reference_hint(
                "style_reference",
                role="style",
                label="笔记风格参考",
                description="上传想要接近的笔记封面、生活方式、光线或构图参考。",
            ),
        ),
        suggested_connections=(
            _suggest("style_reference", "angle", "风格参考帮助封面文案贴近目标内容感。"),
            _suggest("cover_output", "detail", "封面输出继续生成正文配图，保持同一内容调性。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-multi-angle-image-v1",
        title="多角度图",
        description="生成正面、侧面和背面/关键结构图，补齐详情图册基础素材。",
        scenario=_scenario(
            CanvasTemplateScenario.MULTI_ANGLE,
            title="多角度",
            description="用于详情图册轮播，帮助买家看清外观、结构和背面细节。",
            ecommerce_stage="gallery",
            tags=("angle", "gallery", "detail"),
        ),
        nodes=(
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=168),
            _node(
                "angle_plan",
                WorkflowNodeType.COPY_GENERATION,
                title="角度规划",
                x=340,
                y=156,
                instruction_seed=_MULTI_ANGLE_COPY,
            ),
            _node(
                "front_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="正面角度",
                x=680,
                y=48,
                instruction_seed=_MULTI_ANGLE_IMAGE,
                size="1024x1024",
            ),
            _node(
                "front_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="正面输出",
                x=1020,
                y=48,
                config_json={"role": "output", "label": "正面输出"},
                output_slot_label="正面输出",
            ),
            _node(
                "side_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="侧面角度",
                x=680,
                y=180,
                instruction_seed="生成同一商品的侧面或 45 度角展示图，比例和材质与正面保持一致。",
                size="1024x1024",
            ),
            _node(
                "side_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="侧面输出",
                x=1020,
                y=180,
                config_json={"role": "output", "label": "侧面输出"},
                output_slot_label="侧面输出",
            ),
            _node(
                "back_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="背面/结构",
                x=680,
                y=312,
                instruction_seed="生成同一商品的背面、底部或关键结构角度，强调真实结构和比例。",
                size="1024x1024",
            ),
            _node(
                "back_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="结构输出",
                x=1020,
                y=312,
                config_json={"role": "output", "label": "结构输出"},
                output_slot_label="结构输出",
            ),
        ),
        edges=(
            ("product", "angle_plan"),
            ("product", "front_image"),
            ("angle_plan", "front_image"),
            ("front_image", "front_output"),
            ("product", "side_image"),
            ("angle_plan", "side_image"),
            ("side_image", "side_output"),
            ("product", "back_image"),
            ("angle_plan", "back_image"),
            ("back_image", "back_output"),
        ),
        output_slots=(
            _output_slot("front_output", "正面输出", "详情页轮播中的正面候选图。"),
            _output_slot("side_output", "侧面输出", "详情页轮播中的侧面候选图。"),
            _output_slot("back_output", "结构输出", "背面、底部或关键结构候选图。"),
        ),
        suggested_connections=(
            _suggest("angle_plan", "front_image", "角度规划让多张图保持同一展示顺序和主体比例。"),
            _suggest("angle_plan", "back_image", "同一规划约束背面或结构图不偏离商品主体。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-sku-variant-image-v1",
        title="SKU/变体图",
        description="为颜色、规格或组合 SKU 生成差异化展示图。",
        scenario=_scenario(
            CanvasTemplateScenario.SKU_VARIANT,
            title="SKU/变体",
            description="用于商品详情中的规格差异说明。",
            ecommerce_stage="detail",
            tags=("sku", "variant", "detail"),
        ),
        nodes=(
            _node(
                "sku_reference",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="SKU 参考图",
                x=48,
                y=48,
                config_json={"role": "product_reference", "label": "SKU 参考图"},
                reference_input_hint="上传目标 SKU、颜色或规格的参考图。",
            ),
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=204),
            _node(
                "variant_copy",
                WorkflowNodeType.COPY_GENERATION,
                title="变体差异",
                x=360,
                y=120,
                instruction_seed=_SKU_COPY,
            ),
            _node(
                "single_variant",
                WorkflowNodeType.IMAGE_GENERATION,
                title="单 SKU 图",
                x=700,
                y=64,
                instruction_seed=_SKU_IMAGE,
                size="1024x1024",
            ),
            _node(
                "single_variant_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="SKU 图输出",
                x=1040,
                y=64,
                config_json={"role": "output", "label": "SKU 图输出"},
                output_slot_label="SKU 图输出",
            ),
            _node(
                "variant_grid",
                WorkflowNodeType.IMAGE_GENERATION,
                title="变体对照",
                x=700,
                y=224,
                instruction_seed="生成颜色、规格或组合 SKU 的对照展示图，保持同一视角和统一光线。",
                size="1536x1024",
            ),
            _node(
                "variant_grid_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="变体对照输出",
                x=1040,
                y=224,
                config_json={"role": "output", "label": "变体对照输出"},
                output_slot_label="变体对照输出",
            ),
        ),
        edges=(
            ("sku_reference", "variant_copy"),
            ("product", "variant_copy"),
            ("sku_reference", "single_variant"),
            ("product", "single_variant"),
            ("variant_copy", "single_variant"),
            ("single_variant", "single_variant_output"),
            ("sku_reference", "variant_grid"),
            ("product", "variant_grid"),
            ("variant_copy", "variant_grid"),
            ("variant_grid", "variant_grid_output"),
        ),
        output_slots=(
            _output_slot("single_variant_output", "SKU 图输出", "商品规格选择区或详情图候选。"),
            _output_slot("variant_grid_output", "变体对照输出", "多 SKU 对照说明候选。"),
        ),
        reference_input_hints=(
            _reference_hint(
                "sku_reference",
                role="product_reference",
                label="SKU 参考图",
                description="上传目标 SKU、颜色或规格的参考图。",
            ),
        ),
        suggested_connections=(
            _suggest("sku_reference", "single_variant", "SKU 参考图帮助保持颜色、规格和主体一致。"),
            _suggest("variant_copy", "variant_grid", "变体差异说明用于生成可比较的多 SKU 图。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-feature-infographic-v1",
        title="功能卖点图",
        description="把商品核心功能转成有层级的信息图，用于详情页首屏说服。",
        scenario=_scenario(
            CanvasTemplateScenario.FEATURE_INFOGRAPHIC,
            title="功能卖点",
            description="用于详情页卖点解释、功能入口和转化说服。",
            ecommerce_stage="detail",
            tags=("feature", "infographic", "detail"),
        ),
        nodes=(
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=176),
            _node(
                "feature_copy",
                WorkflowNodeType.COPY_GENERATION,
                title="卖点提炼",
                x=340,
                y=92,
                instruction_seed=_FEATURE_INFOGRAPHIC_COPY,
            ),
            _node(
                "layout_copy",
                WorkflowNodeType.COPY_GENERATION,
                title="信息层级",
                x=340,
                y=244,
                instruction_seed="把卖点整理成信息图层级：主标题、功能标签、图标/标注位置和需要留白的区域。",
            ),
            _node(
                "infographic",
                WorkflowNodeType.IMAGE_GENERATION,
                title="卖点信息图",
                x=700,
                y=168,
                instruction_seed=_FEATURE_INFOGRAPHIC_IMAGE,
                size="1024x1536",
            ),
            _node(
                "infographic_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="卖点图输出",
                x=1060,
                y=168,
                config_json={"role": "output", "label": "卖点图输出"},
                output_slot_label="卖点图输出",
            ),
        ),
        edges=(
            ("product", "feature_copy"),
            ("product", "layout_copy"),
            ("feature_copy", "layout_copy"),
            ("product", "infographic"),
            ("feature_copy", "infographic"),
            ("layout_copy", "infographic"),
            ("infographic", "infographic_output"),
        ),
        output_slots=(
            _output_slot("infographic_output", "卖点图输出", "详情页功能卖点说明图候选。"),
        ),
        suggested_connections=(
            _suggest("feature_copy", "layout_copy", "先提炼卖点，再组织信息图层级。"),
            _suggest("layout_copy", "infographic", "信息层级控制画面主次和留白。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-size-spec-image-v1",
        title="尺寸/规格图",
        description="生成尺寸、容量、材质和参数说明图，降低下单前疑问。",
        scenario=_scenario(
            CanvasTemplateScenario.SIZE_SPEC,
            title="尺寸/规格",
            description="用于详情页参数、尺寸、容量和规格解释。",
            ecommerce_stage="detail",
            tags=("size", "spec", "detail"),
        ),
        nodes=(
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=168),
            _node(
                "spec_copy",
                WorkflowNodeType.COPY_GENERATION,
                title="规格整理",
                x=340,
                y=88,
                instruction_seed=_SIZE_SPEC_COPY,
            ),
            _node(
                "dimension_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="尺寸标注图",
                x=680,
                y=64,
                instruction_seed=_SIZE_SPEC_IMAGE,
                size="1536x1024",
            ),
            _node(
                "dimension_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="尺寸输出",
                x=1020,
                y=64,
                config_json={"role": "output", "label": "尺寸输出"},
                output_slot_label="尺寸输出",
            ),
            _node(
                "spec_table_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="参数说明图",
                x=680,
                y=236,
                instruction_seed="生成参数说明图，突出规格、材质、容量、适配范围和注意事项，版式清晰可读。",
                size="1024x1536",
            ),
            _node(
                "spec_table_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="参数输出",
                x=1020,
                y=236,
                config_json={"role": "output", "label": "参数输出"},
                output_slot_label="参数输出",
            ),
        ),
        edges=(
            ("product", "spec_copy"),
            ("product", "dimension_image"),
            ("spec_copy", "dimension_image"),
            ("dimension_image", "dimension_output"),
            ("product", "spec_table_image"),
            ("spec_copy", "spec_table_image"),
            ("spec_table_image", "spec_table_output"),
        ),
        output_slots=(
            _output_slot("dimension_output", "尺寸输出", "尺寸标注详情图候选。"),
            _output_slot("spec_table_output", "参数输出", "规格参数说明图候选。"),
        ),
        suggested_connections=(
            _suggest("spec_copy", "dimension_image", "规格整理为尺寸标注图提供准确说明点。"),
            _suggest("spec_copy", "spec_table_image", "同一规格文案继续生成参数说明图。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-scale-reference-image-v1",
        title="尺度参照图",
        description="通过手持、佩戴、桌面或空间参照，让买家判断真实大小。",
        scenario=_scenario(
            CanvasTemplateScenario.SCALE_REFERENCE,
            title="尺度参照",
            description="用于解释大小、厚度、容量和上身/上桌效果。",
            ecommerce_stage="detail",
            tags=("scale", "reference", "detail"),
        ),
        nodes=(
            _node(
                "scale_reference",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="参照物参考",
                x=48,
                y=52,
                config_json={"role": "scale", "label": "参照物参考"},
                reference_input_hint="上传希望接近的手持、佩戴、桌面或空间参照图。",
            ),
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=212),
            _node(
                "scale_copy",
                WorkflowNodeType.COPY_GENERATION,
                title="尺度说明",
                x=360,
                y=132,
                instruction_seed=_SCALE_REFERENCE_COPY,
            ),
            _node(
                "handheld_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="手持/佩戴参照",
                x=710,
                y=72,
                instruction_seed=_SCALE_REFERENCE_IMAGE,
                size="1024x1536",
            ),
            _node(
                "handheld_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="尺度图输出",
                x=1060,
                y=72,
                config_json={"role": "output", "label": "尺度图输出"},
                output_slot_label="尺度图输出",
            ),
            _node(
                "surface_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="桌面/空间参照",
                x=710,
                y=244,
                instruction_seed="生成桌面、墙面、背包、厨房台面或收纳空间中的尺度参照图，避免夸大商品比例。",
                size="1536x1024",
            ),
            _node(
                "surface_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="空间参照输出",
                x=1060,
                y=244,
                config_json={"role": "output", "label": "空间参照输出"},
                output_slot_label="空间参照输出",
            ),
        ),
        edges=(
            ("scale_reference", "scale_copy"),
            ("product", "scale_copy"),
            ("scale_reference", "handheld_image"),
            ("product", "handheld_image"),
            ("scale_copy", "handheld_image"),
            ("handheld_image", "handheld_output"),
            ("scale_reference", "surface_image"),
            ("product", "surface_image"),
            ("scale_copy", "surface_image"),
            ("surface_image", "surface_output"),
        ),
        output_slots=(
            _output_slot("handheld_output", "尺度图输出", "手持、佩戴或上身尺度图候选。"),
            _output_slot("surface_output", "空间参照输出", "桌面、墙面或空间参照图候选。"),
        ),
        reference_input_hints=(
            _reference_hint(
                "scale_reference",
                role="scale",
                label="参照物参考",
                description="上传希望接近的手持、佩戴、桌面或空间参照图。",
            ),
        ),
        suggested_connections=(
            _suggest("scale_reference", "handheld_image", "参照图约束人物、桌面或空间比例。"),
            _suggest("scale_copy", "surface_image", "尺度说明帮助空间参照图避免比例失真。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-package-checklist-image-v1",
        title="包装/清单图",
        description="展示包装盒、配件、赠品和到手内容，降低售前疑问。",
        scenario=_scenario(
            CanvasTemplateScenario.PACKAGE_CHECKLIST,
            title="包装清单",
            description="用于详情页到手内容、配件数量和礼盒展示。",
            ecommerce_stage="detail",
            tags=("package", "checklist", "detail"),
        ),
        nodes=(
            _node(
                "package_reference",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="包装参考",
                x=48,
                y=56,
                config_json={"role": "package", "label": "包装参考"},
                reference_input_hint="上传包装盒、配件、赠品、礼盒或清单布局参考图。",
            ),
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=208),
            _node(
                "checklist_copy",
                WorkflowNodeType.COPY_GENERATION,
                title="清单文案",
                x=360,
                y=132,
                instruction_seed=_PACKAGE_COPY,
            ),
            _node(
                "flatlay_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="包装平铺图",
                x=700,
                y=88,
                instruction_seed=_PACKAGE_IMAGE,
                size="1536x1024",
            ),
            _node(
                "flatlay_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="清单输出",
                x=1040,
                y=88,
                config_json={"role": "output", "label": "清单输出"},
                output_slot_label="清单输出",
            ),
            _node(
                "gift_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="礼盒/到手图",
                x=700,
                y=252,
                instruction_seed="生成更强调开箱、礼盒或到手状态的图片，包装和配件清楚可见。",
                size="1024x1024",
            ),
            _node(
                "gift_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="到手输出",
                x=1040,
                y=252,
                config_json={"role": "output", "label": "到手输出"},
                output_slot_label="到手输出",
            ),
        ),
        edges=(
            ("package_reference", "checklist_copy"),
            ("product", "checklist_copy"),
            ("package_reference", "flatlay_image"),
            ("product", "flatlay_image"),
            ("checklist_copy", "flatlay_image"),
            ("flatlay_image", "flatlay_output"),
            ("package_reference", "gift_image"),
            ("product", "gift_image"),
            ("checklist_copy", "gift_image"),
            ("gift_image", "gift_output"),
        ),
        output_slots=(
            _output_slot("flatlay_output", "清单输出", "包装、配件和赠品平铺说明图候选。"),
            _output_slot("gift_output", "到手输出", "礼盒或开箱到手图候选。"),
        ),
        reference_input_hints=(
            _reference_hint(
                "package_reference",
                role="package",
                label="包装参考",
                description="上传包装盒、配件、赠品、礼盒或清单布局参考图。",
            ),
        ),
        suggested_connections=(
            _suggest("package_reference", "flatlay_image", "包装参考约束平铺构图和配件陈列。"),
            _suggest("checklist_copy", "gift_image", "清单文案让到手图保留正确内容项。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-usage-steps-image-v1",
        title="使用步骤图",
        description="生成安装、开箱、佩戴或清洁步骤图，适合降低使用门槛。",
        scenario=_scenario(
            CanvasTemplateScenario.USAGE_STEPS,
            title="使用步骤",
            description="用于安装说明、使用教程、清洁维护和售后前置说明。",
            ecommerce_stage="detail",
            tags=("steps", "usage", "detail"),
        ),
        nodes=(
            _node(
                "step_reference",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="步骤参考",
                x=48,
                y=56,
                config_json={"role": "usage", "label": "步骤参考"},
                reference_input_hint="上传安装、佩戴、开箱、清洁或使用动作参考图。",
            ),
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=220),
            _node(
                "step_copy",
                WorkflowNodeType.COPY_GENERATION,
                title="步骤拆解",
                x=360,
                y=132,
                instruction_seed=_USAGE_STEPS_COPY,
            ),
            _node(
                "step_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="步骤说明图",
                x=700,
                y=84,
                instruction_seed=_USAGE_STEPS_IMAGE,
                size="1024x1536",
            ),
            _node(
                "step_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="步骤输出",
                x=1040,
                y=84,
                config_json={"role": "output", "label": "步骤输出"},
                output_slot_label="步骤输出",
            ),
            _node(
                "tip_copy",
                WorkflowNodeType.COPY_GENERATION,
                title="注意事项",
                x=360,
                y=292,
                instruction_seed="补充使用过程中的注意事项、适配限制、清洁维护或安全提醒，保持简短可读。",
            ),
            _node(
                "tip_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="注意事项图",
                x=700,
                y=292,
                instruction_seed="生成注意事项说明图，用图示和短标签解释使用限制、维护方法或错误示范。",
                size="1024x1024",
            ),
            _node(
                "tip_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="注意事项输出",
                x=1040,
                y=292,
                config_json={"role": "output", "label": "注意事项输出"},
                output_slot_label="注意事项输出",
            ),
        ),
        edges=(
            ("step_reference", "step_copy"),
            ("product", "step_copy"),
            ("step_reference", "step_image"),
            ("product", "step_image"),
            ("step_copy", "step_image"),
            ("step_image", "step_output"),
            ("product", "tip_copy"),
            ("step_copy", "tip_copy"),
            ("step_reference", "tip_image"),
            ("product", "tip_image"),
            ("tip_copy", "tip_image"),
            ("tip_image", "tip_output"),
        ),
        output_slots=(
            _output_slot("step_output", "步骤输出", "安装、开箱、佩戴或使用步骤图候选。"),
            _output_slot("tip_output", "注意事项输出", "维护、适配或错误示范说明图候选。"),
        ),
        reference_input_hints=(
            _reference_hint(
                "step_reference",
                role="usage",
                label="步骤参考",
                description="上传安装、佩戴、开箱、清洁或使用动作参考图。",
            ),
        ),
        suggested_connections=(
            _suggest("step_copy", "tip_copy", "从步骤拆解继续推导注意事项，避免漏掉关键限制。"),
            _suggest("tip_copy", "tip_image", "注意事项文案生成单独说明图，方便详情页拆分展示。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-comparison-image-v1",
        title="对比图",
        description="生成和旧款、普通款、竞品或套餐的对比说明图。",
        scenario=_scenario(
            CanvasTemplateScenario.COMPARISON,
            title="对比",
            description="用于解释升级点、套餐差异和购买决策维度。",
            ecommerce_stage="detail",
            tags=("comparison", "upgrade", "detail"),
        ),
        nodes=(
            _node(
                "compare_reference",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="对比参考",
                x=48,
                y=56,
                config_json={"role": "comparison", "label": "对比参考"},
                reference_input_hint="上传旧款、竞品、普通款、套餐或对比版式参考图。",
            ),
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=212),
            _node(
                "comparison_copy",
                WorkflowNodeType.COPY_GENERATION,
                title="对比维度",
                x=360,
                y=128,
                instruction_seed=_COMPARISON_COPY,
            ),
            _node(
                "comparison_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="对比说明图",
                x=700,
                y=88,
                instruction_seed=_COMPARISON_IMAGE,
                size="1536x1024",
            ),
            _node(
                "comparison_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="对比输出",
                x=1040,
                y=88,
                config_json={"role": "output", "label": "对比输出"},
                output_slot_label="对比输出",
            ),
            _node(
                "upgrade_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="升级点图",
                x=700,
                y=248,
                instruction_seed="生成聚焦升级点或套餐差异的说明图，强调可验证的材料、结构、容量或功能差别。",
                size="1024x1024",
            ),
            _node(
                "upgrade_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="升级点输出",
                x=1040,
                y=248,
                config_json={"role": "output", "label": "升级点输出"},
                output_slot_label="升级点输出",
            ),
        ),
        edges=(
            ("compare_reference", "comparison_copy"),
            ("product", "comparison_copy"),
            ("compare_reference", "comparison_image"),
            ("product", "comparison_image"),
            ("comparison_copy", "comparison_image"),
            ("comparison_image", "comparison_output"),
            ("compare_reference", "upgrade_image"),
            ("product", "upgrade_image"),
            ("comparison_copy", "upgrade_image"),
            ("upgrade_image", "upgrade_output"),
        ),
        output_slots=(
            _output_slot("comparison_output", "对比输出", "左右或上下结构的对比说明图候选。"),
            _output_slot("upgrade_output", "升级点输出", "升级点、套餐差异或新旧款差异图候选。"),
        ),
        reference_input_hints=(
            _reference_hint(
                "compare_reference",
                role="comparison",
                label="对比参考",
                description="上传旧款、竞品、普通款、套餐或对比版式参考图。",
            ),
        ),
        suggested_connections=(
            _suggest("compare_reference", "comparison_image", "对比参考帮助画面按目标维度组织。"),
            _suggest("comparison_copy", "upgrade_image", "对比维度继续生成更聚焦的升级点图。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-model-lifestyle-image-v1",
        title="模特/生活方式图",
        description="生成有人物、穿搭或生活方式氛围的商品场景图。",
        scenario=_scenario(
            CanvasTemplateScenario.MODEL_LIFESTYLE,
            title="模特/生活方式",
            description="用于服饰、美妆、家居等需要使用感的场景展示。",
            ecommerce_stage="gallery",
            tags=("model", "lifestyle", "usage"),
        ),
        nodes=(
            _node(
                "style",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="姿态/风格参考",
                x=48,
                y=48,
                config_json={"role": "style", "label": "姿态/风格参考"},
                reference_input_hint="上传风格、姿态、场景或模特氛围参考图。",
            ),
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=200),
            _node(
                "copy",
                WorkflowNodeType.COPY_GENERATION,
                title="人群与场景",
                x=360,
                y=112,
                instruction_seed=_LIFESTYLE_COPY,
            ),
            _node(
                "half_body",
                WorkflowNodeType.IMAGE_GENERATION,
                title="半身/使用图",
                x=700,
                y=64,
                instruction_seed=_LIFESTYLE_IMAGE,
                size="1024x1536",
            ),
            _node(
                "half_body_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="生活方式图",
                x=1040,
                y=64,
                config_json={"role": "output", "label": "生活方式图"},
                output_slot_label="生活方式图",
            ),
            _node(
                "detail_usage",
                WorkflowNodeType.IMAGE_GENERATION,
                title="使用细节图",
                x=700,
                y=224,
                instruction_seed="生成强调使用动作、触感或佩戴细节的图片，商品应清楚可见，氛围自然。",
                size="1024x1536",
            ),
            _node(
                "detail_usage_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="使用细节输出",
                x=1040,
                y=224,
                config_json={"role": "output", "label": "使用细节输出"},
                output_slot_label="使用细节输出",
            ),
        ),
        edges=(
            ("style", "copy"),
            ("product", "copy"),
            ("style", "half_body"),
            ("product", "half_body"),
            ("copy", "half_body"),
            ("half_body", "half_body_output"),
            ("style", "detail_usage"),
            ("product", "detail_usage"),
            ("copy", "detail_usage"),
            ("detail_usage", "detail_usage_output"),
        ),
        output_slots=(
            _output_slot("half_body_output", "生活方式图", "详情页图册中的人物或生活方式图候选。"),
            _output_slot("detail_usage_output", "使用细节输出", "使用动作或局部氛围图候选。"),
        ),
        reference_input_hints=(
            _reference_hint(
                "style",
                role="style",
                label="姿态/风格参考",
                description="上传风格、姿态、场景或模特氛围参考图。",
            ),
        ),
        suggested_connections=(
            _suggest("style", "half_body", "风格参考直接约束人物姿态、光线和画面氛围。"),
            _suggest("copy", "detail_usage", "同一人群与场景说明生成使用细节图。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-scene-image-v1",
        title="场景图",
        description="把商品放入可理解的使用空间或业务场景。",
        scenario=_scenario(
            CanvasTemplateScenario.SCENE_IMAGE,
            title="场景",
            description="用于说明商品使用环境、搭配和空间关系。",
            ecommerce_stage="gallery",
            tags=("scene", "context", "usage"),
        ),
        nodes=(
            _node(
                "scene_reference",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="场景参考",
                x=48,
                y=64,
                config_json={"role": "scene", "label": "场景参考"},
                reference_input_hint="上传目标空间、季节、光线或陈列方式参考图。",
            ),
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=208),
            _node(
                "copy",
                WorkflowNodeType.COPY_GENERATION,
                title="场景说明",
                x=360,
                y=136,
                instruction_seed=_SCENE_COPY,
            ),
            _node(
                "wide_scene",
                WorkflowNodeType.IMAGE_GENERATION,
                title="宽幅场景",
                x=700,
                y=136,
                instruction_seed=_SCENE_IMAGE,
                size="1536x1024",
            ),
            _node(
                "scene_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="场景图输出",
                x=1040,
                y=136,
                config_json={"role": "output", "label": "场景图输出"},
                output_slot_label="场景图输出",
            ),
        ),
        edges=(
            ("scene_reference", "copy"),
            ("product", "copy"),
            ("scene_reference", "wide_scene"),
            ("product", "wide_scene"),
            ("copy", "wide_scene"),
            ("wide_scene", "scene_output"),
        ),
        output_slots=(
            _output_slot("scene_output", "场景图输出", "详情页图册中的使用场景图候选。"),
        ),
        reference_input_hints=(
            _reference_hint(
                "scene_reference",
                role="scene",
                label="场景参考",
                description="上传目标空间、季节、光线或陈列方式参考图。",
            ),
        ),
        suggested_connections=(
            _suggest("scene_reference", "wide_scene", "场景参考直接约束空间、光线和陈列方式。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-detail-material-image-v1",
        title="细节/材质图",
        description="生成材质、工艺、局部结构或功能细节展示图。",
        scenario=_scenario(
            CanvasTemplateScenario.DETAIL_MATERIAL,
            title="细节/材质",
            description="用于详情页解释材质、工艺和关键功能。",
            ecommerce_stage="detail",
            tags=("detail", "material", "macro"),
        ),
        nodes=(
            _node(
                "detail_reference",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="细节参考图",
                x=48,
                y=56,
                config_json={"role": "detail", "label": "细节参考图"},
                reference_input_hint="上传材质纹理、局部结构或工艺细节参考图。",
            ),
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=212),
            _node(
                "detail_copy",
                WorkflowNodeType.COPY_GENERATION,
                title="细节说明",
                x=360,
                y=132,
                instruction_seed=_DETAIL_COPY,
            ),
            _node(
                "macro_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="材质特写",
                x=700,
                y=72,
                instruction_seed=_DETAIL_IMAGE,
                size="1024x1024",
            ),
            _node(
                "macro_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="细节图输出",
                x=1040,
                y=72,
                config_json={"role": "output", "label": "细节图输出"},
                output_slot_label="细节图输出",
            ),
            _node(
                "structure_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="结构说明图",
                x=700,
                y=236,
                instruction_seed="生成局部结构、开合方式、接口、缝线或工艺说明图，强调可理解的结构关系。",
                size="1024x1024",
            ),
            _node(
                "structure_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="结构输出",
                x=1040,
                y=236,
                config_json={"role": "output", "label": "结构输出"},
                output_slot_label="结构输出",
            ),
        ),
        edges=(
            ("detail_reference", "detail_copy"),
            ("product", "detail_copy"),
            ("detail_reference", "macro_image"),
            ("product", "macro_image"),
            ("detail_copy", "macro_image"),
            ("macro_image", "macro_output"),
            ("detail_reference", "structure_image"),
            ("product", "structure_image"),
            ("detail_copy", "structure_image"),
            ("structure_image", "structure_output"),
        ),
        output_slots=(
            _output_slot("macro_output", "细节图输出", "详情页材质或功能说明图候选。"),
            _output_slot("structure_output", "结构输出", "局部结构、接口或工艺说明图候选。"),
        ),
        reference_input_hints=(
            _reference_hint(
                "detail_reference",
                role="detail",
                label="细节参考图",
                description="上传材质纹理、局部结构或工艺细节参考图。",
            ),
        ),
        suggested_connections=(
            _suggest("detail_reference", "macro_image", "细节参考图帮助特写图保持纹理和结构真实。"),
            _suggest("detail_copy", "structure_image", "细节说明继续生成结构关系更清楚的说明图。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-campaign-promotion-image-v1",
        title="活动/促销图",
        description="生成适合活动入口、优惠表达和促销氛围的商品图。",
        scenario=_scenario(
            CanvasTemplateScenario.CAMPAIGN_PROMOTION,
            title="活动/促销",
            description="用于活动页、促销位和站内投放素材。",
            ecommerce_stage="campaign",
            tags=("campaign", "promotion", "banner"),
        ),
        nodes=(
            _node(
                "campaign_style",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="活动风格参考",
                x=48,
                y=48,
                config_json={"role": "style", "label": "活动风格参考"},
                reference_input_hint="上传活动主视觉、品牌色、节日氛围或版式参考图。",
            ),
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=216),
            _node(
                "offer_copy",
                WorkflowNodeType.COPY_GENERATION,
                title="优惠信息",
                x=360,
                y=80,
                instruction_seed=_CAMPAIGN_COPY,
            ),
            _node(
                "visual_copy",
                WorkflowNodeType.COPY_GENERATION,
                title="视觉层级",
                x=360,
                y=224,
                instruction_seed="整理活动图的画面层级：商品主体、利益点、活动氛围和留白位置，避免信息堆满。",
            ),
            _node(
                "banner",
                WorkflowNodeType.IMAGE_GENERATION,
                title="活动横图",
                x=720,
                y=136,
                instruction_seed=_CAMPAIGN_IMAGE,
                size="1536x1024",
            ),
            _node(
                "banner_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="活动图输出",
                x=1080,
                y=136,
                config_json={"role": "output", "label": "活动图输出"},
                output_slot_label="活动图输出",
            ),
        ),
        edges=(
            ("campaign_style", "offer_copy"),
            ("product", "offer_copy"),
            ("campaign_style", "visual_copy"),
            ("product", "visual_copy"),
            ("campaign_style", "banner"),
            ("product", "banner"),
            ("offer_copy", "banner"),
            ("visual_copy", "banner"),
            ("banner", "banner_output"),
        ),
        output_slots=(
            _output_slot("banner_output", "活动图输出", "活动入口、促销位或广告素材候选。"),
        ),
        reference_input_hints=(
            _reference_hint(
                "campaign_style",
                role="style",
                label="活动风格参考",
                description="上传活动主视觉、品牌色、节日氛围或版式参考图。",
            ),
        ),
        suggested_connections=(
            _suggest("offer_copy", "banner", "优惠信息提供活动转化重点。"),
            _suggest("visual_copy", "banner", "视觉层级说明帮助画面留白和主次关系更清楚。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-short-video-cover-v1",
        title="短视频封面图",
        description="生成适合短视频入口、内容流和直播预热视频的竖版封面。",
        scenario=_scenario(
            CanvasTemplateScenario.SHORT_VIDEO_COVER,
            title="短视频封面",
            description="用于站内短视频、内容流、直播预热和广告入口。",
            ecommerce_stage="content",
            tags=("video", "cover", "content"),
        ),
        nodes=(
            _node(
                "cover_style",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="封面风格参考",
                x=48,
                y=56,
                config_json={"role": "style", "label": "封面风格参考"},
                reference_input_hint="上传短视频封面、直播预热、内容流或达人视频截图参考。",
            ),
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=220),
            _node(
                "hook_copy",
                WorkflowNodeType.COPY_GENERATION,
                title="封面钩子",
                x=360,
                y=88,
                instruction_seed=_SHORT_VIDEO_COVER_COPY,
            ),
            _node(
                "frame_copy",
                WorkflowNodeType.COPY_GENERATION,
                title="画面节奏",
                x=360,
                y=244,
                instruction_seed="整理短视频封面的画面节奏：商品特写、使用瞬间、人物视线、标题区域和安全留白。",
            ),
            _node(
                "vertical_cover",
                WorkflowNodeType.IMAGE_GENERATION,
                title="竖版封面",
                x=720,
                y=96,
                instruction_seed=_SHORT_VIDEO_COVER_IMAGE,
                size="1024x1536",
            ),
            _node(
                "vertical_cover_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="短视频封面输出",
                x=1080,
                y=96,
                config_json={"role": "output", "label": "短视频封面输出"},
                output_slot_label="短视频封面输出",
            ),
            _node(
                "closeup_cover",
                WorkflowNodeType.IMAGE_GENERATION,
                title="特写封面",
                x=720,
                y=276,
                instruction_seed="生成更聚焦商品特写和使用瞬间的短视频封面备用版本，适合内容流快速识别。",
                size="1024x1536",
            ),
            _node(
                "closeup_cover_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="特写封面输出",
                x=1080,
                y=276,
                config_json={"role": "output", "label": "特写封面输出"},
                output_slot_label="特写封面输出",
            ),
        ),
        edges=(
            ("cover_style", "hook_copy"),
            ("product", "hook_copy"),
            ("cover_style", "frame_copy"),
            ("product", "frame_copy"),
            ("cover_style", "vertical_cover"),
            ("product", "vertical_cover"),
            ("hook_copy", "vertical_cover"),
            ("frame_copy", "vertical_cover"),
            ("vertical_cover", "vertical_cover_output"),
            ("cover_style", "closeup_cover"),
            ("product", "closeup_cover"),
            ("hook_copy", "closeup_cover"),
            ("frame_copy", "closeup_cover"),
            ("closeup_cover", "closeup_cover_output"),
        ),
        output_slots=(
            _output_slot("vertical_cover_output", "短视频封面输出", "短视频、内容流或直播预热封面候选。"),
            _output_slot("closeup_cover_output", "特写封面输出", "更聚焦商品和使用瞬间的封面候选。"),
        ),
        reference_input_hints=(
            _reference_hint(
                "cover_style",
                role="style",
                label="封面风格参考",
                description="上传短视频封面、直播预热、内容流或达人视频截图参考。",
            ),
        ),
        suggested_connections=(
            _suggest("hook_copy", "vertical_cover", "封面钩子决定用户在内容流里的第一眼信息。"),
            _suggest("frame_copy", "closeup_cover", "画面节奏约束特写封面的标题区和商品识别。"),
        ),
    ),
    _full_canvas_template(
        key="ecommerce-white-background-image-v1",
        title="白底图",
        description="生成用于平台规范、抠图或基础陈列的白底商品图。",
        scenario=_scenario(
            CanvasTemplateScenario.WHITE_BACKGROUND,
            title="白底",
            description="用于平台基础商品图、规格图和素材复用。",
            ecommerce_stage="listing",
            tags=("white-background", "clean", "marketplace"),
        ),
        nodes=(
            _node(
                "product_reference",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="主体参考图",
                x=48,
                y=56,
                config_json={"role": "product_reference", "label": "主体参考图"},
                reference_input_hint="上传需要保留外观、角度或比例的商品主体参考图。",
            ),
            _node("product", WorkflowNodeType.PRODUCT_CONTEXT, title="商品资料", x=48, y=212),
            _node(
                "clean_copy",
                WorkflowNodeType.COPY_GENERATION,
                title="白底要求",
                x=360,
                y=132,
                instruction_seed=_WHITE_BACKGROUND_COPY,
            ),
            _node(
                "white_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="标准白底图",
                x=700,
                y=80,
                instruction_seed=_WHITE_BACKGROUND_IMAGE,
                size="1024x1024",
            ),
            _node(
                "white_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="白底图输出",
                x=1040,
                y=80,
                config_json={"role": "output", "label": "白底图输出"},
                output_slot_label="白底图输出",
            ),
            _node(
                "shadow_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="轻阴影陈列图",
                x=700,
                y=248,
                instruction_seed="生成浅灰或纯白背景下的轻阴影商品陈列图，适合保留材质和体积感。",
                size="1024x1024",
            ),
            _node(
                "shadow_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="陈列输出",
                x=1040,
                y=248,
                config_json={"role": "output", "label": "陈列输出"},
                output_slot_label="陈列输出",
            ),
        ),
        edges=(
            ("product_reference", "clean_copy"),
            ("product", "clean_copy"),
            ("product_reference", "white_image"),
            ("product", "white_image"),
            ("clean_copy", "white_image"),
            ("white_image", "white_output"),
            ("product_reference", "shadow_image"),
            ("product", "shadow_image"),
            ("clean_copy", "shadow_image"),
            ("shadow_image", "shadow_output"),
        ),
        output_slots=(
            _output_slot("white_output", "白底图输出", "平台规范白底图或后续编辑基础素材。"),
            _output_slot("shadow_output", "陈列输出", "带轻阴影的基础陈列备用图。"),
        ),
        reference_input_hints=(
            _reference_hint(
                "product_reference",
                role="product_reference",
                label="主体参考图",
                description="上传需要保留外观、角度或比例的商品主体参考图。",
            ),
        ),
        suggested_connections=(
            _suggest("product_reference", "white_image", "主体参考图用于保持外观、角度和边缘细节。"),
            _suggest("clean_copy", "shadow_image", "白底要求继续生成带轻阴影的基础陈列版本。"),
        ),
    ),
)
