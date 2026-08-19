from __future__ import annotations

import logging
import os
import time
from collections.abc import Iterator
from pathlib import Path

import pytest
from fastapi import Response
from fastapi.testclient import TestClient

from productflow_backend.config import get_settings


@pytest.fixture(autouse=True)
def _clean_log_context() -> Iterator[None]:
    from productflow_backend.infrastructure.logging import (
        reset_image_session_generation_task_id,
        reset_request_id,
        reset_workflow_node_run_id,
        reset_workflow_run_id,
        set_image_session_generation_task_id,
        set_request_id,
        set_workflow_node_run_id,
        set_workflow_run_id,
    )

    request_token = set_request_id("-")
    workflow_token = set_workflow_run_id("-")
    workflow_node_token = set_workflow_node_run_id("-")
    task_token = set_image_session_generation_task_id("-")
    try:
        yield
    finally:
        reset_image_session_generation_task_id(task_token)
        reset_workflow_node_run_id(workflow_node_token)
        reset_workflow_run_id(workflow_token)
        reset_request_id(request_token)


@pytest.fixture(autouse=True)
def _restore_logging_state() -> Iterator[None]:
    loggers = (
        logging.getLogger(),
        logging.getLogger("uvicorn"),
        logging.getLogger("uvicorn.error"),
        logging.getLogger("uvicorn.access"),
    )
    original_state = {
        logger.name: (list(logger.handlers), logger.level, logger.propagate, logger.disabled)
        for logger in loggers
    }
    # Alembic env.py calls logging.config.fileConfig, which can disable existing
    # uvicorn loggers in the same pytest process. These tests intentionally
    # exercise uvicorn mirroring, so start from an enabled baseline.
    for logger in loggers:
        logger.disabled = False
    try:
        yield
    finally:
        for logger in loggers:
            saved_handlers, saved_level, saved_propagate, saved_disabled = original_state[logger.name]
            for handler in list(logger.handlers):
                if handler not in saved_handlers:
                    logger.removeHandler(handler)
                    handler.close()
            logger.handlers = saved_handlers
            logger.setLevel(saved_level)
            logger.propagate = saved_propagate
            logger.disabled = saved_disabled


def test_default_log_dir_uses_backend_storage_when_running_from_backend(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.infrastructure.logging import get_log_file_path

    backend_dir = Path(__file__).resolve().parents[1]
    monkeypatch.delenv("LOG_DIR", raising=False)
    monkeypatch.chdir(backend_dir)
    get_settings.cache_clear()

    settings = get_settings()

    assert settings.log_dir == backend_dir / "storage" / "logs"
    assert get_log_file_path(settings) == backend_dir / "storage" / "logs" / "productflow.log"

def test_log_cleanup_deletes_expired_persistent_logs(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    from productflow_backend.infrastructure.logging import cleanup_old_logs

    log_dir = tmp_path / "logs"
    log_dir.mkdir()
    old_log = log_dir / "old.log"
    fresh_log = log_dir / "fresh.log"
    old_log.write_text("old", encoding="utf-8")
    fresh_log.write_text("fresh", encoding="utf-8")
    old_timestamp = time.time() - 3 * 24 * 60 * 60
    old_log.touch()
    fresh_log.touch()
    os.utime(old_log, (old_timestamp, old_timestamp))
    monkeypatch.setenv("LOG_DIR", str(log_dir))
    monkeypatch.setenv("LOG_RETENTION_DAYS", "1")
    get_settings.cache_clear()

    deleted = cleanup_old_logs(get_settings())

    assert deleted == 1
    assert not old_log.exists()
    assert fresh_log.exists()

def test_configure_logging_keeps_single_stdout_handler_and_log_dir_override(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    from productflow_backend.infrastructure.logging import configure_logging, get_log_file_path

    log_dir = tmp_path / "stdout-logs"
    monkeypatch.setenv("LOG_DIR", str(log_dir))
    monkeypatch.setenv("LOG_LEVEL", "INFO")
    get_settings.cache_clear()
    settings = get_settings()

    root_logger = logging.getLogger()
    original_handlers = list(root_logger.handlers)
    original_level = root_logger.level
    original_propagate = root_logger.propagate

    try:
        root_logger.handlers = []
        configure_logging(settings)
        configure_logging(settings)

        logging.getLogger("productflow_backend.tests.stdout").info("stdout and file visible line")
        for handler in root_logger.handlers:
            handler.flush()

        productflow_stream_handlers = [
            handler for handler in root_logger.handlers if getattr(handler, "_productflow_stream_handler", False)
        ]
        productflow_file_handlers = [
            handler for handler in root_logger.handlers if getattr(handler, "_productflow_file_handler", False)
        ]
        captured = capsys.readouterr()
        log_text = get_log_file_path(settings).read_text(encoding="utf-8")

        assert get_log_file_path(settings).parent == log_dir
        assert len(productflow_stream_handlers) == 1
        assert len(productflow_file_handlers) == 1
        assert "stdout and file visible line" in captured.out
        assert log_text.count("stdout and file visible line") == 1
    finally:
        for handler in list(root_logger.handlers):
            root_logger.removeHandler(handler)
            handler.close()
        root_logger.handlers = original_handlers
        root_logger.setLevel(original_level)
        root_logger.propagate = original_propagate

def test_configure_logging_mirrors_uvicorn_lifecycle_and_access_logs(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    from productflow_backend.infrastructure.logging import configure_logging, get_log_file_path

    log_dir = tmp_path / "uvicorn-logs"
    monkeypatch.setenv("LOG_DIR", str(log_dir))
    monkeypatch.setenv("LOG_LEVEL", "INFO")
    get_settings.cache_clear()
    settings = get_settings()

    root_logger = logging.getLogger()
    uvicorn_logger = logging.getLogger("uvicorn")
    error_logger = logging.getLogger("uvicorn.error")
    access_logger = logging.getLogger("uvicorn.access")
    loggers = (root_logger, uvicorn_logger, error_logger, access_logger)
    original_state = {
        logger.name: (list(logger.handlers), logger.level, logger.propagate)
        for logger in loggers
    }

    try:
        uvicorn_logger.propagate = False
        error_logger.propagate = True
        access_logger.propagate = False

        configure_logging(settings)
        configure_logging(settings)

        logging.getLogger("productflow_backend.tests.logging").info("application persistent line")
        error_logger.info("Started server process [12345]")
        error_logger.info("Application startup complete.")
        access_logger.info('%s - "%s %s HTTP/%s" %d', "127.0.0.1:29282", "GET", "/healthz", "1.1", 200)
        for logger in loggers:
            for handler in logger.handlers:
                handler.flush()

        log_text = get_log_file_path(settings).read_text(encoding="utf-8")

        assert log_text.count("application persistent line") == 1
        assert log_text.count("Started server process [12345]") == 1
        assert log_text.count("Application startup complete.") == 1
        assert log_text.count('127.0.0.1:29282 - "GET /healthz HTTP/1.1" 200 OK') == 1
        productflow_file_handlers = [
            handler
            for logger in (root_logger, error_logger, access_logger)
            for handler in logger.handlers
            if getattr(handler, "_productflow_file_handler", False)
        ]
        assert len({id(handler) for handler in productflow_file_handlers}) == 1
        assert not any(
            getattr(handler, "_productflow_stream_handler", False)
            for logger in (error_logger, access_logger)
            for handler in logger.handlers
        )
    finally:
        for logger in loggers:
            saved_handlers, saved_level, saved_propagate = original_state[logger.name]
            for handler in list(logger.handlers):
                if handler not in saved_handlers:
                    logger.removeHandler(handler)
                    handler.close()
            logger.handlers = saved_handlers
            logger.setLevel(saved_level)
            logger.propagate = saved_propagate


def test_logging_formatter_includes_current_context_and_stable_empty_context() -> None:
    from productflow_backend.infrastructure.logging import (
        _ProductFlowFormatter,
        reset_image_session_generation_task_id,
        reset_request_id,
        reset_workflow_node_run_id,
        reset_workflow_run_id,
        set_image_session_generation_task_id,
        set_request_id,
        set_workflow_node_run_id,
        set_workflow_run_id,
    )

    formatter = _ProductFlowFormatter(
        "request_id=%(request_id)s workflow_run_id=%(workflow_run_id)s workflow_node_run_id=%(workflow_node_run_id)s "
        "image_session_generation_task_id=%(image_session_generation_task_id)s %(message)s"
    )

    empty_record = logging.LogRecord(
        name="productflow_backend.tests.context",
        level=logging.INFO,
        pathname=__file__,
        lineno=1,
        msg="context placeholder line",
        args=(),
        exc_info=None,
    )
    assert formatter.format(empty_record) == (
        "request_id=- workflow_run_id=- workflow_node_run_id=- "
        "image_session_generation_task_id=- context placeholder line"
    )
    assert "request_id" not in empty_record.__dict__
    assert "workflow_run_id" not in empty_record.__dict__
    assert "workflow_node_run_id" not in empty_record.__dict__
    assert "image_session_generation_task_id" not in empty_record.__dict__

    request_token = set_request_id("request-1")
    workflow_token = set_workflow_run_id("workflow-run-1")
    workflow_node_token = set_workflow_node_run_id("workflow-node-run-1")
    task_token = set_image_session_generation_task_id("image-task-1")
    try:
        context_record = logging.LogRecord(
            name="productflow_backend.tests.context",
            level=logging.INFO,
            pathname=__file__,
            lineno=1,
            msg="context value line",
            args=(),
            exc_info=None,
        )

        assert formatter.format(context_record) == (
            "request_id=request-1 workflow_run_id=workflow-run-1 workflow_node_run_id=workflow-node-run-1 "
            "image_session_generation_task_id=image-task-1 context value line"
        )
        assert "request_id" not in context_record.__dict__
        assert "workflow_run_id" not in context_record.__dict__
        assert "workflow_node_run_id" not in context_record.__dict__
        assert "image_session_generation_task_id" not in context_record.__dict__
    finally:
        reset_image_session_generation_task_id(task_token)
        reset_workflow_node_run_id(workflow_node_token)
        reset_workflow_run_id(workflow_token)
        reset_request_id(request_token)


def test_request_id_middleware_returns_header_and_cleans_context(configured_env: Path) -> None:
    from productflow_backend.infrastructure.logging import current_log_context
    from productflow_backend.presentation.api import create_app

    app = create_app()

    @app.get("/tests/request-context")
    def request_context() -> dict[str, str]:
        return current_log_context()

    @app.get("/tests/request-context-overrides-header")
    def request_context_overrides_header(response: Response) -> dict[str, str]:
        response.headers["X-Request-ID"] = "route-request-id"
        return current_log_context()

    client = TestClient(app)

    supplied = client.get("/tests/request-context", headers={"X-Request-ID": "incoming-request-1"})
    generated = client.get("/tests/request-context")
    override = client.get(
        "/tests/request-context-overrides-header",
        headers={"X-Request-ID": "incoming-request-2"},
    )

    assert supplied.status_code == 200
    assert supplied.headers["X-Request-ID"] == "incoming-request-1"
    assert supplied.json() == {
        "request_id": "incoming-request-1",
        "workflow_run_id": "-",
        "workflow_node_run_id": "-",
        "image_session_generation_task_id": "-",
    }
    assert generated.status_code == 200
    assert generated.headers["X-Request-ID"]
    assert generated.json()["request_id"] == generated.headers["X-Request-ID"]
    assert override.status_code == 200
    assert override.headers["X-Request-ID"] == "incoming-request-2"
    assert override.headers.get_list("X-Request-ID") == ["incoming-request-2"]
    assert current_log_context()["request_id"] == "-"


def test_request_id_middleware_preserves_http_exception_body_header_and_context_cleanup(configured_env: Path) -> None:
    from fastapi import HTTPException

    from productflow_backend.infrastructure.logging import current_log_context
    from productflow_backend.presentation.api import create_app

    app = create_app()

    @app.get("/tests/request-error")
    def request_error() -> None:
        assert current_log_context()["request_id"] == "error-request-1"
        raise HTTPException(status_code=418, detail="teapot")

    client = TestClient(app)

    response = client.get("/tests/request-error", headers={"X-Request-ID": "error-request-1"})

    assert response.status_code == 418
    assert response.headers["X-Request-ID"] == "error-request-1"
    assert response.json() == {"detail": "teapot"}
    assert current_log_context()["request_id"] == "-"


def test_worker_actors_set_and_clear_log_context(monkeypatch: pytest.MonkeyPatch, configured_env: Path) -> None:
    from productflow_backend.infrastructure.image.chat_service import ImageChatService
    from productflow_backend.infrastructure.logging import current_log_context
    from productflow_backend.workers import (
        run_image_session_generation_task,
        run_product_workflow_node_run,
        run_product_workflow_run,
    )

    observed: list[dict[str, str]] = []

    def capture_workflow_context(workflow_run_id: str) -> None:
        assert workflow_run_id == "workflow-run-1"
        observed.append(current_log_context())

    def capture_workflow_node_context(workflow_node_run_id: str) -> None:
        assert workflow_node_run_id == "workflow-node-run-1"
        observed.append(current_log_context())

    def capture_image_task_context(task_id: str, **kwargs) -> None:
        assert task_id == "image-task-1"
        assert kwargs["chat_service_factory"] is ImageChatService
        observed.append(current_log_context())

    monkeypatch.setattr("productflow_backend.workers.execute_product_workflow_run", capture_workflow_context)
    monkeypatch.setattr("productflow_backend.workers.execute_product_workflow_node_run", capture_workflow_node_context)
    monkeypatch.setattr("productflow_backend.workers.execute_image_session_generation_task", capture_image_task_context)

    run_product_workflow_run.fn("workflow-run-1")
    assert current_log_context()["workflow_run_id"] == "-"

    run_product_workflow_node_run.fn("workflow-node-run-1")
    assert current_log_context()["workflow_node_run_id"] == "-"

    run_image_session_generation_task.fn("image-task-1")
    assert current_log_context()["image_session_generation_task_id"] == "-"

    assert observed == [
        {
            "request_id": "-",
            "workflow_run_id": "workflow-run-1",
            "workflow_node_run_id": "-",
            "image_session_generation_task_id": "-",
        },
        {
            "request_id": "-",
            "workflow_run_id": "-",
            "workflow_node_run_id": "workflow-node-run-1",
            "image_session_generation_task_id": "-",
        },
        {
            "request_id": "-",
            "workflow_run_id": "-",
            "workflow_node_run_id": "-",
            "image_session_generation_task_id": "image-task-1",
        },
    ]


def test_worker_actors_clear_log_context_when_execution_raises(
    monkeypatch: pytest.MonkeyPatch,
    configured_env: Path,
) -> None:
    from productflow_backend.infrastructure.image.chat_service import ImageChatService
    from productflow_backend.infrastructure.logging import current_log_context
    from productflow_backend.workers import (
        run_image_session_generation_task,
        run_product_workflow_node_run,
        run_product_workflow_run,
    )

    def raise_workflow_error(workflow_run_id: str) -> None:
        assert workflow_run_id == "workflow-run-error"
        assert current_log_context()["workflow_run_id"] == "workflow-run-error"
        raise RuntimeError("workflow failed")

    def raise_workflow_node_error(workflow_node_run_id: str) -> None:
        assert workflow_node_run_id == "workflow-node-run-error"
        assert current_log_context()["workflow_node_run_id"] == "workflow-node-run-error"
        assert current_log_context()["workflow_run_id"] == "-"
        raise RuntimeError("workflow node failed")

    def raise_image_task_error(task_id: str, **kwargs) -> None:
        assert task_id == "image-task-error"
        assert kwargs["chat_service_factory"] is ImageChatService
        assert current_log_context()["image_session_generation_task_id"] == "image-task-error"
        raise RuntimeError("image task failed")

    monkeypatch.setattr("productflow_backend.workers.execute_product_workflow_run", raise_workflow_error)
    monkeypatch.setattr("productflow_backend.workers.execute_product_workflow_node_run", raise_workflow_node_error)
    monkeypatch.setattr("productflow_backend.workers.execute_image_session_generation_task", raise_image_task_error)

    with pytest.raises(RuntimeError, match="workflow failed"):
        run_product_workflow_run.fn("workflow-run-error")
    assert current_log_context()["workflow_run_id"] == "-"

    with pytest.raises(RuntimeError, match="workflow node failed"):
        run_product_workflow_node_run.fn("workflow-node-run-error")
    assert current_log_context()["workflow_node_run_id"] == "-"

    with pytest.raises(RuntimeError, match="image task failed"):
        run_image_session_generation_task.fn("image-task-error")
    assert current_log_context()["image_session_generation_task_id"] == "-"
