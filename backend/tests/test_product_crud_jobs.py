from __future__ import annotations

from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from helpers import (
    _enable_deletion,
    _execute_workflow_queue_inline,
    _login,
    _make_demo_image_bytes,
    _wait_for_workflow_run,
)
from sqlalchemy import event

from productflow_backend.application.canvas_templates import get_builtin_canvas_template
from productflow_backend.application.product_workflow.templates import TEMPLATE_METADATA_CONFIG_KEY
from productflow_backend.application.use_cases import (
    add_reference_images,
    create_product,
    derive_product_state,
    list_products,
)
from productflow_backend.domain.enums import (
    CopyStatus,
    PosterKind,
    ProductWorkflowState,
    SourceAssetKind,
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRunStatus,
)
from productflow_backend.infrastructure.db.models import (
    CopySet,
    PosterVariant,
    ProductWorkflow,
    SourceAsset,
    WorkflowEdge,
    WorkflowNode,
    WorkflowRun,
)


def _variant_paths(path: Path) -> list[Path]:
    return list((path.parent / ".variants").glob(f"{path.stem}.*"))


@pytest.fixture(autouse=True)
def _execute_workflow_queue_inline_fixture(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep API workflow tests deterministic while production delivery goes through Dramatiq."""

    _execute_workflow_queue_inline(monkeypatch)


def test_product_create_persists_source_note_for_ai_context(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    response = client.post(
        "/api/products",
        data={
            "name": "露营保温杯",
            "category": "户外",
            "price": "79.00",
            "source_note": "316 不锈钢，主打长效保温和车载杯架适配。",
        },
        files={"image": ("cup.png", _make_demo_image_bytes(), "image/png")},
    )

    assert response.status_code == 201
    payload = response.json()
    assert payload["source_note"] == "316 不锈钢，主打长效保温和车载杯架适配。"

    minimal = client.post(
        "/api/products",
        data={"name": "极简商品壳"},
        files={"image": ("minimal.png", _make_demo_image_bytes(), "image/png")},
    )
    assert minimal.status_code == 201
    minimal_payload = minimal.json()
    assert minimal_payload["category"] is None
    assert minimal_payload["price"] is None
    assert minimal_payload["source_note"] is None


def test_default_product_create_preserves_lazy_workflow_behavior(configured_env: Path, db_session) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "默认画布商品"},
        files={"image": ("default.png", _make_demo_image_bytes(), "image/png")},
    )

    assert created.status_code == 201
    product_id = created.json()["id"]
    db_session.expire_all()
    assert db_session.query(ProductWorkflow).filter_by(product_id=product_id).count() == 0

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    assert len(workflow["nodes"]) == 4
    assert len(workflow["edges"]) == 4
    assert {node["title"] for node in workflow["nodes"]} == {"商品", "文案", "生图", "参考图"}

    default_key = client.post(
        "/api/products",
        data={"name": "显式默认画布商品", "canvas_template_key": "default"},
        files={"image": ("explicit-default.png", _make_demo_image_bytes(), "image/png")},
    )
    assert default_key.status_code == 201
    db_session.expire_all()
    assert db_session.query(ProductWorkflow).filter_by(product_id=default_key.json()["id"]).count() == 0


def test_product_create_materializes_full_canvas_template(configured_env: Path, db_session) -> None:
    from productflow_backend.presentation.api import create_app

    template = get_builtin_canvas_template("ecommerce-main-image-v1")
    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "模板画布商品", "canvas_template_key": template.key},
        files={"image": ("template.png", _make_demo_image_bytes(), "image/png")},
    )

    assert created.status_code == 201
    product_id = created.json()["id"]
    db_session.expire_all()
    workflow = db_session.query(ProductWorkflow).filter_by(product_id=product_id, active=True).one()
    assert workflow.title == template.title

    nodes = db_session.query(WorkflowNode).filter_by(workflow_id=workflow.id).all()
    edges = db_session.query(WorkflowEdge).filter_by(workflow_id=workflow.id).all()
    assert len(nodes) == len(template.nodes)
    assert len(edges) == len(template.edges)

    persisted_node_ids_by_template_key: dict[str, str] = {}
    unmatched_nodes = list(nodes)
    for template_node in template.nodes:
        matched_node = next(
            (
                node
                for node in unmatched_nodes
                if node.node_type == template_node.node_type
                and node.title == template_node.title
                and node.position_x == template_node.position_x
                and node.position_y == template_node.position_y
                and node.config_json == {
                    **template_node.config_json,
                    TEMPLATE_METADATA_CONFIG_KEY: {
                        "source": "builtin",
                        "template_key": template.key,
                        "node_key": template_node.key,
                    },
                }
            ),
            None,
        )
        assert matched_node is not None
        unmatched_nodes.remove(matched_node)
        persisted_node_ids_by_template_key[template_node.key] = matched_node.id

    assert set(persisted_node_ids_by_template_key) == {node.key for node in template.nodes}
    persisted_edges = {
        (edge.source_node_id, edge.target_node_id, edge.source_handle, edge.target_handle) for edge in edges
    }
    assert persisted_edges == {
        (
            persisted_node_ids_by_template_key[edge.source_node_key],
            persisted_node_ids_by_template_key[edge.target_node_key],
            edge.source_handle,
            edge.target_handle,
        )
        for edge in template.edges
    }

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    assert workflow_response.json()["id"] == workflow.id


def test_product_create_rejects_invalid_canvas_template_key(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    response = client.post(
        "/api/products",
        data={"name": "坏模板商品", "canvas_template_key": "missing-template"},
        files={"image": ("missing.png", _make_demo_image_bytes(), "image/png")},
    )

    assert response.status_code == 400
    assert "画布模板不存在" in response.json()["detail"]


def test_product_create_accepts_broad_builtin_canvas_template_key(configured_env: Path, db_session) -> None:
    from productflow_backend.presentation.api import create_app

    template = get_builtin_canvas_template("ecommerce-sku-variant-image-v1")
    app = create_app()
    client = TestClient(app)
    _login(client)

    response = client.post(
        "/api/products",
        data={"name": "规格模板商品", "canvas_template_key": template.key},
        files={"image": ("sku-template.png", _make_demo_image_bytes(), "image/png")},
    )

    assert response.status_code == 201
    product_id = response.json()["id"]
    db_session.expire_all()
    workflow = db_session.query(ProductWorkflow).filter_by(product_id=product_id, active=True).one()
    assert workflow.title == template.title
    assert db_session.query(WorkflowNode).filter_by(workflow_id=workflow.id).count() == len(template.nodes)
    assert db_session.query(WorkflowEdge).filter_by(workflow_id=workflow.id).count() == len(template.edges)


def test_legacy_jobrun_routes_are_removed(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "无传统任务商品"},
        files={"image": ("legacy.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    assert client.post(f"/api/products/{product_id}/copy-jobs").status_code == 404
    assert client.post(f"/api/products/{product_id}/poster-jobs").status_code == 404
    assert client.post("/api/posters/missing/regenerate").status_code == 404
    assert client.get("/api/jobs/missing").status_code == 404

    history = client.get(f"/api/products/{product_id}/history")
    assert history.status_code == 200
    assert set(history.json()) == {"copy_sets", "poster_variants"}


def test_product_can_be_deleted_from_api(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "待删除商品"},
        files={"image": ("delete.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]
    product_root = configured_env / "products" / product_id
    assert product_root.exists()

    run = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert run.status_code == 200
    completed = _wait_for_workflow_run(client, product_id, status="succeeded")
    assert completed["runs"][0]["node_runs"]
    product_with_artifacts = client.get(f"/api/products/{product_id}")
    assert product_with_artifacts.status_code == 200
    assert product_with_artifacts.json()["copy_sets"]
    assert product_with_artifacts.json()["poster_variants"]

    _enable_deletion(client)
    deleted = client.delete(f"/api/products/{product_id}")
    assert deleted.status_code == 204
    assert deleted.content == b""

    listed = client.get("/api/products")
    assert listed.status_code == 200
    assert product_id not in {item["id"] for item in listed.json()["items"]}
    missing = client.get(f"/api/products/{product_id}")
    assert missing.status_code == 404
    assert not product_root.exists()


def test_reference_images_can_be_attached_to_product(db_session, configured_env: Path) -> None:
    product = create_product(
        db_session,
        name="陶瓷马克杯",
        category="家居",
        price="39.00",
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="mug.png",
        content_type="image/png",
    )

    updated = add_reference_images(
        db_session,
        product_id=product.id,
        reference_image_uploads=[
            (_make_demo_image_bytes(), "sample-1.png", "image/png"),
            (_make_demo_image_bytes(), "sample-2.png", "image/png"),
        ],
    )

    reference_assets = [asset for asset in updated.source_assets if asset.kind == SourceAssetKind.REFERENCE_IMAGE]
    assert len(reference_assets) == 2
    assert all((Path(configured_env) / asset.storage_path).exists() for asset in reference_assets)


def test_product_status_filter_uses_database_pagination_before_eager_loading(db_session, configured_env: Path) -> None:
    draft = create_product(
        db_session,
        name="草稿商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="draft.png",
        content_type="image/png",
    )
    copy_ready = create_product(
        db_session,
        name="文案商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="copy.png",
        content_type="image/png",
    )
    poster_ready = create_product(
        db_session,
        name="海报商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="poster.png",
        content_type="image/png",
    )
    failed_draft = create_product(
        db_session,
        name="失败草稿商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="failed-draft.png",
        content_type="image/png",
    )
    failed_copy = create_product(
        db_session,
        name="失败文案商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="failed-copy.png",
        content_type="image/png",
    )
    failed_node = create_product(
        db_session,
        name="失败节点商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="failed-node.png",
        content_type="image/png",
    )
    inactive_failed = create_product(
        db_session,
        name="非活跃失败商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="inactive-failed.png",
        content_type="image/png",
    )

    copy_set = CopySet(
        product_id=copy_ready.id,
        status=CopyStatus.CONFIRMED,
        structured_payload={
            "version": 2,
            "summary": "海报标题",
            "content": {"kind": "blocks", "blocks": [{"id": "headline", "text": "标题"}]},
        },
        model_structured_payload={
            "version": 2,
            "summary": "海报标题",
            "content": {"kind": "blocks", "blocks": [{"id": "headline", "text": "标题"}]},
        },
        provider_name="test",
        model_name="test",
        prompt_version="test",
    )
    db_session.add(copy_set)
    db_session.flush()
    copy_ready.current_confirmed_copy_set_id = copy_set.id

    poster_copy_set = CopySet(
        product_id=poster_ready.id,
        status=CopyStatus.CONFIRMED,
        structured_payload={
            "version": 2,
            "summary": "海报标题",
            "content": {"kind": "blocks", "blocks": [{"id": "headline", "text": "标题"}]},
        },
        model_structured_payload={
            "version": 2,
            "summary": "海报标题",
            "content": {"kind": "blocks", "blocks": [{"id": "headline", "text": "标题"}]},
        },
        provider_name="test",
        model_name="test",
        prompt_version="test",
    )
    db_session.add(poster_copy_set)
    db_session.flush()
    poster_ready.current_confirmed_copy_set_id = poster_copy_set.id
    db_session.add(
        PosterVariant(
            product_id=poster_ready.id,
            copy_set_id=poster_copy_set.id,
            kind=PosterKind.MAIN_IMAGE,
            template_name="test",
            storage_path="products/poster/poster.png",
            width=800,
            height=800,
        )
    )
    failed_copy_set = CopySet(
        product_id=failed_copy.id,
        status=CopyStatus.CONFIRMED,
        structured_payload={
            "version": 2,
            "summary": "失败商品标题",
            "content": {"kind": "blocks", "blocks": [{"id": "headline", "text": "失败标题"}]},
        },
        model_structured_payload={
            "version": 2,
            "summary": "失败商品标题",
            "content": {"kind": "blocks", "blocks": [{"id": "headline", "text": "失败标题"}]},
        },
        provider_name="test",
        model_name="test",
        prompt_version="test",
    )
    db_session.add(failed_copy_set)
    db_session.flush()
    failed_copy.current_confirmed_copy_set_id = failed_copy_set.id

    failed_draft_workflow = ProductWorkflow(product_id=failed_draft.id, title="失败草稿工作流")
    failed_copy_workflow = ProductWorkflow(product_id=failed_copy.id, title="失败文案工作流")
    failed_node_workflow = ProductWorkflow(product_id=failed_node.id, title="失败节点工作流")
    poster_failed_workflow = ProductWorkflow(product_id=poster_ready.id, title="已有海报失败工作流")
    inactive_failed_workflow = ProductWorkflow(
        product_id=inactive_failed.id,
        title="非活跃失败工作流",
        active=False,
    )
    db_session.add_all(
        [
            failed_draft_workflow,
            failed_copy_workflow,
            failed_node_workflow,
            poster_failed_workflow,
            inactive_failed_workflow,
        ]
    )
    db_session.flush()
    db_session.add_all(
        [
            WorkflowRun(
                workflow_id=failed_draft_workflow.id,
                status=WorkflowRunStatus.FAILED,
                failure_reason="draft failed",
            ),
            WorkflowRun(
                workflow_id=failed_copy_workflow.id,
                status=WorkflowRunStatus.FAILED,
                failure_reason="copy failed",
            ),
            WorkflowRun(
                workflow_id=poster_failed_workflow.id,
                status=WorkflowRunStatus.FAILED,
                failure_reason="poster failed after completion",
            ),
            WorkflowRun(
                workflow_id=inactive_failed_workflow.id,
                status=WorkflowRunStatus.FAILED,
                failure_reason="inactive workflow failure should not affect product state",
            ),
            WorkflowNode(
                workflow_id=failed_node_workflow.id,
                node_type=WorkflowNodeType.IMAGE_GENERATION,
                title="失败节点",
                status=WorkflowNodeStatus.FAILED,
                failure_reason="node failed",
            ),
        ]
    )
    db_session.commit()
    db_session.expire_all()

    expected_state_by_product_id = {
        draft.id: ProductWorkflowState.DRAFT,
        copy_ready.id: ProductWorkflowState.COPY_READY,
        poster_ready.id: ProductWorkflowState.POSTER_READY,
        failed_draft.id: ProductWorkflowState.FAILED,
        failed_copy.id: ProductWorkflowState.FAILED,
        failed_node.id: ProductWorkflowState.FAILED,
        inactive_failed.id: ProductWorkflowState.DRAFT,
    }
    all_products, _ = list_products(db_session, status=None, page=1, page_size=20)
    assert {
        product.id: derive_product_state(product)
        for product in all_products
        if product.id in expected_state_by_product_id
    } == expected_state_by_product_id

    product_selects: list[str] = []

    @event.listens_for(db_session.bind, "before_cursor_execute")
    def record_product_query(conn, cursor, statement, parameters, context, executemany):
        normalized_statement = " ".join(statement.lower().split())
        if (
            normalized_statement.startswith("select products.id")
            and "from products" in normalized_statement
            and "limit" in normalized_statement
        ):
            product_selects.append(normalized_statement)

    try:
        products, total = list_products(
            db_session,
            status=ProductWorkflowState.DRAFT,
            page=1,
            page_size=1,
        )
    finally:
        event.remove(db_session.bind, "before_cursor_execute", record_product_query)

    assert total == 2
    assert len(products) == 1
    assert {product.id for product in products} <= {draft.id, inactive_failed.id}
    assert len(product_selects) == 1
    assert "exists" in product_selects[0]
    assert "limit" in product_selects[0]

    draft_products, draft_total = list_products(
        db_session,
        status=ProductWorkflowState.DRAFT,
        page=1,
        page_size=10,
    )

    copy_products, copy_total = list_products(
        db_session,
        status=ProductWorkflowState.COPY_READY,
        page=1,
        page_size=10,
    )
    poster_products, poster_total = list_products(
        db_session,
        status=ProductWorkflowState.POSTER_READY,
        page=1,
        page_size=10,
    )
    failed_products, failed_total = list_products(
        db_session,
        status=ProductWorkflowState.FAILED,
        page=1,
        page_size=10,
    )

    assert draft_total == 2
    assert {product.id for product in draft_products} == {draft.id, inactive_failed.id}
    assert copy_total == 1
    assert [product.id for product in copy_products] == [copy_ready.id]
    assert poster_total == 1
    assert [product.id for product in poster_products] == [poster_ready.id]
    assert failed_total == 3
    assert {product.id for product in failed_products} == {failed_draft.id, failed_copy.id, failed_node.id}

    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    expected_ids_by_status = {
        ProductWorkflowState.DRAFT: {draft.id, inactive_failed.id},
        ProductWorkflowState.COPY_READY: {copy_ready.id},
        ProductWorkflowState.POSTER_READY: {poster_ready.id},
        ProductWorkflowState.FAILED: {failed_draft.id, failed_copy.id, failed_node.id},
    }
    for status, expected_ids in expected_ids_by_status.items():
        response = client.get("/api/products", params={"status": status.value})
        assert response.status_code == 200
        payload = response.json()
        assert payload["total"] == len(expected_ids)
        assert {item["id"] for item in payload["items"]} == expected_ids
        assert {item["workflow_state"] for item in payload["items"]} == {status.value}


def test_product_reference_image_can_be_deleted(configured_env: Path, db_session) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "香薰蜡烛", "category": "家居", "price": "49.00"},
        files=[
            ("image", ("main.png", _make_demo_image_bytes(), "image/png")),
            ("reference_images", ("ref.png", _make_demo_image_bytes(), "image/png")),
        ],
    )
    assert created.status_code == 201
    payload = created.json()
    original_asset = next(asset for asset in payload["source_assets"] if asset["kind"] == "original_image")
    reference_asset = next(asset for asset in payload["source_assets"] if asset["kind"] == "reference_image")

    db_session.expire_all()
    persisted_reference = db_session.get(SourceAsset, reference_asset["id"])
    assert persisted_reference is not None
    reference_path = Path(configured_env) / persisted_reference.storage_path
    reference_variants = _variant_paths(reference_path)
    assert reference_path.exists()
    assert reference_variants

    deleted = client.delete(f"/api/source-assets/{reference_asset['id']}")
    assert deleted.status_code == 200
    assert all(asset["id"] != reference_asset["id"] for asset in deleted.json()["source_assets"])

    db_session.expire_all()
    assert db_session.get(SourceAsset, reference_asset["id"]) is None
    assert not reference_path.exists()
    assert all(not path.exists() for path in reference_variants)

    rejected = client.delete(f"/api/source-assets/{original_asset['id']}")
    assert rejected.status_code == 400
    assert "只能删除商品参考图" in rejected.json()["detail"]
