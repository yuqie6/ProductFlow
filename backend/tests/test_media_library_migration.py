from __future__ import annotations

from pathlib import Path

import pytest
import sqlalchemy as sa
from test_migrations_database_constraints import _configure_sqlite_alembic

from alembic import command


def test_media_library_assets_migration_creates_schema_and_constraints(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(tmp_path, monkeypatch, filename="media-library.db")
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        inspector = sa.inspect(engine)
        tables = set(inspector.get_table_names())
        assert {
            "media_library_assets",
            "media_library_folders",
            "media_library_tags",
            "media_library_asset_tags",
        } <= tables
        columns = {column["name"] for column in inspector.get_columns("media_library_assets")}
        assert {
            "id",
            "media_object_id",
            "source_type",
            "source_id",
            "provenance_json",
            "provenance_hash",
            "revision",
            "folder_id",
        } <= columns
        product_asset_columns = {column["name"] for column in inspector.get_columns("product_image_assets")}
        assert "source_library_asset_id" in product_asset_columns
        checks = {check["name"] for check in inspector.get_check_constraints("media_library_assets")}
        assert {
            "ck_media_library_assets_source_type",
            "ck_media_library_assets_revision",
            "ck_media_library_assets_provenance_hash",
        } <= checks
        with engine.connect() as connection:
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260817_0051"
    finally:
        engine.dispose()
