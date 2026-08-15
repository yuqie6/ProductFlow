from __future__ import annotations

import json
from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from pydantic import ValidationError
from sqlalchemy import event, func, select

from productflow_backend.application.agent_conversations import reserve_agent_turn
from productflow_backend.application.agent_product_intake import (
    AGENT_PRODUCT_IMAGE_TYPE_CATALOG,
    AgentProductSelectionV1,
    WorkflowIntakeV1,
)
from productflow_backend.application.agent_product_workspaces import (
    create_agent_product_draft_workspace,
    create_agent_product_workspace,
    finalize_agent_product_workspace_intake,
    get_agent_product_workspace,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    MediaObject,
    Product,
    ProductImageAsset,
    ProductWorkflow,
    WorkflowDraft,
    WorkflowDraftRevision,
    WorkflowEdge,
    WorkflowNode,
)
from productflow_backend.infrastructure.storage import LocalStorage


def _selection(*items: tuple[str, int]) -> AgentProductSelectionV1:
    return AgentProductSelectionV1.model_validate(
        {
            "schema_version": 1,
            "image_types": [
                {"key": key, "quantity": quantity, "order": order}
                for order, (key, quantity) in enumerate(items)
            ],
        }
    )


def _workspace_uploads() -> list[tuple[bytes, str, str]]:
    content = _make_demo_image_bytes()
    return [
        (content, "front.png", "image/png"),
        (content, "detail.png", "image/png"),
    ]


def _media_files(storage_root: Path) -> list[Path]:
    return [path for path in storage_root.glob("media/**/*") if path.is_file()]


def test_agent_product_image_type_catalog_and_selection_contract_are_strict() -> None:
    assert [(item.key, item.order) for item in AGENT_PRODUCT_IMAGE_TYPE_CATALOG] == [
        ("hero", 0),
        ("selling_point", 1),
        ("scene", 2),
        ("detail", 3),
        ("sku", 4),
        ("dimensions", 5),
        ("specifications", 6),
        ("after_sales", 7),
        ("brand_story", 8),
        ("precautions", 9),
        ("certification", 10),
        ("faq", 11),
        ("factory", 12),
        ("packaging", 13),
        ("shipping", 14),
    ]
    assert _selection(("hero", 2)).model_dump(mode="json") == {
        "schema_version": 1,
        "image_types": [{"key": "hero", "quantity": 2, "order": 0}],
    }

    invalid_payloads = [
        {"schema_version": 1, "image_types": []},
        {"schema_version": 2, "image_types": [{"key": "hero", "quantity": 2, "order": 0}]},
        {"schema_version": 1, "image_types": [{"key": "unknown", "quantity": 2, "order": 0}]},
        {"schema_version": 1, "image_types": [{"key": "hero", "quantity": 0, "order": 0}]},
        {"schema_version": 1, "image_types": [{"key": "hero", "quantity": 7, "order": 0}]},
        {
            "schema_version": 1,
            "image_types": [
                {"key": "hero", "quantity": 2, "order": 0},
                {"key": "hero", "quantity": 2, "order": 1},
            ],
        },
        {
            "schema_version": 1,
            "image_types": [
                {"key": "hero", "quantity": 2, "order": 1},
                {"key": "scene", "quantity": 2, "order": 0},
            ],
        },
        {
            "schema_version": 1,
            "image_types": [
                {"key": "hero", "quantity": 6, "order": 0},
                {"key": "scene", "quantity": 6, "order": 1},
                {"key": "detail", "quantity": 6, "order": 2},
                {"key": "sku", "quantity": 6, "order": 3},
                {"key": "faq", "quantity": 6, "order": 4},
                {"key": "shipping", "quantity": 1, "order": 5},
            ],
        },
        {
            "schema_version": 1,
            "image_types": [{"key": "hero", "quantity": 2, "order": 0, "hidden": True}],
        },
    ]
    for payload in invalid_payloads:
        with pytest.raises(ValidationError):
            AgentProductSelectionV1.model_validate(payload)


def test_create_agent_product_workspace_is_atomic_coverless_and_has_no_dag(
    configured_env: Path,
    db_session,
) -> None:
    creation = create_agent_product_workspace(
        db_session,
        name="  工业刀具收纳套装  ",
        selection=_selection(("hero", 3), ("detail", 2)),
        image_uploads=_workspace_uploads(),
        idempotency_key=" workspace-create-1 ",
    )

    assert creation.created is True
    assert creation.product.name == "工业刀具收纳套装"
    assert creation.product.cover_image_asset_id is None
    assert [asset.original_filename for asset in creation.created_assets] == ["front.png", "detail.png"]
    assert creation.workflow_draft.current_revision_id is None
    assert creation.workflow_draft.revisions == []
    assert creation.workflow_draft.intake_schema_version == 1
    intake = WorkflowIntakeV1.model_validate(creation.workflow_draft.intake_json)
    assert [(item.key, item.quantity, item.order) for item in intake.image_types] == [
        ("hero", 3, 0),
        ("detail", 2, 1),
    ]
    assert intake.reference_asset_ids == [asset.id for asset in creation.created_assets]
    assert creation.conversation.harness_run_id == creation.conversation.id
    assert creation.conversation.creation_idempotency_key == "workspace-create-1"
    assert len(creation.conversation.creation_request_hash or "") == 64

    assert db_session.scalar(select(func.count()).select_from(Product)) == 1
    assert db_session.scalar(select(func.count()).select_from(ProductImageAsset)) == 2
    assert db_session.scalar(select(func.count()).select_from(MediaObject)) == 2
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftRevision)) == 0
    assert db_session.scalar(select(func.count()).select_from(ProductWorkflow)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowNode)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowEdge)) == 0
    assert len(_media_files(configured_env)) == 6  # two originals plus preview and thumbnail variants


def test_agent_product_draft_workspace_creates_only_durable_identity_and_replays(db_session) -> None:
    first = create_agent_product_draft_workspace(
        db_session,
        name="  两阶段 Agent 商品  ",
        idempotency_key=" draft-workspace-1 ",
    )

    assert first.created is True
    assert first.product.name == "两阶段 Agent 商品"
    assert first.product.cover_image_asset_id is None
    assert first.created_assets == []
    assert first.workflow_draft.current_revision_id is None
    assert first.workflow_draft.revisions == []
    assert first.workflow_draft.intake_schema_version is None
    assert first.workflow_draft.intake_json is None
    assert first.conversation.creation_idempotency_key == "draft-workspace-1"
    assert first.conversation.intake_idempotency_key is None
    assert first.conversation.intake_request_hash is None

    replay = create_agent_product_draft_workspace(
        db_session,
        name="两阶段 Agent 商品",
        idempotency_key="draft-workspace-1",
    )
    restored = get_agent_product_workspace(
        db_session,
        conversation_id=first.conversation.id,
    )
    assert replay.created is False
    assert replay.product.id == first.product.id
    assert restored.created is False
    assert restored.conversation.id == first.conversation.id
    assert restored.created_assets == []
    assert db_session.scalar(select(func.count()).select_from(Product)) == 1
    assert db_session.scalar(select(func.count()).select_from(ProductImageAsset)) == 0
    assert db_session.scalar(select(func.count()).select_from(MediaObject)) == 0
    assert db_session.scalar(select(func.count()).select_from(ProductWorkflow)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftRevision)) == 0

    with pytest.raises(ConflictError, match="相同 Idempotency-Key"):
        create_agent_product_draft_workspace(
            db_session,
            name="不同商品",
            idempotency_key="draft-workspace-1",
        )


def test_agent_product_workspace_intake_finalization_is_atomic_idempotent_and_coverless(
    configured_env: Path,
    db_session,
) -> None:
    draft_creation = create_agent_product_draft_workspace(
        db_session,
        name="待确认商品",
        idempotency_key="draft-before-intake",
    )
    selection = _selection(("hero", 2), ("scene", 3))
    uploads = _workspace_uploads()

    finalized = finalize_agent_product_workspace_intake(
        db_session,
        conversation_id=draft_creation.conversation.id,
        selection=selection,
        image_uploads=uploads,
        idempotency_key="intake-finalize-1",
    )

    assert finalized.created is True
    assert finalized.product.id == draft_creation.product.id
    assert finalized.product.cover_image_asset_id is None
    assert [asset.original_filename for asset in finalized.created_assets] == ["front.png", "detail.png"]
    intake = WorkflowIntakeV1.model_validate(finalized.workflow_draft.intake_json)
    assert [(item.key, item.quantity, item.order) for item in intake.image_types] == [
        ("hero", 2, 0),
        ("scene", 3, 1),
    ]
    assert intake.reference_asset_ids == [asset.id for asset in finalized.created_assets]
    assert finalized.workflow_draft.current_revision_id is None
    assert finalized.workflow_draft.revisions == []
    assert finalized.conversation.intake_idempotency_key == "intake-finalize-1"
    assert len(finalized.conversation.intake_request_hash or "") == 64
    assert db_session.scalar(select(func.count()).select_from(ProductWorkflow)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowNode)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowEdge)) == 0

    first_files = sorted(path.relative_to(configured_env) for path in _media_files(configured_env))
    replay = finalize_agent_product_workspace_intake(
        db_session,
        conversation_id=draft_creation.conversation.id,
        selection=selection,
        image_uploads=uploads,
        idempotency_key="intake-finalize-1",
    )
    assert replay.created is False
    assert [asset.id for asset in replay.created_assets] == [asset.id for asset in finalized.created_assets]
    assert sorted(path.relative_to(configured_env) for path in _media_files(configured_env)) == first_files
    assert db_session.scalar(select(func.count()).select_from(ProductImageAsset)) == 2

    with pytest.raises(ConflictError, match="已经确认"):
        finalize_agent_product_workspace_intake(
            db_session,
            conversation_id=draft_creation.conversation.id,
            selection=_selection(("detail", 1)),
            image_uploads=uploads,
            idempotency_key="intake-finalize-1",
        )


def test_agent_product_workspace_intake_failure_preserves_empty_draft_and_cleans_storage(
    configured_env: Path,
    db_session,
) -> None:
    class FailSecondMediaStorage(LocalStorage):
        def __init__(self, root: Path) -> None:
            super().__init__(root)
            self.calls = 0

        def save_media_image(self, media_id: str, filename: str, content: bytes) -> str:
            self.calls += 1
            if self.calls == 2:
                raise OSError("injected finalization storage failure")
            return super().save_media_image(media_id, filename, content)

    workspace = create_agent_product_draft_workspace(
        db_session,
        name="确认失败商品",
        idempotency_key="draft-finalize-failure",
    )
    with pytest.raises(OSError, match="injected finalization storage failure"):
        finalize_agent_product_workspace_intake(
            db_session,
            conversation_id=workspace.conversation.id,
            selection=_selection(("hero", 2)),
            image_uploads=_workspace_uploads(),
            idempotency_key="intake-finalize-failure",
            storage=FailSecondMediaStorage(configured_env),
        )

    restored = get_agent_product_workspace(db_session, conversation_id=workspace.conversation.id)
    assert restored.workflow_draft.intake_json is None
    assert restored.conversation.intake_idempotency_key is None
    assert restored.created_assets == []
    assert db_session.scalar(select(func.count()).select_from(Product)) == 1
    assert db_session.scalar(select(func.count()).select_from(MediaObject)) == 0
    assert _media_files(configured_env) == []


def test_empty_agent_product_draft_rejects_turn_until_intake_is_finalized(
    configured_env: Path,
    db_session,
) -> None:
    workspace = create_agent_product_draft_workspace(
        db_session,
        name="Turn 边界商品",
        idempotency_key="draft-turn-boundary",
    )
    with pytest.raises(ConflictError, match="请先完成商品图片需求和参考图"):
        reserve_agent_turn(
            db_session,
            product_id=workspace.product.id,
            conversation_id=workspace.conversation.id,
            input_text="开始",
            input_asset_ids=[],
            idempotency_key="turn-before-intake",
        )

    finalized = finalize_agent_product_workspace_intake(
        db_session,
        conversation_id=workspace.conversation.id,
        selection=_selection(("hero", 2)),
        image_uploads=_workspace_uploads(),
        idempotency_key="turn-intake",
    )
    reservation = reserve_agent_turn(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        input_text="开始",
        input_asset_ids=[asset.id for asset in finalized.created_assets],
        idempotency_key="turn-after-intake",
    )
    assert reservation.created is True


def test_agent_product_workspace_idempotency_replays_and_rejects_payload_drift(
    configured_env: Path,
    db_session,
) -> None:
    kwargs = {
        "name": "幂等商品",
        "selection": _selection(("hero", 2), ("scene", 1)),
        "image_uploads": _workspace_uploads(),
        "idempotency_key": "workspace-stable-key",
    }
    first = create_agent_product_workspace(db_session, **kwargs)
    first_files = sorted(path.relative_to(configured_env) for path in _media_files(configured_env))
    replay = create_agent_product_workspace(db_session, **kwargs)

    assert replay.created is False
    assert replay.product.id == first.product.id
    assert replay.workflow_draft.id == first.workflow_draft.id
    assert replay.conversation.id == first.conversation.id
    assert [asset.id for asset in replay.created_assets] == [asset.id for asset in first.created_assets]
    assert sorted(path.relative_to(configured_env) for path in _media_files(configured_env)) == first_files
    assert db_session.scalar(select(func.count()).select_from(Product)) == 1

    with pytest.raises(ConflictError, match="相同 Idempotency-Key"):
        create_agent_product_workspace(db_session, **{**kwargs, "name": "漂移商品"})
    with pytest.raises(ConflictError, match="相同 Idempotency-Key"):
        create_agent_product_draft_workspace(
            db_session,
            name=kwargs["name"],
            idempotency_key=kwargs["idempotency_key"],
        )
    assert db_session.scalar(select(func.count()).select_from(Product)) == 1


@pytest.mark.parametrize("failure_model", [WorkflowDraft, AgentConversation])
def test_agent_product_workspace_flush_failures_rollback_database_and_storage(
    configured_env: Path,
    db_session,
    failure_model,
) -> None:
    def fail_target_flush(session, _flush_context, _instances) -> None:
        if any(isinstance(item, failure_model) for item in session.new):
            raise RuntimeError(f"injected {failure_model.__name__} flush failure")

    event.listen(db_session, "before_flush", fail_target_flush)
    try:
        with pytest.raises(RuntimeError, match="injected"):
            create_agent_product_workspace(
                db_session,
                name="故障注入商品",
                selection=_selection(("hero", 2)),
                image_uploads=_workspace_uploads(),
                idempotency_key=f"failure-{failure_model.__name__}",
            )
    finally:
        event.remove(db_session, "before_flush", fail_target_flush)

    assert db_session.scalar(select(func.count()).select_from(Product)) == 0
    assert db_session.scalar(select(func.count()).select_from(MediaObject)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraft)) == 0
    assert db_session.scalar(select(func.count()).select_from(AgentConversation)) == 0
    assert _media_files(configured_env) == []


def test_agent_product_workspace_commit_failure_rolls_back_database_and_storage(
    configured_env: Path,
    db_session,
) -> None:
    def fail_commit(_session) -> None:
        raise RuntimeError("injected commit failure")

    event.listen(db_session, "before_commit", fail_commit)
    try:
        with pytest.raises(RuntimeError, match="injected commit failure"):
            create_agent_product_workspace(
                db_session,
                name="提交故障商品",
                selection=_selection(("hero", 2)),
                image_uploads=_workspace_uploads(),
                idempotency_key="commit-failure",
            )
    finally:
        event.remove(db_session, "before_commit", fail_commit)

    assert db_session.scalar(select(func.count()).select_from(Product)) == 0
    assert db_session.scalar(select(func.count()).select_from(MediaObject)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraft)) == 0
    assert db_session.scalar(select(func.count()).select_from(AgentConversation)) == 0
    assert _media_files(configured_env) == []


def test_agent_product_workspace_storage_failure_compensates_prior_upload(
    configured_env: Path,
    db_session,
) -> None:
    class FailSecondMediaStorage(LocalStorage):
        def __init__(self, root: Path) -> None:
            super().__init__(root)
            self.calls = 0

        def save_media_image(self, media_id: str, filename: str, content: bytes) -> str:
            self.calls += 1
            if self.calls == 2:
                raise OSError("injected storage failure")
            return super().save_media_image(media_id, filename, content)

    with pytest.raises(OSError, match="injected storage failure"):
        create_agent_product_workspace(
            db_session,
            name="存储故障商品",
            selection=_selection(("hero", 2)),
            image_uploads=_workspace_uploads(),
            idempotency_key="storage-failure",
            storage=FailSecondMediaStorage(configured_env),
        )

    assert db_session.scalar(select(func.count()).select_from(Product)) == 0
    assert db_session.scalar(select(func.count()).select_from(MediaObject)) == 0
    assert _media_files(configured_env) == []


def test_agent_product_workspace_rejects_invalid_later_image_without_residue(
    configured_env: Path,
    db_session,
) -> None:
    with pytest.raises(BusinessValidationError, match="声明媒体类型"):
        create_agent_product_workspace(
            db_session,
            name="无效图片商品",
            selection=_selection(("hero", 2)),
            image_uploads=[
                (_make_demo_image_bytes(), "first.png", "image/png"),
                (_make_demo_image_bytes(), "wrong.jpg", "image/jpeg"),
            ],
            idempotency_key="invalid-image",
        )
    assert db_session.scalar(select(func.count()).select_from(Product)) == 0
    assert db_session.scalar(select(func.count()).select_from(MediaObject)) == 0
    assert _media_files(configured_env) == []


def test_agent_product_workspace_api_exposes_options_and_bounded_create(configured_env: Path) -> None:
    from productflow_backend.infrastructure.db.session import get_session_factory
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    options_response = client.get("/api/v2/agent-product-workspaces/options")
    assert options_response.status_code == 200
    options = options_response.json()
    assert options["schema_version"] == 1
    assert [item["key"] for item in options["image_types"]] == [
        item.key for item in AGENT_PRODUCT_IMAGE_TYPE_CATALOG
    ]
    assert options["limits"] == {
        "min_image_types": 1,
        "default_images_per_type": 2,
        "min_images_per_type": 1,
        "max_images_per_type": 6,
        "max_total_images": 30,
        "min_reference_images": 1,
        "max_reference_images": 6,
        "allowed_image_mime_types": ["image/png", "image/jpeg", "image/webp"],
    }

    selection = _selection(("hero", 2), ("dimensions", 1))
    request = {
        "data": {"name": "API Agent 商品", "selection": selection.model_dump_json()},
        "files": [
            ("images", ("front.png", _make_demo_image_bytes(), "image/png")),
            ("images", ("side.png", _make_demo_image_bytes(), "image/png")),
        ],
        "headers": {"Idempotency-Key": "api-agent-workspace-1"},
    }
    created_response = client.post("/api/v2/agent-product-workspaces", **request)
    assert created_response.status_code == 201, created_response.text
    created = created_response.json()
    assert set(created) == {"product", "created_assets", "workflow_draft", "conversation"}
    assert created["product"]["cover_image_asset_id"] is None
    assert [asset["original_filename"] for asset in created["created_assets"]] == [
        "front.png",
        "side.png",
    ]
    assert created["workflow_draft"]["current_revision"] is None
    assert created["workflow_draft"]["current_version"] == 0
    assert created["workflow_draft"]["revisions"] == []
    assert created["workflow_draft"]["intake"] == {
        "schema_version": 1,
        "image_types": [
            {"key": "hero", "quantity": 2, "order": 0},
            {"key": "dimensions", "quantity": 1, "order": 1},
        ],
        "reference_asset_ids": [asset["id"] for asset in created["created_assets"]],
    }
    assert created["conversation"]["harness_run_id"] == created["conversation"]["id"]

    replay_response = client.post("/api/v2/agent-product-workspaces", **request)
    assert replay_response.status_code == 201
    assert replay_response.json() == created

    session = get_session_factory()()
    try:
        assert session.scalar(select(func.count()).select_from(Product)) == 1
        assert session.scalar(select(func.count()).select_from(ProductWorkflow)) == 0
    finally:
        session.close()


def test_agent_product_workspace_api_supports_draft_resume_and_intake_finalization(
    configured_env: Path,
) -> None:
    from productflow_backend.infrastructure.db.session import get_session_factory
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    draft_response = client.post(
        "/api/v2/agent-product-workspaces/drafts",
        json={"name": "分阶段 API 商品"},
        headers={"Idempotency-Key": "api-draft-workspace-1"},
    )
    assert draft_response.status_code == 201, draft_response.text
    draft = draft_response.json()
    assert set(draft) == {
        "created",
        "intake_finalized",
        "product",
        "created_assets",
        "workflow_draft",
        "conversation",
    }
    assert draft["created"] is True
    assert draft["intake_finalized"] is False
    assert draft["created_assets"] == []
    assert draft["product"]["cover_image_asset_id"] is None
    assert draft["workflow_draft"]["current_version"] == 0
    assert draft["workflow_draft"]["intake"] is None

    replay_response = client.post(
        "/api/v2/agent-product-workspaces/drafts",
        json={"name": "分阶段 API 商品"},
        headers={"Idempotency-Key": "api-draft-workspace-1"},
    )
    assert replay_response.status_code == 201
    assert replay_response.json()["created"] is False
    assert replay_response.json()["conversation"]["id"] == draft["conversation"]["id"]

    restored_response = client.get(
        f"/api/v2/agent-product-workspaces/{draft['conversation']['id']}"
    )
    assert restored_response.status_code == 200
    assert restored_response.json()["created"] is False
    assert restored_response.json()["intake_finalized"] is False

    selection = _selection(("hero", 2), ("detail", 1))
    finalization_request = {
        "data": {"selection": selection.model_dump_json()},
        "files": [
            ("images", ("front.png", _make_demo_image_bytes(), "image/png")),
            ("images", ("detail.png", _make_demo_image_bytes(), "image/png")),
        ],
        "headers": {"Idempotency-Key": "api-intake-finalize-1"},
    }
    finalized_response = client.post(
        f"/api/v2/agent-product-workspaces/{draft['conversation']['id']}/intake",
        **finalization_request,
    )
    assert finalized_response.status_code == 200, finalized_response.text
    finalized = finalized_response.json()
    assert finalized["created"] is True
    assert finalized["intake_finalized"] is True
    assert [asset["original_filename"] for asset in finalized["created_assets"]] == [
        "front.png",
        "detail.png",
    ]
    assert finalized["product"]["cover_image_asset_id"] is None
    assert finalized["workflow_draft"]["current_version"] == 0
    assert finalized["workflow_draft"]["intake"]["reference_asset_ids"] == [
        asset["id"] for asset in finalized["created_assets"]
    ]

    finalization_replay = client.post(
        f"/api/v2/agent-product-workspaces/{draft['conversation']['id']}/intake",
        **finalization_request,
    )
    assert finalization_replay.status_code == 200
    assert finalization_replay.json()["created"] is False
    assert [asset["id"] for asset in finalization_replay.json()["created_assets"]] == [
        asset["id"] for asset in finalized["created_assets"]
    ]

    invalid_draft = client.post(
        "/api/v2/agent-product-workspaces/drafts",
        json={"name": "拒绝未知字段", "template_key": "unexpected"},
        headers={"Idempotency-Key": "strict-draft"},
    )
    assert invalid_draft.status_code == 422

    session = get_session_factory()()
    try:
        assert session.scalar(select(func.count()).select_from(Product)) == 1
        assert session.scalar(select(func.count()).select_from(ProductImageAsset)) == 2
        assert session.scalar(select(func.count()).select_from(ProductWorkflow)) == 0
    finally:
        session.close()


def test_agent_product_workspace_api_rejects_invalid_selection(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    response = client.post(
        "/api/v2/agent-product-workspaces",
        data={
            "name": "无效选择",
            "selection": json.dumps({"schema_version": 1, "image_types": []}),
        },
        files=[("images", ("front.png", _make_demo_image_bytes(), "image/png"))],
        headers={"Idempotency-Key": "invalid-selection"},
    )
    assert response.status_code == 400
    assert response.json() == {"detail": "图片类型选择不符合 AgentProductSelectionV1"}
