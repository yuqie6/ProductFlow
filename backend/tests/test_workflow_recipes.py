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
from productflow_backend.application.product_workflow.graph_commands import apply_graph_change_set, load_applied_graph
from productflow_backend.application.product_workflow.graph_contracts import CreateGroupOp, WorkflowChangeSet
from productflow_backend.application.product_workflow.graph_direct_create import create_product_with_direct_graph
from productflow_backend.application.product_workflow.graph_draft_persist import persist_confirmed_draft_graph
from productflow_backend.application.product_workflow.graph_template import DirectCreateImageType
from productflow_backend.application.products import create_canonical_product
from productflow_backend.application.workflow_drafts.service import (
    append_workflow_draft_revision,
    confirm_workflow_draft_revision,
)
from productflow_backend.application.workflow_recipes.contracts import RecipePayload, recipe_payload_hash
from productflow_backend.application.workflow_recipes.extract import extract_recipe_payload
from productflow_backend.application.workflow_recipes.service import (
    append_workflow_recipe_version,
    apply_workflow_recipe,
    archive_workflow_recipe,
    create_workflow_recipe,
    list_workflow_recipes,
)
from productflow_backend.domain.enums import AgentTurnStatus, GraphActorType, GraphNodeType, WorkflowRecipeKind
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    Base,
    WorkflowDraft,
    WorkflowGraph,
    WorkflowRecipe,
    WorkflowRecipeVersion,
)


def _recipe_payload() -> RecipePayload:
    return RecipePayload.model_validate(
        {
            "schema_version": 3,
            "nodes": [
                {
                    "key": "product",
                    "node_type": "product_source",
                    "title": "商品资料",
                    "position_x": 0,
                    "position_y": 0,
                    "config": {"source_product_id": None, "fact_set_version_id": None},
                },
                {
                    "key": "prompt",
                    "node_type": "prompt_generation",
                    "title": "主图提示词",
                    "position_x": 200,
                    "position_y": 0,
                    "config": {"image_type_key": "hero"},
                },
                {
                    "key": "image",
                    "node_type": "image_generation",
                    "title": "主图 1",
                    "position_x": 400,
                    "position_y": 0,
                    "config": {
                        "image_type_key": "hero",
                        "generation_spec": {
                            "aspect_ratio": "1:1",
                            "resolution_tier": "high",
                            "quality_intent": "high",
                            "reference_fidelity": "high",
                            "background_intent": "auto",
                            "text_policy": "none",
                        },
                    },
                },
            ],
            "edges": [
                {
                    "key": "e1",
                    "source_node_key": "product",
                    "target_node_key": "prompt",
                    "data_type": "product_facts",
                    "role": "facts",
                    "order": 0,
                }
            ],
            "groups": [],
        }
    )


def _seed_recipe(
    db_session,
    *,
    kind: WorkflowRecipeKind = WorkflowRecipeKind.WORKFLOW_RECIPE,
    title: str = "结构配方",
) -> WorkflowRecipe:
    payload = _recipe_payload()
    recipe = WorkflowRecipe(kind=kind)
    db_session.add(recipe)
    db_session.flush()
    version = WorkflowRecipeVersion(
        recipe_id=recipe.id,
        version=1,
        schema_version=3,
        title=title,
        payload_json=payload.model_dump(mode="json"),
        payload_hash=recipe_payload_hash(payload),
    )
    db_session.add(version)
    db_session.flush()
    recipe.current_version_id = version.id
    db_session.commit()
    db_session.expire_all()
    seeded = db_session.get(WorkflowRecipe, recipe.id)
    assert seeded is not None
    return seeded


def _create_product(db_session, *, name: str):
    return create_canonical_product(
        db_session,
        name=name,
        category="工业收纳",
        price="299.00",
        source_note="目标商品事实必须重新核对。",
        image_uploads=[(_make_demo_image_bytes(), "product.png", "image/png")],
    )


def _direct_graph(db_session, *, name: str):
    return create_product_with_direct_graph(
        db_session,
        name=name,
        category="工业收纳",
        price="199.00",
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )


def test_extract_full_graph_strips_identity_and_v2_fields(db_session) -> None:
    created = _direct_graph(db_session, name="提取配方商品")
    payload = extract_recipe_payload(load_applied_graph(db_session, created.graph), source_type="workflow")
    dumped = payload.model_dump(mode="json")
    assert dumped["schema_version"] == 3
    assert "folders" not in dumped
    assert "image_types" not in dumped
    assert "prompt_shapes" not in dumped
    assert "visual_requirements" not in dumped
    assert "boundary_requirements" not in dumped
    node_types = {node.node_type for node in payload.nodes}
    assert GraphNodeType.PRODUCT_SOURCE in node_types
    assert GraphNodeType.IMAGE_ASSET in node_types
    assert "product_context" not in {node.node_type.value for node in payload.nodes}
    assert "reference_image" not in {node.node_type.value for node in payload.nodes}
    for node in payload.nodes:
        assert "image_plan_key" not in node.config
        assert "bound_asset_id" not in node.config
        if node.node_type == GraphNodeType.PRODUCT_SOURCE:
            assert node.config.get("source_product_id") is None
            assert node.config.get("fact_set_version_id") is None
        if node.node_type == GraphNodeType.VISUAL_SYSTEM:
            assert node.config.get("visual_system_version_id") in (None,)
    assert all(edge.data_type for edge in payload.edges)
    assert all(not hasattr(edge, "source_handle") for edge in payload.edges)


def test_extract_selection_keeps_internal_edges_only(db_session) -> None:
    created = _direct_graph(db_session, name="选区配方商品")
    applied = load_applied_graph(db_session, created.graph)
    prompt = next(node for node in applied.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    image = next(node for node in applied.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    payload = extract_recipe_payload(
        applied,
        source_type="selection",
        node_ids=[prompt.id, image.id],
    )
    assert {node.key for node in payload.nodes} == {prompt.id, image.id}
    assert payload.edges
    assert all(
        {edge.source_node_key, edge.target_node_key} <= {prompt.id, image.id} for edge in payload.edges
    )


def test_extract_group_includes_members_and_internal_edges(db_session) -> None:
    created = _direct_graph(db_session, name="分组配方商品")
    applied = load_applied_graph(db_session, created.graph)
    prompt = next(node for node in applied.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    image = next(node for node in applied.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    grouped = apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=created.graph.revision,
            actor_type=GraphActorType.USER,
            summary="分组提示词和生图",
            operations=[
                CreateGroupOp(client_ref="hero-group", title="主图组", member_refs=(prompt.id, image.id)),
            ],
        ),
    )
    payload = extract_recipe_payload(grouped.applied, source_type="group", group_id=grouped.applied.groups[0].id)
    assert {node.key for node in payload.nodes} == {prompt.id, image.id}
    assert len(payload.groups) == 1
    assert payload.groups[0].title == "主图组"
    assert set(payload.groups[0].member_keys) == {prompt.id, image.id}


def test_create_and_append_recipe_from_live_graph(db_session) -> None:
    created = _direct_graph(db_session, name="保存配方商品")
    recipe = create_workflow_recipe(
        db_session,
        product_id=created.product.id,
        workflow_id=created.graph.id,
        source_type="workflow",
        group_id=None,
        node_ids=[],
        expected_graph_revision=created.graph.revision,
        title="结构配方",
        description="从当前图画保存",
    )
    assert recipe.kind == WorkflowRecipeKind.WORKFLOW_RECIPE
    assert recipe.current_version is not None
    assert recipe.current_version.schema_version == 3
    payload = recipe.current_version.payload_json
    assert payload["schema_version"] == 3
    assert "image_types" not in payload
    assert "product_context" not in {node["node_type"] for node in payload["nodes"]}
    source = next(node for node in payload["nodes"] if node["node_type"] == "product_source")
    assert source["config"]["source_product_id"] is None

    appended = append_workflow_recipe_version(
        db_session,
        recipe_id=recipe.id,
        expected_recipe_version=1,
        product_id=created.product.id,
        workflow_id=created.graph.id,
        source_type="workflow",
        group_id=None,
        node_ids=[],
        expected_graph_revision=created.graph.revision,
        title="结构配方 v2",
        description=None,
    )
    assert appended.current_version is not None
    assert appended.current_version.version == 2
    assert appended.current_version.title == "结构配方 v2"


def test_stale_graph_revision_conflicts_on_save(db_session) -> None:
    created = _direct_graph(db_session, name="过期配方商品")
    with pytest.raises(ConflictError, match="工作流已变化"):
        create_workflow_recipe(
            db_session,
            product_id=created.product.id,
            workflow_id=created.graph.id,
            source_type="workflow",
            group_id=None,
            node_ids=[],
            expected_graph_revision=0,
            title="过期保存",
            description=None,
        )


def test_seeded_recipe_archive_hides_active_recipe(db_session) -> None:
    recipe = _seed_recipe(db_session)

    assert [item.id for item in list_workflow_recipes(db_session)] == [recipe.id]
    archived = archive_workflow_recipe(
        db_session,
        recipe_id=recipe.id,
        expected_recipe_version=1,
    )
    assert archived.changed is True
    assert archived.recipe.archived_at is not None
    assert list_workflow_recipes(db_session) == []
    assert [item.id for item in list_workflow_recipes(db_session, include_archived=True)] == [recipe.id]

    replay = archive_workflow_recipe(
        db_session,
        recipe_id=recipe.id,
        expected_recipe_version=1,
    )
    assert replay.changed is False


def test_recipe_apply_creates_version_zero_then_first_artifact_can_persist_v3_graph(db_session) -> None:
    recipe = _seed_recipe(db_session)
    target = _create_product(db_session, name="目标商品")

    assert "product_workflows" not in Base.metadata.tables
    assert db_session.scalar(select(WorkflowGraph).where(WorkflowGraph.product_id == target.id)) is None

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
    assert applied.conversation.workflow_draft_id == applied.draft.id
    assert db_session.scalar(select(WorkflowGraph).where(WorkflowGraph.product_id == target.id)) is None

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

    with pytest.raises(ConflictError, match="相同 idempotency key"):
        apply_workflow_recipe(
            db_session,
            product_id=target.id,
            recipe_id=recipe.id,
            expected_recipe_version=2,
            idempotency_key="target-apply-1",
        )

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
    assert "base_workflow" not in seed_context
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

    confirmed = confirm_workflow_draft_revision(
        db_session,
        product_id=target.id,
        draft_id=applied.draft.id,
        expected_draft_version=1,
    )
    assert confirmed.status.value == "confirmed"

    persisted = persist_confirmed_draft_graph(
        db_session,
        product_id=target.id,
        draft_id=applied.draft.id,
        expected_draft_version=1,
    )
    assert persisted.created is True
    assert persisted.graph.product_id == target.id
    assert persisted.graph.schema_version == 3
    assert persisted.graph.revision == 1
    assert persisted.graph.source_draft_revision_id == confirmed.current_revision_id
    assert db_session.scalar(select(WorkflowGraph).where(WorkflowGraph.product_id == target.id)) is not None


def test_fragment_apply_conflicts_on_v3_product(db_session) -> None:
    fragment = _seed_recipe(db_session, kind=WorkflowRecipeKind.RECIPE_FRAGMENT, title="v3 目标片段")
    target = _create_product(db_session, name="片段目标商品")

    with pytest.raises(ConflictError, match="片段配方尚未支持合并进 schema-v3 工作流"):
        apply_workflow_recipe(
            db_session,
            product_id=target.id,
            recipe_id=fragment.id,
            expected_recipe_version=1,
            idempotency_key="fragment-v3-conflict",
        )

    v3 = create_product_with_direct_graph(
        db_session,
        name="已有 v3 图的片段目标",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "v3.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    with pytest.raises(ConflictError, match="片段配方尚未支持合并进 schema-v3 工作流"):
        apply_workflow_recipe(
            db_session,
            product_id=v3.product.id,
            recipe_id=fragment.id,
            expected_recipe_version=1,
            idempotency_key="fragment-v3-existing-graph",
        )


def test_version_zero_agent_artifact_sync_creates_first_revision_idempotently(db_session) -> None:
    recipe = _seed_recipe(db_session, title="Agent 首次制品配方")
    target = _create_product(db_session, name="Agent 配方目标")
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
    assert refreshed is not None
    db_session.refresh(refreshed)
    assert refreshed.current_revision is not None
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


def test_recipe_api_saves_v3_fragment_from_live_graph(configured_env) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    listed = client.get("/api/v2/workflow-recipes")
    assert listed.status_code == 200
    assert listed.json() == []

    created = client.post(
        "/api/v3/products",
        data={
            "name": "配方 API 商品",
            "image_types": json.dumps([{"key": "hero", "quantity": 1}]),
        },
        files=[("images", ("product.png", _make_demo_image_bytes(), "image/png"))],
    )
    assert created.status_code == 201, created.text
    product_id = created.json()["product"]["id"]
    graph = created.json()["graph"]
    save_response = client.post(
        f"/api/v3/products/{product_id}/workflows/{graph['id']}/recipes",
        json={
            "source_type": "workflow",
            "expected_graph_revision": graph["revision"],
            "title": "API 配方",
            "description": "用户主动保存",
        },
    )
    assert save_response.status_code == 201, save_response.text
    body = save_response.json()
    assert body["kind"] == "workflow_recipe"
    payload = body["current_version"]["payload"]
    assert payload["schema_version"] == 3
    assert "image_types" not in payload
    assert "folders" not in payload
    assert {node["node_type"] for node in payload["nodes"]} >= {
        "product_source",
        "prompt_generation",
        "image_generation",
    }
    assert all("source_handle" not in edge for edge in payload["edges"])
    listed_after = client.get("/api/v2/workflow-recipes")
    assert listed_after.status_code == 200
    assert len(listed_after.json()) == 1
    assert listed_after.json()[0]["current_version"]["payload"]["schema_version"] == 3
