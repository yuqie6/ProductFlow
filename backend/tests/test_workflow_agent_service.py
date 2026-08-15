from __future__ import annotations

from datetime import UTC, datetime, timedelta
from pathlib import Path

import pytest
import sqlalchemy as sa
from alembic.config import Config
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from sqlalchemy import event, func, select
from workflow_draft_helpers import make_workflow_draft_payload

from alembic import command
from productflow_backend.application import agent_control
from productflow_backend.application.agent_control import synchronize_agent_turn_state
from productflow_backend.application.agent_conversations import (
    attach_agent_workflow_draft_artifact,
    bind_harness_turn,
    create_agent_conversation,
    list_agent_turn_page,
    project_agent_turn_state,
    reserve_agent_turn,
)
from productflow_backend.application.agent_cutover import inspect_agent_catalog_cutover
from productflow_backend.application.agent_product_intake import AgentProductSelectionV1
from productflow_backend.application.agent_product_workspaces import create_agent_product_workspace
from productflow_backend.application.agent_sync import (
    execute_agent_turn_sync,
    recover_unfinished_agent_turn_syncs,
)
from productflow_backend.application.agent_tools import (
    apply_agent_asset_move,
    apply_agent_asset_rename,
    apply_agent_folder_create,
    apply_agent_folder_rename,
    get_agent_contract,
    get_agent_product_context,
    inspect_agent_product_assets,
    list_agent_product_assets,
    prepare_agent_asset_move,
    prepare_agent_asset_rename,
    prepare_agent_folder_create,
    prepare_agent_folder_rename,
    read_agent_product_asset_content,
    reconcile_agent_asset_move,
    reconcile_agent_asset_rename,
    reconcile_agent_folder_create,
    reconcile_agent_folder_rename,
)
from productflow_backend.application.gallery_mutations import rename_gallery_asset
from productflow_backend.application.use_cases import create_canonical_product
from productflow_backend.application.workflow_drafts.service import (
    append_workflow_draft_revision,
    create_workflow_draft,
)
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import (
    AgentConversationStatus,
    AgentTurnStatus,
    MediaVerificationStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.agent_service import (
    AgentServiceArtifact,
    AgentServiceQuestion,
    AgentServiceQuestionOption,
    AgentServiceRequestError,
    AgentServiceTurnState,
)
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentToolMutation,
    AgentTurnProjection,
    WorkflowDraft,
    WorkflowDraftRevision,
    new_id,
)


def _create_product_and_draft(
    db_session,
    *,
    name: str = "Agent 测试商品",
    image_count: int = 1,
):
    product = create_canonical_product(
        db_session,
        name=name,
        category="工业收纳",
        price="299.00",
        source_note="橙蓝色刀具收纳套装",
        image_uploads=[
            (_make_demo_image_bytes(), f"{name}-{index}.png", "image/png")
            for index in range(image_count)
        ],
    )
    asset = product.image_assets[0]
    payload = make_workflow_draft_payload(reference_asset_id=asset.id)
    draft = create_workflow_draft(
        db_session,
        product_id=product.id,
        payload=payload,
        ready_for_confirmation=False,
    )
    return product, asset, draft, payload


def _create_agent_first_workspace(db_session):
    selection = AgentProductSelectionV1.model_validate(
        {
            "schema_version": 1,
            "image_types": [
                {"key": "hero", "quantity": 2, "order": 0},
                {"key": "detail", "quantity": 1, "order": 1},
            ],
        }
    )
    return create_agent_product_workspace(
        db_session,
        name="Agent-first 测试商品",
        selection=selection,
        image_uploads=[(_make_demo_image_bytes(), "agent-first.png", "image/png")],
        idempotency_key="agent-first-workspace",
    )


def test_conversation_and_turn_reservation_are_product_scoped_and_idempotent(db_session) -> None:
    product, asset, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    repeated_conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    assert repeated_conversation.id == conversation.id
    assert conversation.harness_run_id == conversation.id

    reservation = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="  请先核对商品资料  ",
        input_asset_ids=[asset.id],
        idempotency_key="turn-key-1",
    )
    assert reservation.created is True
    assert reservation.projection.input_text == "请先核对商品资料"
    assert reservation.projection.input_asset_ids_json == [asset.id]
    assert len(reservation.projection.request_hash) == 64

    repeated = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="请先核对商品资料",
        input_asset_ids=[asset.id],
        idempotency_key="turn-key-1",
    )
    assert repeated.created is False
    assert repeated.projection.id == reservation.projection.id
    assert db_session.scalar(select(func.count()).select_from(AgentTurnProjection)) == 1

    with pytest.raises(ConflictError, match="不同"):
        reserve_agent_turn(
            db_session,
            product_id=product.id,
            conversation_id=conversation.id,
            input_text="换成另一条请求",
            input_asset_ids=[asset.id],
            idempotency_key="turn-key-1",
        )

    other_product, other_asset, _, _ = _create_product_and_draft(db_session, name="其他商品")
    with pytest.raises(BusinessValidationError, match="当前商品"):
        reserve_agent_turn(
            db_session,
            product_id=product.id,
            conversation_id=conversation.id,
            input_text="查看跨商品图片",
            input_asset_ids=[other_asset.id],
            idempotency_key="turn-key-cross-product",
        )
    with pytest.raises(NotFoundError):
        reserve_agent_turn(
            db_session,
            product_id=other_product.id,
            conversation_id=conversation.id,
            input_text="尝试切换商品作用域",
            input_asset_ids=[],
            idempotency_key="turn-key-cross-conversation",
        )


def test_agent_turn_history_uses_bounded_reverse_keyset_pages(db_session) -> None:
    product, _, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    base_time = datetime(2026, 8, 14, 8, 0, tzinfo=UTC)
    db_session.add_all(
        [
            AgentTurnProjection(
                id=new_id(),
                conversation_id=conversation.id,
                idempotency_key=f"page-key-{index}",
                request_hash=f"{index:064x}",
                input_text=f"turn-{index:02d}",
                input_asset_ids_json=[],
                status=AgentTurnStatus.SUCCEEDED,
                output_text=f"output-{index:02d}",
                finished_at=base_time + timedelta(seconds=index),
                created_at=base_time + timedelta(seconds=index),
                updated_at=base_time + timedelta(seconds=index),
            )
            for index in range(55)
        ]
    )
    db_session.commit()

    statements: list[str] = []

    def capture_statement(_connection, _cursor, statement, _parameters, _context, _executemany) -> None:
        if "FROM agent_turn_projections" in statement:
            statements.append(statement)

    assert db_session.bind is not None
    event.listen(db_session.bind, "before_cursor_execute", capture_statement)
    try:
        newest = list_agent_turn_page(
            db_session,
            product_id=product.id,
            conversation_id=conversation.id,
        )
    finally:
        event.remove(db_session.bind, "before_cursor_execute", capture_statement)
    assert [item.input_text for item in newest.items] == [f"turn-{index:02d}" for index in range(35, 55)]
    assert newest.next_cursor is not None
    assert any("LIMIT" in statement.upper() for statement in statements)

    middle = list_agent_turn_page(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        after=newest.next_cursor,
    )
    assert [item.input_text for item in middle.items] == [f"turn-{index:02d}" for index in range(15, 35)]
    assert middle.next_cursor is not None
    oldest = list_agent_turn_page(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        after=middle.next_cursor,
    )
    assert [item.input_text for item in oldest.items] == [f"turn-{index:02d}" for index in range(15)]
    assert oldest.next_cursor is None
    assert [item.input_text for item in [*oldest.items, *middle.items, *newest.items]] == [
        f"turn-{index:02d}" for index in range(55)
    ]

    other_product, _, other_draft, _ = _create_product_and_draft(db_session, name="分页其他商品")
    other_conversation = create_agent_conversation(
        db_session,
        product_id=other_product.id,
        workflow_draft_id=other_draft.id,
    )
    with pytest.raises(BusinessValidationError, match="不匹配"):
        list_agent_turn_page(
            db_session,
            product_id=other_product.id,
            conversation_id=other_conversation.id,
            after=newest.next_cursor,
        )
    with pytest.raises(BusinessValidationError, match="cursor 无效"):
        list_agent_turn_page(
            db_session,
            product_id=product.id,
            conversation_id=conversation.id,
            after="not-a-cursor",
        )
    with pytest.raises(BusinessValidationError, match="limit"):
        list_agent_turn_page(
            db_session,
            product_id=product.id,
            conversation_id=conversation.id,
            limit=51,
        )


@pytest.mark.parametrize(
    ("terminal_status", "conversation_status"),
    [
        (AgentTurnStatus.SUCCEEDED, AgentConversationStatus.COMPLETED),
        (AgentTurnStatus.FAILED, AgentConversationStatus.FAILED),
        (AgentTurnStatus.CANCELED, AgentConversationStatus.CANCELED),
        (AgentTurnStatus.UNKNOWN, AgentConversationStatus.UNKNOWN),
    ],
)
def test_terminal_turn_statuses_allow_continuing_the_same_conversation(
    db_session,
    terminal_status: AgentTurnStatus,
    conversation_status: AgentConversationStatus,
) -> None:
    product, _, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    harness_run_id = conversation.harness_run_id
    first = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="执行第一轮",
        input_asset_ids=[],
        idempotency_key=f"first-{terminal_status.value}",
    ).projection
    first = bind_harness_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=first.id,
        harness_turn_id=f"harness-{terminal_status.value}",
        status=AgentTurnStatus.RUNNING,
    )
    first = project_agent_turn_state(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=first.id,
        harness_turn_id=first.harness_turn_id or "",
        status=terminal_status,
        output_text=None,
        error_text="终态测试",
        question_json=None,
        finished_at=datetime.now(UTC),
    )
    assert first.conversation.status == conversation_status

    second = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="继续下一轮",
        input_asset_ids=[],
        idempotency_key=f"second-{terminal_status.value}",
    )
    assert second.created is True
    assert second.projection.conversation.status == AgentConversationStatus.COLLECTING
    assert second.projection.conversation.harness_run_id == harness_run_id


@pytest.mark.parametrize(
    ("terminal_status", "safe_error"),
    [
        (
            AgentTurnStatus.FAILED,
            "Agent 生成失败，请重试；持续失败请检查 Agent 供应商配置",
        ),
        (AgentTurnStatus.UNKNOWN, "Agent 执行状态不明确，请稍后重试"),
    ],
)
def test_agent_terminal_provider_errors_are_not_projected_to_the_browser(
    db_session,
    monkeypatch: pytest.MonkeyPatch,
    terminal_status: AgentTurnStatus,
    safe_error: str,
) -> None:
    product, _, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="生成工作流",
        input_asset_ids=[],
        idempotency_key=f"provider-error-{terminal_status.value}",
    ).projection
    projection = bind_harness_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id=f"harness-{terminal_status.value}",
        status=AgentTurnStatus.RUNNING,
    )
    raw_error = "provider returned 400 with internal request details"
    warning_calls: list[tuple[str, tuple[object, ...]]] = []
    monkeypatch.setattr(
        agent_control.logger,
        "warning",
        lambda message, *args: warning_calls.append((message, args)),
    )

    projected = synchronize_agent_turn_state(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        state=AgentServiceTurnState(
            api_version="v1alpha1",
            run_id=conversation.harness_run_id,
            turn_id=projection.harness_turn_id or "",
            status=terminal_status,
            error=raw_error,
            created_at=datetime.now(UTC),
            updated_at=datetime.now(UTC),
            finished_at=datetime.now(UTC),
        ),
    )

    assert projected.error_text == safe_error
    assert raw_error not in projected.error_text
    assert len(warning_calls) == 1
    warning_message, warning_args = warning_calls[0]
    assert raw_error not in warning_message
    assert raw_error not in warning_args
    assert warning_args == (conversation.harness_run_id, projection.harness_turn_id)


def test_agent_first_version_zero_context_and_first_artifact_are_replayable(db_session) -> None:
    workspace = _create_agent_first_workspace(db_session)
    product = workspace.product
    asset = workspace.created_assets[0]
    draft = workspace.workflow_draft
    conversation = workspace.conversation

    contract = get_agent_contract(db_session, conversation.id)
    assert contract["current_draft_version"] == 0
    assert "不能静默改写" in contract["system_prompt"]
    assert "不能臆造" in contract["system_prompt"]
    context = get_agent_product_context(db_session, conversation.id)
    assert context["workflow_draft"] == {
        "id": draft.id,
        "status": "collecting",
        "version": 0,
        "payload": None,
        "intake": {
            "schema_version": 1,
            "image_types": [
                {"key": "hero", "quantity": 2, "order": 0},
                {"key": "detail", "quantity": 1, "order": 1},
            ],
            "reference_asset_ids": [asset.id],
        },
    }

    projection = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="请根据 intake 完善工作流",
        input_asset_ids=[asset.id],
        idempotency_key="agent-first-artifact-turn",
    ).projection
    projection = bind_harness_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-agent-first",
        status=AgentTurnStatus.RUNNING,
    )
    projection = project_agent_turn_state(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-agent-first",
        status=AgentTurnStatus.AWAITING_CONFIRMATION,
        output_text="已准备结构化草案",
        error_text=None,
        question_json=None,
        finished_at=datetime.now(UTC),
    )
    artifact = make_workflow_draft_payload(reference_asset_id=asset.id)
    synced = attach_agent_workflow_draft_artifact(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-agent-first",
        artifact_name="propose_workflow_draft",
        artifact_step_id="agent-first-artifact",
        artifact_value=artifact,
    )
    revision_id = synced.workflow_draft_revision_id
    assert revision_id is not None
    persisted_draft = db_session.get(WorkflowDraft, draft.id)
    assert persisted_draft is not None
    db_session.refresh(persisted_draft)
    assert persisted_draft.current_revision is not None
    assert persisted_draft.current_revision.version == 1

    replay = attach_agent_workflow_draft_artifact(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-agent-first",
        artifact_name="propose_workflow_draft",
        artifact_step_id="agent-first-artifact",
        artifact_value=artifact,
    )
    assert replay.workflow_draft_revision_id == revision_id
    assert db_session.scalar(
        select(func.count()).select_from(WorkflowDraftRevision).where(WorkflowDraftRevision.draft_id == draft.id)
    ) == 1
    with pytest.raises(ConflictError, match="version 已变化"):
        append_workflow_draft_revision(
            db_session,
            product_id=product.id,
            draft_id=draft.id,
            expected_draft_version=0,
            payload={**artifact, "title": "并发旧版本草案"},
            ready_for_confirmation=True,
            source_turn_id="stale-turn",
            source_artifact_step_id="stale-artifact",
        )


def test_turn_projection_and_artifact_sync_converge_without_storing_artifact_body(db_session) -> None:
    product, asset, draft, payload = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="生成可确认的工作流草案",
        input_asset_ids=[asset.id],
        idempotency_key="artifact-turn",
    ).projection
    projection = bind_harness_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-turn-1",
        status=AgentTurnStatus.RUNNING,
    )
    assert projection.status == AgentTurnStatus.RUNNING

    projection = project_agent_turn_state(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-turn-1",
        status=AgentTurnStatus.AWAITING_CONFIRMATION,
        output_text="草案已生成，请确认。",
        error_text=None,
        question_json=None,
        finished_at=datetime.now(UTC),
    )
    assert projection.conversation.status == AgentConversationStatus.AWAITING_CONFIRMATION

    projection = attach_agent_workflow_draft_artifact(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-turn-1",
        artifact_name="propose_workflow_draft",
        artifact_step_id="artifact-step-1",
        artifact_value=payload,
    )
    revision_id = projection.workflow_draft_revision_id
    assert revision_id is not None
    assert projection.artifact_name == "propose_workflow_draft"
    assert projection.artifact_step_id == "artifact-step-1"
    assert not hasattr(projection, "artifact_value")
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftRevision)) == 2

    repeated = attach_agent_workflow_draft_artifact(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-turn-1",
        artifact_name="propose_workflow_draft",
        artifact_step_id="artifact-step-1",
        artifact_value=payload,
    )
    assert repeated.workflow_draft_revision_id == revision_id
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftRevision)) == 2

    changed_payload = {**payload, "confirmation_summary": "内容不同但仍符合 schema 的草案"}
    with pytest.raises(ConflictError, match="不能写入不同"):
        attach_agent_workflow_draft_artifact(
            db_session,
            product_id=product.id,
            conversation_id=conversation.id,
            projection_id=projection.id,
            harness_turn_id="harness-turn-1",
            artifact_name="propose_workflow_draft",
            artifact_step_id="artifact-step-1",
            artifact_value=changed_payload,
        )


def test_agent_read_tools_are_bounded_and_rename_is_reconcilable(db_session) -> None:
    product, asset, draft, _ = _create_product_and_draft(db_session, image_count=3)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )

    contract = get_agent_contract(db_session, conversation.id)
    assert contract["conversation_id"] == conversation.id
    assert contract["product_id"] == product.id
    assert contract["workflow_draft_id"] == draft.id
    assert contract["current_draft_version"] == 1
    workflow_schema = contract["workflow_draft_schema"]
    assert workflow_schema["additionalProperties"] is False
    delivery_schema = workflow_schema["$defs"]["DeliverySpec"]
    assert delivery_schema["required"] == list(delivery_schema["properties"])
    assert "background_color" in delivery_schema["required"]
    assert "oneOf" not in workflow_schema["properties"]["nodes"]["items"]
    assert "anyOf" in workflow_schema["properties"]["nodes"]["items"]
    assert contract["tool_contract_version"] == 2

    context = get_agent_product_context(db_session, conversation.id)
    assert context["product"]["name"] == product.name
    assert context["workflow_draft"]["version"] == 1
    assert "storage_path" not in str(context)

    first_page = list_agent_product_assets(
        db_session,
        conversation_id=conversation.id,
        limit=1,
    )
    assert len(first_page.items) == 1
    assert first_page.next_cursor is not None
    assert "storage_path" not in first_page.items[0]
    assert "url" not in first_page.items[0]
    second_page = list_agent_product_assets(
        db_session,
        conversation_id=conversation.id,
        after=first_page.next_cursor,
        limit=1,
    )
    assert second_page.items[0]["id"] != first_page.items[0]["id"]

    inspected = inspect_agent_product_assets(
        db_session,
        conversation_id=conversation.id,
        asset_ids=[asset.id],
    )
    assert inspected[0]["id"] == asset.id
    assert inspected[0]["mime_type"] == "image/png"
    content = read_agent_product_asset_content(
        db_session,
        conversation_id=conversation.id,
        asset_id=asset.id,
    )
    assert content.content == _make_demo_image_bytes()
    assert content.media_type == "image/png"

    prepared = prepare_agent_asset_rename(
        db_session,
        conversation_id=conversation.id,
        asset_id=asset.id,
        target_display_name="  商品正面参考图  ",
    )
    before = reconcile_agent_asset_rename(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="rename-1",
        asset_id=prepared.asset_id,
        expected_display_name=prepared.expected_display_name,
        target_display_name=prepared.target_display_name,
    )
    assert before.state == "not_applied"

    renamed = apply_agent_asset_rename(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="rename-1",
        asset_id=prepared.asset_id,
        expected_display_name=prepared.expected_display_name,
        target_display_name=prepared.target_display_name,
    )
    assert renamed == {
        "asset_id": asset.id,
        "display_name": "商品正面参考图",
        "applied": True,
    }
    assert apply_agent_asset_rename(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="rename-1",
        asset_id=prepared.asset_id,
        expected_display_name=prepared.expected_display_name,
        target_display_name=prepared.target_display_name,
    ) == renamed
    assert db_session.scalar(select(func.count()).select_from(AgentToolMutation)) == 1

    after = reconcile_agent_asset_rename(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="rename-1",
        asset_id=prepared.asset_id,
        expected_display_name=prepared.expected_display_name,
        target_display_name=prepared.target_display_name,
    )
    assert after.state == "applied"
    assert after.result == renamed
    rename_gallery_asset(
        db_session,
        product_id=product.id,
        asset_id=asset.id,
        expected_display_name="商品正面参考图",
        display_name="用户后续改名",
    )
    replayed_after_later_change = reconcile_agent_asset_rename(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="rename-1",
        asset_id=prepared.asset_id,
        expected_display_name=prepared.expected_display_name,
        target_display_name=prepared.target_display_name,
    )
    assert replayed_after_later_change.state == "applied"
    assert replayed_after_later_change.result == renamed
    ambiguous = reconcile_agent_asset_rename(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="rename-without-ledger",
        asset_id=prepared.asset_id,
        expected_display_name=prepared.expected_display_name,
        target_display_name=prepared.target_display_name,
    )
    assert ambiguous.state == "conflict"

    with pytest.raises(ConflictError, match="不同"):
        apply_agent_asset_rename(
            db_session,
            conversation_id=conversation.id,
            idempotency_key="rename-1",
            asset_id=prepared.asset_id,
            expected_display_name=prepared.expected_display_name,
            target_display_name="另一名称",
        )


def test_agent_folder_and_move_tools_share_atomic_gallery_mutations(db_session) -> None:
    product, first_asset, draft, _ = _create_product_and_draft(db_session, image_count=2)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    second_asset = next(asset for asset in product.image_assets if asset.id != first_asset.id)

    prepared_folder = prepare_agent_folder_create(
        db_session,
        conversation_id=conversation.id,
        name="  核心参考  ",
    )
    assert prepared_folder.name == "核心参考"
    assert reconcile_agent_folder_create(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="folder-create-1",
        folder_id=prepared_folder.folder_id,
        name=prepared_folder.name,
    ).state == "not_applied"
    created_folder = apply_agent_folder_create(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="folder-create-1",
        folder_id=prepared_folder.folder_id,
        name=prepared_folder.name,
    )
    assert created_folder["folder_id"] == prepared_folder.folder_id
    assert created_folder["name"] == "核心参考"

    prepared_move = prepare_agent_asset_move(
        db_session,
        conversation_id=conversation.id,
        asset_ids=[first_asset.id, second_asset.id],
        target_folder_id=prepared_folder.folder_id,
    )
    assert [move.expected_folder_id for move in prepared_move.moves] == [None, None]
    moved = apply_agent_asset_move(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="move-1",
        moves=list(prepared_move.moves),
        target_folder_id=prepared_move.target_folder_id,
    )
    assert moved == {
        "asset_ids": [first_asset.id, second_asset.id],
        "folder_id": prepared_folder.folder_id,
        "applied": True,
    }
    assert reconcile_agent_asset_move(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="move-1",
        moves=list(prepared_move.moves),
        target_folder_id=prepared_move.target_folder_id,
    ).state == "applied"

    prepared_rename = prepare_agent_folder_rename(
        db_session,
        conversation_id=conversation.id,
        folder_id=prepared_folder.folder_id,
        target_name="精选参考",
    )
    renamed = apply_agent_folder_rename(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="folder-rename-1",
        folder_id=prepared_rename.folder_id,
        expected_name=prepared_rename.expected_name,
        target_name=prepared_rename.target_name,
    )
    assert renamed["name"] == "精选参考"
    reconciled_rename = reconcile_agent_folder_rename(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="folder-rename-1",
        folder_id=prepared_rename.folder_id,
        expected_name=prepared_rename.expected_name,
        target_name=prepared_rename.target_name,
    )
    assert reconciled_rename.state == "applied"
    assert reconciled_rename.result == renamed

    stale_move = prepare_agent_asset_move(
        db_session,
        conversation_id=conversation.id,
        asset_ids=[first_asset.id, second_asset.id],
        target_folder_id=None,
    )
    first_asset.user_folder_id = None
    db_session.commit()
    with pytest.raises(ConflictError, match="所在文件夹"):
        apply_agent_asset_move(
            db_session,
            conversation_id=conversation.id,
            idempotency_key="move-stale",
            moves=list(stale_move.moves),
            target_folder_id=stale_move.target_folder_id,
        )
    db_session.expire_all()
    assert db_session.get(type(second_asset), second_asset.id).user_folder_id == prepared_folder.folder_id
    assert db_session.scalar(
        select(func.count()).select_from(AgentToolMutation).where(AgentToolMutation.idempotency_key == "move-stale")
    ) == 0


def test_agent_catalog_cutover_cross_checks_projection_and_harness_state(db_session) -> None:
    product, _, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )

    def projection(key: str, status: AgentTurnStatus, harness_turn_id: str | None):
        item = AgentTurnProjection(
            conversation_id=conversation.id,
            harness_turn_id=harness_turn_id,
            idempotency_key=key,
            request_hash="a" * 64,
            input_text=key,
            input_asset_ids_json=[],
            status=status,
        )
        db_session.add(item)
        db_session.flush()
        return item

    unbound = projection("unbound", AgentTurnStatus.QUEUED, None)
    stale_running = projection("stale-running", AgentTurnStatus.RUNNING, "harness-succeeded")
    requires_input = projection("requires-input", AgentTurnStatus.REQUIRES_INPUT, "harness-question")
    unsynced_artifact = projection(
        "unsynced-artifact",
        AgentTurnStatus.AWAITING_CONFIRMATION,
        "harness-awaiting",
    )
    synced_artifact = projection(
        "synced-artifact",
        AgentTurnStatus.AWAITING_CONFIRMATION,
        "harness-synced",
    )
    synced_artifact.workflow_draft_revision_id = draft.current_revision_id
    db_session.commit()

    class Gateway:
        states = {
            "harness-succeeded": AgentTurnStatus.SUCCEEDED,
            "harness-question": AgentTurnStatus.REQUIRES_INPUT,
            "harness-awaiting": AgentTurnStatus.AWAITING_CONFIRMATION,
        }

        def get_turn(self, *, conversation_id: str, turn_id: str):
            assert conversation_id == conversation.id
            return type("TurnState", (), {"status": self.states[turn_id]})()

    summary = inspect_agent_catalog_cutover(db_session, gateway=Gateway())
    assert summary.checked_turns == 4
    assert {blocker.projection_id for blocker in summary.blockers} == {
        unbound.id,
        requires_input.id,
        unsynced_artifact.id,
    }
    assert stale_running.id not in {blocker.projection_id for blocker in summary.blockers}
    assert synced_artifact.id not in {blocker.projection_id for blocker in summary.blockers}
    assert summary.ready is False


def test_agent_catalog_cutover_blocks_when_harness_cannot_be_verified(db_session) -> None:
    product, _, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    projection = AgentTurnProjection(
        conversation_id=conversation.id,
        harness_turn_id="harness-unavailable",
        idempotency_key="unavailable",
        request_hash="b" * 64,
        input_text="unavailable",
        input_asset_ids_json=[],
        status=AgentTurnStatus.UNKNOWN,
    )
    db_session.add(projection)
    db_session.commit()

    class Gateway:
        def get_turn(self, **_kwargs):
            raise AgentServiceRequestError(
                status_code=None,
                code="unavailable",
                safe_message="Agent 服务暂时不可用",
            )

    summary = inspect_agent_catalog_cutover(db_session, gateway=Gateway())
    assert summary.ready is False
    assert summary.blockers[0].projection_id == projection.id
    assert "unavailable" in summary.blockers[0].reason


def test_internal_agent_routes_require_service_token_and_never_need_browser_session(
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    product, asset, draft, payload = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    internal_token = "agent-internal-token-with-at-least-32-characters"
    monkeypatch.setenv("AGENT_SERVICE_INTERNAL_TOKEN", internal_token)
    get_settings.cache_clear()
    client = TestClient(create_app())
    contract_path = f"/api/internal/v1/agent-conversations/{conversation.id}/contract"

    assert client.get(contract_path).status_code == 401
    _login(client)
    assert client.get(contract_path).status_code == 401
    assert client.get(
        contract_path,
        headers={"Authorization": "Bearer wrong-token"},
    ).status_code == 401

    headers = {"Authorization": f"Bearer {internal_token}"}
    contract = client.get(contract_path, headers=headers)
    assert contract.status_code == 200, contract.text
    assert contract.json()["conversation_id"] == conversation.id
    assert contract.json()["tool_contract_version"] == 2

    validation_path = f"/api/internal/v1/agent-conversations/{conversation.id}/workflow-draft/validate"
    validated = client.post(validation_path, headers=headers, json={"value": payload})
    assert validated.status_code == 200, validated.text
    assert validated.json() == {"accepted": True}

    invalid_payload = make_workflow_draft_payload(reference_asset_id=asset.id)
    invalid_payload["visual_system"]["payload"]["colors"][1]["role"] = "primary"
    rejected = client.post(validation_path, headers=headers, json={"value": invalid_payload})
    assert rejected.status_code == 400
    assert "visual_system.payload" in rejected.json()["detail"]
    assert "视觉颜色 role 不能重复" in rejected.json()["detail"]

    other_product, other_asset, _, _ = _create_product_and_draft(db_session, name="其他商品")
    assert other_product.id != product.id
    foreign_payload = make_workflow_draft_payload(reference_asset_id=other_asset.id)
    foreign_reference = client.post(validation_path, headers=headers, json={"value": foreign_payload})
    assert foreign_reference.status_code == 400
    assert foreign_reference.json()["detail"] == "WorkflowDraft 引用了其他商品的图片资产"

    asset.media_object.verification_status = MediaVerificationStatus.LEGACY_PENDING
    db_session.commit()
    unverified_reference = client.post(validation_path, headers=headers, json={"value": payload})
    assert unverified_reference.status_code == 400
    assert unverified_reference.json()["detail"] == "WorkflowDraft 引用了未通过核验的图片资产"
    asset.media_object.verification_status = MediaVerificationStatus.VERIFIED
    db_session.commit()

    assets = client.get(
        f"/api/internal/v1/agent-conversations/{conversation.id}/assets?limit=10",
        headers=headers,
    )
    assert assets.status_code == 200, assets.text
    assert assets.json()["items"][0]["id"] == asset.id
    assert "storage_path" not in assets.text
    assert "url" not in assets.text
    content = client.get(
        f"/api/internal/v1/agent-conversations/{conversation.id}/assets/{asset.id}/content",
        headers=headers,
    )
    assert content.status_code == 200
    assert content.headers["content-type"] == "image/png"
    assert content.content == _make_demo_image_bytes()

    prepared = client.post(
        f"/api/internal/v1/agent-conversations/{conversation.id}/asset-renames/prepare",
        headers=headers,
        json={"asset_id": asset.id, "target_display_name": "正面参考图"},
    )
    assert prepared.status_code == 200, prepared.text
    applied = client.post(
        f"/api/internal/v1/agent-conversations/{conversation.id}/asset-renames",
        headers={**headers, "Idempotency-Key": "internal-rename-1"},
        json=prepared.json(),
    )
    assert applied.status_code == 200, applied.text
    assert applied.json()["display_name"] == "正面参考图"
    reconciled = client.post(
        f"/api/internal/v1/agent-conversations/{conversation.id}/asset-renames/reconcile",
        headers={**headers, "Idempotency-Key": "internal-rename-1"},
        json=prepared.json(),
    )
    assert reconciled.status_code == 200, reconciled.text
    assert reconciled.json()["state"] == "applied"

    invalid_folder = client.post(
        f"/api/internal/v1/agent-conversations/{conversation.id}/folder-creates/prepare",
        headers=headers,
        json={"name": "Agent 整理", "unknown": True},
    )
    assert invalid_folder.status_code == 422
    prepared_folder = client.post(
        f"/api/internal/v1/agent-conversations/{conversation.id}/folder-creates/prepare",
        headers=headers,
        json={"name": "Agent 整理"},
    )
    assert prepared_folder.status_code == 200, prepared_folder.text
    created_folder = client.post(
        f"/api/internal/v1/agent-conversations/{conversation.id}/folder-creates",
        headers={**headers, "Idempotency-Key": "internal-folder-create-1"},
        json=prepared_folder.json(),
    )
    assert created_folder.status_code == 200, created_folder.text
    folder_id = created_folder.json()["folder_id"]

    prepared_move = client.post(
        f"/api/internal/v1/agent-conversations/{conversation.id}/asset-moves/prepare",
        headers=headers,
        json={"asset_ids": [asset.id], "target_folder_id": folder_id},
    )
    assert prepared_move.status_code == 200, prepared_move.text
    moved = client.post(
        f"/api/internal/v1/agent-conversations/{conversation.id}/asset-moves",
        headers={**headers, "Idempotency-Key": "internal-move-1"},
        json=prepared_move.json(),
    )
    assert moved.status_code == 200, moved.text
    assert moved.json()["asset_ids"] == [asset.id]
    assert moved.json()["folder_id"] == folder_id

    prepared_folder_rename = client.post(
        f"/api/internal/v1/agent-conversations/{conversation.id}/folder-renames/prepare",
        headers=headers,
        json={"folder_id": folder_id, "target_name": "已整理参考"},
    )
    assert prepared_folder_rename.status_code == 200, prepared_folder_rename.text
    renamed_folder = client.post(
        f"/api/internal/v1/agent-conversations/{conversation.id}/folder-renames",
        headers={**headers, "Idempotency-Key": "internal-folder-rename-1"},
        json=prepared_folder_rename.json(),
    )
    assert renamed_folder.status_code == 200, renamed_folder.text
    assert renamed_folder.json()["name"] == "已整理参考"


class _FakeAgentGateway:
    def __init__(self) -> None:
        self.run_id = ""
        self.turn_id = "harness-turn-api"
        self.status = AgentTurnStatus.REQUIRES_INPUT
        self.start_calls: list[dict] = []
        self.event_cursors: list[int] = []

    def state(self) -> AgentServiceTurnState:
        question = (
            AgentServiceQuestion(
                id="question-1",
                header="图片文字",
                question="图片中的文字使用哪种语言？",
                options=[AgentServiceQuestionOption(label="中文")],
            )
            if self.status == AgentTurnStatus.REQUIRES_INPUT
            else None
        )
        return AgentServiceTurnState(
            api_version="v1alpha1",
            run_id=self.run_id,
            turn_id=self.turn_id,
            status=self.status,
            question=question,
            created_at=datetime.now(UTC),
            updated_at=datetime.now(UTC),
        )

    def start_turn(self, **kwargs) -> AgentServiceTurnState:
        self.start_calls.append(kwargs)
        return self.state()

    def get_turn(self, **_kwargs) -> AgentServiceTurnState:
        return self.state()

    def answer_question(self, **_kwargs) -> AgentServiceTurnState:
        self.status = AgentTurnStatus.QUEUED
        return self.state()

    def resume_turn(self, **_kwargs) -> AgentServiceTurnState:
        self.status = AgentTurnStatus.RUNNING
        return self.state()

    def cancel_turn(self, **_kwargs) -> AgentServiceTurnState:
        self.status = AgentTurnStatus.CANCELED
        return self.state()

    async def stream_turn_events(self, **kwargs):
        self.event_cursors.append(kwargs["after"])
        yield b'id: 8\nevent: text.delta\ndata: {"delta":"ok"}\n\n'


class _ArtifactAgentGateway(_FakeAgentGateway):
    def __init__(self, payload: dict) -> None:
        super().__init__()
        self.payload = payload
        self.status = AgentTurnStatus.AWAITING_CONFIRMATION

    def state(self) -> AgentServiceTurnState:
        return AgentServiceTurnState(
            api_version="v1alpha1",
            run_id=self.run_id,
            turn_id=self.turn_id,
            status=self.status,
            artifact=AgentServiceArtifact(
                name="propose_workflow_draft",
                value=self.payload,
                step_id="worker-artifact-step",
            ),
            output="草案已准备完成",
            created_at=datetime.now(UTC),
            updated_at=datetime.now(UTC),
            finished_at=datetime.now(UTC),
        )


def test_public_agent_routes_keep_session_scope_idempotency_question_and_sse(
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app
    from productflow_backend.presentation.routes import agent_conversations as agent_routes

    product, asset, draft, _ = _create_product_and_draft(db_session)
    other_product, _, _, _ = _create_product_and_draft(db_session, name="公开 API 其他商品")
    gateway = _FakeAgentGateway()
    enqueued: list[str] = []
    monkeypatch.setattr(agent_routes, "_agent_gateway_or_raise", lambda: gateway)
    monkeypatch.setattr(agent_routes, "enqueue_agent_turn_sync", enqueued.append)
    client = TestClient(create_app())
    collection_path = f"/api/v2/products/{product.id}/agent-conversations"

    unauthorized = client.post(collection_path, json={"workflow_draft_id": draft.id})
    assert unauthorized.status_code == 401
    _login(client)
    created = client.post(collection_path, json={"workflow_draft_id": draft.id})
    assert created.status_code == 201, created.text
    conversation = created.json()
    gateway.run_id = conversation["harness_run_id"]

    turn_path = f"{collection_path}/{conversation['id']}/turns"
    submitted = client.post(
        turn_path,
        json={
            "input_text": "请完善工作流",
            "asset_ids": [asset.id],
            "idempotency_key": "public-turn-1",
        },
    )
    assert submitted.status_code == 202, submitted.text
    body = submitted.json()
    assert body["created"] is True
    assert body["turn"]["status"] == "requires_input"
    assert body["turn"]["question"]["id"] == "question-1"
    projection_id = body["turn"]["id"]
    assert gateway.start_calls[0]["idempotency_key"] == projection_id
    assert gateway.start_calls[0]["asset_ids"] == [asset.id]
    assert enqueued == [projection_id]

    repeated = client.post(
        turn_path,
        json={
            "input_text": "请完善工作流",
            "asset_ids": [asset.id],
            "idempotency_key": "public-turn-1",
        },
    )
    assert repeated.status_code == 202, repeated.text
    assert repeated.json()["created"] is False
    assert repeated.json()["turn"]["id"] == projection_id
    assert len(gateway.start_calls) == 1

    turn_page = client.get(turn_path)
    assert turn_page.status_code == 200, turn_page.text
    assert turn_page.json() == {
        "items": [repeated.json()["turn"]],
        "next_cursor": None,
    }

    cross_scope = client.get(
        f"/api/v2/products/{other_product.id}/agent-conversations/{conversation['id']}"
    )
    assert cross_scope.status_code == 404

    answered = client.post(
        f"{turn_path}/{projection_id}/questions/question-1/answer",
        json={"option": 0},
    )
    assert answered.status_code == 200, answered.text
    assert answered.json()["status"] == "queued"
    assert answered.json()["resume_required"] is True

    def fail_enqueue(_: str) -> None:
        raise RuntimeError("queue unavailable")

    monkeypatch.setattr(agent_routes, "enqueue_agent_turn_sync", fail_enqueue)
    failed_resume = client.post(f"{turn_path}/{projection_id}/resume")
    assert failed_resume.status_code == 503, failed_resume.text
    persisted_after_failed_enqueue = client.get(f"{turn_path}/{projection_id}")
    assert persisted_after_failed_enqueue.status_code == 200
    assert persisted_after_failed_enqueue.json()["status"] == "running"
    assert persisted_after_failed_enqueue.json()["resume_required"] is False
    assert "无法入队" in persisted_after_failed_enqueue.json()["sync_error"]

    monkeypatch.setattr(agent_routes, "enqueue_agent_turn_sync", enqueued.append)
    resumed = client.post(f"{turn_path}/{projection_id}/resume")
    assert resumed.status_code == 200, resumed.text
    assert resumed.json()["status"] == "running"
    assert resumed.json()["resume_required"] is False
    assert resumed.json()["sync_error"] is None

    events = client.get(
        f"{turn_path}/{projection_id}/events?after=3",
        headers={"Last-Event-ID": "7"},
    )
    assert events.status_code == 200, events.text
    assert events.headers["content-type"].startswith("text/event-stream")
    assert events.text == 'id: 8\nevent: text.delta\ndata: {"delta":"ok"}\n\n'
    assert gateway.event_cursors == [7]

    canceled = client.post(f"{turn_path}/{projection_id}/cancel")
    assert canceled.status_code == 200, canceled.text
    assert canceled.json()["status"] == "canceled"


def test_sync_worker_recovers_unbound_turn_and_idempotently_attaches_artifact(db_session) -> None:
    product, asset, draft, payload = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="浏览器关闭后继续生成草案",
        input_asset_ids=[asset.id],
        idempotency_key="worker-turn-1",
    ).projection
    gateway = _ArtifactAgentGateway(payload)
    gateway.run_id = conversation.harness_run_id
    delayed: list[tuple[str, int]] = []

    recovery_enqueued: list[str] = []
    summary = recover_unfinished_agent_turn_syncs(enqueue=recovery_enqueued.append)
    assert summary.pending_turns == 1
    assert summary.enqueued_turns == 1
    assert recovery_enqueued == [projection.id]

    execute_agent_turn_sync(
        projection.id,
        gateway=gateway,
        enqueue_later=lambda target_id, delay_ms: delayed.append((target_id, delay_ms)),
    )
    db_session.expire_all()
    projection = db_session.get(AgentTurnProjection, projection.id)
    assert projection is not None
    assert projection.harness_turn_id == gateway.turn_id
    assert projection.status == AgentTurnStatus.AWAITING_CONFIRMATION
    assert projection.workflow_draft_revision_id is not None
    assert projection.sync_error is None
    assert delayed == []
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftRevision)) == 2

    execute_agent_turn_sync(
        projection.id,
        gateway=gateway,
        enqueue_later=lambda target_id, delay_ms: delayed.append((target_id, delay_ms)),
    )
    db_session.expire_all()
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftRevision)) == 2
    assert recover_unfinished_agent_turn_syncs(enqueue=recovery_enqueued.append).pending_turns == 0

    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    confirmed = client.post(
        f"/api/v2/products/{product.id}/workflow-drafts/{draft.id}/confirm",
        json={"expected_draft_version": 2},
    )
    assert confirmed.status_code == 200, confirmed.text
    db_session.expire_all()
    refreshed_conversation = db_session.get(AgentConversation, conversation.id)
    assert refreshed_conversation is not None
    assert refreshed_conversation.status == AgentConversationStatus.COMPLETED
    followup = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="确认后继续优化工作流",
        input_asset_ids=[asset.id],
        idempotency_key="worker-turn-followup",
    )
    assert followup.created is True
    assert followup.projection.conversation.status == AgentConversationStatus.COLLECTING
    assert followup.projection.conversation.harness_run_id == conversation.harness_run_id


def test_agent_projection_tables_are_removed_without_touching_existing_rows(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path = tmp_path / "workflow-agent-migration.db"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260812_0033")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    now = "2026-08-12 12:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-agent', '迁移保留商品', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_drafts "
                "(id, product_id, status, created_at, updated_at) "
                "VALUES ('draft-agent', 'product-agent', 'collecting', :now, :now)"
            ),
            {"now": now},
        )

    command.upgrade(config, "head")
    inspector = sa.inspect(engine)
    assert {
        "agent_conversations",
        "agent_turn_projections",
        "agent_tool_mutations",
    }.issubset(inspector.get_table_names())
    conversation_fks = {
        foreign_key["name"]: foreign_key
        for foreign_key in inspector.get_foreign_keys("agent_conversations")
    }
    assert conversation_fks["fk_agent_conversations_product_id"]["options"]["ondelete"] == "CASCADE"
    assert conversation_fks["fk_agent_conversations_workflow_draft_id"]["options"]["ondelete"] == "CASCADE"
    turn_constraints = {constraint["name"] for constraint in inspector.get_unique_constraints("agent_turn_projections")}
    assert "uq_agent_turn_projections_conversation_key" in turn_constraints
    assert "uq_agent_turn_projections_harness_turn_id" in turn_constraints

    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO agent_conversations "
                "(id, product_id, workflow_draft_id, harness_run_id, status, created_at, updated_at) "
                "VALUES ('conversation-agent', 'product-agent', 'draft-agent', 'run-agent', "
                "'collecting', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO agent_turn_projections "
                "(id, conversation_id, idempotency_key, request_hash, input_text, "
                "input_asset_ids_json, status, resume_required, created_at, updated_at) "
                "VALUES ('turn-agent', 'conversation-agent', 'key-agent', :hash, '核对商品', "
                "'[]', 'queued', 0, :now, :now)"
            ),
            {"hash": "a" * 64, "now": now},
        )
    with pytest.raises(sa.exc.IntegrityError):
        with engine.begin() as connection:
            connection.execute(
                sa.text(
                    "INSERT INTO agent_turn_projections "
                    "(id, conversation_id, idempotency_key, request_hash, input_text, "
                    "input_asset_ids_json, status, resume_required, created_at, updated_at) "
                    "VALUES ('turn-agent-2', 'conversation-agent', 'key-agent', :hash, '不同请求', "
                    "'[]', 'queued', 0, :now, :now)"
                ),
                {"hash": "b" * 64, "now": now},
            )

    engine.dispose()
    command.downgrade(config, "20260812_0033")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "agent_conversations" not in inspector.get_table_names()
    with engine.connect() as connection:
        assert connection.scalar(sa.text("SELECT name FROM products WHERE id = 'product-agent'")) == "迁移保留商品"
        assert connection.scalar(sa.text("SELECT id FROM workflow_drafts WHERE id = 'draft-agent'")) == "draft-agent"
    engine.dispose()

    command.upgrade(config, "head")
    get_settings.cache_clear()
