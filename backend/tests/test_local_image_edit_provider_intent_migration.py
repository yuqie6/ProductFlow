from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path

import pytest
import sqlalchemy as sa
from test_migrations_database_constraints import _configure_sqlite_alembic

from alembic import command


def test_provider_intent_migration_backfills_legacy_rows_and_downgrade_is_fail_closed(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(
        tmp_path,
        monkeypatch,
        filename="local-edit-provider-intent.db",
    )
    command.upgrade(config, "20260824_0088")
    engine = sa.create_engine(f"sqlite:///{database_path}", future=True)
    now = datetime.now(UTC)
    try:
        with engine.begin() as connection:
            connection.execute(
                sa.text(
                    "INSERT INTO products (id, name, created_at, updated_at) "
                    "VALUES ('provider-intent-product', '能力快照迁移商品', :now, :now)"
                ),
                {"now": now},
            )
            connection.execute(
                sa.text(
                    "INSERT INTO local_image_edit_tasks "
                    "(id, product_id, source_asset_id, source_media_sha256, mask_media_object_id, "
                    "operation, mask_geometry_json, status, revision, idempotency_key, request_hash, "
                    "attempts, is_retryable, created_at, updated_at) "
                    "VALUES ('provider-intent-task', 'provider-intent-product', 'missing-source', :source_hash, "
                    "'missing-mask', 'inpaint', '{}', 'queued', 1, 'legacy-key', :request_hash, 0, 1, :now, :now)"
                ),
                {"source_hash": "s" * 64, "request_hash": "r" * 64, "now": now},
            )
        command.upgrade(config, "20260824_0090")

        with engine.connect() as connection:
            row = connection.execute(
                sa.text(
                    "SELECT requested_provider_name, requested_local_edit_mode "
                    "FROM local_image_edit_tasks WHERE id = 'provider-intent-task'"
                )
            ).one()
            assert row == ("legacy_snapshot_missing", "legacy_snapshot_missing")
            check_names = {
                item["name"]
                for item in sa.inspect(engine).get_check_constraints("local_image_edit_tasks")
            }
            assert "ck_local_image_edit_tasks_provider_intent" in check_names

        with pytest.raises(sa.exc.IntegrityError):
            with engine.begin() as connection:
                connection.execute(
                    sa.text(
                        "UPDATE local_image_edit_tasks SET requested_provider_name = NULL "
                        "WHERE id = 'provider-intent-task'"
                    )
                )

        with pytest.raises(RuntimeError, match="provider 能力快照"):
            command.downgrade(config, "20260824_0089")
    finally:
        engine.dispose()
