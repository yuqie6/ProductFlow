from __future__ import annotations

from pathlib import Path

import pytest
import sqlalchemy as sa
from test_migrations_database_constraints import _configure_sqlite_alembic

from alembic import command


def test_agent_session_migration_backfills_existing_conversations_and_downgrades(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(
        tmp_path,
        monkeypatch,
        filename="agent-session-migration.db",
    )
    command.upgrade(config, "20260816_0049")

    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    now = "2026-08-17 10:00:00"
    with engine.begin() as connection:
        for suffix in ("a", "b"):
            connection.execute(
                sa.text(
                    "INSERT INTO products (id, name, created_at, updated_at) "
                    "VALUES (:product_id, :name, :now, :now)"
                ),
                {"product_id": f"product-{suffix}", "name": f"商品 {suffix}", "now": now},
            )
            connection.execute(
                sa.text(
                    "INSERT INTO workflow_drafts "
                    "(id, product_id, status, current_revision_id, final_workflow_id, created_at, updated_at) "
                    "VALUES (:draft_id, :product_id, 'collecting', NULL, NULL, :now, :now)"
                ),
                {"draft_id": f"draft-{suffix}", "product_id": f"product-{suffix}", "now": now},
            )
            connection.execute(
                sa.text(
                    "INSERT INTO agent_conversations "
                    "(id, product_id, workflow_draft_id, harness_run_id, status, created_at, updated_at) "
                    "VALUES (:conversation_id, :product_id, :draft_id, :run_id, 'collecting', :now, :now)"
                ),
                {
                    "conversation_id": f"conversation-{suffix}",
                    "product_id": f"product-{suffix}",
                    "draft_id": f"draft-{suffix}",
                    "run_id": f"run-{suffix}",
                    "now": now,
                },
            )

    command.upgrade(config, "head")
    with engine.connect() as connection:
        rows = connection.execute(
            sa.text(
                "SELECT c.session_id, s.title, s.status "
                "FROM agent_conversations AS c "
                "JOIN agent_sessions AS s ON s.id = c.session_id "
                "ORDER BY c.id"
            )
        ).all()
        assert len(rows) == 2
        assert [(row[1], row[2]) for row in rows] == [("商品 a", "active"), ("商品 b", "active")]
        assert rows[0][0] != rows[1][0]

    command.downgrade(config, "20260816_0049")
    inspector = sa.inspect(engine)
    assert "agent_sessions" not in inspector.get_table_names()
    assert "session_id" not in {column["name"] for column in inspector.get_columns("agent_conversations")}
    engine.dispose()
