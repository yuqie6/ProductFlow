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


def test_submit_launch_kit_generation_task_creates_durable_task(configured_env: Path, db_session) -> None:
    from productflow_backend.application.launch_kit.generation import submit_launch_kit_generation_task

    ensure_starter_category_playbooks(db_session)
    app = create_app()
    client = TestClient(app)
    _login(client)
    created = client.post(
        "/api/launch-kits",
        json={
            "product_name": "Son dưỡng có màu",
            "category_key": "beauty",
            "target_platforms": ["shopee"],
        },
    ).json()
    enqueued: list[str] = []

    launch_kit = submit_launch_kit_generation_task(
        db_session,
        launch_kit_id=created["id"],
        enqueue=enqueued.append,
    )

    assert launch_kit.status == LaunchKitStatus.GENERATING
    assert len(enqueued) == 1
    task = db_session.get(LaunchKitGenerationTask, enqueued[0])
    assert task is not None
    assert task.status == JobStatus.QUEUED
    assert task.progress_stage == LaunchKitProgressStage.EXTRACTING_FACTS
    assert task.is_cancelable is True


def test_launch_kit_generation_rejects_duplicate_active_task(configured_env: Path, db_session) -> None:
    from productflow_backend.application.launch_kit.generation import submit_launch_kit_generation_task
    from productflow_backend.domain.errors import BusinessValidationError

    ensure_starter_category_playbooks(db_session)
    app = create_app()
    client = TestClient(app)
    _login(client)
    created = client.post(
        "/api/launch-kits",
        json={
            "product_name": "Túi tote canvas",
            "category_key": "fashion",
            "target_platforms": ["tiktok_shop"],
        },
    ).json()
    submit_launch_kit_generation_task(db_session, launch_kit_id=created["id"], enqueue=lambda _task_id: None)

    try:
        submit_launch_kit_generation_task(db_session, launch_kit_id=created["id"], enqueue=lambda _task_id: None)
    except BusinessValidationError as exc:
        assert "already queued or running" in str(exc)
    else:  # pragma: no cover - assertion helper
        raise AssertionError("duplicate active task should fail")


def test_execute_launch_kit_generation_task_produces_manual_export_content(configured_env: Path, db_session) -> None:
    from productflow_backend.application.launch_kit.generation import (
        execute_launch_kit_generation_task,
        submit_launch_kit_generation_task,
    )
    from productflow_backend.domain.launch_kits import LaunchKitExportType, LaunchKitVariantKind
    from productflow_backend.infrastructure.db.session import get_session_factory

    ensure_starter_category_playbooks(db_session)
    app = create_app()
    client = TestClient(app)
    _login(client)
    created = client.post(
        "/api/launch-kits",
        json={
            "product_name": "Áo khoác chống nắng UPF50",
            "category_key": "fashion",
            "target_platforms": ["shopee", "tiktok_shop"],
            "source_references": {
                "pasted_reference_text": "Vải nhẹ, có nhiều màu, size M L XL, chống nắng khi đi xe máy.",
                "reference_urls": ["https://example.com/listing"],
                "notes": "Tone rõ ràng, tránh claim y tế.",
            },
        },
    ).json()
    enqueued: list[str] = []
    submit_launch_kit_generation_task(db_session, launch_kit_id=created["id"], enqueue=enqueued.append)

    execute_launch_kit_generation_task(enqueued[0], session_factory=get_session_factory())

    db_session.expire_all()
    launch_kit = db_session.get(LaunchKit, created["id"])
    assert launch_kit is not None
    assert launch_kit.status == LaunchKitStatus.READY
    assert launch_kit.buyer_angle_key == "proof_first_practical_value"
    assert launch_kit.generated_summary_json["product_facts"]["product_name"] == "Áo khoác chống nắng UPF50"
    assert launch_kit.selected_angle_json["label"] == "Proof-first practical value"
    assert len(launch_kit.variants) == 3
    assert {variant.kind for variant in launch_kit.variants} == {
        LaunchKitVariantKind.FULL_KIT,
        LaunchKitVariantKind.IMAGE_PLAN,
    }
    assert launch_kit.quality_scores[-1].score_json["overall"] > 0
    assert launch_kit.export_snapshot_json["manual_export"]["platform_blocks"]
    assert {export.export_type for export in launch_kit.exports} == {
        LaunchKitExportType.MARKDOWN,
        LaunchKitExportType.CHECKLIST,
    }
    task = db_session.get(LaunchKitGenerationTask, enqueued[0])
    assert task is not None
    assert task.status == JobStatus.SUCCEEDED
    assert task.progress_stage == LaunchKitProgressStage.EXPORTING_OPTIONAL_SNAPSHOT

    exported = client.get(f"/api/launch-kits/{created['id']}/exports/markdown")
    assert exported.status_code == 200
    assert exported.headers["content-type"].startswith("text/markdown")
    assert "attachment;" in exported.headers["content-disposition"]
    assert "# Áo khoác chống nắng UPF50 LaunchKit" in exported.text
    assert "## Platform copy blocks" in exported.text
    assert "## Manual export checklist" in exported.text


def test_launch_kit_generate_endpoint_can_run_inline_without_queue(
    configured_env: Path,
    db_session,
    monkeypatch,
) -> None:
    from productflow_backend.config import get_settings

    monkeypatch.setenv("LAUNCH_KIT_INLINE_GENERATION", "true")
    get_settings.cache_clear()
    ensure_starter_category_playbooks(db_session)
    app = create_app()
    client = TestClient(app)
    _login(client)
    created = client.post(
        "/api/launch-kits",
        json={
            "product_name": "Kệ để bàn nhỏ",
            "category_key": "home_goods",
            "target_platforms": ["shopee"],
            "source_references": {"pasted_reference_text": "Kích thước nhỏ gọn, dùng trên bàn làm việc."},
        },
    ).json()

    generated = client.post(f"/api/launch-kits/{created['id']}/generate")

    assert generated.status_code == 202
    payload = generated.json()
    assert payload["status"] == "ready"
    assert payload["latest_task"]["status"] == "succeeded"
    assert payload["export_snapshot"]["manual_export"]["platform_blocks"]
    assert payload["quality_score_summary"]["overall"] > 0
    get_settings.cache_clear()
