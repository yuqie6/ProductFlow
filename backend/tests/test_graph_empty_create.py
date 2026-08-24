from __future__ import annotations

import pytest
from fastapi.testclient import TestClient
from helpers import _login

from productflow_backend.application.product_facts import product_metadata_facts, stage_product_fact_set
from productflow_backend.application.product_workflow.graph_commands import (
    apply_graph_change_set,
    create_empty_workflow_graph,
)
from productflow_backend.application.products import stage_canonical_product
from productflow_backend.application.product_workflow.graph_contracts import CreateNodeOp, WorkflowChangeSet
from productflow_backend.application.product_workflow.graph_queries import get_graph_projection
from productflow_backend.domain.enums import GraphActorType, GraphNodeType
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.presentation.api import create_app


def _product(db_session, name: str):
    product = stage_canonical_product(
        db_session,
        name=name,
        category=None,
        price=None,
        source_note=None,
    )
    stage_product_fact_set(db_session, product=product, facts=product_metadata_facts(product))
    db_session.commit()
    return product


def test_create_empty_workflow_graph_persists_v3_and_accepts_node_types(db_session) -> None:
    product = _product(db_session, "空白画布 empty-graph-app")
    graph = create_empty_workflow_graph(db_session, product_id=product.id)
    projection = get_graph_projection(db_session, product_id=product.id, graph_id=graph.id)
    assert projection.schema_version == 3
    assert projection.revision == 1
    assert projection.nodes == ()
    assert projection.edges == ()

    result = apply_graph_change_set(
        db_session,
        product_id=product.id,
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
                    config={"source_product_id": product.id},
                )
            ],
        ),
    )
    live = get_graph_projection(db_session, product_id=product.id, graph_id=result.graph.id)
    assert [node.node_type for node in live.nodes] == [GraphNodeType.PRODUCT_SOURCE]

    with pytest.raises(ConflictError, match="已有 active schema-v3"):
        create_empty_workflow_graph(db_session, product_id=product.id)


def test_create_empty_workflow_graph_rejects_missing_product(db_session) -> None:
    with pytest.raises(NotFoundError, match="商品不存在"):
        create_empty_workflow_graph(db_session, product_id="missing-product")


def test_empty_canvas_create_http_persists_and_rejects_second(configured_env) -> None:
    from productflow_backend.infrastructure.db.session import get_session_factory

    session = get_session_factory()()
    try:
        product = _product(session, "空白建图商品")
        product_id = product.id
    finally:
        session.close()
    client = TestClient(create_app())
    _login(client)
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
