from __future__ import annotations

from sqlalchemy.orm import Session

from productflow_backend.config import Settings, build_settings_with_overrides, get_settings
from productflow_backend.infrastructure.runtime_config_store import load_runtime_overrides


def get_runtime_settings(session: Session | None = None) -> Settings:
    """Build the effective settings snapshot for an application operation."""

    overrides = load_runtime_overrides(session)
    if not overrides:
        return get_settings()
    return build_settings_with_overrides(overrides)
