"""直接创建用的预设 v3 ChangeSet：身份参考图、一层分组、每图种一 prompt + N 张图。"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from pydantic import ValidationError

from productflow_backend.application.agent.product_intake import (
    AGENT_PRODUCT_IMAGE_TYPE_CATALOG,
    LISTING_LOOK_RULE,
    agent_product_image_type_option,
    image_type_family,
    image_type_prompt_goal,
)
from productflow_backend.application.product_workflow.graph_contracts import (
    ConnectNodesOp,
    CreateGroupOp,
    CreateNodeOp,
    GraphOperation,
    WorkflowChangeSet,
)
from productflow_backend.application.workflow_drafts.contracts import (
    WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE,
    WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS,
    WORKFLOW_DRAFT_MAX_TOTAL_IMAGES,
    WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE,
    GenerationSpec,
)
from productflow_backend.domain.enums import GraphActorType, GraphNodeType
from productflow_backend.domain.errors import BusinessValidationError

DEFAULT_TEMPLATE_GENERATION_SPEC = {
    "aspect_ratio": "1:1",
    "resolution_tier": "high",
    "quality_intent": "high",
    "reference_fidelity": "high",
    "background_intent": "auto",
    "text_policy": "none",
}

_IMAGE_TYPE_TITLES = {option.key: option.title for option in AGENT_PRODUCT_IMAGE_TYPE_CATALOG}


@dataclass(frozen=True, slots=True)
class DirectCreateImageType:
    key: str
    quantity: int
    order: int
    title: str | None = None
    aspect_ratio: str | None = None


def resolve_template_generation_spec(overrides: dict[str, Any] | None = None) -> dict[str, Any]:
    payload = dict(DEFAULT_TEMPLATE_GENERATION_SPEC)
    if overrides:
        payload.update(overrides)
    try:
        return GenerationSpec.model_validate(payload).model_dump(mode="json")
    except ValidationError as exc:
        raise BusinessValidationError("出图设定无效") from exc


def creative_brief_config_from_source_note(source_note: str | None) -> dict[str, Any]:
    text = source_note.strip() if isinstance(source_note, str) else ""
    if not text:
        return {}
    return {
        "goal": LISTING_LOOK_RULE,
        "design_goals": [f"商品与受众资料：{text[:3900]}"],
        "prohibitions": [
            "商品资料只作事实，不要把浅灰、静物、极简、干净当成套图画风",
            "不要极简大留白、浅灰空棚、杂志静物",
            "不要爆炸贴、满屏色块、牛皮癣标签",
        ],
    }


def build_product_source_create_graph(
    *,
    product_title: str,
    source_product_id: str,
    fact_set_version_id: str | None = None,
) -> WorkflowChangeSet:
    """名称-only 出生：一张只有商品资料节点的 live 图。"""
    return WorkflowChangeSet(
        base_graph_revision=0,
        summary="商品资料",
        actor_type=GraphActorType.USER,
        operations=[
            CreateNodeOp(
                client_ref="product-source",
                node_type=GraphNodeType.PRODUCT_SOURCE,
                title=product_title,
                position_x=80,
                position_y=220,
                config={
                    "source_product_id": source_product_id,
                    "fact_set_version_id": fact_set_version_id,
                },
            )
        ],
    )


def build_direct_create_template(
    *,
    image_types: list[DirectCreateImageType],
    reference_asset_ids: list[str],
    product_title: str = "商品资料",
    source_product_id: str | None = None,
    fact_set_version_id: str | None = None,
    source_note: str | None = None,
    generation_spec: dict[str, Any] | None = None,
    delivery_spec: dict[str, Any] | None = None,
) -> WorkflowChangeSet:
    """生成完整建图 ChangeSet。创建时上传绑定为 product_identity；证据图种是未绑定 image_asset。"""

    if not image_types:
        raise BusinessValidationError("至少选择一种图片类型")
    if not reference_asset_ids:
        raise BusinessValidationError("至少上传一张参考图")
    if len(reference_asset_ids) != len(set(reference_asset_ids)):
        raise BusinessValidationError("参考图资产不能重复")
    if len(reference_asset_ids) > WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS:
        raise BusinessValidationError(f"参考图不能超过 {WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS} 张")
    type_keys = [item.key for item in image_types]
    if len(type_keys) != len(set(type_keys)):
        raise BusinessValidationError("图片类型不能重复")
    generating_types = [item for item in image_types if image_type_family(item.key) != "evidence"]
    evidence_types = [item for item in image_types if image_type_family(item.key) == "evidence"]
    total_images = 0
    for item in generating_types:
        if item.quantity < WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE or item.quantity > WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE:
            raise BusinessValidationError(
                f"每种图片数量必须在 {WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE} 到 {WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE} 之间"
            )
        total_images += item.quantity
    if total_images > WORKFLOW_DRAFT_MAX_TOTAL_IMAGES:
        raise BusinessValidationError(f"图片生成总数不能超过 {WORKFLOW_DRAFT_MAX_TOTAL_IMAGES}")

    operations: list[GraphOperation] = [
        CreateNodeOp(
            client_ref="product-source",
            node_type=GraphNodeType.PRODUCT_SOURCE,
            title=product_title,
            position_x=80,
            position_y=220,
            config={
                "source_product_id": source_product_id,
                "fact_set_version_id": fact_set_version_id,
            },
        ),
        CreateNodeOp(
            client_ref="visual-system",
            node_type=GraphNodeType.VISUAL_SYSTEM,
            title="视觉规范",
            position_x=80,
            position_y=40,
            config={},
        ),
        CreateNodeOp(
            client_ref="creative-brief",
            node_type=GraphNodeType.CREATIVE_BRIEF,
            title="创作要求",
            position_x=80,
            position_y=400,
            config=creative_brief_config_from_source_note(source_note),
        ),
    ]
    identity_refs = [f"image-asset-{index + 1}" for index in range(len(reference_asset_ids))]
    for index, asset_id in enumerate(reference_asset_ids):
        operations.append(
            CreateNodeOp(
                client_ref=identity_refs[index],
                node_type=GraphNodeType.IMAGE_ASSET,
                title=f"参考图 {index + 1}",
                position_x=80 + index * 220,
                position_y=560,
                # 创建时上传绑定为 product_identity，该字符串会传给 prompt / image provider。
                config={"role": "product_identity"},
                bound_asset_id=asset_id,
            )
        )

    processing_refs: list[str] = []
    prompt_refs: list[str] = []
    ordered_generating = sorted(generating_types, key=lambda item: (item.order, item.key))
    for type_index, image_type in enumerate(ordered_generating):
        type_title = image_type.title or _IMAGE_TYPE_TITLES.get(image_type.key, image_type.key)
        type_option = agent_product_image_type_option(image_type.key)
        group_ref = f"shot-{image_type.key}"
        prompt_ref = f"prompt-{image_type.key}"
        prompt_refs.append(prompt_ref)
        processing_refs.append(prompt_ref)
        prompt_goal = image_type_prompt_goal(image_type.key) if type_option else type_title
        group_y = 40 + type_index * 280
        operations.append(CreateGroupOp(client_ref=group_ref, title=type_title, member_refs=()))
        operations.append(
            CreateNodeOp(
                client_ref=prompt_ref,
                node_type=GraphNodeType.PROMPT_GENERATION,
                title=f"{type_title}提示词",
                position_x=420,
                position_y=group_y,
                group_ref=group_ref,
                config={
                    "image_type_key": image_type.key,
                    "prompt": {"design_goal": prompt_goal},
                },
            )
        )
        for asset_index, identity_ref in enumerate(identity_refs):
            operations.append(
                ConnectNodesOp(
                    client_ref=f"edge-ref-{asset_index + 1}-{prompt_ref}",
                    source_ref=identity_ref,
                    target_ref=prompt_ref,
                    order=asset_index,
                )
            )
        type_generation_spec = _generation_spec_for_shot(image_type, generation_spec)
        for image_index in range(image_type.quantity):
            image_ref = f"image-{image_type.key}-{image_index + 1}"
            processing_refs.append(image_ref)
            image_config: dict[str, Any] = {
                "image_type_key": image_type.key,
                "generation_spec": dict(type_generation_spec),
            }
            if delivery_spec is not None:
                image_config["delivery_spec"] = dict(delivery_spec)
            operations.append(
                CreateNodeOp(
                    client_ref=image_ref,
                    node_type=GraphNodeType.IMAGE_GENERATION,
                    title=f"{type_title} {image_index + 1}",
                    position_x=760,
                    position_y=group_y + image_index * 90,
                    group_ref=group_ref,
                    config=image_config,
                )
            )
            operations.append(
                ConnectNodesOp(
                    client_ref=f"edge-prompt-{image_type.key}-{image_index + 1}",
                    source_ref=prompt_ref,
                    target_ref=image_ref,
                    order=image_index,
                )
            )
            for asset_index, identity_ref in enumerate(identity_refs):
                operations.append(
                    ConnectNodesOp(
                        client_ref=f"edge-ref-{asset_index + 1}-{image_ref}",
                        source_ref=identity_ref,
                        target_ref=image_ref,
                        order=asset_index,
                    )
                )

    # 证据图种落成未绑定 image_asset 占位，不是可运行 image_generation。
    for evidence_index, image_type in enumerate(sorted(evidence_types, key=lambda item: (item.order, item.key))):
        type_title = image_type.title or _IMAGE_TYPE_TITLES.get(image_type.key, image_type.key)
        operations.append(
            CreateNodeOp(
                client_ref=f"evidence-{image_type.key}",
                node_type=GraphNodeType.IMAGE_ASSET,
                title=f"{type_title}（待绑定）",
                position_x=80 + evidence_index * 220,
                position_y=760,
                config={"role": "evidence"},
            )
        )

    for order, prompt_ref in enumerate(prompt_refs):
        operations.append(
            ConnectNodesOp(
                client_ref=f"edge-facts-{prompt_ref}",
                source_ref="product-source",
                target_ref=prompt_ref,
                order=order,
            )
        )
        operations.append(
            ConnectNodesOp(
                client_ref=f"edge-brief-{prompt_ref}",
                source_ref="creative-brief",
                target_ref=prompt_ref,
                order=order,
            )
        )
    operations.append(
        ConnectNodesOp(
            client_ref="edge-facts-visual-system",
            source_ref="product-source",
            target_ref="visual-system",
            order=0,
        )
    )
    operations.append(
        ConnectNodesOp(
            client_ref="edge-facts-creative-brief",
            source_ref="product-source",
            target_ref="creative-brief",
            order=0,
        )
    )
    for asset_index, identity_ref in enumerate(identity_refs):
        operations.append(
            ConnectNodesOp(
                client_ref=f"edge-ref-{asset_index + 1}-visual-system",
                source_ref=identity_ref,
                target_ref="visual-system",
                order=asset_index,
            )
        )
        operations.append(
            ConnectNodesOp(
                client_ref=f"edge-ref-{asset_index + 1}-creative-brief",
                source_ref=identity_ref,
                target_ref="creative-brief",
                order=asset_index,
            )
        )
    for order, node_ref in enumerate(processing_refs):
        operations.append(
            ConnectNodesOp(
                client_ref=f"edge-visual-{node_ref}",
                source_ref="visual-system",
                target_ref=node_ref,
                order=order,
            )
        )

    return WorkflowChangeSet(
        base_graph_revision=0,
        summary="直接创建：按构思表单生成预设工作流模版",
        actor_type=GraphActorType.USER,
        operations=operations,
    )


def template_for_existing_product_source(
    *,
    product_source_node_id: str,
    base_graph_revision: int,
    image_types: list[DirectCreateImageType],
    reference_asset_ids: list[str],
    product_title: str = "商品资料",
    source_product_id: str | None = None,
    fact_set_version_id: str | None = None,
    source_note: str | None = None,
    generation_spec: dict[str, Any] | None = None,
    delivery_spec: dict[str, Any] | None = None,
) -> WorkflowChangeSet:
    """把名称-only 出生图扩成与直接创建相同的模板，复用已有商品资料节点。"""

    change_set = build_direct_create_template(
        image_types=image_types,
        reference_asset_ids=reference_asset_ids,
        product_title=product_title,
        source_product_id=source_product_id,
        fact_set_version_id=fact_set_version_id,
        source_note=source_note,
        generation_spec=generation_spec,
        delivery_spec=delivery_spec,
    )
    operations: list[GraphOperation] = []
    for operation in change_set.operations:
        if isinstance(operation, CreateNodeOp) and operation.client_ref == "product-source":
            continue
        if isinstance(operation, ConnectNodesOp) and operation.source_ref == "product-source":
            operations.append(operation.model_copy(update={"source_ref": product_source_node_id}))
            continue
        operations.append(operation)
    return WorkflowChangeSet(
        base_graph_revision=base_graph_revision,
        summary="按商品输入补全画布",
        actor_type=GraphActorType.USER,
        operations=operations,
    )


def _generation_spec_for_shot(
    image_type: DirectCreateImageType,
    generation_spec: dict[str, Any] | None,
) -> dict[str, Any]:
    overrides = dict(generation_spec or {})
    if image_type.aspect_ratio:
        overrides["aspect_ratio"] = image_type.aspect_ratio
    if image_type_family(image_type.key) == "infographic" and "text_policy" not in (generation_spec or {}):
        overrides["text_policy"] = "required"
        overrides.setdefault("text_language", "zh-CN")
    return resolve_template_generation_spec(overrides)
