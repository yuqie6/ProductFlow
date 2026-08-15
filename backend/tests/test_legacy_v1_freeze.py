from __future__ import annotations

import json
from unittest.mock import Mock

import pytest
import sqlalchemy as sa
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.canvas_templates import get_builtin_canvas_template
from productflow_backend.application.image_sessions import attach_image_session_asset_to_product
from productflow_backend.application.legacy_retirement.freeze import (
    LEGACY_V1_WRITE_FREEZE_CONFLICT_DETAIL,
    LEGACY_V1_WRITE_FREEZE_INVALID_DETAIL,
    LEGACY_V1_WRITE_FREEZE_SETTING_KEY,
    ensure_legacy_v1_write_allowed,
    get_legacy_v1_write_freeze_state,
    set_legacy_v1_write_freeze_state,
)
from productflow_backend.application.product_workflow.execution import (
    cancel_product_workflow_run,
    retry_product_workflow_run,
    submit_product_workflow_run,
)
from productflow_backend.application.product_workflow.mutations import (
    apply_node_group_template_to_workflow,
    bind_workflow_node_image,
    create_workflow_edge,
    create_workflow_node,
    delete_workflow_edge,
    delete_workflow_node,
    duplicate_workflow_node_group,
    get_or_create_product_workflow,
    update_workflow_copy_set,
    update_workflow_node,
    upload_workflow_node_image,
)
from productflow_backend.application.product_workflow.templates import materialize_product_workflow_from_template
from productflow_backend.application.product_workflow.user_templates import (
    archive_user_canvas_template,
    create_user_canvas_template_from_workflow_nodes,
    rename_user_canvas_template,
)
from productflow_backend.application.product_workflow.v2_runs import submit_v2_workflow_run
from productflow_backend.application.use_cases import (
    add_reference_images,
    confirm_copy_set,
    create_canonical_product,
    create_product,
    delete_product,
    delete_reference_image,
    update_copy_set,
)
from productflow_backend.application.workflow_drafts.materialization import materialize_workflow_draft
from productflow_backend.application.workflow_drafts.service import (
    confirm_workflow_draft_revision,
    create_workflow_draft,
)
from productflow_backend.commands.manage_legacy_v1_freeze import main as freeze_command_main
from productflow_backend.domain.enums import WorkflowNodeType
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import (
    AppSetting,
    Product,
    ProductWorkflow,
    WorkflowEdge,
    WorkflowNode,
)
from productflow_backend.presentation.api import create_app


def _legacy_product_and_workflow(db_session):
    product = create_product(
        db_session,
        name="冻结测试商品",
        category="工具",
        price="99",
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="product.png",
        content_type="image/png",
    )
    workflow = get_or_create_product_workflow(db_session, product.id)
    editable_node = next(node for node in workflow.nodes if node.node_type != WorkflowNodeType.PRODUCT_CONTEXT)
    template = create_user_canvas_template_from_workflow_nodes(
        db_session,
        product_id=product.id,
        title="冻结前用户模板",
        description=None,
        node_ids=[editable_node.id],
    )
    return product, workflow, editable_node, template


def test_legacy_v1_freeze_state_defaults_unfrozen_and_fails_closed_when_malformed(db_session) -> None:
    initial = get_legacy_v1_write_freeze_state(db_session)
    assert initial.configured is False
    assert initial.frozen is False
    assert initial.valid is True

    frozen = set_legacy_v1_write_freeze_state(db_session, frozen=True)
    assert frozen.configured is True
    assert frozen.frozen is True
    assert frozen.valid is True
    with pytest.raises(ConflictError, match=LEGACY_V1_WRITE_FREEZE_CONFLICT_DETAIL):
        ensure_legacy_v1_write_allowed(db_session)

    row = db_session.get(AppSetting, LEGACY_V1_WRITE_FREEZE_SETTING_KEY)
    assert row is not None
    row.value = "unexpected"
    db_session.commit()
    malformed = get_legacy_v1_write_freeze_state(db_session)
    assert malformed.frozen is True
    assert malformed.valid is False
    with pytest.raises(ConflictError, match=LEGACY_V1_WRITE_FREEZE_INVALID_DETAIL):
        ensure_legacy_v1_write_allowed(db_session)

    unfrozen = set_legacy_v1_write_freeze_state(db_session, frozen=False)
    assert unfrozen.frozen is False
    assert unfrozen.valid is True
    ensure_legacy_v1_write_allowed(db_session)


def test_frozen_v1_write_boundaries_emit_no_dml_queue_or_storage_side_effects(
    configured_env,
    db_session,
) -> None:
    product, workflow, editable_node, template = _legacy_product_and_workflow(db_session)
    product_context = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT)
    duplicate_context = WorkflowNode(
        workflow_id=workflow.id,
        node_type=WorkflowNodeType.PRODUCT_CONTEXT,
        title="重复商品资料",
        position_x=999,
        position_y=999,
        config_json={},
    )
    db_session.add(duplicate_context)
    db_session.commit()
    edge = workflow.edges[0]
    set_legacy_v1_write_freeze_state(db_session, frozen=True)
    expected_node_count = db_session.scalar(sa.select(sa.func.count()).select_from(WorkflowNode))
    expected_edge_count = db_session.scalar(sa.select(sa.func.count()).select_from(WorkflowEdge))

    statements: list[str] = []

    def capture_dml(
        _connection: sa.Connection,
        _cursor: object,
        statement: str,
        _parameters: object,
        _context: object,
        _executemany: bool,
    ) -> None:
        normalized = statement.strip().lower()
        if normalized.startswith(("insert ", "update ", "delete ")):
            statements.append(normalized)

    engine = db_session.get_bind()
    sa.event.listen(engine, "before_cursor_execute", capture_dml)
    queue = Mock()
    storage = Mock()
    blocked_operations = [
        lambda: create_product(
            db_session,
            name="禁止创建",
            category=None,
            price=None,
            source_note=None,
            image_bytes=_make_demo_image_bytes(),
            filename="blocked.png",
            content_type="image/png",
            storage=storage,
        ),
        lambda: add_reference_images(
            db_session,
            product_id=product.id,
            reference_image_uploads=[(_make_demo_image_bytes(), "blocked.png", "image/png")],
            storage=storage,
        ),
        lambda: delete_reference_image(db_session, asset_id="missing", storage=storage),
        lambda: delete_product(db_session, product_id=product.id, storage=storage),
        lambda: update_copy_set(db_session, copy_set_id="missing", structured_payload={}),
        lambda: confirm_copy_set(db_session, copy_set_id="missing"),
        lambda: attach_image_session_asset_to_product(
            db_session,
            image_session_id="missing",
            asset_id="missing",
            target="reference",
            product_id=product.id,
            storage=storage,
        ),
        lambda: create_workflow_node(
            db_session,
            product_id=product.id,
            node_type=WorkflowNodeType.REFERENCE_IMAGE,
            title="禁止节点",
            position_x=1,
            position_y=1,
            config_json={},
        ),
        lambda: apply_node_group_template_to_workflow(
            db_session,
            product_id=product.id,
            template_key="ecommerce-main-image-v1",
            position_x=1,
            position_y=1,
        ),
        lambda: duplicate_workflow_node_group(
            db_session,
            product_id=product.id,
            node_ids=[editable_node.id],
        ),
        lambda: update_workflow_node(
            db_session,
            node_id=editable_node.id,
            title="禁止修改",
            position_x=None,
            position_y=None,
            config_json=None,
        ),
        lambda: update_workflow_copy_set(db_session, node_id=editable_node.id, structured_payload={}),
        lambda: upload_workflow_node_image(
            db_session,
            node_id=editable_node.id,
            image_bytes=_make_demo_image_bytes(),
            filename="blocked.png",
            content_type="image/png",
            storage=storage,
        ),
        lambda: bind_workflow_node_image(
            db_session,
            node_id=editable_node.id,
            source_asset_id="missing",
            storage=storage,
        ),
        lambda: create_workflow_edge(
            db_session,
            product_id=product.id,
            source_node_id=product_context.id,
            target_node_id=editable_node.id,
        ),
        lambda: delete_workflow_edge(db_session, edge_id=edge.id),
        lambda: delete_workflow_node(db_session, node_id=editable_node.id),
        lambda: create_user_canvas_template_from_workflow_nodes(
            db_session,
            product_id=product.id,
            title="禁止模板",
            description=None,
            node_ids=[editable_node.id],
        ),
        lambda: rename_user_canvas_template(
            db_session,
            template_id=template.id,
            title="禁止重命名",
            description=None,
        ),
        lambda: archive_user_canvas_template(db_session, template_id=template.id),
        lambda: materialize_product_workflow_from_template(
            db_session,
            product_id=product.id,
            template=get_builtin_canvas_template("ecommerce-main-image-v1"),
        ),
        lambda: submit_product_workflow_run(db_session, product_id=product.id, enqueue=queue),
        lambda: retry_product_workflow_run(db_session, product_id=product.id, run_id="missing", enqueue=queue),
        lambda: cancel_product_workflow_run(db_session, product_id=product.id, run_id="missing"),
    ]
    try:
        readable = get_or_create_product_workflow(db_session, product.id)
        db_session.expire(readable, ["nodes"])
        assert len([node for node in readable.nodes if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT]) == 2
        for operation in blocked_operations:
            with pytest.raises(ConflictError, match=LEGACY_V1_WRITE_FREEZE_CONFLICT_DETAIL):
                operation()
    finally:
        sa.event.remove(engine, "before_cursor_execute", capture_dml)

    assert statements == []
    queue.assert_not_called()
    assert storage.mock_calls == []
    assert db_session.scalar(sa.select(sa.func.count()).select_from(Product)) == 1
    assert db_session.scalar(sa.select(sa.func.count()).select_from(ProductWorkflow)) == 1
    assert db_session.scalar(sa.select(sa.func.count()).select_from(WorkflowNode)) == expected_node_count
    assert db_session.scalar(sa.select(sa.func.count()).select_from(WorkflowEdge)) == expected_edge_count


def test_frozen_v1_get_does_not_recreate_a_missing_product_context(db_session) -> None:
    product, workflow, _editable_node, _template = _legacy_product_and_workflow(db_session)
    context = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT)
    db_session.execute(
        sa.delete(WorkflowEdge).where(
            (WorkflowEdge.source_node_id == context.id) | (WorkflowEdge.target_node_id == context.id)
        )
    )
    db_session.delete(context)
    db_session.commit()
    set_legacy_v1_write_freeze_state(db_session, frozen=True)
    db_session.expire_all()
    statements: list[str] = []

    def capture_dml(
        _connection: sa.Connection,
        _cursor: object,
        statement: str,
        _parameters: object,
        _context: object,
        _executemany: bool,
    ) -> None:
        if statement.strip().lower().startswith(("insert ", "update ", "delete ")):
            statements.append(statement)

    engine = db_session.get_bind()
    sa.event.listen(engine, "before_cursor_execute", capture_dml)
    try:
        readable = get_or_create_product_workflow(db_session, product.id)
        assert not any(node.node_type == WorkflowNodeType.PRODUCT_CONTEXT for node in readable.nodes)
    finally:
        sa.event.remove(engine, "before_cursor_execute", capture_dml)

    assert statements == []


def test_frozen_v1_http_is_read_only_while_canonical_v2_creation_still_works(configured_env, db_session) -> None:
    product, workflow, editable_node, _template = _legacy_product_and_workflow(db_session)
    set_legacy_v1_write_freeze_state(db_session, frozen=True)

    with TestClient(create_app()) as client:
        _login(client)
        read_response = client.get(f"/api/products/{product.id}/workflow")
        assert read_response.status_code == 200
        assert read_response.json()["id"] == workflow.id

        mutation_response = client.patch(
            f"/api/workflow-nodes/{editable_node.id}",
            json={"title": "禁止修改"},
        )
        assert mutation_response.status_code == 409
        assert mutation_response.json() == {"detail": LEGACY_V1_WRITE_FREEZE_CONFLICT_DETAIL}

        run_response = client.post(f"/api/products/{product.id}/workflow/run", json={})
        assert run_response.status_code == 409
        assert run_response.json() == {"detail": LEGACY_V1_WRITE_FREEZE_CONFLICT_DETAIL}

        canonical_response = client.post(
            "/api/v2/products",
            data={"name": "冻结后 canonical 商品"},
            files=[("images", ("reference.png", _make_demo_image_bytes(), "image/png"))],
        )
        assert canonical_response.status_code == 201
        assert canonical_response.json()["product"]["name"] == "冻结后 canonical 商品"


def test_frozen_v1_does_not_block_v2_draft_materialization_or_run(configured_env, db_session) -> None:
    set_legacy_v1_write_freeze_state(db_session, frozen=True)
    product = create_canonical_product(
        db_session,
        name="冻结期间 v2 商品",
        category="工具",
        price="199",
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
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
    materialized = materialize_workflow_draft(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
        expected_workflow_revision=0,
        idempotency_key="v2-during-legacy-freeze",
    )
    queue = Mock()

    submission = submit_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=materialized.workflow.id,
        enqueue=queue,
    )

    assert submission.created is True
    assert materialized.workflow.schema_version == 2
    queue.assert_called_once_with(submission.run.id)


def test_legacy_v1_freeze_command_requires_explicit_confirmation(configured_env, capsys) -> None:
    assert freeze_command_main(["status"]) == 0
    initial = json.loads(capsys.readouterr().out)
    assert initial["frozen"] is False

    with pytest.raises(SystemExit, match="FREEZE_V1_WRITES"):
        freeze_command_main(["enable", "--confirm", "wrong"])

    assert freeze_command_main(["enable", "--confirm", "FREEZE_V1_WRITES"]) == 0
    enabled = json.loads(capsys.readouterr().out)
    assert enabled["frozen"] is True

    assert freeze_command_main(["disable", "--confirm", "UNFREEZE_V1_WRITES"]) == 0
    disabled = json.loads(capsys.readouterr().out)
    assert disabled["frozen"] is False
