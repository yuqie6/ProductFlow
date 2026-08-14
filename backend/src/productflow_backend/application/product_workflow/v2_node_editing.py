from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass

from pydantic import ValidationError
from sqlalchemy import func, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.product_workflow.folders import WorkflowCanvasMutationResult
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.contracts import (
    DeliverySpec,
    GenerationSpec,
    ImagePromptPayloadV1,
    VisualSystemDraftPayload,
)
from productflow_backend.application.workflow_drafts.materialization import v2_workflow_query
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowNodeType
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    ImagePromptArtifact,
    ImagePromptArtifactVersion,
    ImagePromptArtifactVersionReference,
    ProductFactSetVersion,
    ProductImageAsset,
    ProductWorkflow,
    VisualSystemVersion,
    WorkflowNode,
    WorkflowNodeRun,
)

V2_WORKFLOW_SCHEMA_VERSION = 2
_ACTIVE_NODE_STATUSES = {WorkflowNodeStatus.QUEUED, WorkflowNodeStatus.RUNNING}


@dataclass(frozen=True, slots=True)
class V2PromptArtifactSnapshot:
    artifact: ImagePromptArtifact
    version: ImagePromptArtifactVersion
    payload: ImagePromptPayloadV1


@dataclass(frozen=True, slots=True)
class V2WorkflowNodeDetail:
    workflow: ProductWorkflow
    node: WorkflowNode
    prompt_artifact: V2PromptArtifactSnapshot | None


def get_v2_workflow_node_detail(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    node_id: str,
) -> V2WorkflowNodeDetail:
    workflow = session.scalar(
        select(ProductWorkflow).where(
            ProductWorkflow.id == workflow_id,
            ProductWorkflow.product_id == product_id,
        )
    )
    if workflow is None:
        raise NotFoundError("商品工作流不存在")
    _ensure_active_v2_workflow(workflow)
    node = session.scalar(
        select(WorkflowNode)
        .options(
            selectinload(WorkflowNode.current_prompt_artifact_version).selectinload(
                ImagePromptArtifactVersion.artifact
            ),
            selectinload(WorkflowNode.current_prompt_artifact_version).selectinload(
                ImagePromptArtifactVersion.references
            ),
        )
        .where(WorkflowNode.id == node_id, WorkflowNode.workflow_id == workflow.id)
    )
    if node is None:
        raise NotFoundError("工作流节点不存在")
    _ensure_v2_node(node)
    prompt_snapshot = _prompt_snapshot(node) if node.node_type == WorkflowNodeType.PROMPT_GENERATION else None
    return V2WorkflowNodeDetail(workflow=workflow, node=node, prompt_artifact=prompt_snapshot)


def update_v2_reference_node(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    node_id: str,
    expected_edit_version: int,
    title: str,
    role: str,
    label: str,
) -> WorkflowCanvasMutationResult:
    def mutate(workflow: ProductWorkflow, node: WorkflowNode) -> bool:
        _ensure_node_type(node, WorkflowNodeType.REFERENCE_IMAGE)
        normalized_title = _normalize_text(title, label="节点标题")
        normalized_role = _normalize_text(role, label="参考图用途")
        normalized_label = _normalize_text(label, label="参考图标签")
        current_role = node.config_json.get("role")
        current_label = node.config_json.get("label")
        changed = (
            node.title != normalized_title
            or current_role != normalized_role
            or current_label != normalized_label
        )
        if not changed:
            return False

        semantic_change = current_role != normalized_role or current_label != normalized_label
        affected_nodes = _prompt_and_image_nodes_for_reference(session, workflow_id=workflow.id, node_id=node.id)
        active_run_node_ids = {node.id}
        if semantic_change:
            active_run_node_ids.update(item.id for item in affected_nodes)
        _reject_active_runs(session, active_run_node_ids)
        node.title = normalized_title
        node.config_json = {
            **node.config_json,
            "role": normalized_role,
            "label": normalized_label,
        }
        if semantic_change:
            _mark_nodes_stale_after_reference_change(affected_nodes)
        return True

    return _update_v2_node(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        node_id=node_id,
        expected_edit_version=expected_edit_version,
        mutate=mutate,
    )


def update_v2_prompt_node(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    node_id: str,
    expected_edit_version: int,
    expected_prompt_artifact_version_id: str,
    title: str,
    payload: ImagePromptPayloadV1,
) -> WorkflowCanvasMutationResult:
    def mutate(workflow: ProductWorkflow, node: WorkflowNode) -> bool:
        _ensure_node_type(node, WorkflowNodeType.PROMPT_GENERATION)
        snapshot = _locked_prompt_snapshot(
            session,
            node=node,
            expected_prompt_artifact_version_id=expected_prompt_artifact_version_id,
        )
        normalized_title = _normalize_text(title, label="节点标题")
        _validate_prompt_payload_for_workflow(
            session,
            workflow=workflow,
            node=node,
            current_payload=snapshot.payload,
            next_payload=payload,
        )
        current_payload_json = snapshot.payload.model_dump(mode="json")
        next_payload_json = payload.model_dump(mode="json")
        payload_changed = current_payload_json != next_payload_json
        title_changed = node.title != normalized_title or snapshot.artifact.title != normalized_title
        if not payload_changed and not title_changed:
            return False

        image_nodes = _image_nodes_for_prompt(session, workflow=workflow, prompt_node=node)
        active_run_node_ids = {node.id}
        if payload_changed:
            active_run_node_ids.update(item.id for item in image_nodes)
        _reject_active_runs(session, active_run_node_ids)
        node.title = normalized_title
        snapshot.artifact.title = normalized_title
        if not payload_changed:
            return True

        next_version_number = (
            session.scalar(
                select(func.max(ImagePromptArtifactVersion.version)).where(
                    ImagePromptArtifactVersion.artifact_id == snapshot.artifact.id
                )
            )
            or 0
        ) + 1
        version = ImagePromptArtifactVersion(
            artifact_id=snapshot.artifact.id,
            version=next_version_number,
            schema_version=1,
            payload_json=next_payload_json,
            payload_hash=_json_hash(next_payload_json),
        )
        session.add(version)
        session.flush()
        for position, asset_id in enumerate(payload.evidence_asset_ids):
            session.add(
                ImagePromptArtifactVersionReference(
                    prompt_artifact_version_id=version.id,
                    asset_id=asset_id,
                    purpose="evidence",
                    position=position,
                )
            )

        node.current_prompt_artifact_version_id = version.id
        node.status = WorkflowNodeStatus.SUCCEEDED
        node.failure_reason = None
        node.output_json = {
            "contract_version": 2,
            "prompt_artifact_id": snapshot.artifact.id,
            "prompt_artifact_version_id": version.id,
            "version": version.version,
            "source": "manual_edit",
        }
        for image_node in image_nodes:
            image_node.status = WorkflowNodeStatus.IDLE
            image_node.failure_reason = None
        return True

    return _update_v2_node(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        node_id=node_id,
        expected_edit_version=expected_edit_version,
        mutate=mutate,
    )


def update_v2_image_node(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    node_id: str,
    expected_edit_version: int,
    title: str,
    variation_instruction: str | None,
    generation_spec: GenerationSpec,
    delivery_spec: DeliverySpec | None,
) -> WorkflowCanvasMutationResult:
    def mutate(_workflow: ProductWorkflow, node: WorkflowNode) -> bool:
        _ensure_node_type(node, WorkflowNodeType.IMAGE_GENERATION)
        normalized_title = _normalize_text(title, label="节点标题")
        normalized_variation = variation_instruction.strip() if variation_instruction else None
        next_generation = generation_spec.model_dump(mode="json")
        next_delivery = delivery_spec.model_dump(mode="json") if delivery_spec is not None else None
        generation_changed = (
            node.config_json.get("variation_instruction") != normalized_variation
            or node.config_json.get("generation_spec") != next_generation
        )
        delivery_changed = node.config_json.get("delivery_spec") != next_delivery
        title_changed = node.title != normalized_title
        if not generation_changed and not delivery_changed and not title_changed:
            return False
        _reject_active_runs(session, {node.id})
        node.title = normalized_title
        node.config_json = {
            **node.config_json,
            "variation_instruction": normalized_variation,
            "generation_spec": next_generation,
            "delivery_spec": next_delivery,
        }
        if generation_changed:
            node.status = WorkflowNodeStatus.IDLE
            node.failure_reason = None
        return True

    return _update_v2_node(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        node_id=node_id,
        expected_edit_version=expected_edit_version,
        mutate=mutate,
    )


def _update_v2_node(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    node_id: str,
    expected_edit_version: int,
    mutate,
) -> WorkflowCanvasMutationResult:
    try:
        workflow = session.scalar(
            select(ProductWorkflow)
            .where(
                ProductWorkflow.id == workflow_id,
                ProductWorkflow.product_id == product_id,
            )
            .with_for_update()
        )
        if workflow is None:
            raise NotFoundError("商品工作流不存在")
        _ensure_active_v2_workflow(workflow)
        if workflow.edit_version != expected_edit_version:
            raise ConflictError("工作流 edit version 已变化，请刷新后重试")
        node = session.scalar(
            select(WorkflowNode)
            .where(WorkflowNode.id == node_id, WorkflowNode.workflow_id == workflow.id)
            .with_for_update()
        )
        if node is None:
            raise NotFoundError("工作流节点不存在")
        _ensure_v2_node(node)
        changed = mutate(workflow, node)
        if changed:
            changed_at = now_utc()
            workflow.edit_version += 1
            workflow.updated_at = changed_at
            node.updated_at = changed_at
        session.commit()
        session.expire_all()
        return WorkflowCanvasMutationResult(
            workflow=_reload_workflow(session, workflow.id),
            changed=changed,
            dissolved_folder_ids=(),
        )
    except Exception:
        session.rollback()
        raise


def _ensure_active_v2_workflow(workflow: ProductWorkflow) -> None:
    if workflow.schema_version != V2_WORKFLOW_SCHEMA_VERSION:
        raise ConflictError("节点编辑只支持 schema-v2 工作流")
    if not workflow.active:
        raise ConflictError("只能修改 active schema-v2 工作流")


def _ensure_v2_node(node: WorkflowNode) -> None:
    if node.schema_version != V2_WORKFLOW_SCHEMA_VERSION:
        raise ConflictError("节点编辑只支持 schema-v2 节点")


def _ensure_node_type(node: WorkflowNode, expected: WorkflowNodeType) -> None:
    if node.node_type != expected:
        raise ConflictError("节点类型与编辑请求不一致")


def _normalize_text(value: str, *, label: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError(f"{label}不能为空")
    return normalized


def _prompt_snapshot(node: WorkflowNode) -> V2PromptArtifactSnapshot:
    version = node.current_prompt_artifact_version
    if version is None:
        raise ConflictError("提示词节点缺少 current Prompt Artifact version")
    artifact = version.artifact
    if artifact.workflow_id != node.workflow_id:
        raise ConflictError("提示词节点绑定了其他工作流的 Prompt Artifact")
    payload = _parse_prompt_payload(version)
    return V2PromptArtifactSnapshot(artifact=artifact, version=version, payload=payload)


def _locked_prompt_snapshot(
    session: Session,
    *,
    node: WorkflowNode,
    expected_prompt_artifact_version_id: str,
) -> V2PromptArtifactSnapshot:
    if node.current_prompt_artifact_version_id != expected_prompt_artifact_version_id:
        raise ConflictError("Prompt Artifact current version 已变化，请刷新后重试")
    version = session.scalar(
        select(ImagePromptArtifactVersion)
        .options(selectinload(ImagePromptArtifactVersion.artifact))
        .where(ImagePromptArtifactVersion.id == expected_prompt_artifact_version_id)
        .with_for_update()
    )
    if version is None:
        raise ConflictError("Prompt Artifact current version 不存在")
    artifact = session.scalar(
        select(ImagePromptArtifact)
        .where(ImagePromptArtifact.id == version.artifact_id)
        .with_for_update()
    )
    if artifact is None or artifact.workflow_id != node.workflow_id:
        raise ConflictError("提示词节点绑定了其他工作流的 Prompt Artifact")
    return V2PromptArtifactSnapshot(
        artifact=artifact,
        version=version,
        payload=_parse_prompt_payload(version),
    )


def _parse_prompt_payload(version: ImagePromptArtifactVersion) -> ImagePromptPayloadV1:
    try:
        payload = ImagePromptPayloadV1.model_validate(version.payload_json)
    except ValidationError as exc:
        raise ConflictError("Prompt Artifact version payload 不符合 schema version 1") from exc
    if _json_hash(payload.model_dump(mode="json")) != version.payload_hash:
        raise ConflictError("Prompt Artifact version payload hash 不一致")
    return payload


def _validate_prompt_payload_for_workflow(
    session: Session,
    *,
    workflow: ProductWorkflow,
    node: WorkflowNode,
    current_payload: ImagePromptPayloadV1,
    next_payload: ImagePromptPayloadV1,
) -> None:
    current_image_plan_keys = tuple(item.image_plan_key for item in current_payload.images)
    next_image_plan_keys = tuple(item.image_plan_key for item in next_payload.images)
    if next_image_plan_keys != current_image_plan_keys:
        raise BusinessValidationError("提示词逐图计划和顺序不能通过节点编辑修改")

    fact_set = session.scalar(
        select(ProductFactSetVersion).where(
            ProductFactSetVersion.source_draft_revision_id == workflow.source_draft_revision_id
        )
    )
    facts = fact_set.payload_json.get("facts") if fact_set is not None else None
    if not isinstance(facts, list):
        raise ConflictError("schema-v2 工作流缺少确认商品事实版本")
    fact_keys = {
        str(fact.get("key"))
        for fact in facts
        if isinstance(fact, dict) and isinstance(fact.get("key"), str)
    }
    if not set(next_payload.fact_keys).issubset(fact_keys):
        raise BusinessValidationError("提示词引用了未确认的商品事实")

    visual_version = session.get(VisualSystemVersion, workflow.visual_system_version_id)
    if visual_version is None:
        raise ConflictError("schema-v2 工作流缺少 VisualSystemVersion")
    try:
        visual_payload = VisualSystemDraftPayload.model_validate(visual_version.payload_json)
    except ValidationError as exc:
        raise ConflictError("VisualSystemVersion payload 不符合 schema") from exc
    variant_keys = {variant.key for variant in visual_payload.variants}
    if next_payload.visual_variant_key is not None and next_payload.visual_variant_key not in variant_keys:
        raise BusinessValidationError("提示词引用了不存在的视觉变体")

    if next_payload.evidence_asset_ids:
        matched_asset_ids = set(
            session.scalars(
                select(ProductImageAsset.id).where(
                    ProductImageAsset.product_id == workflow.product_id,
                    ProductImageAsset.id.in_(next_payload.evidence_asset_ids),
                )
            )
        )
        if matched_asset_ids != set(next_payload.evidence_asset_ids):
            raise BusinessValidationError("提示词引用了不属于当前商品的图片资产")


def _image_nodes_for_prompt(
    session: Session,
    *,
    workflow: ProductWorkflow,
    prompt_node: WorkflowNode,
) -> list[WorkflowNode]:
    prompt_plan_key = prompt_node.config_json.get("prompt_plan_key")
    image_nodes = list(
        session.scalars(
            select(WorkflowNode)
            .where(
                WorkflowNode.workflow_id == workflow.id,
                WorkflowNode.node_type == WorkflowNodeType.IMAGE_GENERATION,
            )
            .order_by(WorkflowNode.node_key, WorkflowNode.id)
            .with_for_update()
        )
    )
    if prompt_plan_key is None:
        raise ConflictError("提示词节点缺少 prompt plan key")
    return [item for item in image_nodes if item.config_json.get("prompt_plan_key") == prompt_plan_key]


def _prompt_and_image_nodes_for_reference(
    session: Session,
    *,
    workflow_id: str,
    node_id: str,
) -> list[WorkflowNode]:
    from productflow_backend.application.product_workflow.v2_reference_bindings import reachable_v2_stale_node_ids

    affected_ids = reachable_v2_stale_node_ids(session, workflow_id=workflow_id, source_node_id=node_id)
    if not affected_ids:
        return []
    return list(
        session.scalars(
            select(WorkflowNode)
            .where(WorkflowNode.id.in_(sorted(affected_ids)))
            .order_by(WorkflowNode.node_key, WorkflowNode.id)
            .with_for_update()
        )
    )


def _mark_nodes_stale_after_reference_change(nodes: list[WorkflowNode]) -> None:
    for node in nodes:
        node.status = WorkflowNodeStatus.IDLE
        node.failure_reason = None
        if node.node_type == WorkflowNodeType.PROMPT_GENERATION:
            output = dict(node.output_json) if isinstance(node.output_json, dict) else {}
            output["references_stale"] = True
            node.output_json = output


def _reject_active_runs(session: Session, node_ids: set[str]) -> None:
    if not node_ids:
        return
    active_run_id = session.scalar(
        select(WorkflowNodeRun.id)
        .where(
            WorkflowNodeRun.node_id.in_(sorted(node_ids)),
            WorkflowNodeRun.status.in_(_ACTIVE_NODE_STATUSES),
        )
        .order_by(WorkflowNodeRun.id)
        .with_for_update()
    )
    if active_run_id is not None:
        raise ConflictError("受影响的工作流节点正在运行")


def _reload_workflow(session: Session, workflow_id: str) -> ProductWorkflow:
    workflow = session.scalar(v2_workflow_query().where(ProductWorkflow.id == workflow_id))
    if workflow is None:
        raise NotFoundError("商品工作流不存在")
    return workflow


def _json_hash(payload: dict[str, object]) -> str:
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


__all__ = [
    "V2PromptArtifactSnapshot",
    "V2WorkflowNodeDetail",
    "get_v2_workflow_node_detail",
    "update_v2_image_node",
    "update_v2_prompt_node",
    "update_v2_reference_node",
]
