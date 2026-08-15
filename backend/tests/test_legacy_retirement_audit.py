from __future__ import annotations

import json
from datetime import UTC, datetime, timedelta
from pathlib import Path

import sqlalchemy as sa

from productflow_backend.application.legacy_retirement.audit import (
    CURRENT_ARCHIVE_PROFILE,
    CURRENT_CANONICAL_PROFILE,
    LEGACY_CANVAS_PROFILE,
    UNKNOWN_PROFILE,
    audit_legacy_retirement,
)
from productflow_backend.application.legacy_retirement.contracts import archive_export_page_sha256
from productflow_backend.application.legacy_retirement.preflight import audit_legacy_cutover_preflight
from productflow_backend.application.legacy_retirement.profiles import CURRENT_CUTOVER_PROFILE
from productflow_backend.application.legacy_retirement.snapshots import export_legacy_archive_page
from productflow_backend.infrastructure.db.models import Base


def _legacy_engine(tmp_path: Path) -> tuple[sa.Engine, Path]:
    database_path = tmp_path / "legacy.db"
    storage_root = tmp_path / "legacy-storage"
    storage_root.mkdir()
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    metadata = sa.MetaData()

    sa.Table("alembic_version", metadata, sa.Column("version_num", sa.String(32), primary_key=True))
    sa.Table("products", metadata, sa.Column("id", sa.String(36), primary_key=True))
    sa.Table(
        "product_workflows",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("product_id", sa.String(36), nullable=False),
        sa.Column("active", sa.Boolean, nullable=False),
    )
    sa.Table(
        "workflow_nodes",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("workflow_id", sa.String(36), nullable=False),
        sa.Column("node_type", sa.String(40), nullable=False),
        sa.Column("status", sa.String(40), nullable=False),
        sa.Column("bound_image_asset_id", sa.String(36)),
        sa.Column("config_json", sa.JSON),
        sa.Column("output_json", sa.JSON),
    )
    sa.Table(
        "workflow_edges",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("workflow_id", sa.String(36), nullable=False),
        sa.Column("source_node_id", sa.String(36), nullable=False),
        sa.Column("target_node_id", sa.String(36), nullable=False),
    )
    sa.Table(
        "workflow_runs",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("workflow_id", sa.String(36), nullable=False),
        sa.Column("status", sa.String(40), nullable=False),
    )
    sa.Table(
        "workflow_node_runs",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("workflow_run_id", sa.String(36), nullable=False),
        sa.Column("node_id", sa.String(36), nullable=False),
        sa.Column("status", sa.String(40), nullable=False),
        sa.Column("image_session_asset_id", sa.String(36)),
    )
    sa.Table(
        "source_assets",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("product_id", sa.String(36), nullable=False),
        sa.Column("storage_path", sa.String(500), nullable=False),
        sa.Column("source_poster_variant_id", sa.String(36)),
    )
    sa.Table(
        "poster_variants",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("product_id", sa.String(36), nullable=False),
        sa.Column("copy_set_id", sa.String(36), nullable=False),
        sa.Column("storage_path", sa.String(500), nullable=False),
    )
    sa.Table(
        "user_canvas_templates",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("key", sa.String(80), nullable=False),
        sa.Column("schema_version", sa.Integer, nullable=False),
        sa.Column("template_json", sa.JSON, nullable=False),
        sa.Column("archived_at", sa.DateTime(timezone=True)),
    )
    sa.Table(
        "image_sessions",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("product_id", sa.String(36), nullable=False),
    )
    sa.Table(
        "image_session_assets",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("session_id", sa.String(36), nullable=False),
        sa.Column("storage_path", sa.String(500), nullable=False),
    )
    sa.Table(
        "canvas_agent_threads",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("product_id", sa.String(36), nullable=False),
        sa.Column("status", sa.String(40), nullable=False),
    )
    sa.Table(
        "canvas_agent_messages",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("thread_id", sa.String(36), nullable=False),
        sa.Column("role", sa.String(40), nullable=False),
        sa.Column("content", sa.Text, nullable=False),
        sa.Column("metadata_json", sa.JSON, nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
    )
    sa.Table(
        "canvas_agent_runs",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("thread_id", sa.String(36), nullable=False),
        sa.Column("status", sa.String(40), nullable=False),
        sa.Column("current_state", sa.String(40), nullable=False),
        sa.Column("checkpoint_ref", sa.String(255)),
        sa.Column("goal_state_json", sa.JSON),
    )
    sa.Table(
        "canvas_agent_tool_events",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("run_id", sa.String(36), nullable=False),
        sa.Column("tool_name", sa.String(120), nullable=False),
        sa.Column("status", sa.String(40), nullable=False),
        sa.Column("args_json", sa.JSON, nullable=False),
        sa.Column("result_json", sa.JSON),
    )
    sa.Table(
        "canvas_agent_plans",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("run_id", sa.String(36), nullable=False),
        sa.Column("status", sa.String(40), nullable=False),
        sa.Column("applied_workflow_id", sa.String(36)),
        sa.Column("plan_json", sa.JSON, nullable=False),
        sa.Column("validation_json", sa.JSON, nullable=False),
    )
    sa.Table(
        "canvas_agent_task_plans",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("thread_id", sa.String(36), nullable=False),
        sa.Column("status", sa.String(40), nullable=False),
        sa.Column("task_plan_json", sa.JSON, nullable=False),
    )
    sa.Table(
        "canvas_agent_timeline_events",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("thread_id", sa.String(36), nullable=False),
        sa.Column("run_id", sa.String(36)),
        sa.Column("sequence", sa.Integer, nullable=False),
        sa.Column("type", sa.String(80), nullable=False),
        sa.Column("payload_json", sa.JSON, nullable=False),
    )
    metadata.create_all(engine)

    tables = metadata.tables
    with engine.begin() as connection:
        connection.execute(tables["alembic_version"].insert(), {"version_num": "20260518_0032"})
        connection.execute(tables["products"].insert(), {"id": "product-1"})
        connection.execute(
            tables["product_workflows"].insert(),
            {"id": "workflow-1", "product_id": "product-1", "active": True},
        )
        connection.execute(
            tables["workflow_nodes"].insert(),
            {
                "id": "node-1",
                "workflow_id": "workflow-1",
                "node_type": "image_generation",
                "status": "succeeded",
                "config_json": {
                    "api_key": "must-not-enter-archive",
                    "source_url": "https://github.com/example/public-recipe",
                    "signed_url": "https://media.example.test/image.png?signature=secret",
                    "storage_path": "private/products/product-1/source.png",
                },
                "output_json": {
                    "preview": "data:image/png;base64,must-not-enter-archive",
                    "provider_output_json": {"raw": "must-not-enter-archive"},
                },
            },
        )
        connection.execute(
            tables["workflow_edges"].insert(),
            {
                "id": "edge-1",
                "workflow_id": "workflow-1",
                "source_node_id": "node-1",
                "target_node_id": "node-1",
            },
        )
        connection.execute(
            tables["workflow_runs"].insert(),
            {"id": "workflow-run-1", "workflow_id": "workflow-1", "status": "succeeded"},
        )
        connection.execute(
            tables["image_sessions"].insert(),
            {"id": "image-session-1", "product_id": "product-1"},
        )
        connection.execute(
            tables["image_session_assets"].insert(),
            {
                "id": "session-asset-1",
                "session_id": "image-session-1",
                "storage_path": "image_sessions/product-1/reference.png",
            },
        )
        connection.execute(
            tables["workflow_node_runs"].insert(),
            {
                "id": "node-run-1",
                "workflow_run_id": "workflow-run-1",
                "node_id": "node-1",
                "status": "succeeded",
                "image_session_asset_id": "session-asset-1",
            },
        )
        connection.execute(
            tables["source_assets"].insert(),
            {
                "id": "source-1",
                "product_id": "product-1",
                "storage_path": "products/product-1/source.png",
                "source_poster_variant_id": None,
            },
        )
        connection.execute(
            tables["user_canvas_templates"].insert(),
            {
                "id": "template-1",
                "key": "saved-template",
                "schema_version": 1,
                "template_json": {
                    "nodes": [{"id": "saved-node", "local_path": "/private/template.png"}],
                    "edges": [],
                },
                "archived_at": None,
            },
        )
        connection.execute(
            tables["canvas_agent_threads"].insert(),
            {"id": "thread-1", "product_id": "product-1", "status": "active"},
        )
        connection.execute(
            tables["canvas_agent_messages"].insert(),
            [
                {
                    "id": "message-z",
                    "thread_id": "thread-1",
                    "role": "user",
                    "content": "z-first",
                    "metadata_json": {},
                    "created_at": datetime(2026, 8, 15, 1, 0, tzinfo=UTC),
                },
                {
                    "id": "message-a",
                    "thread_id": "thread-1",
                    "role": "assistant",
                    "content": "a-second",
                    "metadata_json": {},
                    "created_at": datetime(2026, 8, 15, 1, 1, tzinfo=UTC),
                },
            ],
        )
        connection.execute(
            tables["canvas_agent_runs"].insert(),
            {
                "id": "agent-run-1",
                "thread_id": "thread-1",
                "status": "needs_approval",
                "current_state": "awaiting_approval",
                "checkpoint_ref": "opaque-checkpoint",
                "goal_state_json": {"objective": "调整主图"},
            },
        )
        connection.execute(
            tables["canvas_agent_tool_events"].insert(),
            {
                "id": "tool-1",
                "run_id": "agent-run-1",
                "tool_name": "get_current_workflow",
                "status": "succeeded",
                "args_json": {},
                "result_json": {"workflow_id": "workflow-1"},
            },
        )
        connection.execute(
            tables["canvas_agent_plans"].insert(),
            {
                "id": "plan-1",
                "run_id": "agent-run-1",
                "status": "pending_approval",
                "applied_workflow_id": None,
                "plan_json": {"intent": "update_node_config"},
                "validation_json": {"ok": True},
            },
        )
        connection.execute(
            tables["canvas_agent_task_plans"].insert(),
            {
                "id": "task-plan-1",
                "thread_id": "thread-1",
                "status": "active",
                "task_plan_json": {"steps": ["inspect", "confirm"]},
            },
        )
        connection.execute(
            tables["canvas_agent_timeline_events"].insert(),
            [
                {
                    "id": "timeline-1",
                    "thread_id": "thread-1",
                    "run_id": "agent-run-1",
                    "sequence": 1,
                    "type": "assistant_delta",
                    "payload_json": {"delta": "正在"},
                },
                {
                    "id": "timeline-2",
                    "thread_id": "thread-1",
                    "run_id": "agent-run-1",
                    "sequence": 2,
                    "type": "approval_requested",
                    "payload_json": {"plan_id": "plan-1"},
                },
            ],
        )

    existing_path = storage_root / "products/product-1/source.png"
    existing_path.parent.mkdir(parents=True)
    existing_path.write_bytes(b"fixture")
    return engine, storage_root


def _current_engine(tmp_path: Path, *, archive_schema: bool = True) -> tuple[sa.Engine, Path]:
    database_path = tmp_path / "current.db"
    storage_root = tmp_path / "current-storage"
    storage_root.mkdir()
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    Base.metadata.create_all(engine)
    with engine.begin() as connection:
        connection.exec_driver_sql("DROP TABLE product_workflows")
        connection.exec_driver_sql(
            "CREATE TABLE product_workflows ("
            "id VARCHAR(36) PRIMARY KEY, product_id VARCHAR(36) NOT NULL, title VARCHAR(255), "
            "active BOOLEAN NOT NULL, schema_version INTEGER NOT NULL, revision INTEGER NOT NULL, "
            "edit_version INTEGER NOT NULL, created_at DATETIME, updated_at DATETIME"
            ")"
        )
        connection.exec_driver_sql(
            "CREATE TABLE source_assets ("
            "id VARCHAR(36) PRIMARY KEY, product_id VARCHAR(36) NOT NULL, "
            "storage_path VARCHAR(500) NOT NULL, canonical_asset_id VARCHAR(36)"
            ")"
        )
        connection.exec_driver_sql(
            "CREATE TABLE poster_variants ("
            "id VARCHAR(36) PRIMARY KEY, product_id VARCHAR(36) NOT NULL, "
            "storage_path VARCHAR(500) NOT NULL, canonical_asset_id VARCHAR(36)"
            ")"
        )
        connection.exec_driver_sql(
            "CREATE TABLE user_canvas_templates ("
            "id VARCHAR(36) PRIMARY KEY, key VARCHAR(80) NOT NULL, schema_version INTEGER NOT NULL, "
            "template_json JSON NOT NULL, archived_at DATETIME"
            ")"
        )
    now = datetime(2026, 8, 15, 4, 0, tzinfo=UTC)

    with engine.begin() as connection:
        connection.exec_driver_sql("CREATE TABLE alembic_version (version_num VARCHAR(32) PRIMARY KEY)")
        revision = "20260815_0041" if archive_schema else "20260814_0038"
        connection.execute(
            sa.text("INSERT INTO alembic_version (version_num) VALUES (:revision)"),
            {"revision": revision},
        )
        connection.execute(
            Base.metadata.tables["products"].insert(),
            {"id": "product-current", "name": "现行商品", "created_at": now, "updated_at": now},
        )
        connection.execute(
            Base.metadata.tables["product_workflows"].insert(),
            {
                "id": "workflow-current",
                "product_id": "product-current",
                "title": "待归档 v1",
                "active": True,
                "schema_version": 1,
                "revision": 1,
                "edit_version": 0,
                "created_at": now,
                "updated_at": now,
            },
        )
        connection.execute(
            Base.metadata.tables["media_objects"].insert(),
            {
                "id": "media-current",
                "storage_path": "media/current.png",
                "mime_type": "image/png",
                "verification_status": "legacy_pending",
                "created_at": now,
            },
        )
        connection.execute(
            Base.metadata.tables["product_image_assets"].insert(),
            {
                "id": "asset-current",
                "product_id": "product-current",
                "media_object_id": "media-current",
                "origin_type": "upload",
                "display_name": "现行参考图",
                "original_filename": "current.png",
                "created_at": now,
                "updated_at": now,
            },
        )
        if not archive_schema:
            connection.exec_driver_sql("DROP TABLE workflow_draft_legacy_archive_seeds")
            connection.exec_driver_sql("DROP TABLE legacy_workflow_archive_assets")
            connection.exec_driver_sql("DROP TABLE legacy_canvas_agent_archives")
            connection.exec_driver_sql("DROP TABLE legacy_user_template_archives")
            connection.exec_driver_sql("DROP TABLE legacy_workflow_archives")

    media_path = storage_root / "media/current.png"
    media_path.parent.mkdir(parents=True)
    media_path.write_bytes(b"fixture")
    return engine, storage_root


def _record_sql(engine: sa.Engine) -> tuple[list[str], object]:
    statements: list[str] = []

    def capture(
        _connection: sa.Connection,
        _cursor: object,
        statement: str,
        _parameters: object,
        _context: object,
        _executemany: bool,
    ) -> None:
        statements.append(statement.strip().lower())

    sa.event.listen(engine, "before_cursor_execute", capture)
    return statements, capture


def test_legacy_canvas_profile_is_stable_read_only_and_counts_hidden_transport_events(tmp_path: Path) -> None:
    engine, storage_root = _legacy_engine(tmp_path)
    statements, listener = _record_sql(engine)
    first_time = datetime(2026, 8, 15, 5, 0, tzinfo=UTC)
    try:
        first = audit_legacy_retirement(engine, storage_root=storage_root, generated_at=first_time)
        second = audit_legacy_retirement(
            engine,
            storage_root=storage_root,
            generated_at=first_time + timedelta(minutes=1),
        )
    finally:
        sa.event.remove(engine, "before_cursor_execute", listener)
        engine.dispose()

    assert first.source.schema_profile == LEGACY_CANVAS_PROFILE
    assert first.source.read_only_enforced is True
    assert first.report_sha256 == second.report_sha256
    assert first.source_fingerprint_sha256 == second.source_fingerprint_sha256
    assert first.workflows.workflow_count == 1
    assert first.workflows.items[0].schema_version is None
    assert first.workflows.items[0].archive_candidate is True
    assert first.user_templates.template_count == 1
    assert first.canvas_agent.thread_count == 1
    assert first.canvas_agent.visible_event_type_counts == {"approval_requested": 1}
    assert first.canvas_agent.technical_event_type_counts == {"assistant_delta": 1}
    assert first.canvas_agent.items[0].runs_without_terminal_evidence == 0
    assert first.media.present_record_count == 1
    assert first.media.missing_record_count == 1
    assert {issue.code for issue in first.issues} == {
        "active_canvas_agent_run",
        "media_file_missing",
        "migration_bridge_required",
    }
    assert first.ready_for_archive is False
    assert not any(
        statement.startswith(("insert ", "update ", "delete ", "create ", "alter ", "drop ", "truncate "))
        for statement in statements
    )


def test_legacy_cutover_preflight_identifies_blocking_canvas_agent_thread(tmp_path: Path) -> None:
    engine, storage_root = _legacy_engine(tmp_path)
    try:
        report = audit_legacy_cutover_preflight(
            engine,
            storage_root=storage_root,
            environment={},
        )
    finally:
        engine.dispose()

    assert report.execution.blocking_workflow_ids == []
    assert report.execution.blocking_canvas_agent_thread_ids == ["thread-1"]
    assert report.execution.active_canvas_agent_run_count == 1
    assert "active_canvas_agent_run" in {issue.code for issue in report.issues}


def test_current_archive_profile_can_audit_v1_candidates_without_canvas_tables(tmp_path: Path) -> None:
    engine, storage_root = _current_engine(tmp_path)
    statements, listener = _record_sql(engine)
    try:
        report = audit_legacy_retirement(engine, storage_root=storage_root)
    finally:
        sa.event.remove(engine, "before_cursor_execute", listener)
        engine.dispose()

    assert report.source.schema_profile == CURRENT_ARCHIVE_PROFILE
    assert report.workflows.workflow_count == 1
    assert report.workflows.archive_candidate_count == 1
    assert report.workflows.items[0].canonical_asset_count == 1
    assert report.canvas_agent.present is False
    assert report.media.declared_record_count == 1
    assert report.media.present_record_count == 1
    assert report.integrity_counts == {key: 0 for key in report.integrity_counts}
    assert report.issues == []
    assert report.ready_for_archive is True
    assert not any(statement.startswith(("insert ", "update ", "delete ")) for statement in statements)


def test_current_cutover_profile_audits_preserved_v1_candidates_after_0042(tmp_path: Path) -> None:
    engine, storage_root = _current_engine(tmp_path)
    with engine.begin() as connection:
        connection.execute(sa.text("UPDATE alembic_version SET version_num = '20260816_0042'"))

    try:
        report = audit_legacy_retirement(engine, storage_root=storage_root)
    finally:
        engine.dispose()

    assert report.source.schema_profile == CURRENT_CUTOVER_PROFILE
    assert report.workflows.workflow_count == 1
    assert report.workflows.archive_candidate_count == 1
    assert report.ready_for_archive is True


def test_current_canonical_profile_rejects_archive_tables_before_their_revision(tmp_path: Path) -> None:
    engine, storage_root = _current_engine(tmp_path, archive_schema=False)
    try:
        canonical = audit_legacy_retirement(engine, storage_root=storage_root)
        with engine.begin() as connection:
            Base.metadata.tables["legacy_workflow_archives"].create(connection)
        drifted = audit_legacy_retirement(engine, storage_root=storage_root)
    finally:
        engine.dispose()

    assert canonical.source.schema_profile == CURRENT_CANONICAL_PROFILE
    assert drifted.source.schema_profile == UNKNOWN_PROFILE
    assert "存在不应属于该 profile 的表: legacy_workflow_archives" in drifted.source.profile_diagnostics


def test_unknown_revision_blocks_without_reading_assumed_source_rows(tmp_path: Path) -> None:
    engine, storage_root = _legacy_engine(tmp_path)
    with engine.begin() as connection:
        connection.exec_driver_sql("UPDATE alembic_version SET version_num = 'unexpected_revision'")

    statements, listener = _record_sql(engine)
    try:
        report = audit_legacy_retirement(engine, storage_root=storage_root)
    finally:
        sa.event.remove(engine, "before_cursor_execute", listener)
        engine.dispose()

    assert report.source.schema_profile == UNKNOWN_PROFILE
    assert report.source.profile_diagnostics == ["Alembic revision 不受支持: unexpected_revision"]
    assert report.source.table_counts["product_workflows"] == 1
    assert report.workflows.workflow_count == 0
    assert [issue.code for issue in report.issues] == ["schema_profile_unknown"]
    assert report.ready_for_archive is False
    assert not any("from workflow_nodes" in statement and "count(" not in statement for statement in statements)
    assert not any(statement.startswith(("insert ", "update ", "delete ")) for statement in statements)


def test_cross_workflow_edges_and_node_runs_are_blocking_integrity_mismatches(tmp_path: Path) -> None:
    engine, storage_root = _legacy_engine(tmp_path)
    metadata = sa.MetaData()
    metadata.reflect(bind=engine)
    tables = metadata.tables
    with engine.begin() as connection:
        connection.execute(tables["products"].insert(), {"id": "product-2"})
        connection.execute(
            tables["product_workflows"].insert(),
            {"id": "workflow-2", "product_id": "product-2", "active": True},
        )
        connection.execute(
            tables["workflow_nodes"].insert(),
            {
                "id": "node-2",
                "workflow_id": "workflow-2",
                "node_type": "reference_image",
                "status": "idle",
            },
        )
        connection.execute(
            tables["workflow_edges"].insert(),
            {
                "id": "edge-cross",
                "workflow_id": "workflow-1",
                "source_node_id": "node-2",
                "target_node_id": "node-1",
            },
        )
        connection.execute(
            tables["workflow_node_runs"].insert(),
            {
                "id": "node-run-cross",
                "workflow_run_id": "workflow-run-1",
                "node_id": "node-2",
                "status": "succeeded",
                "image_session_asset_id": None,
            },
        )

    try:
        report = audit_legacy_retirement(engine, storage_root=storage_root)
    finally:
        engine.dispose()

    assert report.integrity_counts["edge_cross_workflow_source"] == 1
    assert report.integrity_counts["node_run_cross_workflow"] == 1
    integrity_issue = next(issue for issue in report.issues if issue.code == "source_integrity_mismatch")
    assert integrity_issue.severity == "blocking"
    assert integrity_issue.count == 2


def test_invalid_storage_path_is_reported_without_leaving_storage_root(tmp_path: Path) -> None:
    engine, storage_root = _legacy_engine(tmp_path)
    metadata = sa.MetaData()
    metadata.reflect(bind=engine)
    with engine.begin() as connection:
        connection.execute(
            metadata.tables["source_assets"].insert(),
            {
                "id": "source-invalid",
                "product_id": "product-1",
                "storage_path": "../outside.png",
                "source_poster_variant_id": None,
            },
        )

    try:
        report = audit_legacy_retirement(engine, storage_root=storage_root)
    finally:
        engine.dispose()

    assert report.media.invalid_path_record_count == 1
    problem = next(problem for problem in report.media.problems if problem.record_id == "source-invalid")
    assert problem.reason == "invalid_path"
    assert any(issue.code == "invalid_storage_path" and issue.severity == "blocking" for issue in report.issues)


def test_legacy_archive_snapshots_are_stable_and_exclude_canvas_transport_payloads(tmp_path: Path) -> None:
    engine, storage_root = _legacy_engine(tmp_path)
    timestamp = datetime(2026, 8, 15, 12, 0, tzinfo=UTC)
    try:
        workflow_page = export_legacy_archive_page(
            engine,
            storage_root=storage_root,
            kind="workflow",
            limit=1,
            generated_at=timestamp,
        )
        repeated_workflow_page = export_legacy_archive_page(
            engine,
            storage_root=storage_root,
            kind="workflow",
            limit=1,
            generated_at=timestamp + timedelta(minutes=1),
        )
        template_page = export_legacy_archive_page(
            engine,
            storage_root=storage_root,
            kind="user_template",
            limit=1,
            generated_at=timestamp,
        )
        canvas_page = export_legacy_archive_page(
            engine,
            storage_root=storage_root,
            kind="canvas_agent_thread",
            limit=1,
            generated_at=timestamp,
        )
    finally:
        engine.dispose()

    assert workflow_page.source_profile == LEGACY_CANVAS_PROFILE
    assert workflow_page.page_sha256 == repeated_workflow_page.page_sha256
    assert archive_export_page_sha256(workflow_page) == workflow_page.page_sha256
    assert workflow_page.items[0].payload_sha256 == repeated_workflow_page.items[0].payload_sha256
    assert workflow_page.items[0].counts == {
        "asset_declaration_count": 2,
        "edge_count": 1,
        "node_count": 1,
        "node_run_count": 1,
        "run_count": 1,
    }
    assert {item.legacy_source_type for item in workflow_page.items[0].asset_declarations} == {
        "image_session_asset",
        "source_asset",
    }
    assert "storage_path" not in workflow_page.items[0].asset_declarations[0].model_dump()
    assert template_page.items[0].kind == "user_template"
    workflow_payload = workflow_page.items[0].payload_json
    node_config = workflow_payload["nodes"][0]["config_json"]
    node_output = workflow_payload["nodes"][0]["output_json"]
    assert node_config["source_url"] == "https://github.com/example/public-recipe"
    assert node_config["api_key"] == {"_archive_omitted": "sensitive_field"}
    assert node_config["signed_url"] == {"_archive_omitted": "unsafe_url"}
    assert node_config["storage_path"] == {"_archive_omitted": "storage_path"}
    assert node_output["preview"] == {"_archive_omitted": "inline_data"}
    assert node_output["provider_output_json"] == {"_archive_omitted": "sensitive_field"}
    assert template_page.items[0].payload_json["template_json"]["nodes"][0]["local_path"] == {
        "_archive_omitted": "storage_path"
    }
    serialized_payloads = json.dumps(
        [workflow_payload, template_page.items[0].payload_json],
        ensure_ascii=False,
    )
    assert "must-not-enter-archive" not in serialized_payloads
    assert "/private/template.png" not in serialized_payloads

    canvas = canvas_page.items[0]
    assert canvas.kind == "canvas_agent_thread"
    assert canvas.counts["timeline_event_count"] == 2
    assert canvas.counts["visible_event_count"] == 1
    assert canvas.counts["technical_event_count"] == 1
    assert len(canvas.payload_json["technical_events_source_sha256"]) == 64
    assert [message["content"] for message in canvas.payload_json["messages"]] == [
        "z-first",
        "a-second",
    ]
    assert all(
        event["type"] == "approval_requested"
        for event in canvas.payload_json["visible_timeline"]
    )
    assert all("metadata_json" not in message for message in canvas.payload_json["messages"])
    assert all("checkpoint_ref" not in run and "goal_state_json" not in run for run in canvas.payload_json["runs"])
    assert all("args_json" not in tool and "result_json" not in tool for tool in canvas.payload_json["tool_events"])
