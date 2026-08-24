from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path

import pytest
import sqlalchemy as sa
from alembic.config import Config
from test_migrations_database_constraints import _configure_sqlite_alembic

from alembic import command


def _config(tmp_path: Path, monkeypatch: pytest.MonkeyPatch, *, filename: str) -> tuple[Path, Config]:
    return _configure_sqlite_alembic(tmp_path, monkeypatch, filename=filename)


def _insert_asset_fixture(connection: sa.Connection) -> None:
    now = datetime.now(UTC)
    connection.execute(
        sa.text(
            "INSERT INTO products (id, name, created_at, updated_at) "
            "VALUES ('fidelity-product', '人工检查迁移商品', :now, :now)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO media_objects "
            "(id, storage_path, mime_type, verification_status, created_at) "
            "VALUES ('fidelity-media', 'fidelity/migration.png', 'image/png', 'legacy_pending', :now)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO product_image_assets "
            "(id, product_id, media_object_id, origin_type, display_name, original_filename, created_at, updated_at) "
            "VALUES ('fidelity-asset', 'fidelity-product', 'fidelity-media', 'upload', "
            "'迁移图', 'migration.png', :now, :now)"
        ),
        {"now": now},
    )


def _insert_check(connection: sa.Connection, *, shape: str = "pass") -> None:
    now = datetime.now(UTC)
    connection.execute(
        sa.text(
            "INSERT INTO product_image_fidelity_checks "
            "(id, product_id, asset_id, version, shape_fidelity, color_material_fidelity, "
            "logo_text_legibility, text_policy_compliance, notes, checked_by, idempotency_key, "
            "request_hash, created_at) VALUES "
            "('fidelity-check-1', 'fidelity-product', 'fidelity-asset', 1, :shape, 'fail', "
            "'not_applicable', 'pass', '备注', 'administrator', 'migration-check-1', :hash, :now)"
        ),
        {"shape": shape, "hash": "a" * 64, "now": now},
    )


def test_fidelity_checks_migration_upgrades_fresh_sqlite_and_enforces_outcomes(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _config(tmp_path, monkeypatch, filename="fidelity-fresh.db")
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260824_0090"
        _insert_asset_fixture(connection)
        _insert_check(connection)
        with pytest.raises(sa.exc.IntegrityError):
            _insert_check(connection, shape="unknown")
    engine.dispose()


def test_fidelity_checks_downgrade_fails_closed_when_history_exists(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _config(tmp_path, monkeypatch, filename="fidelity-downgrade.db")
    command.upgrade(config, "head")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        _insert_asset_fixture(connection)
        _insert_check(connection)
    engine.dispose()

    with pytest.raises(RuntimeError, match="不能 downgrade"):
        command.downgrade(config, "20260824_0088")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        connection.execute(sa.text("DELETE FROM product_image_fidelity_checks"))
    engine.dispose()
    command.downgrade(config, "20260824_0088")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        assert not sa.inspect(connection).has_table("product_image_fidelity_checks")
        assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260824_0088"
    engine.dispose()
