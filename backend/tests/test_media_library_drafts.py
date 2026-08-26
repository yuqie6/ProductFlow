from __future__ import annotations

from datetime import UTC, datetime

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from sqlalchemy import select
from test_media_library_organization import _asset

from productflow_backend.application.agent.control import synchronize_agent_turn_state
from productflow_backend.application.agent.sessions import create_agent_session
from productflow_backend.application.agent.tasks import create_agent_task
from productflow_backend.application.agent.turn_projection import bind_harness_turn, reserve_agent_turn
from productflow_backend.application.media_library.drafts import (
    append_library_organization_draft_revision,
    confirm_library_organization_draft_revision,
    get_library_organization_draft_or_raise,
)
from productflow_backend.application.media_library.organization import move_media_library_assets
from productflow_backend.application.media_library.service import save_media_library_asset_from_product
from productflow_backend.application.products import create_canonical_product
from productflow_backend.domain.enums import AgentConversationStatus, AgentTurnStatus, LibraryOrganizationDraftStatus
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.agent_service import AgentServiceArtifact, AgentServiceTurnState
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    WorkflowGraph,
    WorkflowMediaLibraryAsset,
)
from productflow_backend.presentation.api import create_app


def _before(asset) -> dict[str, object]:
    return {
        "revision": asset.revision,
        "display_name": asset.display_name,
        "folder_id": asset.folder_id,
        "tag_names": [
            assignment.tag.name
            for assignment in asset.tag_assignments
            if assignment.tag is not None
        ],
        "is_archived": asset.is_archived,
    }


def _rename_payload(asset, *, target_name: str = "整理后的主图") -> dict[str, object]:
    return {
        "schema_version": 1,
        "confirmation_summary": "整理一张素材的名称",
        "operations": [
            {
                "operation": "rename",
                "asset_id": asset.id,
                "expected_revision": asset.revision,
                "before": _before(asset),
                "target": {"display_name": target_name},
                "reason": "统一场景图命名",
            }
        ],
    }


def _asset_with_workflows(db_session, workflow_titles: list[str]):
    product = create_canonical_product(
        db_session,
        name="素材关联测试商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "asset.png", "image/png")],
    )
    asset = save_media_library_asset_from_product(
        db_session,
        product_image_asset_id=product.image_assets[0].id,
    ).asset
    workflows = [
        WorkflowGraph(
            product_id=product.id,
            title=title,
            active=index == 0,
            revision=index + 1,
        )
        for index, title in enumerate(workflow_titles)
    ]
    db_session.add_all(workflows)
    db_session.commit()
    return asset, workflows


def _link_payload(asset, workflow, *, expected_linked: bool = False) -> dict[str, object]:
    return {
        "schema_version": 1,
        "confirmation_summary": f"关联素材到工作流 {workflow.title}",
        "operations": [
            {
                "operation": "link_workflow",
                "asset_id": asset.id,
                "expected_revision": asset.revision,
                "before": _before(asset),
                "target": {
                    "workflow_id": workflow.id,
                    "workflow_title": workflow.title,
                    "expected_workflow_revision": workflow.revision,
                    "expected_linked": expected_linked,
                },
                "reason": "让工作流使用全局素材",
            }
        ],
    }


def _global_conversation(db_session):
    agent_session = create_agent_session(db_session, title="素材整理测试")
    return db_session.scalar(
        select(AgentConversation).where(AgentConversation.session_id == agent_session.id)
    )


def test_library_organization_draft_publish_has_no_media_side_effect(db_session) -> None:
    asset = _asset(db_session)
    conversation = _global_conversation(db_session)

    draft = append_library_organization_draft_revision(
        db_session,
        conversation_id=conversation.id,
        expected_draft_version=0,
        payload=_rename_payload(asset),
        source_turn_id="turn-library-1",
        source_artifact_step_id="artifact-library-1",
    )

    assert draft.status == LibraryOrganizationDraftStatus.AWAITING_CONFIRMATION
    assert draft.current_revision is not None
    assert draft.current_revision.version == 1
    db_session.refresh(asset)
    assert asset.display_name != "整理后的主图"
    assert asset.revision == 1


def test_global_turn_only_enters_confirmation_when_optional_draft_is_returned(db_session) -> None:
    asset = _asset(db_session)
    conversation = _global_conversation(db_session)
    task = create_agent_task(
        db_session,
        session_id=conversation.session_id,
        conversation_id=conversation.id,
        title="整理测试任务",
        goal="整理最近生成的素材",
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=None,
        conversation_id=conversation.id,
        input_text="整理最近生成的素材",
        input_asset_ids=[],
        idempotency_key="global-draft-turn",
        task_id=task.id,
    ).projection
    projection = bind_harness_turn(
        db_session,
        product_id=None,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="global-draft-harness-turn",
        status=AgentTurnStatus.RUNNING,
    )
    synced = synchronize_agent_turn_state(
        db_session,
        product_id=None,
        conversation_id=conversation.id,
        projection_id=projection.id,
        state=AgentServiceTurnState(
            api_version="v1alpha1",
            run_id=task.harness_run_id,
            turn_id="global-draft-harness-turn",
            status=AgentTurnStatus.SUCCEEDED,
            output="已准备整理建议",
            artifact=AgentServiceArtifact(
                name="propose_library_organization_draft",
                value=_rename_payload(asset),
                step_id="global-draft-step",
            ),
            created_at=datetime.now(UTC),
            updated_at=datetime.now(UTC),
            finished_at=datetime.now(UTC),
        ),
    )
    assert synced.status == AgentTurnStatus.AWAITING_CONFIRMATION
    assert synced.library_organization_draft_revision_id is not None
    assert synced.workflow_draft_revision_id is None
    assert conversation.status == AgentConversationStatus.AWAITING_CONFIRMATION
    db_session.refresh(task)
    assert task.status.value == "awaiting_confirmation"

    draft = get_library_organization_draft_or_raise(db_session, conversation_id=conversation.id)

    pending_query_projection = reserve_agent_turn(
        db_session,
        product_id=None,
        conversation_id=conversation.id,
        input_text="查询素材数量",
        input_asset_ids=[],
        idempotency_key="global-query-before-confirm",
    ).projection
    pending_query_projection = bind_harness_turn(
        db_session,
        product_id=None,
        conversation_id=conversation.id,
        projection_id=pending_query_projection.id,
        harness_turn_id="global-query-before-confirm-harness-turn",
        status=AgentTurnStatus.RUNNING,
    )
    pending_query_synced = synchronize_agent_turn_state(
        db_session,
        product_id=None,
        conversation_id=conversation.id,
        projection_id=pending_query_projection.id,
        state=AgentServiceTurnState(
            api_version="v1alpha1",
            run_id=conversation.harness_run_id,
            turn_id="global-query-before-confirm-harness-turn",
            status=AgentTurnStatus.SUCCEEDED,
            output="共有一张素材",
            created_at=datetime.now(UTC),
            updated_at=datetime.now(UTC),
            finished_at=datetime.now(UTC),
        ),
    )
    assert pending_query_synced.status == AgentTurnStatus.SUCCEEDED
    assert conversation.status == AgentConversationStatus.AWAITING_CONFIRMATION

    confirm_library_organization_draft_revision(
        db_session,
        draft_id=draft.id,
        expected_draft_version=1,
        idempotency_key="global-draft-confirm",
    )
    db_session.refresh(task)
    assert task.status.value == "succeeded"

    query_conversation = _global_conversation(db_session)
    query_projection = reserve_agent_turn(
        db_session,
        product_id=None,
        conversation_id=query_conversation.id,
        input_text="查询素材数量",
        input_asset_ids=[],
        idempotency_key="global-query-turn",
    ).projection
    query_projection = bind_harness_turn(
        db_session,
        product_id=None,
        conversation_id=query_conversation.id,
        projection_id=query_projection.id,
        harness_turn_id="global-query-harness-turn",
        status=AgentTurnStatus.RUNNING,
    )
    query_synced = synchronize_agent_turn_state(
        db_session,
        product_id=None,
        conversation_id=query_conversation.id,
        projection_id=query_projection.id,
        state=AgentServiceTurnState(
            api_version="v1alpha1",
            run_id=query_conversation.harness_run_id,
            turn_id="global-query-harness-turn",
            status=AgentTurnStatus.SUCCEEDED,
            output="共有一张素材",
            created_at=datetime.now(UTC),
            updated_at=datetime.now(UTC),
            finished_at=datetime.now(UTC),
        ),
    )
    assert query_synced.status == AgentTurnStatus.SUCCEEDED
    assert query_synced.library_organization_draft_revision_id is None
    assert query_conversation.status == AgentConversationStatus.COMPLETED


def test_library_organization_draft_confirmation_is_idempotent(db_session) -> None:
    asset = _asset(db_session)
    conversation = _global_conversation(db_session)
    append_library_organization_draft_revision(
        db_session,
        conversation_id=conversation.id,
        expected_draft_version=0,
        payload=_rename_payload(asset),
        source_turn_id="turn-library-2",
        source_artifact_step_id="artifact-library-2",
    )
    draft = get_library_organization_draft_or_raise(db_session, conversation_id=conversation.id)

    confirmed = confirm_library_organization_draft_revision(
        db_session,
        draft_id=draft.id,
        expected_draft_version=1,
        idempotency_key="confirm-library-1",
    )
    assert confirmed.status == LibraryOrganizationDraftStatus.CONFIRMED
    assert confirmed.confirmation_result_json is not None
    db_session.refresh(asset)
    assert asset.display_name == "整理后的主图"
    assert asset.revision == 2

    replayed = confirm_library_organization_draft_revision(
        db_session,
        draft_id=draft.id,
        expected_draft_version=1,
        idempotency_key="confirm-library-1",
    )
    db_session.refresh(asset)
    assert replayed.status == LibraryOrganizationDraftStatus.CONFIRMED
    assert asset.revision == 2


def test_library_organization_draft_rejects_stale_revision_without_partial_apply(db_session) -> None:
    first = _asset(db_session)
    second = _asset(db_session)
    conversation = _global_conversation(db_session)
    payload = {
        "schema_version": 1,
        "confirmation_summary": "整理两张素材的名称",
        "operations": [
            {
                "operation": "rename",
                "asset_id": first.id,
                "expected_revision": first.revision,
                "before": _before(first),
                "target": {"display_name": "第一张整理图"},
                "reason": "统一命名",
            },
            {
                "operation": "rename",
                "asset_id": second.id,
                "expected_revision": second.revision,
                "before": _before(second),
                "target": {"display_name": "第二张整理图"},
                "reason": "统一命名",
            },
        ],
    }
    append_library_organization_draft_revision(
        db_session,
        conversation_id=conversation.id,
        expected_draft_version=0,
        payload=payload,
        source_turn_id="turn-library-3",
        source_artifact_step_id="artifact-library-3",
    )
    move_media_library_assets(
        db_session,
        asset_ids=[second.id],
        folder_id=None,
        expected_revision={second.id: second.revision},
    )
    draft = get_library_organization_draft_or_raise(db_session, conversation_id=conversation.id)

    with pytest.raises(ConflictError, match="当前状态已变化"):
        confirm_library_organization_draft_revision(
            db_session,
            draft_id=draft.id,
            expected_draft_version=1,
            idempotency_key="confirm-library-2",
        )

    db_session.refresh(first)
    assert first.display_name != "第一张整理图"


def test_library_organization_draft_can_link_asset_to_workflow_without_copying_media(db_session) -> None:
    asset, [workflow] = _asset_with_workflows(db_session, ["场景图工作流"])
    conversation = _global_conversation(db_session)
    append_library_organization_draft_revision(
        db_session,
        conversation_id=conversation.id,
        expected_draft_version=0,
        payload=_link_payload(asset, workflow),
        source_turn_id="turn-library-link-1",
        source_artifact_step_id="artifact-library-link-1",
    )

    assert db_session.get(WorkflowMediaLibraryAsset, (workflow.id, asset.id)) is None
    confirmed = confirm_library_organization_draft_revision(
        db_session,
        draft_id=get_library_organization_draft_or_raise(db_session, conversation_id=conversation.id).id,
        expected_draft_version=1,
        idempotency_key="confirm-library-link-1",
    )

    link = db_session.get(WorkflowMediaLibraryAsset, (workflow.id, asset.id))
    assert link is not None
    assert confirmed.confirmation_result_json["workflow_links"] == [
        {
            "asset_id": asset.id,
            "product_id": workflow.product_id,
            "workflow_id": workflow.id,
            "workflow_title": workflow.title,
            "linked": True,
            "changed": True,
        }
    ]


def test_library_organization_draft_can_link_one_global_asset_to_multiple_workflows(db_session) -> None:
    asset, workflows = _asset_with_workflows(db_session, ["主图工作流", "详情页工作流"])
    conversation = _global_conversation(db_session)
    payload = _link_payload(asset, workflows[0])
    payload["operations"].append(_link_payload(asset, workflows[1])["operations"][0])
    append_library_organization_draft_revision(
        db_session,
        conversation_id=conversation.id,
        expected_draft_version=0,
        payload=payload,
        source_turn_id="turn-library-link-2",
        source_artifact_step_id="artifact-library-link-2",
    )

    confirm_library_organization_draft_revision(
        db_session,
        draft_id=get_library_organization_draft_or_raise(db_session, conversation_id=conversation.id).id,
        expected_draft_version=1,
        idempotency_key="confirm-library-link-2",
    )

    assert db_session.get(WorkflowMediaLibraryAsset, (workflows[0].id, asset.id)) is not None
    assert db_session.get(WorkflowMediaLibraryAsset, (workflows[1].id, asset.id)) is not None


def test_library_organization_draft_rechecks_workflow_link_state_before_apply(db_session) -> None:
    asset, [workflow] = _asset_with_workflows(db_session, ["并发关联工作流"])
    conversation = _global_conversation(db_session)
    payload = _link_payload(asset, workflow)
    payload["operations"].append(
        {
            "operation": "rename",
            "asset_id": asset.id,
            "expected_revision": asset.revision,
            "before": _before(asset),
            "target": {"display_name": "不应被应用的名称"},
            "reason": "验证 Draft 失败时整体回滚",
        }
    )
    append_library_organization_draft_revision(
        db_session,
        conversation_id=conversation.id,
        expected_draft_version=0,
        payload=payload,
        source_turn_id="turn-library-link-3",
        source_artifact_step_id="artifact-library-link-3",
    )
    db_session.add(
        WorkflowMediaLibraryAsset(
            workflow_id=workflow.id,
            media_library_asset_id=asset.id,
        )
    )
    db_session.commit()

    draft = get_library_organization_draft_or_raise(db_session, conversation_id=conversation.id)
    with pytest.raises(ConflictError, match="关联状态已变化"):
        confirm_library_organization_draft_revision(
            db_session,
            draft_id=draft.id,
            expected_draft_version=1,
            idempotency_key="confirm-library-link-3",
        )

    db_session.refresh(asset)
    assert asset.display_name != "不应被应用的名称"
    db_session.refresh(draft)
    assert draft.status == LibraryOrganizationDraftStatus.AWAITING_CONFIRMATION


def test_library_organization_draft_rejects_duplicate_asset_operations() -> None:
    from productflow_backend.application.media_library.draft_contracts import (
        parse_library_organization_draft_payload,
    )

    before = {
        "revision": 1,
        "display_name": "asset.png",
        "folder_id": None,
        "tag_names": [],
        "is_archived": False,
    }
    operation = {
        "operation": "rename",
        "asset_id": "asset-1",
        "expected_revision": 1,
        "before": before,
        "target": {"display_name": "renamed.png"},
        "reason": "命名",
    }
    with pytest.raises(ValueError, match="只能出现一次"):
        parse_library_organization_draft_payload(
            {
                "schema_version": 1,
                "confirmation_summary": "重复素材",
                "operations": [operation, operation],
            }
        )


def test_library_organization_draft_confirm_uses_existing_asset_lineage(db_session) -> None:
    product = create_canonical_product(
        db_session,
        name="素材 Draft 来源商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "source.png", "image/png")],
    )
    asset = save_media_library_asset_from_product(
        db_session,
        product_image_asset_id=product.image_assets[0].id,
    ).asset
    conversation = _global_conversation(db_session)
    append_library_organization_draft_revision(
        db_session,
        conversation_id=conversation.id,
        expected_draft_version=0,
        payload=_rename_payload(asset),
        source_turn_id="turn-library-4",
        source_artifact_step_id="artifact-library-4",
    )

    confirmed = confirm_library_organization_draft_revision(
        db_session,
        draft_id=get_library_organization_draft_or_raise(db_session, conversation_id=conversation.id).id,
        expected_draft_version=1,
        idempotency_key="confirm-library-3",
    )

    assert confirmed.confirmation_result_json["assets"][0]["asset_id"] == asset.id
    assert asset.source_product_asset is not None


def test_global_library_organization_draft_api_exposes_review_and_confirmation(configured_env) -> None:
    from productflow_backend.infrastructure.db.session import get_session_factory

    factory = get_session_factory()
    with factory() as session:
        asset = _asset(session)
        conversation = _global_conversation(session)
        append_library_organization_draft_revision(
            session,
            conversation_id=conversation.id,
            expected_draft_version=0,
            payload=_rename_payload(asset),
            source_turn_id="api-turn-1",
            source_artifact_step_id="api-step-1",
        )
        conversation_id = conversation.id

    client = TestClient(create_app())
    _login(client)

    review = client.get(
        f"/api/v2/agent-conversations/{conversation_id}/library-organization-draft"
    )
    assert review.status_code == 200, review.text
    assert review.json()["status"] == "awaiting_confirmation"
    assert review.json()["current_revision"]["version"] == 1

    confirmed = client.post(
        f"/api/v2/agent-conversations/{conversation_id}/library-organization-draft/confirm",
        json={"expected_draft_version": 1, "idempotency_key": "api-confirm-1"},
    )
    assert confirmed.status_code == 200, confirmed.text
    assert confirmed.json()["status"] == "confirmed"
