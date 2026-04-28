from __future__ import annotations

from pathlib import Path

import pytest
from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker

from productflow_backend.config import get_settings
from productflow_backend.infrastructure.db.models import Base
from productflow_backend.infrastructure.db.session import get_engine, get_session_factory


@pytest.fixture()
def configured_env(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> Path:
    database_path = tmp_path / "test.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SETTINGS_ACCESS_TOKEN", "super-secret-settings-token")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("SESSION_COOKIE_SECURE", "false")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    monkeypatch.setenv("LOG_DIR", str(tmp_path / "logs"))
    monkeypatch.setenv("TEXT_PROVIDER_KIND", "mock")
    monkeypatch.setenv("IMAGE_PROVIDER_KIND", "mock")
    monkeypatch.setenv("POSTER_GENERATION_MODE", "template")

    get_settings.cache_clear()
    get_engine.cache_clear()
    get_session_factory.cache_clear()

    engine = create_engine(f"sqlite:///{database_path}", future=True, connect_args={"check_same_thread": False})
    Base.metadata.create_all(engine)
    yield storage_root

    Base.metadata.drop_all(engine)
    engine.dispose()
    get_settings.cache_clear()
    get_engine.cache_clear()
    get_session_factory.cache_clear()


@pytest.fixture()
def db_session(configured_env: Path):
    factory: sessionmaker = get_session_factory()
    session = factory()
    try:
        yield session
    finally:
        session.close()


@pytest.fixture(autouse=True)
def _execute_image_session_queue_inline(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep image-session route tests deterministic while production delivery goes through Dramatiq."""

    from productflow_backend.application.image_sessions import execute_image_session_generation_task

    monkeypatch.setattr(
        "productflow_backend.application.image_sessions.enqueue_image_session_generation_task",
        execute_image_session_generation_task,
    )
