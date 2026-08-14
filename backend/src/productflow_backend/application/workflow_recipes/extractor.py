from __future__ import annotations

import hashlib
import json
from collections.abc import Collection, Mapping, Sequence
from dataclasses import dataclass
from typing import Any, Literal

from pydantic import ValidationError

from productflow_backend.application.workflow_drafts.contracts import (
    DeliverySpec,
    GenerationSpec,
    ImagePromptPayloadV1,
    VisualSystemDraftPayload,
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
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
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


@dataclass(frozen=True, slots=True)
class _RuntimePromptGroup:
    prompt_plan_key: str
    image_type_key: str
    title: str
    order: int
    prompt_node: WorkflowNode
    image_nodes: tuple[WorkflowNode, ...]
    prompt_payload: ImagePromptPayloadV1


def extract_recipe_payload(
    *,
    workflow: ProductWorkflow,
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

    runtime_by_id = {node.id: node for node in workflow.nodes}
    runtime_groups = _runtime_prompt_groups(workflow)
    group_by_prompt_node_id = {group.prompt_node.id: group for group in runtime_groups}
    group_by_image_node_id = {
        node.id: group
        for group in runtime_groups
        for node in group.image_nodes
    }

    selected_folder_ids = sorted(
        {node.folder_id for node in sorted_nodes if node.folder_id is not None},
        key=lambda candidate_id: _folder_order(workflow, candidate_id),
    )
    folder_key_map = {
        candidate_id: f"folder_{index}"
        for index, candidate_id in enumerate(selected_folder_ids, start=1)
    }

    relevant_group_keys, selected_image_plan_keys = _relevant_runtime_image_plans(
        sorted_nodes,
        group_by_prompt_node_id=group_by_prompt_node_id,
        group_by_image_node_id=group_by_image_node_id,
    )
    ordered_groups = [
        group
        for group in runtime_groups
        if group.prompt_plan_key in relevant_group_keys
    ]
    type_key_map = {
        group.prompt_plan_key: f"image_type_{index}"
        for index, group in enumerate(ordered_groups, start=1)
    }
    image_key_map: dict[str, str] = {}
    image_counter = 0
    recipe_image_types: list[RecipeImageType] = []
    prompt_shapes: list[RecipePromptShape] = []
    for group in ordered_groups:
        included_images = [
            image_node
            for image_node in group.image_nodes
            if _required_config_text(image_node, "image_plan_key", label="图片节点")
            in selected_image_plan_keys[group.prompt_plan_key]
        ]
        recipe_images: list[RecipePlannedImage] = []
        for new_order, image_node in enumerate(included_images):
            image_counter += 1
            recipe_key = f"image_{image_counter}"
            image_plan_key = _required_config_text(image_node, "image_plan_key", label="图片节点")
            image_key_map[image_plan_key] = recipe_key
            recipe_images.append(
                RecipePlannedImage(
                    key=recipe_key,
                    order=new_order,
                    generation_spec=_runtime_generation_spec(image_node),
                    delivery_spec=_runtime_delivery_spec(image_node),
                )
            )
        recipe_image_types.append(
            RecipeImageType(
                key=type_key_map[group.prompt_plan_key],
                title=group.title,
                order=len(recipe_image_types),
                default_quantity=len(recipe_images),
                images=recipe_images,
            )
        )
        prompt_images_by_key = {
            prompt_image.image_plan_key: prompt_image
            for prompt_image in group.prompt_payload.images
        }
        prompt_shapes.append(
            RecipePromptShape(
                image_type_key=type_key_map[group.prompt_plan_key],
                product_present=group.prompt_payload.product_fidelity.product_present,
                picture_in_picture=group.prompt_payload.product_fidelity.picture_in_picture,
                product_share_percent=group.prompt_payload.composition.product_share_percent,
                text_slots=[
                    field_name
                    for field_name in ("headline", "subtitle", "body")
                    if getattr(group.prompt_payload.text, field_name) is not None
                ],
                fact_keys=group.prompt_payload.fact_keys,
                visual_variant_key=group.prompt_payload.visual_variant_key,
                per_image_slots=[
                    {
                        "image_plan_key": image_key_map[image_plan_key],
                        "viewpoint": prompt_images_by_key[image_plan_key].viewpoint is not None,
                        "composition_adjustments": bool(
                            prompt_images_by_key[image_plan_key].composition_adjustments
                        ),
                        "lighting": prompt_images_by_key[image_plan_key].lighting is not None,
                    }
                    for image_node in included_images
                    for image_plan_key in [
                        _required_config_text(image_node, "image_plan_key", label="图片节点")
                    ]
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
        common = {
            "key": node_key_map[node.id],
            "position_x": node.position_x - min_x,
            "position_y": node.position_y - min_y,
            "folder_key": folder_key_map.get(node.folder_id),
        }
        if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT:
            recipe_nodes.append(RecipeProductContextNode(**common))
        elif node.node_type == WorkflowNodeType.REFERENCE_IMAGE:
            recipe_nodes.append(
                RecipeReferenceImageNode(
                    **common,
                    reference_requirement_key=reference_key_by_node_id[node.id],
                )
            )
        elif node.node_type == WorkflowNodeType.PROMPT_GENERATION:
            group = group_by_prompt_node_id.get(node.id)
            if group is None:
                raise ConflictError("提示词节点无法解析到当前 Prompt Artifact")
            recipe_nodes.append(
                RecipePromptGenerationNode(
                    **common,
                    image_type_key=type_key_map[group.prompt_plan_key],
                )
            )
        elif node.node_type == WorkflowNodeType.IMAGE_GENERATION:
            group = group_by_image_node_id.get(node.id)
            if group is None:
                raise ConflictError("图片节点无法解析到当前 Prompt Artifact")
            image_plan_key = _required_config_text(node, "image_plan_key", label="图片节点")
            recipe_nodes.append(
                RecipeImageGenerationNode(
                    **common,
                    image_type_key=type_key_map[group.prompt_plan_key],
                    image_plan_key=image_key_map[image_plan_key],
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
            title=_runtime_folder_title(workflow, source_folder_id),
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


def _runtime_prompt_groups(workflow: ProductWorkflow) -> tuple[_RuntimePromptGroup, ...]:
    version_by_id = {
        version.id: (artifact, version)
        for artifact in workflow.prompt_artifacts
        for version in artifact.versions
    }
    prompt_nodes = [
        node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION
    ]
    image_nodes = [
        node for node in workflow.nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION
    ]
    groups: list[_RuntimePromptGroup] = []
    prompt_plan_keys: set[str] = set()
    image_type_keys: set[str] = set()
    grouped_image_ids: set[str] = set()
    for prompt_node in prompt_nodes:
        prompt_plan_key = _required_config_text(prompt_node, "prompt_plan_key", label="提示词节点")
        image_type_key = _required_config_text(prompt_node, "image_type_key", label="提示词节点")
        if prompt_plan_key in prompt_plan_keys:
            raise ConflictError("同一 prompt plan 不能关联多个提示词节点")
        if image_type_key in image_type_keys:
            raise ConflictError("同一图片类型不能关联多个提示词节点")
        prompt_plan_keys.add(prompt_plan_key)
        image_type_keys.add(image_type_key)

        version_id = prompt_node.current_prompt_artifact_version_id
        resolved = version_by_id.get(version_id or "")
        if resolved is None:
            raise ConflictError("提示词节点缺少 current Prompt Artifact version")
        artifact, version = resolved
        if artifact.workflow_id != workflow.id or artifact.image_type_key != image_type_key:
            raise ConflictError("提示词节点与当前 Prompt Artifact lineage 不一致")
        try:
            prompt_payload = ImagePromptPayloadV1.model_validate(version.payload_json)
        except ValidationError as exc:
            raise ConflictError("current Prompt Artifact payload 不符合 schema version 1") from exc
        if _json_hash(prompt_payload.model_dump(mode="json")) != version.payload_hash:
            raise ConflictError("current Prompt Artifact payload hash 不一致")

        group_images = [
            node
            for node in image_nodes
            if node.config_json.get("prompt_plan_key") == prompt_plan_key
        ]
        if not group_images:
            raise ConflictError("提示词节点没有对应的图片节点")
        group_type_orders = {
            _required_config_int(node, "image_type_order", label="图片节点")
            for node in group_images
        }
        if len(group_type_orders) != 1:
            raise ConflictError("同一图片类型的运行时 order 不一致")
        for image_node in group_images:
            if _required_config_text(image_node, "image_type_key", label="图片节点") != image_type_key:
                raise ConflictError("图片节点与提示词节点的 image type 不一致")
            _runtime_generation_spec(image_node)
            _runtime_delivery_spec(image_node)
        ordered_images = tuple(
            sorted(
                group_images,
                key=lambda node: (
                    _required_config_int(node, "image_plan_order", label="图片节点"),
                    _required_config_text(node, "image_plan_key", label="图片节点"),
                    node.id,
                ),
            )
        )
        image_plan_keys = [
            _required_config_text(node, "image_plan_key", label="图片节点")
            for node in ordered_images
        ]
        image_plan_orders = [
            _required_config_int(node, "image_plan_order", label="图片节点")
            for node in ordered_images
        ]
        if len(image_plan_keys) != len(set(image_plan_keys)) or len(image_plan_orders) != len(set(image_plan_orders)):
            raise ConflictError("同一图片类型的运行时图片计划 key/order 不能重复")
        if set(image_plan_keys) != {item.image_plan_key for item in prompt_payload.images}:
            raise ConflictError("图片节点与 current Prompt Artifact 逐图计划不一致")
        grouped_image_ids.update(node.id for node in ordered_images)
        groups.append(
            _RuntimePromptGroup(
                prompt_plan_key=prompt_plan_key,
                image_type_key=image_type_key,
                title=artifact.title,
                order=next(iter(group_type_orders)),
                prompt_node=prompt_node,
                image_nodes=ordered_images,
                prompt_payload=prompt_payload,
            )
        )
    if grouped_image_ids != {node.id for node in image_nodes}:
        raise ConflictError("图片节点无法解析到唯一的当前 Prompt Artifact")
    return tuple(sorted(groups, key=lambda group: (group.order, group.image_type_key, group.prompt_node.id)))


def _relevant_runtime_image_plans(
    selected_nodes: Sequence[WorkflowNode],
    *,
    group_by_prompt_node_id: Mapping[str, _RuntimePromptGroup],
    group_by_image_node_id: Mapping[str, _RuntimePromptGroup],
) -> tuple[set[str], dict[str, set[str]]]:
    selected_images_by_group: dict[str, set[str]] = {}
    prompt_group_keys: set[str] = set()
    for node in selected_nodes:
        if node.node_type == WorkflowNodeType.PROMPT_GENERATION:
            group = group_by_prompt_node_id.get(node.id)
            if group is None:
                raise ConflictError("提示词节点无法解析到当前 Prompt Artifact")
            prompt_group_keys.add(group.prompt_plan_key)
        elif node.node_type == WorkflowNodeType.IMAGE_GENERATION:
            group = group_by_image_node_id.get(node.id)
            if group is None:
                raise ConflictError("图片节点无法解析到当前 Prompt Artifact")
            selected_images_by_group.setdefault(group.prompt_plan_key, set()).add(
                _required_config_text(node, "image_plan_key", label="图片节点")
            )
    relevant = set(selected_images_by_group) | prompt_group_keys
    included: dict[str, set[str]] = {}
    all_groups: dict[str, _RuntimePromptGroup] = {}
    for group in [*group_by_prompt_node_id.values(), *group_by_image_node_id.values()]:
        all_groups[group.prompt_plan_key] = group
    for group_key in relevant:
        group = all_groups[group_key]
        included[group_key] = selected_images_by_group.get(group_key) or {
            _required_config_text(node, "image_plan_key", label="图片节点")
            for node in group.image_nodes
        }
    return relevant, included


def _runtime_generation_spec(node: WorkflowNode) -> GenerationSpec:
    try:
        return GenerationSpec.model_validate(node.config_json.get("generation_spec"))
    except ValidationError as exc:
        raise ConflictError("图片节点 GenerationSpec 不符合当前合同") from exc


def _runtime_delivery_spec(node: WorkflowNode) -> DeliverySpec | None:
    value = node.config_json.get("delivery_spec")
    if value is None:
        return None
    try:
        return DeliverySpec.model_validate(value)
    except ValidationError as exc:
        raise ConflictError("图片节点 DeliverySpec 不符合当前合同") from exc


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


def _runtime_folder_title(workflow: ProductWorkflow, folder_id: str) -> str:
    folder = next((candidate for candidate in workflow.folders if candidate.id == folder_id), None)
    if folder is None:
        raise ConflictError("节点引用了不存在的 runtime 文件夹")
    title = folder.title.strip()
    if not title:
        raise ConflictError("runtime 文件夹标题不能为空")
    return title[:255]


def _required_config_text(node: WorkflowNode, key: str, *, label: str) -> str:
    value = node.config_json.get(key) if isinstance(node.config_json, dict) else None
    if not isinstance(value, str) or not value.strip():
        raise ConflictError(f"{label}缺少 {key}")
    return value.strip()


def _required_config_int(node: WorkflowNode, key: str, *, label: str) -> int:
    value = node.config_json.get(key) if isinstance(node.config_json, dict) else None
    if not isinstance(value, int) or isinstance(value, bool):
        raise ConflictError(f"{label}缺少 {key}")
    return value


def _config_text(node: WorkflowNode, key: str, *, fallback: str) -> str:
    value = node.config_json.get(key) if isinstance(node.config_json, dict) else None
    if isinstance(value, str) and value.strip():
        return value.strip()[:255]
    return fallback


def _json_hash(payload: dict[str, Any]) -> str:
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


__all__ = [
    "RecipeSourceType",
    "assert_recipe_payload_has_no_entity_ids",
    "assert_recipe_payload_has_no_forbidden_keys",
    "extract_recipe_payload",
]
