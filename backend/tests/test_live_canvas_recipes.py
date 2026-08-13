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
from workflow_draft_helpers import make_workflow_draft_payload

from alembic import command
from productflow_backend.application.product_workflow.folders import (
    rename_workflow_folder,
    translate_workflow_folder,
)
from productflow_backend.application.use_cases import create_canonical_product
from productflow_backend.application.workflow_drafts.materialization import materialize_workflow_draft
from productflow_backend.application.workflow_drafts.service import (
    confirm_workflow_draft_revision,
    create_workflow_draft,
)
from productflow_backend.application.workflow_recipes.service import (
    apply_workflow_recipe,
    create_workflow_recipe,
)
from productflow_backend.config import get_settings
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.session import get_engine, get_session_factory

LIVE_CANVAS_RECIPE_SWITCH = "PRODUCTFLOW_RUN_LIVE_CANVAS_RECIPE"

pytestmark = [
    pytest.mark.live_dependencies,
    pytest.mark.skipif(
        os.getenv(LIVE_CANVAS_RECIPE_SWITCH) != "1",
        reason=f"set {LIVE_CANVAS_RECIPE_SWITCH}=1 to run the PostgreSQL canvas/recipe gate",
    ),
]


@contextmanager
def _temporary_postgres_database(base_database_url: str) -> Iterator[URL]:
    base_url = make_url(base_database_url)
    if base_url.get_backend_name() != "postgresql":
        pytest.fail("DATABASE_URL must point to PostgreSQL for the live canvas/recipe gate", pytrace=False)

    database_name = f"productflow_live_canvas_{uuid4().hex}"
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


def test_canvas_folder_and_recipe_contracts_on_postgresql(
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

            engine = sa.create_engine(database_url, future=True)
            inspector = sa.inspect(engine)
            assert {
                "workflow_recipes",
                "workflow_recipe_versions",
                "workflow_draft_recipe_seeds",
            }.issubset(inspector.get_table_names())
            assert "edit_version" in {column["name"] for column in inspector.get_columns("product_workflows")}
            assert {
                "position_x",
                "position_y",
                "width",
                "height",
                "config_json",
            }.isdisjoint(column["name"] for column in inspector.get_columns("workflow_folders"))
            engine.dispose()

            session_factory = get_session_factory()
            with session_factory() as session:
                source_product = create_canonical_product(
                    session,
                    name="PostgreSQL canvas source",
                    category="industrial storage",
                    price="299.00",
                    source_note="live canvas contract",
                    image_uploads=[(_make_demo_image_bytes(), "source.png", "image/png")],
                )
                draft = create_workflow_draft(
                    session,
                    product_id=source_product.id,
                    payload=make_workflow_draft_payload(reference_asset_id=source_product.image_assets[0].id),
                    ready_for_confirmation=True,
                )
                confirm_workflow_draft_revision(
                    session,
                    product_id=source_product.id,
                    draft_id=draft.id,
                    expected_draft_version=1,
                )
                materialized = materialize_workflow_draft(
                    session,
                    product_id=source_product.id,
                    draft_id=draft.id,
                    expected_draft_version=1,
                    expected_workflow_revision=0,
                    idempotency_key="live-canvas-materialize",
                )
                workflow_id = materialized.workflow.id
                folder_id = materialized.workflow.folders[0].id
                member_positions = {
                    node.id: (node.position_x, node.position_y)
                    for node in materialized.workflow.nodes
                    if node.folder_id == folder_id
                }
                source_product_id = source_product.id

            with session_factory() as session:
                translated = translate_workflow_folder(
                    session,
                    product_id=source_product_id,
                    workflow_id=workflow_id,
                    folder_id=folder_id,
                    delta_x=37,
                    delta_y=-19,
                    expected_edit_version=0,
                )
                assert translated.workflow.edit_version == 1
                assert {
                    node.id: (node.position_x, node.position_y)
                    for node in translated.workflow.nodes
                    if node.folder_id == folder_id
                } == {
                    node_id: (position_x + 37, position_y - 19)
                    for node_id, (position_x, position_y) in member_positions.items()
                }

            with session_factory() as session, pytest.raises(ConflictError, match="edit version"):
                rename_workflow_folder(
                    session,
                    product_id=source_product_id,
                    workflow_id=workflow_id,
                    folder_id=folder_id,
                    title="stale rename",
                    expected_edit_version=0,
                )

            with session_factory() as session:
                recipe = create_workflow_recipe(
                    session,
                    product_id=source_product_id,
                    workflow_id=workflow_id,
                    source_type="folder",
                    folder_id=folder_id,
                    node_ids=(),
                    expected_edit_version=1,
                    title="PostgreSQL fragment",
                    description="live recipe contract",
                    preferred_visual_system_version_id=None,
                )
                target_product = create_canonical_product(
                    session,
                    name="PostgreSQL recipe target",
                    category="industrial storage",
                    price=None,
                    source_note=None,
                    image_uploads=[(_make_demo_image_bytes(), "target.png", "image/png")],
                )
                applied = apply_workflow_recipe(
                    session,
                    product_id=target_product.id,
                    recipe_id=recipe.id,
                    expected_recipe_version=1,
                    idempotency_key="live-recipe-apply",
                )
                assert applied.draft.current_revision_id is None
                assert applied.draft.revisions == []
                assert applied.draft.recipe_seed is not None
                assert applied.conversation.workflow_draft_id == applied.draft.id
                assert applied.draft.final_workflow_id is None

            _reset_database_state()

        _reset_database_state()
