from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from typing import Any

from sqlalchemy import func, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.contracts import (
    ImageGenerationNodePlan,
    PromptGenerationNodePlan,
    ReferenceImageNodePlan,
    WorkflowDraftPayloadV1,
    WorkflowNodePlan,
    workflow_draft_payload_hash,
)
from productflow_backend.application.workflow_drafts.service import (
    parse_workflow_draft_payload_or_raise,
    validate_workflow_draft_reference_assets,
)
from productflow_backend.domain.enums import WorkflowDraftStatus, WorkflowNodeType, WorkflowRevealEventKind
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    ImagePromptArtifact,
    ImagePromptArtifactVersion,
    ImagePromptArtifactVersionReference,
    Product,
    ProductFactSetVersion,
    ProductWorkflow,
    VisualException,
    WorkflowDraft,
    WorkflowDraftRevision,
    WorkflowEdge,
    WorkflowFolder,
    WorkflowMaterialization,
    WorkflowMaterializationKey,
    WorkflowNode,
    WorkflowRevealEvent,
)


@dataclass(frozen=True, slots=True)
class ActiveV2WorkflowSnapshot:
    workflow: ProductWorkflow | None
    latest_revision: int


@dataclass(frozen=True, slots=True)
class WorkflowMaterializationResult:
    materialization: WorkflowMaterialization
    workflow: ProductWorkflow
    created: bool


def v2_workflow_query():
    return select(ProductWorkflow).options(
        selectinload(ProductWorkflow.folders),
        selectinload(ProductWorkflow.nodes),
        selectinload(ProductWorkflow.nodes).selectinload(WorkflowNode.current_prompt_artifact_version),
        selectinload(ProductWorkflow.edges),
        selectinload(ProductWorkflow.prompt_artifacts).selectinload(ImagePromptArtifact.versions),
        selectinload(ProductWorkflow.visual_exceptions),
        selectinload(ProductWorkflow.materialization).selectinload(WorkflowMaterialization.reveal_events),
    )


def get_active_v2_workflow_snapshot(session: Session, *, product_id: str) -> ActiveV2WorkflowSnapshot:
    if session.get(Product, product_id) is None:
        raise NotFoundError("商品不存在")
    workflow = session.scalar(
        v2_workflow_query().where(
            ProductWorkflow.product_id == product_id,
            ProductWorkflow.active.is_(True),
            ProductWorkflow.schema_version == 2,
        )
    )
    latest_revision = (
        session.scalar(
            select(func.max(ProductWorkflow.revision)).where(
                ProductWorkflow.product_id == product_id,
                ProductWorkflow.schema_version == 2,
            )
        )
        or 0
    )
    return ActiveV2WorkflowSnapshot(workflow=workflow, latest_revision=latest_revision)


def materialize_workflow_draft(
    session: Session,
    *,
    product_id: str,
    draft_id: str,
    expected_draft_version: int,
    expected_workflow_revision: int,
    idempotency_key: str,
) -> WorkflowMaterializationResult:
    normalized_key = _normalize_idempotency_key(idempotency_key)
    request_hash = _materialization_request_hash(
        product_id=product_id,
        draft_id=draft_id,
        expected_draft_version=expected_draft_version,
        expected_workflow_revision=expected_workflow_revision,
    )
    try:
        product = session.scalar(select(Product).where(Product.id == product_id).with_for_update())
        if product is None:
            raise NotFoundError("商品不存在")
        draft = session.scalar(
            select(WorkflowDraft)
            .options(
                selectinload(WorkflowDraft.current_revision).selectinload(WorkflowDraftRevision.fact_set_version)
            )
            .where(WorkflowDraft.id == draft_id, WorkflowDraft.product_id == product_id)
            .with_for_update()
        )
        if draft is None:
            raise NotFoundError("WorkflowDraft 不存在")

        idempotent_key = session.scalar(
            select(WorkflowMaterializationKey).where(
                WorkflowMaterializationKey.product_id == product_id,
                WorkflowMaterializationKey.idempotency_key == normalized_key,
            )
        )
        if idempotent_key is not None:
            if idempotent_key.request_hash != request_hash:
                raise ConflictError("相同 idempotency key 不能用于不同的物化参数")
            session.commit()
            return _load_materialization_result(session, idempotent_key.materialization_id, created=False)

        requested_revision = session.scalar(
            select(WorkflowDraftRevision)
            .options(
                selectinload(WorkflowDraftRevision.fact_set_version),
                selectinload(WorkflowDraftRevision.visual_system_version),
            )
            .where(
                WorkflowDraftRevision.draft_id == draft.id,
                WorkflowDraftRevision.version == expected_draft_version,
            )
        )
        if requested_revision is None:
            raise ConflictError("WorkflowDraft expected version 不存在")
        existing_for_revision = session.scalar(
            select(WorkflowMaterialization).where(
                WorkflowMaterialization.draft_revision_id == requested_revision.id
            )
        )
        if existing_for_revision is not None:
            session.add(
                WorkflowMaterializationKey(
                    product_id=product_id,
                    materialization_id=existing_for_revision.id,
                    idempotency_key=normalized_key,
                    request_hash=request_hash,
                )
            )
            session.commit()
            return _load_materialization_result(session, existing_for_revision.id, created=False)

        if draft.current_revision_id != requested_revision.id:
            raise ConflictError("WorkflowDraft version 已变化，请基于最新 revision 物化")
        if draft.status != WorkflowDraftStatus.CONFIRMED or requested_revision.confirmed_at is None:
            raise ConflictError("只有 confirmed WorkflowDraft revision 可以物化")
        fact_set = requested_revision.fact_set_version
        if fact_set is None or product.current_fact_set_version_id != fact_set.id:
            raise ConflictError("商品当前事实版本与 confirmed WorkflowDraft 不一致")

        artifact = _parse_and_verify_revision(requested_revision)
        validate_workflow_draft_reference_assets(session, product_id=product_id, artifact=artifact)
        visual_system_version = requested_revision.visual_system_version
        if visual_system_version is None:
            raise ConflictError("confirmed WorkflowDraft revision 缺少视觉体系版本")
        if (
            artifact.visual_system.mode == "confirmed_version"
            and artifact.visual_system.version_id != visual_system_version.id
        ):
            raise ConflictError("WorkflowDraft 视觉体系版本绑定不一致")
        if (
            artifact.visual_system.mode == "draft"
            and visual_system_version.source_draft_revision_id != requested_revision.id
        ):
            raise ConflictError("WorkflowDraft draft 视觉体系来源不一致")

        active_workflow = session.scalar(
            select(ProductWorkflow)
            .where(ProductWorkflow.product_id == product_id, ProductWorkflow.active.is_(True))
            .with_for_update()
        )
        if active_workflow is not None and active_workflow.schema_version != 2:
            raise ConflictError("商品仍有 active v1 工作流，需先完成历史归档切换")
        latest_revision = (
            session.scalar(
                select(func.max(ProductWorkflow.revision)).where(
                    ProductWorkflow.product_id == product_id,
                    ProductWorkflow.schema_version == 2,
                )
            )
            or 0
        )
        if expected_workflow_revision != latest_revision:
            raise ConflictError("ProductWorkflow revision 已变化，请刷新后重试")

        draft.status = WorkflowDraftStatus.MATERIALIZING
        draft.updated_at = now_utc()
        if active_workflow is not None:
            active_workflow.active = False
            active_workflow.updated_at = now_utc()
            session.flush()

        workflow = ProductWorkflow(
            product_id=product_id,
            title=artifact.title,
            active=True,
            schema_version=2,
            revision=latest_revision + 1,
            source_draft_revision_id=requested_revision.id,
            visual_system_version_id=visual_system_version.id,
        )
        session.add(workflow)
        session.flush()

        prompt_versions_by_plan = _materialize_prompt_artifacts(
            session,
            workflow=workflow,
            artifact=artifact,
            draft_revision=requested_revision,
        )
        _materialize_visual_exceptions(
            session,
            workflow=workflow,
            artifact=artifact,
            draft_revision=requested_revision,
        )
        session.flush()
        folders_by_key = _materialize_folders(session, workflow=workflow, artifact=artifact)
        session.flush()
        nodes_by_key = _materialize_nodes(
            session,
            workflow=workflow,
            artifact=artifact,
            fact_set=fact_set,
            draft_revision=requested_revision,
            folders_by_key=folders_by_key,
            prompt_versions_by_plan=prompt_versions_by_plan,
        )
        session.flush()
        edges_by_key = _materialize_edges(
            session,
            workflow=workflow,
            artifact=artifact,
            nodes_by_key=nodes_by_key,
        )
        session.flush()

        materialization = WorkflowMaterialization(
            product_id=product_id,
            draft_id=draft.id,
            draft_revision_id=requested_revision.id,
            workflow_id=workflow.id,
            idempotency_key=normalized_key,
            request_hash=request_hash,
            expected_draft_version=expected_draft_version,
            expected_workflow_revision=expected_workflow_revision,
        )
        session.add(materialization)
        session.flush()
        session.add(
            WorkflowMaterializationKey(
                product_id=product_id,
                materialization_id=materialization.id,
                idempotency_key=normalized_key,
                request_hash=request_hash,
            )
        )
        _materialize_reveal_events(
            session,
            materialization=materialization,
            workflow=workflow,
            artifact=artifact,
            folders_by_key=folders_by_key,
            nodes_by_key=nodes_by_key,
            edges_by_key=edges_by_key,
        )
        session.flush()

        draft.status = WorkflowDraftStatus.READY
        draft.final_workflow_id = workflow.id
        draft.updated_at = now_utc()
        session.commit()
        return _load_materialization_result(session, materialization.id, created=True)
    except IntegrityError:
        session.rollback()
        existing_key = session.scalar(
            select(WorkflowMaterializationKey).where(
                WorkflowMaterializationKey.product_id == product_id,
                WorkflowMaterializationKey.idempotency_key == normalized_key,
            )
        )
        if existing_key is not None and existing_key.request_hash == request_hash:
            return _load_materialization_result(session, existing_key.materialization_id, created=False)
        raise ConflictError("WorkflowDraft 物化与并发写入冲突") from None
    except Exception:
        session.rollback()
        raise


def get_workflow_materialization_or_raise(
    session: Session,
    materialization_id: str,
) -> WorkflowMaterialization:
    materialization = session.scalar(
        select(WorkflowMaterialization)
        .options(selectinload(WorkflowMaterialization.reveal_events))
        .where(WorkflowMaterialization.id == materialization_id)
    )
    if materialization is None:
        raise NotFoundError("Workflow materialization 不存在")
    return materialization


def list_workflow_reveal_events(
    session: Session,
    *,
    materialization_id: str,
    after: int,
) -> list[WorkflowRevealEvent]:
    if after < 0:
        raise BusinessValidationError("reveal event cursor 不能为负数")
    if session.get(WorkflowMaterialization, materialization_id) is None:
        raise NotFoundError("Workflow materialization 不存在")
    return list(
        session.scalars(
            select(WorkflowRevealEvent)
            .where(
                WorkflowRevealEvent.materialization_id == materialization_id,
                WorkflowRevealEvent.sequence > after,
            )
            .order_by(WorkflowRevealEvent.sequence)
        )
    )


def _parse_and_verify_revision(revision: WorkflowDraftRevision) -> WorkflowDraftPayloadV1:
    artifact = parse_workflow_draft_payload_or_raise(revision.payload_json)
    if workflow_draft_payload_hash(artifact) != revision.payload_hash:
        raise ConflictError("WorkflowDraft revision payload hash 不一致")
    return artifact


def _materialize_prompt_artifacts(
    session: Session,
    *,
    workflow: ProductWorkflow,
    artifact: WorkflowDraftPayloadV1,
    draft_revision: WorkflowDraftRevision,
) -> dict[str, ImagePromptArtifactVersion]:
    versions_by_plan: dict[str, ImagePromptArtifactVersion] = {}
    for prompt_plan in artifact.prompt_plans:
        prompt_artifact = ImagePromptArtifact(
            workflow_id=workflow.id,
            image_type_key=prompt_plan.image_type_key,
            title=prompt_plan.title,
        )
        session.add(prompt_artifact)
        session.flush()
        payload_json = prompt_plan.payload.model_dump(mode="json")
        version = ImagePromptArtifactVersion(
            artifact_id=prompt_artifact.id,
            version=1,
            schema_version=1,
            payload_json=payload_json,
            payload_hash=_json_hash(payload_json),
            source_draft_revision_id=draft_revision.id,
        )
        session.add(version)
        session.flush()
        for position, asset_id in enumerate(prompt_plan.payload.evidence_asset_ids):
            session.add(
                ImagePromptArtifactVersionReference(
                    prompt_artifact_version_id=version.id,
                    asset_id=asset_id,
                    purpose="evidence",
                    position=position,
                )
            )
        versions_by_plan[prompt_plan.key] = version
    return versions_by_plan


def _materialize_visual_exceptions(
    session: Session,
    *,
    workflow: ProductWorkflow,
    artifact: WorkflowDraftPayloadV1,
    draft_revision: WorkflowDraftRevision,
) -> None:
    assert draft_revision.confirmed_at is not None
    for exception in artifact.visual_exceptions:
        session.add(
            VisualException(
                workflow_id=workflow.id,
                source_draft_revision_id=draft_revision.id,
                exception_key=exception.key,
                scope_type=exception.scope.type,
                scope_key=exception.scope.key,
                overrides_json=[override.model_dump(mode="json") for override in exception.overrides],
                reason=exception.reason,
                confirmed_at=draft_revision.confirmed_at,
            )
        )


def _materialize_folders(
    session: Session,
    *,
    workflow: ProductWorkflow,
    artifact: WorkflowDraftPayloadV1,
) -> dict[str, WorkflowFolder]:
    folders_by_key: dict[str, WorkflowFolder] = {}
    for folder_plan in sorted(artifact.folders, key=lambda item: (item.order, item.key)):
        folder = WorkflowFolder(
            workflow_id=workflow.id,
            folder_key=folder_plan.key,
            title=folder_plan.title,
            sort_order=folder_plan.order,
        )
        session.add(folder)
        folders_by_key[folder_plan.key] = folder
    return folders_by_key


def _materialize_nodes(
    session: Session,
    *,
    workflow: ProductWorkflow,
    artifact: WorkflowDraftPayloadV1,
    fact_set: ProductFactSetVersion,
    draft_revision: WorkflowDraftRevision,
    folders_by_key: dict[str, WorkflowFolder],
    prompt_versions_by_plan: dict[str, ImagePromptArtifactVersion],
) -> dict[str, WorkflowNode]:
    references_by_key = {plan.key: plan for plan in artifact.reference_bindings}
    prompts_by_key = {plan.key: plan for plan in artifact.prompt_plans}
    image_type_by_image_key = {
        image.key: image_type for image_type in artifact.image_types for image in image_type.images
    }
    images_by_key = {image.key: image for image_type in artifact.image_types for image in image_type.images}
    cover_priority_by_image_key = {
        image.key: priority
        for priority, (image_type, image) in enumerate(
            sorted(
                (
                    (image_type, image)
                    for image_type in artifact.image_types
                    for image in image_type.images
                ),
                key=lambda item: (
                    item[0].key != "hero",
                    item[0].order,
                    item[1].order,
                    item[1].key,
                ),
            )
        )
    }
    nodes_by_key: dict[str, WorkflowNode] = {}
    for plan in artifact.nodes:
        config_json, bound_asset_id, prompt_version_id = _resolved_node_config(
            plan,
            fact_set=fact_set,
            draft_revision=draft_revision,
            references_by_key=references_by_key,
            prompts_by_key=prompts_by_key,
            image_type_by_image_key=image_type_by_image_key,
            images_by_key=images_by_key,
            cover_priority_by_image_key=cover_priority_by_image_key,
            prompt_versions_by_plan=prompt_versions_by_plan,
        )
        node = WorkflowNode(
            workflow_id=workflow.id,
            schema_version=2,
            node_key=plan.key,
            node_type=plan.node_type,
            title=plan.title,
            position_x=plan.position_x,
            position_y=plan.position_y,
            folder_id=folders_by_key[plan.folder_key].id if plan.folder_key is not None else None,
            bound_image_asset_id=bound_asset_id,
            current_prompt_artifact_version_id=prompt_version_id,
            config_json=config_json,
        )
        session.add(node)
        nodes_by_key[plan.key] = node
    return nodes_by_key


def _resolved_node_config(
    plan: WorkflowNodePlan,
    *,
    fact_set: ProductFactSetVersion,
    draft_revision: WorkflowDraftRevision,
    references_by_key: dict[str, Any],
    prompts_by_key: dict[str, Any],
    image_type_by_image_key: dict[str, Any],
    images_by_key: dict[str, Any],
    cover_priority_by_image_key: dict[str, int],
    prompt_versions_by_plan: dict[str, ImagePromptArtifactVersion],
) -> tuple[dict[str, Any], str | None, str | None]:
    base = {"contract_version": 2, "source_draft_revision_id": draft_revision.id}
    if plan.node_type == WorkflowNodeType.PRODUCT_CONTEXT:
        return {**base, "fact_set_version_id": fact_set.id}, None, None
    if isinstance(plan, ReferenceImageNodePlan):
        reference = references_by_key[plan.reference_key]
        return {
            **base,
            "reference_key": reference.key,
            "role": reference.role,
            "label": reference.label,
        }, reference.asset_id, None
    if isinstance(plan, PromptGenerationNodePlan):
        prompt = prompts_by_key[plan.prompt_plan_key]
        return {
            **base,
            "prompt_plan_key": prompt.key,
            "image_type_key": prompt.image_type_key,
        }, None, prompt_versions_by_plan[prompt.key].id
    if isinstance(plan, ImageGenerationNodePlan):
        image = images_by_key[plan.image_plan_key]
        image_type = image_type_by_image_key[plan.image_plan_key]
        return {
            **base,
            "image_plan_key": image.key,
            "image_type_key": image_type.key,
            "image_type_order": image_type.order,
            "image_plan_order": image.order,
            "cover_priority": cover_priority_by_image_key[image.key],
            "prompt_plan_key": image_type.prompt_plan_key,
            "variation_instruction": image.variation_instruction,
            "generation_spec": image.generation_spec.model_dump(mode="json"),
            "delivery_spec": image.delivery_spec.model_dump(mode="json") if image.delivery_spec else None,
        }, None, None
    raise BusinessValidationError("WorkflowDraft 包含不支持的 v2 节点类型")


def _materialize_edges(
    session: Session,
    *,
    workflow: ProductWorkflow,
    artifact: WorkflowDraftPayloadV1,
    nodes_by_key: dict[str, WorkflowNode],
) -> dict[str, WorkflowEdge]:
    edges_by_key: dict[str, WorkflowEdge] = {}
    for edge_plan in artifact.edges:
        edge = WorkflowEdge(
            workflow_id=workflow.id,
            edge_key=edge_plan.key,
            source_node_id=nodes_by_key[edge_plan.source_node_key].id,
            target_node_id=nodes_by_key[edge_plan.target_node_key].id,
            source_handle=edge_plan.source_handle,
            target_handle=edge_plan.target_handle,
        )
        session.add(edge)
        edges_by_key[edge_plan.key] = edge
    return edges_by_key


def _materialize_reveal_events(
    session: Session,
    *,
    materialization: WorkflowMaterialization,
    workflow: ProductWorkflow,
    artifact: WorkflowDraftPayloadV1,
    folders_by_key: dict[str, WorkflowFolder],
    nodes_by_key: dict[str, WorkflowNode],
    edges_by_key: dict[str, WorkflowEdge],
) -> None:
    sequence = 0

    def add_event(
        kind: WorkflowRevealEventKind,
        *,
        entity_type: str | None,
        entity_id: str | None,
        payload_json: dict[str, Any],
    ) -> None:
        nonlocal sequence
        sequence += 1
        session.add(
            WorkflowRevealEvent(
                materialization_id=materialization.id,
                sequence=sequence,
                kind=kind,
                entity_type=entity_type,
                entity_id=entity_id,
                payload_json=payload_json,
            )
        )

    for folder_plan in sorted(artifact.folders, key=lambda item: (item.order, item.key)):
        folder = folders_by_key[folder_plan.key]
        add_event(
            WorkflowRevealEventKind.FOLDER,
            entity_type="folder",
            entity_id=folder.id,
            payload_json={"folder_key": folder.folder_key},
        )
    node_order = {
        WorkflowNodeType.PRODUCT_CONTEXT: 0,
        WorkflowNodeType.REFERENCE_IMAGE: 1,
        WorkflowNodeType.PROMPT_GENERATION: 2,
        WorkflowNodeType.IMAGE_GENERATION: 3,
    }
    for node_plan in sorted(artifact.nodes, key=lambda item: (node_order[item.node_type], item.key)):
        node = nodes_by_key[node_plan.key]
        add_event(
            WorkflowRevealEventKind.NODE,
            entity_type="node",
            entity_id=node.id,
            payload_json={"node_key": node.node_key, "node_type": node.node_type.value},
        )
    for edge_plan in artifact.edges:
        edge = edges_by_key[edge_plan.key]
        add_event(
            WorkflowRevealEventKind.EDGE,
            entity_type="edge",
            entity_id=edge.id,
            payload_json={"edge_key": edge.edge_key},
        )
    add_event(
        WorkflowRevealEventKind.COMPLETED,
        entity_type="workflow",
        entity_id=workflow.id,
        payload_json={"workflow_id": workflow.id, "workflow_revision": workflow.revision},
    )


def _load_materialization_result(
    session: Session,
    materialization_id: str,
    *,
    created: bool,
) -> WorkflowMaterializationResult:
    materialization = session.scalar(
        select(WorkflowMaterialization)
        .options(
            selectinload(WorkflowMaterialization.reveal_events),
            selectinload(WorkflowMaterialization.workflow).selectinload(ProductWorkflow.folders),
            selectinload(WorkflowMaterialization.workflow).selectinload(ProductWorkflow.nodes),
            selectinload(WorkflowMaterialization.workflow)
            .selectinload(ProductWorkflow.nodes)
            .selectinload(WorkflowNode.current_prompt_artifact_version),
            selectinload(WorkflowMaterialization.workflow).selectinload(ProductWorkflow.edges),
            selectinload(WorkflowMaterialization.workflow)
            .selectinload(ProductWorkflow.prompt_artifacts)
            .selectinload(ImagePromptArtifact.versions),
            selectinload(WorkflowMaterialization.workflow).selectinload(ProductWorkflow.visual_exceptions),
        )
        .where(WorkflowMaterialization.id == materialization_id)
    )
    if materialization is None:
        raise NotFoundError("Workflow materialization 不存在")
    return WorkflowMaterializationResult(
        materialization=materialization,
        workflow=materialization.workflow,
        created=created,
    )


def _normalize_idempotency_key(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("idempotency key 不能为空")
    if len(normalized) > 120:
        raise BusinessValidationError("idempotency key 不能超过 120 个字符")
    return normalized


def _materialization_request_hash(
    *,
    product_id: str,
    draft_id: str,
    expected_draft_version: int,
    expected_workflow_revision: int,
) -> str:
    payload = {
        "product_id": product_id,
        "draft_id": draft_id,
        "expected_draft_version": expected_draft_version,
        "expected_workflow_revision": expected_workflow_revision,
    }
    encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def _json_hash(payload: dict[str, Any]) -> str:
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


__all__ = [
    "ActiveV2WorkflowSnapshot",
    "WorkflowMaterializationResult",
    "get_active_v2_workflow_snapshot",
    "get_workflow_materialization_or_raise",
    "list_workflow_reveal_events",
    "materialize_workflow_draft",
    "v2_workflow_query",
]
