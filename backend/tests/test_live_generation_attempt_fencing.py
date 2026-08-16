from __future__ import annotations

import os
from collections.abc import Iterator
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
from datetime import UTC, datetime, timedelta
from pathlib import Path
from threading import Barrier, Event, current_thread
from uuid import uuid4

import pytest
import sqlalchemy as sa
from helpers import _make_demo_image_bytes
from sqlalchemy.engine import URL, make_url

from productflow_backend.application.durable_recovery import (
    recover_unfinished_image_session_generation_tasks,
    recover_unfinished_workflow_runs,
)
from productflow_backend.application.image_sessions import (
    ImageSessionGenerationStaleAttemptError,
    _execute_image_session_round_generation,
    _mark_image_generation_task_running,
    create_image_session,
    create_image_session_generation_task,
)
from productflow_backend.application.product_workflow.run_state import claim_workflow_node_run
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import JobStatus, WorkflowNodeStatus, WorkflowNodeType, WorkflowRunStatus
from productflow_backend.infrastructure.db.models import (
    Base,
    ImageSessionAsset,
    ImageSessionGenerationTask,
    ImageSessionRound,
    Product,
    ProductWorkflow,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)
from productflow_backend.infrastructure.db.session import get_engine, get_session_factory
from productflow_backend.infrastructure.image.chat_service import GeneratedChatImage
from productflow_backend.infrastructure.storage import LocalStorage

LIVE_ATTEMPT_FENCING_SWITCH = "PRODUCTFLOW_RUN_LIVE_GENERATION_ATTEMPT_FENCING"

pytestmark = [
    pytest.mark.live_dependencies,
    pytest.mark.skipif(
        os.getenv(LIVE_ATTEMPT_FENCING_SWITCH) != "1",
        reason=f"set {LIVE_ATTEMPT_FENCING_SWITCH}=1 to run the PostgreSQL attempt-fencing gate",
    ),
]


def _required_environment(name: str) -> str:
    value = os.getenv(name, "").strip()
    if not value:
        pytest.fail(f"{name} must be provided by the development environment", pytrace=False)
    return value


@contextmanager
def _temporary_postgres_database(base_database_url: str) -> Iterator[URL]:
    try:
        base_url = make_url(base_database_url)
    except Exception:
        pytest.fail("DATABASE_URL must be a valid SQLAlchemy URL", pytrace=False)
    if base_url.get_backend_name() != "postgresql":
        pytest.fail("DATABASE_URL must point to PostgreSQL for the live attempt-fencing gate", pytrace=False)

    database_name = f"productflow_live_attempt_fencing_{uuid4().hex}"
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


def _reset_database_state() -> None:
    cached_engine = get_engine() if get_engine.cache_info().currsize else None
    get_session_factory.cache_clear()
    try:
        if cached_engine is not None:
            cached_engine.dispose()
    finally:
        get_engine.cache_clear()
        get_settings.cache_clear()


@pytest.fixture()
def live_attempt_fencing_database(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> Iterator[Path]:
    with _temporary_postgres_database(_required_environment("DATABASE_URL")) as database_url:
        with monkeypatch.context() as environment:
            storage_root = tmp_path / "storage"
            environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
            environment.setenv("STORAGE_ROOT", str(storage_root))
            environment.setenv("LOG_DIR", str(tmp_path / "logs"))
            _reset_database_state()
            try:
                Base.metadata.create_all(get_engine())
                yield storage_root
            finally:
                _reset_database_state()
        _reset_database_state()


def test_postgres_claims_each_generation_row_once(live_attempt_fencing_database: Path) -> None:
    session_factory = get_session_factory()
    with session_factory() as session:
        image_session = create_image_session(session, title="Live claim fencing")
        image_task = create_image_session_generation_task(
            session,
            image_session_id=image_session.id,
            prompt="claim once",
            size="1024x1024",
        ).task
        product = Product(name="Live workflow claim")
        workflow = ProductWorkflow(product=product, title="Live workflow claim")
        node = WorkflowNode(
            workflow=workflow,
            node_type=WorkflowNodeType.PROMPT_GENERATION,
            title="Prompt",
            status=WorkflowNodeStatus.QUEUED,
        )
        run = WorkflowRun(workflow=workflow, status=WorkflowRunStatus.RUNNING)
        node_run = WorkflowNodeRun(workflow_run=run, node=node, status=WorkflowNodeStatus.QUEUED)
        session.add(product)
        session.commit()
        image_task_id = image_task.id
        node_run_id = node_run.id
        node_id = node.id

    image_barrier = Barrier(2)

    def claim_image(attempt_id: str) -> str | None:
        with session_factory() as session:
            task = session.get(ImageSessionGenerationTask, image_task_id)
            assert task is not None
            image_barrier.wait(timeout=5)
            return _mark_image_generation_task_running(session, task, attempt_id=attempt_id).attempt_id

    with ThreadPoolExecutor(max_workers=2) as executor:
        image_claims = list(executor.map(claim_image, ("image-attempt-a", "image-attempt-b")))
    assert sum(attempt is not None for attempt in image_claims) == 1

    workflow_barrier = Barrier(2)

    def claim_workflow(attempt_id: str) -> str | None:
        with session_factory() as session:
            workflow_barrier.wait(timeout=5)
            return claim_workflow_node_run(
                session,
                node_run_id=node_run_id,
                node_id=node_id,
                attempt_id=attempt_id,
            ).attempt_id

    with ThreadPoolExecutor(max_workers=2) as executor:
        workflow_claims = list(executor.map(claim_workflow, ("workflow-attempt-a", "workflow-attempt-b")))
    assert sum(attempt is not None for attempt in workflow_claims) == 1

    with session_factory() as session:
        persisted_image_task = session.get(ImageSessionGenerationTask, image_task_id)
        persisted_node_run = session.get(WorkflowNodeRun, node_run_id)
        assert persisted_image_task is not None
        assert persisted_image_task.status == JobStatus.RUNNING
        assert persisted_image_task.attempts == 1
        assert persisted_image_task.active_attempt_id in image_claims
        assert persisted_node_run is not None
        assert persisted_node_run.status == WorkflowNodeStatus.RUNNING
        assert persisted_node_run.attempts == 1
        assert persisted_node_run.active_attempt_id in workflow_claims


def test_postgres_image_recovery_cas_preserves_new_attempt(live_attempt_fencing_database: Path) -> None:
    session_factory = get_session_factory()
    old_time = datetime.now(UTC) - timedelta(hours=2)
    with session_factory() as session:
        image_session = create_image_session(session, title="Live recovery CAS")
        task = create_image_session_generation_task(
            session,
            image_session_id=image_session.id,
            prompt="do not overwrite reclaimed attempt",
            size="1024x1024",
        ).task
        task.status = JobStatus.RUNNING
        task.attempts = 1
        task.active_attempt_id = "old-image-attempt"
        task.started_at = old_time
        task.progress_updated_at = old_time
        session.commit()
        task_id = task.id

    update_entered = Event()
    continue_update = Event()
    engine = get_engine()

    def pause_recovery_update(
        connection: object,
        cursor: object,
        statement: str,
        parameters: object,
        context: object,
        executemany: bool,
    ) -> None:
        del connection, cursor, parameters, context, executemany
        if current_thread().name.startswith("image-recovery") and statement.startswith(
            "UPDATE image_session_generation_tasks"
        ):
            update_entered.set()
            assert continue_update.wait(timeout=5)

    sa.event.listen(engine, "before_cursor_execute", pause_recovery_update)
    enqueued: list[str] = []
    try:
        with ThreadPoolExecutor(max_workers=1, thread_name_prefix="image-recovery") as executor:
            future = executor.submit(
                recover_unfinished_image_session_generation_tasks,
                enqueue=enqueued.append,
                reset_stale_running=True,
                stale_running_after=timedelta(minutes=30),
            )
            assert update_entered.wait(timeout=5)
            with session_factory() as session:
                replaced = session.execute(
                    sa.update(ImageSessionGenerationTask)
                    .where(
                        ImageSessionGenerationTask.id == task_id,
                        ImageSessionGenerationTask.active_attempt_id == "old-image-attempt",
                    )
                    .values(
                        active_attempt_id="new-image-attempt",
                        attempts=2,
                        started_at=datetime.now(UTC),
                        progress_updated_at=datetime.now(UTC),
                    )
                )
                assert replaced.rowcount == 1
                session.commit()
            continue_update.set()
            summary = future.result(timeout=5)
    finally:
        continue_update.set()
        sa.event.remove(engine, "before_cursor_execute", pause_recovery_update)

    with session_factory() as session:
        persisted = session.get(ImageSessionGenerationTask, task_id)
        assert persisted is not None
        assert persisted.status == JobStatus.RUNNING
        assert persisted.active_attempt_id == "new-image-attempt"
        assert persisted.attempts == 2
    assert summary.stale_running_tasks == 0
    assert summary.enqueued_tasks == 0
    assert enqueued == []


def test_postgres_workflow_recovery_cas_preserves_new_attempt(live_attempt_fencing_database: Path) -> None:
    session_factory = get_session_factory()
    old_time = datetime.now(UTC) - timedelta(hours=2)
    with session_factory() as session:
        product = Product(name="Live workflow recovery CAS")
        workflow = ProductWorkflow(product=product, title="Live workflow recovery CAS")
        node = WorkflowNode(
            workflow=workflow,
            node_type=WorkflowNodeType.PROMPT_GENERATION,
            title="Prompt",
            status=WorkflowNodeStatus.RUNNING,
        )
        run = WorkflowRun(workflow=workflow, status=WorkflowRunStatus.RUNNING)
        node_run = WorkflowNodeRun(
            workflow_run=run,
            node=node,
            status=WorkflowNodeStatus.RUNNING,
            attempts=1,
            active_attempt_id="old-workflow-attempt",
            started_at=old_time,
        )
        session.add(product)
        session.commit()
        run_id = run.id
        node_run_id = node_run.id

    update_entered = Event()
    continue_update = Event()
    engine = get_engine()

    def pause_recovery_update(
        connection: object,
        cursor: object,
        statement: str,
        parameters: object,
        context: object,
        executemany: bool,
    ) -> None:
        del connection, cursor, parameters, context, executemany
        if current_thread().name.startswith("workflow-recovery") and statement.startswith(
            "UPDATE workflow_node_runs"
        ):
            update_entered.set()
            assert continue_update.wait(timeout=5)

    sa.event.listen(engine, "before_cursor_execute", pause_recovery_update)
    enqueued: list[str] = []
    try:
        with ThreadPoolExecutor(max_workers=1, thread_name_prefix="workflow-recovery") as executor:
            future = executor.submit(
                recover_unfinished_workflow_runs,
                enqueue=enqueued.append,
                reset_stale_running=True,
                stale_running_after=timedelta(minutes=30),
            )
            assert update_entered.wait(timeout=5)
            with session_factory() as session:
                replaced = session.execute(
                    sa.update(WorkflowNodeRun)
                    .where(
                        WorkflowNodeRun.id == node_run_id,
                        WorkflowNodeRun.active_attempt_id == "old-workflow-attempt",
                    )
                    .values(
                        active_attempt_id="new-workflow-attempt",
                        attempts=2,
                        started_at=datetime.now(UTC),
                    )
                )
                assert replaced.rowcount == 1
                session.commit()
            continue_update.set()
            summary = future.result(timeout=5)
    finally:
        continue_update.set()
        sa.event.remove(engine, "before_cursor_execute", pause_recovery_update)

    with session_factory() as session:
        persisted_run = session.get(WorkflowRun, run_id)
        persisted_node_run = session.get(WorkflowNodeRun, node_run_id)
        assert persisted_run is not None
        assert persisted_run.status == WorkflowRunStatus.RUNNING
        assert persisted_node_run is not None
        assert persisted_node_run.status == WorkflowNodeStatus.RUNNING
        assert persisted_node_run.active_attempt_id == "new-workflow-attempt"
        assert persisted_node_run.attempts == 2
    assert summary.stale_running_runs == 0
    assert summary.enqueued_runs == 0
    assert enqueued == []


def test_postgres_stale_image_result_writes_no_media_or_children(
    live_attempt_fencing_database: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    session_factory = get_session_factory()
    with session_factory() as session:
        image_session = create_image_session(session, title="Live stale provider result")
        task = create_image_session_generation_task(
            session,
            image_session_id=image_session.id,
            prompt="stale provider result",
            size="1024x1024",
        ).task
        claim = _mark_image_generation_task_running(session, task, attempt_id="old-provider-attempt")
        assert claim.claimed is True
        task_id = task.id
        image_session_id = image_session.id
        prompt = task.prompt
        size = task.size

    def generate_after_reclaim(self, **kwargs) -> GeneratedChatImage:
        del self
        with session_factory() as session:
            replaced = session.execute(
                sa.update(ImageSessionGenerationTask)
                .where(
                    ImageSessionGenerationTask.id == task_id,
                    ImageSessionGenerationTask.active_attempt_id == "old-provider-attempt",
                )
                .values(
                    active_attempt_id="new-provider-attempt",
                    attempts=2,
                    started_at=datetime.now(UTC),
                    progress_updated_at=datetime.now(UTC),
                )
            )
            assert replaced.rowcount == 1
            session.commit()
        return GeneratedChatImage(
            bytes_data=_make_demo_image_bytes(),
            mime_type="image/png",
            model_name="live-test",
            provider_name="live-test",
            prompt_version="live-test-v1",
            size=kwargs["size"],
            generated_at=datetime.now(UTC),
            provider_request_json={"size": kwargs["size"]},
            provider_output_json={},
        )

    monkeypatch.setattr(
        "productflow_backend.infrastructure.image.chat_service.ImageChatService.generate",
        generate_after_reclaim,
    )
    with session_factory() as session, pytest.raises(ImageSessionGenerationStaleAttemptError):
        _execute_image_session_round_generation(
            session,
            image_session_id=image_session_id,
            prompt=prompt,
            size=size,
            generation_task_id=task_id,
            generation_attempt_id="old-provider-attempt",
            storage=LocalStorage(),
        )

    with session_factory() as session:
        persisted = session.get(ImageSessionGenerationTask, task_id)
        assert persisted is not None
        assert persisted.status == JobStatus.RUNNING
        assert persisted.active_attempt_id == "new-provider-attempt"
        assert persisted.attempts == 2
        assert session.scalar(sa.select(sa.func.count()).select_from(ImageSessionRound)) == 0
        assert session.scalar(sa.select(sa.func.count()).select_from(ImageSessionAsset)) == 0
    assert not live_attempt_fencing_database.exists() or not any(
        path.is_file() for path in live_attempt_fencing_database.rglob("*")
    )
