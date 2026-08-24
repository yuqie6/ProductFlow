from __future__ import annotations

from datetime import UTC, datetime

import pytest
from fastapi.testclient import TestClient
from helpers import _enable_deletion, _login, _make_demo_image_bytes
from sqlalchemy.exc import IntegrityError

from productflow_backend.application.product_images.assets import delete_product_image_asset
from productflow_backend.application.product_images.fidelity_checks import (
    create_product_image_fidelity_check,
    list_product_image_fidelity_checks,
)
from productflow_backend.domain.enums import MediaVerificationStatus, ProductImageFidelityOutcome
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    MediaObject,
    Product,
    ProductImageAsset,
    ProductImageFidelityCheck,
)
from productflow_backend.presentation.api import create_app
from productflow_backend.presentation.routes.image_fidelity_checks import router as image_fidelity_checks_router


def _make_asset(db_session, *, product_id: str, asset_id: str) -> ProductImageAsset:
    now = datetime.now(UTC)
    product = Product(id=product_id, name=f"商品-{product_id}")
    media = MediaObject(
        id=f"media-{asset_id}",
        storage_path=f"images/{asset_id}.png",
        mime_type="image/png",
        byte_size=4,
        width=1,
        height=1,
        sha256="a" * 64,
        verification_status=MediaVerificationStatus.VERIFIED,
        verified_at=now,
    )
    asset = ProductImageAsset(
        id=asset_id,
        product=product,
        media_object=media,
        origin_type="upload",
        display_name=asset_id,
        original_filename=f"{asset_id}.png",
    )
    db_session.add(asset)
    db_session.flush()
    return asset


def _valid_payload(*, expected_latest_version: int, key: str = "check-1") -> dict[str, object]:
    return {
        "expected_latest_version": expected_latest_version,
        "idempotency_key": key,
        "shape_fidelity": "pass",
        "color_material_fidelity": "fail",
        "logo_text_legibility": "not_applicable",
        "text_policy_compliance": "pass",
        "notes": "人工复核",
    }


def test_fidelity_check_versions_replay_conflict_and_old_rows_are_immutable(db_session) -> None:
    _make_asset(db_session, product_id="product-fidelity", asset_id="asset-fidelity")
    first = create_product_image_fidelity_check(
        db_session,
        product_id="product-fidelity",
        asset_id="asset-fidelity",
        **_valid_payload(expected_latest_version=0),
    )
    first_id = first.id
    first_shape = first.shape_fidelity

    replay = create_product_image_fidelity_check(
        db_session,
        product_id="product-fidelity",
        asset_id="asset-fidelity",
        **_valid_payload(expected_latest_version=0),
    )
    assert replay.id == first_id
    assert db_session.query(ProductImageFidelityCheck).count() == 1

    second = create_product_image_fidelity_check(
        db_session,
        product_id="product-fidelity",
        asset_id="asset-fidelity",
        **_valid_payload(expected_latest_version=1, key="check-2"),
    )
    assert second.version == 2
    assert first.version == 1
    assert first.shape_fidelity == first_shape

    with pytest.raises(ConflictError):
        create_product_image_fidelity_check(
            db_session,
            product_id="product-fidelity",
            asset_id="asset-fidelity",
            **_valid_payload(expected_latest_version=0, key="stale"),
        )
    with pytest.raises(ConflictError):
        create_product_image_fidelity_check(
            db_session,
            product_id="product-fidelity",
            asset_id="asset-fidelity",
            expected_latest_version=1,
            idempotency_key="check-1",
            shape_fidelity="fail",
            color_material_fidelity="fail",
            logo_text_legibility="not_applicable",
            text_policy_compliance="pass",
            notes="不同 payload",
        )

    listed = list_product_image_fidelity_checks(
        db_session,
        product_id="product-fidelity",
        asset_id="asset-fidelity",
    )
    assert listed.latest_version == 2
    assert [item.version for item in listed.items] == [2, 1]
    assert [item.shape_fidelity for item in listed.items] == ["pass", "pass"]


def test_fidelity_check_keeps_four_outcomes_independent_and_scopes_asset_to_product(db_session) -> None:
    _make_asset(db_session, product_id="product-fidelity-fields", asset_id="asset-fidelity-fields")
    check = create_product_image_fidelity_check(
        db_session,
        product_id="product-fidelity-fields",
        asset_id="asset-fidelity-fields",
        expected_latest_version=0,
        idempotency_key="independent-fields",
        shape_fidelity=ProductImageFidelityOutcome.PASS,
        color_material_fidelity=ProductImageFidelityOutcome.FAIL,
        logo_text_legibility=ProductImageFidelityOutcome.NOT_APPLICABLE,
        text_policy_compliance=ProductImageFidelityOutcome.FAIL,
        notes=None,
    )
    assert check.shape_fidelity == "pass"
    assert check.color_material_fidelity == "fail"
    assert check.logo_text_legibility == "not_applicable"
    assert check.text_policy_compliance == "fail"

    with pytest.raises(BusinessValidationError):
        create_product_image_fidelity_check(
            db_session,
            product_id="product-fidelity-fields",
            asset_id="asset-fidelity-fields",
            expected_latest_version=1,
            idempotency_key="invalid-outcome",
            shape_fidelity="unknown",
            color_material_fidelity="pass",
            logo_text_legibility="pass",
            text_policy_compliance="pass",
            notes=None,
        )

    _make_asset(db_session, product_id="other-product", asset_id="other-asset")
    with pytest.raises(NotFoundError):
        list_product_image_fidelity_checks(
            db_session,
            product_id="product-fidelity-fields",
            asset_id="other-asset",
        )


def test_fidelity_check_database_constraints_and_asset_delete_are_protected(db_session) -> None:
    asset = _make_asset(db_session, product_id="product-fidelity-fk", asset_id="asset-fidelity-fk")
    create_product_image_fidelity_check(
        db_session,
        product_id=asset.product_id,
        asset_id=asset.id,
        **_valid_payload(expected_latest_version=0),
    )
    db_session.add(
        ProductImageFidelityCheck(
            product_id=asset.product_id,
            asset_id=asset.id,
            version=99,
            shape_fidelity="invalid",
            color_material_fidelity="pass",
            logo_text_legibility="pass",
            text_policy_compliance="pass",
            checked_by="administrator",
            idempotency_key="invalid-db-outcome",
            request_hash="b" * 64,
        )
    )
    with pytest.raises(IntegrityError):
        db_session.commit()
    db_session.rollback()


def test_fidelity_check_history_blocks_application_asset_delete_and_preserves_history(db_session) -> None:
    asset = _make_asset(db_session, product_id="product-fidelity-owner", asset_id="asset-fidelity-owner")
    create_product_image_fidelity_check(
        db_session,
        product_id=asset.product_id,
        asset_id=asset.id,
        **_valid_payload(expected_latest_version=0),
    )

    with pytest.raises(ConflictError, match="人工保真检查历史"):
        delete_product_image_asset(db_session, asset_id=asset.id)

    assert db_session.get(ProductImageAsset, asset.id) is not None
    assert db_session.query(ProductImageFidelityCheck).filter_by(asset_id=asset.id).count() == 1


def test_fidelity_check_history_makes_asset_delete_api_return_conflict(configured_env, db_session) -> None:
    asset = _make_asset(db_session, product_id="product-fidelity-api-owner", asset_id="asset-fidelity-api-owner")
    create_product_image_fidelity_check(
        db_session,
        product_id=asset.product_id,
        asset_id=asset.id,
        **_valid_payload(expected_latest_version=0),
    )
    client = TestClient(create_app())
    _login(client)
    _enable_deletion(client)

    deleted = client.delete(f"/api/v2/product-image-assets/{asset.id}")
    assert deleted.status_code == 409, deleted.text
    assert "人工保真检查历史" in deleted.json()["detail"]
    assert db_session.get(ProductImageAsset, asset.id) is not None
    assert db_session.query(ProductImageFidelityCheck).filter_by(asset_id=asset.id).count() == 1

    db_session.delete(asset)
    with pytest.raises(IntegrityError):
        db_session.commit()
    db_session.rollback()


def test_fidelity_check_http_contract_uses_admin_auth_and_bounded_latest_list(configured_env) -> None:
    app = create_app()
    app.include_router(image_fidelity_checks_router)
    client = TestClient(app)
    _login(client)
    product = client.post(
        "/api/v2/products",
        data={"name": "人工检查商品"},
        files=[("images", ("reference.png", _make_demo_image_bytes(), "image/png"))],
    )
    assert product.status_code == 201, product.text
    product_id = product.json()["product"]["id"]
    asset_id = product.json()["created_assets"][0]["id"]
    path = f"/api/v3/products/{product_id}/image-assets/{asset_id}/fidelity-checks"

    first = client.post(path, json=_valid_payload(expected_latest_version=0))
    assert first.status_code == 201, first.text
    assert first.json()["checked_by"] == "administrator"
    assert len(first.json()["request_hash"]) == 64
    assert "storage_path" not in first.json()

    replay = client.post(path, json=_valid_payload(expected_latest_version=0))
    assert replay.status_code == 201, replay.text
    assert replay.json()["id"] == first.json()["id"]

    listed = client.get(path)
    assert listed.status_code == 200, listed.text
    assert listed.json()["latest_version"] == 1
    assert [item["version"] for item in listed.json()["items"]] == [1]

    invalid = client.post(
        path,
        json={**_valid_payload(expected_latest_version=1, key="invalid-http"), "shape_fidelity": "unknown"},
    )
    assert invalid.status_code == 422
