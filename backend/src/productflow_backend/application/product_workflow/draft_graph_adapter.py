from __future__ import annotations

from typing import Any

from productflow_backend.application.product_workflow.graph_contracts import (
    ConnectNodesOp,
    CreateGroupOp,
    CreateNodeOp,
    GraphOperation,
    WorkflowChangeSet,
)
from productflow_backend.application.product_workflow.graph_visual import visual_overlay_from_config
from productflow_backend.application.workflow_drafts.contracts import (
    ImageGenerationNodePlan,
    PromptGenerationNodePlan,
    ReferenceImageNodePlan,
    WorkflowDraftPayloadV1,
)
from productflow_backend.domain.enums import GraphActorType, GraphNodeType, WorkflowNodeType
from productflow_backend.domain.errors import StructuredBusinessValidationError
from productflow_backend.domain.graph_catalog import graph_input_contract

_V2_TO_V3_NODE_TYPE = {
    WorkflowNodeType.PRODUCT_CONTEXT: GraphNodeType.PRODUCT_SOURCE,
    WorkflowNodeType.REFERENCE_IMAGE: GraphNodeType.IMAGE_ASSET,
    WorkflowNodeType.PROMPT_GENERATION: GraphNodeType.PROMPT_GENERATION,
    WorkflowNodeType.IMAGE_GENERATION: GraphNodeType.IMAGE_GENERATION,
}


def build_draft_initial_graph_change_set(
    payload: WorkflowDraftPayloadV1,
    *,
    draft_revision_id: str,
    visual_system_version_id: str | None = None,
) -> WorkflowChangeSet:
    operations: list[GraphOperation] = []
    references = {item.key: item for item in payload.reference_bindings}
    prompts = {item.key: item for item in payload.prompt_plans}
    images = {image.key: image for image_type in payload.image_types for image in image_type.images}
    image_types = {image.key: image_type for image_type in payload.image_types for image in image_type.images}

    operations.append(
        CreateNodeOp(
            client_ref="visual-system",
            node_type=GraphNodeType.VISUAL_SYSTEM,
            title="视觉规范",
            position_x=80,
            position_y=40,
            config=_visual_system_config(payload, visual_system_version_id=visual_system_version_id),
        )
    )
    operations.append(
        CreateNodeOp(
            client_ref="creative-brief",
            node_type=GraphNodeType.CREATIVE_BRIEF,
            title="创作要求",
            position_x=80,
            position_y=400,
            config=_creative_brief_config(payload),
        )
    )

    for folder in payload.folders:
        operations.append(CreateGroupOp(client_ref=folder.key, title=folder.title, member_refs=()))

    folder_keys = {folder.key for folder in payload.folders}
    for node in payload.nodes:
        v3_type = _V2_TO_V3_NODE_TYPE[node.node_type]
        config, bound_asset_id = _node_config(node, payload, references, prompts, images, image_types)
        operations.append(
            CreateNodeOp(
                client_ref=node.key,
                node_type=v3_type,
                title=node.title,
                position_x=node.position_x,
                position_y=node.position_y,
                config=config,
                bound_asset_id=bound_asset_id,
                group_ref=node.folder_key if node.folder_key in folder_keys else None,
            )
        )

    prompt_and_image_refs = [
        node.key
        for node in payload.nodes
        if node.node_type in {WorkflowNodeType.PROMPT_GENERATION, WorkflowNodeType.IMAGE_GENERATION}
    ]
    for order, node_ref in enumerate(prompt_and_image_refs):
        operations.append(
            ConnectNodesOp(
                client_ref=f"edge-visual-{node_ref}",
                source_ref="visual-system",
                target_ref=node_ref,
                order=order,
            )
        )
    prompt_refs = [node.key for node in payload.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION]
    for order, node_ref in enumerate(prompt_refs):
        operations.append(
            ConnectNodesOp(
                client_ref=f"edge-brief-{node_ref}",
                source_ref="creative-brief",
                target_ref=node_ref,
                order=order,
            )
        )

    nodes_by_key = {node.key: node for node in payload.nodes}
    edge_order: dict[tuple[str, str], int] = {}
    for edge in payload.edges:
        source = nodes_by_key[edge.source_node_key]
        target = nodes_by_key[edge.target_node_key]
        source_type = _V2_TO_V3_NODE_TYPE[source.node_type]
        target_type = _V2_TO_V3_NODE_TYPE[target.node_type]
        if (
            source.node_type == WorkflowNodeType.PRODUCT_CONTEXT
            and target.node_type == WorkflowNodeType.IMAGE_GENERATION
        ):
            continue
        if graph_input_contract(source_type, target_type) is None:
            raise StructuredBusinessValidationError(
                "WorkflowDraft 包含无法映射到 v3 的连线",
                code="draft_graph_edge_unsupported",
                issues=[{"path": f"edges.{edge.key}", "message": "节点类型在 v3 中不能连接"}],
            )
        key = (edge.source_node_key, edge.target_node_key)
        order = edge_order.get(key, 0)
        edge_order[key] = order + 1
        operations.append(
            ConnectNodesOp(
                client_ref=edge.key,
                source_ref=edge.source_node_key,
                target_ref=edge.target_node_key,
                order=order,
            )
        )

    _assert_image_nodes_have_prompt(payload)

    return WorkflowChangeSet(
        base_graph_revision=0,
        summary=f"确认 Draft revision {draft_revision_id} 的初始图",
        actor_type=GraphActorType.USER,
        operations=operations,
    )


def _visual_system_config(
    payload: WorkflowDraftPayloadV1,
    *,
    visual_system_version_id: str | None = None,
) -> dict[str, object]:
    version_id = visual_system_version_id
    if not version_id and payload.visual_system.mode == "confirmed_version":
        version_id = payload.visual_system.version_id
    config: dict[str, Any] = {}
    if version_id:
        config["visual_system_version_id"] = version_id
    workflow_exceptions = [
        exception.model_dump(mode="json")
        for exception in payload.visual_exceptions
        if exception.scope.type == "workflow"
    ]
    if workflow_exceptions:
        config["visual_overrides"] = workflow_exceptions
        overlay = visual_overlay_from_config(config)
        if overlay:
            config["visual_overlay"] = overlay
    return config


def _creative_brief_config(payload: WorkflowDraftPayloadV1) -> dict[str, object]:
    goals = [plan.payload.design_goal for plan in payload.prompt_plans]
    prohibitions: list[str] = []
    required_copy: list[str] = []
    for plan in payload.prompt_plans:
        prohibitions.extend(plan.payload.creative_boundary)
        text = plan.payload.text
        for value in (text.headline, text.subtitle, text.body):
            if value:
                required_copy.append(value)
    return {
        "design_goals": goals,
        "required_copy": required_copy,
        "prohibitions": prohibitions,
        "goal": goals[0] if goals else "",
        "title": payload.prompt_plans[0].title if payload.prompt_plans else "",
    }


def _node_config(node, payload, references, prompts, images, image_types) -> tuple[dict[str, object], str | None]:
    if isinstance(node, ReferenceImageNodePlan):
        binding = references[node.reference_key]
        return {"label": binding.label, "role": binding.role}, binding.asset_id
    if isinstance(node, PromptGenerationNodePlan):
        prompt = prompts[node.prompt_plan_key]
        dumped = prompt.payload.model_dump(mode="json")
        dumped.pop("images", None)
        dumped.pop("fact_keys", None)
        dumped.pop("evidence_asset_ids", None)
        return {"image_type_key": prompt.image_type_key, "prompt": dumped}, None
    if isinstance(node, ImageGenerationNodePlan):
        image = images[node.image_plan_key]
        image_type = image_types[node.image_plan_key]
        overrides = [
            exception.model_dump(mode="json")
            for exception in payload.visual_exceptions
            if (exception.scope.type == "image_plan" and exception.scope.key == image.key)
            or (exception.scope.type == "image_type" and exception.scope.key == image_type.key)
        ]
        image_config: dict[str, Any] = {
            "image_type_key": image_type.key,
            "variation_instruction": image.variation_instruction,
            "generation_spec": image.generation_spec.model_dump(mode="json"),
            "delivery_spec": image.delivery_spec.model_dump(mode="json") if image.delivery_spec else None,
        }
        if overrides:
            image_config["visual_overrides"] = overrides
            overlay = visual_overlay_from_config(image_config)
            if overlay:
                image_config["visual_overlay"] = overlay
        return image_config, None
    return {}, None


def _assert_image_nodes_have_prompt(payload: WorkflowDraftPayloadV1) -> None:
    nodes_by_key = {node.key: node for node in payload.nodes}
    image_keys = {node.key for node in payload.nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION}
    prompted = set()
    for edge in payload.edges:
        source = nodes_by_key[edge.source_node_key]
        if source.node_type == WorkflowNodeType.PROMPT_GENERATION and edge.target_node_key in image_keys:
            prompted.add(edge.target_node_key)
    missing = sorted(image_keys - prompted)
    if missing:
        raise StructuredBusinessValidationError(
            "图片节点缺少提示词边，不能映射到 v3",
            code="draft_graph_image_missing_prompt",
            issues=[{"path": f"nodes.{key}", "message": "v3 图片节点必须有 prompt 边"} for key in missing[:8]],
        )
