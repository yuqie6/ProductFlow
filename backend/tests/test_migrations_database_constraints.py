from __future__ import annotations

from pathlib import Path

import sqlalchemy as sa
from alembic.config import Config

from alembic import command
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import (
    CopyStatus,
    ImageSessionAssetKind,
    JobKind,
    JobStatus,
    PosterKind,
    SourceAssetKind,
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRunStatus,
)
from productflow_backend.infrastructure.db.models import (
    CopySet,
    ImageSessionAsset,
    JobRun,
    PosterVariant,
    SourceAsset,
    WorkflowNode,
    WorkflowRun,
)


def test_sqlalchemy_enum_columns_use_database_values() -> None:
    assert SourceAsset.__table__.c.kind.type.enums == [member.value for member in SourceAssetKind]
    assert ImageSessionAsset.__table__.c.kind.type.enums == [member.value for member in ImageSessionAssetKind]
    assert CopySet.__table__.c.status.type.enums == [member.value for member in CopyStatus]
    assert PosterVariant.__table__.c.kind.type.enums == [member.value for member in PosterKind]
    assert JobRun.__table__.c.kind.type.enums == [member.value for member in JobKind]
    assert JobRun.__table__.c.status.type.enums == [member.value for member in JobStatus]
    assert JobRun.__table__.c.target_poster_kind.type.enums == [member.value for member in PosterKind]
    assert WorkflowNode.__table__.c.node_type.type.enums == [member.value for member in WorkflowNodeType]
    assert WorkflowNode.__table__.c.status.type.enums == [member.value for member in WorkflowNodeStatus]
    assert WorkflowRun.__table__.c.status.type.enums == [member.value for member in WorkflowRunStatus]

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
