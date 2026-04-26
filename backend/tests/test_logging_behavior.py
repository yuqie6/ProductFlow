from __future__ import annotations

import logging
import os
import time
from pathlib import Path

import pytest

from productflow_backend.config import get_settings


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
