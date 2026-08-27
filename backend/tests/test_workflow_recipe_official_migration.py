from __future__ import annotations

import json
from datetime import UTC, datetime

import pytest
import sqlalchemy as sa
from alembic.script import ScriptDirectory
from test_migrations_database_constraints import _configure_sqlite_alembic

from alembic import command
from productflow_backend.application.workflow_recipes.official import official_recipe_seeds


def test_official_recipe_migration_seeds_canonical_v3_fragments(
    tmp_path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(
        tmp_path,
        monkeypatch,
        filename="official-recipes.db",
    )
    expected_head = ScriptDirectory.from_config(config).get_current_head()
    command.upgrade(config, "20260824_0086")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        with engine.connect() as connection:
            rows = connection.execute(
                sa.text(
                    "SELECT recipes.id AS recipe_id, recipes.origin, recipes.official_key, "
                    "recipes.archived_at, "
                    "versions.id AS version_id, versions.schema_version, versions.catalog_version, "
                    "versions.creation_source, versions.title, versions.payload_json, "
                    "versions.payload_hash, versions.governance_json "
                    "FROM workflow_recipes AS recipes "
                    "JOIN workflow_recipe_versions AS versions "
                    "ON versions.id = recipes.current_version_id "
                    "WHERE recipes.origin = 'official' "
                    "ORDER BY recipes.official_key"
                )
            ).mappings().all()
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260824_0086"

        seeds = {seed.official_key: seed for seed in official_recipe_seeds()}
        assert [row["official_key"] for row in rows] == ["detail", "hero", "scene", "selling_point"]
        assert len(rows) == len(seeds) == 4
        for row in rows:
            seed = seeds[row["official_key"]]
            assert row["recipe_id"] == seed.recipe_id
            assert row["origin"] == "official"
            assert row["schema_version"] == 3
            assert row["catalog_version"] == 5
            assert row["creation_source"] == "official_seed"
            assert row["title"] == seed.title
            payload_json = (
                json.loads(row["payload_json"]) if isinstance(row["payload_json"], str) else row["payload_json"]
            )
            governance_json = (
                json.loads(row["governance_json"])
                if isinstance(row["governance_json"], str)
                else row["governance_json"]
            )
            assert payload_json == seed.payload.model_dump(mode="json")
            assert row["payload_hash"] == seed.payload_hash
            assert governance_json == seed.governance.model_dump(mode="json")
            assert {node["node_type"] for node in payload_json["nodes"]} == {
                "prompt_generation",
                "image_generation",
            }
            assert len(payload_json["edges"]) == 1
            assert payload_json["edges"][0]["data_type"] == "prompt"
            assert "product_source" not in str(payload_json)
            assert row["archived_at"] is None

        inspector = sa.inspect(engine)
        recipe_columns = {column["name"] for column in inspector.get_columns("workflow_recipes")}
        version_columns = {column["name"] for column in inspector.get_columns("workflow_recipe_versions")}
        assert {"origin", "official_key"} <= recipe_columns
        assert {"catalog_version", "creation_source", "governance_json"} <= version_columns
        recipe_constraints = {
            item["name"] for item in inspector.get_check_constraints("workflow_recipes")
        }
        version_constraints = {
            item["name"] for item in inspector.get_check_constraints("workflow_recipe_versions")
        }
        assert "ck_workflow_recipes_origin_key" in recipe_constraints
        assert "ck_workflow_recipe_versions_catalog_version" in version_constraints
    finally:
        engine.dispose()

    command.upgrade(config, "head")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        with engine.connect() as connection:
            archived = connection.execute(
                sa.text(
                    "SELECT official_key, archived_at FROM workflow_recipes "
                    "WHERE origin = 'official' ORDER BY official_key"
                )
            ).mappings().all()
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == expected_head
            assert [row["official_key"] for row in archived] == ["detail", "hero", "scene", "selling_point"]
            assert all(row["archived_at"] is not None for row in archived)
    finally:
        engine.dispose()


def test_official_recipe_migration_backfills_existing_user_versions(
    tmp_path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(
        tmp_path,
        monkeypatch,
        filename="official-recipes-backfill.db",
    )
    command.upgrade(config, "20260822_0085")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    now = datetime.now(UTC)
    try:
        with engine.begin() as connection:
            connection.execute(
                sa.text(
                    "INSERT INTO workflow_recipes "
                    "(id, kind, current_version_id, archived_at, created_at, updated_at) "
                    "VALUES ('legacy-user-recipe', 'recipe_fragment', NULL, NULL, :now, :now)"
                ),
                {"now": now},
            )
            connection.execute(
                sa.text(
                    "INSERT INTO workflow_recipe_versions "
                    "(id, recipe_id, version, schema_version, title, description, payload_json, payload_hash, "
                    "preferred_visual_system_version_id, created_at) "
                    "VALUES ('legacy-user-version', 'legacy-user-recipe', 1, 3, '历史用户配方', NULL, "
                    ":payload, :payload_hash, NULL, :now)"
                ),
                {"payload": "{}", "payload_hash": "a" * 64, "now": now},
            )
            connection.execute(
                sa.text(
                    "UPDATE workflow_recipes SET current_version_id = 'legacy-user-version' "
                    "WHERE id = 'legacy-user-recipe'"
                )
            )
    finally:
        engine.dispose()

    command.upgrade(config, "head")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        with engine.connect() as connection:
            recipe = connection.execute(
                sa.text(
                    "SELECT origin, official_key FROM workflow_recipes "
                    "WHERE id = 'legacy-user-recipe'"
                )
            ).one()
            version = connection.execute(
                sa.text(
                    "SELECT catalog_version, creation_source, governance_json "
                    "FROM workflow_recipe_versions WHERE id = 'legacy-user-version'"
                )
            ).one()
            assert recipe == ("user", None)
            assert version == (5, "user_extract", None)
    finally:
        engine.dispose()


def test_official_recipe_migration_downgrade_is_blocked_after_compat_table_drop(
    tmp_path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(
        tmp_path,
        monkeypatch,
        filename="official-recipes-downgrade.db",
    )
    command.upgrade(config, "head")
    with pytest.raises(RuntimeError, match="已删除的兼容表和 Draft 列不能降级"):
        command.downgrade(config, "20260822_0085")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        with engine.connect() as connection:
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260827_0094"
        inspector = sa.inspect(engine)
        assert "workflow_draft_recipe_seeds" not in inspector.get_table_names()
        assert "workflow_drafts" not in inspector.get_table_names()
    finally:
        engine.dispose()
