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
from productflow_backend.application.agent.sessions import new_agent_session
from productflow_backend.application.workflow_drafts.service import PRODUCT_WORKFLOW_DRAFT_RETIRED
from productflow_backend.application.product_workflow.graph_commands import apply_graph_change_set, load_applied_graph
from productflow_backend.application.product_workflow.graph_contracts import (
    CreateGroupOp,
    MoveNodesOp,
    WorkflowChangeSet,
)
from productflow_backend.application.product_workflow.graph_direct_create import create_product_with_direct_graph
from productflow_backend.application.product_workflow.graph_template import DirectCreateImageType
from productflow_backend.application.products import create_canonical_product
from productflow_backend.application.workflow_recipes.contracts import RecipePayload, recipe_payload_hash
from productflow_backend.application.workflow_recipes.extract import extract_recipe_payload
from productflow_backend.application.workflow_recipes.official import official_recipe_seed
from productflow_backend.application.workflow_recipes.service import (
    append_workflow_recipe_version,
    apply_workflow_recipe,
    archive_workflow_recipe,
    create_workflow_recipe,
    get_workflow_recipe_or_raise,
    list_workflow_recipes,
    preview_workflow_recipe,
)
from productflow_backend.domain.enums import (
    AgentConversationStatus,
    AgentTurnStatus,
    GraphActorType,
    GraphNodeType,
    WorkflowDraftStatus,
    WorkflowRecipeCreationSource,
    WorkflowRecipeKind,
    WorkflowRecipeOrigin,
)
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    Base,
    WorkflowDraft,
    WorkflowGraph,
    WorkflowRecipe,
    WorkflowRecipeVersion,
)
from productflow_backend.infrastructure.db.session import get_session_factory


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
    recipe = WorkflowRecipe(kind=kind, origin=WorkflowRecipeOrigin.USER, official_key=None)
    db_session.add(recipe)
    db_session.flush()
    version = WorkflowRecipeVersion(
        recipe_id=recipe.id,
        version=1,
        schema_version=3,
        catalog_version=5,
        creation_source=WorkflowRecipeCreationSource.USER_EXTRACT,
        title=title,
        payload_json=payload.model_dump(mode="json"),
        payload_hash=recipe_payload_hash(payload),
        governance_json=None,
    )
    db_session.add(version)
    db_session.flush()
    recipe.current_version_id = version.id
    db_session.commit()
    db_session.expire_all()
    seeded = db_session.get(WorkflowRecipe, recipe.id)
    assert seeded is not None
    return seeded


def _seed_official_recipe(db_session, *, official_key: str = "hero") -> WorkflowRecipe:
    seed = official_recipe_seed(official_key)
    recipe = WorkflowRecipe(
        id=seed.recipe_id,
        kind=WorkflowRecipeKind.RECIPE_FRAGMENT,
        origin=WorkflowRecipeOrigin.OFFICIAL,
        official_key=seed.official_key,
    )
    db_session.add(recipe)
    db_session.flush()
    version = WorkflowRecipeVersion(
        id=seed.version_id,
        recipe_id=recipe.id,
        version=1,
        schema_version=3,
        catalog_version=5,
        creation_source=WorkflowRecipeCreationSource.OFFICIAL_SEED,
        title=seed.title,
        description=seed.description,
        payload_json=seed.payload.model_dump(mode="json"),
        payload_hash=seed.payload_hash,
        governance_json=seed.governance.model_dump(mode="json"),
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
    shot_group = next(group for group in grouped.applied.groups if group.title == "主图组")
    payload = extract_recipe_payload(grouped.applied, source_type="group", group_id=shot_group.id)
    assert {node.key for node in payload.nodes} == {prompt.id, image.id}
    named = [group for group in payload.groups if group.title == "主图组"]
    assert len(named) == 1
    assert set(named[0].member_keys) == {prompt.id, image.id}


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
    assert recipe.origin is WorkflowRecipeOrigin.USER
    assert recipe.official_key is None
    assert recipe.current_version is not None
    assert recipe.current_version.schema_version == 3
    assert recipe.current_version.catalog_version == 5
    assert recipe.current_version.creation_source is WorkflowRecipeCreationSource.USER_EXTRACT
    assert recipe.current_version.governance_json is None
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


def test_official_recipes_are_hidden_from_the_online_library(db_session) -> None:
    official = _seed_official_recipe(db_session)
    user = _seed_recipe(db_session)

    assert [item.id for item in list_workflow_recipes(db_session)] == [user.id]
    assert [item.id for item in list_workflow_recipes(db_session, include_archived=True)] == [user.id]
    with pytest.raises(NotFoundError, match="工作流配方不存在"):
        get_workflow_recipe_or_raise(db_session, recipe_id=official.id)
    target = _create_product(db_session, name="官方配方不可应用")
    with pytest.raises(NotFoundError, match="工作流配方不存在"):
        preview_workflow_recipe(
            db_session,
            product_id=target.id,
            recipe_id=official.id,
            expected_recipe_version=1,
        )
    with pytest.raises(NotFoundError, match="工作流配方不存在"):
        apply_workflow_recipe(
            db_session,
            product_id=target.id,
            recipe_id=official.id,
            expected_recipe_version=1,
            expected_graph_revision=0,
            preview_digest="0" * 64,
            idempotency_key="official-hidden",
        )
    with pytest.raises(NotFoundError, match="工作流配方不存在"):
        archive_workflow_recipe(
            db_session,
            recipe_id=official.id,
            expected_recipe_version=1,
        )


def test_recipe_apply_rejects_wrong_digest_stale_revision_and_recipe_drift(db_session) -> None:
    recipe = _seed_recipe(db_session, kind=WorkflowRecipeKind.RECIPE_FRAGMENT)
    target = _direct_graph(db_session, name="配方预览绑定目标")
    preview = preview_workflow_recipe(
        db_session,
        product_id=target.product.id,
        recipe_id=recipe.id,
        expected_recipe_version=1,
    )
    with pytest.raises(ConflictError, match="预览已变化"):
        apply_workflow_recipe(
            db_session,
            product_id=target.product.id,
            recipe_id=recipe.id,
            expected_recipe_version=1,
            expected_graph_revision=preview.base_graph_revision,
            preview_digest="0" * 64,
            idempotency_key="wrong-preview-digest",
        )
    current = db_session.get(WorkflowGraph, target.graph.id)
    assert current is not None
    assert current.revision == preview.base_graph_revision

    before = load_applied_graph(db_session, target.graph)
    node = before.nodes[0]
    apply_graph_change_set(
        db_session,
        product_id=target.product.id,
        graph_id=target.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=before.revision,
            summary="预览后移动节点",
            actor_type=GraphActorType.USER,
            operations=[MoveNodesOp(nodes=[(node.id, node.position_x + 10, node.position_y)])],
        ),
    )
    with pytest.raises(ConflictError, match="工作流已变化"):
        apply_workflow_recipe(
            db_session,
            product_id=target.product.id,
            recipe_id=recipe.id,
            expected_recipe_version=1,
            expected_graph_revision=preview.base_graph_revision,
            preview_digest=preview.preview_digest,
            idempotency_key="stale-preview-revision",
        )

    user_recipe = _seed_recipe(
        db_session,
        kind=WorkflowRecipeKind.RECIPE_FRAGMENT,
        title="版本漂移片段",
    )
    user_target = _direct_graph(db_session, name="版本漂移目标")
    user_preview = preview_workflow_recipe(
        db_session,
        product_id=user_target.product.id,
        recipe_id=user_recipe.id,
        expected_recipe_version=1,
    )
    append_workflow_recipe_version(
        db_session,
        recipe_id=user_recipe.id,
        expected_recipe_version=1,
        product_id=user_target.product.id,
        workflow_id=user_target.graph.id,
        source_type="workflow",
        group_id=None,
        node_ids=[],
        expected_graph_revision=user_target.graph.revision,
        title="版本漂移片段 v2",
        description=None,
    )
    with pytest.raises(ConflictError, match="版本已变化"):
        apply_workflow_recipe(
            db_session,
            product_id=user_target.product.id,
            recipe_id=user_recipe.id,
            expected_recipe_version=1,
            expected_graph_revision=user_preview.base_graph_revision,
            preview_digest=user_preview.preview_digest,
            idempotency_key="recipe-version-drift",
        )


def test_recipe_apply_writes_live_graph_via_graph_command(db_session) -> None:
    recipe = _seed_recipe(db_session)
    target = _create_product(db_session, name="目标商品")

    assert "product_workflows" not in Base.metadata.tables
    assert db_session.scalar(select(WorkflowGraph).where(WorkflowGraph.product_id == target.id)) is None

    preview = preview_workflow_recipe(
        db_session,
        product_id=target.id,
        recipe_id=recipe.id,
        expected_recipe_version=1,
    )
    assert preview.mode == "create"
    assert {node.node_type for node in preview.nodes} >= {
        GraphNodeType.PRODUCT_SOURCE,
        GraphNodeType.PROMPT_GENERATION,
        GraphNodeType.IMAGE_GENERATION,
    }
    assert preview.edges

    applied = apply_workflow_recipe(
        db_session,
        product_id=target.id,
        recipe_id=recipe.id,
        expected_recipe_version=1,
        expected_graph_revision=preview.base_graph_revision,
        preview_digest=preview.preview_digest,
        idempotency_key="target-apply-1",
    )
    assert applied.created is True
    assert applied.mode == "create"
    assert applied.graph.product_id == target.id
    assert applied.graph.schema_version == 3
    assert applied.added_node_ids
    dumped = recipe.current_version.payload_json
    assert "product_identity" not in json.dumps(dumped)
    live = load_applied_graph(db_session, applied.graph)
    assert {node.node_type for node in live.nodes} >= {
        GraphNodeType.PRODUCT_SOURCE,
        GraphNodeType.PROMPT_GENERATION,
        GraphNodeType.IMAGE_GENERATION,
    }
    source = next(node for node in live.nodes if node.node_type == GraphNodeType.PRODUCT_SOURCE)
    assert source.config.get("source_product_id") == target.id
    assert all(node.bound_asset_id is None for node in live.nodes if node.node_type == GraphNodeType.IMAGE_ASSET)
    assert db_session.scalar(select(WorkflowDraft).where(WorkflowDraft.product_id == target.id)) is None

    replay = apply_workflow_recipe(
        db_session,
        product_id=target.id,
        recipe_id=recipe.id,
        expected_recipe_version=1,
        expected_graph_revision=preview.base_graph_revision,
        preview_digest=preview.preview_digest,
        idempotency_key="target-apply-1",
    )
    assert replay.created is False
    assert replay.graph.id == applied.graph.id
    assert replay.added_node_ids == applied.added_node_ids

    with pytest.raises(ConflictError, match="相同 idempotency key"):
        apply_workflow_recipe(
            db_session,
            product_id=target.id,
            recipe_id=recipe.id,
            expected_recipe_version=2,
            expected_graph_revision=preview.base_graph_revision,
            preview_digest=preview.preview_digest,
            idempotency_key="target-apply-1",
        )


def test_fragment_apply_merges_into_existing_v3_graph(db_session) -> None:
    fragment = _seed_recipe(db_session, kind=WorkflowRecipeKind.RECIPE_FRAGMENT, title="v3 目标片段")
    target = _create_product(db_session, name="片段目标商品")

    with pytest.raises(ConflictError, match="片段配方需要已有 schema-v3 工作流"):
        apply_workflow_recipe(
            db_session,
            product_id=target.id,
            recipe_id=fragment.id,
            expected_recipe_version=1,
            expected_graph_revision=0,
            preview_digest="a" * 64,
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
    before = load_applied_graph(db_session, v3.graph)
    preview = preview_workflow_recipe(
        db_session,
        product_id=v3.product.id,
        recipe_id=fragment.id,
        expected_recipe_version=1,
    )
    assert preview.mode == "merge"
    assert preview.nodes
    applied = apply_workflow_recipe(
        db_session,
        product_id=v3.product.id,
        recipe_id=fragment.id,
        expected_recipe_version=1,
        expected_graph_revision=preview.base_graph_revision,
        preview_digest=preview.preview_digest,
        idempotency_key="fragment-v3-existing-graph",
    )
    assert applied.created is True
    assert applied.mode == "merge"
    after = load_applied_graph(db_session, v3.graph)
    assert len(after.nodes) == len(before.nodes) + len(preview.nodes)
    assert {node.id for node in before.nodes} < {node.id for node in after.nodes}


def test_full_recipe_conflicts_on_existing_v3_graph(db_session) -> None:
    recipe = _seed_recipe(db_session, title="完整配方不能并进现图")
    v3 = create_product_with_direct_graph(
        db_session,
        name="已有图的完整配方目标",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "v3.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    with pytest.raises(ConflictError, match="完整配方不能合并进已有工作流"):
        preview_workflow_recipe(
            db_session,
            product_id=v3.product.id,
            recipe_id=recipe.id,
            expected_recipe_version=1,
        )
    with pytest.raises(ConflictError, match="完整配方不能合并进已有工作流"):
        apply_workflow_recipe(
            db_session,
            product_id=v3.product.id,
            recipe_id=recipe.id,
            expected_recipe_version=1,
            expected_graph_revision=v3.graph.revision,
            preview_digest="a" * 64,
            idempotency_key="full-recipe-existing-graph",
        )


def test_version_zero_agent_artifact_sync_creates_first_revision_idempotently(db_session) -> None:
    target = _create_product(db_session, name="Agent 配方目标")
    draft = WorkflowDraft(
        product_id=target.id,
        status=WorkflowDraftStatus.COLLECTING,
        intake_schema_version=1,
        intake_json={
            "schema_version": 1,
            "image_types": [{"key": "hero", "quantity": 1, "order": 0}],
            "reference_asset_ids": [target.image_assets[0].id],
        },
    )
    db_session.add(draft)
    db_session.flush()
    agent_session = new_agent_session(title="Agent 配方目标")
    db_session.add(agent_session)
    db_session.flush()
    conversation = AgentConversation(
        session_id=agent_session.id,
        product_id=target.id,
        workflow_draft_id=draft.id,
        harness_run_id=draft.id,
        status=AgentConversationStatus.COLLECTING,
    )
    db_session.add(conversation)
    db_session.commit()
    projection = reserve_agent_turn(
        db_session,
        product_id=target.id,
        conversation_id=conversation.id,
        input_text="请按配方重建目标商品工作流",
        input_asset_ids=[target.image_assets[0].id],
        idempotency_key="agent-artifact-turn",
    ).projection
    projection = bind_harness_turn(
        db_session,
        product_id=target.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-recipe-turn",
        status=AgentTurnStatus.RUNNING,
    )
    projection = project_agent_turn_state(
        db_session,
        product_id=target.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-recipe-turn",
        status=AgentTurnStatus.AWAITING_CONFIRMATION,
        output_text="已重建目标商品草案。",
        error_text=None,
        question_json=None,
        finished_at=datetime.now(UTC),
    )
    payload = make_workflow_draft_payload(reference_asset_id=target.image_assets[0].id)
    with pytest.raises(ConflictError, match=PRODUCT_WORKFLOW_DRAFT_RETIRED):
        attach_agent_workflow_draft_artifact(
            db_session,
            product_id=target.id,
            conversation_id=conversation.id,
            projection_id=projection.id,
            harness_turn_id="harness-recipe-turn",
            artifact_name="propose_workflow_draft",
            artifact_step_id="recipe-artifact-step",
            artifact_value=payload,
        )
    refreshed = db_session.get(WorkflowDraft, draft.id)
    assert refreshed is not None
    db_session.refresh(refreshed)
    assert refreshed.current_revision is None
    assert refreshed.revisions == []


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

    other = client.post(
        "/api/v3/products",
        data={
            "name": "配方应用目标",
            "image_types": json.dumps([{"key": "hero", "quantity": 1}]),
        },
        files=[("images", ("other.png", _make_demo_image_bytes(), "image/png"))],
    )
    assert other.status_code == 201, other.text
    other_id = other.json()["product"]["id"]
    blocked = client.post(
        f"/api/v3/products/{other_id}/workflow-recipes/{body['id']}/preview",
        json={"expected_recipe_version": 1},
    )
    assert blocked.status_code == 409, blocked.text
    assert "完整配方不能合并" in blocked.text

    prompt_id = next(node["id"] for node in graph["nodes"] if node["node_type"] == "prompt_generation")
    image_id = next(node["id"] for node in graph["nodes"] if node["node_type"] == "image_generation")
    fragment = client.post(
        f"/api/v3/products/{product_id}/workflows/{graph['id']}/recipes",
        json={
            "source_type": "selection",
            "node_ids": [prompt_id, image_id],
            "expected_graph_revision": graph["revision"],
            "title": "API 片段",
        },
    )
    assert fragment.status_code == 201, fragment.text
    assert fragment.json()["kind"] == "recipe_fragment"
    preview = client.post(
        f"/api/v3/products/{other_id}/workflow-recipes/{fragment.json()['id']}/preview",
        json={"expected_recipe_version": 1},
    )
    assert preview.status_code == 200, preview.text
    assert preview.json()["mode"] == "merge"
    assert preview.json()["nodes"]
    applied = client.post(
        f"/api/v3/products/{other_id}/workflow-recipes/{fragment.json()['id']}/apply",
        json={
            "expected_recipe_version": 1,
            "expected_graph_revision": preview.json()["base_graph_revision"],
            "preview_digest": preview.json()["preview_digest"],
            "idempotency_key": "api-apply-1",
        },
    )
    assert applied.status_code == 201, applied.text
    applied_body = applied.json()
    assert applied_body["mode"] == "merge"
    assert "draft" not in applied_body
    assert applied_body["graph"]["id"] == other.json()["graph"]["id"]
    assert applied_body["added_node_ids"]


def test_recipe_api_hides_official_seeds_from_online_library(configured_env) -> None:
    from productflow_backend.presentation.api import create_app

    factory = get_session_factory()
    session = factory()
    try:
        official_id = _seed_official_recipe(session).id
        user_id = _seed_recipe(session).id
    finally:
        session.close()

    client = TestClient(create_app())
    _login(client)
    listed = client.get("/api/v3/workflow-recipes")
    assert listed.status_code == 200, listed.text
    assert [item["id"] for item in listed.json()] == [user_id]
    assert all(item["origin"] == "user" for item in listed.json())

    v2_listed = client.get("/api/v2/workflow-recipes")
    assert v2_listed.status_code == 200, v2_listed.text
    assert [item["id"] for item in v2_listed.json()] == [user_id]

    detail = client.get(f"/api/v3/workflow-recipes/{official_id}")
    assert detail.status_code == 404, detail.text

    archived = client.delete(
        f"/api/v3/workflow-recipes/{official_id}",
        params={"expected_recipe_version": 1},
    )
    assert archived.status_code == 404, archived.text
