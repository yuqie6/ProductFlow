from __future__ import annotations

from datetime import UTC, datetime, timedelta
from pathlib import Path

from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes

from productflow_backend.application.products import list_products
from productflow_backend.infrastructure.db.models import Product, ProductWorkflow


def test_v2_product_create_persists_context_without_prebuilding_workflow(
    configured_env: Path,
    db_session,
) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)

    response = client.post(
        "/api/v2/products",
        data={
            "name": "露营保温杯",
            "category": "户外",
            "price": "79.00",
            "source_note": "316 不锈钢，主打长效保温和车载杯架适配。",
        },
        files={"images": ("cup.png", _make_demo_image_bytes(), "image/png")},
    )

    assert response.status_code == 201
    payload = response.json()
    assert payload["product"]["source_note"] == "316 不锈钢，主打长效保温和车载杯架适配。"
    assert len(payload["created_assets"]) == 1
    product_id = payload["product"]["id"]
    db_session.expire_all()
    assert db_session.query(ProductWorkflow).filter_by(product_id=product_id).count() == 0


def test_product_name_search_is_literal_case_insensitive_and_paginated(db_session) -> None:
    products = [
        Product(name="Alpha Studio Lamp"),
        Product(name="alpha Task Lamp"),
        Product(name="100%_Cotton Tote"),
        Product(name="Walnut Shelf"),
    ]
    db_session.add_all(products)
    db_session.commit()

    first_page, total = list_products(db_session, page=1, page_size=1, q="  ALPHA  ")
    second_page, second_total = list_products(db_session, page=2, page_size=1, q="alpha")
    literal, literal_total = list_products(db_session, page=1, page_size=20, q="%_")

    assert total == second_total == 2
    assert len(first_page) == len(second_page) == 1
    assert first_page[0].id != second_page[0].id
    assert literal_total == 1
    assert [product.name for product in literal] == ["100%_Cotton Tote"]


def test_product_list_sort_is_stable_before_pagination(configured_env: Path, db_session) -> None:
    base_time = datetime(2025, 1, 1, tzinfo=UTC)
    alpha = Product(
        id="00000000-0000-0000-0000-000000000001",
        name="Alpha Lamp",
        created_at=base_time + timedelta(days=2),
        updated_at=base_time + timedelta(days=1),
    )
    alpha_peer = Product(
        id="00000000-0000-0000-0000-000000000002",
        name="Alpha Lamp",
        created_at=base_time + timedelta(days=2),
        updated_at=base_time + timedelta(days=1),
    )
    bravo = Product(
        name="bravo Shelf",
        created_at=base_time + timedelta(days=3),
        updated_at=base_time + timedelta(days=2),
    )
    zulu = Product(
        name="Zulu Camera",
        created_at=base_time + timedelta(days=1),
        updated_at=base_time + timedelta(days=3),
    )
    db_session.add_all([alpha, alpha_peer, bravo, zulu])
    db_session.commit()

    first_page, total = list_products(db_session, page=1, page_size=3)
    second_page, second_total = list_products(db_session, page=2, page_size=3)
    created, created_total = list_products(db_session, page=1, page_size=4, sort="created_desc")
    named, named_total = list_products(db_session, page=1, page_size=4, sort="name_asc")

    assert total == second_total == created_total == named_total == 4
    assert [product.id for product in first_page] == [zulu.id, bravo.id, alpha_peer.id]
    assert [product.id for product in second_page] == [alpha.id]
    assert [product.id for product in created] == [bravo.id, alpha_peer.id, alpha.id, zulu.id]
    assert [product.id for product in named] == [alpha.id, alpha_peer.id, bravo.id, zulu.id]

    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    response = client.get("/api/v2/products", params={"sort": "name_asc", "page_size": 3})
    assert response.status_code == 200
    assert [item["id"] for item in response.json()["items"]] == [alpha.id, alpha_peer.id, bravo.id]
    assert client.get("/api/v2/products", params={"sort": "unknown"}).status_code == 422
