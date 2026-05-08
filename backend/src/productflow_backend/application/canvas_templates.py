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


class CanvasTemplateScenario(StrEnum):
    MAIN_IMAGE = "main_image"
    SKU_VARIANT = "sku_variant"
    MODEL_LIFESTYLE = "model_lifestyle"
    SCENE_IMAGE = "scene_image"
    DETAIL_MATERIAL = "detail_material"
    CAMPAIGN_PROMOTION = "campaign_promotion"
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

    try:
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
    except BusinessValidationError:
        raise
    except ValueError as exc:
        raise BusinessValidationError(str(exc)) from exc


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


def _commerce_template(
    *,
    key: str,
    kind: TemplateKind = "full_canvas",
    title: str,
    description: str,
    scenario: CanvasTemplateScenarioMetadata,
    copy_instruction: str,
    image_instruction: str,
    size: str,
    output_label: str,
    output_description: str,
    reference_label: str | None = None,
    reference_description: str | None = None,
    reference_role: str = "style",
    extra_image_node: bool = False,
) -> CanvasTemplate:
    product_x = 48
    product_y = 120 if reference_label is None else 184
    reference_x = 48
    reference_y = 54
    copy_x = 320 if reference_label is None else 348
    copy_y = 80 if reference_label is None else 112
    image_x = 640 if reference_label is None else 668
    image_y = 112
    output_x = 960 if reference_label is None else 988
    output_y = 66 if extra_image_node else 112
    iteration_image_x = 1280 if reference_label is None else 1308
    iteration_image_y = 188
    iteration_output_x = 1600 if reference_label is None else 1628
    iteration_output_y = 188

    nodes = [
        _node(
            "product",
            WorkflowNodeType.PRODUCT_CONTEXT,
            title="商品资料",
            x=product_x,
            y=product_y,
            config_json={},
        ),
        _node(
            "copy",
            WorkflowNodeType.COPY_GENERATION,
            title="电商文案",
            x=copy_x,
            y=copy_y,
            instruction_seed=copy_instruction,
        ),
    ]
    edges = [_edge("product", "copy")]
    reference_hints: list[CanvasTemplateReferenceInputHint] = []
    suggested_connections = [
        _suggest("product", "copy", "商品资料为文案提供基础卖点。"),
    ]
    if reference_label is not None and reference_description is not None:
        nodes.append(
            _node(
                "reference",
                WorkflowNodeType.REFERENCE_IMAGE,
                title=reference_label,
                x=reference_x,
                y=reference_y,
                config_json={"role": reference_role, "label": reference_label},
                reference_input_hint=reference_description,
            )
        )
        edges.append(_edge("reference", "copy"))
        reference_hints.append(
            _reference_hint(
                "reference",
                role=reference_role,
                label=reference_label,
                description=reference_description,
            )
        )
        suggested_connections.append(_suggest("reference", "copy", "参考图为文案提供风格、材质或场景约束。"))

    nodes.append(
        _node(
            "image",
            WorkflowNodeType.IMAGE_GENERATION,
            title="生成图片",
            x=image_x,
            y=image_y,
            instruction_seed=image_instruction,
            size=size,
        )
    )
    nodes.append(
        _node(
            "output",
            WorkflowNodeType.REFERENCE_IMAGE,
            title=output_label,
            x=output_x,
            y=output_y,
            config_json={"role": "output", "label": output_label},
            output_slot_label=output_label,
        )
    )
    edges.extend(
        [
            _edge("product", "image"),
            _edge("copy", "image"),
            _edge("image", "output"),
        ]
    )
    suggested_connections.extend(
        [
            _suggest("copy", "image", "文案为生图提供标题、卖点和视觉重点。"),
            _suggest("image", "output", "生图节点将结果写入下游输出槽，避免在生图节点自身承载图片。"),
        ]
    )

    if extra_image_node:
        nodes.append(
            _node(
                "iteration_image",
                WorkflowNodeType.IMAGE_GENERATION,
                title="细化生图",
                x=iteration_image_x,
                y=iteration_image_y,
                instruction_seed="基于上一张输出图继续细化，但保持 DAG 下游连接，不创建回环。",
                size=size,
            )
        )
        nodes.append(
            _node(
                "iteration_output",
                WorkflowNodeType.REFERENCE_IMAGE,
                title="细化输出",
                x=iteration_output_x,
                y=iteration_output_y,
                config_json={"role": "output", "label": "细化输出"},
                output_slot_label="细化输出",
            )
        )
        edges.extend(
            [
                _edge("output", "iteration_image"),
                _edge("iteration_image", "iteration_output"),
            ]
        )
        suggested_connections.extend(
            [
                _suggest("output", "iteration_image", "用上一轮输出作为下游参考输入，表达迭代但不形成真实循环。"),
                _suggest("iteration_image", "iteration_output", "细化结果进入新的参考图输出槽。"),
            ]
        )

    template = CanvasTemplate(
        key=key,
        kind=kind,
        title=title,
        description=description,
        scenario=scenario,
        nodes=tuple(nodes),
        edges=tuple(edges),
        prompt_seeds=(copy_instruction, image_instruction),
        instruction_seeds=(copy_instruction, image_instruction),
        output_slots=(
            _output_slot("output", output_label, output_description),
            *(
                (_output_slot("iteration_output", "细化输出", "用于承接 DAG-safe 二次生图结果。"),)
                if extra_image_node
                else ()
            ),
        ),
        reference_input_hints=tuple(reference_hints),
        suggested_connections=tuple(suggested_connections),
    )
    return template


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
            _suggest("reference", "image", "参考图可作为生图输入，保持主体或细节一致。"),
            _suggest("copy", "image", "文案为生图提供标题、卖点和视觉重点。"),
            _suggest("image", "output", "生图结果写入下游参考图输出槽。"),
        ),
        default_external_connections=(
            _default_product_connection("copy"),
            _default_product_connection("image"),
        ),
    )


BUILTIN_CANVAS_TEMPLATES: tuple[CanvasTemplate, ...] = (
    _commerce_template(
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
        copy_instruction="提炼商品核心卖点，文案适合电商主图，控制为简洁标题和 3 个利益点。",
        image_instruction="生成干净、聚焦商品主体的电商主图，主体清晰，卖点可被视觉表达。",
        size="1024x1024",
        output_label="主图输出",
        output_description="商品列表和详情首图候选。",
        extra_image_node=True,
    ),
    _node_group_template(
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
        copy_instruction="围绕当前 SKU 的颜色、规格、容量或组合差异生成简短说明。",
        image_instruction="生成突出 SKU 差异的商品图，保持主体一致，明确展示规格差别。",
        size="1024x1024",
        output_label="SKU 图输出",
        output_description="商品规格选择区或详情图候选。",
        reference_label="SKU 参考图",
        reference_description="上传目标 SKU、颜色或规格的参考图。",
        reference_role="product_reference",
    ),
    _commerce_template(
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
        copy_instruction="提炼目标人群、使用场景和生活方式氛围，避免夸张承诺。",
        image_instruction="生成自然生活方式图，让商品在真实使用场景中清楚可见，人物或环境服务于商品。",
        size="1024x1536",
        output_label="生活方式图输出",
        output_description="详情页图册中的场景展示图候选。",
        reference_label="风格/模特参考",
        reference_description="上传风格、姿态、场景或模特氛围参考图。",
        reference_role="style",
    ),
    _commerce_template(
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
        copy_instruction="提炼商品适合的使用场景、搭配对象和环境关键词。",
        image_instruction="生成商品使用场景图，环境真实可信，商品仍是画面主体。",
        size="1536x1024",
        output_label="场景图输出",
        output_description="详情页图册中的使用场景图候选。",
        reference_label="场景参考图",
        reference_description="上传目标空间、季节、光线或陈列方式参考图。",
        reference_role="scene",
    ),
    _node_group_template(
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
        copy_instruction="提炼商品材质、工艺、结构和功能细节，形成短标题和说明点。",
        image_instruction="生成细节或材质特写图，强调纹理、结构和功能点，避免失真。",
        size="1024x1024",
        output_label="细节图输出",
        output_description="详情页材质或功能说明图候选。",
        reference_label="细节参考图",
        reference_description="上传材质纹理、局部结构或工艺细节参考图。",
        reference_role="detail",
    ),
    _commerce_template(
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
        copy_instruction="生成活动图文案，突出优惠、时效和商品利益点，语气明确但不过度承诺。",
        image_instruction="生成活动促销商品图，保留商品主体，增加促销氛围和明确视觉层级。",
        size="1536x1024",
        output_label="活动图输出",
        output_description="活动入口、促销位或广告素材候选。",
        reference_label="活动风格参考",
        reference_description="上传活动主视觉、品牌色、节日氛围或版式参考图。",
        reference_role="style",
    ),
    _node_group_template(
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
        copy_instruction="提炼白底图需要保留的商品主体、角度和规格重点，不添加促销语。",
        image_instruction="生成白底商品图，纯净背景，商品边缘清晰，保持真实比例和材质。",
        size="1024x1024",
        output_label="白底图输出",
        output_description="平台规范白底图或后续编辑基础素材。",
        reference_label="主体参考图",
        reference_description="上传需要保留外观、角度或比例的商品主体参考图。",
        reference_role="product_reference",
    ),
)
