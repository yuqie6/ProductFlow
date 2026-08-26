from __future__ import annotations

import logging
from collections.abc import Iterator
from pathlib import Path

import pytest
import sqlalchemy as sa
from alembic.config import Config
from helpers import _make_demo_image_bytes
from sqlalchemy.exc import IntegrityError

from alembic import command
from productflow_backend.application.product_images.assets import (
    clear_product_cover,
    delete_product_image_asset,
)
from productflow_backend.application.products import create_canonical_product
from productflow_backend.config import get_settings
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import (
    LegacyCanvasAgentArchive,
    LegacyUserTemplateArchive,
    LegacyWorkflowArchive,
    LegacyWorkflowArchiveAsset,
    ProductImageAsset,
    WorkflowDraftLegacyArchiveSeed,
)


@pytest.fixture(autouse=True)
def _restore_logger_disabled_flags() -> Iterator[None]:
    # Alembic 进程内 fileConfig 会关掉已有的非 Alembic logger。
    logger_states = {
        logger: logger.disabled
        for logger in logging.root.manager.loggerDict.values()
        if isinstance(logger, logging.Logger)
    }
    try:
        yield
    finally:
        for logger, disabled in logger_states.items():
            logger.disabled = disabled


def _constraint_names(table: sa.Table, kind: type[sa.Constraint]) -> set[str | None]:
    return {constraint.name for constraint in table.constraints if isinstance(constraint, kind)}


def _migration_config(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> tuple[Path, Config]:
    database_path = tmp_path / "legacy-archive-migration.db"
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


def test_legacy_archive_models_match_immutable_snapshot_contract() -> None:
    workflow_table = LegacyWorkflowArchive.__table__
    assert _constraint_names(workflow_table, sa.UniqueConstraint) == {
        "uq_legacy_workflow_archives_source_id"
    }
    assert _constraint_names(workflow_table, sa.CheckConstraint) == {
        "ck_legacy_workflow_archives_schema_version",
        "ck_legacy_workflow_archives_source_hash",
        "ck_legacy_workflow_archives_payload_hash",
        "ck_legacy_workflow_archives_counts",
    }
    assert {index.name for index in workflow_table.indexes} == {
        "ix_legacy_workflow_archives_product_created"
    }
    assert "updated_at" not in workflow_table.c
    workflow_product_fk = next(fk for fk in workflow_table.foreign_keys if fk.parent.name == "product_id")
    assert workflow_product_fk.constraint.name == "fk_legacy_workflow_archives_product_id"
    assert workflow_product_fk.ondelete == "CASCADE"

    asset_table = LegacyWorkflowArchiveAsset.__table__
    assert _constraint_names(asset_table, sa.UniqueConstraint) == {
        "uq_legacy_workflow_archive_assets_identity"
    }
    archive_fk = next(fk for fk in asset_table.foreign_keys if fk.parent.name == "archive_id")
    asset_fk = next(fk for fk in asset_table.foreign_keys if fk.parent.name == "product_image_asset_id")
    assert archive_fk.constraint.name == "fk_legacy_workflow_archive_assets_archive_id"
    assert archive_fk.ondelete == "CASCADE"
    assert asset_fk.constraint.name == "fk_legacy_workflow_archive_assets_asset_id"
    assert asset_fk.ondelete == "RESTRICT"

    template_table = LegacyUserTemplateArchive.__table__
    assert _constraint_names(template_table, sa.UniqueConstraint) == {
        "uq_legacy_user_template_archives_source_id"
    }
    assert _constraint_names(template_table, sa.CheckConstraint) == {
        "ck_legacy_user_template_archives_schema_version",
        "ck_legacy_user_template_archives_status",
        "ck_legacy_user_template_archives_source_hash",
        "ck_legacy_user_template_archives_payload_hash",
    }
    assert "updated_at" not in template_table.c

    canvas_table = LegacyCanvasAgentArchive.__table__
    assert _constraint_names(canvas_table, sa.UniqueConstraint) == {
        "uq_legacy_canvas_agent_archives_source_id"
    }
    assert _constraint_names(canvas_table, sa.CheckConstraint) == {
        "ck_legacy_canvas_agent_archives_schema_version",
        "ck_legacy_canvas_agent_archives_source_hash",
        "ck_legacy_canvas_agent_archives_payload_hash",
        "ck_legacy_canvas_agent_archives_counts",
        "ck_legacy_canvas_agent_archives_event_total",
    }
    assert "updated_at" not in canvas_table.c

    rebuild_seed_table = WorkflowDraftLegacyArchiveSeed.__table__
    assert _constraint_names(rebuild_seed_table, sa.UniqueConstraint) == {
        "uq_workflow_draft_legacy_archive_seeds_draft_id",
        "uq_workflow_draft_legacy_archive_seeds_product_key",
    }
    assert _constraint_names(rebuild_seed_table, sa.CheckConstraint) == {
        "ck_workflow_draft_legacy_archive_seeds_schema_version",
        "ck_workflow_draft_legacy_archive_seeds_request_hash",
        "ck_workflow_draft_legacy_archive_seeds_one_archive",
    }
    assert {index.name for index in rebuild_seed_table.indexes} == {
        "ix_workflow_draft_legacy_archive_seeds_product_created"
    }
    seed_fks = {foreign_key.parent.name: foreign_key for foreign_key in rebuild_seed_table.foreign_keys}
    assert seed_fks["workflow_draft_id"].constraint.name == "fk_workflow_draft_legacy_archive_seeds_draft_id"
    assert seed_fks["workflow_draft_id"].ondelete == "CASCADE"
    assert seed_fks["product_id"].constraint.name == "fk_workflow_draft_legacy_archive_seeds_product_id"
    assert seed_fks["product_id"].ondelete == "CASCADE"
    assert seed_fks["workflow_archive_id"].ondelete == "RESTRICT"
    assert seed_fks["canvas_agent_archive_id"].ondelete == "RESTRICT"
    assert seed_fks["user_template_archive_id"].ondelete == "RESTRICT"


def test_legacy_workflow_archive_reference_blocks_image_deletion(configured_env: Path, db_session) -> None:
    product = create_canonical_product(
        db_session,
        name="旧工作流归档商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "legacy-reference.png", "image/png")],
    )
    asset = product.image_assets[0]
    clear_product_cover(db_session, product_id=product.id)
    archive = LegacyWorkflowArchive(
        source_profile="legacy_canvas_agent_20260518_0032",
        legacy_workflow_id="legacy-workflow-1",
        product_id=product.id,
        source_title="旧主图工作流",
        source_updated_at=None,
        archive_schema_version=1,
        payload_json={"nodes": [], "edges": []},
        source_fingerprint_sha256="a" * 64,
        payload_sha256="b" * 64,
        node_count=0,
        edge_count=0,
        run_count=0,
        node_run_count=0,
        asset_count=1,
    )
    archive.assets.append(
        LegacyWorkflowArchiveAsset(
            asset=asset,
            role="reference",
            legacy_source_type="source_asset",
            legacy_source_id="legacy-source-1",
        )
    )
    db_session.add(archive)
    db_session.commit()

    with pytest.raises(ConflictError, match="旧工作流归档"):
        delete_product_image_asset(db_session, asset_id=asset.id)

    db_session.delete(archive)
    db_session.commit()
    deleted_product_id = delete_product_image_asset(db_session, asset_id=asset.id)

    assert deleted_product_id == product.id
    assert db_session.get(ProductImageAsset, asset.id) is None


def test_canvas_archive_event_totals_are_database_enforced(db_session) -> None:
    product = create_canonical_product(
        db_session,
        name="旧 Agent 归档商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "agent-reference.png", "image/png")],
    )
    db_session.add(
        LegacyCanvasAgentArchive(
            source_profile="legacy_canvas_agent_20260518_0032",
            legacy_thread_id="legacy-thread-invalid",
            product_id=product.id,
            title="旧 Agent 对话",
            source_status="active",
            source_updated_at=None,
            archive_schema_version=1,
            payload_json={"messages": []},
            source_fingerprint_sha256="c" * 64,
            payload_sha256="d" * 64,
            message_count=0,
            run_count=0,
            tool_event_count=0,
            plan_count=0,
            task_plan_count=0,
            timeline_event_count=1,
            visible_event_count=1,
            technical_event_count=1,
        )
    )

    with pytest.raises(IntegrityError):
        db_session.commit()
    db_session.rollback()


def test_legacy_archive_migration_round_trips_and_refuses_populated_downgrade(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _migration_config(tmp_path, monkeypatch)
    command.upgrade(config, "20260814_0038")
    now = "2026-08-15 08:00:00"
    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-archive', '归档迁移商品', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO media_objects "
                "(id, storage_path, mime_type, verification_status, created_at) "
                "VALUES ('media-archive', 'media/archive.png', 'image/png', 'legacy_pending', :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO product_image_assets "
                "(id, product_id, media_object_id, origin_type, display_name, original_filename, "
                "created_at, updated_at) "
                "VALUES ('asset-archive', 'product-archive', 'media-archive', 'upload', "
                "'归档图片', 'archive.png', :now, :now)"
            ),
            {"now": now},
        )
    engine.dispose()

    command.upgrade(config, "20260815_0039")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert {
        "legacy_workflow_archives",
        "legacy_workflow_archive_assets",
        "legacy_user_template_archives",
        "legacy_canvas_agent_archives",
    } <= set(inspector.get_table_names())
    archive_asset_fks = {fk["name"]: fk for fk in inspector.get_foreign_keys("legacy_workflow_archive_assets")}
    assert archive_asset_fks["fk_legacy_workflow_archive_assets_archive_id"]["options"]["ondelete"] == "CASCADE"
    assert archive_asset_fks["fk_legacy_workflow_archive_assets_asset_id"]["options"]["ondelete"] == "RESTRICT"

    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO legacy_workflow_archives "
                "(id, source_profile, legacy_workflow_id, product_id, source_title, source_updated_at, "
                "archive_schema_version, payload_json, source_fingerprint_sha256, payload_sha256, "
                "node_count, edge_count, run_count, node_run_count, asset_count, created_at) "
                "VALUES ('archive-1', 'legacy_canvas_agent_20260518_0032', 'workflow-old', 'product-archive', "
                "'旧工作流', NULL, 1, '{}', :source_hash, :payload_hash, 0, 0, 0, 0, 1, :now)"
            ),
            {"source_hash": "a" * 64, "payload_hash": "b" * 64, "now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO legacy_workflow_archive_assets "
                "(id, archive_id, product_image_asset_id, role, legacy_source_type, legacy_source_id, created_at) "
                "VALUES ('archive-asset-1', 'archive-1', 'asset-archive', 'reference', "
                "'source_asset', 'source-old', :now)"
            ),
            {"now": now},
        )

    with pytest.raises(IntegrityError):
        with engine.begin() as connection:
            connection.exec_driver_sql("PRAGMA foreign_keys = ON")
            connection.execute(sa.text("DELETE FROM product_image_assets WHERE id = 'asset-archive'"))
    engine.dispose()

    with pytest.raises(RuntimeError, match="archive data exists"):
        command.downgrade(config, "20260814_0038")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        connection.execute(sa.text("DELETE FROM legacy_workflow_archive_assets"))
        connection.execute(sa.text("DELETE FROM legacy_workflow_archives"))
    engine.dispose()

    command.downgrade(config, "20260814_0038")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "legacy_workflow_archives" not in inspector.get_table_names()
    with engine.connect() as connection:
        assert connection.scalar(
            sa.text("SELECT COUNT(*) FROM product_image_assets WHERE id = 'asset-archive'")
        ) == 1
    engine.dispose()

    command.upgrade(config, "20260815_0039")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    assert "legacy_workflow_archives" in sa.inspect(engine).get_table_names()
    engine.dispose()
    get_settings.cache_clear()


def test_legacy_archive_rebuild_seed_migration_is_additive_and_refuses_data_loss(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _migration_config(tmp_path, monkeypatch)
    command.upgrade(config, "20260815_0040")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "workflow_draft_legacy_archive_seeds" in inspector.get_table_names()
    foreign_keys = {
        foreign_key["name"]: foreign_key
        for foreign_key in inspector.get_foreign_keys("workflow_draft_legacy_archive_seeds")
    }
    assert foreign_keys["fk_workflow_draft_legacy_archive_seeds_draft_id"]["options"]["ondelete"] == "CASCADE"
    workflow_archive_fk = foreign_keys["fk_workflow_draft_legacy_archive_seeds_workflow_archive_id"]
    assert workflow_archive_fk["options"]["ondelete"] == "RESTRICT"
    now = "2026-08-15 09:30:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-rebuild', '重建迁移商品', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_drafts (id, product_id, status, created_at, updated_at) "
                "VALUES ('draft-rebuild', 'product-rebuild', 'collecting', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO legacy_workflow_archives "
                "(id, source_profile, legacy_workflow_id, product_id, source_title, source_updated_at, "
                "archive_schema_version, payload_json, source_fingerprint_sha256, payload_sha256, "
                "node_count, edge_count, run_count, node_run_count, asset_count, created_at) "
                "VALUES ('archive-rebuild', 'legacy_canvas_agent_20260518_0032', 'workflow-rebuild', "
                "'product-rebuild', '待重建设计', NULL, 1, '{}', :source_hash, :payload_hash, "
                "0, 0, 0, 0, 0, :now)"
            ),
            {"source_hash": "a" * 64, "payload_hash": "b" * 64, "now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_draft_legacy_archive_seeds "
                "(id, workflow_draft_id, product_id, workflow_archive_id, canvas_agent_archive_id, "
                "user_template_archive_id, schema_version, idempotency_key, request_hash, created_at) "
                "VALUES ('seed-rebuild', 'draft-rebuild', 'product-rebuild', 'archive-rebuild', NULL, NULL, "
                "1, 'migration-rebuild', :request_hash, :now)"
            ),
            {"request_hash": "c" * 64, "now": now},
        )
    engine.dispose()

    with pytest.raises(RuntimeError, match="seed data exists"):
        command.downgrade(config, "20260815_0039")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        connection.execute(sa.text("DELETE FROM workflow_draft_legacy_archive_seeds"))
    engine.dispose()
    command.downgrade(config, "20260815_0039")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "workflow_draft_legacy_archive_seeds" not in inspector.get_table_names()
    assert "legacy_workflow_archives" in inspector.get_table_names()
    with engine.connect() as connection:
        assert connection.scalar(sa.text("SELECT COUNT(*) FROM legacy_workflow_archives")) == 1
    engine.dispose()
    get_settings.cache_clear()
