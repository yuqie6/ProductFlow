from __future__ import annotations

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.product_workflow.folders import (
    WorkflowNodePosition,
    create_workflow_folder,
    dissolve_workflow_folder,
    rename_workflow_folder,
    set_workflow_folder_members,
    translate_workflow_folder,
    update_workflow_node_layout,
)
from productflow_backend.application.product_workflow.mutations import get_or_create_product_workflow
from productflow_backend.application.use_cases import create_canonical_product
from productflow_backend.application.workflow_drafts.materialization import materialize_workflow_draft
from productflow_backend.application.workflow_drafts.service import (
    confirm_workflow_draft_revision,
    create_workflow_draft,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError


def _materialize_v2_workflow(db_session):
    product = create_canonical_product(
        db_session,
        name="画布文件夹商品",
        category="工业收纳",
        price="299.00",
        source_note="用于文件夹 mutation 测试",
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
        idempotency_key="folder-test-materialize",
    )
    return product, result.workflow


def test_folder_member_moves_dissolve_empty_sources_without_deleting_nodes(db_session) -> None:
    product, workflow = _materialize_v2_workflow(db_session)
    original_folder = workflow.folders[0]
    original_member_ids = sorted(node.id for node in workflow.nodes if node.folder_id == original_folder.id)
    context_node = next(node for node in workflow.nodes if node.folder_id is None)
    positions_before = {
        node.id: (node.position_x, node.position_y)
        for node in workflow.nodes
    }

    created = create_workflow_folder(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        title="商品基础",
        node_ids=[context_node.id],
        expected_edit_version=0,
    )
    assert created.changed is True
    assert created.workflow.edit_version == 1
    assert created.dissolved_folder_ids == ()
    target_folder = next(folder for folder in created.workflow.folders if folder.title == "商品基础")
    assert target_folder.sort_order == 1

    moved = set_workflow_folder_members(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        folder_id=target_folder.id,
        node_ids=[context_node.id, *original_member_ids],
        expected_edit_version=1,
    )
    assert moved.changed is True
    assert moved.workflow.edit_version == 2
    assert moved.dissolved_folder_ids == (original_folder.id,)
    assert [folder.id for folder in moved.workflow.folders] == [target_folder.id]
    assert {node.id for node in moved.workflow.nodes} == set(positions_before)
    assert all(node.folder_id == target_folder.id for node in moved.workflow.nodes)
    assert {
        node.id: (node.position_x, node.position_y)
        for node in moved.workflow.nodes
    } == positions_before

    dissolved = set_workflow_folder_members(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        folder_id=target_folder.id,
        node_ids=[],
        expected_edit_version=2,
    )
    assert dissolved.changed is True
    assert dissolved.workflow.edit_version == 3
    assert dissolved.dissolved_folder_ids == (target_folder.id,)
    assert dissolved.workflow.folders == []
    assert all(node.folder_id is None for node in dissolved.workflow.nodes)


def test_folder_rename_translate_and_layout_use_one_edit_version_each(db_session) -> None:
    product, workflow = _materialize_v2_workflow(db_session)
    folder = workflow.folders[0]
    members_before = {
        node.id: (node.position_x, node.position_y)
        for node in workflow.nodes
        if node.folder_id == folder.id
    }

    no_op_rename = rename_workflow_folder(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        folder_id=folder.id,
        title=f"  {folder.title}  ",
        expected_edit_version=0,
    )
    assert no_op_rename.changed is False
    assert no_op_rename.workflow.edit_version == 0

    no_op_translate = translate_workflow_folder(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        folder_id=folder.id,
        delta_x=0,
        delta_y=0,
        expected_edit_version=0,
    )
    assert no_op_translate.changed is False
    assert no_op_translate.workflow.edit_version == 0

    translated = translate_workflow_folder(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        folder_id=folder.id,
        delta_x=90,
        delta_y=-35,
        expected_edit_version=0,
    )
    assert translated.changed is True
    assert translated.workflow.edit_version == 1
    assert {
        node.id: (node.position_x, node.position_y)
        for node in translated.workflow.nodes
        if node.folder_id == folder.id
    } == {
        node_id: (position_x + 90, position_y - 35)
        for node_id, (position_x, position_y) in members_before.items()
    }

    first_member = next(node for node in translated.workflow.nodes if node.folder_id == folder.id)
    first_member_position = (first_member.position_x, first_member.position_y)
    no_op_layout = update_workflow_node_layout(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        positions=[
            WorkflowNodePosition(
                node_id=first_member.id,
                position_x=first_member.position_x,
                position_y=first_member.position_y,
            )
        ],
        expected_edit_version=1,
    )
    assert no_op_layout.changed is False
    assert no_op_layout.workflow.edit_version == 1

    laid_out = update_workflow_node_layout(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        positions=[
            WorkflowNodePosition(
                node_id=first_member.id,
                position_x=first_member_position[0] + 12,
                position_y=first_member_position[1] + 18,
            )
        ],
        expected_edit_version=1,
    )
    assert laid_out.changed is True
    assert laid_out.workflow.edit_version == 2
    updated = next(node for node in laid_out.workflow.nodes if node.id == first_member.id)
    assert (updated.position_x, updated.position_y) == (
        first_member_position[0] + 12,
        first_member_position[1] + 18,
    )

    with pytest.raises(ConflictError, match="edit version"):
        rename_workflow_folder(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            folder_id=folder.id,
            title="过期修改",
            expected_edit_version=1,
        )


def test_folder_mutations_reject_invalid_scope_duplicates_and_v1_workflows(db_session) -> None:
    product, workflow = _materialize_v2_workflow(db_session)
    node = workflow.nodes[0]

    with pytest.raises(BusinessValidationError, match="不能重复"):
        create_workflow_folder(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            title="重复节点",
            node_ids=[node.id, node.id],
            expected_edit_version=0,
        )

    with pytest.raises(NotFoundError, match="不属于当前工作流"):
        create_workflow_folder(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            title="未知节点",
            node_ids=["00000000-0000-0000-0000-000000000000"],
            expected_edit_version=0,
        )

    with pytest.raises(NotFoundError, match="文件夹不存在"):
        dissolve_workflow_folder(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            folder_id="00000000-0000-0000-0000-000000000000",
            expected_edit_version=0,
        )

    legacy_product = create_canonical_product(
        db_session,
        name="旧工作流商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "legacy.png", "image/png")],
    )
    legacy_workflow = get_or_create_product_workflow(db_session, legacy_product.id)
    with pytest.raises(ConflictError, match="schema-v2"):
        create_workflow_folder(
            db_session,
            product_id=legacy_product.id,
            workflow_id=legacy_workflow.id,
            title="不支持",
            node_ids=[legacy_workflow.nodes[0].id],
            expected_edit_version=0,
        )


def test_folder_api_returns_complete_latest_workflow_and_rejects_unknown_fields(configured_env) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    product_response = client.post(
        "/api/v2/products",
        data={"name": "文件夹 API 商品"},
        files=[("images", ("product.png", _make_demo_image_bytes(), "image/png"))],
    )
    product = product_response.json()
    product_id = product["product"]["id"]
    reference_asset_id = product["created_assets"][0]["id"]
    draft = client.post(
        f"/api/v2/products/{product_id}/workflow-drafts",
        json={
            "payload": make_workflow_draft_payload(reference_asset_id=reference_asset_id),
            "ready_for_confirmation": True,
        },
    ).json()
    assert client.post(
        f"/api/v2/products/{product_id}/workflow-drafts/{draft['id']}/confirm",
        json={"expected_draft_version": 1},
    ).status_code == 200
    materialized = client.post(
        f"/api/v2/products/{product_id}/workflow-drafts/{draft['id']}/materialize",
        json={
            "expected_draft_version": 1,
            "expected_workflow_revision": 0,
            "idempotency_key": "folder-api-materialize",
        },
    ).json()
    workflow = materialized["workflow"]
    ungrouped_node = next(node for node in workflow["nodes"] if node["folder_id"] is None)
    url = f"/api/v2/products/{product_id}/workflows/{workflow['id']}/folders"

    invalid = client.post(
        url,
        json={
            "title": "基础信息",
            "node_ids": [ungrouped_node["id"]],
            "expected_edit_version": 0,
            "unknown": True,
        },
    )
    assert invalid.status_code == 422

    created = client.post(
        url,
        json={
            "title": "基础信息",
            "node_ids": [ungrouped_node["id"]],
            "expected_edit_version": 0,
        },
    )
    assert created.status_code == 201, created.text
    payload = created.json()
    assert payload["changed"] is True
    assert payload["edit_version"] == 1
    assert payload["workflow"]["edit_version"] == 1
    assert len(payload["workflow"]["folders"]) == 2
    assert {
        "position_x",
        "position_y",
        "width",
        "height",
        "config_json",
    }.isdisjoint(payload["workflow"]["folders"][0])

    stale = client.patch(
        f"{url}/{payload['workflow']['folders'][0]['id']}",
        json={"title": "过期", "expected_edit_version": 0},
    )
    assert stale.status_code == 409
    assert "edit version" in stale.json()["detail"]
