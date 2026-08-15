from __future__ import annotations

from pathlib import Path

import pytest
import sqlalchemy as sa
from alembic.config import Config

from alembic import command
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import (
    ImageSessionAssetKind,
    JobStatus,
    MediaVerificationStatus,
    ProductImageOriginType,
    WorkflowDraftStatus,
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRecipeKind,
    WorkflowRevealEventKind,
    WorkflowRunStatus,
)
from productflow_backend.infrastructure.db.models import (
    Base,
    DeliveryRenditionJob,
    ImageSessionAsset,
    MediaObject,
    ProductImageAsset,
    ProductWorkflow,
    WorkflowDraft,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRecipe,
    WorkflowRevealEvent,
    WorkflowRun,
)

LEGACY_SOURCE_TABLES = {
    "creative_briefs",
    "copy_sets",
    "poster_variants",
    "source_assets",
    "user_canvas_templates",
}
ARCHIVE_TABLES = {
    "legacy_workflow_archives",
    "legacy_user_template_archives",
    "legacy_canvas_agent_archives",
    "legacy_workflow_archive_assets",
    "workflow_draft_legacy_archive_seeds",
}
CUTOVER_GATE_TABLE = "legacy_cutover_gates"


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


def test_current_enum_columns_use_database_values() -> None:
    enum_contracts = [
        (MediaObject.__table__.c.verification_status, MediaVerificationStatus),
        (ProductImageAsset.__table__.c.origin_type, ProductImageOriginType),
        (ImageSessionAsset.__table__.c.kind, ImageSessionAssetKind),
        (ProductWorkflow.__table__.c.schema_version, None),
        (WorkflowNode.__table__.c.node_type, WorkflowNodeType),
        (WorkflowNode.__table__.c.status, WorkflowNodeStatus),
        (WorkflowRun.__table__.c.status, WorkflowRunStatus),
        (WorkflowDraft.__table__.c.status, WorkflowDraftStatus),
        (WorkflowRecipe.__table__.c.kind, WorkflowRecipeKind),
        (WorkflowRevealEvent.__table__.c.kind, WorkflowRevealEventKind),
        (DeliveryRenditionJob.__table__.c.status, JobStatus),
    ]
    for column, enum_cls in enum_contracts:
        if enum_cls is None:
            continue
        assert column.type.enums == [member.value for member in enum_cls]

    assert [member.value for member in WorkflowNodeType] == [
        "product_context",
        "reference_image",
        "prompt_generation",
        "image_generation",
    ]
    assert [member.value for member in MediaVerificationStatus] == ["verified", "missing", "legacy_pending"]
    assert [member.value for member in ProductImageOriginType] == [
        "upload",
        "workflow_generation",
        "image_session_attach",
        "legacy_import",
    ]


def test_current_model_metadata_exposes_only_current_runtime_and_archive_contract() -> None:
    assert LEGACY_SOURCE_TABLES.isdisjoint(Base.metadata.tables)
    assert ARCHIVE_TABLES <= Base.metadata.tables.keys()
    assert "current_confirmed_copy_set_id" not in ProductWorkflow.metadata.tables["products"].c
    assert "copy_set_id" not in WorkflowNodeRun.__table__.c
    assert "poster_variant_id" not in WorkflowNodeRun.__table__.c
    assert ImageSessionAsset.__table__.c.media_object_id.nullable is False
    assert ProductWorkflow.__table__.c.schema_version.default.arg == 2
    assert WorkflowNode.__table__.c.schema_version.default.arg == 2


def test_alembic_upgrade_head_supports_fresh_sqlite(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    database_path, config = _configure_sqlite_alembic(tmp_path, monkeypatch, filename="fresh-head.db")
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        inspector = sa.inspect(engine)
        tables = set(inspector.get_table_names())
        assert LEGACY_SOURCE_TABLES <= tables
        assert ARCHIVE_TABLES <= tables
        assert CUTOVER_GATE_TABLE in tables
        assert {
            "products",
            "product_image_assets",
            "media_objects",
            "product_workflows",
            "workflow_nodes",
            "workflow_drafts",
            "provider_profiles",
            "provider_bindings",
        } <= tables
        assert {"copy_set_id", "poster_variant_id"} <= {
            column["name"] for column in inspector.get_columns("workflow_node_runs")
        }
        media_column = next(
            column for column in inspector.get_columns("image_session_assets") if column["name"] == "media_object_id"
        )
        assert media_column["nullable"] is True
        with engine.connect() as connection:
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260816_0042"
    finally:
        engine.dispose()


def test_cutover_migration_preserves_legacy_data_and_installs_pending_gate(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(tmp_path, monkeypatch, filename="cleanup.db")
    command.upgrade(config, "20260815_0041")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    now = "2026-08-16 10:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO app_settings (key, value, created_at, updated_at) "
                "VALUES ('text_provider_kind', 'mock', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO app_settings (key, value, created_at, updated_at) "
                "VALUES ('image_tool_allowed_fields', 'quality,n,partial_images', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO provider_bindings "
                "(id, purpose, provider_kind, provider_profile_id, model_settings_json, config_json, "
                "created_at, updated_at) "
                "VALUES ('binding-text', 'text', 'mock', NULL, '{}', '{}', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-old', '旧运行时商品', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO media_objects "
                "(id, storage_path, mime_type, byte_size, width, height, sha256, verification_status, "
                "created_at, verified_at) "
                "VALUES ('media-old', 'products/old.png', 'image/png', NULL, NULL, NULL, NULL, "
                "'legacy_pending', :now, NULL)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO product_image_assets "
                "(id, product_id, media_object_id, origin_type, display_name, original_filename, "
                "parent_asset_id, source_image_session_asset_id, created_at, updated_at) "
                "VALUES ('asset-old', 'product-old', 'media-old', 'legacy_import', '旧图片', 'old.png', "
                "NULL, NULL, :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO product_workflows "
                "(id, product_id, title, active, schema_version, revision, edit_version, created_at, updated_at) "
                "VALUES ('workflow-v1', 'product-old', '旧工作流', 1, 1, 1, 0, :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_nodes "
                "(id, workflow_id, schema_version, node_key, node_type, title, position_x, position_y, "
                "config_json, status, output_json, failure_reason, last_run_at, folder_id, "
                "bound_image_asset_id, current_prompt_artifact_version_id, created_at, updated_at) "
                "VALUES ('node-copy-v1', 'workflow-v1', 1, NULL, 'copy_generation', '旧文案', 0, 0, "
                "'{}', 'idle', NULL, NULL, NULL, NULL, NULL, NULL, :now, :now)"
            ),
            {"now": now},
        )
    engine.dispose()

    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        inspector = sa.inspect(engine)
        tables = set(inspector.get_table_names())
        assert LEGACY_SOURCE_TABLES <= tables
        assert ARCHIVE_TABLES <= tables
        assert CUTOVER_GATE_TABLE in tables
        assert "current_confirmed_copy_set_id" in {
            column["name"] for column in inspector.get_columns("products")
        }
        assert {"copy_set_id", "poster_variant_id"} <= {
            column["name"] for column in inspector.get_columns("workflow_node_runs")
        }
        with engine.begin() as connection:
            assert connection.scalar(
                sa.text("SELECT COUNT(*) FROM provider_bindings WHERE purpose = 'text'")
            ) == 1
            assert connection.scalar(
                sa.text("SELECT COUNT(*) FROM app_settings WHERE key = 'text_provider_kind'")
            ) == 1
            assert connection.scalar(
                sa.text("SELECT value FROM app_settings WHERE key = 'image_tool_allowed_fields'")
            ) == "quality,n,partial_images"
            assert connection.scalar(sa.text("SELECT COUNT(*) FROM product_workflows WHERE id = 'workflow-v1'")) == 1
            assert connection.scalar(sa.text("SELECT COUNT(*) FROM workflow_nodes WHERE id = 'node-copy-v1'")) == 1
            assert connection.scalar(
                sa.text("SELECT verification_status FROM media_objects WHERE id = 'media-old'")
            ) == "legacy_pending"
            assert connection.scalar(
                sa.text("SELECT origin_type FROM product_image_assets WHERE id = 'asset-old'")
            ) == "legacy_import"
            assert connection.execute(
                sa.text(
                    "INSERT INTO product_workflows "
                    "(id, product_id, title, active, schema_version, revision, edit_version, "
                    "created_at, updated_at) "
                    "VALUES ('workflow-invalid', 'product-old', '旧兼容工作流', 0, 1, 1, 0, :now, :now)"
                ),
                {"now": now},
            )
            gate = connection.execute(
                sa.text(
                    "SELECT phase, active_run_count, source_report_sha256, archive_report_sha256, "
                    "canonical_report_sha256, backup_restore_verified_at "
                    "FROM legacy_cutover_gates WHERE id = 'singleton'"
                )
            ).one()
            assert tuple(gate) == ("pending", 0, None, None, None, None)
    finally:
        engine.dispose()
