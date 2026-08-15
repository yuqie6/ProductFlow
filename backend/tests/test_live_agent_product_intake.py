from __future__ import annotations

import os
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path
from uuid import uuid4

import pytest
import sqlalchemy as sa
from alembic.config import Config
from helpers import _make_demo_image_bytes
from sqlalchemy.engine import URL, make_url

from alembic import command
from productflow_backend.application.agent_product_intake import AgentProductSelectionV1
from productflow_backend.application.agent_product_workspaces import (
    create_agent_product_draft_workspace,
    create_agent_product_workspace,
    finalize_agent_product_workspace_intake,
    get_agent_product_workspace,
)
from productflow_backend.config import get_settings
from productflow_backend.infrastructure.db.session import get_engine, get_session_factory

LIVE_AGENT_PRODUCT_INTAKE_SWITCH = "PRODUCTFLOW_RUN_LIVE_AGENT_PRODUCT_INTAKE"

pytestmark = [
    pytest.mark.live_dependencies,
    pytest.mark.skipif(
        os.getenv(LIVE_AGENT_PRODUCT_INTAKE_SWITCH) != "1",
        reason=f"set {LIVE_AGENT_PRODUCT_INTAKE_SWITCH}=1 to run the PostgreSQL Agent product intake gate",
    ),
]


@contextmanager
def _temporary_postgres_database(base_database_url: str) -> Iterator[URL]:
    base_url = make_url(base_database_url)
    if base_url.get_backend_name() != "postgresql":
        pytest.fail("DATABASE_URL must point to PostgreSQL for the live Agent product intake gate", pytrace=False)

    database_name = f"productflow_live_agent_intake_{uuid4().hex}"
    maintenance_engine = sa.create_engine(
        base_url.set(database="postgres"),
        future=True,
        isolation_level="AUTOCOMMIT",
        pool_pre_ping=True,
    )
    quoted_name = maintenance_engine.dialect.identifier_preparer.quote(database_name)
    created = False
    try:
        with maintenance_engine.connect() as connection:
            connection.exec_driver_sql(f"CREATE DATABASE {quoted_name} TEMPLATE template0")
        created = True
        yield base_url.set(database=database_name)
    finally:
        try:
            if created:
                with maintenance_engine.connect() as connection:
                    connection.execute(
                        sa.text(
                            "SELECT pg_terminate_backend(pid) FROM pg_stat_activity "
                            "WHERE datname = :database_name AND pid <> pg_backend_pid()"
                        ),
                        {"database_name": database_name},
                    ).all()
                    connection.exec_driver_sql(f"DROP DATABASE IF EXISTS {quoted_name}")
        finally:
            maintenance_engine.dispose()


def _reset_database_state() -> None:
    cached_engine = get_engine() if get_engine.cache_info().currsize else None
    get_session_factory.cache_clear()
    if cached_engine is not None:
        cached_engine.dispose()
    get_engine.cache_clear()
    get_settings.cache_clear()


def test_agent_product_intake_round_trips_and_creates_atomically_on_postgresql(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    base_database_url = os.getenv("DATABASE_URL", "").strip()
    if not base_database_url:
        pytest.fail("DATABASE_URL must be provided by the development environment", pytrace=False)

    with _temporary_postgres_database(base_database_url) as database_url:
        with monkeypatch.context() as environment:
            environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
            environment.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
            environment.setenv("TEXT_PROVIDER_KIND", "mock")
            environment.setenv("IMAGE_PROVIDER_KIND", "mock")
            _reset_database_state()
            backend_dir = Path(__file__).resolve().parents[1]
            config = Config(str(backend_dir / "alembic.ini"))
            config.set_main_option("script_location", str(backend_dir / "alembic"))

            command.upgrade(config, "20260814_0037")
            now = "2026-08-14 14:00:00+00"
            engine = sa.create_engine(database_url, future=True)
            with engine.begin() as connection:
                connection.execute(
                    sa.text(
                        "INSERT INTO products (id, name, created_at, updated_at) "
                        "VALUES ('product-intake-history', '历史商品', :now, :now)"
                    ),
                    {"now": now},
                )
                connection.execute(
                    sa.text(
                        "INSERT INTO workflow_drafts "
                        "(id, product_id, status, current_revision_id, final_workflow_id, created_at, updated_at) "
                        "VALUES ('draft-intake-history', 'product-intake-history', 'collecting', "
                        "NULL, NULL, :now, :now)"
                    ),
                    {"now": now},
                )
                connection.execute(
                    sa.text(
                        "INSERT INTO agent_conversations "
                        "(id, product_id, workflow_draft_id, harness_run_id, status, created_at, updated_at) "
                        "VALUES ('conversation-intake-history', 'product-intake-history', "
                        "'draft-intake-history', 'run-intake-history', 'collecting', :now, :now)"
                    ),
                    {"now": now},
                )
            engine.dispose()

            command.upgrade(config, "head")
            session_factory = get_session_factory()
            with session_factory() as session:
                selection = AgentProductSelectionV1.model_validate(
                    {
                        "schema_version": 1,
                        "image_types": [
                            {"key": "hero", "quantity": 2, "order": 0},
                            {"key": "detail", "quantity": 1, "order": 1},
                        ],
                    }
                )
                created = create_agent_product_workspace(
                    session,
                    name="PostgreSQL Agent 商品",
                    selection=selection,
                    image_uploads=[
                        (_make_demo_image_bytes(), "front.png", "image/png"),
                        (_make_demo_image_bytes(), "detail.png", "image/png"),
                    ],
                    idempotency_key="postgres-agent-create",
                )
                product_id = created.product.id
                draft_id = created.workflow_draft.id
                conversation_id = created.conversation.id
                assert created.product.cover_image_asset_id is None
                assert created.workflow_draft.current_revision_id is None
                assert created.workflow_draft.intake_schema_version == 1
                assert created.conversation.creation_request_hash is not None
                assert create_agent_product_workspace(
                    session,
                    name="PostgreSQL Agent 商品",
                    selection=selection,
                    image_uploads=[
                        (_make_demo_image_bytes(), "front.png", "image/png"),
                        (_make_demo_image_bytes(), "detail.png", "image/png"),
                    ],
                    idempotency_key="postgres-agent-create",
                ).created is False

            _reset_database_state()
            command.downgrade(config, "20260814_0037")
            engine = sa.create_engine(database_url, future=True)
            inspector = sa.inspect(engine)
            assert "intake_json" not in {column["name"] for column in inspector.get_columns("workflow_drafts")}
            with engine.connect() as connection:
                assert connection.scalar(sa.text("SELECT COUNT(*) FROM products")) == 2
                assert connection.scalar(sa.text("SELECT COUNT(*) FROM workflow_drafts")) == 2
                assert connection.scalar(sa.text("SELECT COUNT(*) FROM agent_conversations")) == 2
                assert connection.scalar(
                    sa.text("SELECT COUNT(*) FROM products WHERE id = :id"), {"id": product_id}
                ) == 1
                assert connection.scalar(
                    sa.text("SELECT COUNT(*) FROM workflow_drafts WHERE id = :id"), {"id": draft_id}
                ) == 1
                assert connection.scalar(
                    sa.text("SELECT COUNT(*) FROM agent_conversations WHERE id = :id"),
                    {"id": conversation_id},
                ) == 1
            engine.dispose()

            command.upgrade(config, "20260814_0038")
            engine = sa.create_engine(database_url, future=True)
            with engine.connect() as connection:
                assert connection.execute(
                    sa.text(
                        "SELECT intake_schema_version, intake_json FROM workflow_drafts WHERE id = :id"
                    ),
                    {"id": draft_id},
                ).one() == (None, None)
                assert connection.execute(
                    sa.text(
                        "SELECT creation_idempotency_key, creation_request_hash "
                        "FROM agent_conversations WHERE id = :id"
                    ),
                    {"id": conversation_id},
                ).one() == (None, None)
            engine.dispose()
            _reset_database_state()

        _reset_database_state()


def test_draft_first_agent_product_intake_replays_across_sessions_on_postgresql(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    base_database_url = os.getenv("DATABASE_URL", "").strip()
    if not base_database_url:
        pytest.fail("DATABASE_URL must be provided by the development environment", pytrace=False)

    with _temporary_postgres_database(base_database_url) as database_url:
        with monkeypatch.context() as environment:
            environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
            environment.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
            environment.setenv("TEXT_PROVIDER_KIND", "mock")
            environment.setenv("IMAGE_PROVIDER_KIND", "mock")
            _reset_database_state()
            backend_dir = Path(__file__).resolve().parents[1]
            config = Config(str(backend_dir / "alembic.ini"))
            config.set_main_option("script_location", str(backend_dir / "alembic"))
            command.upgrade(config, "head")

            selection = AgentProductSelectionV1.model_validate(
                {
                    "schema_version": 1,
                    "image_types": [
                        {"key": "hero", "quantity": 3, "order": 0},
                        {"key": "scene", "quantity": 2, "order": 1},
                    ],
                }
            )
            uploads = [
                (_make_demo_image_bytes(), "front.png", "image/png"),
                (_make_demo_image_bytes(), "detail.png", "image/png"),
            ]
            session_factory = get_session_factory()
            with session_factory() as session:
                draft = create_agent_product_draft_workspace(
                    session,
                    name="PostgreSQL 两阶段 Agent 商品",
                    idempotency_key="postgres-draft-first",
                )
                conversation_id = draft.conversation.id
                product_id = draft.product.id
                assert draft.created is True
                assert draft.created_assets == []
                assert draft.workflow_draft.intake_json is None
                assert draft.workflow_draft.current_revision_id is None
                assert draft.product.cover_image_asset_id is None

            with session_factory() as session:
                restored = get_agent_product_workspace(
                    session,
                    conversation_id=conversation_id,
                )
                assert restored.product.id == product_id
                assert restored.created is False
                assert restored.created_assets == []
                finalized = finalize_agent_product_workspace_intake(
                    session,
                    conversation_id=conversation_id,
                    selection=selection,
                    image_uploads=uploads,
                    idempotency_key="postgres-intake-finalization",
                )
                asset_ids = [asset.id for asset in finalized.created_assets]
                assert finalized.created is True
                assert len(asset_ids) == 2
                assert finalized.workflow_draft.intake_json == {
                    "schema_version": 1,
                    "image_types": [
                        {"key": "hero", "quantity": 3, "order": 0},
                        {"key": "scene", "quantity": 2, "order": 1},
                    ],
                    "reference_asset_ids": asset_ids,
                }

            with session_factory() as session:
                replay = finalize_agent_product_workspace_intake(
                    session,
                    conversation_id=conversation_id,
                    selection=selection,
                    image_uploads=uploads,
                    idempotency_key="postgres-intake-finalization",
                )
                assert replay.created is False
                assert [asset.id for asset in replay.created_assets] == asset_ids
                assert session.scalar(sa.text("SELECT COUNT(*) FROM products")) == 1
                assert session.scalar(sa.text("SELECT COUNT(*) FROM product_image_assets")) == 2
                assert session.scalar(sa.text("SELECT COUNT(*) FROM media_objects")) == 2
                assert session.scalar(sa.text("SELECT COUNT(*) FROM product_workflows")) == 0
                assert session.scalar(sa.text("SELECT COUNT(*) FROM workflow_draft_revisions")) == 0

            _reset_database_state()

        _reset_database_state()
