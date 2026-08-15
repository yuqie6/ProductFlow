from __future__ import annotations

import json
from datetime import UTC, datetime

import pytest
import sqlalchemy as sa
from helpers import _make_demo_image_bytes

from productflow_backend.application.legacy_retirement.backfill import (
    backfill_legacy_archive_page,
    render_archive_backfill_csv,
)
from productflow_backend.application.legacy_retirement.contracts import (
    LegacyArchiveExportPage,
    LegacyArchiveSnapshot,
    archive_export_page_sha256,
    canonical_sha256,
)
from productflow_backend.application.legacy_retirement.profiles import LEGACY_CANVAS_PROFILE
from productflow_backend.application.legacy_retirement.snapshots import (
    export_legacy_archive_page,
    render_archive_export_csv,
)
from productflow_backend.application.use_cases import create_canonical_product
from productflow_backend.commands.backfill_legacy_archives import main as backfill_command_main
from productflow_backend.commands.export_legacy_archives import main as export_command_main
from productflow_backend.domain.enums import ImageSessionAssetKind, SourceAssetKind
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.infrastructure.db.models import (
    ImageSession,
    ImageSessionAsset,
    LegacyCanvasAgentArchive,
    LegacyUserTemplateArchive,
    LegacyWorkflowArchive,
    ProductWorkflow,
    SourceAsset,
)
from productflow_backend.infrastructure.db.session import get_engine


def _install_archive_revision(db_session, revision: str = "20260815_0039") -> None:
    db_session.execute(sa.text("CREATE TABLE alembic_version (version_num VARCHAR(32) PRIMARY KEY)"))
    db_session.execute(
        sa.text("INSERT INTO alembic_version (version_num) VALUES (:revision)"),
        {"revision": revision},
    )
    db_session.commit()


def _seed_current_v1_source(db_session) -> tuple[SourceAsset, list[ProductWorkflow]]:
    product = create_canonical_product(
        db_session,
        name="旧工作流归档回填商品",
        category="工具",
        price="199.00",
        source_note="仅用于确定性归档回填测试",
        image_uploads=[(_make_demo_image_bytes(), "legacy-backfill.png", "image/png")],
    )
    asset = product.image_assets[0]
    image_session = ImageSession(title="旧工作流图片会话")
    db_session.add(image_session)
    db_session.flush()
    session_asset = ImageSessionAsset(
        session_id=image_session.id,
        kind=ImageSessionAssetKind.REFERENCE_UPLOAD,
        original_filename="legacy-backfill.png",
        mime_type="image/png",
        storage_path=asset.media_object.storage_path,
        media_object_id=asset.media_object_id,
    )
    db_session.add(session_asset)
    db_session.flush()
    asset.source_image_session_asset_id = session_asset.id
    source = SourceAsset(
        id="legacy-source-backfill",
        product_id=product.id,
        kind=SourceAssetKind.ORIGINAL_IMAGE,
        original_filename="legacy-backfill.png",
        mime_type="image/png",
        storage_path=asset.media_object.storage_path,
        canonical_asset_id=asset.id,
    )
    workflows = [
        ProductWorkflow(
            id="legacy-workflow-a",
            product_id=product.id,
            title="旧工作流 A",
            active=True,
            schema_version=1,
            revision=1,
            edit_version=0,
        ),
        ProductWorkflow(
            id="legacy-workflow-b",
            product_id=product.id,
            title="旧工作流 B",
            active=False,
            schema_version=1,
            revision=1,
            edit_version=0,
        ),
    ]
    db_session.add_all([source, *workflows])
    db_session.commit()
    return source, workflows


def _legacy_bridge_page(snapshot: LegacyArchiveSnapshot, *, generated_at: datetime) -> LegacyArchiveExportPage:
    provisional = LegacyArchiveExportPage(
        generated_at=generated_at,
        source_profile=LEGACY_CANVAS_PROFILE,
        source_report_sha256="a" * 64,
        source_fingerprint_sha256="b" * 64,
        blocking_issue_codes=["migration_bridge_required"],
        kind=snapshot.kind,
        items=[snapshot],
        page_sha256="0" * 64,
    )
    return provisional.model_copy(update={"page_sha256": archive_export_page_sha256(provisional)})


def test_archive_export_cursor_and_backfill_are_hash_bound_and_idempotent(
    configured_env,
    db_session,
) -> None:
    _install_archive_revision(db_session)
    source, workflows = _seed_current_v1_source(db_session)
    timestamp = datetime(2026, 8, 15, 10, 0, tzinfo=UTC)
    engine = get_engine()

    first = export_legacy_archive_page(
        engine,
        storage_root=configured_env,
        kind="workflow",
        limit=1,
        generated_at=timestamp,
    )
    repeated_first = export_legacy_archive_page(
        engine,
        storage_root=configured_env,
        kind="workflow",
        limit=1,
        generated_at=timestamp,
    )
    assert first.page_sha256 == repeated_first.page_sha256
    assert first.next_cursor is not None
    assert first.items[0].source_id == workflows[0].id
    assert first.items[0].asset_declarations[0].source_canonical_asset_id == source.canonical_asset_id
    assert {item.legacy_source_type for item in first.items[0].asset_declarations} == {
        "image_session_asset",
        "source_asset",
    }

    second = export_legacy_archive_page(
        engine,
        storage_root=configured_env,
        kind="workflow",
        limit=1,
        after=first.next_cursor,
        generated_at=timestamp,
    )
    assert second.items[0].source_id == workflows[1].id
    assert second.next_cursor is None
    assert "payload_json" not in render_archive_export_csv(first).splitlines()[0]
    with pytest.raises(BusinessValidationError, match="cursor 无效"):
        export_legacy_archive_page(
            engine,
            storage_root=configured_env,
            kind="workflow",
            after="not-a-cursor",
        )
    with pytest.raises(BusinessValidationError, match="cursor 无效"):
        export_legacy_archive_page(
            engine,
            storage_root=configured_env,
            kind="workflow",
            after="a" * 4097,
        )

    planned = backfill_legacy_archive_page(
        db_session,
        page=first,
        expected_source_report_sha256=first.source_report_sha256,
        generated_at=timestamp,
    )
    assert planned.applied is False
    assert planned.global_blocking_issue_codes == []
    assert [item.status for item in planned.items] == ["would_create"]

    created = backfill_legacy_archive_page(
        db_session,
        page=first,
        expected_source_report_sha256=first.source_report_sha256,
        apply=True,
        generated_at=timestamp,
    )
    assert created.applied is True
    assert created.created_count == 1
    assert [item.status for item in created.items] == ["created"]
    assert db_session.scalar(sa.select(sa.func.count()).select_from(LegacyWorkflowArchive)) == 1

    unchanged = backfill_legacy_archive_page(
        db_session,
        page=first,
        expected_source_report_sha256=first.source_report_sha256,
        apply=True,
        generated_at=timestamp,
    )
    assert unchanged.applied is True
    assert unchanged.unchanged_count == 1
    assert [item.status for item in unchanged.items] == ["unchanged"]
    assert db_session.scalar(sa.select(sa.func.count()).select_from(LegacyWorkflowArchive)) == 1
    assert "resolved_asset_count" in render_archive_backfill_csv(unchanged).splitlines()[0]

    source.canonical_asset_id = None
    db_session.commit()
    blocked = backfill_legacy_archive_page(
        db_session,
        page=second,
        expected_source_report_sha256=second.source_report_sha256,
        apply=True,
        generated_at=timestamp,
    )
    assert blocked.applied is False
    assert [item.status for item in blocked.items] == ["blocked"]
    assert blocked.items[0].diagnostic_codes == ["target_canonical_asset_mapping_missing"]
    assert db_session.scalar(sa.select(sa.func.count()).select_from(LegacyWorkflowArchive)) == 1


def test_archive_backfill_rejects_unapproved_or_tampered_source_page(configured_env, db_session) -> None:
    _install_archive_revision(db_session)
    _seed_current_v1_source(db_session)
    page = export_legacy_archive_page(
        get_engine(),
        storage_root=configured_env,
        kind="workflow",
        source_id="legacy-workflow-a",
    )

    with pytest.raises(BusinessValidationError, match="批准的 source report hash"):
        backfill_legacy_archive_page(
            db_session,
            page=page,
            expected_source_report_sha256="0" * 64,
        )

    tampered = page.model_copy(update={"page_sha256": "0" * 64})
    with pytest.raises(BusinessValidationError, match="page hash"):
        backfill_legacy_archive_page(
            db_session,
            page=tampered,
            expected_source_report_sha256=page.source_report_sha256,
        )


def test_archive_backfill_blocks_before_querying_missing_target_archive_tables(configured_env, db_session) -> None:
    _install_archive_revision(db_session, "20260814_0038")
    _seed_current_v1_source(db_session)
    for table_name in (
        "legacy_workflow_archive_assets",
        "legacy_canvas_agent_archives",
        "legacy_user_template_archives",
        "legacy_workflow_archives",
    ):
        db_session.execute(sa.text(f"DROP TABLE {table_name}"))
    db_session.commit()

    page = export_legacy_archive_page(
        get_engine(),
        storage_root=configured_env,
        kind="workflow",
        source_id="legacy-workflow-a",
    )
    report = backfill_legacy_archive_page(
        db_session,
        page=page,
        expected_source_report_sha256=page.source_report_sha256,
        apply=True,
    )

    assert report.applied is False
    assert report.global_blocking_issue_codes == ["target_archive_schema_profile_required"]
    assert report.blocked_count == 1
    assert report.items[0].diagnostic_codes == ["target_archive_schema_profile_required"]


def test_template_and_canvas_backfill_are_idempotent_and_bridge_does_not_hide_other_blockers(
    configured_env,
    db_session,
) -> None:
    _install_archive_revision(db_session)
    _source, workflows = _seed_current_v1_source(db_session)
    timestamp = datetime(2026, 8, 15, 11, 0, tzinfo=UTC)
    template_payload = {
        "template": {"key": "legacy-saved-layout", "description": "用户保存的旧布局"},
        "template_json": {"nodes": [], "edges": []},
    }
    template_snapshot = LegacyArchiveSnapshot(
        kind="user_template",
        source_profile=LEGACY_CANVAS_PROFILE,
        source_id="legacy-template-1",
        title="旧布局",
        payload_json=template_payload,
        source_fingerprint_sha256="c" * 64,
        payload_sha256=canonical_sha256(template_payload),
        counts={},
    )
    canvas_payload = {"thread": {"status": "completed"}, "messages": [], "runs": []}
    canvas_snapshot = LegacyArchiveSnapshot(
        kind="canvas_agent_thread",
        source_profile=LEGACY_CANVAS_PROFILE,
        source_id="legacy-thread-1",
        product_id=workflows[0].product_id,
        title="旧 Agent 对话",
        payload_json=canvas_payload,
        source_fingerprint_sha256="d" * 64,
        payload_sha256=canonical_sha256(canvas_payload),
        counts={
            "message_count": 0,
            "run_count": 0,
            "tool_event_count": 0,
            "plan_count": 0,
            "task_plan_count": 0,
            "timeline_event_count": 0,
            "visible_event_count": 0,
            "technical_event_count": 0,
        },
    )
    template_page = _legacy_bridge_page(template_snapshot, generated_at=timestamp)
    canvas_page = _legacy_bridge_page(canvas_snapshot, generated_at=timestamp)

    for page in (template_page, canvas_page):
        created = backfill_legacy_archive_page(
            db_session,
            page=page,
            expected_source_report_sha256=page.source_report_sha256,
            apply=True,
            allow_legacy_bridge=True,
            generated_at=timestamp,
        )
        assert created.applied is True
        assert created.created_count == 1
        unchanged = backfill_legacy_archive_page(
            db_session,
            page=page,
            expected_source_report_sha256=page.source_report_sha256,
            apply=True,
            allow_legacy_bridge=True,
            generated_at=timestamp,
        )
        assert unchanged.applied is True
        assert unchanged.unchanged_count == 1

    assert db_session.scalar(sa.select(sa.func.count()).select_from(LegacyUserTemplateArchive)) == 1
    assert db_session.scalar(sa.select(sa.func.count()).select_from(LegacyCanvasAgentArchive)) == 1

    still_blocked = canvas_page.model_copy(
        update={
            "blocking_issue_codes": ["active_canvas_agent_run", "migration_bridge_required"],
            "page_sha256": "0" * 64,
        }
    )
    still_blocked = still_blocked.model_copy(
        update={"page_sha256": archive_export_page_sha256(still_blocked)}
    )
    refused = backfill_legacy_archive_page(
        db_session,
        page=still_blocked,
        expected_source_report_sha256=still_blocked.source_report_sha256,
        apply=True,
        allow_legacy_bridge=True,
    )
    assert refused.applied is False
    assert refused.global_blocking_issue_codes == ["active_canvas_agent_run"]


def test_archive_export_and_backfill_commands_write_json_and_summary_csv(
    configured_env,
    db_session,
    tmp_path,
    capsys,
) -> None:
    _install_archive_revision(db_session)
    _seed_current_v1_source(db_session)
    export_json = tmp_path / "reports" / "workflow-page.json"
    export_csv = tmp_path / "reports" / "workflow-page.csv"

    export_exit = export_command_main(
        [
            "--kind",
            "workflow",
            "--workflow-id",
            "legacy-workflow-a",
            "--output-json",
            str(export_json),
            "--output-csv",
            str(export_csv),
            "--compact",
        ]
    )
    capsys.readouterr()
    assert export_exit == 0
    page_payload = json.loads(export_json.read_text(encoding="utf-8"))
    assert page_payload["items"][0]["source_id"] == "legacy-workflow-a"
    assert "payload_json" not in export_csv.read_text(encoding="utf-8").splitlines()[0]

    result_json = tmp_path / "reports" / "backfill-plan.json"
    result_csv = tmp_path / "reports" / "backfill-plan.csv"
    backfill_exit = backfill_command_main(
        [
            "--input",
            str(export_json),
            "--expected-source-report-sha256",
            page_payload["source_report_sha256"],
            "--output-json",
            str(result_json),
            "--output-csv",
            str(result_csv),
            "--compact",
        ]
    )
    capsys.readouterr()
    assert backfill_exit == 0
    result_payload = json.loads(result_json.read_text(encoding="utf-8"))
    assert result_payload["applied"] is False
    assert result_payload["items"][0]["status"] == "would_create"
    assert "resolved_asset_count" in result_csv.read_text(encoding="utf-8").splitlines()[0]
