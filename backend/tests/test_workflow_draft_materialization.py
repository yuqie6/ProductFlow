from __future__ import annotations

from copy import deepcopy

import pytest
from helpers import _make_demo_image_bytes
from sqlalchemy import func, select
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.product_workflow.mutations import get_or_create_product_workflow
from productflow_backend.application.use_cases import create_canonical_product, delete_product
from productflow_backend.application.workflow_drafts.materialization import (
    get_active_v2_workflow_snapshot,
    list_workflow_reveal_events,
    materialize_workflow_draft,
)
from productflow_backend.application.workflow_drafts.service import (
    append_workflow_draft_revision,
    confirm_workflow_draft_revision,
    create_workflow_draft,
    get_workflow_draft_or_raise,
)
from productflow_backend.domain.enums import WorkflowDraftStatus, WorkflowRevealEventKind
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.infrastructure.db.models import (
    ProductFactSetVersion,
    ProductWorkflow,
    WorkflowDraftRevision,
    WorkflowEdge,
    WorkflowFolder,
    WorkflowMaterialization,
    WorkflowMaterializationKey,
    WorkflowNode,
    WorkflowRevealEvent,
)


def _create_product_with_reference(db_session, *, name: str = "硬质刀具收纳套装"):
    return create_canonical_product(
        db_session,
        name=name,
        category="工业收纳",
        price="299.00",
        source_note="五款收纳盘，橙蓝配色。",
        image_uploads=[(_make_demo_image_bytes(), f"{name}.png", "image/png")],
    )


def _create_confirmed_draft(db_session, *, product_id: str, reference_asset_id: str):
    draft = create_workflow_draft(
        db_session,
        product_id=product_id,
        payload=make_workflow_draft_payload(reference_asset_id=reference_asset_id),
        ready_for_confirmation=True,
        source_turn_id="turn-1",
        source_artifact_step_id="artifact-1",
    )
    return confirm_workflow_draft_revision(
        db_session,
        product_id=product_id,
        draft_id=draft.id,
        expected_draft_version=1,
    )


def test_confirmed_revision_and_fact_set_remain_immutable_when_new_information_arrives(db_session) -> None:
    product = _create_product_with_reference(db_session)
    reference_asset_id = product.image_assets[0].id
    confirmed = _create_confirmed_draft(
        db_session,
        product_id=product.id,
        reference_asset_id=reference_asset_id,
    )
    first_revision = confirmed.current_revision
    assert first_revision is not None
    first_hash = first_revision.payload_hash
    first_payload = deepcopy(first_revision.payload_json)
    first_fact_set = first_revision.fact_set_version
    assert first_fact_set is not None

    next_payload = make_workflow_draft_payload(reference_asset_id=reference_asset_id)
    next_payload["facts"][0]["value"] = "硬质刀具收纳套装（五件套）"
    next_payload["confirmation_summary"] = "商品名称出现新信息，需要重新确认；图片计划保持不变。"
    revised = append_workflow_draft_revision(
        db_session,
        product_id=product.id,
        draft_id=confirmed.id,
        expected_draft_version=1,
        payload=next_payload,
        ready_for_confirmation=True,
        source_turn_id="turn-2",
        source_artifact_step_id="artifact-2",
    )

    assert revised.status == WorkflowDraftStatus.AWAITING_CONFIRMATION
    assert revised.current_revision is not None
    assert revised.current_revision.version == 2
    persisted_first = db_session.get(WorkflowDraftRevision, first_revision.id)
    assert persisted_first is not None
    assert persisted_first.payload_hash == first_hash
    assert persisted_first.payload_json == first_payload
    assert persisted_first.confirmed_at is not None
    persisted_fact_set = db_session.get(ProductFactSetVersion, first_fact_set.id)
    assert persisted_fact_set is not None
    assert persisted_fact_set.payload_hash == first_fact_set.payload_hash
    assert persisted_fact_set.payload_json == first_fact_set.payload_json


def test_agent_artifact_origin_retry_is_idempotent_and_rejects_different_content(db_session) -> None:
    product = _create_product_with_reference(db_session)
    payload = make_workflow_draft_payload(reference_asset_id=product.image_assets[0].id)
    draft = create_workflow_draft(
        db_session,
        product_id=product.id,
        payload=payload,
        ready_for_confirmation=False,
    )

    first = append_workflow_draft_revision(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
        payload=payload,
        ready_for_confirmation=True,
        source_turn_id="turn-retry",
        source_artifact_step_id="artifact-retry",
    )
    retried = append_workflow_draft_revision(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
        payload=payload,
        ready_for_confirmation=True,
        source_turn_id="turn-retry",
        source_artifact_step_id="artifact-retry",
    )
    assert retried.current_revision_id == first.current_revision_id
    assert len(retried.revisions) == 2

    changed = deepcopy(payload)
    changed["confirmation_summary"] = "相同来源却给出不同内容"
    with pytest.raises(ConflictError, match="同一 Agent artifact 来源"):
        append_workflow_draft_revision(
            db_session,
            product_id=product.id,
            draft_id=draft.id,
            expected_draft_version=2,
            payload=changed,
            ready_for_confirmation=True,
            source_turn_id="turn-retry",
            source_artifact_step_id="artifact-retry",
        )


def test_materialization_creates_complete_v2_workflow_and_is_idempotent(db_session) -> None:
    product = _create_product_with_reference(db_session)
    draft = _create_confirmed_draft(
        db_session,
        product_id=product.id,
        reference_asset_id=product.image_assets[0].id,
    )

    result = materialize_workflow_draft(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
        expected_workflow_revision=0,
        idempotency_key="materialize-1",
    )

    assert result.created is True
    assert result.workflow.schema_version == 2
    assert result.workflow.revision == 1
    assert result.workflow.active is True
    assert len(result.workflow.folders) == 1
    assert len(result.workflow.nodes) == 5
    assert len(result.workflow.edges) == 4
    reference_node = next(node for node in result.workflow.nodes if node.node_type.value == "reference_image")
    assert reference_node.bound_image_asset_id == product.image_assets[0].id
    assert all(node.schema_version == 2 and node.node_key for node in result.workflow.nodes)
    assert all(edge.edge_key for edge in result.workflow.edges)
    events = result.materialization.reveal_events
    assert [event.sequence for event in events] == list(range(1, len(events) + 1))
    assert events[-1].kind == WorkflowRevealEventKind.COMPLETED
    persisted_draft = get_workflow_draft_or_raise(db_session, product_id=product.id, draft_id=draft.id)
    assert persisted_draft.status == WorkflowDraftStatus.READY
    assert persisted_draft.final_workflow_id == result.workflow.id

    same_key = materialize_workflow_draft(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
        expected_workflow_revision=0,
        idempotency_key="materialize-1",
    )
    same_revision = materialize_workflow_draft(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
        expected_workflow_revision=1,
        idempotency_key="materialize-alias",
    )
    assert same_key.created is False
    assert same_key.workflow.id == result.workflow.id
    assert same_revision.created is False
    assert same_revision.workflow.id == result.workflow.id
    assert db_session.scalar(select(func.count()).select_from(WorkflowMaterialization)) == 1
    assert db_session.scalar(select(func.count()).select_from(WorkflowMaterializationKey)) == 2

    with pytest.raises(ConflictError, match="相同 idempotency key"):
        materialize_workflow_draft(
            db_session,
            product_id=product.id,
            draft_id=draft.id,
            expected_draft_version=1,
            expected_workflow_revision=1,
            idempotency_key="materialize-1",
        )
    with pytest.raises(ConflictError, match="相同 idempotency key"):
        materialize_workflow_draft(
            db_session,
            product_id=product.id,
            draft_id=draft.id,
            expected_draft_version=1,
            expected_workflow_revision=0,
            idempotency_key="materialize-alias",
        )


def test_materialization_rejects_cross_product_assets_without_writing_a_workflow(db_session) -> None:
    product = _create_product_with_reference(db_session, name="商品 A")
    other = _create_product_with_reference(db_session, name="商品 B")
    draft = _create_confirmed_draft(
        db_session,
        product_id=product.id,
        reference_asset_id=other.image_assets[0].id,
    )

    with pytest.raises(BusinessValidationError, match="其他商品"):
        materialize_workflow_draft(
            db_session,
            product_id=product.id,
            draft_id=draft.id,
            expected_draft_version=1,
            expected_workflow_revision=0,
            idempotency_key="cross-product",
        )

    assert db_session.scalar(
        select(func.count()).select_from(ProductWorkflow).where(ProductWorkflow.product_id == product.id)
    ) == 0
    persisted = get_workflow_draft_or_raise(db_session, product_id=product.id, draft_id=draft.id)
    assert persisted.status == WorkflowDraftStatus.CONFIRMED


def test_materialization_does_not_modify_an_active_v1_workflow(db_session) -> None:
    product = _create_product_with_reference(db_session)
    legacy = get_or_create_product_workflow(db_session, product.id)
    legacy_updated_at = legacy.updated_at
    legacy_node_ids = {node.id for node in legacy.nodes}
    draft = _create_confirmed_draft(
        db_session,
        product_id=product.id,
        reference_asset_id=product.image_assets[0].id,
    )

    with pytest.raises(ConflictError, match="active v1"):
        materialize_workflow_draft(
            db_session,
            product_id=product.id,
            draft_id=draft.id,
            expected_draft_version=1,
            expected_workflow_revision=0,
            idempotency_key="blocked-by-v1",
        )

    db_session.expire_all()
    persisted_legacy = db_session.get(ProductWorkflow, legacy.id)
    assert persisted_legacy is not None
    assert persisted_legacy.schema_version == 1
    assert persisted_legacy.active is True
    assert persisted_legacy.updated_at == legacy_updated_at
    assert {node.id for node in persisted_legacy.nodes} == legacy_node_ids
    assert db_session.scalar(
        select(func.count()).select_from(ProductWorkflow).where(
            ProductWorkflow.product_id == product.id,
            ProductWorkflow.schema_version == 2,
        )
    ) == 0


@pytest.mark.parametrize(
    "failing_model",
    [WorkflowFolder, WorkflowNode, WorkflowEdge, WorkflowRevealEvent],
    ids=["folder", "node", "edge", "event"],
)
def test_materialization_rolls_back_every_write_stage(db_session, monkeypatch, failing_model) -> None:
    product = _create_product_with_reference(db_session)
    draft = _create_confirmed_draft(
        db_session,
        product_id=product.id,
        reference_asset_id=product.image_assets[0].id,
    )
    original_flush = db_session.flush

    def fail_target_stage(objects=None) -> None:
        if any(isinstance(item, failing_model) for item in db_session.new):
            raise RuntimeError(f"injected {failing_model.__tablename__} write failure")
        original_flush(objects)

    monkeypatch.setattr(db_session, "flush", fail_target_stage)
    with pytest.raises(RuntimeError, match="injected"):
        materialize_workflow_draft(
            db_session,
            product_id=product.id,
            draft_id=draft.id,
            expected_draft_version=1,
            expected_workflow_revision=0,
            idempotency_key=f"fail-{failing_model.__tablename__}",
        )
    monkeypatch.setattr(db_session, "flush", original_flush)
    db_session.expire_all()

    assert db_session.scalar(
        select(func.count()).select_from(ProductWorkflow).where(
            ProductWorkflow.product_id == product.id,
            ProductWorkflow.schema_version == 2,
        )
    ) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowFolder)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowMaterialization)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowMaterializationKey)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowRevealEvent)) == 0
    persisted = get_workflow_draft_or_raise(db_session, product_id=product.id, draft_id=draft.id)
    assert persisted.status == WorkflowDraftStatus.CONFIRMED
    assert persisted.final_workflow_id is None


def test_v2_query_and_reveal_replay_are_read_only(db_session) -> None:
    product = _create_product_with_reference(db_session)
    product_updated_at = product.updated_at
    empty = get_active_v2_workflow_snapshot(db_session, product_id=product.id)
    assert empty.workflow is None
    assert empty.latest_revision == 0
    assert db_session.scalar(
        select(func.count()).select_from(ProductWorkflow).where(ProductWorkflow.product_id == product.id)
    ) == 0

    draft = _create_confirmed_draft(
        db_session,
        product_id=product.id,
        reference_asset_id=product.image_assets[0].id,
    )
    result = materialize_workflow_draft(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
        expected_workflow_revision=0,
        idempotency_key="replay-read-only",
    )
    all_events = list_workflow_reveal_events(
        db_session,
        materialization_id=result.materialization.id,
        after=0,
    )
    cursor = all_events[2].sequence
    replayed = list_workflow_reveal_events(
        db_session,
        materialization_id=result.materialization.id,
        after=cursor,
    )
    assert [event.sequence for event in replayed] == [
        event.sequence for event in all_events if event.sequence > cursor
    ]
    assert db_session.scalar(select(func.count()).select_from(WorkflowRevealEvent)) == len(all_events)
    assert db_session.get(ProductWorkflow, result.workflow.id).updated_at == result.workflow.updated_at
    assert product_updated_at <= db_session.get(type(product), product.id).updated_at


def test_failed_replacement_materialization_restores_previous_active_workflow(db_session, monkeypatch) -> None:
    product = _create_product_with_reference(db_session)
    reference_asset_id = product.image_assets[0].id
    first_draft = _create_confirmed_draft(
        db_session,
        product_id=product.id,
        reference_asset_id=reference_asset_id,
    )
    first_result = materialize_workflow_draft(
        db_session,
        product_id=product.id,
        draft_id=first_draft.id,
        expected_draft_version=1,
        expected_workflow_revision=0,
        idempotency_key="first-active-workflow",
    )
    next_payload = make_workflow_draft_payload(reference_asset_id=reference_asset_id)
    next_payload["confirmation_summary"] = "重新确认后尝试替换当前工作流。"
    revised = append_workflow_draft_revision(
        db_session,
        product_id=product.id,
        draft_id=first_draft.id,
        expected_draft_version=1,
        payload=next_payload,
        ready_for_confirmation=True,
        source_turn_id="turn-replacement",
        source_artifact_step_id="artifact-replacement",
    )
    confirm_workflow_draft_revision(
        db_session,
        product_id=product.id,
        draft_id=revised.id,
        expected_draft_version=2,
    )
    original_flush = db_session.flush

    def fail_node_stage(objects=None) -> None:
        if any(
            isinstance(item, WorkflowNode) and item.workflow_id != first_result.workflow.id
            for item in db_session.new
        ):
            raise RuntimeError("injected replacement node failure")
        original_flush(objects)

    monkeypatch.setattr(db_session, "flush", fail_node_stage)
    with pytest.raises(RuntimeError, match="replacement node failure"):
        materialize_workflow_draft(
            db_session,
            product_id=product.id,
            draft_id=revised.id,
            expected_draft_version=2,
            expected_workflow_revision=1,
            idempotency_key="replacement-failure",
        )
    monkeypatch.setattr(db_session, "flush", original_flush)
    db_session.expire_all()

    active_workflows = list(
        db_session.scalars(
            select(ProductWorkflow).where(
                ProductWorkflow.product_id == product.id,
                ProductWorkflow.active.is_(True),
            )
        )
    )
    assert [workflow.id for workflow in active_workflows] == [first_result.workflow.id]
    assert db_session.scalar(
        select(func.count()).select_from(ProductWorkflow).where(ProductWorkflow.product_id == product.id)
    ) == 1
    persisted_draft = get_workflow_draft_or_raise(db_session, product_id=product.id, draft_id=revised.id)
    assert persisted_draft.status == WorkflowDraftStatus.CONFIRMED
    assert persisted_draft.final_workflow_id is None

    retried = materialize_workflow_draft(
        db_session,
        product_id=product.id,
        draft_id=revised.id,
        expected_draft_version=2,
        expected_workflow_revision=1,
        idempotency_key="replacement-failure",
    )
    assert retried.created is True
    assert retried.workflow.revision == 2
    assert retried.workflow.active is True
    db_session.expire_all()
    assert db_session.get(ProductWorkflow, first_result.workflow.id).active is False
    ready_draft = get_workflow_draft_or_raise(db_session, product_id=product.id, draft_id=revised.id)
    assert ready_draft.status == WorkflowDraftStatus.READY
    assert ready_draft.final_workflow_id == retried.workflow.id


def test_deleting_materialized_product_cascades_draft_and_reveal_state(db_session) -> None:
    product = _create_product_with_reference(db_session)
    draft = _create_confirmed_draft(
        db_session,
        product_id=product.id,
        reference_asset_id=product.image_assets[0].id,
    )
    result = materialize_workflow_draft(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
        expected_workflow_revision=0,
        idempotency_key="delete-cascade",
    )
    materialization_id = result.materialization.id

    delete_product(db_session, product_id=product.id)

    assert db_session.get(ProductWorkflow, result.workflow.id) is None
    assert db_session.get(WorkflowMaterialization, materialization_id) is None
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftRevision)) == 0
    assert db_session.scalar(select(func.count()).select_from(ProductFactSetVersion)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowMaterializationKey)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowRevealEvent)) == 0
