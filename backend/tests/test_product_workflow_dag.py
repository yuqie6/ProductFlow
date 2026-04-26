from __future__ import annotations

from pathlib import Path

import pytest
import sqlalchemy as sa
from fastapi.testclient import TestClient
from helpers import (
    _execute_workflow_queue_inline,
    _login,
    _make_demo_image_bytes,
    _wait_for_workflow_run,
)

from productflow_backend.application.contracts import (
    PosterGenerationInput,
)
from productflow_backend.domain.enums import (
    PosterKind,
    WorkflowNodeType,
)
from productflow_backend.infrastructure.db.models import (
    AppSetting,
    ProductWorkflow,
    WorkflowNode,
)
from productflow_backend.infrastructure.db.session import get_session_factory


@pytest.fixture(autouse=True)
def _execute_workflow_queue_inline_fixture(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep API workflow tests deterministic while production delivery goes through Dramatiq."""

    _execute_workflow_queue_inline(monkeypatch)


def test_product_workflow_dag_runs_and_persists_artifacts(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "多功能收纳架"},
        files={"image": ("rack.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    assert len(workflow["nodes"]) >= 4
    assert len(workflow["edges"]) >= 3
    assert {node["node_type"] for node in workflow["nodes"]} == {
        "product_context",
        "reference_image",
        "copy_generation",
        "image_generation",
    }

    context_node = next(node for node in workflow["nodes"] if node["node_type"] == "product_context")
    updated_context = client.patch(
        f"/api/workflow-nodes/{context_node['id']}",
        json={
            "position_x": 96,
            "position_y": 144,
            "config_json": {
                "name": "多功能收纳架",
                "category": "家居",
                "price": "49.90",
                "source_note": "免打孔安装，适合厨房和浴室，强调承重和整洁。",
            }
        },
    )
    assert updated_context.status_code == 200
    moved_context = next(node for node in updated_context.json()["nodes"] if node["id"] == context_node["id"])
    assert moved_context["position_x"] == 96
    assert moved_context["position_y"] == 144

    manual_reference = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "reference_image",
            "title": "风格参考",
            "position_x": 320,
            "position_y": 260,
            "config_json": {"role": "style", "label": "厨房风格"},
        },
    )
    assert manual_reference.status_code == 201
    upload_node = next(
        node
        for node in manual_reference.json()["nodes"]
        if node["node_type"] == "reference_image" and node["title"] == "风格参考"
    )
    uploaded = client.post(
        f"/api/workflow-nodes/{upload_node['id']}/image",
        data={"role": "style", "label": "厨房风格图"},
        files={"image": ("style.png", _make_demo_image_bytes(), "image/png")},
    )
    assert uploaded.status_code == 200
    uploaded_node = next(node for node in uploaded.json()["nodes"] if node["id"] == upload_node["id"])
    assert uploaded_node["output_json"]["source_asset_ids"]

    copy_node = next(node for node in workflow["nodes"] if node["node_type"] == "copy_generation")
    updated = client.patch(
        f"/api/workflow-nodes/{copy_node['id']}",
        json={"config_json": {"instruction": "突出免打孔和厨房整洁场景"}},
    )
    assert updated.status_code == 200
    reference_to_copy = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": upload_node["id"],
            "target_node_id": copy_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert reference_to_copy.status_code == 201
    image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")
    updated_image = client.patch(
        f"/api/workflow-nodes/{image_node['id']}",
        json={"config_json": {"instruction": "沿用上游文案和参考图，生成主图", "size": "1024x1024"}},
    )
    assert updated_image.status_code == 200

    upstream_edge = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": upload_node["id"],
            "target_node_id": image_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert upstream_edge.status_code == 201
    default_reference_node = next(
        node
        for node in workflow["nodes"]
        if node["node_type"] == "reference_image" and node["title"] == "参考图"
    )
    default_target_edge = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": image_node["id"],
            "target_node_id": default_reference_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert default_target_edge.status_code == 201
    second_target = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "reference_image",
            "title": "参考图 2",
            "position_x": 1160,
            "position_y": 240,
            "config_json": {"role": "reference", "label": "参考图 2"},
        },
    )
    assert second_target.status_code == 201
    second_target_node = next(node for node in second_target.json()["nodes"] if node["title"] == "参考图 2")
    second_target_edge = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": image_node["id"],
            "target_node_id": second_target_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert second_target_edge.status_code == 201
    duplicate_target_edge = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": image_node["id"],
            "target_node_id": second_target_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert duplicate_target_edge.status_code == 201
    workflow_before_run = duplicate_target_edge.json()

    run_response = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert run_response.status_code == 200
    assert run_response.json()["runs"][0]["status"] == "running"
    run_payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    assert run_payload["runs"][0]["status"] == "succeeded"
    assert all(node["status"] == "succeeded" for node in run_payload["nodes"])
    copy_output = next(node for node in run_payload["nodes"] if node["node_type"] == "copy_generation")["output_json"]
    assert copy_output["copy_set_id"]
    assert "免打孔" in " ".join(copy_output["selling_points"])
    assert "厨房风格图" in " ".join(copy_output["selling_points"])
    edited_copy = client.patch(
        f"/api/workflow-nodes/{copy_node['id']}/copy",
        json={
            "title": "厨房免打孔收纳架",
            "selling_points": ["免打孔安装", "厨房台面更整洁", "承重稳定"],
            "poster_headline": "厨房整洁一步到位",
            "cta": "立即整理厨房",
        },
    )
    assert edited_copy.status_code == 200
    edited_copy_node = next(node for node in edited_copy.json()["nodes"] if node["id"] == copy_node["id"])
    assert edited_copy_node["output_json"]["title"] == "厨房免打孔收纳架"
    assert edited_copy_node["output_json"]["poster_headline"] == "厨房整洁一步到位"
    rerun_image = client.post(
        f"/api/products/{product_id}/workflow/run",
        json={"start_node_id": image_node["id"]},
    )
    assert rerun_image.status_code == 200
    rerun_payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    assert rerun_payload["runs"][0]["status"] == "succeeded"
    rerun_copy_output = next(node for node in rerun_payload["nodes"] if node["id"] == copy_node["id"])["output_json"]
    rerun_image_output = next(node for node in rerun_payload["nodes"] if node["id"] == image_node["id"])["output_json"]
    assert rerun_copy_output["copy_set_id"] == copy_output["copy_set_id"]
    assert rerun_copy_output["poster_headline"] == "厨房整洁一步到位"
    assert rerun_image_output["copy_set_id"] == copy_output["copy_set_id"]
    image_output = next(node for node in run_payload["nodes"] if node["node_type"] == "image_generation")["output_json"]
    assert "poster_variant_ids" not in image_output
    assert len(image_output["generated_poster_variant_ids"]) == 2
    assert image_output["target_count"] == 2
    assert len(image_output["filled_source_asset_ids"]) == 2
    assert len(image_output["filled_reference_node_ids"]) == 2
    assert image_output["size"] == "1024x1024"
    context_sources = image_output["context_sources"]
    assert any(source["label"] == "文案" and "多功能收纳架" in source["text"] for source in context_sources)
    assert any(source["label"] == "参考图" and "厨房风格图" in source["text"] for source in context_sources)
    assert image_output["context_summary"]["reference_image_count"] >= 1
    filled_nodes = [
        node for node in run_payload["nodes"] if node["id"] in set(image_output["filled_reference_node_ids"])
    ]
    assert all(node["output_json"]["source_asset_ids"] for node in filled_nodes)

    product_after = client.get(f"/api/products/{product_id}")
    assert product_after.status_code == 200
    product_payload = product_after.json()
    assert any(copy_set["id"] == copy_output["copy_set_id"] for copy_set in product_payload["copy_sets"])
    assert len(product_payload["poster_variants"]) == 4
    reference_assets = [asset for asset in product_payload["source_assets"] if asset["kind"] == "reference_image"]
    assert len(reference_assets) == 5

    rejected_cycle = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": image_node["id"],
            "target_node_id": copy_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert rejected_cycle.status_code == 400
    assert "循环依赖" in rejected_cycle.json()["detail"]
    refreshed = client.get(f"/api/products/{product_id}/workflow")
    assert refreshed.status_code == 200
    assert len(refreshed.json()["edges"]) == len(workflow_before_run["edges"])

    edge_to_delete = refreshed.json()["edges"][0]
    deleted_edge = client.delete(f"/api/workflow-edges/{edge_to_delete['id']}")
    assert deleted_edge.status_code == 200
    deleted_payload = deleted_edge.json()
    assert len(deleted_payload["edges"]) == len(workflow_before_run["edges"]) - 1
    assert edge_to_delete["id"] not in {edge["id"] for edge in deleted_payload["edges"]}

    isolated_image = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "image_generation",
            "title": "未连接生图",
            "position_x": 620,
            "position_y": 420,
            "config_json": {"instruction": "生成但不落槽", "size": "1024x1024"},
        },
    )
    assert isolated_image.status_code == 201
    isolated_image_node = next(node for node in isolated_image.json()["nodes"] if node["title"] == "未连接生图")
    context_to_isolated = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": context_node["id"],
            "target_node_id": isolated_image_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert context_to_isolated.status_code == 201
    copy_to_isolated = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": copy_node["id"],
            "target_node_id": isolated_image_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert copy_to_isolated.status_code == 201
    direct_run = client.post(
        f"/api/products/{product_id}/workflow/run",
        json={"start_node_id": isolated_image_node["id"]},
    )
    assert direct_run.status_code == 200
    direct_payload = _wait_for_workflow_run(client, product_id, status="failed")
    assert direct_payload["runs"][0]["status"] == "failed"
    assert "至少一个图片/参考图节点" in direct_payload["runs"][0]["failure_reason"]
    direct_node = next(node for node in direct_payload["nodes"] if node["id"] == isolated_image_node["id"])
    assert direct_node["status"] == "failed"
    assert "至少一个图片/参考图节点" in direct_node["failure_reason"]

    session = get_session_factory()()
    try:
        assert session.query(ProductWorkflow).filter_by(product_id=product_id).count() == 1
    finally:
        session.close()

def test_product_workflow_singleton_context_and_direct_image_run(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "直跑台灯"},
        files={"image": ("lamp.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    list_response = client.get("/api/products?page=1&page_size=1")
    assert list_response.status_code == 200
    listed = list_response.json()
    assert listed["total"] == 1
    summary = listed["items"][0]
    assert summary["source_image_filename"] == "lamp.png"
    assert summary["source_image_thumbnail_url"].endswith("variant=thumbnail")

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    context_node = next(node for node in workflow["nodes"] if node["node_type"] == "product_context")
    image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")

    duplicate_context = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "product_context",
            "title": "重复商品",
            "position_x": 120,
            "position_y": 120,
            "config_json": {},
        },
    )
    assert duplicate_context.status_code == 400
    assert duplicate_context.json()["detail"] == "商品资料节点已存在"

    session = get_session_factory()()
    try:
        persisted_workflow = session.scalar(sa.select(ProductWorkflow).where(ProductWorkflow.product_id == product_id))
        assert persisted_workflow is not None
        duplicate_node = WorkflowNode(
            workflow_id=persisted_workflow.id,
            node_type=WorkflowNodeType.PRODUCT_CONTEXT,
            title="历史重复商品",
            position_x=180,
            position_y=140,
            config_json={},
        )
        session.add(duplicate_node)
        session.commit()
    finally:
        session.close()

    normalized_response = client.get(f"/api/products/{product_id}/workflow")
    assert normalized_response.status_code == 200
    normalized_workflow = normalized_response.json()
    assert [node["node_type"] for node in normalized_workflow["nodes"]].count("product_context") == 1

    removable_nodes = [
        node for node in normalized_workflow["nodes"] if node["node_type"] in {"copy_generation", "reference_image"}
    ]
    for removable in removable_nodes:
        deleted = client.delete(f"/api/workflow-nodes/{removable['id']}")
        assert deleted.status_code == 200

    patched_image = client.patch(
        f"/api/workflow-nodes/{image_node['id']}",
        json={"config_json": {"instruction": "只根据商品资料生成干净主图", "size": "1024x1024"}},
    )
    assert patched_image.status_code == 200

    run_response = client.post(
        f"/api/products/{product_id}/workflow/run",
        json={"start_node_id": image_node["id"]},
    )
    assert run_response.status_code == 200
    payload = _wait_for_workflow_run(client, product_id, status="failed")
    assert [node["node_type"] for node in payload["nodes"]] == ["product_context", "image_generation"]
    image_node_after = next(node for node in payload["nodes"] if node["id"] == image_node["id"])
    assert image_node_after["status"] == "failed"
    assert "至少一个图片/参考图节点" in image_node_after["failure_reason"]
    assert next(node for node in payload["nodes"] if node["id"] == context_node["id"])["node_type"] == "product_context"

def test_direct_downstream_run_uses_latest_saved_product_context(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "旅行背包"},
        files={"image": ("bag.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    context_node = next(node for node in workflow["nodes"] if node["node_type"] == "product_context")
    image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")

    initial_context = client.patch(
        f"/api/workflow-nodes/{context_node['id']}",
        json={
            "config_json": {
                "name": "旅行背包",
                "category": "旧类目",
                "price": "199",
                "source_note": "旧说明：城市通勤。",
            }
        },
    )
    assert initial_context.status_code == 200
    first_run = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert first_run.status_code == 200
    first_payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    stale_context_output = next(
        node for node in first_payload["nodes"] if node["id"] == context_node["id"]
    )["output_json"]
    assert stale_context_output["source_note"] == "旧说明：城市通勤。"

    latest_context = client.patch(
        f"/api/workflow-nodes/{context_node['id']}",
        json={
            "config_json": {
                "name": "旅行背包",
                "category": "户外装备",
                "price": "249",
                "source_note": "最新说明：防泼水牛津布，适合短途出差和周末露营。",
            }
        },
    )
    assert latest_context.status_code == 200

    selected_run = client.post(
        f"/api/products/{product_id}/workflow/run",
        json={"start_node_id": image_node["id"]},
    )
    assert selected_run.status_code == 200
    payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    image_output = next(node for node in payload["nodes"] if node["id"] == image_node["id"])["output_json"]

    assert image_output["context_summary"]["product_context"]["category"] == "户外装备"
    assert image_output["context_summary"]["product_context"]["price"] == "249"
    assert (
        image_output["context_summary"]["product_context"]["source_note"]
        == "最新说明：防泼水牛津布，适合短途出差和周末露营。"
    )
    assert any("最新说明" in source["text"] for source in image_output["context_sources"])

def test_product_context_source_image_reaches_image_generation_context(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.infrastructure.image.base import GeneratedImagePayload
    from productflow_backend.presentation.api import create_app

    session = get_session_factory()()
    try:
        session.add(AppSetting(key="poster_generation_mode", value="generated"))
        session.commit()
    finally:
        session.close()

    captured_inputs: list[PosterGenerationInput] = []

    class CapturingImageProvider:
        provider_name = "capturing"
        prompt_version = "capturing-v1"

        def generate_poster_image(
            self,
            poster: PosterGenerationInput,
            kind: PosterKind,
        ) -> tuple[GeneratedImagePayload, str]:
            captured_inputs.append(poster)
            return (
                GeneratedImagePayload(
                    kind=kind,
                    bytes_data=_make_demo_image_bytes(),
                    mime_type="image/png",
                    width=800,
                    height=800,
                    variant_label=f"capturing-r{len(poster.reference_images)}",
                ),
                "capturing-v1",
            )

    monkeypatch.setattr(
        "productflow_backend.application.product_workflows.get_image_provider",
        CapturingImageProvider,
    )

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "旅行背包"},
        files={"image": ("bag.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    context_node = next(node for node in workflow["nodes"] if node["node_type"] == "product_context")
    image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")
    assert any(
        edge["source_node_id"] == context_node["id"] and edge["target_node_id"] == image_node["id"]
        for edge in workflow["edges"]
    )

    selected_run = client.post(
        f"/api/products/{product_id}/workflow/run",
        json={"start_node_id": image_node["id"]},
    )
    assert selected_run.status_code == 200
    payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    image_output = next(node for node in payload["nodes"] if node["id"] == image_node["id"])["output_json"]

    assert image_output["context_summary"]["reference_image_count"] == 1
    assert any(
        source["label"] == "商品图" and "bag.png" in source["text"]
        for source in image_output["context_sources"]
    )
    assert image_output["context_summary"]["copy_prompt_mode"] == "copy"
    assert len(captured_inputs) == 1
    provider_input = captured_inputs[0]
    assert provider_input.copy_prompt_mode == "copy"
    assert len(provider_input.reference_images) == 1
    assert provider_input.reference_images[0].path == provider_input.source_image

def test_single_node_workflow_run_reuses_succeeded_upstream_outputs(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "露营杯"},
        files={"image": ("cup.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    initial_workflow = client.get(f"/api/products/{product_id}/workflow")
    assert initial_workflow.status_code == 200
    workflow = initial_workflow.json()
    upstream_image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")
    upstream_reference_node = next(node for node in workflow["nodes"] if node["node_type"] == "reference_image")
    upstream_slot_edge = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": upstream_image_node["id"],
            "target_node_id": upstream_reference_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert upstream_slot_edge.status_code == 201

    first_run = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert first_run.status_code == 200
    first_payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    assert first_payload["runs"][0]["status"] == "succeeded"
    succeeded_image_node = next(node for node in first_payload["nodes"] if node["id"] == upstream_image_node["id"])
    succeeded_reference_node = next(
        node for node in first_payload["nodes"] if node["id"] == upstream_reference_node["id"]
    )
    upstream_poster_ids = succeeded_image_node["output_json"]["generated_poster_variant_ids"]
    upstream_reference_asset_ids = succeeded_reference_node["output_json"]["source_asset_ids"]
    assert upstream_poster_ids
    assert upstream_reference_asset_ids

    downstream_image = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "image_generation",
            "title": "下游生图",
            "position_x": 900,
            "position_y": 360,
            "config_json": {"instruction": "沿用上游图片继续生成", "size": "1024x1024"},
        },
    )
    assert downstream_image.status_code == 201
    downstream_image_node = next(node for node in downstream_image.json()["nodes"] if node["title"] == "下游生图")
    downstream_reference = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "reference_image",
            "title": "下游参考图",
            "position_x": 1180,
            "position_y": 360,
            "config_json": {"role": "reference", "label": "下游参考图"},
        },
    )
    assert downstream_reference.status_code == 201
    downstream_reference_node = next(
        node for node in downstream_reference.json()["nodes"] if node["title"] == "下游参考图"
    )
    upstream_edge = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": upstream_image_node["id"],
            "target_node_id": downstream_image_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert upstream_edge.status_code == 201
    target_edge = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": downstream_image_node["id"],
            "target_node_id": downstream_reference_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert target_edge.status_code == 201

    product_before = client.get(f"/api/products/{product_id}")
    assert product_before.status_code == 200
    copy_count_before = len(product_before.json()["copy_sets"])
    poster_count_before = len(product_before.json()["poster_variants"])
    source_asset_count_before = len(product_before.json()["source_assets"])

    single_run = client.post(
        f"/api/products/{product_id}/workflow/run",
        json={"start_node_id": downstream_image_node["id"]},
    )
    assert single_run.status_code == 200
    single_payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    assert single_payload["runs"][0]["status"] == "succeeded"
    assert [node_run["node_id"] for node_run in single_payload["runs"][0]["node_runs"]] == [downstream_image_node["id"]]

    unchanged_upstream_image = next(node for node in single_payload["nodes"] if node["id"] == upstream_image_node["id"])
    unchanged_reference = next(node for node in single_payload["nodes"] if node["id"] == upstream_reference_node["id"])
    downstream_after = next(node for node in single_payload["nodes"] if node["id"] == downstream_image_node["id"])
    assert unchanged_upstream_image["output_json"]["generated_poster_variant_ids"] == upstream_poster_ids
    assert unchanged_reference["output_json"]["source_asset_ids"] == upstream_reference_asset_ids
    assert downstream_after["output_json"]["copy_set_id"] == unchanged_upstream_image["output_json"]["copy_set_id"]
    assert len(downstream_after["output_json"]["generated_poster_variant_ids"]) == 1
    assert "poster_variant_ids" not in downstream_after["output_json"]

    product_after = client.get(f"/api/products/{product_id}")
    assert product_after.status_code == 200
    assert len(product_after.json()["copy_sets"]) == copy_count_before
    assert len(product_after.json()["poster_variants"]) == poster_count_before + 1
    assert len(product_after.json()["source_assets"]) == source_asset_count_before + 1

def test_single_reference_run_reruns_upstream_when_target_slot_missing_artifact(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "桌面灯"},
        files={"image": ("lamp.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")

    first_run = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert first_run.status_code == 200
    assert _wait_for_workflow_run(client, product_id, status="succeeded")["runs"][0]["status"] == "succeeded"

    new_reference = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "reference_image",
            "title": "新增参考图",
            "position_x": 1180,
            "position_y": 380,
            "config_json": {"role": "reference", "label": "新增参考图"},
        },
    )
    assert new_reference.status_code == 201
    new_reference_node = next(node for node in new_reference.json()["nodes"] if node["title"] == "新增参考图")
    connected = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": image_node["id"],
            "target_node_id": new_reference_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert connected.status_code == 201

    product_before = client.get(f"/api/products/{product_id}")
    assert product_before.status_code == 200
    copy_count_before = len(product_before.json()["copy_sets"])

    slot_run = client.post(
        f"/api/products/{product_id}/workflow/run",
        json={"start_node_id": new_reference_node["id"]},
    )
    assert slot_run.status_code == 200
    payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    assert payload["runs"][0]["status"] == "succeeded"
    assert [node_run["node_id"] for node_run in payload["runs"][0]["node_runs"]] == [
        image_node["id"],
        new_reference_node["id"],
    ]
    filled_reference = next(node for node in payload["nodes"] if node["id"] == new_reference_node["id"])
    rerun_image = next(node for node in payload["nodes"] if node["id"] == image_node["id"])
    assert filled_reference["output_json"]["source_asset_ids"]
    assert new_reference_node["id"] in rerun_image["output_json"]["filled_reference_node_ids"]

    product_after = client.get(f"/api/products/{product_id}")
    assert product_after.status_code == 200
    assert len(product_after.json()["copy_sets"]) == copy_count_before
