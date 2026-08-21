from __future__ import annotations

from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes

from productflow_backend.presentation.api import create_app


def test_product_facts_put_creates_immutable_versions_and_checks_expected_version(configured_env) -> None:
    client = TestClient(create_app())
    _login(client)
    created = client.post(
        "/api/v2/products",
        data={"name": "事实商品", "category": "收纳", "price": "12.00"},
        files=[("images", ("product.png", _make_demo_image_bytes(), "image/png"))],
    )
    assert created.status_code == 201, created.text
    product_id = created.json()["product"]["id"]

    initial = client.get(f"/api/v3/products/{product_id}/facts")
    assert initial.status_code == 200, initial.text
    assert initial.json()["current_fact_set_version_id"] is None

    first = client.put(
        f"/api/v3/products/{product_id}/facts",
        json={
            "name": "事实商品新名",
            "category": "厨房",
            "price": "13.50",
            "source_note": "用户维护",
            "facts": [{"key": "material", "value": "钢"}],
        },
    )
    assert first.status_code == 200, first.text
    first_payload = first.json()
    first_id = first_payload["current_fact_set_version_id"]
    assert first_payload["current_fact_version"] == 1
    assert first_payload["facts"][0]["key"] == "material"

    second = client.put(
        f"/api/v3/products/{product_id}/facts",
        json={
            "expected_fact_set_version_id": first_id,
            "facts": [{"key": "material", "value": "不锈钢"}],
        },
    )
    assert second.status_code == 200, second.text
    assert second.json()["current_fact_version"] == 2
    assert second.json()["current_fact_set_version_id"] != first_id

    stale = client.put(
        f"/api/v3/products/{product_id}/facts",
        json={
            "expected_fact_set_version_id": first_id,
            "facts": [{"key": "material", "value": "铝"}],
        },
    )
    assert stale.status_code == 409, stale.text

    latest = client.get(f"/api/v3/products/{product_id}/facts")
    assert latest.json()["facts"][0]["value"] == "不锈钢"
