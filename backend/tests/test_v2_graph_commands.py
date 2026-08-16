from __future__ import annotations

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from sqlalchemy import func, select
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.gallery_assets import list_gallery_assets
from productflow_backend.application.product_workflow.v2_graph_commands import (
    create_v2_reference_node,
    create_v2_workflow_edge,
    delete_v2_workflow_edge,
    delete_v2_workflow_node,
    duplicate_v2_workflow_node,
)
from productflow_backend.application.use_cases import create_canonical_product
from productflow_backend.application.workflow_drafts.contracts import ImagePromptPayloadV1
from productflow_backend.application.workflow_drafts.materialization import materialize_workflow_draft
from productflow_backend.application.workflow_drafts.service import (
    confirm_workflow_draft_revision,
    create_workflow_draft,
)
from productflow_backend.domain.enums import (
    ProductImageOriginType,
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.infrastructure.db.models import (
    ImagePromptArtifact,
    ImagePromptArtifactVersion,
    ProductImageAsset,
    WorkflowNodeRun,
    WorkflowRun,
)


def _create_materialized_workflow(db_session):
    product = create_canonical_product(
        db_session,
        name="类型化结构命令商品",
        category="工业收纳",
        price="299.00",
        source_note="验证 schema-v2 图结构命令",
        image_uploads=[(_make_demo_image_bytes(), "product.png", "image/png")],
    )
    draft = create_workflow_draft(
        db_session,
        product_id=product.id,
        payload=make_workflow_draft_payload(reference_asset_id=product.image_assets[0].id),
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
        idempotency_key="materialize-v2-graph-commands",
    )
    return product, result.workflow


def _nodes(workflow, node_type: WorkflowNodeType):
    return [node for node in workflow.nodes if node.node_type == node_type]


def _prompt_payload(db_session, prompt_node) -> ImagePromptPayloadV1:
    version = db_session.get(ImagePromptArtifactVersion, prompt_node.current_prompt_artifact_version_id)
    assert version is not None
    return ImagePromptPayloadV1.model_validate(version.payload_json)


def test_create_and_duplicate_reference_nodes_preserve_typed_state(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    folder = workflow.folders[0]

    created = create_v2_reference_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        expected_edit_version=0,
        title="材质参考",
        role="material_detail",
        label="表面与金属质感",
        position_x=520,
        position_y=560,
        folder_id=folder.id,
    )

    assert created.changed is True
    assert created.workflow.edit_version == 1
    new_reference = next(node for node in created.workflow.nodes if node.title == "材质参考")
    assert new_reference.node_type == WorkflowNodeType.REFERENCE_IMAGE
    assert new_reference.bound_image_asset_id is None
    assert new_reference.folder_id == folder.id
    assert new_reference.config_json["role"] == "material_detail"
    assert new_reference.config_json["reference_key"].startswith("reference-")

    original_reference = _nodes(created.workflow, WorkflowNodeType.REFERENCE_IMAGE)[0]
    duplicated = duplicate_v2_workflow_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=original_reference.id,
        expected_edit_version=1,
    )

    references = _nodes(duplicated.workflow, WorkflowNodeType.REFERENCE_IMAGE)
    duplicate = next(node for node in references if node.id not in {original_reference.id, new_reference.id})
    assert duplicated.workflow.edit_version == 2
    assert duplicate.bound_image_asset_id == original_reference.bound_image_asset_id
    assert duplicate.config_json["role"] == original_reference.config_json["role"]
    assert duplicate.config_json["reference_key"] != original_reference.config_json["reference_key"]
    assert duplicate.position_x == original_reference.position_x + 48
    assert duplicate.position_y == original_reference.position_y + 48


def test_duplicate_image_appends_prompt_version_and_copies_incoming_dependencies(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    reference = _nodes(workflow, WorkflowNodeType.REFERENCE_IMAGE)[0]
    prompt = _nodes(workflow, WorkflowNodeType.PROMPT_GENERATION)[0]
    source_image = _nodes(workflow, WorkflowNodeType.IMAGE_GENERATION)[0]
    initial_prompt_version_id = prompt.current_prompt_artifact_version_id
    initial_node_ids = {item.id for item in workflow.nodes}

    with_optional_dependency = create_v2_workflow_edge(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        source_node_id=reference.id,
        target_node_id=source_image.id,
        expected_edit_version=0,
    )
    duplicated = duplicate_v2_workflow_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=source_image.id,
        expected_edit_version=with_optional_dependency.workflow.edit_version,
    )

    image_nodes = _nodes(duplicated.workflow, WorkflowNodeType.IMAGE_GENERATION)
    duplicate = next(node for node in image_nodes if node.id not in initial_node_ids)
    refreshed_prompt = next(node for node in duplicated.workflow.nodes if node.id == prompt.id)
    payload = _prompt_payload(db_session, refreshed_prompt)
    assert duplicated.workflow.edit_version == 2
    assert len(image_nodes) == 3
    assert len(payload.images) == 3
    assert refreshed_prompt.current_prompt_artifact_version_id != initial_prompt_version_id
    assert duplicate.config_json["image_plan_key"] == payload.images[-1].image_plan_key
    assert duplicate.config_json["prompt_plan_key"] == prompt.config_json["prompt_plan_key"]
    assert duplicate.bound_image_asset_id is None
    incoming_sources = {
        edge.source_node_id
        for edge in duplicated.workflow.edges
        if edge.target_node_id == duplicate.id
    }
    assert incoming_sources == {reference.id, prompt.id}
    assert db_session.scalar(
        select(func.count())
        .select_from(ImagePromptArtifactVersion)
        .where(ImagePromptArtifactVersion.artifact_id == refreshed_prompt.current_prompt_artifact_version.artifact_id)
    ) == 2


def test_duplicate_prompt_creates_independent_artifact_group_and_lineage(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    context = _nodes(workflow, WorkflowNodeType.PRODUCT_CONTEXT)[0]
    reference = _nodes(workflow, WorkflowNodeType.REFERENCE_IMAGE)[0]
    prompt = _nodes(workflow, WorkflowNodeType.PROMPT_GENERATION)[0]
    original_image_ids = {node.id for node in _nodes(workflow, WorkflowNodeType.IMAGE_GENERATION)}

    duplicated = duplicate_v2_workflow_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=prompt.id,
        expected_edit_version=0,
    )

    prompt_nodes = _nodes(duplicated.workflow, WorkflowNodeType.PROMPT_GENERATION)
    duplicate_prompt = next(node for node in prompt_nodes if node.id != prompt.id)
    duplicate_images = [
        node
        for node in _nodes(duplicated.workflow, WorkflowNodeType.IMAGE_GENERATION)
        if node.id not in original_image_ids
    ]
    duplicate_payload = _prompt_payload(db_session, duplicate_prompt)
    assert duplicated.workflow.edit_version == 1
    assert len(duplicate_images) == 2
    assert len(duplicate_payload.images) == 2
    assert duplicate_prompt.config_json["prompt_plan_key"] != prompt.config_json["prompt_plan_key"]
    assert duplicate_prompt.config_json["image_type_key"] != prompt.config_json["image_type_key"]
    assert {
        node.config_json["image_plan_key"]
        for node in duplicate_images
    } == {plan.image_plan_key for plan in duplicate_payload.images}
    assert all(
        node.config_json["prompt_plan_key"] == duplicate_prompt.config_json["prompt_plan_key"]
        for node in duplicate_images
    )
    incoming_prompt_sources = {
        edge.source_node_id
        for edge in duplicated.workflow.edges
        if edge.target_node_id == duplicate_prompt.id
    }
    assert incoming_prompt_sources == {context.id, reference.id}
    assert all(
        any(
            edge.source_node_id == duplicate_prompt.id and edge.target_node_id == image.id
            for edge in duplicated.workflow.edges
        )
        for image in duplicate_images
    )
    assert db_session.scalar(
        select(func.count()).select_from(ImagePromptArtifact).where(ImagePromptArtifact.workflow_id == workflow.id)
    ) == 2


def test_optional_edges_reject_duplicates_cycles_and_protect_lineage(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    prompt = _nodes(workflow, WorkflowNodeType.PROMPT_GENERATION)[0]
    first_image, second_image = _nodes(workflow, WorkflowNodeType.IMAGE_GENERATION)
    first_image.status = WorkflowNodeStatus.SUCCEEDED
    second_image.status = WorkflowNodeStatus.SUCCEEDED
    db_session.commit()

    created = create_v2_workflow_edge(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        source_node_id=first_image.id,
        target_node_id=second_image.id,
        expected_edit_version=0,
    )
    optional_edge = next(
        edge
        for edge in created.workflow.edges
        if edge.source_node_id == first_image.id and edge.target_node_id == second_image.id
    )
    assert (optional_edge.source_handle, optional_edge.target_handle) == ("image", "reference")
    stale_target = next(node for node in created.workflow.nodes if node.id == second_image.id)
    assert stale_target.status == WorkflowNodeStatus.IDLE

    with pytest.raises(BusinessValidationError, match="重复"):
        create_v2_workflow_edge(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            source_node_id=first_image.id,
            target_node_id=second_image.id,
            expected_edit_version=1,
        )
    with pytest.raises(BusinessValidationError, match="循环"):
        create_v2_workflow_edge(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            source_node_id=second_image.id,
            target_node_id=first_image.id,
            expected_edit_version=1,
        )

    lineage_edge = next(
        edge
        for edge in created.workflow.edges
        if edge.source_node_id == prompt.id and edge.target_node_id == first_image.id
    )
    with pytest.raises(ConflictError, match="lineage"):
        delete_v2_workflow_edge(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            edge_id=lineage_edge.id,
            expected_edit_version=1,
        )

    deleted = delete_v2_workflow_edge(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        edge_id=optional_edge.id,
        expected_edit_version=1,
    )
    assert deleted.workflow.edit_version == 2
    assert all(edge.id != optional_edge.id for edge in deleted.workflow.edges)


def test_delete_image_updates_prompt_version_and_last_image_deletes_prompt_group(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    prompt = _nodes(workflow, WorkflowNodeType.PROMPT_GENERATION)[0]
    duplicated = duplicate_v2_workflow_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=prompt.id,
        expected_edit_version=0,
    )
    duplicate_prompt = next(
        node for node in _nodes(duplicated.workflow, WorkflowNodeType.PROMPT_GENERATION) if node.id != prompt.id
    )
    duplicate_images = [
        node
        for node in _nodes(duplicated.workflow, WorkflowNodeType.IMAGE_GENERATION)
        if node.config_json["prompt_plan_key"] == duplicate_prompt.config_json["prompt_plan_key"]
    ]
    duplicate_artifact_id = duplicate_prompt.current_prompt_artifact_version.artifact_id

    first_delete = delete_v2_workflow_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=duplicate_images[0].id,
        expected_edit_version=1,
    )
    remaining_duplicate_prompt = next(node for node in first_delete.workflow.nodes if node.id == duplicate_prompt.id)
    assert len(_prompt_payload(db_session, remaining_duplicate_prompt).images) == 1
    assert all(node.id != duplicate_images[0].id for node in first_delete.workflow.nodes)

    second_delete = delete_v2_workflow_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=duplicate_images[1].id,
        expected_edit_version=2,
    )
    assert second_delete.workflow.edit_version == 3
    assert all(node.id != duplicate_prompt.id for node in second_delete.workflow.nodes)
    assert all(node.id not in {item.id for item in duplicate_images} for node in second_delete.workflow.nodes)
    assert db_session.get(ImagePromptArtifact, duplicate_artifact_id) is not None
    assert len(_nodes(second_delete.workflow, WorkflowNodeType.PROMPT_GENERATION)) == 1


def test_graph_commands_reject_product_context_stale_versions_limits_and_active_runs(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    context = _nodes(workflow, WorkflowNodeType.PRODUCT_CONTEXT)[0]
    reference = _nodes(workflow, WorkflowNodeType.REFERENCE_IMAGE)[0]
    image = _nodes(workflow, WorkflowNodeType.IMAGE_GENERATION)[0]

    with pytest.raises(ConflictError, match="商品事实"):
        delete_v2_workflow_node(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            node_id=context.id,
            expected_edit_version=0,
        )
    with pytest.raises(ConflictError, match="edit version"):
        duplicate_v2_workflow_node(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            node_id=reference.id,
            expected_edit_version=7,
        )

    run = WorkflowRun(workflow_id=workflow.id, status=WorkflowRunStatus.RUNNING)
    db_session.add(run)
    db_session.flush()
    db_session.add(
        WorkflowNodeRun(
            workflow_run_id=run.id,
            node_id=image.id,
            status=WorkflowNodeStatus.RUNNING,
            active_attempt_id="graph-command-active-attempt",
        )
    )
    db_session.commit()
    with pytest.raises(ConflictError, match="正在运行"):
        create_v2_workflow_edge(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            source_node_id=context.id,
            target_node_id=image.id,
            expected_edit_version=0,
        )
    db_session.refresh(workflow)
    assert workflow.edit_version == 0


def test_graph_commands_reject_topology_changes_while_workflow_run_is_active(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    reference = _nodes(workflow, WorkflowNodeType.REFERENCE_IMAGE)[0]
    image = _nodes(workflow, WorkflowNodeType.IMAGE_GENERATION)[0]
    run = WorkflowRun(workflow_id=workflow.id, status=WorkflowRunStatus.RUNNING)
    db_session.add(run)
    db_session.flush()
    db_session.add(
        WorkflowNodeRun(
            workflow_run_id=run.id,
            node_id=image.id,
            status=WorkflowNodeStatus.SUCCEEDED,
        )
    )
    db_session.commit()

    with pytest.raises(ConflictError, match="工作流正在运行"):
        create_v2_reference_node(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            expected_edit_version=0,
            title="运行中新增参考",
            role="product_detail",
            label="局部细节",
            position_x=520,
            position_y=560,
        )
    with pytest.raises(ConflictError, match="工作流正在运行"):
        delete_v2_workflow_node(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            node_id=image.id,
            expected_edit_version=0,
        )
    with pytest.raises(ConflictError, match="工作流正在运行"):
        duplicate_v2_workflow_node(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            node_id=reference.id,
            expected_edit_version=0,
        )

    db_session.refresh(workflow)
    assert workflow.edit_version == 0


def test_delete_image_node_keeps_bound_asset_in_product_gallery(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    image = _nodes(workflow, WorkflowNodeType.IMAGE_GENERATION)[0]
    source_asset = product.image_assets[0]
    generated_asset = ProductImageAsset(
        product_id=product.id,
        media_object_id=source_asset.media_object_id,
        origin_type=ProductImageOriginType.WORKFLOW_GENERATION,
        display_name="已生成候选图",
        original_filename="candidate.png",
        image_type_key=str(image.config_json["image_type_key"]),
    )
    db_session.add(generated_asset)
    db_session.flush()
    generated_asset_id = generated_asset.id
    image.bound_image_asset_id = generated_asset_id
    image.status = WorkflowNodeStatus.SUCCEEDED
    db_session.commit()

    deleted = delete_v2_workflow_node(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=image.id,
        expected_edit_version=0,
    )

    assert all(node.id != image.id for node in deleted.workflow.nodes)
    assert db_session.get(ProductImageAsset, generated_asset_id) is not None
    gallery = list_gallery_assets(db_session, product_id=product.id)
    assert generated_asset_id in {record.asset.id for record in gallery.items}


def test_graph_command_api_is_strict_and_returns_complete_latest_workflow(configured_env) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    product_response = client.post(
        "/api/v2/products",
        data={"name": "类型化结构命令 API 商品"},
        files=[("images", ("product.png", _make_demo_image_bytes(), "image/png"))],
    )
    assert product_response.status_code == 201, product_response.text
    product_payload = product_response.json()
    product_id = product_payload["product"]["id"]
    reference_asset_id = product_payload["created_assets"][0]["id"]
    draft_response = client.post(
        f"/api/v2/products/{product_id}/workflow-drafts",
        json={
            "payload": make_workflow_draft_payload(reference_asset_id=reference_asset_id),
            "ready_for_confirmation": True,
        },
    )
    assert draft_response.status_code == 201, draft_response.text
    draft = draft_response.json()
    assert client.post(
        f"/api/v2/products/{product_id}/workflow-drafts/{draft['id']}/confirm",
        json={"expected_draft_version": 1},
    ).status_code == 200
    materialized_response = client.post(
        f"/api/v2/products/{product_id}/workflow-drafts/{draft['id']}/materialize",
        json={
            "expected_draft_version": 1,
            "expected_workflow_revision": 0,
            "idempotency_key": "graph-command-api-materialize",
        },
    )
    assert materialized_response.status_code == 200, materialized_response.text
    workflow = materialized_response.json()["workflow"]
    workflow_url = f"/api/v2/products/{product_id}/workflows/{workflow['id']}"

    invalid = client.post(
        f"{workflow_url}/reference-nodes",
        json={
            "expected_edit_version": 0,
            "title": "局部细节参考",
            "role": "detail",
            "label": "表面纹理",
            "position_x": 480,
            "position_y": 520,
            "unknown": True,
        },
    )
    assert invalid.status_code == 422

    created_response = client.post(
        f"{workflow_url}/reference-nodes",
        json={
            "expected_edit_version": 0,
            "title": "局部细节参考",
            "role": "detail",
            "label": "表面纹理",
            "position_x": 480,
            "position_y": 520,
        },
    )
    assert created_response.status_code == 201, created_response.text
    created = created_response.json()
    reference = next(node for node in created["workflow"]["nodes"] if node["title"] == "局部细节参考")
    assert created["edit_version"] == created["workflow"]["edit_version"] == 1
    assert {"folders", "nodes", "edges"} <= created["workflow"].keys()

    duplicated_response = client.post(
        f"{workflow_url}/nodes/{reference['id']}/duplicate",
        json={"expected_edit_version": 1},
    )
    assert duplicated_response.status_code == 201, duplicated_response.text
    duplicated = duplicated_response.json()
    duplicate = next(
        node
        for node in duplicated["workflow"]["nodes"]
        if node["node_type"] == "reference_image" and node["id"] != reference["id"]
        and node["title"].startswith(reference["title"])
    )
    image = next(node for node in duplicated["workflow"]["nodes"] if node["node_type"] == "image_generation")

    edge_response = client.post(
        f"{workflow_url}/edges",
        json={
            "expected_edit_version": 2,
            "source_node_id": duplicate["id"],
            "target_node_id": image["id"],
        },
    )
    assert edge_response.status_code == 201, edge_response.text
    edged = edge_response.json()
    edge = next(
        item
        for item in edged["workflow"]["edges"]
        if item["source_node_id"] == duplicate["id"] and item["target_node_id"] == image["id"]
    )
    assert (edge["source_handle"], edge["target_handle"]) == ("asset", "reference")

    deleted_edge_response = client.delete(
        f"{workflow_url}/edges/{edge['id']}",
        params={"expected_edit_version": 3},
    )
    assert deleted_edge_response.status_code == 200, deleted_edge_response.text
    assert all(item["id"] != edge["id"] for item in deleted_edge_response.json()["workflow"]["edges"])

    deleted_node_response = client.delete(
        f"{workflow_url}/nodes/{duplicate['id']}",
        params={"expected_edit_version": 4},
    )
    assert deleted_node_response.status_code == 200, deleted_node_response.text
    deleted = deleted_node_response.json()
    assert deleted["workflow"]["edit_version"] == 5
    assert all(node["id"] != duplicate["id"] for node in deleted["workflow"]["nodes"])
