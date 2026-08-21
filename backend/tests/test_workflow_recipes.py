from __future__ import annotations

import json
from datetime import UTC, datetime

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from sqlalchemy import select
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.agent.conversations import (
    attach_agent_workflow_draft_artifact,
    bind_harness_turn,
    project_agent_turn_state,
    reserve_agent_turn,
)
from productflow_backend.application.agent.tools import get_agent_contract, get_agent_product_context
from productflow_backend.application.product_workflow.folders import WorkflowNodePosition, update_workflow_node_layout
from productflow_backend.application.product_workflow.v2_graph_commands import (
    create_v2_reference_node,
    duplicate_v2_workflow_node,
)
from productflow_backend.application.product_workflow.v2_node_editing import (
    update_v2_image_node,
    update_v2_prompt_node,
)
from productflow_backend.application.product_workflow.graph_direct_create import create_product_with_direct_graph
from productflow_backend.application.product_workflow.graph_template import DirectCreateImageType
from productflow_backend.application.products import create_canonical_product, delete_product
from productflow_backend.application.workflow_drafts.contracts import GenerationSpec, ImagePromptPayloadV1
from productflow_backend.application.workflow_drafts.materialization import materialize_workflow_draft
from productflow_backend.application.workflow_drafts.service import (
    append_workflow_draft_revision,
    confirm_workflow_draft_revision,
    create_workflow_draft,
)
from productflow_backend.application.workflow_recipes.extractor import (
    assert_recipe_payload_has_no_entity_ids,
    assert_recipe_payload_has_no_forbidden_keys,
)
from productflow_backend.application.workflow_recipes.service import (
    append_workflow_recipe_version,
    apply_workflow_recipe,
    archive_workflow_recipe,
    create_workflow_recipe,
    list_workflow_recipes,
)
from productflow_backend.domain.enums import AgentTurnStatus, WorkflowNodeType
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    ImagePromptArtifactVersion,
    Product,
    ProductWorkflow,
    WorkflowDraft,
    WorkflowRecipe,
    WorkflowRecipeVersion,
)
from productflow_backend.infrastructure.db.session import get_session_factory


def _materialize_recipe_source(db_session):
    product = create_canonical_product(
        db_session,
        name="配方来源商品",
        category="工业收纳",
        price="299.00",
        source_note="五款收纳盘，橙蓝配色。",
        image_uploads=[(_make_demo_image_bytes(), "product.png", "image/png")],
    )
    source_payload = make_workflow_draft_payload(reference_asset_id=product.image_assets[0].id)
    draft = create_workflow_draft(
        db_session,
        product_id=product.id,
        payload=source_payload,
        ready_for_confirmation=True,
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
        idempotency_key="recipe-source-materialize",
    )
    return product, result.workflow, source_payload


def _current_entity_ids(product, workflow) -> set[str]:
    revision = workflow.source_draft_revision
    return {
        product.id,
        *(asset.id for asset in product.image_assets),
        workflow.id,
        workflow.source_draft_revision_id,
        workflow.visual_system_version_id,
        workflow.materialization.id,
        revision.id,
        revision.draft_id,
        *(folder.id for folder in workflow.folders),
        *(node.id for node in workflow.nodes),
        *(edge.id for edge in workflow.edges),
        *(artifact.id for artifact in workflow.prompt_artifacts),
        *(
            version.id
            for artifact in workflow.prompt_artifacts
            for version in artifact.versions
        ),
    }


def test_full_workflow_recipe_uses_local_keys_and_excludes_current_product_content(db_session) -> None:
    product, workflow, source_payload = _materialize_recipe_source(db_session)

    recipe = create_workflow_recipe(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        source_type="workflow",
        folder_id=None,
        node_ids=[],
        expected_edit_version=0,
        title="工业商品全套图片",
        description="保留图片类型、数量和生成规格。",
    )

    assert recipe.kind.value == "workflow_recipe"
    assert recipe.current_version.version == 1
    assert len(recipe.versions) == 1
    payload = recipe.current_version.payload_json
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True)
    assert_recipe_payload_has_no_forbidden_keys(payload)
    assert_recipe_payload_has_no_entity_ids(payload, _current_entity_ids(product, workflow))
    assert "source_draft_revision_id" not in encoded
    assert "asset_id" not in encoded
    assert "硬质刀具 分类收纳" not in encoded
    assert "一站式解决 车间杂乱难题" not in encoded
    assert "套装完整性" not in encoded
    assert "主灯从左上方照射" not in encoded
    assert source_payload["prompt_plans"][0]["payload"]["design_goal"] not in encoded
    assert [node["key"] for node in payload["nodes"]] == [
        f"node_{index}" for index in range(1, len(workflow.nodes) + 1)
    ]
    assert all(node["position_x"] >= 0 and node["position_y"] >= 0 for node in payload["nodes"])
    assert payload["image_types"][0]["default_quantity"] == 2
    assert payload["image_types"][0]["images"][0]["generation_spec"]["aspect_ratio"] == "1:1"
    assert payload["prompt_shapes"][0]["text_slots"] == ["headline", "subtitle"]
    assert payload["prompt_shapes"][0]["fact_keys"] == ["product_name"]
    assert payload["reference_requirements"][0]["role"] == "product_identity"


def test_recipe_extracts_current_runtime_nodes_prompt_and_generation_specs(db_session) -> None:
    product, workflow, _ = _materialize_recipe_source(db_session)
    original_prompt = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION)
    original_image_ids = {
        node.id
        for node in workflow.nodes
        if node.node_type == WorkflowNodeType.IMAGE_GENERATION
        and node.config_json.get("prompt_plan_key") == original_prompt.config_json.get("prompt_plan_key")
    }

    duplicated_type = duplicate_v2_workflow_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=original_prompt.id,
        expected_edit_version=0,
    )
    workflow = duplicated_type.workflow
    original_image = next(node for node in workflow.nodes if node.id in original_image_ids)
    duplicated_image_result = duplicate_v2_workflow_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=original_image.id,
        expected_edit_version=1,
    )
    workflow = duplicated_image_result.workflow
    duplicated_image = next(
        node
        for node in workflow.nodes
        if node.node_type == WorkflowNodeType.IMAGE_GENERATION
        and node.config_json.get("prompt_plan_key") == original_prompt.config_json.get("prompt_plan_key")
        and node.id not in original_image_ids
    )
    current_prompt = next(node for node in workflow.nodes if node.id == original_prompt.id)
    prompt_version = db_session.get(ImagePromptArtifactVersion, current_prompt.current_prompt_artifact_version_id)
    assert prompt_version is not None
    current_prompt_payload = ImagePromptPayloadV1.model_validate(prompt_version.payload_json)
    current_prompt_payload = current_prompt_payload.model_copy(
        update={
            "composition": current_prompt_payload.composition.model_copy(
                update={"product_share_percent": 61.0}
            )
        }
    )
    prompt_updated = update_v2_prompt_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=current_prompt.id,
        expected_edit_version=2,
        expected_prompt_artifact_version_id=prompt_version.id,
        title="当前运行时提示词",
        payload=current_prompt_payload,
    )
    workflow = prompt_updated.workflow
    duplicated_image = next(node for node in workflow.nodes if node.id == duplicated_image.id)
    generation_spec = GenerationSpec.model_validate(duplicated_image.config_json["generation_spec"]).model_copy(
        update={"aspect_ratio": "4:5"}
    )
    image_updated = update_v2_image_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=duplicated_image.id,
        expected_edit_version=3,
        title=duplicated_image.title,
        variation_instruction=duplicated_image.config_json.get("variation_instruction"),
        generation_spec=generation_spec,
        delivery_spec=None,
    )
    workflow = image_updated.workflow
    reference_created = create_v2_reference_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        expected_edit_version=4,
        title="新增材质参考",
        role="material_detail",
        label="表面纹理",
        position_x=640,
        position_y=720,
    )
    workflow = reference_created.workflow

    recipe = create_workflow_recipe(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        source_type="workflow",
        folder_id=None,
        node_ids=[],
        expected_edit_version=5,
        title="当前运行时配方",
        description=None,
    )

    payload = recipe.current_version.payload_json
    assert len(payload["nodes"]) == len(workflow.nodes)
    assert sorted(image_type["default_quantity"] for image_type in payload["image_types"]) == [2, 3]
    assert any(
        image["generation_spec"]["aspect_ratio"] == "4:5"
        for image_type in payload["image_types"]
        for image in image_type["images"]
    )
    assert any(shape["product_share_percent"] == 61.0 for shape in payload["prompt_shapes"])
    assert any(image_type["title"] == "当前运行时提示词" for image_type in payload["image_types"])
    assert {item["role"] for item in payload["reference_requirements"]} == {
        "material_detail",
        "product_identity",
    }


def test_recipe_fragments_keep_internal_edges_and_summarize_boundaries(db_session) -> None:
    product, workflow, _ = _materialize_recipe_source(db_session)
    folder = workflow.folders[0]
    folder_member_ids = [node.id for node in workflow.nodes if node.folder_id == folder.id]

    folder_recipe = create_workflow_recipe(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        source_type="folder",
        folder_id=folder.id,
        node_ids=[],
        expected_edit_version=0,
        title="首屏图节点组",
        description=None,
    )
    payload = folder_recipe.current_version.payload_json
    assert folder_recipe.kind.value == "recipe_fragment"
    assert len(payload["nodes"]) == len(folder_member_ids)
    assert len(payload["folders"]) == 1
    assert payload["folders"][0]["key"] == "folder_1"
    assert len(payload["edges"]) == 3
    assert len(payload["boundary_requirements"]) == 1
    boundary = payload["boundary_requirements"][0]
    assert boundary["direction"] == "inbound"
    assert boundary["external_node_type"] == "product_context"

    image_node = next(node for node in workflow.nodes if node.node_type.value == "image_generation")
    selection_recipe = create_workflow_recipe(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        source_type="selection",
        folder_id=None,
        node_ids=[image_node.id],
        expected_edit_version=0,
        title="单张生图片段",
        description="只有图片节点，提示词作为外部依赖。",
    )
    selection_payload = selection_recipe.current_version.payload_json
    assert len(selection_payload["nodes"]) == 1
    assert selection_payload["edges"] == []
    assert selection_payload["boundary_requirements"][0]["direction"] == "inbound"
    assert selection_payload["boundary_requirements"][0]["external_node_type"] == "prompt_generation"
    assert selection_payload["image_types"][0]["default_quantity"] == 1
    assert len(selection_payload["prompt_shapes"][0]["per_image_slots"]) == 1


def test_recipe_versions_are_append_only_and_archive_hides_identity(db_session) -> None:
    product, workflow, _ = _materialize_recipe_source(db_session)
    recipe = create_workflow_recipe(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        source_type="workflow",
        folder_id=None,
        node_ids=[],
        expected_edit_version=0,
        title="版本一",
        description=None,
    )
    first_version_id = recipe.current_version_id
    first_payload_hash = recipe.current_version.payload_hash

    appended = append_workflow_recipe_version(
        db_session,
        recipe_id=recipe.id,
        expected_recipe_version=1,
        product_id=product.id,
        workflow_id=workflow.id,
        source_type="workflow",
        folder_id=None,
        node_ids=[],
        expected_edit_version=0,
        title="版本二",
        description="用户重新保存。",
    )
    assert appended.current_version.version == 2
    assert len(appended.versions) == 2
    assert appended.versions[0].id == first_version_id
    assert appended.versions[0].payload_hash == first_payload_hash
    assert appended.versions[0].title == "版本一"

    with pytest.raises(ConflictError, match="版本已变化"):
        append_workflow_recipe_version(
            db_session,
            recipe_id=recipe.id,
            expected_recipe_version=1,
            product_id=product.id,
            workflow_id=workflow.id,
            source_type="workflow",
            folder_id=None,
            node_ids=[],
            expected_edit_version=0,
            title="过期版本",
            description=None,
        )

    archived = archive_workflow_recipe(
        db_session,
        recipe_id=recipe.id,
        expected_recipe_version=2,
    )
    assert archived.changed is True
    assert archived.recipe.archived_at is not None
    assert list_workflow_recipes(db_session) == []
    assert [item.id for item in list_workflow_recipes(db_session, include_archived=True)] == [recipe.id]
    replay = archive_workflow_recipe(
        db_session,
        recipe_id=recipe.id,
        expected_recipe_version=2,
    )
    assert replay.changed is False


def test_source_product_delete_is_rejected_while_recipe_uses_its_visual_version(db_session) -> None:
    product, workflow, _ = _materialize_recipe_source(db_session)
    recipe = create_workflow_recipe(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        source_type="workflow",
        folder_id=None,
        node_ids=[],
        expected_edit_version=0,
        title="保留视觉建议的配方",
        description=None,
    )

    with pytest.raises(ConflictError, match="视觉体系仍被其他商品使用"):
        delete_product(db_session, product_id=product.id)

    assert db_session.get(Product, product.id) is not None
    assert db_session.get(WorkflowRecipe, recipe.id) is not None


def test_recipe_save_rejects_stale_canvas_and_sanitizer_fails_closed(db_session) -> None:
    product, workflow, _ = _materialize_recipe_source(db_session)
    node = workflow.nodes[0]
    update_workflow_node_layout(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        positions=[
            WorkflowNodePosition(
                node_id=node.id,
                position_x=node.position_x + 1,
                position_y=node.position_y,
            )
        ],
        expected_edit_version=0,
    )
    with pytest.raises(ConflictError, match="edit version"):
        create_workflow_recipe(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            source_type="workflow",
            folder_id=None,
            node_ids=[],
            expected_edit_version=0,
            title="过期画布",
            description=None,
        )

    with pytest.raises(BusinessValidationError, match="禁止字段"):
        assert_recipe_payload_has_no_forbidden_keys({"safe": {"provider_model": "secret"}})
    with pytest.raises(BusinessValidationError, match="泄漏当前实体 ID"):
        assert_recipe_payload_has_no_entity_ids(
            {"safe": "prefix-current-workflow-id-suffix"},
            {"current-workflow-id"},
        )


def test_recipe_apply_creates_version_zero_then_first_artifact_can_materialize(db_session) -> None:
    source_product, source_workflow, _ = _materialize_recipe_source(db_session)
    recipe = create_workflow_recipe(
        db_session,
        product_id=source_product.id,
        workflow_id=source_workflow.id,
        source_type="workflow",
        folder_id=None,
        node_ids=[],
        expected_edit_version=0,
        title="跨商品结构配方",
        description="只作为 Agent 重建结构的种子。",
    )
    target = create_canonical_product(
        db_session,
        name="目标商品",
        category="工具收纳",
        price="399.00",
        source_note="目标商品事实必须重新核对。",
        image_uploads=[(_make_demo_image_bytes(), "target.png", "image/png")],
    )
    assert db_session.scalar(
        select(ProductWorkflow).where(ProductWorkflow.product_id == target.id)
    ) is None

    applied = apply_workflow_recipe(
        db_session,
        product_id=target.id,
        recipe_id=recipe.id,
        expected_recipe_version=1,
        idempotency_key="target-apply-1",
    )
    assert applied.created is True
    assert applied.draft.status.value == "collecting"
    assert applied.draft.current_revision_id is None
    assert applied.draft.current_revision is None
    assert applied.draft.revisions == []
    assert applied.draft.recipe_seed is not None
    assert applied.draft.recipe_seed.recipe_version_id == recipe.current_version_id
    assert applied.draft.recipe_seed.base_workflow_id is None
    assert applied.conversation.workflow_draft_id == applied.draft.id
    assert db_session.scalar(
        select(ProductWorkflow).where(ProductWorkflow.product_id == target.id)
    ) is None

    replay = apply_workflow_recipe(
        db_session,
        product_id=target.id,
        recipe_id=recipe.id,
        expected_recipe_version=1,
        idempotency_key="target-apply-1",
    )
    assert replay.created is False
    assert replay.draft.id == applied.draft.id
    assert replay.conversation.id == applied.conversation.id
    assert len(list(db_session.scalars(select(WorkflowDraft).where(WorkflowDraft.product_id == target.id)))) == 1
    assert len(
        list(db_session.scalars(select(AgentConversation).where(AgentConversation.product_id == target.id)))
    ) == 1

    contract = get_agent_contract(db_session, applied.conversation.id)
    assert contract["current_draft_version"] == 0
    context = get_agent_product_context(db_session, applied.conversation.id)
    assert context["workflow_draft"] == {
        "id": applied.draft.id,
        "status": "collecting",
        "version": 0,
        "payload": None,
        "intake": None,
    }
    seed_context = context["workflow_recipe_seed"]
    assert seed_context["recipe_id"] == recipe.id
    assert seed_context["recipe_version"] == 1
    assert seed_context["payload"] == recipe.current_version.payload_json
    assert seed_context["base_workflow"] is None
    assert "不得把 recipe payload 直接作为 WorkflowDraft" in contract["system_prompt"]

    target_payload = make_workflow_draft_payload(reference_asset_id=target.image_assets[0].id)
    target_payload["title"] = "目标商品工作流"
    first_revision = append_workflow_draft_revision(
        db_session,
        product_id=target.id,
        draft_id=applied.draft.id,
        expected_draft_version=0,
        payload=target_payload,
        ready_for_confirmation=True,
        source_turn_id="target-turn-1",
        source_artifact_step_id="target-artifact-1",
    )
    assert first_revision.current_revision is not None
    assert first_revision.current_revision.version == 1
    assert len(first_revision.revisions) == 1
    with pytest.raises(ConflictError, match="version 已变化"):
        append_workflow_draft_revision(
            db_session,
            product_id=target.id,
            draft_id=applied.draft.id,
            expected_draft_version=0,
            payload=target_payload,
            ready_for_confirmation=True,
        )

    confirmed = confirm_workflow_draft_revision(
        db_session,
        product_id=target.id,
        draft_id=applied.draft.id,
        expected_draft_version=1,
    )
    assert confirmed.status.value == "confirmed"
    materialized = materialize_workflow_draft(
        db_session,
        product_id=target.id,
        draft_id=applied.draft.id,
        expected_draft_version=1,
        expected_workflow_revision=0,
        idempotency_key="target-materialize-1",
    )
    assert materialized.workflow.product_id == target.id
    assert materialized.workflow.title == "目标商品工作流"
    assert materialized.workflow.source_draft_revision_id == confirmed.current_revision_id


def test_fragment_apply_records_target_base_workflow_and_idempotency_drift(db_session) -> None:
    product, workflow, _ = _materialize_recipe_source(db_session)
    folder = workflow.folders[0]
    fragment = create_workflow_recipe(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        source_type="folder",
        folder_id=folder.id,
        node_ids=[],
        expected_edit_version=0,
        title="目标工作流片段",
        description=None,
    )
    applied = apply_workflow_recipe(
        db_session,
        product_id=product.id,
        recipe_id=fragment.id,
        expected_recipe_version=1,
        idempotency_key="fragment-apply-1",
    )
    seed = applied.draft.recipe_seed
    assert seed is not None
    assert seed.base_workflow_id == workflow.id
    assert seed.base_workflow_revision == workflow.revision
    context = get_agent_product_context(db_session, applied.conversation.id)
    assert context["workflow_recipe_seed"]["base_workflow"]["id"] == workflow.id
    assert len(context["workflow_recipe_seed"]["base_workflow"]["nodes"]) == len(workflow.nodes)

    with pytest.raises(ConflictError, match="相同 idempotency key"):
        apply_workflow_recipe(
            db_session,
            product_id=product.id,
            recipe_id=fragment.id,
            expected_recipe_version=2,
            idempotency_key="fragment-apply-1",
        )


def test_fragment_apply_conflicts_on_v3_product(db_session) -> None:
    product, workflow, _ = _materialize_recipe_source(db_session)
    folder = workflow.folders[0]
    fragment = create_workflow_recipe(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        source_type="folder",
        folder_id=folder.id,
        node_ids=[],
        expected_edit_version=0,
        title="v3 目标片段",
        description=None,
    )
    v3 = create_product_with_direct_graph(
        db_session,
        name="v3 片段目标",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "v3.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    with pytest.raises(ConflictError, match="schema-v3"):
        apply_workflow_recipe(
            db_session,
            product_id=v3.product.id,
            recipe_id=fragment.id,
            expected_recipe_version=1,
            idempotency_key="fragment-v3-conflict",
        )


def test_version_zero_agent_artifact_sync_creates_first_revision_idempotently(db_session) -> None:
    source_product, source_workflow, _ = _materialize_recipe_source(db_session)
    recipe = create_workflow_recipe(
        db_session,
        product_id=source_product.id,
        workflow_id=source_workflow.id,
        source_type="workflow",
        folder_id=None,
        node_ids=[],
        expected_edit_version=0,
        title="Agent 首次制品配方",
        description=None,
    )
    target = create_canonical_product(
        db_session,
        name="Agent 配方目标",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "agent-target.png", "image/png")],
    )
    applied = apply_workflow_recipe(
        db_session,
        product_id=target.id,
        recipe_id=recipe.id,
        expected_recipe_version=1,
        idempotency_key="agent-artifact-apply",
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=target.id,
        conversation_id=applied.conversation.id,
        input_text="请按配方重建目标商品工作流",
        input_asset_ids=[target.image_assets[0].id],
        idempotency_key="agent-artifact-turn",
    ).projection
    projection = bind_harness_turn(
        db_session,
        product_id=target.id,
        conversation_id=applied.conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-recipe-turn",
        status=AgentTurnStatus.RUNNING,
    )
    projection = project_agent_turn_state(
        db_session,
        product_id=target.id,
        conversation_id=applied.conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-recipe-turn",
        status=AgentTurnStatus.AWAITING_CONFIRMATION,
        output_text="已重建目标商品草案。",
        error_text=None,
        question_json=None,
        finished_at=datetime.now(UTC),
    )
    payload = make_workflow_draft_payload(reference_asset_id=target.image_assets[0].id)
    synced = attach_agent_workflow_draft_artifact(
        db_session,
        product_id=target.id,
        conversation_id=applied.conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-recipe-turn",
        artifact_name="propose_workflow_draft",
        artifact_step_id="recipe-artifact-step",
        artifact_value=payload,
    )
    assert synced.workflow_draft_revision_id is not None
    refreshed = db_session.get(WorkflowDraft, applied.draft.id)
    db_session.refresh(refreshed)
    assert refreshed.current_revision.version == 1
    assert len(refreshed.revisions) == 1

    repeated = attach_agent_workflow_draft_artifact(
        db_session,
        product_id=target.id,
        conversation_id=applied.conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-recipe-turn",
        artifact_name="propose_workflow_draft",
        artifact_step_id="recipe-artifact-step",
        artifact_value=payload,
    )
    assert repeated.workflow_draft_revision_id == synced.workflow_draft_revision_id
    assert len(refreshed.revisions) == 1


def test_recipe_api_starts_empty_and_uses_saved_recipes_only(configured_env) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    listed = client.get("/api/v2/workflow-recipes")
    assert listed.status_code == 200
    assert listed.json() == []

    product_response = client.post(
        "/api/v2/products",
        data={"name": "配方 API 商品"},
        files=[("images", ("product.png", _make_demo_image_bytes(), "image/png"))],
    )
    product = product_response.json()
    product_id = product["product"]["id"]
    asset_id = product["created_assets"][0]["id"]
    draft = client.post(
        f"/api/v2/products/{product_id}/workflow-drafts",
        json={"payload": make_workflow_draft_payload(reference_asset_id=asset_id), "ready_for_confirmation": True},
    ).json()
    assert client.post(
        f"/api/v2/products/{product_id}/workflow-drafts/{draft['id']}/confirm",
        json={"expected_draft_version": 1},
    ).status_code == 200
    workflow = client.post(
        f"/api/v2/products/{product_id}/workflow-drafts/{draft['id']}/materialize",
        json={
            "expected_draft_version": 1,
            "expected_workflow_revision": 0,
            "idempotency_key": "recipe-api-materialize",
        },
    ).json()["workflow"]
    created = client.post(
        f"/api/v2/products/{product_id}/workflows/{workflow['id']}/recipes",
        json={
            "source_type": "workflow",
            "expected_edit_version": 0,
            "title": "API 配方",
            "description": "用户主动保存",
            "unknown": True,
        },
    )
    assert created.status_code == 422
    created = client.post(
        f"/api/v2/products/{product_id}/workflows/{workflow['id']}/recipes",
        json={
            "source_type": "workflow",
            "expected_edit_version": 0,
            "title": "API 配方",
            "description": "用户主动保存",
        },
    )
    assert created.status_code == 201, created.text
    recipe = created.json()
    assert recipe["current_version"]["version"] == 1
    assert recipe["versions"][0]["title"] == "API 配方"

    listed = client.get("/api/v2/workflow-recipes")
    assert listed.status_code == 200
    assert [item["id"] for item in listed.json()] == [recipe["id"]]

    target_response = client.post(
        "/api/v2/products",
        data={"name": "配方应用目标商品"},
        files=[("images", ("target.png", _make_demo_image_bytes(), "image/png"))],
    )
    target_id = target_response.json()["product"]["id"]
    applied = client.post(
        f"/api/v2/products/{target_id}/workflow-recipes/{recipe['id']}/apply",
        json={"expected_recipe_version": 1, "idempotency_key": "recipe-api-apply"},
    )
    assert applied.status_code == 201, applied.text
    application = applied.json()
    assert application["created"] is True
    assert application["recipe_id"] == recipe["id"]
    assert application["recipe_version"] == 1
    assert application["draft"]["current_revision_id"] is None
    assert application["draft"]["current_revision"] is None
    assert application["draft"]["current_version"] == 0
    assert application["draft"]["revisions"] == []
    assert application["draft"]["recipe_seed"]["recipe_id"] == recipe["id"]
    assert application["conversation"]["workflow_draft_id"] == application["draft"]["id"]
    replay = client.post(
        f"/api/v2/products/{target_id}/workflow-recipes/{recipe['id']}/apply",
        json={"expected_recipe_version": 1, "idempotency_key": "recipe-api-apply"},
    )
    assert replay.status_code == 201
    assert replay.json()["created"] is False
    assert replay.json()["draft"]["id"] == application["draft"]["id"]

    session = get_session_factory()()
    try:
        assert session.scalar(select(WorkflowRecipe).where(WorkflowRecipe.id == recipe["id"])) is not None
        assert session.scalar(
            select(WorkflowRecipeVersion).where(WorkflowRecipeVersion.recipe_id == recipe["id"])
        ) is not None
    finally:
        session.close()
