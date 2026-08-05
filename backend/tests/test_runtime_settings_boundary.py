from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

import pytest
from sqlalchemy import create_engine
from sqlalchemy.orm import Session

from productflow_backend.application.runtime_settings import get_runtime_settings
from productflow_backend.config import (
    filter_image_tool_options,
    normalize_image_generation_size,
)
from productflow_backend.infrastructure import provider_config, runtime_config_store
from productflow_backend.infrastructure.db.models import AppSetting
from productflow_backend.infrastructure.provider_config import resolve_text_provider_config


def test_config_import_does_not_initialize_database_modules() -> None:
    src_dir = Path(__file__).resolve().parents[1] / "src"
    script = """
import sys
import sqlalchemy

def fail_create_engine(*args, **kwargs):
    raise AssertionError("config import initialized a database engine")

sqlalchemy.create_engine = fail_create_engine
import productflow_backend.config

assert "productflow_backend.infrastructure.db.models" not in sys.modules
assert "productflow_backend.infrastructure.db.session" not in sys.modules
"""
    env = {**os.environ, "PYTHONPATH": str(src_dir)}
    result = subprocess.run(
        [sys.executable, "-c", script],
        cwd=src_dir.parent,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0, result.stderr


def test_config_import_after_database_session_import_does_not_initialize_engine() -> None:
    src_dir = Path(__file__).resolve().parents[1] / "src"
    script = """
import sqlalchemy

def fail_create_engine(*args, **kwargs):
    raise AssertionError("module import initialized a database engine")

sqlalchemy.create_engine = fail_create_engine
import productflow_backend.infrastructure.db.session
import productflow_backend.config
"""
    env = {**os.environ, "PYTHONPATH": str(src_dir)}
    result = subprocess.run(
        [sys.executable, "-c", script],
        cwd=src_dir.parent,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0, result.stderr


def test_runtime_settings_reuses_supplied_session_without_opening_another(
    configured_env: Path,
    db_session: Session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    db_session.add(AppSetting(key="generation_max_concurrent_tasks", value="7"))
    db_session.add(AppSetting(key="database_url", value="sqlite:///database-override.db"))
    db_session.commit()

    def fail_get_session_factory():
        raise AssertionError("supplied session should avoid opening an owned session")

    monkeypatch.setattr(runtime_config_store, "get_session_factory", fail_get_session_factory)

    settings = get_runtime_settings(db_session)

    assert settings.generation_max_concurrent_tasks == 7
    assert settings.database_url != "sqlite:///database-override.db"


def test_runtime_settings_falls_back_to_env_when_app_settings_table_is_missing(
    configured_env: Path,
) -> None:
    engine = create_engine("sqlite:///:memory:", future=True)
    session = Session(engine)
    try:
        settings = get_runtime_settings(session)
    finally:
        session.close()
        engine.dispose()

    assert settings.generation_max_concurrent_tasks == 3
    assert settings.admin_access_required is True


def test_provider_resolver_reuses_supplied_session_without_closing_it(
    configured_env: Path,
    db_session: Session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    def fail_get_session_factory():
        raise AssertionError("supplied session should avoid opening an owned session")

    close_calls = 0
    original_close = db_session.close

    def track_close():
        nonlocal close_calls
        close_calls += 1
        original_close()

    monkeypatch.setattr(provider_config, "get_session_factory", fail_get_session_factory)
    monkeypatch.setattr(db_session, "close", track_close)

    resolved = resolve_text_provider_config(session=db_session)

    assert resolved.provider_kind == "mock"
    assert close_calls == 0


def test_config_helpers_use_explicit_runtime_limits() -> None:
    assert normalize_image_generation_size("2048x1024", max_dimension=1024) == "1024x512"
    assert filter_image_tool_options({"model": "gpt-image-2"}, allowed_fields=()) is None
