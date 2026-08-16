from __future__ import annotations

from copy import deepcopy

import pytest
from helpers import _make_demo_image_bytes
from sqlalchemy import func, select
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.product_workflow.v2_node_editing import (
    get_v2_workflow_node_detail,
    update_v2_image_node,
    update_v2_prompt_node,
    update_v2_reference_node,
)
from productflow_backend.application.use_cases import create_canonical_product
from productflow_backend.application.workflow_drafts.contracts import (
    DeliverySpec,
    GenerationSpec,
    ImagePromptPayloadV1,
)
from productflow_backend.application.workflow_drafts.materialization import materialize_workflow_draft
from productflow_backend.application.workflow_drafts.service import (
    confirm_workflow_draft_revision,
    create_workflow_draft,
)
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowNodeType, WorkflowRunStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.infrastructure.db.models import (
    ImagePromptArtifactVersion,
    WorkflowNodeRun,
    WorkflowRun,
)


def _create_materialized_workflow(db_session):
    product = create_canonical_product(
        db_session,
        name="硬质刀具收纳套装",
        category="工业收纳",
        price="299.00",
        source_note="五款收纳盘，橙蓝配色。",
        image_uploads=[(_make_demo_image_bytes(), "product.png", "image/png")],
    )
    payload = make_workflow_draft_payload(reference_asset_id=product.image_assets[0].id)
    draft = create_workflow_draft(
        db_session,
        product_id=product.id,
        payload=payload,
        ready_for_confirmation=True,
        source_turn_id="turn-node-edit",
        source_artifact_step_id="artifact-node-edit",
    )
    confirm_workflow_draft_revision(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
    )
    result = materialize_workflow_draft(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
        expected_workflow_revision=0,
        idempotency_key="materialize-node-edit",
    )
    return product, result.workflow


def test_prompt_node_detail_and_manual_edit_append_immutable_version(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    prompt_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION)
    image_nodes = [node for node in workflow.nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION]
    for image_node in image_nodes:
        image_node.status = WorkflowNodeStatus.SUCCEEDED
        image_node.bound_image_asset_id = product.image_assets[0].id
    db_session.commit()

    detail = get_v2_workflow_node_detail(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=prompt_node.id,
    )
    assert detail.prompt_artifact is not None
    initial_version = detail.prompt_artifact.version
    initial_edit_version = workflow.edit_version
    next_payload_json = deepcopy(detail.prompt_artifact.payload.model_dump(mode="json"))
    next_payload_json["design_goal"] = "突出完整套装、秩序和专业生产效率"
    next_payload = ImagePromptPayloadV1.model_validate(next_payload_json)

    result = update_v2_prompt_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=prompt_node.id,
        expected_edit_version=initial_edit_version,
        expected_prompt_artifact_version_id=initial_version.id,
        title="首屏海报提示词",
        payload=next_payload,
    )

    assert result.changed is True
    assert result.workflow.edit_version == initial_edit_version + 1
    refreshed_prompt = next(node for node in result.workflow.nodes if node.id == prompt_node.id)
    assert refreshed_prompt.current_prompt_artifact_version_id != initial_version.id
    assert refreshed_prompt.status == WorkflowNodeStatus.SUCCEEDED
    assert refreshed_prompt.output_json["source"] == "manual_edit"
    assert all(
        node.status == WorkflowNodeStatus.IDLE
        for node in result.workflow.nodes
        if node.node_type == WorkflowNodeType.IMAGE_GENERATION
    )
    assert all(
        node.bound_image_asset_id == product.image_assets[0].id
        for node in result.workflow.nodes
        if node.node_type == WorkflowNodeType.IMAGE_GENERATION
    )
    versions = list(
        db_session.scalars(
            select(ImagePromptArtifactVersion)
            .where(ImagePromptArtifactVersion.artifact_id == initial_version.artifact_id)
            .order_by(ImagePromptArtifactVersion.version)
        )
    )
    assert [version.version for version in versions] == [1, 2]
    assert versions[0].payload_json["design_goal"] != versions[1].payload_json["design_goal"]
    assert versions[1].source_draft_revision_id is None
    assert versions[1].source_node_run_id is None

    no_change = update_v2_prompt_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=prompt_node.id,
        expected_edit_version=result.workflow.edit_version,
        expected_prompt_artifact_version_id=refreshed_prompt.current_prompt_artifact_version_id,
        title=refreshed_prompt.title,
        payload=next_payload,
    )
    assert no_change.changed is False
    assert no_change.workflow.edit_version == result.workflow.edit_version
    assert db_session.scalar(
        select(func.count())
        .select_from(ImagePromptArtifactVersion)
        .where(ImagePromptArtifactVersion.artifact_id == initial_version.artifact_id)
    ) == 2


def test_prompt_edit_rejects_plan_drift_and_stale_versions(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    prompt_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION)
    detail = get_v2_workflow_node_detail(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=prompt_node.id,
    )
    assert detail.prompt_artifact is not None
    payload_json = detail.prompt_artifact.payload.model_dump(mode="json")
    payload_json["images"] = list(reversed(payload_json["images"]))

    with pytest.raises(BusinessValidationError, match="逐图计划和顺序"):
        update_v2_prompt_node(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            node_id=prompt_node.id,
            expected_edit_version=workflow.edit_version,
            expected_prompt_artifact_version_id=detail.prompt_artifact.version.id,
            title=prompt_node.title,
            payload=ImagePromptPayloadV1.model_validate(payload_json),
        )

    with pytest.raises(ConflictError, match="current version 已变化"):
        update_v2_prompt_node(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            node_id=prompt_node.id,
            expected_edit_version=workflow.edit_version,
            expected_prompt_artifact_version_id="00000000-0000-0000-0000-000000000000",
            title=prompt_node.title,
            payload=detail.prompt_artifact.payload,
        )


def test_image_node_edit_separates_generation_and_delivery_changes(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    image_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION)
    image_node.status = WorkflowNodeStatus.SUCCEEDED
    image_node.bound_image_asset_id = product.image_assets[0].id
    db_session.commit()
    initial_edit_version = workflow.edit_version
    generation_spec = GenerationSpec.model_validate(image_node.config_json["generation_spec"])

    delivery_result = update_v2_image_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=image_node.id,
        expected_edit_version=initial_edit_version,
        title=image_node.title,
        variation_instruction=image_node.config_json.get("variation_instruction"),
        generation_spec=generation_spec,
        delivery_spec=DeliverySpec(
            width=1200,
            height=1200,
            format="webp",
            fit="contain",
            background_color="#F2F2F2",
        ),
    )
    delivered_node = next(node for node in delivery_result.workflow.nodes if node.id == image_node.id)
    assert delivered_node.status == WorkflowNodeStatus.SUCCEEDED
    assert delivered_node.bound_image_asset_id == product.image_assets[0].id

    changed_generation = generation_spec.model_copy(update={"quality_intent": "standard"})
    generation_result = update_v2_image_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=image_node.id,
        expected_edit_version=delivery_result.workflow.edit_version,
        title=image_node.title,
        variation_instruction="改为正面视角并保留右侧文案空间",
        generation_spec=changed_generation,
        delivery_spec=DeliverySpec.model_validate(delivered_node.config_json["delivery_spec"]),
    )
    generated_node = next(node for node in generation_result.workflow.nodes if node.id == image_node.id)
    assert generated_node.status == WorkflowNodeStatus.IDLE
    assert generated_node.bound_image_asset_id == product.image_assets[0].id
    assert generated_node.config_json["generation_spec"]["quality_intent"] == "standard"

    with pytest.raises(ConflictError, match="edit version 已变化"):
        update_v2_image_node(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            node_id=image_node.id,
            expected_edit_version=initial_edit_version,
            title=image_node.title,
            variation_instruction=None,
            generation_spec=generation_spec,
            delivery_spec=None,
        )


def test_reference_semantic_edit_marks_downstream_stale_and_rejects_active_run(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    reference_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.REFERENCE_IMAGE)
    prompt_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION)
    initial_edit_version = workflow.edit_version
    run = WorkflowRun(workflow_id=workflow.id, status=WorkflowRunStatus.RUNNING)
    db_session.add(run)
    db_session.flush()
    prompt_node.status = WorkflowNodeStatus.QUEUED
    db_session.add(
        WorkflowNodeRun(
            workflow_run_id=run.id,
            node_id=prompt_node.id,
            status=WorkflowNodeStatus.QUEUED,
        )
    )
    db_session.commit()

    with pytest.raises(ConflictError, match="正在运行"):
        update_v2_reference_node(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            node_id=reference_node.id,
            expected_edit_version=initial_edit_version,
            title=reference_node.title,
            role="product_detail",
            label="商品细节参考",
        )

    node_run = db_session.scalar(select(WorkflowNodeRun).where(WorkflowNodeRun.node_id == prompt_node.id))
    assert node_run is not None
    node_run.status = WorkflowNodeStatus.FAILED
    run.status = WorkflowRunStatus.CANCELLED
    db_session.commit()
    result = update_v2_reference_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=reference_node.id,
        expected_edit_version=initial_edit_version,
        title=reference_node.title,
        role="product_detail",
        label="商品细节参考",
    )
    refreshed_prompt = next(node for node in result.workflow.nodes if node.id == prompt_node.id)
    assert refreshed_prompt.status == WorkflowNodeStatus.IDLE
    assert refreshed_prompt.output_json["references_stale"] is True


def test_target_node_title_edit_rejects_active_run(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    image_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION)
    run = WorkflowRun(workflow_id=workflow.id, status=WorkflowRunStatus.RUNNING)
    db_session.add(run)
    db_session.flush()
    image_node.status = WorkflowNodeStatus.RUNNING
    db_session.add(
        WorkflowNodeRun(
            workflow_run_id=run.id,
            node_id=image_node.id,
            status=WorkflowNodeStatus.RUNNING,
            active_attempt_id="node-edit-active-attempt",
        )
    )
    db_session.commit()

    with pytest.raises(ConflictError, match="正在运行"):
        update_v2_image_node(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            node_id=image_node.id,
            expected_edit_version=workflow.edit_version,
            title=f"{image_node.title}（更新）",
            variation_instruction=image_node.config_json.get("variation_instruction"),
            generation_spec=GenerationSpec.model_validate(image_node.config_json["generation_spec"]),
            delivery_spec=(
                DeliverySpec.model_validate(image_node.config_json["delivery_spec"])
                if image_node.config_json.get("delivery_spec") is not None
                else None
            ),
        )

    db_session.refresh(workflow)
    db_session.refresh(image_node)
    assert workflow.edit_version == 0
    assert "（更新）" not in image_node.title
