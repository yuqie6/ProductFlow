from __future__ import annotations

import logging
import sys
from collections.abc import Iterable
from copy import copy
from http import HTTPStatus
from logging.handlers import RotatingFileHandler
from pathlib import Path

from productflow_backend.config import Settings, get_settings
from productflow_backend.infrastructure.db.models import utcnow

_PRODUCTFLOW_FILE_HANDLER = "_productflow_file_handler"
_PRODUCTFLOW_STREAM_HANDLER = "_productflow_stream_handler"
_UVICORN_FILE_LOGGERS = ("uvicorn.error", "uvicorn.access")


def get_log_file_path(settings: Settings | None = None) -> Path:
    """Return the effective persistent ProductFlow log file path."""

    settings = settings or get_settings()
    return settings.log_dir.expanduser() / "productflow.log"


def configure_logging(settings: Settings | None = None) -> None:
    """Configure stdout plus persistent rotating file logs for the current process.

    Uvicorn installs its own non-root loggers for server lifecycle and access records. Those loggers do not reliably
    propagate to the root logger, so ProductFlow mirrors them to the same rotating file handler while leaving their
    existing console handlers untouched.
    """

    settings = settings or get_settings()
    level_name = settings.log_level.upper()
    level = getattr(logging, level_name, logging.INFO)
    log_file = get_log_file_path(settings)
    log_dir = log_file.parent
    log_dir.mkdir(parents=True, exist_ok=True)

    formatter = _ProductFlowFormatter("%(asctime)s %(levelname)s [%(name)s] %(message)s")
    root_logger = logging.getLogger()
    root_logger.setLevel(level)

    stream_handler_exists = any(_is_console_stream_handler(handler) for handler in root_logger.handlers)
    if not stream_handler_exists:
        stream_handler = logging.StreamHandler(sys.stdout)
        stream_handler.setFormatter(formatter)
        stream_handler.setLevel(level)
        setattr(stream_handler, _PRODUCTFLOW_STREAM_HANDLER, True)
        root_logger.addHandler(stream_handler)

    file_handler = _ensure_shared_file_handler(root_logger, log_file, settings, formatter, level)
    _mirror_uvicorn_logs_to_file(file_handler, level)
    logging.getLogger(__name__).info("持久化日志已启用: file=%s", log_file.resolve())
    # Keep a strong reference in this frame until every target logger has been configured.
    file_handler.flush()


def cleanup_old_logs(settings: Settings | None = None) -> int:
    """Delete log files in the configured directory older than retention days."""

    settings = settings or get_settings()
    retention_days = settings.log_retention_days
    if retention_days <= 0:
        return 0
    log_dir = settings.log_dir.expanduser()
    if not log_dir.exists():
        return 0
    cutoff = utcnow().timestamp() - retention_days * 24 * 60 * 60
    deleted = 0
    for path in log_dir.glob("*.log*"):
        if not path.is_file():
            continue
        try:
            if path.stat().st_mtime >= cutoff:
                continue
            path.unlink()
            deleted += 1
        except OSError:
            logging.getLogger(__name__).exception("清理日志文件失败: path=%s", path)
    logging.getLogger(__name__).info(
        "日志清理完成: dir=%s deleted=%s retention_days=%s",
        log_dir,
        deleted,
        retention_days,
    )
    return deleted


class _ProductFlowFormatter(logging.Formatter):
    """Project formatter that preserves Uvicorn's human access status phrase in file logs."""

    def format(self, record: logging.LogRecord) -> str:
        if record.name == "uvicorn.access":
            record = _with_uvicorn_status_phrase(record)
        return super().format(record)


def _ensure_shared_file_handler(
    logger: logging.Logger,
    log_file: Path,
    settings: Settings,
    formatter: logging.Formatter,
    level: int,
) -> RotatingFileHandler:
    """Install one shared ProductFlow file handler for all persistent log mirrors."""

    resolved_log_file = log_file.resolve()
    matching_handler = _find_matching_file_handler(resolved_log_file)

    if matching_handler is None:
        matching_handler = RotatingFileHandler(
            log_file,
            maxBytes=settings.log_max_bytes,
            backupCount=settings.log_backup_count,
            encoding="utf-8",
        )
        setattr(matching_handler, _PRODUCTFLOW_FILE_HANDLER, True)

    matching_handler.setFormatter(formatter)
    matching_handler.setLevel(level)
    for configured_logger in _configured_file_loggers():
        _remove_productflow_file_handlers(configured_logger, keep=matching_handler)
    if matching_handler not in logger.handlers:
        logger.addHandler(matching_handler)
    return matching_handler


def _is_console_stream_handler(handler: logging.Handler) -> bool:
    return isinstance(handler, logging.StreamHandler) and not isinstance(handler, logging.FileHandler)


def _mirror_uvicorn_logs_to_file(
    file_handler: RotatingFileHandler,
    level: int,
) -> None:
    """Mirror Uvicorn lifecycle/access loggers when their records cannot reach the root file handler."""

    for logger_name in _UVICORN_FILE_LOGGERS:
        logger = logging.getLogger(logger_name)
        if _logger_records_reach_root(logger):
            _remove_productflow_file_handlers(logger, keep=None)
            continue
        logger.setLevel(level)
        _remove_productflow_file_handlers(logger, keep=file_handler)
        if file_handler not in logger.handlers:
            logger.addHandler(file_handler)


def _configured_file_loggers() -> Iterable[logging.Logger]:
    yield logging.getLogger()
    for logger_name in _UVICORN_FILE_LOGGERS:
        yield logging.getLogger(logger_name)


def _find_matching_file_handler(resolved_log_file: Path) -> RotatingFileHandler | None:
    for logger in _configured_file_loggers():
        for handler in logger.handlers:
            if not getattr(handler, _PRODUCTFLOW_FILE_HANDLER, False):
                continue
            if isinstance(handler, RotatingFileHandler) and Path(handler.baseFilename).resolve() == resolved_log_file:
                return handler
    return None


def _with_uvicorn_status_phrase(record: logging.LogRecord) -> logging.LogRecord:
    """Copy an Uvicorn access record and append the HTTP status phrase when Uvicorn supplies raw args."""

    if not isinstance(record.args, tuple) or len(record.args) < 5:
        return record
    client_addr, method, full_path, http_version, status_code = record.args[:5]
    if not isinstance(status_code, int):
        return record
    try:
        status = f"{status_code} {HTTPStatus(status_code).phrase}"
    except ValueError:
        status = str(status_code)
    access_record = copy(record)
    access_record.msg = '%s - "%s %s HTTP/%s" %s'
    access_record.args = (client_addr, method, full_path, http_version, status)
    return access_record


def _logger_records_reach_root(logger: logging.Logger) -> bool:
    """Return whether records logged on logger would propagate to the root logger."""

    current: logging.Logger = logger
    while current.name:
        if not current.propagate:
            return False
        parent = current.parent
        if parent is None:
            return True
        current = parent
    return True


def _remove_productflow_file_handlers(
    logger: logging.Logger,
    *,
    keep: RotatingFileHandler | None,
) -> None:
    for handler in list(logger.handlers):
        if handler is keep or not getattr(handler, _PRODUCTFLOW_FILE_HANDLER, False):
            continue
        logger.removeHandler(handler)
        if not any(handler in configured_logger.handlers for configured_logger in _configured_file_loggers()):
            handler.close()
