from __future__ import annotations

import threading
from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from helpers import (
    _execute_workflow_queue_inline,
    _login,
    _make_demo_image_bytes,
    _make_demo_image_bytes_with_size,
    _wait_for_workflow_run,
)

from productflow_backend.application.contracts import (
    PosterGenerationInput,
)
from productflow_backend.application.use_cases import (
    confirm_copy_set,
    create_copy_job,
    create_poster_job,
    execute_copy_job,
    execute_poster_job,
    get_product_detail,
)
from productflow_backend.domain.enums import (
    PosterKind,
)
from productflow_backend.infrastructure.db.models import (
    AppSetting,
    PosterVariant,
)
from productflow_backend.infrastructure.db.session import get_session_factory


@pytest.fixture(autouse=True)
def _execute_workflow_queue_inline_fixture(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep API workflow tests deterministic while production delivery goes through Dramatiq."""

    _execute_workflow_queue_inline(monkeypatch)


def test_reference_workflow_node_upload_replaces_current_image(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "桌面收纳盒"},
        files={"image": ("box.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    reference_node = next(node for node in workflow_response.json()["nodes"] if node["node_type"] == "reference_image")

    first_upload = client.post(
        f"/api/workflow-nodes/{reference_node['id']}/image",
        data={"role": "style", "label": "第一次参考"},
        files={"image": ("first.png", _make_demo_image_bytes(), "image/png")},
    )
    assert first_upload.status_code == 200
    first_node = next(node for node in first_upload.json()["nodes"] if node["id"] == reference_node["id"])
    first_asset_id = first_node["output_json"]["source_asset_ids"][0]
    assert first_node["config_json"]["source_asset_ids"] == [first_asset_id]
    assert first_node["output_json"]["source_asset_ids"] == [first_asset_id]

    second_upload = client.post(
        f"/api/workflow-nodes/{reference_node['id']}/image",
        data={"role": "style", "label": "第二次参考"},
        files={"image": ("second.png", _make_demo_image_bytes_with_size(640, 480), "image/png")},
    )
    assert second_upload.status_code == 200
    second_node = next(node for node in second_upload.json()["nodes"] if node["id"] == reference_node["id"])
    second_asset_id = second_node["output_json"]["source_asset_ids"][0]
    assert second_asset_id != first_asset_id
    assert second_node["config_json"]["source_asset_ids"] == [second_asset_id]
    assert second_node["output_json"]["source_asset_ids"] == [second_asset_id]
    assert second_node["output_json"]["image_asset_ids"] == [second_asset_id]
    assert len(second_node["output_json"]["images"]) == 1

    product_after = client.get(f"/api/products/{product_id}")
    assert product_after.status_code == 200
    reference_asset_ids = {
        asset["id"] for asset in product_after.json()["source_assets"] if asset["kind"] == "reference_image"
    }
    assert {first_asset_id, second_asset_id}.issubset(reference_asset_ids)

def test_reference_workflow_node_can_bind_existing_source_or_poster_image(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "桌面灯架"},
        files={"image": ("lamp-stand.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    session = get_session_factory()()
    try:
        copy_job = create_copy_job(session, product_id=product_id).job
        execute_copy_job(copy_job.id)
        session.expire_all()
        product = get_product_detail(session, product_id)
        confirm_copy_set(session, copy_set_id=product.copy_sets[0].id)
        poster_job = create_poster_job(session, product_id=product_id).job
        execute_poster_job(poster_job.id)
        session.expire_all()
        poster_id = get_product_detail(session, product_id).poster_variants[0].id
    finally:
        session.close()

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    reference_node = next(node for node in workflow_response.json()["nodes"] if node["node_type"] == "reference_image")

    bound_poster = client.post(
        f"/api/workflow-nodes/{reference_node['id']}/image-source",
        json={"poster_variant_id": poster_id},
    )
    assert bound_poster.status_code == 200
    poster_bound_node = next(node for node in bound_poster.json()["nodes"] if node["id"] == reference_node["id"])
    materialized_asset_id = poster_bound_node["output_json"]["source_asset_ids"][0]
    assert poster_bound_node["config_json"]["source_asset_ids"] == [materialized_asset_id]
    assert poster_bound_node["config_json"]["source_poster_variant_id"] == poster_id
    assert poster_bound_node["output_json"]["source_poster_variant_id"] == poster_id

    product_after_poster = client.get(f"/api/products/{product_id}")
    assert product_after_poster.status_code == 200
    reference_assets_after_poster = [
        asset for asset in product_after_poster.json()["source_assets"] if asset["kind"] == "reference_image"
    ]
    materialized_asset = next(asset for asset in reference_assets_after_poster if asset["id"] == materialized_asset_id)
    assert materialized_asset["original_filename"] == f"poster-{poster_id}.png"
    assert materialized_asset["source_poster_variant_id"] == poster_id
    reference_asset_ids_after_poster = [asset["id"] for asset in reference_assets_after_poster]
    assert materialized_asset_id in reference_asset_ids_after_poster

    conflicting_upload = client.post(
        f"/api/products/{product_id}/reference-images",
        files={"reference_images": (f"poster-{poster_id}.png", _make_demo_image_bytes(), "image/png")},
    )
    assert conflicting_upload.status_code == 200
    conflicting_asset = next(
        asset
        for asset in conflicting_upload.json()["source_assets"]
        if asset["kind"] == "reference_image" and asset["id"] != materialized_asset_id
    )
    assert conflicting_asset["original_filename"] == f"poster-{poster_id}.png"
    assert conflicting_asset["source_poster_variant_id"] is None

    rebound_to_user_upload = client.post(
        f"/api/workflow-nodes/{reference_node['id']}/image-source",
        json={"source_asset_id": conflicting_asset["id"]},
    )
    assert rebound_to_user_upload.status_code == 200
    user_upload_bound_node = next(
        node for node in rebound_to_user_upload.json()["nodes"] if node["id"] == reference_node["id"]
    )
    assert user_upload_bound_node["output_json"]["source_asset_ids"] == [conflicting_asset["id"]]
    assert "source_poster_variant_id" not in user_upload_bound_node["output_json"]

    product_after_conflicting_upload = client.get(f"/api/products/{product_id}")
    assert product_after_conflicting_upload.status_code == 200
    reference_asset_ids_after_conflicting_upload = [
        asset["id"]
        for asset in product_after_conflicting_upload.json()["source_assets"]
        if asset["kind"] == "reference_image"
    ]
    assert sorted(reference_asset_ids_after_conflicting_upload) == sorted(
        [*reference_asset_ids_after_poster, conflicting_asset["id"]]
    )

    second_reference = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "reference_image",
            "title": "复用参考图",
            "position_x": 720,
            "position_y": 320,
            "config_json": {"role": "reference", "label": "复用参考图"},
        },
    )
    assert second_reference.status_code == 201
    second_reference_node = next(node for node in second_reference.json()["nodes"] if node["title"] == "复用参考图")

    bound_source = client.post(
        f"/api/workflow-nodes/{second_reference_node['id']}/image-source",
        json={"source_asset_id": materialized_asset_id},
    )
    assert bound_source.status_code == 200
    source_bound_node = next(node for node in bound_source.json()["nodes"] if node["id"] == second_reference_node["id"])
    assert source_bound_node["output_json"]["source_asset_ids"] == [materialized_asset_id]
    assert source_bound_node["config_json"]["source_asset_ids"] == [materialized_asset_id]
    assert source_bound_node["config_json"]["source_poster_variant_id"] == poster_id
    assert source_bound_node["output_json"]["source_poster_variant_id"] == poster_id

    product_after_source = client.get(f"/api/products/{product_id}")
    assert product_after_source.status_code == 200
    reference_asset_ids_after_source = [
        asset["id"] for asset in product_after_source.json()["source_assets"] if asset["kind"] == "reference_image"
    ]
    assert sorted(reference_asset_ids_after_source) == sorted(reference_asset_ids_after_conflicting_upload)

    third_reference = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "reference_image",
            "title": "复用海报",
            "position_x": 980,
            "position_y": 320,
            "config_json": {"role": "reference", "label": "复用海报"},
        },
    )
    assert third_reference.status_code == 201
    third_reference_node = next(node for node in third_reference.json()["nodes"] if node["title"] == "复用海报")

    rebound_poster = client.post(
        f"/api/workflow-nodes/{third_reference_node['id']}/image-source",
        json={"poster_variant_id": poster_id},
    )
    assert rebound_poster.status_code == 200
    rebound_node = next(node for node in rebound_poster.json()["nodes"] if node["id"] == third_reference_node["id"])
    assert rebound_node["output_json"]["source_asset_ids"] == [materialized_asset_id]
    assert rebound_node["output_json"]["source_poster_variant_id"] == poster_id

    product_after_rebound = client.get(f"/api/products/{product_id}")
    assert product_after_rebound.status_code == 200
    reference_asset_ids_after_rebound = [
        asset["id"] for asset in product_after_rebound.json()["source_assets"] if asset["kind"] == "reference_image"
    ]
    assert sorted(reference_asset_ids_after_rebound) == sorted(reference_asset_ids_after_conflicting_upload)

def test_reference_workflow_node_bind_poster_reports_missing_file_as_bad_request(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "文件缺失海报"},
        files={"image": ("missing-poster.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    session = get_session_factory()()
    try:
        copy_job = create_copy_job(session, product_id=product_id).job
        execute_copy_job(copy_job.id)
        session.expire_all()
        product = get_product_detail(session, product_id)
        confirm_copy_set(session, copy_set_id=product.copy_sets[0].id)
        poster_job = create_poster_job(session, product_id=product_id).job
        execute_poster_job(poster_job.id)
        session.expire_all()
        poster_id = get_product_detail(session, product_id).poster_variants[0].id
        poster = session.get(PosterVariant, poster_id)
        assert poster is not None
        poster.storage_path = f"products/{product_id}/missing-poster-file.png"
        session.commit()
    finally:
        session.close()

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    reference_node = next(node for node in workflow_response.json()["nodes"] if node["node_type"] == "reference_image")

    response = client.post(
        f"/api/workflow-nodes/{reference_node['id']}/image-source",
        json={"poster_variant_id": poster_id},
    )

    assert response.status_code == 400
    assert response.json()["detail"] == "海报文件不存在"

def test_image_generation_fill_replaces_reference_node_current_image(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "床头灯"},
        files={"image": ("lamp.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")
    reference_node = next(node for node in workflow["nodes"] if node["node_type"] == "reference_image")

    upload = client.post(
        f"/api/workflow-nodes/{reference_node['id']}/image",
        data={"role": "reference", "label": "旧参考"},
        files={"image": ("old.png", _make_demo_image_bytes(), "image/png")},
    )
    assert upload.status_code == 200
    uploaded_reference = next(node for node in upload.json()["nodes"] if node["id"] == reference_node["id"])
    old_asset_id = uploaded_reference["output_json"]["source_asset_ids"][0]

    connected = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": image_node["id"],
            "target_node_id": reference_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert connected.status_code == 201

    run_response = client.post(f"/api/products/{product_id}/workflow/run", json={"start_node_id": image_node["id"]})
    assert run_response.status_code == 200
    payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    filled_reference = next(node for node in payload["nodes"] if node["id"] == reference_node["id"])
    new_asset_id = filled_reference["output_json"]["source_asset_ids"][0]
    assert new_asset_id != old_asset_id
    assert filled_reference["config_json"]["source_asset_ids"] == [new_asset_id]
    assert filled_reference["output_json"]["source_asset_ids"] == [new_asset_id]
    assert len(filled_reference["output_json"]["images"]) == 1

    product_after = client.get(f"/api/products/{product_id}")
    assert product_after.status_code == 200
    reference_asset_ids = {
        asset["id"] for asset in product_after.json()["source_assets"] if asset["kind"] == "reference_image"
    }
    assert {old_asset_id, new_asset_id}.issubset(reference_asset_ids)

def test_image_generation_fills_multiple_targets_with_concurrent_provider_calls(
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

    class CoordinatedImageProvider:
        provider_name = "coordinated"
        prompt_version = "coordinated-v1"

        def __init__(self) -> None:
            self._lock = threading.Lock()
            self._both_started = threading.Event()
            self.started = 0
            self.max_in_flight = 0
            self._in_flight = 0
            self.thread_ids: list[int] = []

        def generate_poster_image(
            self,
            poster: PosterGenerationInput,
            kind: PosterKind,
        ) -> tuple[GeneratedImagePayload, str]:
            del poster
            with self._lock:
                self.thread_ids.append(threading.get_ident())
                self.started += 1
                self._in_flight += 1
                self.max_in_flight = max(self.max_in_flight, self._in_flight)
                call_index = self.started
                if self.started >= 2:
                    self._both_started.set()
            if not self._both_started.wait(timeout=1.0):
                raise AssertionError("provider calls were not initiated concurrently")
            try:
                return (
                    GeneratedImagePayload(
                        kind=kind,
                        bytes_data=_make_demo_image_bytes(),
                        mime_type="image/png",
                        width=800,
                        height=800,
                        variant_label=f"coordinated-{call_index}",
                    ),
                    "coordinated-v1",
                )
            finally:
                with self._lock:
                    self._in_flight -= 1

    fake_provider = CoordinatedImageProvider()
    provider_factory_thread_ids: list[int] = []

    def fake_provider_factory() -> CoordinatedImageProvider:
        provider_factory_thread_ids.append(threading.get_ident())
        return fake_provider

    monkeypatch.setattr(
        "productflow_backend.application.product_workflows.get_image_provider",
        fake_provider_factory,
    )

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "并发生图商品"},
        files={"image": ("parallel.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")
    second_target = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "reference_image",
            "title": "并发参考图 2",
            "position_x": 1180,
            "position_y": 240,
            "config_json": {"role": "reference", "label": "并发参考图 2"},
        },
    )
    assert second_target.status_code == 201
    second_target_node = next(node for node in second_target.json()["nodes"] if node["title"] == "并发参考图 2")
    connected = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": image_node["id"],
            "target_node_id": second_target_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert connected.status_code == 201

    run_response = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert run_response.status_code == 200
    payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    image_output = next(node for node in payload["nodes"] if node["id"] == image_node["id"])["output_json"]
    assert len(provider_factory_thread_ids) == 2
    assert set(provider_factory_thread_ids).isdisjoint(fake_provider.thread_ids)
    assert fake_provider.started == 2
    assert fake_provider.max_in_flight == 2
    assert image_output["target_count"] == 2
    assert len(image_output["filled_reference_node_ids"]) == 2
    assert len(image_output["filled_source_asset_ids"]) == 2
    assert len(image_output["generated_poster_variant_ids"]) == 2
    assert "poster_variant_ids" not in image_output

def test_workflow_node_can_be_deleted_with_connected_edges(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "可删节点商品"},
        files={"image": ("node.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]
    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    copy_node = next(node for node in workflow["nodes"] if node["node_type"] == "copy_generation")
    connected_edge_ids = {
        edge["id"]
        for edge in workflow["edges"]
        if edge["source_node_id"] == copy_node["id"] or edge["target_node_id"] == copy_node["id"]
    }
    assert connected_edge_ids

    deleted = client.delete(f"/api/workflow-nodes/{copy_node['id']}")
    assert deleted.status_code == 200
    deleted_payload = deleted.json()
    assert copy_node["id"] not in {node["id"] for node in deleted_payload["nodes"]}
    assert all(
        edge["source_node_id"] != copy_node["id"] and edge["target_node_id"] != copy_node["id"]
        for edge in deleted_payload["edges"]
    )
    assert connected_edge_ids.isdisjoint({edge["id"] for edge in deleted_payload["edges"]})

    refreshed = client.get(f"/api/products/{product_id}/workflow")
    assert refreshed.status_code == 200
    assert copy_node["id"] not in {node["id"] for node in refreshed.json()["nodes"]}
