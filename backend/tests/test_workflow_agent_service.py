from __future__ import annotations

from datetime import UTC, datetime, timedelta

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from sqlalchemy import event, func, select
from workflow_draft_helpers import make_workflow_draft_payload

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
    AgentToolStepKind,
    AgentToolStepStatus,
    AgentTurnStatus,
    MediaVerificationStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.agent_service import (
    AgentServiceArtifact,
    AgentServiceQuestion,
    AgentServiceQuestionOption,
    AgentServiceToolStep,
    AgentServiceTurnState,
)
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentToolMutation,
    AgentTurnProjection,
    ProviderBinding,
    ProviderProfile,
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


def _agent_service_state_payload(**overrides) -> dict:
    payload = {
        "api_version": "v1alpha1",
        "run_id": "run-1",
        "turn_id": "turn-1",
        "status": "running",
        "created_at": datetime.now(UTC),
        "updated_at": datetime.now(UTC),
    }
    payload.update(overrides)
    return payload


def test_agent_service_tool_step_contract_is_strict_bounded_and_optional() -> None:
    omitted = AgentServiceTurnState.model_validate(_agent_service_state_payload())
    assert omitted.tool_steps is None

    accepted = AgentServiceTurnState.model_validate(
        _agent_service_state_payload(
            tool_steps=[
                {
                    "step_id": "inspect-1",
                    "kind": "inspect_image",
                    "summary": "查看商品主图",
                    "status": "succeeded",
                }
            ]
        )
    )
    assert accepted.tool_steps == [
        AgentServiceToolStep(
            step_id="inspect-1",
            kind=AgentToolStepKind.INSPECT_IMAGE,
            summary="查看商品主图",
            status=AgentToolStepStatus.SUCCEEDED,
        )
    ]

    invalid_steps = [
        {"step_id": "inspect-1", "kind": "generate_image", "summary": "生成图片", "status": "running"},
        {"step_id": "inspect-1", "kind": "inspect_image", "summary": "查看图片", "status": "canceled"},
        {
            "step_id": "inspect-1",
            "kind": "inspect_image",
            "summary": "查看图片",
            "status": "running",
            "tool_name": "read_file",
        },
        {
            "step_id": "inspect-1",
            "kind": "inspect_image",
            "summary": "查看图片",
            "status": "running",
            "input": {"path": "/secret"},
        },
        {"step_id": "inspect-1", "kind": "inspect_image", "summary": "第一行\n第二行", "status": "running"},
        {"step_id": "inspect-1", "kind": "inspect_image", "summary": "a" * 161, "status": "running"},
        {"step_id": " ", "kind": "inspect_image", "summary": "查看图片", "status": "running"},
        {"step_id": "界" * 67, "kind": "inspect_image", "summary": "查看图片", "status": "running"},
        {"step_id": "inspect-1", "kind": "inspect_image", "summary": "图" * 54, "status": "running"},
    ]
    for step in invalid_steps:
        with pytest.raises(ValueError):
            AgentServiceTurnState.model_validate(_agent_service_state_payload(tool_steps=[step]))

    with pytest.raises(ValueError):
        AgentServiceTurnState.model_validate(
            _agent_service_state_payload(
                tool_steps=[
                    {
                        "step_id": f"step-{index}",
                        "kind": "read_history",
                        "summary": "读取历史",
                        "status": "unknown",
                    }
                    for index in range(101)
                ]
            )
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


def test_agent_tool_step_snapshot_replaces_preserves_and_clears(db_session) -> None:
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
        input_text="检查商品信息",
        input_asset_ids=[],
        idempotency_key="tool-step-sync",
    ).projection
    projection = bind_harness_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-tool-steps",
        status=AgentTurnStatus.RUNNING,
    )
    step = AgentServiceToolStep(
        step_id="context-1",
        kind=AgentToolStepKind.INSPECT_CONTEXT,
        summary="检查商品上下文",
        status=AgentToolStepStatus.SUCCEEDED,
    )
    projection = synchronize_agent_turn_state(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        state=AgentServiceTurnState(
            **_agent_service_state_payload(
                run_id=conversation.harness_run_id,
                turn_id="harness-tool-steps",
                tool_steps=[step.model_dump(mode="json")],
            )
        ),
    )
    assert projection.tool_steps_json == [step.model_dump(mode="json")]

    projection = synchronize_agent_turn_state(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        state=AgentServiceTurnState(
            **_agent_service_state_payload(
                run_id=conversation.harness_run_id,
                turn_id="harness-tool-steps",
            )
        ),
    )
    assert projection.tool_steps_json == [step.model_dump(mode="json")]

    projection = synchronize_agent_turn_state(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        state=AgentServiceTurnState(
            **_agent_service_state_payload(
                run_id=conversation.harness_run_id,
                turn_id="harness-tool-steps",
                tool_steps=[],
            )
        ),
    )
    assert projection.tool_steps_json == []


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

    follow_up = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="然后呢？",
        input_asset_ids=[],
        idempotency_key="artifact-follow-up",
    ).projection
    follow_up = bind_harness_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=follow_up.id,
        harness_turn_id="harness-turn-2",
        status=AgentTurnStatus.RUNNING,
    )
    follow_up = synchronize_agent_turn_state(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=follow_up.id,
        state=AgentServiceTurnState(
            api_version="v1alpha1",
            run_id=conversation.harness_run_id,
            turn_id="harness-turn-2",
            status=AgentTurnStatus.SUCCEEDED,
            output="请审阅并确认当前草案。",
            created_at=datetime.now(UTC),
            updated_at=datetime.now(UTC),
            finished_at=datetime.now(UTC),
        ),
    )
    assert follow_up.output_text == "请审阅并确认当前草案。"
    assert follow_up.workflow_draft_revision_id is None
    assert follow_up.conversation.status == AgentConversationStatus.AWAITING_CONFIRMATION
    assert follow_up.conversation.workflow_draft.current_revision_id == revision_id


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
    assert contract["tool_contract_version"] == 3

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


def test_internal_agent_runtime_config_returns_bound_secret_only_to_service(
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    profile = ProviderProfile(
        name="工作流 Agent 网关",
        provider_type="openai_compatible",
        base_url="https://agent.example/v1",
        api_key="agent-runtime-secret",
        capabilities_json=["text_responses"],
        default_models_json={},
        config_json={},
        enabled=True,
    )
    db_session.add(profile)
    db_session.flush()
    db_session.add(
        ProviderBinding(
            purpose="agent",
            provider_kind="openai",
            provider_profile_id=profile.id,
            model_settings_json={"model": "gpt-agent"},
            config_json={
                "reasoning_effort": "high",
                "reasoning_summary": "concise",
                "text_verbosity": "low",
                "service_tier": "priority",
            },
        )
    )
    db_session.commit()

    internal_token = "agent-internal-token-with-at-least-32-characters"
    monkeypatch.setenv("AGENT_SERVICE_INTERNAL_TOKEN", internal_token)
    get_settings.cache_clear()
    client = TestClient(create_app())
    path = "/api/internal/v1/agent-runtime/provider-config"

    assert client.get(path).status_code == 401
    response = client.get(path, headers={"Authorization": f"Bearer {internal_token}"})

    assert response.status_code == 200, response.text
    assert response.json() == {
        "schema_version": 1,
        "provider_kind": "openai",
        "api_key": "agent-runtime-secret",
        "base_url": "https://agent.example/v1",
        "model": "gpt-agent",
        "reasoning_effort": "high",
        "reasoning_summary": "concise",
        "text_verbosity": "low",
        "service_tier": "priority",
    }


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
    assert contract.json()["tool_contract_version"] == 3

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

    asset.media_object.verification_status = MediaVerificationStatus.MISSING
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
    monkeypatch.setattr(
        agent_routes,
        "enqueue_agent_turn_sync",
        lambda _session, projection_id: enqueued.append(projection_id),
    )
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

    def fail_enqueue(_session: object, _projection_id: str) -> None:
        raise RuntimeError("queue unavailable")

    monkeypatch.setattr(agent_routes, "enqueue_agent_turn_sync", fail_enqueue)
    failed_resume = client.post(f"{turn_path}/{projection_id}/resume")
    assert failed_resume.status_code == 503, failed_resume.text
    persisted_after_failed_enqueue = client.get(f"{turn_path}/{projection_id}")
    assert persisted_after_failed_enqueue.status_code == 200
    assert persisted_after_failed_enqueue.json()["status"] == "queued"
    assert persisted_after_failed_enqueue.json()["resume_required"] is True
    assert "无法入队" in persisted_after_failed_enqueue.json()["sync_error"]

    monkeypatch.setattr(
        agent_routes,
        "enqueue_agent_turn_sync",
        lambda _session, projection_id: enqueued.append(projection_id),
    )
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


def test_stored_malformed_agent_tool_steps_degrade_safely_in_detail_and_list_routes(
    db_session,
) -> None:
    from productflow_backend.presentation.api import create_app

    product, _, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    valid_steps = [
        {
            "step_id": f"history-{index}",
            "kind": "read_history",
            "summary": "读取既有对话历史",
            "status": "succeeded",
        }
        for index in range(101)
    ]
    invalid_steps = [
        "not-an-object",
        {**valid_steps[1], "kind": "unknown_kind"},
        {**valid_steps[2], "raw_sentinel": "RAW_SENTINEL"},
        {**valid_steps[3], "summary": "第一行\nRAW_SENTINEL"},
        {**valid_steps[4], "summary": "图" * 54},
    ]
    projection = AgentTurnProjection(
        conversation_id=conversation.id,
        harness_turn_id="historical-tool-step-turn",
        idempotency_key="historical-tool-step-key",
        request_hash="a" * 64,
        input_text="历史投影",
        input_asset_ids_json=[],
        status=AgentTurnStatus.SUCCEEDED,
        tool_steps_json=[valid_steps[0], *invalid_steps, *valid_steps[1:]],
    )
    malformed_root_projection = AgentTurnProjection(
        conversation_id=conversation.id,
        harness_turn_id="malformed-root-tool-step-turn",
        idempotency_key="malformed-root-tool-step-key",
        request_hash="b" * 64,
        input_text="损坏的历史投影",
        input_asset_ids_json=[],
        status=AgentTurnStatus.SUCCEEDED,
        tool_steps_json={"raw_sentinel": "RAW_SENTINEL"},
    )
    db_session.add_all([projection, malformed_root_projection])
    db_session.commit()

    client = TestClient(create_app())
    _login(client)
    base_path = f"/api/v2/products/{product.id}/agent-conversations/{conversation.id}/turns"
    detail_response = client.get(f"{base_path}/{projection.id}")
    malformed_root_response = client.get(f"{base_path}/{malformed_root_projection.id}")
    list_response = client.get(base_path)

    assert detail_response.status_code == 200, detail_response.text
    assert malformed_root_response.status_code == 200, malformed_root_response.text
    assert list_response.status_code == 200, list_response.text
    assert malformed_root_response.json()["tool_steps"] == []

    expected_steps = valid_steps[:100]
    detail_steps = detail_response.json()["tool_steps"]
    listed_turns = {item["id"]: item for item in list_response.json()["items"]}
    assert detail_steps == expected_steps
    assert listed_turns[projection.id]["tool_steps"] == expected_steps
    assert listed_turns[malformed_root_projection.id]["tool_steps"] == []
    assert len(detail_steps) == 100
    assert "RAW_SENTINEL" not in detail_response.text
    assert "RAW_SENTINEL" not in list_response.text
    assert all(set(step) == {"step_id", "kind", "summary", "status"} for step in detail_steps)


def test_get_active_agent_turn_refreshes_and_attaches_artifact(
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app
    from productflow_backend.presentation.routes import agent_conversations as agent_routes

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
        input_text="生成可确认草案",
        input_asset_ids=[asset.id],
        idempotency_key="refresh-artifact-turn",
    ).projection
    projection = bind_harness_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-refresh-artifact",
        status=AgentTurnStatus.RUNNING,
    )
    projection = project_agent_turn_state(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-refresh-artifact",
        status=AgentTurnStatus.AWAITING_CONFIRMATION,
        output_text="草案终态已到达",
        error_text=None,
        question_json=None,
        finished_at=datetime.now(UTC),
    )
    assert projection.workflow_draft_revision_id is None
    gateway = _ArtifactAgentGateway(payload)
    gateway.run_id = conversation.harness_run_id
    gateway.turn_id = projection.harness_turn_id or ""
    monkeypatch.setattr(agent_routes, "_agent_gateway_or_raise", lambda: gateway)

    client = TestClient(create_app())
    _login(client)
    response = client.get(
        f"/api/v2/products/{product.id}/agent-conversations/{conversation.id}/turns/{projection.id}"
    )

    assert response.status_code == 200, response.text
    body = response.json()
    assert body["status"] == "awaiting_confirmation"
    assert body["workflow_draft_revision_id"] is not None
    assert body["output_text"] == "草案已准备完成"


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
