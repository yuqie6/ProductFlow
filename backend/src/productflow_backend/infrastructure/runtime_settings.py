"""从环境变量和数据库覆盖组装生效的运行时设置。"""

from __future__ import annotations

from sqlalchemy.orm import Session

from productflow_backend.config import Settings, build_settings_with_overrides, get_settings
from productflow_backend.infrastructure.runtime_config_store import load_runtime_overrides


def get_runtime_settings(session: Session | None = None) -> Settings:
    """为一次应用操作组装生效的设置快照。"""

    overrides = load_runtime_overrides(session)
    if not overrides:
        return get_settings()
    return build_settings_with_overrides(overrides)
