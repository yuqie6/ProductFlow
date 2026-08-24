from __future__ import annotations

from datetime import UTC, datetime

import pytest
import sqlalchemy as sa
from alembic.script import ScriptDirectory
from test_migrations_database_constraints import _configure_sqlite_alembic

from alembic import command


def _insert_application_fixture(
    connection: sa.Connection,
    *,
    schema_version: int,
    preview_fields: bool,
) -> None:
    now = datetime.now(UTC)
    connection.execute(
        sa.text(
            "INSERT INTO products (id, name, created_at, updated_at) "
            "VALUES ('recipe-application-product', '配方应用迁移商品', :now, :now)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO workflow_graphs "
            "(id, product_id, title, active, schema_version, revision, created_at, updated_at) "
            "VALUES ('recipe-application-graph', 'recipe-application-product', '迁移图', 1, 3, 1, :now, :now)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO workflow_operation_groups "
            "(id, graph_id, actor_type, summary, base_revision, result_revision, "
            "operations_json, inverse_operations_json, created_at) "
            "VALUES ('recipe-application-operation', 'recipe-application-graph', 'recipe', "
            "'迁移应用', 0, 1, '[]', '[]', :now)"
        ),
        {"now": now},
    )
    columns = (
        "id, product_id, recipe_version_id, graph_id, operation_group_id, mode, schema_version, "
        "idempotency_key, request_hash, added_node_ids_json, added_edge_ids_json"
    )
    values = (
        "'recipe-application-row', 'recipe-application-product', "
        "'00000000-0000-4000-8000-000000000401', 'recipe-application-graph', "
        "'recipe-application-operation', 'merge', :schema_version, "
        "'migration-application', :request_hash, '[]', '[]'"
    )
    if preview_fields:
        columns += ", preview_graph_revision, preview_digest, updated_node_ids_json, required_bindings_json"
        values += ", 1, :preview_digest, '[]', '[]'"
    connection.execute(
        sa.text(
            f"INSERT INTO workflow_recipe_applications ({columns}, created_at) "
            f"VALUES ({values}, :now)"
        ),
        {
            "now": now,
            "schema_version": schema_version,
            "request_hash": "b" * 64,
            "preview_digest": "a" * 64,
        },
    )


def test_recipe_preview_migration_fresh_head_has_schema2_contract(
    tmp_path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(
        tmp_path,
        monkeypatch,
        filename="recipe-preview-fresh.db",
    )
    expected_head = ScriptDirectory.from_config(config).get_current_head()
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        inspector = sa.inspect(engine)
        columns = {column["name"] for column in inspector.get_columns("workflow_recipe_applications")}
        checks = {
            item["name"] for item in inspector.get_check_constraints("workflow_recipe_applications")
        }
        assert {
            "preview_graph_revision",
            "preview_digest",
            "updated_node_ids_json",
            "required_bindings_json",
        } <= columns
        assert {
            "ck_workflow_recipe_applications_schema_version",
            "ck_workflow_recipe_applications_preview_v2",
        } <= checks
        with engine.connect() as connection:
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == expected_head
    finally:
        engine.dispose()


def test_recipe_preview_migration_preserves_schema1_history_and_downgrades(
    tmp_path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(
        tmp_path,
        monkeypatch,
        filename="recipe-preview-history.db",
    )
    command.upgrade(config, "20260824_0086")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        with engine.begin() as connection:
            _insert_application_fixture(connection, schema_version=1, preview_fields=False)
    finally:
        engine.dispose()

    command.upgrade(config, "head")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        with engine.connect() as connection:
            assert connection.execute(
                sa.text(
                    "SELECT schema_version, preview_graph_revision, preview_digest, "
                    "updated_node_ids_json, required_bindings_json "
                    "FROM workflow_recipe_applications"
                )
            ).one() == (1, None, None, None, None)
    finally:
        engine.dispose()

    command.downgrade(config, "20260824_0086")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        columns = {column["name"] for column in sa.inspect(engine).get_columns("workflow_recipe_applications")}
        assert not {
            "preview_graph_revision",
            "preview_digest",
            "updated_node_ids_json",
            "required_bindings_json",
        } & columns
        with engine.connect() as connection:
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260824_0086"
    finally:
        engine.dispose()


def test_recipe_preview_migration_downgrade_refuses_schema2_application(
    tmp_path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(
        tmp_path,
        monkeypatch,
        filename="recipe-preview-schema2.db",
    )
    command.upgrade(config, "head")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        with engine.begin() as connection:
            _insert_application_fixture(connection, schema_version=2, preview_fields=True)
    finally:
        engine.dispose()

    with pytest.raises(RuntimeError, match="schema2 recipe application"):
        command.downgrade(config, "20260824_0086")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    try:
        with engine.connect() as connection:
            assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260824_0087"
            assert connection.scalar(
                sa.text("SELECT preview_digest FROM workflow_recipe_applications")
            ) == "a" * 64
    finally:
        engine.dispose()
