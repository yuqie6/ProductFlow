from __future__ import annotations

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes

from productflow_backend.application.agent.product_intake import AgentProductSelectionV1
from productflow_backend.application.agent.product_workspaces import create_agent_product_workspace
from productflow_backend.application.product_workflow.graph_commands import (
    apply_graph_change_set,
    create_empty_workflow_graph,
)
from productflow_backend.application.product_workflow.graph_contracts import CreateNodeOp, WorkflowChangeSet
from productflow_backend.application.product_workflow.graph_queries import get_graph_projection
from productflow_backend.domain.enums import GraphActorType, GraphNodeType
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.presentation.api import create_app


def _workspace(db_session, key: str):
    selection = AgentProductSelectionV1.model_validate(
        {
            "schema_version": 1,
            "image_types": [{"key": "hero", "quantity": 1, "order": 0}],
        }
    )
    return create_agent_product_workspace(
        db_session,
        name=f"空白画布 {key}",
        selection=selection,
        image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
        idempotency_key=key,
    )


def test_create_empty_workflow_graph_persists_v3_and_accepts_node_types(db_session) -> None:
    workspace = _workspace(db_session, "empty-graph-app")
    graph = create_empty_workflow_graph(db_session, product_id=workspace.product.id)
    projection = get_graph_projection(db_session, product_id=workspace.product.id, graph_id=graph.id)
    assert projection.schema_version == 3
    assert projection.revision == 1
    assert projection.nodes == ()
    assert projection.edges == ()

    result = apply_graph_change_set(
        db_session,
        product_id=workspace.product.id,
        graph_id=graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=1,
            summary="添加商品资料",
            actor_type=GraphActorType.USER,
            operations=[
                CreateNodeOp(
                    client_ref="n1",
                    node_type=GraphNodeType.PRODUCT_SOURCE,
                    title="商品资料",
                    position_x=120,
                    position_y=80,
                    config={"source_product_id": workspace.product.id},
                )
            ],
        ),
    )
    live = get_graph_projection(db_session, product_id=workspace.product.id, graph_id=result.graph.id)
    assert [node.node_type for node in live.nodes] == [GraphNodeType.PRODUCT_SOURCE]

    with pytest.raises(ConflictError, match="已有 active schema-v3"):
        create_empty_workflow_graph(db_session, product_id=workspace.product.id)


def test_create_empty_workflow_graph_rejects_missing_product(db_session) -> None:
    with pytest.raises(NotFoundError, match="商品不存在"):
        create_empty_workflow_graph(db_session, product_id="missing-product")


def test_empty_canvas_create_http_persists_and_rejects_second(configured_env) -> None:
    from productflow_backend.application.agent.sessions import create_agent_session
    from productflow_backend.infrastructure.db.session import get_session_factory

    session_factory = get_session_factory()
    session = session_factory()
    try:
        agent_session = create_agent_session(session, title="空白画布 Session")
    finally:
        session.close()

    client = TestClient(create_app())
    _login(client)
    draft = client.post(
        "/api/v2/agent-product-workspaces/drafts",
        json={"name": "空白建图商品", "agent_session_id": agent_session.id},
        headers={"Idempotency-Key": "empty-canvas-http-1"},
    )
    assert draft.status_code == 201, draft.text
    product_id = draft.json()["product"]["id"]
    missing = client.get(f"/api/v3/products/{product_id}/workflows/current")
    assert missing.status_code == 404

    created = client.post(f"/api/v3/products/{product_id}/workflows")
    assert created.status_code == 201, created.text
    payload = created.json()
    assert payload["schema_version"] == 3
    assert payload["revision"] == 1
    assert payload["nodes"] == []
    assert payload["edges"] == []

    current = client.get(f"/api/v3/products/{product_id}/workflows/current")
    assert current.status_code == 200
    assert current.json()["id"] == payload["id"]

    added = client.post(
        f"/api/v3/products/{product_id}/workflows/{payload['id']}/changesets",
        json={
            "base_graph_revision": 1,
            "summary": "添加商品资料",
            "operations": [
                {
                    "op": "create_node",
                    "client_ref": "n1",
                    "node_type": "product_source",
                    "title": "商品资料",
                    "position_x": 120,
                    "position_y": 80,
                    "config": {"source_product_id": product_id},
                }
            ],
        },
    )
    assert added.status_code == 200, added.text
    added_payload = added.json()
    assert added_payload["revision"] == 2
    assert [node["node_type"] for node in added_payload["nodes"]] == ["product_source"]

    duplicate = client.post(f"/api/v3/products/{product_id}/workflows")
    assert duplicate.status_code == 409
    assert "已有" in duplicate.json()["detail"]
