from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path

import pytest
import sqlalchemy as sa
from alembic.config import Config

from alembic import command
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import (
    AgentSessionStatus,
    AsyncDispatchStatus,
    GraphNodeType,
    ImageSessionAssetKind,
    JobStatus,
    MediaVerificationStatus,
    ProductImageOriginType,
    WorkflowRecipeCreationSource,
    WorkflowRecipeKind,
    WorkflowRecipeOrigin,
)
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentSession,
    AsyncDispatch,
    Base,
    DeliveryRenditionJob,
    ImageSessionAsset,
    ImageSessionGenerationTask,
    MediaObject,
    ProductImageAsset,
    WorkflowGraph,
    WorkflowGraphNode,
    WorkflowGraphNodeRun,
    WorkflowGraphRun,
    WorkflowRecipe,
    WorkflowRecipeVersion,
)

RETIRED_COMPAT_TABLES = {
    "creative_briefs",
    "copy_sets",
    "poster_variants",
    "source_assets",
    "user_canvas_templates",
    "image_gallery_entries",
    "legacy_workflow_archives",
    "legacy_user_template_archives",
    "legacy_canvas_agent_archives",
    "legacy_workflow_archive_assets",
    "workflow_draft_legacy_archive_seeds",
    "workflow_drafts",
    "workflow_draft_revisions",
    "workflow_draft_recipe_seeds",
    "legacy_cutover_gates",
    "media_library_cutover_gates",
}


def _configure_sqlite_alembic(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    *,
    filename: str,
) -> tuple[Path, Config]:
    database_path = tmp_path / filename
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    return database_path, config


def _insert_workflow_media_link_fixture(
    connection: sa.Connection,
    *,
    include_target_graph: bool,
) -> None:
    now = datetime.now(UTC)
    connection.execute(
        sa.text(
            "INSERT INTO products (id, name, created_at, updated_at) "
            "VALUES ('product-media-link', '子图库迁移商品', :now, :now)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO product_workflows "
            "(id, product_id, title, active, schema_version, revision, edit_version, created_at, updated_at) "
            "VALUES ('workflow-media-link', 'product-media-link', '旧工作流', 1, 2, 1, 0, :now, :now)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO media_objects "
            "(id, storage_path, mime_type, verification_status, created_at) "
            "VALUES ('media-media-link', 'migration/media-link.png', 'image/png', 'legacy_pending', :now)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO media_library_assets "
            "(id, media_object_id, source_type, source_id, provenance_json, provenance_hash, revision, "
            "display_name, original_filename, is_archived, created_at, updated_at) "
            "VALUES ('library-media-link', 'media-media-link', 'direct_upload', 'source-media-link', "
            ":provenance, :payload_hash, 1, '迁移素材', 'media-link.png', 0, :now, :now)"
        ),
        {"provenance": "{}", "payload_hash": "a" * 64, "now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO workflow_media_library_assets "
            "(workflow_id, media_library_asset_id, created_at) "
            "VALUES ('workflow-media-link', 'library-media-link', :now)"
        ),
        {"now": now},
    )
    if include_target_graph:
        connection.execute(
            sa.text(
                "INSERT INTO workflow_graphs "
                "(id, product_id, title, active, schema_version, revision, created_at, updated_at) "
                "VALUES ('graph-media-link', 'product-media-link', 'V3 工作流', 1, 3, 1, :now, :now)"
            ),
            {"now": now},
        )


def test_current_enum_columns_use_database_values() -> None:
    enum_contracts = [
        (AgentSession.__table__.c.status, AgentSessionStatus),
        (AsyncDispatch.__table__.c.status, AsyncDispatchStatus),
        (MediaObject.__table__.c.verification_status, MediaVerificationStatus),
        (ProductImageAsset.__table__.c.origin_type, ProductImageOriginType),
        (ImageSessionAsset.__table__.c.kind, ImageSessionAssetKind),
        (WorkflowRecipe.__table__.c.kind, WorkflowRecipeKind),
        (WorkflowRecipe.__table__.c.origin, WorkflowRecipeOrigin),
        (WorkflowRecipeVersion.__table__.c.creation_source, WorkflowRecipeCreationSource),
        (DeliveryRenditionJob.__table__.c.status, JobStatus),
    ]
    for column, enum_cls in enum_contracts:
        if enum_cls is None:
            continue
        assert column.type.enums == [member.value for member in enum_cls]

    assert [member.value for member in GraphNodeType] == [
        "product_source",
        "image_asset",
        "creative_brief",
        "visual_system",
        "prompt_generation",
        "image_generation",
    ]
    assert [member.value for member in MediaVerificationStatus] == ["verified", "missing", "legacy_pending"]
    assert {"upload", "workflow_generation", "image_session_attach", "local_edit"} <= {
        member.value for member in ProductImageOriginType
    }


def test_current_model_metadata_exposes_only_current_runtime_contract() -> None:
    assert RETIRED_COMPAT_TABLES.isdisjoint(Base.metadata.tables)
    assert "current_confirmed_copy_set_id" not in Base.metadata.tables["products"].c
    assert "workflow_drafts" not in Base.metadata.tables
    assert "product_workflows" not in Base.metadata.tables
    assert "workflow_nodes" not in Base.metadata.tables
    assert "workflow_runs" not in Base.metadata.tables
    assert "workflow_draft_id" not in Base.metadata.tables["agent_conversations"].c
    assert "workflow_draft_id" not in Base.metadata.tables["agent_tasks"].c
    assert "workflow_draft_revision_id" not in Base.metadata.tables["agent_turn_projections"].c
    assert "source_draft_revision_id" not in Base.metadata.tables["workflow_graphs"].c
    assert "source_draft_revision_id" not in Base.metadata.tables["visual_system_versions"].c
    assert "source_draft_revision_id" not in Base.metadata.tables["product_fact_set_versions"].c
    assert ImageSessionAsset.__table__.c.media_object_id.nullable is False
    assert ImageSessionGenerationTask.__table__.c.active_attempt_id.nullable is True
    assert "workflow_graphs" in Base.metadata.tables
    assert WorkflowGraph.__table__.c.schema_version.default.arg == 3
    assert WorkflowGraphRun.__table__.c.status.type.length == 40
    assert WorkflowGraphNodeRun.__table__.c.status.type.length == 40
    assert WorkflowGraphNode.__table__.c.node_type.type.length == 40
    scope_check = next(
        constraint
        for constraint in AgentConversation.__table__.constraints
        if constraint.name == "ck_agent_conversations_scope_fields"
    )
    assert "workflow_draft_id" not in str(scope_check.sqltext)
    media_source_check = next(
        constraint
        for constraint in Base.metadata.tables["media_library_assets"].constraints
        if constraint.name == "ck_media_library_assets_source_type"
    )
    assert "legacy_gallery" not in str(media_source_check.sqltext)


def test_alembic_upgrade_head_supports_fresh_sqlite(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    database_path, config = _configure_sqlite_alembic(tmp_path, monkeypatch, filename="fresh-head.db")
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        inspector = sa.inspect(engine)
        tables = set(inspector.get_table_names())
        assert RETIRED_COMPAT_TABLES.isdisjoint(tables)
        assert {
            "products",
            "product_image_assets",
            "media_objects",
            "workflow_graphs",
            "workflow_graph_nodes",
            "provider_profiles",
            "provider_bindings",
            "async_dispatches",
            "media_library_assets",
            "media_library_folders",
            "media_library_tags",
            "media_library_asset_tags",
            "media_library_upload_keys",
            "agent_sessions",
            "agent_tasks",
            "agent_page_context_snapshots",
            "agent_turn_executions",
            "agent_turn_events",
            "agent_turn_effect_reconciliations",
        } <= tables
        execution_columns = {column["name"] for column in inspector.get_columns("agent_turn_executions")}
        assert {
            "turn_projection_id",
            "harness_turn_id",
            "owner_id",
            "lease_token",
            "lease_expires_at",
            "attempt",
            "fencing_token",
            "phase",
            "last_checkpoint_sequence",
            "last_checkpoint_at",
        } <= execution_columns
        execution_checks = {check["name"] for check in inspector.get_check_constraints("agent_turn_executions")}
        assert {
            "ck_agent_turn_executions_non_negative_attempt",
            "ck_agent_turn_executions_non_negative_fencing",
        } <= execution_checks
        checkpoint_tables = set(inspector.get_table_names())
        assert "agent_turn_checkpoints" in checkpoint_tables
        checkpoint_columns = {column["name"] for column in inspector.get_columns("agent_turn_checkpoints")}
        assert {"execution_id", "attempt", "fencing_token", "sequence", "kind", "payload_json"} <= checkpoint_columns
        event_columns = {column["name"] for column in inspector.get_columns("agent_turn_events")}
        assert {
            "turn_projection_id",
            "execution_id",
            "run_id",
            "turn_id",
            "schema_version",
            "sequence",
            "attempt",
            "fencing_token",
            "kind",
            "payload_json",
        } <= event_columns
        reconciliation_columns = {
            column["name"] for column in inspector.get_columns("agent_turn_effect_reconciliations")
        }
        assert {
            "turn_projection_id",
            "tool_call_id",
            "tool_name",
            "idempotency_key",
            "effect_result",
            "reconciliation_state",
            "result_json",
            "detail",
        } <= reconciliation_columns
        assert {
            "product_workflows",
            "workflow_nodes",
            "workflow_edges",
            "workflow_runs",
            "workflow_node_runs",
            "workflow_provider_effects",
            "workflow_materializations",
            "image_prompt_artifacts",
            "visual_exceptions",
            "workflow_image_generation_records",
        }.isdisjoint(tables)
        graph_run_columns = {column["name"] for column in inspector.get_columns("workflow_graph_runs")}
        assert {"graph_id", "status", "run_scope", "graph_revision", "snapshot_json"} <= graph_run_columns
        image_provider_effect_columns = {
            column["name"] for column in inspector.get_columns("image_session_provider_effects")
        }
        assert {
            "generation_task_id",
            "candidate_start_index",
            "candidate_count",
            "operation_key",
            "effect_kind",
            "request_hash",
            "provider_name",
            "attempt_id",
            "effect_result",
            "reconciliation_state",
            "provider_response_id",
            "provider_status",
            "request_json",
            "result_json",
            "detail",
        } <= image_provider_effect_columns
        media_column = next(
            column for column in inspector.get_columns("image_session_assets") if column["name"] == "media_object_id"
        )
        assert media_column["nullable"] is False
        image_task_columns = {
            column["name"]: column for column in inspector.get_columns("image_session_generation_tasks")
        }
        assert image_task_columns["active_attempt_id"]["nullable"] is True
        image_task_checks = {
            check["name"] for check in inspector.get_check_constraints("image_session_generation_tasks")
        }
        assert {
            "ck_image_session_generation_tasks_active_attempt",
            "ck_image_session_generation_tasks_non_negative_attempts",
        } <= image_task_checks
        image_provider_effect_checks = {
            check["name"] for check in inspector.get_check_constraints("image_session_provider_effects")
        }
        assert {
            "ck_image_session_provider_effects_candidate_start",
            "ck_image_session_provider_effects_candidate_count",
            "ck_image_session_provider_effects_effect_result",
            "ck_image_session_provider_effects_reconciliation_state",
            "ck_image_session_provider_effects_request_hash",
        } <= image_provider_effect_checks
        tool_steps_column = next(
            column for column in inspector.get_columns("agent_turn_projections") if column["name"] == "tool_steps_json"
        )
        assert tool_steps_column["nullable"] is False
        assert {"id", "title", "status", "archived_at"} <= {
            column["name"] for column in inspector.get_columns("agent_sessions")
        }
        assert {"summary"} <= {column["name"] for column in inspector.get_columns("agent_sessions")}
        assert {"summary"} <= {column["name"] for column in inspector.get_columns("agent_tasks")}
        assert {"task_id", "page_context_snapshot_id"} <= {
            column["name"] for column in inspector.get_columns("agent_turn_projections")
        }
        assert {"question_answer_json", "continuation_turn_id"} <= {
            column["name"] for column in inspector.get_columns("agent_turn_projections")
        }
        request_columns = {column["name"] for column in inspector.get_columns("agent_workflow_run_requests")}
        assert {"graph_id", "graph_run_id", "source_graph_run_id"} <= request_columns
        assert "workflow_id" not in request_columns
        assert "workflow_run_id" not in request_columns
        assert "source_run_id" not in request_columns
        request_checks = {check["name"] for check in inspector.get_check_constraints("agent_workflow_run_requests")}
        assert "ck_agent_workflow_run_requests_graph_required" in request_checks
        assert "ck_agent_workflow_run_requests_v2_or_v3" not in request_checks
        assert {
            "workflow_graphs",
            "workflow_graph_nodes",
            "workflow_graph_edges",
            "workflow_graph_groups",
            "workflow_operation_groups",
            "workflow_graph_runs",
            "workflow_graph_node_runs",
            "workflow_graph_artifacts",
            "workflow_graph_provider_effects",
        } <= tables
        graph_checks = {check["name"] for check in inspector.get_check_constraints("workflow_graphs")}
        assert "ck_workflow_graphs_schema_version" in graph_checks
        graph_columns = {column["name"] for column in inspector.get_columns("workflow_graph_edges")}
        assert {"data_type", "role", "sort_order"} <= graph_columns
        history_columns = {column["name"] for column in inspector.get_columns("workflow_operation_groups")}
        assert "history_kind" in history_columns
        history_checks = {check["name"] for check in inspector.get_check_constraints("workflow_operation_groups")}
        assert "ck_workflow_operation_groups_history_kind" in history_checks
        recipe_checks = {check["name"] for check in inspector.get_check_constraints("workflow_recipe_versions")}
        assert "ck_workflow_recipe_versions_schema_version" in recipe_checks
        artifact_fks = {fk["name"]: fk for fk in inspector.get_foreign_keys("workflow_graph_artifacts")}
        assert artifact_fks["fk_workflow_graph_artifacts_node_id"]["options"]["ondelete"] == "SET NULL"
        node_run_fks = {fk["name"]: fk for fk in inspector.get_foreign_keys("workflow_graph_node_runs")}
        assert node_run_fks["fk_workflow_graph_node_runs_node_id"]["options"]["ondelete"] == "SET NULL"
        artifact_node_id = next(
            column for column in inspector.get_columns("workflow_graph_artifacts") if column["name"] == "node_id"
        )
        node_run_node_id = next(
            column for column in inspector.get_columns("workflow_graph_node_runs") if column["name"] == "node_id"
        )
        node_run_columns = {column["name"] for column in inspector.get_columns("workflow_graph_node_runs")}
        assert {"active_attempt_id", "progress_phase", "progress_updated_at"} <= node_run_columns
        assert artifact_node_id["nullable"] is True
        assert node_run_node_id["nullable"] is True
        conversation_columns = {column["name"] for column in inspector.get_columns("agent_conversations")}
        assert "workflow_draft_id" not in conversation_columns
        assert "product_id" in conversation_columns
        assert "workflow_draft_id" not in {column["name"] for column in inspector.get_columns("agent_tasks")}
        assert "workflow_draft_revision_id" not in {
            column["name"] for column in inspector.get_columns("agent_turn_projections")
        }
        assert "source_draft_revision_id" not in {column["name"] for column in inspector.get_columns("workflow_graphs")}
        assert "source_draft_revision_id" not in {
            column["name"] for column in inspector.get_columns("visual_system_versions")
        }
        assert "source_draft_revision_id" not in {
            column["name"] for column in inspector.get_columns("product_fact_set_versions")
        }
        assert "current_confirmed_copy_set_id" not in {column["name"] for column in inspector.get_columns("products")}
        conversation_checks = {
            check["name"]: check.get("sqltext") or ""
            for check in inspector.get_check_constraints("agent_conversations")
        }
        assert "ck_agent_conversations_scope_fields" in conversation_checks
        assert "workflow_draft_id" not in conversation_checks["ck_agent_conversations_scope_fields"]
        media_source_checks = {
            check["name"]: check.get("sqltext") or ""
            for check in inspector.get_check_constraints("media_library_assets")
        }
        assert "legacy_gallery" not in media_source_checks["ck_media_library_assets_source_type"]
        assert "direct_upload" in media_source_checks["ck_media_library_assets_source_type"]
        origin_checks = {
            check["name"]: check.get("sqltext") or ""
            for check in inspector.get_check_constraints("product_image_assets")
        }
        origin_sql = " ".join(origin_checks.values())
        assert "legacy_import" not in origin_sql
        now = datetime.now(UTC)
        with engine.begin() as connection:
            connection.execute(
                sa.text(
                    "INSERT INTO products (id, name, created_at, updated_at) "
                    "VALUES ('product-scope', '范围校验商品', :now, :now)"
                ),
                {"now": now},
            )
            connection.execute(
                sa.text(
                    "INSERT INTO agent_conversations "
                    "(id, scope_type, product_id, harness_run_id, status, created_at, updated_at) "
                    "VALUES ('conversation-product', 'product_workflow', 'product-scope', "
                    "'run-product-scope', 'collecting', :now, :now)"
                ),
                {"now": now},
            )
            connection.execute(
                sa.text(
                    "INSERT INTO agent_conversations "
                    "(id, scope_type, product_id, harness_run_id, status, created_at, updated_at) "
                    "VALUES ('conversation-global', 'global', NULL, "
                    "'run-global-scope', 'collecting', :now, :now)"
                ),
                {"now": now},
            )
        with engine.connect() as connection:
            with pytest.raises(sa.exc.IntegrityError):
                with connection.begin():
                    connection.execute(
                        sa.text(
                            "INSERT INTO agent_conversations "
                            "(id, scope_type, product_id, harness_run_id, status, created_at, updated_at) "
                            "VALUES ('conversation-invalid', 'product_workflow', NULL, "
                            "'run-invalid-scope', 'collecting', :now, :now)"
                        ),
                        {"now": now},
                    )
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260827_0094"
    finally:
        engine.dispose()


def test_recipe_schema_v3_migration_blocks_unreadable_v1_payloads_without_deleting(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(tmp_path, monkeypatch, filename="recipe-v1-purge.db")
    command.upgrade(config, "20260822_0081")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    now = datetime.now(UTC)
    try:
        with engine.begin() as connection:
            connection.execute(
                sa.text(
                    "INSERT INTO workflow_recipes "
                    "(id, kind, created_at, updated_at) "
                    "VALUES ('recipe-v1', 'workflow_recipe', :now, :now)"
                ),
                {"now": now},
            )
            connection.execute(
                sa.text(
                    "INSERT INTO workflow_recipe_versions "
                    "(id, recipe_id, version, schema_version, title, payload_json, payload_hash, created_at) "
                    "VALUES ('recipe-v1-version', 'recipe-v1', 1, 1, '旧配方', :payload, :payload_hash, :now)"
                ),
                {"payload": "{}", "payload_hash": "a" * 64, "now": now},
            )
            connection.execute(
                sa.text("UPDATE workflow_recipes SET current_version_id = 'recipe-v1-version' WHERE id = 'recipe-v1'")
            )
    finally:
        engine.dispose()

    with pytest.raises(RuntimeError, match="不会删除配方"):
        command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        with engine.connect() as connection:
            assert connection.scalar(sa.text("SELECT count(*) FROM workflow_recipe_versions")) == 1
            assert connection.scalar(sa.text("SELECT count(*) FROM workflow_recipes")) == 1
            assert (
                connection.scalar(sa.text("SELECT current_version_id FROM workflow_recipes WHERE id = 'recipe-v1'"))
                == "recipe-v1-version"
            )
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260822_0081"
    finally:
        engine.dispose()


def test_workflow_media_link_migration_blocks_unmapped_rows_without_deleting(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(tmp_path, monkeypatch, filename="media-link-block.db")
    command.upgrade(config, "20260821_0078")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        with engine.begin() as connection:
            _insert_workflow_media_link_fixture(connection, include_target_graph=False)
    finally:
        engine.dispose()

    with pytest.raises(RuntimeError, match="不会删除原关联"):
        command.upgrade(config, "20260821_0079")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        with engine.connect() as connection:
            assert connection.execute(
                sa.text("SELECT workflow_id, media_library_asset_id FROM workflow_media_library_assets")
            ).one() == ("workflow-media-link", "library-media-link")
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260821_0078"
    finally:
        engine.dispose()


def test_workflow_media_link_migration_retargets_deterministic_active_graph(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(tmp_path, monkeypatch, filename="media-link-retarget.db")
    command.upgrade(config, "20260821_0078")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        with engine.begin() as connection:
            _insert_workflow_media_link_fixture(connection, include_target_graph=True)
    finally:
        engine.dispose()

    command.upgrade(config, "20260821_0079")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        inspector = sa.inspect(engine)
        workflow_fk = next(
            foreign_key
            for foreign_key in inspector.get_foreign_keys("workflow_media_library_assets")
            if foreign_key["name"] == "fk_workflow_media_library_assets_workflow_id"
        )
        assert workflow_fk["referred_table"] == "workflow_graphs"
        with engine.connect() as connection:
            assert connection.execute(
                sa.text("SELECT workflow_id, media_library_asset_id FROM workflow_media_library_assets")
            ).one() == ("graph-media-link", "library-media-link")
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260821_0079"
    finally:
        engine.dispose()


def test_schema_v2_online_graph_drop_breaks_prompt_artifact_cycle(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(tmp_path, monkeypatch, filename="v2-drop-cycle.db")
    command.upgrade(config, "20260821_0079")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        inspector = sa.inspect(engine)
        node_fks = {fk["name"]: fk for fk in inspector.get_foreign_keys("workflow_nodes")}
        prompt_fk = node_fks["fk_workflow_nodes_current_prompt_artifact_version_id"]
        assert prompt_fk["referred_table"] == "image_prompt_artifact_versions"
        version_fks = {fk["name"]: fk for fk in inspector.get_foreign_keys("image_prompt_artifact_versions")}
        assert version_fks["fk_image_prompt_artifact_versions_source_node_run_id"]["referred_table"] == (
            "workflow_node_runs"
        )
        assert "workflow_nodes" in {fk["referred_table"] for fk in inspector.get_foreign_keys("workflow_node_runs")}
    finally:
        engine.dispose()

    command.upgrade(config, "20260821_0080")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        tables = set(sa.inspect(engine).get_table_names())
        assert {
            "product_workflows",
            "workflow_nodes",
            "workflow_node_runs",
            "image_prompt_artifact_versions",
            "image_prompt_artifacts",
        }.isdisjoint(tables)
        with engine.connect() as connection:
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260821_0080"
    finally:
        engine.dispose()


def test_edge_handle_migration_repairs_existing_v2_reference_edges(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(tmp_path, monkeypatch, filename="edge-handles.db")
    command.upgrade(config, "20260820_0069")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    now = datetime.now(UTC)
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-edge', '端口迁移商品', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO product_workflows "
                "(id, product_id, title, active, schema_version, revision, edit_version, created_at, updated_at) "
                "VALUES ('workflow-edge', 'product-edge', '端口迁移工作流', 1, 2, 1, 0, :now, :now)"
            ),
            {"now": now},
        )
        for node_id, node_key, node_type in (
            ("reference-edge", "reference-edge", "reference_image"),
            ("prompt-edge", "prompt-edge", "prompt_generation"),
        ):
            connection.execute(
                sa.text(
                    "INSERT INTO workflow_nodes "
                    "(id, workflow_id, schema_version, node_key, node_type, title, position_x, position_y, "
                    "config_json, status, created_at, updated_at) "
                    "VALUES (:id, 'workflow-edge', 2, :node_key, :node_type, :node_key, 0, 0, "
                    "'{}', 'idle', :now, :now)"
                ),
                {"id": node_id, "node_key": node_key, "node_type": node_type, "now": now},
            )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_edges "
                "(id, workflow_id, edge_key, source_node_id, target_node_id, source_handle, target_handle, created_at) "
                "VALUES ('edge-reference-prompt', 'workflow-edge', 'edge-reference-prompt', "
                "'reference-edge', 'prompt-edge', 'reference', 'reference', :now)"
            ),
            {"now": now},
        )
    engine.dispose()

    command.upgrade(config, "20260820_0070")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        with engine.connect() as connection:
            handles = connection.execute(
                sa.text("SELECT source_handle, target_handle FROM workflow_edges WHERE id = 'edge-reference-prompt'")
            ).one()
            assert handles == ("asset", "reference")
    finally:
        engine.dispose()


def test_attempt_fencing_migration_requeues_existing_running_work(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(tmp_path, monkeypatch, filename="attempt-fencing.db")
    command.upgrade(config, "20260816_0043")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    now = "2026-08-16 10:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO image_sessions (id, title, created_at, updated_at) "
                "VALUES ('session-attempt-migration', 'Attempt migration', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO image_session_generation_tasks "
                "(id, session_id, status, prompt, size, generation_count, completed_candidates, "
                "created_at, started_at, attempts, is_retryable) VALUES "
                "('image-task-attempt-migration', 'session-attempt-migration', 'running', "
                "'resume safely', '1024x1024', 1, 0, :now, :now, 1, 1)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-attempt-migration', 'Attempt product', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO product_workflows "
                "(id, product_id, title, active, schema_version, revision, edit_version, created_at, updated_at) "
                "VALUES ('workflow-attempt-migration', 'product-attempt-migration', 'Attempt workflow', "
                "1, 2, 1, 0, :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_nodes "
                "(id, workflow_id, schema_version, node_key, node_type, title, position_x, position_y, "
                "config_json, status, created_at, updated_at) VALUES "
                "('node-attempt-migration', 'workflow-attempt-migration', 2, 'prompt', "
                "'prompt_generation', 'Prompt', 0, 0, '{}', 'running', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_runs (id, workflow_id, status, started_at) "
                "VALUES ('run-attempt-migration', 'workflow-attempt-migration', 'running', :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_node_runs "
                "(id, workflow_run_id, node_id, status, started_at) VALUES "
                "('node-run-attempt-migration', 'run-attempt-migration', "
                "'node-attempt-migration', 'running', :now)"
            ),
            {"now": now},
        )
    engine.dispose()

    command.upgrade(config, "20260816_0044")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        with engine.connect() as connection:
            assert connection.execute(
                sa.text(
                    "SELECT status, active_attempt_id, started_at, progress_phase "
                    "FROM image_session_generation_tasks WHERE id = 'image-task-attempt-migration'"
                )
            ).one() == ("queued", None, None, "requeued_after_attempt_fencing_migration")
            assert connection.execute(
                sa.text(
                    "SELECT status, attempts, active_attempt_id FROM workflow_node_runs "
                    "WHERE id = 'node-run-attempt-migration'"
                )
            ).one() == ("queued", 0, None)
            assert (
                connection.scalar(sa.text("SELECT status FROM workflow_nodes WHERE id = 'node-attempt-migration'"))
                == "queued"
            )
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260816_0044"
    finally:
        engine.dispose()


def test_agent_tool_step_projection_migration_backfills_existing_turns(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(tmp_path, monkeypatch, filename="tool-steps.db")
    command.upgrade(config, "20260816_0042")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    now = "2026-08-16 10:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-tool-step', '工具步骤商品', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_drafts "
                "(id, product_id, status, created_at, updated_at) "
                "VALUES ('draft-tool-step', 'product-tool-step', 'collecting', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO agent_conversations "
                "(id, product_id, workflow_draft_id, harness_run_id, status, created_at, updated_at) "
                "VALUES ('conversation-tool-step', 'product-tool-step', 'draft-tool-step', "
                "'run-tool-step', 'collecting', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO agent_turn_projections "
                "(id, conversation_id, idempotency_key, request_hash, input_text, input_asset_ids_json, "
                "status, resume_required, created_at, updated_at) "
                "VALUES ('turn-tool-step', 'conversation-tool-step', 'tool-step-key', :request_hash, "
                "'inspect', '[]', 'succeeded', 0, :now, :now)"
            ),
            {"request_hash": "a" * 64, "now": now},
        )
    engine.dispose()

    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        with engine.connect() as connection:
            value = connection.scalar(
                sa.text("SELECT tool_steps_json FROM agent_turn_projections WHERE id = 'turn-tool-step'")
            )
            assert value == "[]"
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260827_0094"
    finally:
        engine.dispose()


@pytest.mark.parametrize(
    ("media_object_id", "carrier_path", "carrier_mime", "error_match"),
    [
        (None, "media/missing.png", "image/png", "no canonical MediaObject"),
        ("media-orphan", "media/orphan.png", "image/png", "no canonical MediaObject"),
        ("media-drift", "media/stale.jpg", "image/jpeg", "carrier drift"),
    ],
)
def test_media_authority_migration_rejects_missing_or_drifted_carriers(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    media_object_id: str | None,
    carrier_path: str,
    carrier_mime: str,
    error_match: str,
) -> None:
    _, config = _configure_sqlite_alembic(tmp_path, monkeypatch, filename=f"media-drift-{error_match}.db")
    command.upgrade(config, "20260816_0042")

    engine = sa.create_engine(get_settings().database_url, future=True)
    try:
        with engine.begin() as connection:
            connection.execute(
                sa.text(
                    "INSERT INTO image_sessions (id, title, created_at, updated_at) "
                    "VALUES ('session-drift', 'Media drift', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)"
                )
            )
            if media_object_id == "media-drift":
                connection.execute(
                    sa.text(
                        "INSERT INTO media_objects "
                        "(id, storage_path, mime_type, verification_status, created_at) "
                        "VALUES (:id, 'media/canonical.png', 'image/png', 'legacy_pending', CURRENT_TIMESTAMP)"
                    ),
                    {"id": media_object_id},
                )
            connection.execute(
                sa.text(
                    "INSERT INTO image_session_assets "
                    "(id, session_id, kind, original_filename, mime_type, storage_path, media_object_id, created_at) "
                    "VALUES ('session-asset-drift', 'session-drift', 'reference_upload', 'reference.png', "
                    ":mime_type, :storage_path, :media_object_id, CURRENT_TIMESTAMP)"
                ),
                {
                    "mime_type": carrier_mime,
                    "storage_path": carrier_path,
                    "media_object_id": media_object_id,
                },
            )
    finally:
        engine.dispose()

    with pytest.raises(RuntimeError, match=error_match):
        command.upgrade(config, "head")


def test_media_library_upload_keys_migration_upgrade_and_downgrade(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """0061 的 upload-keys 表可来回升级；0060 的 SQLite 降级会丢掉 source_run_id。"""
    database_path, config = _configure_sqlite_alembic(tmp_path, monkeypatch, filename="upload-keys.db")
    command.upgrade(config, "20260821_0079")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        assert "media_library_upload_keys" in sa.inspect(engine).get_table_names()
        with engine.connect() as connection:
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260821_0079"
    finally:
        engine.dispose()

    # 一步降到 0060 会删掉 upload-keys 表
    command.downgrade(config, "20260819_0060")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        assert "media_library_upload_keys" not in sa.inspect(engine).get_table_names()
        with engine.connect() as connection:
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260819_0060"
    finally:
        engine.dispose()

    # 再降到 0058 会跑 0060 和 0059 的降级，并丢掉 source_run_id
    command.downgrade(config, "20260818_0058")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        source_run_columns = {
            column["name"] for column in sa.inspect(engine).get_columns("agent_workflow_run_requests")
        }
        assert "source_run_id" not in source_run_columns
        with engine.connect() as connection:
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260818_0058"
    finally:
        engine.dispose()

    # 再完整升级应全部恢复（round-trip）
    command.upgrade(config, "head")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        assert "media_library_upload_keys" in sa.inspect(engine).get_table_names()
        source_run_columns = {
            column["name"] for column in sa.inspect(engine).get_columns("agent_workflow_run_requests")
        }
        assert "source_run_id" not in source_run_columns
        assert "graph_id" in source_run_columns
        with engine.connect() as connection:
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260827_0094"
    finally:
        engine.dispose()
