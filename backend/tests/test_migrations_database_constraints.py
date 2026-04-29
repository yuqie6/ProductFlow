from __future__ import annotations

from pathlib import Path

import sqlalchemy as sa
from alembic.config import Config

from alembic import command
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import (
    CopyStatus,
    ImageSessionAssetKind,
    JobStatus,
    PosterKind,
    SourceAssetKind,
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRunStatus,
)
from productflow_backend.infrastructure.db.models import (
    CopySet,
    ImageGalleryEntry,
    ImageSessionAsset,
    ImageSessionGenerationTask,
    PosterVariant,
    SourceAsset,
    WorkflowNode,
    WorkflowRun,
    new_id,
    utcnow,
)


def test_sqlalchemy_enum_columns_use_database_values() -> None:
    assert SourceAsset.__table__.c.kind.type.enums == [member.value for member in SourceAssetKind]
    assert ImageSessionAsset.__table__.c.kind.type.enums == [member.value for member in ImageSessionAssetKind]
    assert CopySet.__table__.c.status.type.enums == [member.value for member in CopyStatus]
    assert PosterVariant.__table__.c.kind.type.enums == [member.value for member in PosterKind]
    assert ImageSessionGenerationTask.__table__.c.status.type.enums == [member.value for member in JobStatus]
    assert WorkflowNode.__table__.c.node_type.type.enums == [member.value for member in WorkflowNodeType]
    assert WorkflowNode.__table__.c.status.type.enums == [member.value for member in WorkflowNodeStatus]
    assert WorkflowRun.__table__.c.status.type.enums == [member.value for member in WorkflowRunStatus]


def test_gallery_entry_model_matches_migration_contract() -> None:
    table = ImageGalleryEntry.__table__
    assert table.c.id.type.length == 36
    assert not table.c.id.nullable
    assert table.c.id.default is not None
    assert table.c.id.default.arg.__name__ == new_id.__name__
    assert table.c.image_session_asset_id.type.length == 36
    assert not table.c.image_session_asset_id.nullable
    assert table.c.image_session_round_id.nullable
    assert not table.c.created_at.nullable
    assert table.c.created_at.default is not None
    assert table.c.created_at.default.arg.__name__ == utcnow.__name__
    assert {index.name for index in table.indexes} == {
        "uq_image_gallery_entries_asset_id",
        "ix_image_gallery_entries_round_id",
        "ix_image_gallery_entries_created_at",
    }
    foreign_keys = {fk.parent.name: fk for fk in table.foreign_keys}
    assert foreign_keys["image_session_asset_id"].constraint.name == "fk_image_gallery_entries_image_session_asset_id"
    assert foreign_keys["image_session_asset_id"].ondelete == "CASCADE"
    assert foreign_keys["image_session_round_id"].constraint.name == "fk_image_gallery_entries_image_session_round_id"
    assert foreign_keys["image_session_round_id"].ondelete == "SET NULL"


def test_alembic_upgrade_head_supports_sqlite(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "alembic.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "head")

    assert database_path.exists()
    get_settings.cache_clear()


def test_gallery_migration_schema_and_downgrade_support_sqlite(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "gallery-migration.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "image_gallery_entries" in inspector.get_table_names()
    columns = {column["name"]: column for column in inspector.get_columns("image_gallery_entries")}
    assert columns["id"]["nullable"] is False
    assert columns["image_session_asset_id"]["nullable"] is False
    assert columns["image_session_round_id"]["nullable"] is True
    assert columns["created_at"]["nullable"] is False
    indexes = {index["name"]: index for index in inspector.get_indexes("image_gallery_entries")}
    assert bool(indexes["uq_image_gallery_entries_asset_id"]["unique"])
    assert indexes["uq_image_gallery_entries_asset_id"]["column_names"] == ["image_session_asset_id"]
    assert indexes["ix_image_gallery_entries_round_id"]["column_names"] == ["image_session_round_id"]
    assert indexes["ix_image_gallery_entries_created_at"]["column_names"] == ["created_at"]
    foreign_keys = {tuple(fk["constrained_columns"]): fk for fk in inspector.get_foreign_keys("image_gallery_entries")}
    assert foreign_keys[("image_session_asset_id",)]["referred_table"] == "image_session_assets"
    assert foreign_keys[("image_session_asset_id",)]["options"]["ondelete"] == "CASCADE"
    assert foreign_keys[("image_session_round_id",)]["referred_table"] == "image_session_rounds"
    assert foreign_keys[("image_session_round_id",)]["options"]["ondelete"] == "SET NULL"

    engine.dispose()
    command.downgrade(config, "20260427_0015")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "image_gallery_entries" not in inspector.get_table_names()
    engine.dispose()
    get_settings.cache_clear()


def test_job_runs_drop_migration_and_downgrade_support_sqlite(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "job-runs-drop.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260428_0016")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "job_runs" in inspector.get_table_names()
    assert "uq_job_runs_one_active_per_product_kind" in {
        index["name"] for index in inspector.get_indexes("job_runs")
    }

    engine.dispose()
    command.upgrade(config, "head")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "job_runs" not in inspector.get_table_names()

    engine.dispose()
    command.downgrade(config, "20260428_0016")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "job_runs" in inspector.get_table_names()
    columns = {column["name"]: column for column in inspector.get_columns("job_runs")}
    assert columns["product_id"]["nullable"] is False
    assert columns["kind"]["nullable"] is False
    assert columns["status"]["nullable"] is False
    assert "uq_job_runs_one_active_per_product_kind" in {
        index["name"] for index in inspector.get_indexes("job_runs")
    }

    engine.dispose()
    get_settings.cache_clear()


def test_image_session_generation_progress_migration_and_downgrade_support_sqlite(
    tmp_path: Path,
    monkeypatch,
) -> None:
    database_path = tmp_path / "image-session-progress.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260428_0017")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    now = "2026-04-28 00:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO image_sessions (id, title, created_at, updated_at) "
                "VALUES ('session-1', '迁移会话', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO image_session_generation_tasks "
                "(id, session_id, status, prompt, size, generation_count, created_at, attempts, is_retryable) "
                "VALUES ('task-1', 'session-1', 'running', '旧任务', '1024x1024', 2, :now, 1, 1)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO image_session_generation_tasks "
                "(id, session_id, status, prompt, size, generation_count, created_at, attempts, is_retryable) "
                "VALUES ('failed-task-1', 'session-1', 'failed', '旧失败任务', '1024x1024', 4, :now, 1, 0)"
            ),
            {"now": now},
        )

    engine.dispose()
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    columns = {column["name"]: column for column in inspector.get_columns("image_session_generation_tasks")}
    assert columns["completed_candidates"]["nullable"] is False
    assert columns["completed_candidates"]["default"] is None
    assert columns["active_candidate_index"]["nullable"] is True
    assert columns["progress_phase"]["nullable"] is True
    assert columns["progress_updated_at"]["nullable"] is True
    assert columns["provider_response_id"]["nullable"] is True
    assert columns["provider_response_status"]["nullable"] is True
    assert columns["progress_metadata"]["nullable"] is True
    with engine.connect() as connection:
        completed_candidates = connection.execute(
            sa.text("SELECT completed_candidates FROM image_session_generation_tasks WHERE id = 'task-1'")
        ).scalar_one()
        failed_task_retryable = connection.execute(
            sa.text("SELECT is_retryable FROM image_session_generation_tasks WHERE id = 'failed-task-1'")
        ).scalar_one()
    assert completed_candidates == 0
    assert bool(failed_task_retryable) is True

    engine.dispose()
    command.downgrade(config, "20260428_0017")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    columns = {column["name"] for column in inspector.get_columns("image_session_generation_tasks")}
    assert "completed_candidates" not in columns
    assert "progress_metadata" not in columns

    engine.dispose()
    get_settings.cache_clear()


def test_alembic_upgrade_removes_legacy_workflow_nodes(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "legacy-workflow.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260424_0009")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    now = "2026-04-24 00:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-1', '旧工作流商品', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO product_workflows (id, product_id, title, active, created_at, updated_at) "
                "VALUES ('workflow-1', 'product-1', '旧工作流', 1, :now, :now)"
            ),
            {"now": now},
        )
        for node_id, node_type in (
            ("context-1", "product_context"),
            ("copy-1", "copy_generation"),
            ("legacy-text-1", "legacy_text"),
            ("image-1", "image_generation"),
            ("legacy-result-1", "legacy_result"),
            ("slot-1", "image_upload"),
        ):
            connection.execute(
                sa.text(
                    "INSERT INTO workflow_nodes "
                    "(id, workflow_id, node_type, title, position_x, position_y, config_json, status, "
                    "created_at, updated_at) "
                    "VALUES (:id, 'workflow-1', :node_type, :id, 0, 0, '{}', 'idle', :now, :now)"
                ),
                {"id": node_id, "node_type": node_type, "now": now},
            )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_edges "
                "(id, workflow_id, source_node_id, target_node_id, source_handle, target_handle, created_at) "
                "VALUES "
                "('edge-old-target', 'workflow-1', 'copy-1', 'legacy-text-1', 'output', 'input', :now), "
                "('edge-old-source', 'workflow-1', 'legacy-result-1', 'image-1', 'output', 'input', :now), "
                "('edge-supported', 'workflow-1', 'context-1', 'copy-1', 'output', 'input', :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_runs (id, workflow_id, status, started_at) "
                "VALUES ('run-1', 'workflow-1', 'running', :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_node_runs (id, workflow_run_id, node_id, status, started_at) "
                "VALUES "
                "('node-run-old', 'run-1', 'legacy-text-1', 'succeeded', :now), "
                "('node-run-supported', 'run-1', 'copy-1', 'succeeded', :now)"
            ),
            {"now": now},
        )

    command.upgrade(config, "head")

    with engine.connect() as connection:
        node_types = connection.execute(sa.text("SELECT node_type FROM workflow_nodes ORDER BY id")).scalars().all()
        edge_ids = connection.execute(sa.text("SELECT id FROM workflow_edges ORDER BY id")).scalars().all()
        node_run_ids = connection.execute(sa.text("SELECT id FROM workflow_node_runs ORDER BY id")).scalars().all()

    assert node_types == ["product_context", "copy_generation", "image_generation", "reference_image"]
    assert edge_ids == ["edge-supported"]
    assert node_run_ids == ["node-run-supported"]
    get_settings.cache_clear()
