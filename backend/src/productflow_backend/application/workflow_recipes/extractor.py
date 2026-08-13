from __future__ import annotations

from collections.abc import Collection, Mapping, Sequence
from typing import Any, Literal

from pydantic import ValidationError

from productflow_backend.application.workflow_drafts.contracts import (
    ImageGenerationNodePlan,
    PromptGenerationNodePlan,
    ReferenceImageNodePlan,
    VisualSystemDraftPayload,
    WorkflowDraftPayloadV1,
)
from productflow_backend.application.workflow_recipes.contracts import (
    RecipeBoundaryRequirement,
    RecipeEdge,
    RecipeFolder,
    RecipeImageGenerationNode,
    RecipeImageType,
    RecipePayloadV1,
    RecipePlannedImage,
    RecipeProductContextNode,
    RecipePromptGenerationNode,
    RecipePromptShape,
    RecipeReferenceImageNode,
    RecipeReferenceRequirement,
    RecipeVisualRequirements,
    recipe_payload_dict,
    recipe_payload_json,
)
from productflow_backend.domain.enums import WorkflowNodeType
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import ProductWorkflow, WorkflowEdge, WorkflowNode

RecipeSourceType = Literal["workflow", "folder", "selection"]

_NODE_TYPE_ORDER = {
    WorkflowNodeType.PRODUCT_CONTEXT: 0,
    WorkflowNodeType.REFERENCE_IMAGE: 1,
    WorkflowNodeType.PROMPT_GENERATION: 2,
    WorkflowNodeType.IMAGE_GENERATION: 3,
}
_FORBIDDEN_EXACT_KEYS = {
    "product_id",
    "workflow_id",
    "node_id",
    "edge_id",
    "folder_id",
    "draft_id",
    "run_id",
    "source_draft_revision_id",
    "fact_set_version_id",
    "prompt_artifact_id",
    "prompt_artifact_version_id",
    "current_prompt_artifact_version_id",
    "cover_image_id",
    "cover_image_asset_id",
    "output",
    "output_json",
    "status",
    "failure",
    "failure_reason",
    "history",
    "provider",
    "provider_name",
    "provider_model",
    "provider_response_id",
}
_FORBIDDEN_KEY_MARKERS = ("output", "failure", "history", "provider", "cover")


def extract_recipe_payload(
    *,
    workflow: ProductWorkflow,
    source_artifact: WorkflowDraftPayloadV1,
    visual_system_payload: VisualSystemDraftPayload,
    source_type: RecipeSourceType,
    folder_id: str | None,
    node_ids: Sequence[str],
    forbidden_entity_ids: Collection[str],
) -> RecipePayloadV1:
    selected_nodes = _select_nodes(
        workflow,
        source_type=source_type,
        folder_id=folder_id,
        node_ids=node_ids,
    )
    sorted_nodes = sorted(
        selected_nodes,
        key=lambda node: (
            node.position_x,
            node.position_y,
            _NODE_TYPE_ORDER[node.node_type],
            node.node_key or "",
            node.id,
        ),
    )
    node_key_map = {node.id: f"node_{index}" for index, node in enumerate(sorted_nodes, start=1)}
    selected_ids = set(node_key_map)
    min_x = min(node.position_x for node in sorted_nodes)
    min_y = min(node.position_y for node in sorted_nodes)

    source_node_plans = {node.key: node for node in source_artifact.nodes}
    source_image_types = {image_type.key: image_type for image_type in source_artifact.image_types}
    source_prompts = {prompt.key: prompt for prompt in source_artifact.prompt_plans}
    runtime_by_id = {node.id: node for node in workflow.nodes}
    runtime_plan_by_id = {
        node.id: _source_node_plan(node, source_node_plans)
        for node in workflow.nodes
    }

    selected_folder_ids = sorted(
        {node.folder_id for node in sorted_nodes if node.folder_id is not None},
        key=lambda candidate_id: _folder_order(workflow, candidate_id),
    )
    folder_key_map = {
        candidate_id: f"folder_{index}"
        for index, candidate_id in enumerate(selected_folder_ids, start=1)
    }

    relevant_type_keys, selected_image_plan_keys = _relevant_image_plans(
        sorted_nodes,
        runtime_plan_by_id,
        source_image_types=source_image_types,
    )
    type_key_map = {
        type_key: f"image_type_{index}"
        for index, type_key in enumerate(
            sorted(relevant_type_keys, key=lambda key: (source_image_types[key].order, key)),
            start=1,
        )
    }
    image_key_map: dict[str, str] = {}
    image_counter = 0
    recipe_image_types: list[RecipeImageType] = []
    prompt_shapes: list[RecipePromptShape] = []
    for source_type_key in sorted(
        relevant_type_keys,
        key=lambda key: (source_image_types[key].order, key),
    ):
        image_type = source_image_types[source_type_key]
        included_images = [
            image
            for image in sorted(image_type.images, key=lambda item: (item.order, item.key))
            if image.key in selected_image_plan_keys[source_type_key]
        ]
        recipe_images: list[RecipePlannedImage] = []
        for new_order, image in enumerate(included_images):
            image_counter += 1
            recipe_key = f"image_{image_counter}"
            image_key_map[image.key] = recipe_key
            recipe_images.append(
                RecipePlannedImage(
                    key=recipe_key,
                    order=new_order,
                    generation_spec=image.generation_spec,
                    delivery_spec=image.delivery_spec,
                )
            )
        recipe_image_types.append(
            RecipeImageType(
                key=type_key_map[source_type_key],
                title=image_type.title,
                order=len(recipe_image_types),
                default_quantity=len(recipe_images),
                images=recipe_images,
            )
        )
        prompt = source_prompts[image_type.prompt_plan_key]
        prompt_shapes.append(
            RecipePromptShape(
                image_type_key=type_key_map[source_type_key],
                product_present=prompt.payload.product_fidelity.product_present,
                picture_in_picture=prompt.payload.product_fidelity.picture_in_picture,
                product_share_percent=prompt.payload.composition.product_share_percent,
                text_slots=[
                    field_name
                    for field_name in ("headline", "subtitle", "body")
                    if getattr(prompt.payload.text, field_name) is not None
                ],
                fact_keys=prompt.payload.fact_keys,
                visual_variant_key=prompt.payload.visual_variant_key,
                per_image_slots=[
                    {
                        "image_plan_key": image_key_map[source_image.key],
                        "viewpoint": source_prompt_image.viewpoint is not None,
                        "composition_adjustments": bool(source_prompt_image.composition_adjustments),
                        "lighting": source_prompt_image.lighting is not None,
                    }
                    for source_image in included_images
                    for source_prompt_image in prompt.payload.images
                    if source_prompt_image.image_plan_key == source_image.key
                ],
            )
        )

    boundary_edges = [
        edge
        for edge in workflow.edges
        if (edge.source_node_id in selected_ids) != (edge.target_node_id in selected_ids)
    ]
    reference_nodes = _reference_requirement_nodes(
        sorted_nodes,
        boundary_edges=boundary_edges,
        runtime_by_id=runtime_by_id,
    )
    reference_key_by_node_id: dict[str, str] = {}
    reference_requirements: list[RecipeReferenceRequirement] = []
    for index, node in enumerate(reference_nodes, start=1):
        requirement_key = f"reference_{index}"
        reference_key_by_node_id[node.id] = requirement_key
        reference_requirements.append(
            RecipeReferenceRequirement(
                key=requirement_key,
                role=_config_text(node, "role", fallback="product_reference"),
                label=_config_text(node, "label", fallback="商品参考图"),
                required=True,
            )
        )

    recipe_nodes = []
    for node in sorted_nodes:
        plan = runtime_plan_by_id[node.id]
        common = {
            "key": node_key_map[node.id],
            "position_x": node.position_x - min_x,
            "position_y": node.position_y - min_y,
            "folder_key": folder_key_map.get(node.folder_id),
        }
        if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT:
            recipe_nodes.append(RecipeProductContextNode(**common))
        elif isinstance(plan, ReferenceImageNodePlan):
            recipe_nodes.append(
                RecipeReferenceImageNode(
                    **common,
                    reference_requirement_key=reference_key_by_node_id[node.id],
                )
            )
        elif isinstance(plan, PromptGenerationNodePlan):
            source_prompt = source_prompts[plan.prompt_plan_key]
            recipe_nodes.append(
                RecipePromptGenerationNode(
                    **common,
                    image_type_key=type_key_map[source_prompt.image_type_key],
                )
            )
        elif isinstance(plan, ImageGenerationNodePlan):
            source_type_key = _image_type_key_for_plan(plan.image_plan_key, source_image_types)
            recipe_nodes.append(
                RecipeImageGenerationNode(
                    **common,
                    image_type_key=type_key_map[source_type_key],
                    image_plan_key=image_key_map[plan.image_plan_key],
                )
            )
        else:
            raise BusinessValidationError("配方提取遇到不支持的 schema-v2 节点")

    internal_edges = sorted(
        (
            edge
            for edge in workflow.edges
            if edge.source_node_id in selected_ids and edge.target_node_id in selected_ids
        ),
        key=lambda edge: (edge.edge_key or "", edge.id),
    )
    recipe_edges = [
        RecipeEdge(
            key=f"edge_{index}",
            source_node_key=node_key_map[edge.source_node_id],
            target_node_key=node_key_map[edge.target_node_id],
            source_handle=edge.source_handle,
            target_handle=edge.target_handle,
        )
        for index, edge in enumerate(internal_edges, start=1)
    ]
    boundaries = [
        _boundary_requirement(
            edge,
            index=index,
            node_key_map=node_key_map,
            runtime_by_id=runtime_by_id,
        )
        for index, edge in enumerate(
            sorted(boundary_edges, key=lambda item: (item.edge_key or "", item.id)),
            start=1,
        )
    ]

    folders = [
        RecipeFolder(
            key=folder_key_map[source_folder_id],
            title=_folder_recipe_title(
                source_folder_id,
                sorted_nodes=sorted_nodes,
                runtime_plan_by_id=runtime_plan_by_id,
                source_image_types=source_image_types,
                source_prompts=source_prompts,
                fallback_index=index,
            ),
            order=index - 1,
        )
        for index, source_folder_id in enumerate(selected_folder_ids, start=1)
    ]
    try:
        payload = RecipePayloadV1(
            folders=folders,
            nodes=recipe_nodes,
            edges=recipe_edges,
            boundary_requirements=boundaries,
            reference_requirements=reference_requirements,
            image_types=recipe_image_types,
            prompt_shapes=prompt_shapes,
            visual_requirements=RecipeVisualRequirements(
                required_locked_fields=visual_system_payload.locked_fields,
                required_variant_keys=[variant.key for variant in visual_system_payload.variants],
            ),
        )
        serialized = recipe_payload_dict(payload)
        assert_recipe_payload_has_no_forbidden_keys(serialized)
        assert_recipe_payload_has_no_entity_ids(serialized, forbidden_entity_ids)
        recipe_payload_json(payload)
        return payload
    except (ValidationError, ValueError) as exc:
        raise BusinessValidationError(f"无法构造安全的工作流配方: {exc}") from exc


def assert_recipe_payload_has_no_forbidden_keys(value: Any, *, path: str = "$") -> None:
    if isinstance(value, Mapping):
        for key, child in value.items():
            normalized_key = str(key).lower()
            if (
                normalized_key in _FORBIDDEN_EXACT_KEYS
                or normalized_key.endswith("_asset_id")
                or normalized_key.endswith("_asset_ids")
                or any(marker in normalized_key for marker in _FORBIDDEN_KEY_MARKERS)
            ):
                raise BusinessValidationError(f"配方包含禁止字段 {path}.{key}")
            assert_recipe_payload_has_no_forbidden_keys(child, path=f"{path}.{key}")
    elif isinstance(value, list):
        for index, child in enumerate(value):
            assert_recipe_payload_has_no_forbidden_keys(child, path=f"{path}[{index}]")


def assert_recipe_payload_has_no_entity_ids(value: Any, entity_ids: Collection[str]) -> None:
    forbidden_ids = tuple(sorted({entity_id for entity_id in entity_ids if entity_id}, key=len, reverse=True))

    def visit(child: Any, path: str) -> None:
        if isinstance(child, Mapping):
            for key, nested in child.items():
                visit(nested, f"{path}.{key}")
        elif isinstance(child, list):
            for index, nested in enumerate(child):
                visit(nested, f"{path}[{index}]")
        elif isinstance(child, str):
            leaked = next((entity_id for entity_id in forbidden_ids if entity_id in child), None)
            if leaked is not None:
                raise BusinessValidationError(f"配方值 {path} 泄漏当前实体 ID")

    visit(value, "$")


def _select_nodes(
    workflow: ProductWorkflow,
    *,
    source_type: RecipeSourceType,
    folder_id: str | None,
    node_ids: Sequence[str],
) -> list[WorkflowNode]:
    workflow_nodes = {node.id: node for node in workflow.nodes}
    if source_type == "workflow":
        if folder_id is not None or node_ids:
            raise BusinessValidationError("完整工作流来源不能同时指定 folder_id 或 node_ids")
        selected = list(workflow.nodes)
    elif source_type == "folder":
        if folder_id is None or node_ids:
            raise BusinessValidationError("文件夹来源必须且只能指定 folder_id")
        if not any(folder.id == folder_id for folder in workflow.folders):
            raise NotFoundError("工作流文件夹不存在")
        selected = [node for node in workflow.nodes if node.folder_id == folder_id]
    elif source_type == "selection":
        if folder_id is not None or not node_ids:
            raise BusinessValidationError("多选来源必须且只能指定非空 node_ids")
        if len(node_ids) != len(set(node_ids)):
            raise BusinessValidationError("配方来源 node_ids 不能重复")
        if not set(node_ids).issubset(workflow_nodes):
            raise NotFoundError("配方来源节点不存在或不属于当前工作流")
        selected = [workflow_nodes[node_id] for node_id in node_ids]
    else:
        raise BusinessValidationError("不支持的配方来源类型")
    if not selected:
        raise BusinessValidationError("配方来源必须至少包含一个节点")
    if any(node.schema_version != 2 or node.node_key is None for node in selected):
        raise BusinessValidationError("配方只能从 schema-v2 typed nodes 提取")
    return selected


def _source_node_plan(node: WorkflowNode, source_node_plans: Mapping[str, Any]):
    if node.node_key is None or node.node_key not in source_node_plans:
        raise BusinessValidationError("schema-v2 节点无法追溯到 source Draft")
    plan = source_node_plans[node.node_key]
    if plan.node_type != node.node_type:
        raise BusinessValidationError("schema-v2 节点类型与 source Draft 不一致")
    return plan


def _relevant_image_plans(
    selected_nodes: Sequence[WorkflowNode],
    runtime_plan_by_id: Mapping[str, Any],
    *,
    source_image_types: Mapping[str, Any],
) -> tuple[set[str], dict[str, set[str]]]:
    selected_images_by_type: dict[str, set[str]] = {}
    prompt_type_keys: set[str] = set()
    for node in selected_nodes:
        plan = runtime_plan_by_id[node.id]
        if isinstance(plan, PromptGenerationNodePlan):
            prompt_type_key = _prompt_image_type_key(plan.prompt_plan_key, source_image_types)
            prompt_type_keys.add(prompt_type_key)
        elif isinstance(plan, ImageGenerationNodePlan):
            type_key = _image_type_key_for_plan(plan.image_plan_key, source_image_types)
            selected_images_by_type.setdefault(type_key, set()).add(plan.image_plan_key)
    relevant = set(selected_images_by_type) | prompt_type_keys
    included: dict[str, set[str]] = {}
    for type_key in relevant:
        included[type_key] = selected_images_by_type.get(type_key) or {
            image.key for image in source_image_types[type_key].images
        }
    return relevant, included


def _prompt_image_type_key(prompt_plan_key: str, source_image_types: Mapping[str, Any]) -> str:
    matches = [
        image_type.key
        for image_type in source_image_types.values()
        if image_type.prompt_plan_key == prompt_plan_key
    ]
    if len(matches) != 1:
        raise BusinessValidationError("source Draft 提示词计划无法映射到唯一图片类型")
    return matches[0]


def _image_type_key_for_plan(image_plan_key: str, source_image_types: Mapping[str, Any]) -> str:
    matches = [
        image_type.key
        for image_type in source_image_types.values()
        if any(image.key == image_plan_key for image in image_type.images)
    ]
    if len(matches) != 1:
        raise BusinessValidationError("source Draft 图片计划无法映射到唯一图片类型")
    return matches[0]


def _reference_requirement_nodes(
    selected_nodes: Sequence[WorkflowNode],
    *,
    boundary_edges: Sequence[WorkflowEdge],
    runtime_by_id: Mapping[str, WorkflowNode],
) -> list[WorkflowNode]:
    candidates = {
        node.id: node
        for node in selected_nodes
        if node.node_type == WorkflowNodeType.REFERENCE_IMAGE
    }
    for edge in boundary_edges:
        for node_id in (edge.source_node_id, edge.target_node_id):
            node = runtime_by_id[node_id]
            if node.node_type == WorkflowNodeType.REFERENCE_IMAGE:
                candidates[node.id] = node
    return sorted(
        candidates.values(),
        key=lambda node: (node.position_x, node.position_y, node.node_key or "", node.id),
    )


def _boundary_requirement(
    edge: WorkflowEdge,
    *,
    index: int,
    node_key_map: Mapping[str, str],
    runtime_by_id: Mapping[str, WorkflowNode],
) -> RecipeBoundaryRequirement:
    inbound = edge.target_node_id in node_key_map
    local_node_id = edge.target_node_id if inbound else edge.source_node_id
    external_node_id = edge.source_node_id if inbound else edge.target_node_id
    external_node = runtime_by_id[external_node_id]
    role = (
        _config_text(external_node, "role", fallback="")
        if external_node.node_type == WorkflowNodeType.REFERENCE_IMAGE
        else ""
    )
    if not role:
        role = (
            (edge.target_handle or edge.source_handle)
            if inbound
            else (edge.source_handle or edge.target_handle)
        ) or f"{external_node.node_type.value}_dependency"
    return RecipeBoundaryRequirement(
        key=f"boundary_{index}",
        direction="inbound" if inbound else "outbound",
        local_node_key=node_key_map[local_node_id],
        external_node_type=external_node.node_type.value,
        role=role,
    )


def _folder_order(workflow: ProductWorkflow, folder_id: str) -> tuple[int, str]:
    folder = next((candidate for candidate in workflow.folders if candidate.id == folder_id), None)
    if folder is None:
        raise BusinessValidationError("节点引用了不存在的 runtime 文件夹")
    return folder.sort_order, folder.id


def _folder_recipe_title(
    folder_id: str,
    *,
    sorted_nodes: Sequence[WorkflowNode],
    runtime_plan_by_id: Mapping[str, Any],
    source_image_types: Mapping[str, Any],
    source_prompts: Mapping[str, Any],
    fallback_index: int,
) -> str:
    titles: list[str] = []
    has_product_context = False
    has_reference = False
    for node in sorted_nodes:
        if node.folder_id != folder_id:
            continue
        plan = runtime_plan_by_id[node.id]
        if isinstance(plan, PromptGenerationNodePlan):
            titles.append(source_image_types[source_prompts[plan.prompt_plan_key].image_type_key].title)
        elif isinstance(plan, ImageGenerationNodePlan):
            titles.append(source_image_types[_image_type_key_for_plan(plan.image_plan_key, source_image_types)].title)
        elif isinstance(plan, ReferenceImageNodePlan):
            has_reference = True
        else:
            has_product_context = True
    unique_titles = list(dict.fromkeys(titles))
    if len(unique_titles) == 1:
        return unique_titles[0]
    if len(unique_titles) > 1:
        return " / ".join(unique_titles)[:255]
    if has_product_context:
        return "商品信息"
    if has_reference:
        return "参考素材"
    return f"节点组 {fallback_index}"


def _config_text(node: WorkflowNode, key: str, *, fallback: str) -> str:
    value = node.config_json.get(key) if isinstance(node.config_json, dict) else None
    if isinstance(value, str) and value.strip():
        return value.strip()[:255]
    return fallback


__all__ = [
    "RecipeSourceType",
    "assert_recipe_payload_has_no_entity_ids",
    "assert_recipe_payload_has_no_forbidden_keys",
    "extract_recipe_payload",
]
