from __future__ import annotations

from pathlib import Path

from fastapi.testclient import TestClient
from helpers import _login

from productflow_backend.application.launch_kit.payloads import CategoryPlaybookPayload, SourceReferencePayload
from productflow_backend.application.launch_kit.playbooks import (
    STARTER_CATEGORY_PLAYBOOKS,
    ensure_starter_category_playbooks,
    get_active_category_playbook,
)
from productflow_backend.domain.enums import JobStatus
from productflow_backend.domain.launch_kits import LaunchKitProgressStage, LaunchKitStatus
from productflow_backend.infrastructure.db.models import (
    CategoryPlaybook,
    LaunchKit,
    LaunchKitGenerationTask,
    Product,
)
from productflow_backend.presentation.api import create_app


def test_launch_kit_models_keep_task_lifecycle_boring() -> None:
    assert LaunchKit.__table__.c.status.type.enums == [member.value for member in LaunchKitStatus]
    assert LaunchKitGenerationTask.__table__.c.status.type.enums == [member.value for member in JobStatus]
    assert LaunchKitGenerationTask.__table__.c.progress_stage.type.enums == [
        member.value for member in LaunchKitProgressStage
    ]
    assert "progress_stage" in LaunchKitGenerationTask.__table__.c
    assert "failure_category" in LaunchKitGenerationTask.__table__.c
    assert "is_retryable" in LaunchKitGenerationTask.__table__.c
    assert "is_cancelable" in LaunchKitGenerationTask.__table__.c


def test_starter_category_playbooks_seed_and_validate(configured_env: Path, db_session) -> None:
    ensure_starter_category_playbooks(db_session)

    keys = {row.key for row in db_session.query(CategoryPlaybook).all()}
    assert keys == set(STARTER_CATEGORY_PLAYBOOKS)

    fashion = get_active_category_playbook(db_session, "fashion")
    payload = CategoryPlaybookPayload.model_validate(fashion.playbook_json)
    assert payload.schema_version == 1
    assert payload.buyer_objections
    assert payload.required_visual_proof
    assert payload.suggested_image_sequence
    assert set(payload.platform_notes) == {"shopee", "tiktok_shop"}


def test_create_launch_kit_api_persists_product_root_and_summary_status(configured_env: Path, db_session) -> None:
    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/launch-kits",
        json={
            "product_name": "Áo khoác chống nắng",
            "category_key": "fashion",
            "target_platforms": ["shopee", "tiktok_shop"],
            "source_references": {
                "pasted_reference_text": "Chất vải nhẹ, chống nắng, nhiều màu.",
                "notes": "Need Vietnamese listing copy.",
            },
        },
    )

    assert created.status_code == 201
    payload = created.json()
    assert payload["status"] == "draft"
    assert payload["product_name"] == "Áo khoác chống nắng"
    assert payload["target_platforms"] == ["shopee", "tiktok_shop"]
    assert payload["source_references"]["schema_version"] == SourceReferencePayload().schema_version
    assert payload["source_references"]["pasted_reference_text"] == "Chất vải nhẹ, chống nắng, nhiều màu."
    assert payload["latest_task"] is None
    assert payload["variants"] == []

    db_session.expire_all()
    launch_kit = db_session.query(LaunchKit).filter_by(id=payload["id"]).one()
    product = db_session.get(Product, launch_kit.product_id)
    assert product is not None
    assert product.name == "Áo khoác chống nắng"
    assert product.category == "fashion"

    listed = client.get("/api/launch-kits")
    assert listed.status_code == 200
    listed_payload = listed.json()
    assert listed_payload["total"] == 1
    assert listed_payload["items"][0]["id"] == payload["id"]
    assert "variants" not in listed_payload["items"][0]

    status = client.get(f"/api/launch-kits/{payload['id']}/status")
    assert status.status_code == 200
    assert status.json() == {
        "id": payload["id"],
        "status": "draft",
        "latest_task": None,
        "updated_at": payload["updated_at"],
    }
