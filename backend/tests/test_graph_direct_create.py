from __future__ import annotations

import json

from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from sqlalchemy import func, select
from test_graph_execution import RecordingImageProvider, RecordingPromptProvider

from productflow_backend.application.product_workflow.dependencies import WorkflowExecutionDependencies
from productflow_backend.application.product_workflow.graph_direct_create import create_product_with_direct_graph
from productflow_backend.application.product_workflow.graph_execution import execute_graph_run
from productflow_backend.application.product_workflow.graph_template import DirectCreateImageType
from productflow_backend.domain.enums import GraphNodeType
from productflow_backend.infrastructure.db.models import WorkflowDraft, WorkflowGraph
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.presentation.api import create_app


def test_direct_create_writes_v3_graph_without_draft_or_v2_workflow(db_session) -> None:
    result = create_product_with_direct_graph(
        db_session,
        name="直接创建演示商品",
        category="工业收纳",
        price="199.00",
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "ref.png", "image/png")],
        image_types=[
            DirectCreateImageType(key="hero", quantity=2, order=0),
            DirectCreateImageType(key="detail", quantity=1, order=1),
        ],
    )
    assert result.graph.schema_version == 3
    assert result.graph.revision == 1
    assert result.projection.revision == 1
    types = {node.node_type for node in result.projection.nodes}
    assert GraphNodeType.PRODUCT_SOURCE in types
    assert GraphNodeType.IMAGE_ASSET in types
    product_source = next(
        node for node in result.projection.nodes if node.node_type == GraphNodeType.PRODUCT_SOURCE
    )
    assert product_source.config["source_product_id"] == result.product.id
    assert product_source.product_source is not None
    assert product_source.product_source.fact_set_version_id == result.product.current_fact_set_version_id
    assert {fact["key"] for fact in product_source.product_source.facts} == {
        "product_name",
        "category",
        "price",
    }
    image_nodes = [node for node in result.projection.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION]
    prompt_nodes = [node for node in result.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION]
    assert len(image_nodes) == 3
    unused = [node for node in result.projection.nodes if node.unused]
    assert unused == []
    asset = next(node for node in result.projection.nodes if node.node_type == GraphNodeType.IMAGE_ASSET)
    assert asset.unused is False
    assert all(any(edge.role.value == "reference" for edge in node.incoming) for node in image_nodes)
    assert all(any(edge.role.value == "reference" for edge in node.incoming) for node in prompt_nodes)
    assert all(any(edge.role.value == "facts" for edge in node.incoming) for node in prompt_nodes)
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraft)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowGraph)) == 1


def test_direct_create_and_changeset_api_round_trip(configured_env, monkeypatch) -> None:
    client = TestClient(create_app())
    _login(client)
    created = client.post(
        "/api/v3/products",
        data={
            "name": "API 直接创建商品",
            "category": "工业收纳",
            "price": "88.00",
            "image_types": json.dumps([{"key": "hero", "quantity": 1}]),
        },
        files=[("images", ("product.png", _make_demo_image_bytes(), "image/png"))],
    )
    assert created.status_code == 201, created.text
    payload = created.json()
    product_id = payload["product"]["id"]
    graph_id = payload["graph"]["id"]
    assert payload["graph"]["schema_version"] == 3
    assert payload["graph"]["revision"] == 1
    assert payload["graph"]["last_operation_group_id"]
    assert payload["graph"]["can_undo"] is True
    assert payload["graph"]["can_redo"] is False
    source = next(node for node in payload["graph"]["nodes"] if node["node_type"] == "product_source")
    assert source["source_product"]["id"] == product_id
    assert source["product_fact_set"]["facts"][0]["key"] == "product_name"
    assert any(node["node_type"] == "image_asset" and not node["unused"] for node in payload["graph"]["nodes"])
    current = client.get(f"/api/v3/products/{product_id}/workflows/current")
    assert current.status_code == 200, current.text
    assert current.json()["id"] == graph_id
    brief = next(node for node in current.json()["nodes"] if node["node_type"] == "creative_brief")
    renamed = client.post(
        f"/api/v3/products/{product_id}/workflows/{graph_id}/changesets",
        json={
            "base_graph_revision": 1,
            "summary": "重命名创作要求",
            "operations": [{"op": "rename_node", "node_ref": brief["id"], "title": "拍摄要求"}],
        },
    )
    assert renamed.status_code == 200, renamed.text
    assert renamed.json()["revision"] == 2
    renamed_brief = next(node for node in renamed.json()["nodes"] if node["id"] == brief["id"])
    assert renamed_brief["title"] == "拍摄要求"
    stale = client.post(
        f"/api/v3/products/{product_id}/workflows/{graph_id}/changesets",
        json={
            "base_graph_revision": 1,
            "summary": "过期写入",
            "operations": [{"op": "rename_node", "node_ref": brief["id"], "title": "不会生效"}],
        },
    )
    assert stale.status_code == 409
    image_bytes = _make_demo_image_bytes()
    dependencies = WorkflowExecutionDependencies(
        prompt_generation_provider_resolver=lambda: RecordingPromptProvider(),
        image_provider_resolver=lambda: RecordingImageProvider(image_bytes),
    )

    def execute_inline(run_id: str) -> None:
        execute_graph_run(run_id, dependencies=dependencies)

    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.graph_runs.enqueue_graph_run",
        execute_inline,
    )
    image_node_id = next(
        node["id"] for node in renamed.json()["nodes"] if node["node_type"] == "image_generation"
    )
    run = client.post(
        f"/api/v3/products/{product_id}/workflows/{graph_id}/runs",
        json={"scope": "to_node", "node_id": image_node_id},
    )
    assert run.status_code == 201, run.text
    assert run.json()["status"] == "succeeded"
    assert {item["status"] for item in run.json()["node_runs"]} == {"succeeded"}
    factory = get_session_factory()
    session = factory()
    try:
        assert session.scalar(select(func.count()).select_from(WorkflowDraft)) == 0
    finally:
        session.close()


def test_direct_create_api_writes_brief_and_generation_spec(configured_env) -> None:
    client = TestClient(create_app())
    _login(client)
    created = client.post(
        "/api/v3/products",
        data={
            "name": "带字海报商品",
            "source_note": "无线洗地机，面向都市白领，画面干净专业",
            "image_types": json.dumps([{"key": "hero", "quantity": 1}]),
            "generation_spec": json.dumps(
                {
                    "aspect_ratio": "3:4",
                    "text_policy": "required",
                    "text_language": "zh-CN",
                }
            ),
        },
        files=[("images", ("product.png", _make_demo_image_bytes(), "image/png"))],
    )
    assert created.status_code == 201, created.text
    payload = created.json()
    assert payload["product"]["source_note"] == "无线洗地机，面向都市白领，画面干净专业"
    brief = next(node for node in payload["graph"]["nodes"] if node["node_type"] == "creative_brief")
    image = next(node for node in payload["graph"]["nodes"] if node["node_type"] == "image_generation")
    assert brief["config"]["goal"] == "无线洗地机，面向都市白领，画面干净专业"
    assert image["config"]["generation_spec"]["aspect_ratio"] == "3:4"
    assert image["config"]["generation_spec"]["text_policy"] == "required"
    assert image["config"]["generation_spec"]["text_language"] == "zh-CN"
