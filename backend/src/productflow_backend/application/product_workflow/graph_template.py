from __future__ import annotations

from dataclasses import dataclass

from productflow_backend.application.agent.product_intake import AGENT_PRODUCT_IMAGE_TYPE_CATALOG
from productflow_backend.application.product_workflow.graph_contracts import (
    ConnectNodesOp,
    CreateNodeOp,
    WorkflowChangeSet,
)
from productflow_backend.application.workflow_drafts.contracts import (
    WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE,
    WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS,
    WORKFLOW_DRAFT_MAX_TOTAL_IMAGES,
    WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE,
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


def build_direct_create_template(
    *,
    image_types: list[DirectCreateImageType],
    reference_asset_ids: list[str],
    product_title: str = "商品资料",
    source_product_id: str | None = None,
    fact_set_version_id: str | None = None,
) -> WorkflowChangeSet:
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
    total_images = 0
    for item in image_types:
        if item.quantity < WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE or item.quantity > WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE:
            raise BusinessValidationError(
                f"每种图片数量必须在 {WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE} 到 {WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE} 之间"
            )
        total_images += item.quantity
    if total_images > WORKFLOW_DRAFT_MAX_TOTAL_IMAGES:
        raise BusinessValidationError(f"图片生成总数不能超过 {WORKFLOW_DRAFT_MAX_TOTAL_IMAGES}")

    operations: list[CreateNodeOp | ConnectNodesOp] = [
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
            config={},
        ),
    ]
    for index, asset_id in enumerate(reference_asset_ids):
        operations.append(
            CreateNodeOp(
                client_ref=f"image-asset-{index + 1}",
                node_type=GraphNodeType.IMAGE_ASSET,
                title=f"参考图 {index + 1}",
                position_x=80 + index * 220,
                position_y=560,
                bound_asset_id=asset_id,
            )
        )

    processing_refs: list[str] = []
    prompt_refs: list[str] = []
    ordered_types = sorted(image_types, key=lambda item: (item.order, item.key))
    for type_index, image_type in enumerate(ordered_types):
        type_title = image_type.title or _IMAGE_TYPE_TITLES.get(image_type.key, image_type.key)
        prompt_ref = f"prompt-{image_type.key}"
        prompt_refs.append(prompt_ref)
        processing_refs.append(prompt_ref)
        operations.append(
            CreateNodeOp(
                client_ref=prompt_ref,
                node_type=GraphNodeType.PROMPT_GENERATION,
                title=f"{type_title}提示词",
                position_x=420,
                position_y=40 + type_index * 200,
                config={"image_type_key": image_type.key},
            )
        )
        for image_index in range(image_type.quantity):
            image_ref = f"image-{image_type.key}-{image_index + 1}"
            processing_refs.append(image_ref)
            operations.append(
                CreateNodeOp(
                    client_ref=image_ref,
                    node_type=GraphNodeType.IMAGE_GENERATION,
                    title=f"{type_title} {image_index + 1}",
                    position_x=760,
                    position_y=40 + type_index * 200 + image_index * 90,
                    config={
                        "image_type_key": image_type.key,
                        "generation_spec": dict(DEFAULT_TEMPLATE_GENERATION_SPEC),
                    },
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
