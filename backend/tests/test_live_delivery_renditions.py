from __future__ import annotations

import importlib
import os
import sys
from collections.abc import Iterator
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
from datetime import UTC, datetime, timedelta
from pathlib import Path
from threading import Barrier
from types import ModuleType
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit
from uuid import uuid4

import pytest
import sqlalchemy as sa
from alembic.config import Config
from dramatiq.brokers.redis import RedisBroker
from sqlalchemy.engine import URL, make_url
from test_delivery_renditions import _create_generated_source
from test_prompt_visual_image_nodes import (
    RecordingImageProvider,
    _create_materialized_workflow,
    _png_bytes,
    _queue_single_node_run,
)

from alembic import command
from productflow_backend.application.delivery_renditions.service import (
    _fail_delivery_rendition_job,
    claim_delivery_rendition_job,
    create_delivery_rendition_job,
    execute_delivery_rendition_job,
    get_delivery_rendition_job,
    retry_delivery_rendition_job,
    submit_delivery_rendition_job,
)
from productflow_backend.application.durable_recovery import recover_unfinished_delivery_rendition_jobs
from productflow_backend.application.media_assets import clear_product_cover
from productflow_backend.application.product_workflow.v2_execution import execute_v2_workflow_node_run
from productflow_backend.application.product_workflow_dependencies import WorkflowExecutionDependencies
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import JobStatus, WorkflowNodeStatus, WorkflowRunStatus
from productflow_backend.infrastructure.db.models import (
    DeliveryRenditionJob,
    Product,
    WorkflowImageGenerationRecord,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)
from productflow_backend.infrastructure.db.session import get_engine, get_session_factory
from productflow_backend.infrastructure.queue import get_broker
from productflow_backend.infrastructure.storage import LocalStorage

LIVE_DELIVERY_RENDITION_SWITCH = "PRODUCTFLOW_RUN_LIVE_DELIVERY_RENDITIONS"
WORKERS_MODULE = "productflow_backend.workers"
REDIS_TEST_DATABASE = 14

pytestmark = [
    pytest.mark.live_dependencies,
    pytest.mark.skipif(
        os.getenv(LIVE_DELIVERY_RENDITION_SWITCH) != "1",
        reason=f"set {LIVE_DELIVERY_RENDITION_SWITCH}=1 to run the PostgreSQL/Redis delivery rendition gate",
    ),
]


def _required_environment(name: str) -> str:
    value = os.getenv(name, "").strip()
    if not value:
        pytest.fail(f"{name} must be provided by the development environment", pytrace=False)
    return value


def _redis_url_for_database(base_url: str, database: int) -> str:
    parsed = urlsplit(base_url)
    if parsed.scheme not in {"redis", "rediss"}:
        pytest.fail("REDIS_URL must use redis:// or rediss:// for the live delivery rendition gate", pytrace=False)
    query = urlencode([(key, value) for key, value in parse_qsl(parsed.query) if key != "db"])
    return urlunsplit(parsed._replace(path=f"/{database}", query=query))


@contextmanager
def _temporary_postgres_database(base_database_url: str) -> Iterator[URL]:
    try:
        base_url = make_url(base_database_url)
    except Exception:
        pytest.fail("DATABASE_URL must be a valid SQLAlchemy URL", pytrace=False)
    if base_url.get_backend_name() != "postgresql":
        pytest.fail("DATABASE_URL must point to PostgreSQL for the live delivery rendition gate", pytrace=False)

    database_name = f"productflow_live_delivery_{uuid4().hex}"
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
                    assert connection.scalar(
                        sa.text("SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = :database_name)"),
                        {"database_name": database_name},
                    ) is False
        finally:
            maintenance_engine.dispose()


def _reset_runtime_state() -> None:
    sys.modules.pop(WORKERS_MODULE, None)
    cached_engine = get_engine() if get_engine.cache_info().currsize else None
    cached_broker = get_broker() if get_broker.cache_info().currsize else None
    get_session_factory.cache_clear()
    try:
        if cached_broker is not None:
            cached_broker.client.connection_pool.disconnect()
    finally:
        get_broker.cache_clear()
        try:
            if cached_engine is not None:
                cached_engine.dispose()
        finally:
            get_engine.cache_clear()
            get_settings.cache_clear()
            importlib.import_module("dramatiq.broker").global_broker = None


@pytest.fixture()
def live_delivery_dependencies(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> Iterator[tuple[RedisBroker, ModuleType]]:
    base_database_url = _required_environment("DATABASE_URL")
    base_redis_url = _required_environment("REDIS_URL")
    isolated_redis_url = _redis_url_for_database(base_redis_url, REDIS_TEST_DATABASE)

    with _temporary_postgres_database(base_database_url) as database_url:
        with monkeypatch.context() as environment:
            environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
            environment.setenv("REDIS_URL", isolated_redis_url)
            environment.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
            environment.setenv("LOG_DIR", str(tmp_path / "logs"))
            environment.setenv("TEXT_PROVIDER_KIND", "mock")
            environment.setenv("IMAGE_PROVIDER_KIND", "mock")
            environment.setenv("POSTER_GENERATION_MODE", "template")
            _reset_runtime_state()

            broker: RedisBroker | None = None
            try:
                backend_dir = Path(__file__).resolve().parents[1]
                config = Config(str(backend_dir / "alembic.ini"))
                config.set_main_option("script_location", str(backend_dir / "alembic"))
                command.upgrade(config, "head")

                inspector = sa.inspect(get_engine())
                assert "delivery_rendition_jobs" in inspector.get_table_names()
                assert "uq_delivery_rendition_jobs_source_spec" in {
                    constraint["name"]
                    for constraint in inspector.get_unique_constraints("delivery_rendition_jobs")
                }

                broker = get_broker()
                assert broker.client.connection_pool.connection_kwargs["db"] == REDIS_TEST_DATABASE
                broker.client.flushdb()
                workers = importlib.import_module(WORKERS_MODULE)
                assert workers.run_delivery_rendition_job.broker is broker
                yield broker, workers
            finally:
                try:
                    if broker is not None:
                        broker.client.flushdb()
                        assert broker.client.dbsize() == 0
                finally:
                    _reset_runtime_state()

        _reset_runtime_state()


def _consume_delivery_message(broker: RedisBroker, workers: ModuleType, *, job_id: str) -> None:
    consumer = broker.consume(workers.run_delivery_rendition_job.queue_name, prefetch=1, timeout=5_000)
    try:
        message = next(consumer)
        assert message is not None
        assert message.actor_name == "run_delivery_rendition_job"
        assert message.args == (job_id,)
        assert message.kwargs == {}
        consumer.ack(message)
    finally:
        consumer.close()


def test_delivery_renditions_migrate_enqueue_and_render_real_files(
    live_delivery_dependencies: tuple[RedisBroker, ModuleType],
) -> None:
    broker, workers = live_delivery_dependencies
    session_factory = get_session_factory()
    with session_factory() as session:
        source, workflow_run, node_run = _create_generated_source(session)
        source_id = source.id
        workflow_run_id = workflow_run.id
        node_run_id = node_run.id

    specs = (
        {"width": 48, "height": 48, "format": "png", "fit": "contain"},
        {
            "width": 64,
            "height": 40,
            "format": "jpeg",
            "fit": "contain",
            "background_color": "#FFFFFF",
        },
        {"width": 50, "height": 70, "format": "webp", "fit": "cover", "crop_anchor": "top"},
    )
    job_ids: list[str] = []
    with session_factory() as session:
        for spec in specs:
            job = submit_delivery_rendition_job(
                session,
                source_asset_id=source_id,
                delivery_spec=spec,
            )
            job_ids.append(job.id)

    for job_id in job_ids:
        _consume_delivery_message(broker, workers, job_id=job_id)
        execute_delivery_rendition_job(job_id)
    broker.join(workers.run_delivery_rendition_job.queue_name, timeout=5_000)

    expected = (
        ("image/png", 48, 48),
        ("image/jpeg", 64, 40),
        ("image/webp", 50, 70),
    )
    storage = LocalStorage()
    with session_factory() as session:
        for job_id, contract in zip(job_ids, expected, strict=True):
            job = get_delivery_rendition_job(session, job_id)
            assert job.status == JobStatus.SUCCEEDED
            assert job.attempts == 1
            assert job.result_asset is not None
            assert job.result_asset.parent_asset_id == source_id
            media = job.result_asset.media_object
            assert (media.mime_type, media.width, media.height) == contract
            assert media.byte_size == storage.resolve(media.storage_path).stat().st_size
            assert media.sha256 is not None and len(media.sha256) == 64

        assert session.get(WorkflowRun, workflow_run_id).status == WorkflowRunStatus.SUCCEEDED
        assert session.get(WorkflowNodeRun, node_run_id).status == WorkflowNodeStatus.SUCCEEDED
        assert session.query(WorkflowImageGenerationRecord).count() == 1


def test_delivery_rendition_claim_recovery_retry_and_attempt_fencing_on_postgres(
    live_delivery_dependencies: tuple[RedisBroker, ModuleType],
) -> None:
    _, _ = live_delivery_dependencies
    session_factory = get_session_factory()
    with session_factory() as session:
        source, _, _ = _create_generated_source(session)
        creation = create_delivery_rendition_job(
            session,
            source_asset_id=source.id,
            delivery_spec={"width": 37, "height": 29, "format": "png", "fit": "cover"},
        )
        session.commit()
        job_id = creation.job.id

    def claim(attempt_id: str) -> str | None:
        with session_factory() as claim_session:
            claimed = claim_delivery_rendition_job(
                claim_session,
                job_id=job_id,
                attempt_id=attempt_id,
            )
            return None if claimed is None else claimed.attempt_id

    with ThreadPoolExecutor(max_workers=2) as executor:
        claimed_attempts = list(executor.map(claim, ("attempt-a", "attempt-b")))
    winner = next(attempt for attempt in claimed_attempts if attempt is not None)
    assert sum(attempt is not None for attempt in claimed_attempts) == 1

    with session_factory() as session:
        running = session.get(DeliveryRenditionJob, job_id)
        assert running is not None
        running.started_at = datetime.now(UTC) - timedelta(hours=2)
        session.commit()

    recovered_ids: list[str] = []
    summary = recover_unfinished_delivery_rendition_jobs(
        enqueue=recovered_ids.append,
        reset_stale_running=True,
        stale_running_after=timedelta(minutes=30),
    )
    assert summary.stale_running_jobs == 1
    assert summary.enqueued_jobs == 1
    assert recovered_ids == [job_id]

    with session_factory() as session:
        assert (
            _fail_delivery_rendition_job(
                session,
                job_id=job_id,
                attempt_id=winner,
                reason="late worker",
                retryable=True,
            )
            is False
        )
        reclaimed = claim_delivery_rendition_job(session, job_id=job_id, attempt_id="attempt-c")
        assert reclaimed is not None
        assert _fail_delivery_rendition_job(
            session,
            job_id=job_id,
            attempt_id="attempt-c",
            reason="temporary storage failure",
            retryable=True,
        )
        retried = retry_delivery_rendition_job(session, job_id=job_id, enqueue=lambda _: None)
        assert retried.status == JobStatus.QUEUED
        assert retried.attempts == 2


def test_concurrent_v2_image_completions_fill_empty_cover_once_on_postgres(
    live_delivery_dependencies: tuple[RedisBroker, ModuleType],
) -> None:
    _, _ = live_delivery_dependencies
    session_factory = get_session_factory()
    with session_factory() as session:
        product, workflow = _create_materialized_workflow(session, include_scene_before_hero=True)
        clear_product_cover(session, product_id=product.id)
        image_nodes = sorted(
            (node for node in workflow.nodes if node.node_type.value == "image_generation"),
            key=lambda node: node.config_json["cover_priority"],
        )
        assert len(image_nodes) == 2
        node_run_ids = [
            _queue_single_node_run(session, workflow=workflow, node=node)[1].id for node in image_nodes
        ]
        product_id = product.id

    provider_barrier = Barrier(2)

    class ConcurrentImageProvider(RecordingImageProvider):
        def generate_workflow_image(self, request):
            provider_barrier.wait(timeout=10)
            return super().generate_workflow_image(request)

    def execute_node(item: tuple[str, tuple[int, int, int]]) -> None:
        node_run_id, color = item
        with session_factory() as session:
            execute_v2_workflow_node_run(
                session,
                node_run_id=node_run_id,
                dependencies=WorkflowExecutionDependencies(
                    image_provider_resolver=lambda: ConcurrentImageProvider(
                        image_bytes=_png_bytes(color=color, size=(80, 64))
                    )
                ),
            )

    with ThreadPoolExecutor(max_workers=2) as executor:
        list(executor.map(execute_node, zip(node_run_ids, ((220, 80, 20), (40, 100, 190)), strict=True)))

    with session_factory() as session:
        records = list(
            session.scalars(
                sa.select(WorkflowImageGenerationRecord).where(
                    WorkflowImageGenerationRecord.workflow_node_run_id.in_(node_run_ids)
                )
            )
        )
        product = session.get(Product, product_id)
        assert product is not None
        assert len(records) == 2
        assert product.cover_image_asset_id in {record.result_asset_id for record in records}
        assert all(
            session.get(WorkflowNode, record.node_id).bound_image_asset_id == record.result_asset_id
            for record in records
        )
