from __future__ import annotations

import json
from unittest.mock import Mock

import pytest
from helpers import _make_demo_image_bytes
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.legacy_retirement.freeze import (
    LEGACY_V1_WRITE_FREEZE_CONFLICT_DETAIL,
    LEGACY_V1_WRITE_FREEZE_INVALID_DETAIL,
    LEGACY_V1_WRITE_FREEZE_SETTING_KEY,
    ensure_legacy_v1_write_allowed,
    get_legacy_v1_write_freeze_state,
    set_legacy_v1_write_freeze_state,
)
from productflow_backend.application.product_workflow.v2_runs import submit_v2_workflow_run
from productflow_backend.application.use_cases import create_canonical_product
from productflow_backend.application.workflow_drafts.materialization import materialize_workflow_draft
from productflow_backend.application.workflow_drafts.service import (
    confirm_workflow_draft_revision,
    create_workflow_draft,
)
from productflow_backend.commands.manage_legacy_v1_freeze import main as freeze_command_main
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import AppSetting


def test_legacy_v1_freeze_state_fails_closed_when_malformed(db_session) -> None:
    initial = get_legacy_v1_write_freeze_state(db_session)
    assert initial.configured is False
    assert initial.frozen is False
    assert initial.valid is True

    frozen = set_legacy_v1_write_freeze_state(db_session, frozen=True)
    assert frozen.configured is True
    assert frozen.frozen is True
    with pytest.raises(ConflictError, match=LEGACY_V1_WRITE_FREEZE_CONFLICT_DETAIL):
        ensure_legacy_v1_write_allowed(db_session)

    row = db_session.get(AppSetting, LEGACY_V1_WRITE_FREEZE_SETTING_KEY)
    assert row is not None
    row.value = "unexpected"
    db_session.commit()
    malformed = get_legacy_v1_write_freeze_state(db_session)
    assert malformed.frozen is True
    assert malformed.valid is False
    with pytest.raises(ConflictError, match=LEGACY_V1_WRITE_FREEZE_INVALID_DETAIL):
        ensure_legacy_v1_write_allowed(db_session)

    unfrozen = set_legacy_v1_write_freeze_state(db_session, frozen=False)
    assert unfrozen.frozen is False
    assert unfrozen.valid is True
    ensure_legacy_v1_write_allowed(db_session)


def test_v2_draft_materialization_and_run_remain_available_during_legacy_freeze(db_session) -> None:
    set_legacy_v1_write_freeze_state(db_session, frozen=True)
    product = create_canonical_product(
        db_session,
        name="冻结期间 V2 商品",
        category="工具",
        price="199",
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
    )
    draft = create_workflow_draft(
        db_session,
        product_id=product.id,
        payload=make_workflow_draft_payload(reference_asset_id=product.image_assets[0].id),
        ready_for_confirmation=True,
    )
    confirm_workflow_draft_revision(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
    )
    materialized = materialize_workflow_draft(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
        expected_workflow_revision=0,
        idempotency_key="v2-during-legacy-freeze",
    )
    queue = Mock()

    submission = submit_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=materialized.workflow.id,
        enqueue=queue,
    )

    assert submission.created is True
    assert materialized.workflow.schema_version == 2
    queue.assert_called_once_with(submission.run.id)


def test_legacy_v1_freeze_command_requires_explicit_confirmation(configured_env, capsys) -> None:
    assert freeze_command_main(["status"]) == 0
    initial = json.loads(capsys.readouterr().out)
    assert initial["frozen"] is False

    with pytest.raises(SystemExit, match="FREEZE_V1_WRITES"):
        freeze_command_main(["enable", "--confirm", "wrong"])

    assert freeze_command_main(["enable", "--confirm", "FREEZE_V1_WRITES"]) == 0
    enabled = json.loads(capsys.readouterr().out)
    assert enabled["frozen"] is True

    assert freeze_command_main(["disable", "--confirm", "UNFREEZE_V1_WRITES"]) == 0
    disabled = json.loads(capsys.readouterr().out)
    assert disabled["frozen"] is False
