from __future__ import annotations

from datetime import UTC, datetime, timedelta

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from sqlalchemy import event, func, select
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.agent import control as agent_control
from productflow_backend.application.agent.control import refresh_agent_turn, synchronize_agent_turn_state
from productflow_backend.application.agent.conversations import (
    attach_agent_workflow_draft_artifact,
    bind_harness_turn,
    create_agent_conversation,
    list_agent_turn_page,
    project_agent_turn_state,
    record_agent_turn_start_error,
    reserve_agent_turn,
)
from productflow_backend.application.agent.execution import (
    claim_agent_turn_execution,
    recover_expired_agent_turn_executions,
)
from productflow_backend.application.agent.product_intake import AgentProductSelectionV1
from productflow_backend.application.agent.product_workspaces import create_agent_product_workspace
from productflow_backend.application.agent.sync import (
    execute_agent_turn_sync,
    recover_unfinished_agent_turn_syncs,
)
from productflow_backend.application.agent.tools import (
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
from productflow_backend.application.async_delivery import (
    recover_async_dispatch_for_actor,
    stage_async_dispatch_for_actor,
)
from productflow_backend.application.delivery_renditions.presets import get_delivery_preset
from productflow_backend.application.product_images.mutations import rename_gallery_asset
from productflow_backend.application.products import create_canonical_product
from productflow_backend.application.workflow_drafts.service import (
    PRODUCT_WORKFLOW_DRAFT_RETIRED,
    append_workflow_draft_revision,
)
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import (
    AgentConversationStatus,
    AgentExecutionPhase,
    AgentToolStepKind,
    AgentToolStepStatus,
    AgentTurnStatus,
    AsyncDispatchStatus,
    MediaVerificationStatus,
    WorkflowDraftStatus,
)
from productflow_backend.domain.errors import (
    BusinessValidationError,
    ConflictError,
    NotFoundError,
)
from productflow_backend.domain.graph_catalog import GRAPH_CATALOG_VERSION
from productflow_backend.infrastructure.agent_service import (
    AgentServiceArtifact,
    AgentServiceQuestion,
    AgentServiceQuestionOption,
    AgentServiceRequestError,
    AgentServiceToolStep,
    AgentServiceTurnState,
)
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentToolMutation,
    AgentTurnEvent,
    AgentTurnExecution,
    AgentTurnProjection,
    AsyncDispatch,
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
    draft = WorkflowDraft(product_id=product.id, status=WorkflowDraftStatus.COLLECTING)
    db_session.add(draft)
    db_session.commit()
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


def test_agent_workflow_draft_requires_explicit_intake_delivery_snapshot(db_session) -> None:
    preset = get_delivery_preset("jd_hero")
    selection = AgentProductSelectionV1.model_validate(
        {
            "schema_version": 1,
            "image_types": [{"key": "hero", "quantity": 2, "order": 0}],
            "delivery_preset_key": preset.key,
        }
    )
    workspace = create_agent_product_workspace(
        db_session,
        name="Draft 交付规格约束商品",
        selection=selection,
        image_uploads=[(_make_demo_image_bytes(), "draft-reference.png", "image/png")],
        idempotency_key="draft-delivery-spec-constraint",
    )
    payload = make_workflow_draft_payload(reference_asset_id=workspace.created_assets[0].id)
    expected_spec = preset.spec.model_dump(mode="json")
    for image in payload["image_types"][0]["images"]:
        image["delivery_spec"] = expected_spec
    with pytest.raises(ConflictError, match=PRODUCT_WORKFLOW_DRAFT_RETIRED):
        append_workflow_draft_revision(
            db_session,
            product_id=workspace.product.id,
            draft_id="retired",
            expected_draft_version=0,
            payload=payload,
            ready_for_confirmation=True,
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
    detailed = AgentServiceTurnState.model_validate(
        _agent_service_state_payload(
            tool_steps=[
                {
                    "step_id": "draft-1",
                    "kind": "propose_draft",
                    "summary": "提交草案",
                    "status": "failed",
                    "tool_name": "propose_workflow_draft",
                    "details": {
                        "phase": "tool_result",
                        "error_code": "workflow_draft_validation_failed",
                        "validation_issues": [
                            {
                                "path": "image_types.0.images.0.delivery_spec.crop_anchor",
                                "message": "contain 不能指定 crop_anchor",
                            }
                        ],
                        "retryable": True,
                    },
                }
            ]
        )
    )
    assert detailed.tool_steps[0].details is not None
    assert detailed.tool_steps[0].details.error_code == "workflow_draft_validation_failed"

    invalid_steps = [
        {"step_id": "inspect-1", "kind": "generate_image", "summary": "生成图片", "status": "running"},
        {"step_id": "inspect-1", "kind": "inspect_image", "summary": "查看图片", "status": "canceled"},
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
    assert projection.tool_steps_json == [step.model_dump(mode="json", exclude_none=True)]

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
    assert projection.tool_steps_json == [step.model_dump(mode="json", exclude_none=True)]

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
    conversation = workspace.conversation

    contract = get_agent_contract(db_session, conversation.id)
    assert contract["current_draft_version"] == 0
    assert contract["has_live_graph"] is True
    assert contract["workflow_draft_id"] is None
    assert "不得提交第二份完整拓扑" in contract["system_prompt"]
    assert "request_workflow_run_v1" not in contract["system_prompt"]
    context = get_agent_product_context(db_session, conversation.id)
    assert "workflow_draft" not in context
    assert context["intake"] == {
        "schema_version": 1,
        "image_types": [
            {"key": "hero", "quantity": 2, "order": 0},
            {"key": "detail", "quantity": 1, "order": 1},
        ],
        "reference_asset_ids": [asset.id],
    }
    assert context["node_catalog"]["version"] == GRAPH_CATALOG_VERSION
    image = next(node for node in context["node_catalog"]["nodes"] if node["node_type"] == "image_generation")
    generation = next(field for field in image["config_fields"] if field["key"] == "generation_spec")
    assert any(child["key"] == "aspect_ratio" for child in generation["fields"])

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
    with pytest.raises(ConflictError, match=PRODUCT_WORKFLOW_DRAFT_RETIRED):
        attach_agent_workflow_draft_artifact(
            db_session,
            product_id=product.id,
            conversation_id=conversation.id,
            projection_id=projection.id,
            harness_turn_id="harness-agent-first",
            artifact_name="propose_workflow_draft",
            artifact_step_id="agent-first-artifact",
            artifact_value=artifact,
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

    with pytest.raises(ConflictError, match=PRODUCT_WORKFLOW_DRAFT_RETIRED):
        attach_agent_workflow_draft_artifact(
            db_session,
            product_id=product.id,
            conversation_id=conversation.id,
            projection_id=projection.id,
            harness_turn_id="harness-turn-1",
            artifact_name="propose_workflow_draft",
            artifact_step_id="artifact-step-1",
            artifact_value=payload,
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
    assert contract["current_draft_version"] == 0
    assert contract["workflow_draft_schema"] == {}
    assert contract["tool_contract_version"] == 13

    context = get_agent_product_context(db_session, conversation.id)
    assert context["product"]["name"] == product.name
    assert "workflow_draft" not in context
    assert context["intake"] is None
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
    assert contract.json()["tool_contract_version"] == 13

    validation_path = f"/api/internal/v1/agent-conversations/{conversation.id}/workflow-draft/validate"
    validated = client.post(validation_path, headers=headers, json={"value": payload})
    assert validated.status_code == 409, validated.text
    assert PRODUCT_WORKFLOW_DRAFT_RETIRED in validated.json()["detail"]

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
        self.turn_id = f"harness-turn-{len(self.start_calls)}"
        if self.status != AgentTurnStatus.AWAITING_CONFIRMATION:
            self.status = AgentTurnStatus.REQUIRES_INPUT if len(self.start_calls) == 1 else AgentTurnStatus.RUNNING
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
        self.turn_id = _kwargs["turn_id"]
        self.status = AgentTurnStatus.CANCELED
        return self.state()

    async def stream_turn_events(self, **kwargs):
        self.event_cursors.append(kwargs["after"])
        yield b'id: 8\nevent: text.delta\ndata: {"delta":"ok"}\n\n'


class _UnavailableContinuationGateway(_FakeAgentGateway):
    def cancel_turn(self, **_kwargs) -> AgentServiceTurnState:
        raise AgentServiceRequestError(
            status_code=None,
            code="unavailable",
            safe_message="Agent 服务暂时不可用",
        )

    def start_turn(self, **_kwargs) -> AgentServiceTurnState:
        raise AgentServiceRequestError(
            status_code=None,
            code="unavailable",
            safe_message="Agent 服务暂时不可用",
        )


class _StaleCancellationGateway(_UnavailableContinuationGateway):
    def cancel_turn(self, **kwargs) -> AgentServiceTurnState:
        self.turn_id = kwargs["turn_id"]
        self.status = AgentTurnStatus.CANCELED
        return self.state().model_copy(update={"execution_attempt": 1, "execution_fencing_token": 1})


class _QueuedResumeAgentGateway(_FakeAgentGateway):
    def __init__(self) -> None:
        super().__init__()
        self.status = AgentTurnStatus.QUEUED
        self.resume_calls: list[dict] = []

    def resume_turn(self, **kwargs) -> AgentServiceTurnState:
        self.resume_calls.append(kwargs)
        return self.state()


class _MissingQueuedTurnGateway(_QueuedResumeAgentGateway):
    def __init__(self) -> None:
        super().__init__()
        self.get_calls = 0

    def start_turn(self, **kwargs) -> AgentServiceTurnState:
        self.start_calls.append(kwargs)
        self.turn_id = kwargs["turn_id"]
        return self.state()

    def get_turn(self, **_kwargs) -> AgentServiceTurnState:
        self.get_calls += 1
        if self.get_calls == 1:
            raise AgentServiceRequestError(
                status_code=404,
                code="not_found",
                safe_message="Agent Turn 不存在",
            )
        return self.state()


class _TransientGetGateway(_FakeAgentGateway):
    def __init__(self) -> None:
        super().__init__()
        self.get_calls = 0

    def get_turn(self, **_kwargs) -> AgentServiceTurnState:
        self.get_calls += 1
        if self.get_calls == 1:
            raise AgentServiceRequestError(
                status_code=None,
                code="unavailable",
                safe_message="Agent 服务暂时不可用",
            )
        return self.state()


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
    monkeypatch.setattr(agent_routes, "_agent_gateway_or_none", lambda: gateway)
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
    assert gateway.start_calls[0]["idempotency_key"] == "public-turn-1"
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
    answer_body = answered.json()
    assert answer_body["answered_turn"]["status"] == "canceled"
    assert answer_body["answered_turn"]["question_answer"] == {"option": 0}
    continuation = answer_body["continuation_turn"]
    assert continuation["status"] == "running"
    assert continuation["harness_turn_id"] == "harness-turn-2"
    assert continuation["continuation_turn_id"] is None
    assert answer_body["answered_turn"]["continuation_turn_id"] == continuation["id"]
    assert "选择第 1 项" in continuation["input_text"]
    assert enqueued[-1] == continuation["id"]
    assert enqueued.count(continuation["id"]) == 1

    repeated_answer = client.post(
        f"{turn_path}/{projection_id}/questions/question-1/answer",
        json={"option": 0},
    )
    assert repeated_answer.status_code == 200, repeated_answer.text
    assert repeated_answer.json()["continuation_turn"]["id"] == continuation["id"]
    assert len(gateway.start_calls) == 2

    wrong_question = client.post(
        f"{turn_path}/{projection_id}/questions/question-2/answer",
        json={"option": 0},
    )
    assert wrong_question.status_code == 409, wrong_question.text

    db_session.add(
        AgentTurnEvent(
            turn_projection_id=projection_id,
            execution_id=None,
            run_id=conversation["harness_run_id"],
            turn_id="harness-turn-1",
            schema_version=1,
            sequence=8,
            kind="text.delta",
            payload_json={"delta": "ok"},
            created_at=datetime.now(UTC),
        )
    )
    db_session.commit()

    events = client.get(
        f"{turn_path}/{projection_id}/events?after=3",
        headers={"Last-Event-ID": "7"},
    )
    assert events.status_code == 200, events.text
    assert events.headers["content-type"].startswith("text/event-stream")
    assert 'id: 8\nevent: text.delta\ndata: {"schema_version":1,"run_id":"' in events.text
    assert '"turn_id":"harness-turn-1","sequence":8' in events.text
    assert '"kind":"text.delta","payload":{"delta":"ok"}' in events.text
    assert gateway.event_cursors == []

    canceled = client.post(f"{turn_path}/{projection_id}/cancel")
    assert canceled.status_code == 200, canceled.text
    assert canceled.json()["status"] == "canceled"


def test_public_agent_turn_get_recovers_after_transient_sync_error(
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app
    from productflow_backend.presentation.routes import agent_conversations as agent_routes

    product, _, draft, _ = _create_product_and_draft(db_session, name="同步恢复商品")
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="短暂断连后继续读取状态",
        input_asset_ids=[],
        idempotency_key="transient-sync-recovery",
    ).projection
    projection = bind_harness_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-transient-sync-recovery",
        status=AgentTurnStatus.RUNNING,
    )
    record_agent_turn_start_error(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        safe_error="Agent 服务暂时不可用",
    )
    gateway = _TransientGetGateway()
    gateway.run_id = conversation.harness_run_id
    gateway.turn_id = projection.harness_turn_id or ""
    gateway.status = AgentTurnStatus.SUCCEEDED
    monkeypatch.setattr(agent_routes, "_agent_gateway_or_none", lambda: gateway)

    client = TestClient(create_app())
    _login(client)
    turn_path = (
        f"/api/v2/products/{product.id}/agent-conversations/"
        f"{conversation.id}/turns/{projection.id}"
    )

    first = client.get(turn_path)
    assert first.status_code == 200, first.text
    assert first.json()["status"] == "running"
    assert first.json()["sync_error"] == "Agent 服务暂时不可用"

    recovered = client.get(turn_path)
    assert recovered.status_code == 200, recovered.text
    assert recovered.json()["status"] == "succeeded"
    assert recovered.json()["sync_error"] is None
    assert gateway.get_calls == 2


def test_public_agent_cancel_terminates_unbound_turn_when_agent_service_is_unavailable(
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app
    from productflow_backend.presentation.routes import agent_conversations as agent_routes

    product, _, draft, _ = _create_product_and_draft(db_session, name="Agent 服务不可用取消")
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="这条 Turn 在 Agent 服务不可用时也必须可以取消",
        input_asset_ids=[],
        idempotency_key="cancel-unbound-public-turn",
    ).projection

    monkeypatch.setattr(
        agent_routes,
        "_agent_gateway_or_raise",
        lambda: pytest.fail("unbound Turn cancellation must not construct the Agent service client"),
    )
    client = TestClient(create_app())
    _login(client)

    response = client.post(
        f"/api/v2/products/{product.id}/agent-conversations/{conversation.id}/turns/{projection.id}/cancel"
    )

    assert response.status_code == 200, response.text
    body = response.json()
    assert body["status"] == "canceled"
    assert body["harness_turn_id"] is None
    assert body["sync_error"] is None
    db_session.expire_all()
    persisted = db_session.get(AgentTurnProjection, projection.id)
    assert persisted is not None
    assert persisted.status == AgentTurnStatus.CANCELED
    assert persisted.finished_at is not None


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


def test_submit_turn_defers_transient_start_failure_and_get_stays_queued(
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app
    from productflow_backend.presentation.routes import agent_conversations as agent_routes

    product, asset, draft, _ = _create_product_and_draft(db_session, name="Turn 提交延迟绑定")
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    enqueued: list[str] = []

    class _FailStartGateway:
        def start_turn(self, **kwargs):  # noqa: ANN003
            raise AgentServiceRequestError(
                status_code=502,
                code="invalid_response",
                safe_message="Agent 服务返回了无效响应",
            )

        def get_turn(self, **kwargs):  # noqa: ANN003
            raise AssertionError("unbound queued Turn must not refresh from Agent runtime")

    gateway = _FailStartGateway()
    monkeypatch.setattr(agent_routes, "_agent_gateway_or_raise", lambda: gateway)
    monkeypatch.setattr(agent_routes, "_agent_gateway_or_none", lambda: gateway)
    monkeypatch.setattr(
        agent_routes,
        "enqueue_agent_turn_sync",
        lambda _session, projection_id: enqueued.append(projection_id),
    )
    client = TestClient(create_app())
    _login(client)
    submitted = client.post(
        f"/api/v2/products/{product.id}/agent-conversations/{conversation.id}/turns",
        json={
            "input_text": "请读取当前商品工作流",
            "asset_ids": [asset.id],
            "idempotency_key": "defer-start-502",
        },
    )
    assert submitted.status_code == 202, submitted.text
    body = submitted.json()
    assert body["created"] is True
    assert body["turn"]["status"] == "queued"
    assert body["turn"]["harness_turn_id"] is None
    assert body["turn"]["sync_error"] == "Agent 服务暂时不可用"
    assert enqueued == [body["turn"]["id"]]

    polled = client.get(
        f"/api/v2/products/{product.id}/agent-conversations/{conversation.id}/turns/{body['turn']['id']}"
    )
    assert polled.status_code == 200, polled.text
    assert polled.json()["status"] == "queued"
    assert polled.json()["harness_turn_id"] is None


def test_get_queued_unbound_agent_turn_returns_projection_instead_of_conflict(
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app
    from productflow_backend.presentation.routes import agent_conversations as agent_routes

    product, asset, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="进入工作台后自动排队",
        input_asset_ids=[asset.id],
        idempotency_key="queued-unbound-poll",
    ).projection
    assert projection.harness_turn_id is None
    assert projection.status == AgentTurnStatus.QUEUED

    class _ForbiddenGateway:
        def get_turn(self, **kwargs):  # noqa: ANN003
            raise AssertionError("queued unbound Turn must not refresh from Agent runtime")

    monkeypatch.setattr(agent_routes, "_agent_gateway_or_none", lambda: _ForbiddenGateway())
    client = TestClient(create_app())
    _login(client)
    response = client.get(
        f"/api/v2/products/{product.id}/agent-conversations/{conversation.id}/turns/{projection.id}"
    )

    assert response.status_code == 200, response.text
    body = response.json()
    assert body["status"] == "queued"
    assert body["harness_turn_id"] is None
    refreshed = refresh_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        gateway=_ForbiddenGateway(),
        tolerate_transient_unavailable=True,
    )
    assert refreshed.id == projection.id
    assert refreshed.status == AgentTurnStatus.QUEUED
    assert refreshed.harness_turn_id is None


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
    monkeypatch.setattr(agent_routes, "_agent_gateway_or_none", lambda: gateway)

    client = TestClient(create_app())
    _login(client)
    response = client.get(
        f"/api/v2/products/{product.id}/agent-conversations/{conversation.id}/turns/{projection.id}"
    )

    assert response.status_code == 409, response.text
    assert PRODUCT_WORKFLOW_DRAFT_RETIRED in response.json()["detail"]


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
    assert projection.workflow_draft_revision_id is None


def test_agent_recovery_requeues_first_stale_publish_dead_dispatch_for_pollable_projection(db_session) -> None:
    product, asset, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="恢复 DEAD dispatch 对应的排队 Turn",
        input_asset_ids=[asset.id],
        idempotency_key="dead-dispatch-recovery",
    ).projection
    dispatch = stage_async_dispatch_for_actor(
        db_session,
        "run_agent_turn_sync",
        projection.id,
    )
    dispatch.status = AsyncDispatchStatus.DEAD
    dispatch.attempts = 10
    db_session.commit()

    # Generic staging reports the dead row without reviving it; recovery must
    # choose the explicit dead-letter transition before claiming enqueue.
    observed_dead = recover_unfinished_agent_turn_syncs(
        stage_dispatch=lambda session, projection_id: stage_async_dispatch_for_actor(
            session,
            "run_agent_turn_sync",
            projection_id,
        )
    )
    assert observed_dead.pending_turns == 1
    assert observed_dead.enqueued_turns == 0

    summary = recover_unfinished_agent_turn_syncs(
        stage_dispatch=lambda session, projection_id: recover_async_dispatch_for_actor(
            session,
            "run_agent_turn_sync",
            projection_id,
        )
    )

    assert summary.pending_turns == 1
    assert summary.enqueued_turns == 1
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.PENDING
    assert persisted.attempts == 10
    assert persisted.available_at.replace(tzinfo=UTC) <= datetime.now(UTC)


@pytest.mark.parametrize("dispatch_status", [AsyncDispatchStatus.PENDING, AsyncDispatchStatus.SENT])
def test_agent_recovery_skips_projection_with_active_dispatch(
    db_session,
    dispatch_status: AsyncDispatchStatus,
) -> None:
    product, asset, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="已有投递时不重复 staging",
        input_asset_ids=[asset.id],
        idempotency_key=f"active-dispatch-{dispatch_status.value}",
    ).projection
    dispatch = stage_async_dispatch_for_actor(
        db_session,
        "run_agent_turn_sync",
        projection.id,
    )
    dispatch.status = dispatch_status
    db_session.commit()

    staged: list[str] = []
    summary = recover_unfinished_agent_turn_syncs(
        stage_dispatch=lambda _session, projection_id: staged.append(projection_id)
    )

    assert summary.pending_turns == 1
    assert summary.enqueued_turns == 0
    assert staged == []
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == dispatch_status


def test_agent_recovery_restages_consumed_pollable_projection_without_losing_schedule(db_session) -> None:
    product, asset, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="已消费投递仍需继续轮询",
        input_asset_ids=[asset.id],
        idempotency_key="consumed-dispatch-recovery",
    ).projection
    next_poll_at = datetime.now(UTC) + timedelta(minutes=5)
    dispatch = stage_async_dispatch_for_actor(
        db_session,
        "run_agent_turn_sync",
        projection.id,
    )
    dispatch.status = AsyncDispatchStatus.CONSUMED
    dispatch.available_at = next_poll_at
    dispatch.consumed_at = datetime.now(UTC)
    db_session.commit()

    summary = recover_unfinished_agent_turn_syncs(
        stage_dispatch=lambda session, projection_id: stage_async_dispatch_for_actor(
            session,
            "run_agent_turn_sync",
            projection_id,
        )
    )

    assert summary.pending_turns == 1
    assert summary.enqueued_turns == 1
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.PENDING
    assert persisted.available_at.replace(tzinfo=UTC) == next_poll_at


def test_workflow_draft_confirmation_is_retired(db_session) -> None:
    from productflow_backend.presentation.api import create_app

    product, _, draft, payload = _create_product_and_draft(db_session)
    with pytest.raises(ConflictError, match=PRODUCT_WORKFLOW_DRAFT_RETIRED):
        append_workflow_draft_revision(
            db_session,
            product_id=product.id,
            draft_id=draft.id,
            expected_draft_version=0,
            payload=payload,
            ready_for_confirmation=True,
        )

    client = TestClient(create_app())
    _login(client)
    response = client.post(
        f"/api/v2/products/{product.id}/workflow-drafts/{draft.id}/confirm",
        json={"expected_draft_version": 1},
    )
    assert response.status_code == 409
    assert PRODUCT_WORKFLOW_DRAFT_RETIRED in response.json()["detail"]


def test_sync_worker_requeues_local_turn_after_expired_claim_recovery(db_session) -> None:
    product, asset, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="恢复过期 claim",
        input_asset_ids=[asset.id],
        idempotency_key="expired-claim-requeue",
    ).projection
    projection = bind_harness_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-expired-claim",
        status=AgentTurnStatus.QUEUED,
    )
    lease = claim_agent_turn_execution(
        db_session,
        conversation_id=conversation.id,
        task_id=None,
        idempotency_key=projection.idempotency_key,
        harness_turn_id=projection.harness_turn_id or "",
        owner_id="agent-instance-1",
    )
    execution = db_session.get(AgentTurnExecution, lease.execution_id)
    assert execution is not None
    execution.lease_expires_at = datetime.now(UTC) - timedelta(seconds=1)
    db_session.commit()
    recovery = recover_expired_agent_turn_executions(db_session)
    assert recovery.requeued == 1

    gateway = _QueuedResumeAgentGateway()
    gateway.run_id = conversation.harness_run_id
    gateway.turn_id = "harness-expired-claim"
    delayed: list[tuple[str, int]] = []
    execute_agent_turn_sync(
        projection.id,
        gateway=gateway,
        enqueue_later=lambda _session, target_id, delay_ms: delayed.append((target_id, delay_ms)),
    )

    assert gateway.resume_calls == [
        {
            "conversation_id": conversation.id,
            "turn_id": "harness-expired-claim",
            "task_id": None,
        }
    ]
    assert delayed and delayed[0][0] == projection.id


def test_sync_worker_handoffs_missing_queued_turn_to_another_agent_instance(db_session) -> None:
    product, asset, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="跨 Agent 实例接管尚未开始的 Turn",
        input_asset_ids=[asset.id],
        idempotency_key="expired-claim-handoff",
    ).projection
    projection = bind_harness_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-expired-handoff",
        status=AgentTurnStatus.QUEUED,
    )
    lease = claim_agent_turn_execution(
        db_session,
        conversation_id=conversation.id,
        task_id=None,
        idempotency_key=projection.idempotency_key,
        harness_turn_id=projection.harness_turn_id or "",
        owner_id="agent-instance-1",
    )
    execution = db_session.get(AgentTurnExecution, lease.execution_id)
    assert execution is not None
    execution.lease_expires_at = datetime.now(UTC) - timedelta(seconds=1)
    db_session.commit()
    recovery = recover_expired_agent_turn_executions(db_session)
    assert recovery.requeued == 1
    db_session.expire_all()
    recovered_projection = db_session.get(AgentTurnProjection, projection.id)
    recovered_execution = db_session.get(AgentTurnExecution, lease.execution_id)
    assert recovered_projection is not None and recovered_projection.status == AgentTurnStatus.QUEUED
    assert recovered_execution is not None
    assert recovered_execution.phase == AgentExecutionPhase.CLAIMED
    assert recovered_execution.owner_id is None
    assert recovered_execution.lease_token is None
    assert recovered_execution.lease_expires_at is None

    gateway = _MissingQueuedTurnGateway()
    gateway.run_id = conversation.harness_run_id
    gateway.turn_id = "harness-expired-handoff"
    delayed: list[tuple[str, int]] = []
    execute_agent_turn_sync(
        projection.id,
        gateway=gateway,
        enqueue_later=lambda _session, target_id, delay_ms: delayed.append((target_id, delay_ms)),
    )

    assert gateway.get_calls == 2
    assert gateway.start_calls[0] == {
        "conversation_id": conversation.id,
        "task_id": None,
        "input_text": "跨 Agent 实例接管尚未开始的 Turn",
        "asset_ids": [asset.id],
        "idempotency_key": "expired-claim-handoff",
        "page_context": None,
        "turn_id": "harness-expired-handoff",
    }
    assert gateway.resume_calls == [
        {
            "conversation_id": conversation.id,
            "turn_id": "harness-expired-handoff",
            "task_id": None,
        }
    ]
    assert delayed and delayed[0][0] == projection.id


def test_answer_persists_continuation_when_agent_service_is_unavailable(db_session) -> None:
    product, asset, draft, _ = _create_product_and_draft(db_session, name="问题续接故障商品")
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    reservation = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="需要补充图片语言",
        input_asset_ids=[asset.id],
        idempotency_key="question-unavailable-turn",
    )
    bound = bind_harness_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=reservation.projection.id,
        harness_turn_id="question-unavailable-harness-turn",
        status=AgentTurnStatus.REQUIRES_INPUT,
    )
    project_agent_turn_state(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=bound.id,
        harness_turn_id=bound.harness_turn_id or "",
        status=AgentTurnStatus.REQUIRES_INPUT,
        output_text="",
        error_text=None,
        question_json={
            "id": "question-unavailable-1",
            "header": "图片文字",
            "question": "使用哪种语言？",
            "options": [{"label": "中文"}],
        },
        finished_at=None,
    )

    result = agent_control.answer_agent_question(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=bound.id,
        question_id="question-unavailable-1",
        answer={"option": 0},
        gateway=_UnavailableContinuationGateway(),
        enqueue_sync=lambda _session, _projection_id: None,
    )
    assert result.continuation_turn.status == AgentTurnStatus.QUEUED
    assert result.continuation_turn.harness_turn_id is None

    db_session.expire_all()
    answered = db_session.get(AgentTurnProjection, bound.id)
    assert answered is not None
    assert answered.question_answer_json == {"option": 0}
    assert answered.continuation_turn_id is not None
    continuation = db_session.get(AgentTurnProjection, answered.continuation_turn_id)
    assert continuation is not None
    assert continuation.status == AgentTurnStatus.QUEUED
    assert continuation.harness_turn_id is None
    assert continuation.sync_error == "Agent 服务暂时不可用"


def test_answer_continues_when_old_waiter_returns_stale_fencing_snapshot(db_session) -> None:
    product, asset, draft, _ = _create_product_and_draft(db_session, name="问题旧 fencing 商品")
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    reservation = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="需要回答旧 waiter 问题",
        input_asset_ids=[asset.id],
        idempotency_key="question-stale-fencing-turn",
    )
    bound = bind_harness_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=reservation.projection.id,
        harness_turn_id="question-stale-fencing-harness-turn",
        status=AgentTurnStatus.REQUIRES_INPUT,
    )
    question = {
        "id": "question-stale-fencing-1",
        "header": "图片文字",
        "question": "使用哪种语言？",
        "options": [{"label": "中文"}],
    }
    project_agent_turn_state(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=bound.id,
        harness_turn_id=bound.harness_turn_id or "",
        status=AgentTurnStatus.REQUIRES_INPUT,
        output_text="",
        error_text=None,
        question_json=question,
        finished_at=None,
    )
    lease = claim_agent_turn_execution(
        db_session,
        conversation_id=conversation.id,
        task_id=None,
        idempotency_key=bound.idempotency_key,
        harness_turn_id=bound.harness_turn_id or "",
        owner_id="old-agent-instance",
    )
    execution = db_session.get(AgentTurnExecution, lease.execution_id)
    assert execution is not None
    execution.phase = AgentExecutionPhase.TERMINAL
    execution.owner_id = None
    execution.lease_token = None
    execution.lease_expires_at = None
    execution.fencing_token += 1
    db_session.commit()

    gateway = _StaleCancellationGateway()
    gateway.run_id = conversation.harness_run_id
    result = agent_control.answer_agent_question(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=bound.id,
        question_id="question-stale-fencing-1",
        answer={"option": 0},
        gateway=gateway,
        enqueue_sync=lambda _session, _projection_id: None,
    )

    assert result.answered_turn.status == AgentTurnStatus.REQUIRES_INPUT
    assert result.answered_turn.question_answer_json == {"option": 0}
    assert result.answered_turn.sync_error == "问题答案已持久化；原等待 Turn 状态已过期，continuation Turn 接管"
    assert result.continuation_turn.status == AgentTurnStatus.QUEUED
    assert result.continuation_turn.harness_turn_id is None


def test_public_question_answer_returns_queued_continuation_without_agent_gateway(
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app
    from productflow_backend.presentation.routes import agent_conversations as agent_routes

    product, asset, draft, _ = _create_product_and_draft(db_session, name="公共问题续接商品")
    gateway = _FakeAgentGateway()
    enqueued: list[str] = []
    monkeypatch.setattr(agent_routes, "_agent_gateway_or_raise", lambda: gateway)
    monkeypatch.setattr(agent_routes, "_agent_gateway_or_none", lambda: gateway)
    monkeypatch.setattr(
        agent_routes,
        "enqueue_agent_turn_sync",
        lambda _session, projection_id: enqueued.append(projection_id),
    )
    client = TestClient(create_app())
    _login(client)

    collection_path = f"/api/v2/products/{product.id}/agent-conversations"
    conversation_response = client.post(collection_path, json={"workflow_draft_id": draft.id})
    assert conversation_response.status_code == 201, conversation_response.text
    conversation = conversation_response.json()
    gateway.run_id = conversation["harness_run_id"]

    turn_path = f"{collection_path}/{conversation['id']}/turns"
    submitted = client.post(
        turn_path,
        json={
            "input_text": "请补充图片语言",
            "asset_ids": [asset.id],
            "idempotency_key": "public-question-unavailable-turn",
        },
    )
    assert submitted.status_code == 202, submitted.text
    projection_id = submitted.json()["turn"]["id"]

    monkeypatch.setattr(agent_routes, "_agent_gateway_or_none", lambda: None)
    answered = client.post(
        f"{turn_path}/{projection_id}/questions/question-1/answer",
        json={"option": 0},
    )

    assert answered.status_code == 200, answered.text
    body = answered.json()
    assert body["answered_turn"]["question_answer"] == {"option": 0}
    assert body["answered_turn"]["status"] == "requires_input"
    assert body["continuation_turn"]["status"] == "queued"
    assert body["continuation_turn"]["harness_turn_id"] is None
    assert enqueued[-1] == body["continuation_turn"]["id"]
