from __future__ import annotations

from collections.abc import Iterable
from copy import deepcopy

from sqlalchemy import delete, select
from sqlalchemy.orm import Session

from productflow_backend.application.product_workflow.folders import delete_empty_workflow_folders
from productflow_backend.application.product_workflow.v2_canvas_mutations import (
    V2_WORKFLOW_SCHEMA_VERSION,
    WorkflowCanvasMutationResult,
    reject_active_v2_node_runs,
    reject_active_v2_workflow_runs,
    run_v2_canvas_mutation,
)
from productflow_backend.application.product_workflow.v2_node_editing import (
    append_v2_prompt_artifact_version,
    parse_v2_prompt_payload,
)
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.contracts import (
    WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE,
    WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS,
    WORKFLOW_DRAFT_MAX_TOTAL_IMAGES,
    ImagePromptPayloadV1,
)
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowNodeType
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.domain.workflow_rules import (
    WorkflowRuleEdge,
    WorkflowRuleNode,
    canonical_workflow_edge_handles,
    topological_node_ids,
)
from productflow_backend.infrastructure.db.models import (
    ImagePromptArtifact,
    ImagePromptArtifactVersion,
    ProductWorkflow,
    WorkflowEdge,
    WorkflowFolder,
    WorkflowNode,
    WorkflowNodeRun,
    new_id,
)

_DUPLICATE_OFFSET = 48
_RUNNABLE_NODE_TYPES = {WorkflowNodeType.PROMPT_GENERATION, WorkflowNodeType.IMAGE_GENERATION}


def create_v2_reference_node(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    expected_edit_version: int,
    title: str,
    role: str,
    label: str,
    position_x: int,
    position_y: int,
    folder_id: str | None = None,
) -> WorkflowCanvasMutationResult:
    def mutate(workflow: ProductWorkflow) -> tuple[bool, set[str]]:
        reject_active_v2_workflow_runs(session, workflow.id)
        nodes, edges = _lock_v2_graph(session, workflow)
        _validate_v2_graph(nodes, edges)
        reference_count = sum(node.node_type == WorkflowNodeType.REFERENCE_IMAGE for node in nodes)
        if reference_count >= WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS:
            raise BusinessValidationError(
                f"参考图节点不能超过 {WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS} 个"
            )
        _lock_optional_folder(session, workflow=workflow, folder_id=folder_id)
        node = WorkflowNode(
            workflow_id=workflow.id,
            schema_version=V2_WORKFLOW_SCHEMA_VERSION,
            node_key=_stable_key("reference"),
            node_type=WorkflowNodeType.REFERENCE_IMAGE,
            title=_normalize_text(title, label="节点标题", max_length=255),
            position_x=position_x,
            position_y=position_y,
            folder_id=folder_id,
            config_json={
                "contract_version": 2,
                "source_draft_revision_id": workflow.source_draft_revision_id,
                "reference_key": _stable_key("reference"),
                "role": _normalize_text(role, label="参考图用途", max_length=120),
                "label": _normalize_text(label, label="参考图标签", max_length=255),
            },
        )
        session.add(node)
        return True, set()

    return run_v2_canvas_mutation(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        expected_edit_version=expected_edit_version,
        mutate=mutate,
    )


def duplicate_v2_workflow_node(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    node_id: str,
    expected_edit_version: int,
) -> WorkflowCanvasMutationResult:
    def mutate(workflow: ProductWorkflow) -> tuple[bool, set[str]]:
        reject_active_v2_workflow_runs(session, workflow.id)
        nodes, edges = _lock_v2_graph(session, workflow)
        _validate_v2_graph(nodes, edges)
        node = _node_or_raise(nodes, node_id)
        if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT:
            raise ConflictError("商品事实节点不可复制")
        if node.node_type == WorkflowNodeType.REFERENCE_IMAGE:
            _duplicate_reference(session, workflow=workflow, node=node, nodes=nodes)
        elif node.node_type == WorkflowNodeType.IMAGE_GENERATION:
            _duplicate_image(session, workflow=workflow, node=node, nodes=nodes, edges=edges)
        elif node.node_type == WorkflowNodeType.PROMPT_GENERATION:
            _duplicate_prompt_group(session, workflow=workflow, node=node, nodes=nodes, edges=edges)
        else:
            raise ConflictError("当前节点类型不支持复制")
        session.flush()
        refreshed_nodes, refreshed_edges = _lock_v2_graph(session, workflow)
        _validate_v2_graph(refreshed_nodes, refreshed_edges)
        return True, set()

    return run_v2_canvas_mutation(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        expected_edit_version=expected_edit_version,
        mutate=mutate,
    )


def delete_v2_workflow_node(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    node_id: str,
    expected_edit_version: int,
) -> WorkflowCanvasMutationResult:
    def mutate(workflow: ProductWorkflow) -> tuple[bool, set[str]]:
        reject_active_v2_workflow_runs(session, workflow.id)
        nodes, edges = _lock_v2_graph(session, workflow)
        _validate_v2_graph(nodes, edges)
        node = _node_or_raise(nodes, node_id)
        if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT:
            raise ConflictError("商品事实节点不可删除")

        deleting_nodes = [node]
        prompt_to_update: tuple[WorkflowNode, ImagePromptArtifact, ImagePromptPayloadV1, str] | None = None
        if node.node_type in {WorkflowNodeType.PROMPT_GENERATION, WorkflowNodeType.IMAGE_GENERATION}:
            prompt, image_nodes, artifact, payload = _prompt_group_for_node(
                session,
                workflow=workflow,
                node=node,
                nodes=nodes,
            )
            if node.node_type == WorkflowNodeType.PROMPT_GENERATION or len(image_nodes) == 1:
                deleting_nodes = [prompt, *image_nodes]
            else:
                image_plan_key = _config_key(node, "image_plan_key", label="图片节点")
                prompt_to_update = (prompt, artifact, payload, image_plan_key)

        deleting_ids = {item.id for item in deleting_nodes}
        affected_ids = _reachable_runnable_node_ids(
            nodes,
            edges,
            start_node_ids=deleting_ids,
        ) - deleting_ids
        if prompt_to_update is not None:
            prompt, _artifact, _payload, _image_plan_key = prompt_to_update
            owned_image_ids = {
                item.id
                for item in nodes
                if item.node_type == WorkflowNodeType.IMAGE_GENERATION
                and item.config_json.get("prompt_plan_key") == prompt.config_json.get("prompt_plan_key")
            }
            affected_ids.update(owned_image_ids - deleting_ids)
            affected_ids.add(prompt.id)
        reject_active_v2_node_runs(session, deleting_ids | affected_ids)

        if prompt_to_update is not None:
            prompt, artifact, payload, image_plan_key = prompt_to_update
            remaining_plans = [plan for plan in payload.images if plan.image_plan_key != image_plan_key]
            if len(remaining_plans) != len(payload.images) - 1:
                raise ConflictError("图片节点与 Prompt Artifact 逐图计划不一致")
            next_payload = payload.model_copy(update={"images": remaining_plans})
            version = append_v2_prompt_artifact_version(session, artifact=artifact, payload=next_payload)
            _set_prompt_manual_version(prompt, artifact=artifact, version=version)
            affected_ids.discard(prompt.id)

        changed_at = now_utc()
        _mark_v2_nodes_stale(nodes, affected_ids, changed_at=changed_at)
        for item in deleting_nodes:
            if item.folder_id is not None:
                item.updated_at = changed_at
        session.execute(delete(WorkflowNodeRun).where(WorkflowNodeRun.node_id.in_(sorted(deleting_ids))))
        for edge in edges:
            if edge.source_node_id in deleting_ids or edge.target_node_id in deleting_ids:
                session.delete(edge)
        for item in deleting_nodes:
            session.delete(item)
        session.flush()
        dissolved = delete_empty_workflow_folders(
            session,
            workflow_id=workflow.id,
            folder_ids={item.folder_id for item in deleting_nodes if item.folder_id is not None},
        )
        refreshed_nodes, refreshed_edges = _lock_v2_graph(session, workflow)
        _validate_v2_graph(refreshed_nodes, refreshed_edges)
        return True, dissolved

    return run_v2_canvas_mutation(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        expected_edit_version=expected_edit_version,
        mutate=mutate,
    )


def create_v2_workflow_edge(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    source_node_id: str,
    target_node_id: str,
    expected_edit_version: int,
) -> WorkflowCanvasMutationResult:
    def mutate(workflow: ProductWorkflow) -> tuple[bool, set[str]]:
        reject_active_v2_workflow_runs(session, workflow.id)
        nodes, edges = _lock_v2_graph(session, workflow)
        _validate_v2_graph(nodes, edges)
        source = _node_or_raise(nodes, source_node_id)
        target = _node_or_raise(nodes, target_node_id)
        if source.id == target.id:
            raise BusinessValidationError("工作流连线不能连接到自身")
        handles = canonical_workflow_edge_handles(source.node_type, target.node_type)
        if handles is None:
            raise BusinessValidationError("工作流连线包含不支持的 v2 节点类型组合")
        if any(edge.source_node_id == source.id and edge.target_node_id == target.id for edge in edges):
            raise BusinessValidationError("相同工作流节点之间不能重复连线")
        proposed_edges = [
            *(_rule_edge(edge) for edge in edges),
            WorkflowRuleEdge(source_node_id=source.id, target_node_id=target.id),
        ]
        _validate_v2_graph(nodes, proposed_edges)
        affected_ids = _reachable_runnable_node_ids(
            nodes,
            proposed_edges,
            start_node_ids={target.id},
        )
        reject_active_v2_node_runs(session, affected_ids)
        _mark_v2_nodes_stale(nodes, affected_ids, changed_at=now_utc())
        _add_v2_edge(
            session,
            workflow=workflow,
            source=source,
            target=target,
            handles=handles,
        )
        return True, set()

    return run_v2_canvas_mutation(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        expected_edit_version=expected_edit_version,
        mutate=mutate,
    )


def delete_v2_workflow_edge(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    edge_id: str,
    expected_edit_version: int,
) -> WorkflowCanvasMutationResult:
    def mutate(workflow: ProductWorkflow) -> tuple[bool, set[str]]:
        reject_active_v2_workflow_runs(session, workflow.id)
        nodes, edges = _lock_v2_graph(session, workflow)
        _validate_v2_graph(nodes, edges)
        edge = next((item for item in edges if item.id == edge_id), None)
        if edge is None:
            raise NotFoundError("工作流连线不存在")
        nodes_by_id = {node.id: node for node in nodes}
        source = nodes_by_id[edge.source_node_id]
        target = nodes_by_id[edge.target_node_id]
        if _is_lineage_edge(source, target):
            raise ConflictError("工作流 lineage 连线不可删除")
        remaining_edges = [item for item in edges if item.id != edge.id]
        _validate_v2_graph(nodes, remaining_edges)
        affected_ids = _reachable_runnable_node_ids(
            nodes,
            remaining_edges,
            start_node_ids={target.id},
        )
        reject_active_v2_node_runs(session, affected_ids)
        _mark_v2_nodes_stale(nodes, affected_ids, changed_at=now_utc())
        session.delete(edge)
        return True, set()

    return run_v2_canvas_mutation(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        expected_edit_version=expected_edit_version,
        mutate=mutate,
    )


def _duplicate_reference(
    session: Session,
    *,
    workflow: ProductWorkflow,
    node: WorkflowNode,
    nodes: list[WorkflowNode],
) -> None:
    if sum(item.node_type == WorkflowNodeType.REFERENCE_IMAGE for item in nodes) >= WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS:
        raise BusinessValidationError(f"参考图节点不能超过 {WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS} 个")
    config = deepcopy(node.config_json)
    config["reference_key"] = _stable_key("reference")
    session.add(
        WorkflowNode(
            workflow_id=workflow.id,
            schema_version=V2_WORKFLOW_SCHEMA_VERSION,
            node_key=_stable_key("reference-node"),
            node_type=WorkflowNodeType.REFERENCE_IMAGE,
            title=_duplicate_title(node.title),
            position_x=node.position_x + _DUPLICATE_OFFSET,
            position_y=node.position_y + _DUPLICATE_OFFSET,
            config_json=config,
            folder_id=node.folder_id,
            bound_image_asset_id=node.bound_image_asset_id,
        )
    )


def _duplicate_image(
    session: Session,
    *,
    workflow: ProductWorkflow,
    node: WorkflowNode,
    nodes: list[WorkflowNode],
    edges: list[WorkflowEdge],
) -> None:
    prompt, image_nodes, artifact, payload = _prompt_group_for_node(
        session,
        workflow=workflow,
        node=node,
        nodes=nodes,
    )
    _ensure_image_capacity(nodes, group_size=len(image_nodes), addition=1)
    reject_active_v2_node_runs(session, {prompt.id, *(item.id for item in image_nodes)})
    image_plan_key = _config_key(node, "image_plan_key", label="图片节点")
    matching_plans = [plan for plan in payload.images if plan.image_plan_key == image_plan_key]
    if len(matching_plans) != 1:
        raise ConflictError("图片节点与 Prompt Artifact 逐图计划不一致")
    next_image_plan_key = _stable_key("image-plan")
    next_plan = matching_plans[0].model_copy(update={"image_plan_key": next_image_plan_key})
    next_payload = payload.model_copy(update={"images": [*payload.images, next_plan]})
    version = append_v2_prompt_artifact_version(session, artifact=artifact, payload=next_payload)
    _set_prompt_manual_version(prompt, artifact=artifact, version=version)
    changed_at = now_utc()
    _mark_v2_nodes_stale(nodes, {item.id for item in image_nodes}, changed_at=changed_at)

    config = deepcopy(node.config_json)
    config.update(
        {
            "image_plan_key": next_image_plan_key,
            "image_plan_order": max(
                (_config_int(item, "image_plan_order", default=-1) for item in image_nodes),
                default=-1,
            )
            + 1,
            "cover_priority": max(
                (_config_int(item, "cover_priority", default=-1) for item in nodes),
                default=-1,
            )
            + 1,
        }
    )
    duplicate = WorkflowNode(
        workflow_id=workflow.id,
        schema_version=V2_WORKFLOW_SCHEMA_VERSION,
        node_key=_stable_key("image-node"),
        node_type=WorkflowNodeType.IMAGE_GENERATION,
        title=_duplicate_title(node.title),
        position_x=node.position_x + _DUPLICATE_OFFSET,
        position_y=node.position_y + _DUPLICATE_OFFSET,
        config_json=config,
        folder_id=node.folder_id,
        status=WorkflowNodeStatus.IDLE,
    )
    session.add(duplicate)
    session.flush()
    nodes_by_id = {item.id: item for item in nodes}
    for edge in edges:
        if edge.target_node_id != node.id:
            continue
        source = nodes_by_id[edge.source_node_id]
        handles = canonical_workflow_edge_handles(source.node_type, duplicate.node_type)
        if handles is None:
            raise BusinessValidationError("工作流连线包含不支持的 v2 节点类型组合")
        _add_v2_edge(
            session,
            workflow=workflow,
            source=source,
            target=duplicate,
            handles=handles,
        )


def _duplicate_prompt_group(
    session: Session,
    *,
    workflow: ProductWorkflow,
    node: WorkflowNode,
    nodes: list[WorkflowNode],
    edges: list[WorkflowEdge],
) -> None:
    prompt, image_nodes, _artifact, payload = _prompt_group_for_node(
        session,
        workflow=workflow,
        node=node,
        nodes=nodes,
    )
    _ensure_image_capacity(nodes, group_size=0, addition=len(image_nodes))
    image_type_key = _stable_key("image-type")
    prompt_plan_key = _stable_key("prompt-plan")
    next_plan_keys = {
        _config_key(image, "image_plan_key", label="图片节点"): _stable_key("image-plan")
        for image in image_nodes
    }
    next_payload = payload.model_copy(
        update={
            "images": [
                plan.model_copy(update={"image_plan_key": next_plan_keys[plan.image_plan_key]})
                for plan in payload.images
            ]
        }
    )
    duplicate_title = _duplicate_title(prompt.title)
    artifact = ImagePromptArtifact(
        workflow_id=workflow.id,
        image_type_key=image_type_key,
        title=duplicate_title,
    )
    session.add(artifact)
    session.flush()
    version = append_v2_prompt_artifact_version(
        session,
        artifact=artifact,
        payload=next_payload,
        version_number=1,
    )
    prompt_config = deepcopy(prompt.config_json)
    prompt_config.update({"prompt_plan_key": prompt_plan_key, "image_type_key": image_type_key})
    duplicate_prompt = WorkflowNode(
        workflow_id=workflow.id,
        schema_version=V2_WORKFLOW_SCHEMA_VERSION,
        node_key=_stable_key("prompt-node"),
        node_type=WorkflowNodeType.PROMPT_GENERATION,
        title=duplicate_title,
        position_x=prompt.position_x + _DUPLICATE_OFFSET,
        position_y=prompt.position_y + _DUPLICATE_OFFSET,
        config_json=prompt_config,
        folder_id=prompt.folder_id,
        current_prompt_artifact_version_id=version.id,
        status=WorkflowNodeStatus.SUCCEEDED,
        output_json=_prompt_version_output(artifact=artifact, version=version, source="manual_duplicate"),
    )
    session.add(duplicate_prompt)

    next_image_type_order = max(
        (_config_int(item, "image_type_order", default=-1) for item in nodes),
        default=-1,
    ) + 1
    next_cover_priority = max(
        (_config_int(item, "cover_priority", default=-1) for item in nodes),
        default=-1,
    ) + 1
    duplicate_images_by_original_id: dict[str, WorkflowNode] = {}
    for index, image in enumerate(sorted(image_nodes, key=_image_node_order)):
        original_plan_key = _config_key(image, "image_plan_key", label="图片节点")
        config = deepcopy(image.config_json)
        config.update(
            {
                "image_plan_key": next_plan_keys[original_plan_key],
                "image_type_key": image_type_key,
                "image_type_order": next_image_type_order,
                "image_plan_order": index,
                "cover_priority": next_cover_priority + index,
                "prompt_plan_key": prompt_plan_key,
            }
        )
        duplicate = WorkflowNode(
            workflow_id=workflow.id,
            schema_version=V2_WORKFLOW_SCHEMA_VERSION,
            node_key=_stable_key("image-node"),
            node_type=WorkflowNodeType.IMAGE_GENERATION,
            title=_duplicate_title(image.title),
            position_x=image.position_x + _DUPLICATE_OFFSET,
            position_y=image.position_y + _DUPLICATE_OFFSET,
            config_json=config,
            folder_id=image.folder_id,
            status=WorkflowNodeStatus.IDLE,
        )
        session.add(duplicate)
        duplicate_images_by_original_id[image.id] = duplicate
    session.flush()

    nodes_by_id = {item.id: item for item in nodes}
    nodes_by_id[duplicate_prompt.id] = duplicate_prompt
    for duplicate in duplicate_images_by_original_id.values():
        nodes_by_id[duplicate.id] = duplicate
    copied_target_ids = {prompt.id, *duplicate_images_by_original_id.keys()}
    for edge in edges:
        if edge.target_node_id not in copied_target_ids:
            continue
        target = (
            duplicate_prompt
            if edge.target_node_id == prompt.id
            else duplicate_images_by_original_id[edge.target_node_id]
        )
        source = (
            duplicate_prompt
            if edge.source_node_id == prompt.id
            else duplicate_images_by_original_id.get(edge.source_node_id)
            or nodes_by_id[edge.source_node_id]
        )
        handles = canonical_workflow_edge_handles(source.node_type, target.node_type)
        if handles is None:
            raise BusinessValidationError("工作流连线包含不支持的 v2 节点类型组合")
        _add_v2_edge(
            session,
            workflow=workflow,
            source=source,
            target=target,
            handles=handles,
        )


def _prompt_group_for_node(
    session: Session,
    *,
    workflow: ProductWorkflow,
    node: WorkflowNode,
    nodes: list[WorkflowNode],
) -> tuple[WorkflowNode, list[WorkflowNode], ImagePromptArtifact, ImagePromptPayloadV1]:
    if node.node_type == WorkflowNodeType.PROMPT_GENERATION:
        prompt = node
    elif node.node_type == WorkflowNodeType.IMAGE_GENERATION:
        prompt_plan_key = _config_key(node, "prompt_plan_key", label="图片节点")
        matches = [
            item
            for item in nodes
            if item.node_type == WorkflowNodeType.PROMPT_GENERATION
            and item.config_json.get("prompt_plan_key") == prompt_plan_key
        ]
        if len(matches) != 1:
            raise ConflictError("图片节点必须且只能解析到一个提示词节点")
        prompt = matches[0]
    else:
        raise ConflictError("当前节点不属于 Prompt Artifact 图片组")
    prompt_plan_key = _config_key(prompt, "prompt_plan_key", label="提示词节点")
    image_nodes = sorted(
        [
            item
            for item in nodes
            if item.node_type == WorkflowNodeType.IMAGE_GENERATION
            and item.config_json.get("prompt_plan_key") == prompt_plan_key
        ],
        key=_image_node_order,
    )
    if not image_nodes:
        raise ConflictError("提示词节点没有对应的图片节点")
    version_id = prompt.current_prompt_artifact_version_id
    if version_id is None:
        raise ConflictError("提示词节点缺少 current Prompt Artifact version")
    version = session.scalar(
        select(ImagePromptArtifactVersion)
        .where(ImagePromptArtifactVersion.id == version_id)
        .with_for_update()
    )
    if version is None:
        raise ConflictError("Prompt Artifact current version 不存在")
    artifact = session.scalar(
        select(ImagePromptArtifact)
        .where(ImagePromptArtifact.id == version.artifact_id)
        .with_for_update()
    )
    if artifact is None or artifact.workflow_id != workflow.id:
        raise ConflictError("提示词节点绑定了其他工作流的 Prompt Artifact")
    payload = parse_v2_prompt_payload(version)
    payload_plan_keys = {plan.image_plan_key for plan in payload.images}
    node_plan_keys = {_config_key(item, "image_plan_key", label="图片节点") for item in image_nodes}
    if payload_plan_keys != node_plan_keys:
        raise ConflictError("图片节点与 Prompt Artifact 逐图计划不一致")
    return prompt, image_nodes, artifact, payload


def _lock_v2_graph(
    session: Session,
    workflow: ProductWorkflow,
) -> tuple[list[WorkflowNode], list[WorkflowEdge]]:
    nodes = list(
        session.scalars(
            select(WorkflowNode)
            .where(WorkflowNode.workflow_id == workflow.id)
            .order_by(WorkflowNode.node_key, WorkflowNode.id)
            .with_for_update()
        )
    )
    edges = list(
        session.scalars(
            select(WorkflowEdge)
            .where(WorkflowEdge.workflow_id == workflow.id)
            .order_by(WorkflowEdge.edge_key, WorkflowEdge.id)
            .with_for_update()
        )
    )
    return nodes, edges


def _validate_v2_graph(
    nodes: list[WorkflowNode],
    edges: Iterable[WorkflowEdge | WorkflowRuleEdge],
) -> None:
    if any(node.schema_version != V2_WORKFLOW_SCHEMA_VERSION or node.node_key is None for node in nodes):
        raise ConflictError("schema-v2 工作流包含无效节点")
    nodes_by_id = {node.id: node for node in nodes}
    if len(nodes_by_id) != len(nodes):
        raise ConflictError("schema-v2 工作流包含重复节点")
    edge_list = [_rule_edge(edge) for edge in edges]
    pairs: set[tuple[str, str]] = set()
    for edge in edge_list:
        source = nodes_by_id.get(edge.source_node_id)
        target = nodes_by_id.get(edge.target_node_id)
        if source is None or target is None:
            raise BusinessValidationError("工作流连线引用了不存在的节点")
        if source.id == target.id:
            raise BusinessValidationError("工作流连线不能连接到自身")
        if canonical_workflow_edge_handles(source.node_type, target.node_type) is None:
            raise BusinessValidationError("工作流连线包含不支持的 v2 节点类型组合")
        pair = (source.id, target.id)
        if pair in pairs:
            raise BusinessValidationError("相同工作流节点之间不能重复连线")
        pairs.add(pair)
    topological_node_ids(
        [WorkflowRuleNode(id=node.id, node_type=node.node_type, position_x=node.position_x) for node in nodes],
        edge_list,
    )
    contexts = [node for node in nodes if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT]
    if len(contexts) != 1:
        raise ConflictError("schema-v2 工作流必须且只能包含一个商品事实节点")
    context_id = contexts[0].id
    prompt_by_plan: dict[str, WorkflowNode] = {}
    for prompt in (node for node in nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION):
        prompt_plan_key = _config_key(prompt, "prompt_plan_key", label="提示词节点")
        if prompt_plan_key in prompt_by_plan:
            raise ConflictError("同一 prompt plan 不能关联多个提示词节点")
        prompt_by_plan[prompt_plan_key] = prompt
        if (context_id, prompt.id) not in pairs:
            raise ConflictError("每个提示词节点必须保留商品事实 lineage 连线")
    for image in (node for node in nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION):
        prompt_plan_key = _config_key(image, "prompt_plan_key", label="图片节点")
        prompt = prompt_by_plan.get(prompt_plan_key)
        if prompt is None or (prompt.id, image.id) not in pairs:
            raise ConflictError("每个图片节点必须保留对应提示词 lineage 连线")


def _reachable_runnable_node_ids(
    nodes: list[WorkflowNode],
    edges: Iterable[WorkflowEdge | WorkflowRuleEdge],
    *,
    start_node_ids: set[str],
) -> set[str]:
    outgoing: dict[str, list[str]] = {}
    for edge in edges:
        rule_edge = _rule_edge(edge)
        outgoing.setdefault(rule_edge.source_node_id, []).append(rule_edge.target_node_id)
    seen: set[str] = set()
    queue = list(start_node_ids)
    while queue:
        node_id = queue.pop(0)
        if node_id in seen:
            continue
        seen.add(node_id)
        queue.extend(outgoing.get(node_id, []))
    runnable_ids = {node.id for node in nodes if node.node_type in _RUNNABLE_NODE_TYPES}
    return seen & runnable_ids


def _mark_v2_nodes_stale(
    nodes: list[WorkflowNode],
    node_ids: Iterable[str],
    *,
    changed_at,
) -> None:
    affected = set(node_ids)
    for node in nodes:
        if node.id not in affected:
            continue
        node.status = WorkflowNodeStatus.IDLE
        node.failure_reason = None
        node.updated_at = changed_at
        if node.node_type == WorkflowNodeType.PROMPT_GENERATION:
            output = dict(node.output_json) if isinstance(node.output_json, dict) else {}
            output["references_stale"] = True
            node.output_json = output


def _set_prompt_manual_version(
    prompt: WorkflowNode,
    *,
    artifact: ImagePromptArtifact,
    version: ImagePromptArtifactVersion,
) -> None:
    prompt.current_prompt_artifact_version_id = version.id
    prompt.status = WorkflowNodeStatus.SUCCEEDED
    prompt.failure_reason = None
    prompt.output_json = _prompt_version_output(
        artifact=artifact,
        version=version,
        source="manual_structure_edit",
    )
    prompt.updated_at = now_utc()


def _prompt_version_output(
    *,
    artifact: ImagePromptArtifact,
    version: ImagePromptArtifactVersion,
    source: str,
) -> dict[str, object]:
    return {
        "contract_version": 2,
        "prompt_artifact_id": artifact.id,
        "prompt_artifact_version_id": version.id,
        "version": version.version,
        "source": source,
    }


def _add_v2_edge(
    session: Session,
    *,
    workflow: ProductWorkflow,
    source: WorkflowNode,
    target: WorkflowNode,
    handles: tuple[str, str],
) -> WorkflowEdge:
    edge = WorkflowEdge(
        workflow_id=workflow.id,
        edge_key=_stable_key("edge"),
        source_node_id=source.id,
        target_node_id=target.id,
        source_handle=handles[0],
        target_handle=handles[1],
    )
    session.add(edge)
    return edge


def _is_lineage_edge(source: WorkflowNode, target: WorkflowNode) -> bool:
    if source.node_type == WorkflowNodeType.PRODUCT_CONTEXT and target.node_type == WorkflowNodeType.PROMPT_GENERATION:
        return True
    return (
        source.node_type == WorkflowNodeType.PROMPT_GENERATION
        and target.node_type == WorkflowNodeType.IMAGE_GENERATION
        and source.config_json.get("prompt_plan_key") == target.config_json.get("prompt_plan_key")
    )


def _lock_optional_folder(
    session: Session,
    *,
    workflow: ProductWorkflow,
    folder_id: str | None,
) -> WorkflowFolder | None:
    if folder_id is None:
        return None
    folder = session.scalar(
        select(WorkflowFolder)
        .where(WorkflowFolder.id == folder_id, WorkflowFolder.workflow_id == workflow.id)
        .with_for_update()
    )
    if folder is None:
        raise NotFoundError("工作流文件夹不存在")
    return folder


def _ensure_image_capacity(nodes: list[WorkflowNode], *, group_size: int, addition: int) -> None:
    if group_size + addition > WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE:
        raise BusinessValidationError(f"单个图片类型不能超过 {WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE} 张")
    total = sum(node.node_type == WorkflowNodeType.IMAGE_GENERATION for node in nodes)
    if total + addition > WORKFLOW_DRAFT_MAX_TOTAL_IMAGES:
        raise BusinessValidationError(f"工作流图片总数不能超过 {WORKFLOW_DRAFT_MAX_TOTAL_IMAGES} 张")


def _node_or_raise(nodes: list[WorkflowNode], node_id: str) -> WorkflowNode:
    node = next((item for item in nodes if item.id == node_id), None)
    if node is None:
        raise NotFoundError("工作流节点不存在")
    return node


def _config_key(node: WorkflowNode, key: str, *, label: str) -> str:
    value = node.config_json.get(key)
    if not isinstance(value, str) or not value:
        raise ConflictError(f"{label}缺少 {key}")
    return value


def _config_int(node: WorkflowNode, key: str, *, default: int) -> int:
    value = node.config_json.get(key)
    return value if isinstance(value, int) and not isinstance(value, bool) else default


def _image_node_order(node: WorkflowNode) -> tuple[int, str]:
    return _config_int(node, "image_plan_order", default=0), node.id


def _normalize_text(value: str, *, label: str, max_length: int) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError(f"{label}不能为空")
    if len(normalized) > max_length:
        raise BusinessValidationError(f"{label}不能超过 {max_length} 个字符")
    return normalized


def _duplicate_title(title: str) -> str:
    suffix = "（副本）"
    return f"{title[: 255 - len(suffix)]}{suffix}"


def _stable_key(prefix: str) -> str:
    return f"{prefix}-{new_id()}"


def _rule_edge(edge: WorkflowEdge | WorkflowRuleEdge) -> WorkflowRuleEdge:
    if isinstance(edge, WorkflowRuleEdge):
        return edge
    return WorkflowRuleEdge(
        source_node_id=edge.source_node_id,
        target_node_id=edge.target_node_id,
    )


__all__ = [
    "create_v2_reference_node",
    "create_v2_workflow_edge",
    "delete_v2_workflow_edge",
    "delete_v2_workflow_node",
    "duplicate_v2_workflow_node",
]
