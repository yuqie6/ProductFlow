from __future__ import annotations

from datetime import UTC, datetime

import pytest
from fastapi.testclient import TestClient
from helpers import _make_demo_image_bytes
from sqlalchemy import func, select
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.agent_control import synchronize_agent_turn_state
from productflow_backend.application.agent_conversations import (
    bind_harness_turn,
    reserve_agent_turn,
)
from productflow_backend.application.agent_product_intake import AgentProductSelectionV1
from productflow_backend.application.agent_product_workspaces import create_agent_product_workspace
from productflow_backend.application.agent_tasks import create_agent_task
from productflow_backend.application.agent_tools import (
    get_agent_global_workflow_context,
    get_agent_global_workflow_target,
)
from productflow_backend.application.global_agent_draft_contracts import (
    GLOBAL_AGENT_DRAFT_ARTIFACT_NAME,
    GlobalAgentDraftPayloadV1,
    global_agent_draft_schema,
)
from productflow_backend.application.global_agent_drafts import (
    confirm_global_workflow_draft_review,
    get_global_workflow_draft_review,
    validate_global_agent_draft,
)
from productflow_backend.application.workflow_drafts.service import append_workflow_draft_revision
from productflow_backend.domain.enums import AgentConversationStatus, AgentTaskStatus, AgentTurnStatus
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.infrastructure.agent_service import AgentServiceArtifact, AgentServiceTurnState
from productflow_backend.infrastructure.db.models import AgentConversation, ProductWorkflow


def _workspace(db_session, *, key: str):
    selection = AgentProductSelectionV1.model_validate(
        {
            "schema_version": 1,
            "image_types": [{"key": "hero", "quantity": 2, "order": 0}],
        }
    )
    return create_agent_product_workspace(
        db_session,
        name=f"全局 Draft 测试商品 {key}",
        selection=selection,
        image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
        idempotency_key=key,
    )


def _global_conversation(db_session, workspace):
    conversation = db_session.scalar(
        select(AgentConversation).where(
            AgentConversation.session_id == workspace.conversation.session_id,
            AgentConversation.scope_type == "global",
        )
    )
    assert conversation is not None
    return conversation


def _global_workflow_artifact(workspace, *, expected_version: int) -> dict[str, object]:
    return {
        "schema_version": 1,
        "draft_kind": "workflow",
        "product_id": workspace.product.id,
        "workflow_draft_id": workspace.workflow_draft.id,
        "expected_draft_version": expected_version,
        "workflow_payload": make_workflow_draft_payload(reference_asset_id=workspace.created_assets[0].id),
        "library_payload": None,
    }


def test_global_workflow_context_is_explicit_and_bounded(db_session) -> None:
    workspace = _workspace(db_session, key="global-context")
    conversation = _global_conversation(db_session, workspace)

    context = get_agent_global_workflow_context(
        db_session,
        conversation_id=conversation.id,
        product_id=workspace.product.id,
    )

    assert context["target"] == {
        "product_id": workspace.product.id,
        "product_conversation_id": workspace.conversation.id,
        "workflow_draft_id": workspace.workflow_draft.id,
    }
    assert context["workflow_draft"]["version"] == 0
    assert context["product"]["id"] == workspace.product.id

    with pytest.raises(NotFoundError, match="商品不存在"):
        get_agent_global_workflow_target(db_session, product_id="00000000-0000-4000-8000-000000000099")


def test_global_workflow_artifact_attaches_to_target_draft_without_materializing(db_session) -> None:
    workspace = _workspace(db_session, key="global-attach")
    conversation = _global_conversation(db_session, workspace)
    task = create_agent_task(
        db_session,
        session_id=conversation.session_id,
        conversation_id=conversation.id,
        title="设计商品工作流",
        goal="为指定商品设计完整工作流",
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=None,
        conversation_id=conversation.id,
        input_text="为这个商品设计主图工作流",
        input_asset_ids=[],
        idempotency_key="global-workflow-turn",
        task_id=task.id,
    ).projection
    projection = bind_harness_turn(
        db_session,
        product_id=None,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="global-workflow-harness-turn",
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
            turn_id="global-workflow-harness-turn",
            status=AgentTurnStatus.SUCCEEDED,
            output="工作流草案已准备",
            artifact=AgentServiceArtifact(
                name=GLOBAL_AGENT_DRAFT_ARTIFACT_NAME,
                value=_global_workflow_artifact(workspace, expected_version=0),
                step_id="global-workflow-step",
            ),
            created_at=datetime.now(UTC),
            updated_at=datetime.now(UTC),
            finished_at=datetime.now(UTC),
        ),
    )

    assert synced.status == AgentTurnStatus.AWAITING_CONFIRMATION
    assert synced.workflow_draft_revision_id is not None
    assert synced.library_organization_draft_revision_id is None
    assert db_session.scalar(select(func.count()).select_from(ProductWorkflow)) == 0
    assert workspace.workflow_draft.current_revision is not None
    assert workspace.workflow_draft.current_revision.version == 1
    db_session.refresh(task)
    assert task.status == AgentTaskStatus.AWAITING_CONFIRMATION
    assert conversation.status == AgentConversationStatus.AWAITING_CONFIRMATION

    review = get_global_workflow_draft_review(
        db_session,
        conversation_id=conversation.id,
        revision_id=synced.workflow_draft_revision_id,
    )
    assert review.product_id == workspace.product.id
    assert review.product_conversation_id == workspace.conversation.id
    assert review.draft.current_revision is not None
    assert review.draft.current_revision.version == 1

    confirmed = confirm_global_workflow_draft_review(
        db_session,
        conversation_id=conversation.id,
        revision_id=synced.workflow_draft_revision_id,
        expected_draft_version=1,
    )

    assert confirmed.draft.status.value == "confirmed"
    assert confirmed.draft.current_revision is not None
    assert confirmed.draft.current_revision.confirmed_at is not None
    assert db_session.scalar(select(func.count()).select_from(ProductWorkflow)) == 0
    db_session.refresh(task)
    assert task.status == AgentTaskStatus.SUCCEEDED
    db_session.refresh(workspace.conversation)
    assert workspace.conversation.status.value == "completed"


def test_global_workflow_artifact_rejects_stale_target_version(db_session) -> None:
    workspace = _workspace(db_session, key="global-stale")
    conversation = _global_conversation(db_session, workspace)
    payload = _global_workflow_artifact(workspace, expected_version=0)
    validate_global_agent_draft(db_session, conversation_id=conversation.id, value=payload)

    append_workflow_draft_revision(
        db_session,
        product_id=workspace.product.id,
        draft_id=workspace.workflow_draft.id,
        expected_draft_version=0,
        payload=make_workflow_draft_payload(reference_asset_id=workspace.created_assets[0].id),
        ready_for_confirmation=False,
        source_turn_id="stale-target-turn",
        source_artifact_step_id="stale-target-step",
    )

    with pytest.raises(ConflictError, match="version 已变化"):
        validate_global_agent_draft(db_session, conversation_id=conversation.id, value=payload)


def test_global_workflow_draft_api_exposes_review_and_confirmation(configured_env) -> None:
    from helpers import _login

    from productflow_backend.infrastructure.db.session import get_session_factory
    from productflow_backend.presentation.api import create_app

    factory = get_session_factory()
    with factory() as session:
        workspace = _workspace(session, key="global-api")
        conversation = _global_conversation(session, workspace)
        task = create_agent_task(
            session,
            session_id=conversation.session_id,
            conversation_id=conversation.id,
            title="API 工作流草案",
            goal="通过全局 Agent 审核商品工作流",
        )
        projection = reserve_agent_turn(
            session,
            product_id=None,
            conversation_id=conversation.id,
            input_text="为指定商品生成工作流",
            input_asset_ids=[],
            idempotency_key="global-api-turn",
            task_id=task.id,
        ).projection
        projection = bind_harness_turn(
            session,
            product_id=None,
            conversation_id=conversation.id,
            projection_id=projection.id,
            harness_turn_id="global-api-harness-turn",
            status=AgentTurnStatus.RUNNING,
        )
        synced = synchronize_agent_turn_state(
            session,
            product_id=None,
            conversation_id=conversation.id,
            projection_id=projection.id,
            state=AgentServiceTurnState(
                api_version="v1alpha1",
                run_id=task.harness_run_id,
                turn_id="global-api-harness-turn",
                status=AgentTurnStatus.SUCCEEDED,
                output="工作流草案已准备",
                artifact=AgentServiceArtifact(
                    name=GLOBAL_AGENT_DRAFT_ARTIFACT_NAME,
                    value=_global_workflow_artifact(workspace, expected_version=0),
                    step_id="global-api-step",
                ),
                created_at=datetime.now(UTC),
                updated_at=datetime.now(UTC),
                finished_at=datetime.now(UTC),
            ),
        )
        assert synced.workflow_draft_revision_id is not None
        conversation_id = conversation.id
        revision_id = synced.workflow_draft_revision_id

    client = TestClient(create_app())
    _login(client)

    review = client.get(
        f"/api/v2/agent-conversations/{conversation_id}/workflow-draft-reviews/{revision_id}"
    )
    assert review.status_code == 200, review.text
    assert review.json()["product_id"] == workspace.product.id
    assert review.json()["draft"]["current_revision"]["version"] == 1

    confirmed = client.post(
        f"/api/v2/agent-conversations/{conversation_id}/workflow-draft-reviews/{revision_id}/confirm",
        json={"expected_draft_version": 1},
    )
    assert confirmed.status_code == 200, confirmed.text
    assert confirmed.json()["draft"]["status"] == "confirmed"


def test_global_workflow_artifact_contract_requires_one_branch() -> None:
    with pytest.raises(ValueError, match="library_payload"):
        GlobalAgentDraftPayloadV1.model_validate(
            {
                "schema_version": 1,
                "draft_kind": "library_organization",
                "product_id": None,
                "workflow_draft_id": None,
                "expected_draft_version": None,
                "workflow_payload": None,
                "library_payload": None,
            }
        )


def test_global_agent_draft_schema_is_strict_for_tool_contract() -> None:
    schema = global_agent_draft_schema()
    forbidden_keys = {"default", "deprecated", "discriminator", "oneOf"}

    def assert_strict(node: object) -> None:
        if isinstance(node, list):
            for item in node:
                assert_strict(item)
            return
        if not isinstance(node, dict):
            return
        assert not forbidden_keys.intersection(node)
        properties = node.get("properties")
        if isinstance(properties, dict):
            assert node.get("additionalProperties") is False
            assert node.get("required") == list(properties)
        for value in node.values():
            assert_strict(value)

    assert_strict(schema)
    assert schema["additionalProperties"] is False
    assert set(schema["required"]) == set(schema["properties"])
