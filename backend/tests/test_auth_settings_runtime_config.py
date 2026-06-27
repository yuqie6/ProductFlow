from __future__ import annotations

from pathlib import Path

import itsdangerous.timed
import pytest
from fastapi.testclient import TestClient
from helpers import (
    _login,
    _unlock_settings,
)
from sqlalchemy import select

from productflow_backend.config import (
    CONFIG_DEFINITION_BY_KEY,
    RUNTIME_CONFIG_KEYS,
    get_runtime_settings,
    get_settings,
    normalize_config_values,
)
from productflow_backend.infrastructure.db.models import (
    AppSetting,
    ProviderBinding,
    ProviderProfile,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.provider_config import (
    resolve_image_provider_config,
    resolve_text_provider_config,
)


def test_auth_session_required(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)

    unauthorized = client.get("/api/products")
    assert unauthorized.status_code == 401

    wrong_key = client.post("/api/auth/session", json={"admin_key": "wrong-admin-key"})
    assert wrong_key.status_code == 401
    assert wrong_key.json()["detail"] == "管理员密钥不正确"

    _login(client)

    authorized = client.get("/api/products")
    assert authorized.status_code == 200
    assert authorized.json()["items"] == []


def test_auth_session_survives_small_wall_clock_rollback(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    current_timestamp = 1_800_000_000
    monkeypatch.setattr(itsdangerous.timed.time, "time", lambda: current_timestamp)
    app = create_app()
    client = TestClient(app)

    _login(client)

    current_timestamp -= 2
    authorized = client.get("/api/products")

    assert authorized.status_code == 200
    assert authorized.json()["items"] == []


def test_session_signer_does_not_keep_large_future_timestamp_after_clock_recovers(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.session import MonotonicTimestampSigner

    current_timestamp = 1_800_000_000
    monkeypatch.setattr(itsdangerous.timed.time, "time", lambda: current_timestamp)
    signer = MonotonicTimestampSigner("super-secret-session-key-123")

    future_signed = signer.sign(b"payload")
    current_timestamp -= 60
    recovered_signed = signer.sign(b"payload")

    assert signer.unsign(recovered_signed) == b"payload"
    assert future_signed != recovered_signed


def test_admin_access_can_be_disabled_and_re_enabled(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    admin_client = TestClient(app)
    _login(admin_client)
    _unlock_settings(admin_client)

    disabled = admin_client.patch("/api/settings", json={"values": {"admin_access_required": False}})
    assert disabled.status_code == 200
    disabled_items = {item["key"]: item for item in disabled.json()["items"]}
    assert disabled_items["admin_access_required"]["value"] is False
    assert get_runtime_settings().admin_access_required is False

    public_client = TestClient(app)
    public_products = public_client.get("/api/products")
    assert public_products.status_code == 200
    assert public_products.json()["items"] == []

    session_state = public_client.get("/api/auth/session")
    assert session_state.status_code == 200
    assert session_state.json() == {"authenticated": True, "access_required": False}

    disabled_login = public_client.post("/api/auth/session", json={"admin_key": ""})
    assert disabled_login.status_code == 200

    locked_settings = public_client.get("/api/settings")
    assert locked_settings.status_code == 403
    assert locked_settings.json()["detail"] == "请先解锁系统配置"

    _unlock_settings(public_client)
    unlocked_settings = public_client.get("/api/settings")
    assert unlocked_settings.status_code == 200

    disabled_login_after_unlock = public_client.post("/api/auth/session", json={"admin_key": ""})
    assert disabled_login_after_unlock.status_code == 200
    still_unlocked = public_client.get("/api/settings/lock-state")
    assert still_unlocked.status_code == 200
    assert still_unlocked.json() == {"unlocked": True, "configured": True}

    re_enabled = public_client.patch("/api/settings", json={"values": {"admin_access_required": True}})
    assert re_enabled.status_code == 200
    assert get_runtime_settings().admin_access_required is True
    assert public_client.get("/api/products").status_code == 200

    new_client = TestClient(app)
    private_products = new_client.get("/api/products")
    assert private_products.status_code == 401

    required_session = new_client.get("/api/auth/session")
    assert required_session.status_code == 200
    assert required_session.json() == {"authenticated": False, "access_required": True}


def test_settings_api_requires_secondary_unlock(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    locked_state = client.get("/api/settings/lock-state")
    assert locked_state.status_code == 200
    assert locked_state.json() == {"unlocked": False, "configured": True}

    locked_config = client.get("/api/settings")
    assert locked_config.status_code == 403
    assert locked_config.json()["detail"] == "请先解锁系统配置"

    wrong = client.post("/api/settings/unlock", json={"token": "wrong-token"})
    assert wrong.status_code == 401
    assert wrong.json()["detail"] == "设置解锁令牌不正确"

    _unlock_settings(client)
    unlocked_state = client.get("/api/settings/lock-state")
    assert unlocked_state.status_code == 200
    assert unlocked_state.json() == {"unlocked": True, "configured": True}

    config = client.get("/api/settings")
    assert config.status_code == 200
    payload = config.json()
    assert "super-secret-settings-token" not in str(payload)
    assert "super-secret-admin-key" not in str(payload)

    _login(client)
    relogin_state = client.get("/api/settings/lock-state")
    assert relogin_state.status_code == 200
    assert relogin_state.json() == {"unlocked": False, "configured": True}


def test_runtime_config_registry_excludes_env_only_settings(configured_env: Path) -> None:
    assert RUNTIME_CONFIG_KEYS == set(CONFIG_DEFINITION_BY_KEY)
    assert {
        "admin_access_key",
        "settings_access_token",
        "session_secret",
        "database_url",
        "redis_url",
    }.isdisjoint(RUNTIME_CONFIG_KEYS)


def test_responses_background_default_is_disabled(configured_env: Path) -> None:
    assert get_settings().image_responses_background_enabled is False


def test_runtime_config_ignores_database_rows_for_env_only_settings(configured_env: Path) -> None:
    session = get_session_factory()()
    try:
        session.add(AppSetting(key="admin_access_key", value="database-admin-key"))
        session.add(AppSetting(key="settings_access_token", value="database-settings-token"))
        session.add(AppSetting(key="session_secret", value="database-session-secret-123"))
        session.add(AppSetting(key="database_url", value="sqlite:///database-override.db"))
        session.add(AppSetting(key="redis_url", value="redis://database-override:6379/0"))
        session.commit()
    finally:
        session.close()

    settings = get_runtime_settings()
    assert settings.admin_access_key == "super-secret-admin-key"
    assert settings.settings_access_token == "super-secret-settings-token"
    assert settings.session_secret == "super-secret-session-key-123"
    assert settings.database_url != "sqlite:///database-override.db"
    assert settings.redis_url == "redis://localhost:6379/9"


def test_settings_unlock_does_not_bypass_missing_token_after_env_change(
    configured_env: Path,
    monkeypatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    monkeypatch.setenv("SETTINGS_ACCESS_TOKEN", "")
    get_settings.cache_clear()

    lock_state = client.get("/api/settings/lock-state")
    assert lock_state.status_code == 200
    assert lock_state.json() == {"unlocked": False, "configured": False}

    config = client.get("/api/settings")
    assert config.status_code == 503
    assert config.json()["detail"] == "设置解锁令牌未配置，请联系管理员"


def test_settings_api_persists_database_overrides(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    initial = client.get("/api/settings")
    assert initial.status_code == 200
    initial_items = {item["key"]: item for item in initial.json()["items"]}
    assert {
        "text_provider_kind",
        "text_api_key",
        "text_base_url",
        "text_brief_model",
        "text_copy_model",
        "image_provider_kind",
        "image_api_key",
        "image_base_url",
        "image_generate_model",
        "image_images_quality",
        "image_images_style",
        "image_responses_background_enabled",
    }.isdisjoint(initial_items)
    assert initial_items["generation_max_concurrent_tasks"]["value"] == 3
    assert initial_items["image_session_stale_running_after_minutes"]["value"] == 90
    assert initial_items["image_session_stale_running_after_minutes"]["category"] == "生成队列"
    assert initial_items["image_session_stale_running_after_minutes"]["minimum"] == 1
    assert initial_items["image_session_stale_running_after_minutes"]["maximum"] == 24 * 60
    assert "progress heartbeat" in initial_items["image_session_stale_running_after_minutes"]["description"]
    assert initial_items["workflow_image_generation_provider_timeout_seconds"]["value"] == 15 * 60
    assert initial_items["workflow_image_generation_provider_timeout_seconds"]["category"] == "生成队列"
    assert initial_items["workflow_image_generation_provider_timeout_seconds"]["minimum"] == 1
    assert initial_items["workflow_image_generation_provider_timeout_seconds"]["maximum"] == 24 * 60 * 60
    assert initial_items["admin_access_required"]["value"] is True
    assert initial_items["admin_access_required"]["category"] == "安全与运维"
    assert initial_items["deletion_enabled"]["value"] is False
    assert initial_items["deletion_enabled"]["category"] == "安全与运维"

    updated = client.patch(
        "/api/settings",
        json={
            "values": {
                "generation_max_concurrent_tasks": 2,
                "image_session_stale_running_after_minutes": 75,
                "workflow_image_generation_provider_timeout_seconds": 120,
                "deletion_enabled": True,
            }
        },
    )
    assert updated.status_code == 200
    assert get_runtime_settings().generation_max_concurrent_tasks == 2
    assert get_runtime_settings().image_session_stale_running_after_minutes == 75
    assert get_runtime_settings().workflow_image_generation_provider_timeout_seconds == 120
    assert get_runtime_settings().deletion_enabled is True

    session = get_session_factory()()
    try:
        assert session.get(AppSetting, "image_session_stale_running_after_minutes").value == "75"
        assert session.get(AppSetting, "workflow_image_generation_provider_timeout_seconds").value == "120"
    finally:
        session.close()

    invalid_timeout = client.patch(
        "/api/settings",
        json={"values": {"image_session_stale_running_after_minutes": 0}},
    )
    assert invalid_timeout.status_code == 400
    assert "不能小于 1" in invalid_timeout.json()["detail"]

    invalid_workflow_timeout = client.patch(
        "/api/settings",
        json={"values": {"workflow_image_generation_provider_timeout_seconds": 0}},
    )
    assert invalid_workflow_timeout.status_code == 400
    assert "不能小于 1" in invalid_workflow_timeout.json()["detail"]

    legacy_provider_update = client.patch("/api/settings", json={"values": {"image_provider_kind": "openai_images"}})
    assert legacy_provider_update.status_code == 400
    assert "未知配置项: image_provider_kind" in legacy_provider_update.json()["detail"]


def test_settings_export_includes_migratable_runtime_config_provider_secrets_and_excludes_env_only(
    configured_env: Path,
) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    updated = client.patch(
        "/api/settings",
        json={"values": {"generation_max_concurrent_tasks": 2, "deletion_enabled": True}},
    )
    assert updated.status_code == 200

    created_profile = client.post(
        "/api/settings/provider-profiles",
        json={
            "name": "导出网关",
            "base_url": "https://export.example/v1",
            "api_key": "export-secret-key",
            "capabilities": ["text_responses", "image_images"],
            "default_models": {"brief_model": "brief-export", "copy_model": "copy-export"},
            "config": {"note": "exportable"},
            "enabled": True,
        },
    )
    assert created_profile.status_code == 200
    profile_id = created_profile.json()["id"]

    text_binding = client.patch(
        "/api/settings/provider-bindings/text",
        json={
            "provider_kind": "openai",
            "provider_profile_id": profile_id,
            "model_settings": {"brief_model": "brief-export", "copy_model": "copy-export"},
            "config": {},
        },
    )
    assert text_binding.status_code == 200

    exported = client.get("/api/settings/export")

    assert exported.status_code == 200
    payload = exported.json()
    assert payload["metadata"]["schema_version"] == 1
    assert payload["metadata"]["app"] == "ProductFlow"
    assert payload["metadata"]["app_version"]
    assert payload["runtime_config"]["generation_max_concurrent_tasks"] == 2
    assert payload["runtime_config"]["deletion_enabled"] is True
    assert set(RUNTIME_CONFIG_KEYS).issubset(payload["runtime_config"])
    assert {
        "admin_access_key",
        "settings_access_token",
        "session_secret",
        "database_url",
        "redis_url",
        "backend_cors_origins",
        "app_port",
        "storage_root",
    }.isdisjoint(payload["runtime_config"])
    assert "super-secret-settings-token" not in str(payload)
    assert "super-secret-admin-key" not in str(payload)
    assert "super-secret-session-key-123" not in str(payload)
    assert "sqlite:///" not in str(payload)
    assert "redis://localhost:6379/9" not in str(payload)

    exported_profile = next(profile for profile in payload["provider_profiles"] if profile["id"] == profile_id)
    assert exported_profile["api_key"] == "export-secret-key"
    assert exported_profile["base_url"] == "https://export.example/v1"
    assert exported_profile["capabilities"] == ["text_responses", "image_images"]
    exported_bindings = {binding["purpose"]: binding for binding in payload["provider_bindings"]}
    assert exported_bindings["text"]["provider_kind"] == "openai"
    assert exported_bindings["text"]["provider_profile_id"] == profile_id


def test_settings_import_preview_and_commit_replaces_runtime_and_provider_config(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    exported = client.get("/api/settings/export")
    assert exported.status_code == 200
    document = exported.json()
    imported_profile_id = "11111111-1111-4111-8111-111111111111"
    document["runtime_config"]["generation_max_concurrent_tasks"] = 4
    document["runtime_config"]["deletion_enabled"] = True
    document["provider_profiles"] = [
        {
            "id": imported_profile_id,
            "name": "导入网关",
            "provider_type": "openai_compatible",
            "base_url": "https://import.example/v1",
            "api_key": "import-secret-key",
            "capabilities": ["text_responses", "image_responses"],
            "default_models": {
                "brief_model": "brief-import",
                "copy_model": "copy-import",
                "image_model": "image-import",
            },
            "config": {"region": "local"},
            "enabled": True,
        }
    ]
    document["provider_bindings"] = [
        {
            "purpose": "text",
            "provider_kind": "openai",
            "provider_profile_id": imported_profile_id,
            "model_settings": {"brief_model": "brief-import", "copy_model": "copy-import"},
            "config": {},
        },
        {
            "purpose": "image",
            "provider_kind": "openai_responses",
            "provider_profile_id": imported_profile_id,
            "model_settings": {"model": "image-import"},
            "config": {"responses_background_enabled": True, "images_quality": "high"},
        },
    ]

    preview = client.post("/api/settings/import/preview", json=document)
    assert preview.status_code == 200
    assert preview.json() == {
        "schema_version": 1,
        "runtime_config_count": len(RUNTIME_CONFIG_KEYS),
        "provider_profile_count": 1,
        "provider_binding_count": 2,
        "provider_profile_names": ["导入网关"],
        "provider_binding_purposes": ["image", "text"],
        "includes_api_keys": True,
        "provider_profiles_with_api_key_count": 1,
    }

    imported = client.post("/api/settings/import", json=document)
    assert imported.status_code == 200
    response_payload = imported.json()
    imported_items = {item["key"]: item for item in response_payload["config"]["items"]}
    assert imported_items["generation_max_concurrent_tasks"]["value"] == 4
    assert imported_items["deletion_enabled"]["value"] is True
    assert imported_items["poster_generation_mode"]["value"] == "generated"
    assert imported_items["poster_generation_mode"]["source"] == "database"
    assert "import-secret-key" not in str(response_payload)

    session = get_session_factory()()
    try:
        assert session.get(AppSetting, "generation_max_concurrent_tasks").value == "4"
        assert session.get(AppSetting, "deletion_enabled").value == "true"
        assert session.get(AppSetting, "poster_generation_mode").value == "generated"
        profiles = session.scalars(select(ProviderProfile)).all()
        assert [profile.id for profile in profiles] == [imported_profile_id]
        assert profiles[0].api_key == "import-secret-key"
        bindings = {binding.purpose: binding for binding in session.scalars(select(ProviderBinding)).all()}
        assert bindings["text"].provider_profile_id == imported_profile_id
        assert bindings["image"].provider_kind == "openai_responses"
        assert bindings["image"].config_json == {"responses_background_enabled": True}
    finally:
        session.close()


def test_settings_import_rejects_unknown_version_and_rolls_back_invalid_bindings(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    exported = client.get("/api/settings/export")
    assert exported.status_code == 200
    document = exported.json()

    unknown_version = {**document, "metadata": {**document["metadata"], "schema_version": 99}}
    rejected_version = client.post("/api/settings/import/preview", json=unknown_version)
    assert rejected_version.status_code == 400
    assert rejected_version.json()["detail"] == "配置文件版本不支持"

    invalid_binding = dict(document)
    invalid_binding["runtime_config"] = {**document["runtime_config"], "generation_max_concurrent_tasks": 5}
    invalid_binding["provider_profiles"] = []
    invalid_binding["provider_bindings"] = [
        {
            "purpose": "text",
            "provider_kind": "openai",
            "provider_profile_id": "missing-profile",
            "model_settings": {"brief_model": "brief", "copy_model": "copy"},
            "config": {},
        },
        {
            "purpose": "image",
            "provider_kind": "mock",
            "provider_profile_id": None,
            "model_settings": {"model": "mock-image"},
            "config": {},
        },
    ]
    rejected_import = client.post("/api/settings/import", json=invalid_binding)
    assert rejected_import.status_code == 400
    assert "供应商不存在" in rejected_import.json()["detail"]

    assert get_runtime_settings().generation_max_concurrent_tasks == 3
    session = get_session_factory()()
    try:
        assert session.get(AppSetting, "generation_max_concurrent_tasks") is None
        bindings = {binding.purpose: binding for binding in session.scalars(select(ProviderBinding)).all()}
        assert {purpose: binding.provider_kind for purpose, binding in bindings.items()} == {
            "image": "mock",
            "text": "mock",
        }
    finally:
        session.close()


def test_provider_bootstrap_runs_on_app_startup(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    session = get_session_factory()()
    try:
        session.add_all(
            [
                AppSetting(key="text_provider_kind", value="openai"),
                AppSetting(key="text_api_key", value="shared-key"),
                AppSetting(key="text_base_url", value="http://localhost:3000/v1"),
                AppSetting(key="image_provider_kind", value="openai_responses"),
                AppSetting(key="image_api_key", value="shared-key"),
                AppSetting(key="image_base_url", value="http://localhost:3000/v1"),
            ]
        )
        session.commit()
    finally:
        session.close()

    app = create_app()
    with TestClient(app):
        session = get_session_factory()()
        try:
            profiles = session.scalars(select(ProviderProfile)).all()
            bindings = session.scalars(select(ProviderBinding)).all()
        finally:
            session.close()

    assert len(profiles) == 1
    assert set(profiles[0].capabilities_json) == {"text_responses", "image_responses"}
    assert {binding.purpose for binding in bindings} == {"text", "image"}


def test_provider_bootstrap_merges_matching_legacy_text_and_image_config(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    session = get_session_factory()()
    try:
        session.add_all(
            [
                AppSetting(key="text_provider_kind", value="openai"),
                AppSetting(key="text_api_key", value="shared-key"),
                AppSetting(key="text_base_url", value="http://localhost:3000/v1"),
                AppSetting(key="text_brief_model", value="brief-model"),
                AppSetting(key="text_copy_model", value="copy-model"),
                AppSetting(key="image_provider_kind", value="openai_images"),
                AppSetting(key="image_api_key", value="shared-key"),
                AppSetting(key="image_base_url", value="http://localhost:3000/v1"),
                AppSetting(key="image_generate_model", value="gpt-image-2"),
                AppSetting(key="image_images_quality", value="high"),
                AppSetting(key="image_images_style", value="natural"),
                AppSetting(key="image_responses_background_enabled", value="false"),
            ]
        )
        session.commit()
    finally:
        session.close()

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    response = client.get("/api/settings/provider-config")
    assert response.status_code == 200
    payload = response.json()
    assert len(payload["profiles"]) == 1
    profile = payload["profiles"][0]
    assert profile["base_url"] == "http://localhost:3000/v1"
    assert profile["has_api_key"] is True
    assert "shared-key" not in str(payload)
    assert set(profile["capabilities"]) == {"text_responses", "image_images"}
    bindings = {binding["purpose"]: binding for binding in payload["bindings"]}
    assert bindings["text"]["provider_kind"] == "openai"
    assert bindings["text"]["provider_profile_id"] == profile["id"]
    assert bindings["text"]["model_settings"] == {"brief_model": "brief-model", "copy_model": "copy-model"}
    assert bindings["image"]["provider_kind"] == "openai_images"
    assert bindings["image"]["provider_profile_id"] == profile["id"]
    assert bindings["image"]["model_settings"] == {"model": "gpt-image-2"}
    assert bindings["image"]["config"] == {
        "images_quality": "high",
        "images_style": "natural",
    }

    assert client.get("/api/settings/provider-config").json()["profiles"] == payload["profiles"]

    text_config = resolve_text_provider_config()
    assert text_config.provider_kind == "openai"
    assert text_config.api_key == "shared-key"
    assert text_config.base_url == "http://localhost:3000/v1"
    assert text_config.brief_model == "brief-model"
    assert text_config.copy_model == "copy-model"

    image_config = resolve_image_provider_config()
    assert image_config.provider_kind == "openai_images"
    assert image_config.api_key == "shared-key"
    assert image_config.base_url == "http://localhost:3000/v1"
    assert image_config.model == "gpt-image-2"
    assert image_config.images_quality == "high"
    assert image_config.images_style == "natural"
    assert image_config.responses_background_enabled is False


def test_provider_bootstrap_splits_different_legacy_connections(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    session = get_session_factory()()
    try:
        session.add_all(
            [
                AppSetting(key="text_provider_kind", value="openai"),
                AppSetting(key="text_api_key", value="text-key"),
                AppSetting(key="text_base_url", value="https://text.example/v1"),
                AppSetting(key="image_provider_kind", value="openai_responses"),
                AppSetting(key="image_api_key", value="image-key"),
                AppSetting(key="image_base_url", value="https://image.example/v1"),
            ]
        )
        session.commit()
    finally:
        session.close()

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    response = client.get("/api/settings/provider-config")
    assert response.status_code == 200
    payload = response.json()
    assert len(payload["profiles"]) == 2
    profiles_by_base_url = {profile["base_url"]: profile for profile in payload["profiles"]}
    assert set(profiles_by_base_url["https://text.example/v1"]["capabilities"]) == {"text_responses"}
    assert set(profiles_by_base_url["https://image.example/v1"]["capabilities"]) == {"image_responses"}
    bindings = {binding["purpose"]: binding for binding in payload["bindings"]}
    assert bindings["text"]["provider_profile_id"] == profiles_by_base_url["https://text.example/v1"]["id"]
    assert bindings["image"]["provider_profile_id"] == profiles_by_base_url["https://image.example/v1"]["id"]


def test_provider_config_api_masks_keys_preserves_blank_update_and_validates_bindings(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    initial = client.get("/api/settings/provider-config")
    assert initial.status_code == 200
    initial_payload = initial.json()
    assert initial_payload["profiles"] == []
    assert {binding["purpose"]: binding["provider_kind"] for binding in initial_payload["bindings"]} == {
        "image": "mock",
        "text": "mock",
    }

    created = client.post(
        "/api/settings/provider-profiles",
        json={
            "name": "本地 3000 网关",
            "base_url": "http://localhost:3000/v1",
            "api_key": "chatgpt2api",
            "capabilities": ["text_responses", "image_responses", "image_images"],
            "default_models": {"brief_model": "gpt-4.1", "copy_model": "gpt-4.1", "image_model": "gpt-image-2"},
            "config": {},
            "enabled": True,
        },
    )
    assert created.status_code == 200
    profile = created.json()
    profile_id = profile["id"]
    assert profile["has_api_key"] is True
    assert "chatgpt2api" not in str(profile)

    updated_blank_key = client.patch(
        f"/api/settings/provider-profiles/{profile_id}",
        json={
            "name": "本地 3000 网关",
            "base_url": None,
            "api_key": "",
            "capabilities": ["text_responses", "image_images"],
            "default_models": {"brief_model": "gpt-4.1-mini", "copy_model": "gpt-4.1-mini"},
            "config": {},
            "enabled": True,
        },
    )
    assert updated_blank_key.status_code == 200
    assert updated_blank_key.json()["base_url"] is None

    session = get_session_factory()()
    try:
        db_profile = session.get(ProviderProfile, profile_id)
        assert db_profile is not None
        assert db_profile.api_key == "chatgpt2api"
        assert db_profile.base_url is None
    finally:
        session.close()

    text_binding = client.patch(
        "/api/settings/provider-bindings/text",
        json={
            "provider_kind": "openai",
            "provider_profile_id": profile_id,
            "model_settings": {
                "brief_model": "brief-model",
                "copy_model": "copy-model",
            },
            "config": {},
        },
    )
    assert text_binding.status_code == 200
    assert text_binding.json()["provider_kind"] == "openai"
    assert text_binding.json()["model_settings"] == {
        "brief_model": "brief-model",
        "copy_model": "copy-model",
    }

    image_binding = client.patch(
        "/api/settings/provider-bindings/image",
        json={
            "provider_kind": "openai_images",
            "provider_profile_id": profile_id,
            "model_settings": {"model": "gpt-image-2"},
            "config": {"images_quality": "high", "images_style": "natural", "responses_background_enabled": True},
        },
    )
    assert image_binding.status_code == 200
    assert image_binding.json()["provider_kind"] == "openai_images"
    assert image_binding.json()["config"] == {"images_quality": "high", "images_style": "natural"}

    invalid_binding = client.patch(
        "/api/settings/provider-bindings/image",
        json={
            "provider_kind": "openai_responses",
            "provider_profile_id": profile_id,
            "model_settings": {"model": "gpt-image-2"},
            "config": {"responses_background_enabled": True},
        },
    )
    assert invalid_binding.status_code == 400
    assert "不支持当前接口能力" in invalid_binding.json()["detail"]

    missing_text_model = client.patch(
        "/api/settings/provider-bindings/text",
        json={"provider_kind": "mock", "provider_profile_id": None, "model_settings": {}, "config": {}},
    )
    assert missing_text_model.status_code == 400
    assert "文案商品理解模型未配置" in missing_text_model.json()["detail"]

    missing_image_model = client.patch(
        "/api/settings/provider-bindings/image",
        json={
            "provider_kind": "mock",
            "provider_profile_id": None,
            "model_settings": {},
            "config": {},
        },
    )
    assert missing_image_model.status_code == 400
    assert "图片模型未配置" in missing_image_model.json()["detail"]

    missing_responses_background = client.patch(
        "/api/settings/provider-bindings/image",
        json={
            "provider_kind": "openai_responses",
            "provider_profile_id": profile_id,
            "model_settings": {"model": "gpt-5.4"},
            "config": {},
        },
    )
    assert missing_responses_background.status_code == 400
    assert "图片 Responses 后台响应模式未配置" in missing_responses_background.json()["detail"]

    remove_active_capability = client.patch(
        f"/api/settings/provider-profiles/{profile_id}",
        json={
            "capabilities": ["text_responses"],
        },
    )
    assert remove_active_capability.status_code == 400
    assert "不能移除当前接口能力" in remove_active_capability.json()["detail"]

    disable_active_profile = client.patch(
        f"/api/settings/provider-profiles/{profile_id}",
        json={"enabled": False},
    )
    assert disable_active_profile.status_code == 400
    assert "不能停用" in disable_active_profile.json()["detail"]

    archive_active = client.delete(f"/api/settings/provider-profiles/{profile_id}")
    assert archive_active.status_code == 400
    assert "仍被文案或图片配置使用" in archive_active.json()["detail"]

    reset_image = client.patch(
        "/api/settings/provider-bindings/image",
        json={
            "provider_kind": "mock",
            "provider_profile_id": profile_id,
            "model_settings": {"model": "mock-image"},
            "config": {"images_quality": "high", "responses_background_enabled": True},
        },
    )
    assert reset_image.status_code == 200
    assert reset_image.json()["provider_profile_id"] is None
    assert reset_image.json()["config"] == {}
    reset_text = client.patch(
        "/api/settings/provider-bindings/text",
        json={
            "provider_kind": "mock",
            "provider_profile_id": None,
            "model_settings": {"brief_model": "mock-brief", "copy_model": "mock-copy"},
            "config": {},
        },
    )
    assert reset_text.status_code == 200
    archived = client.delete(f"/api/settings/provider-profiles/{profile_id}")
    assert archived.status_code == 200
    assert archived.json()["archived_at"] is not None


def test_real_image_binding_switches_visible_poster_mode_to_generated(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    initial_config = client.get("/api/settings")
    assert initial_config.status_code == 200
    initial_items = {item["key"]: item for item in initial_config.json()["items"]}
    assert initial_items["poster_generation_mode"]["value"] == "template"
    assert initial_items["poster_generation_mode"]["source"] == "env_default"

    created = client.post(
        "/api/settings/provider-profiles",
        json={
            "name": "图片网关",
            "base_url": "https://image.example/v1",
            "api_key": "image-secret-key",
            "capabilities": ["image_images"],
            "default_models": {"image_model": "gpt-image-2"},
            "config": {},
            "enabled": True,
        },
    )
    assert created.status_code == 200
    profile_id = created.json()["id"]

    image_binding = client.patch(
        "/api/settings/provider-bindings/image",
        json={
            "provider_kind": "openai_images",
            "provider_profile_id": profile_id,
            "model_settings": {"model": "gpt-image-2"},
            "config": {"images_quality": "high", "images_style": "natural"},
        },
    )
    assert image_binding.status_code == 200
    assert image_binding.json()["provider_kind"] == "openai_images"

    updated_config = client.get("/api/settings")
    assert updated_config.status_code == 200
    updated_items = {item["key"]: item for item in updated_config.json()["items"]}
    assert updated_items["poster_generation_mode"]["value"] == "generated"
    assert updated_items["poster_generation_mode"]["source"] == "database"
    assert get_runtime_settings().poster_generation_mode == "generated"

    session = get_session_factory()()
    try:
        app_setting = session.get(AppSetting, "poster_generation_mode")
        assert app_setting is not None
        assert app_setting.value == "generated"
    finally:
        session.close()


def test_provider_config_supports_google_gemini_profiles_bindings_and_import(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    created = client.post(
        "/api/settings/provider-profiles",
        json={
            "name": "Gemini 图片",
            "provider_type": "google_gemini",
            "base_url": None,
            "api_key": "google-secret-key",
            "capabilities": ["image_google_gemini"],
            "default_models": {"image_model": "gemini-2.5-flash-image"},
            "config": {},
            "enabled": True,
        },
    )
    assert created.status_code == 200
    profile = created.json()
    profile_id = profile["id"]
    assert profile["provider_type"] == "google_gemini"
    assert profile["capabilities"] == ["image_google_gemini"]
    assert profile["base_url"] is None
    assert profile["has_api_key"] is True
    assert "google-secret-key" not in str(profile)

    rejected_base_url = client.post(
        "/api/settings/provider-profiles",
        json={
            "name": "Gemini 自定义地址",
            "provider_type": "google_gemini",
            "base_url": "https://example.invalid",
            "api_key": "google-secret-key",
            "capabilities": ["image_google_gemini"],
            "default_models": {},
            "config": {},
            "enabled": True,
        },
    )
    assert rejected_base_url.status_code == 400
    assert "暂不支持自定义 Base URL" in rejected_base_url.json()["detail"]

    image_binding = client.patch(
        "/api/settings/provider-bindings/image",
        json={
            "provider_kind": "google_gemini_image",
            "provider_profile_id": profile_id,
            "model_settings": {"model": "gemini-2.5-flash-image"},
            "config": {"gemini_api_version": "v1beta", "gemini_output_mime_type": "image/png"},
        },
    )
    assert image_binding.status_code == 200
    assert image_binding.json()["provider_kind"] == "google_gemini_image"
    assert image_binding.json()["config"] == {
        "gemini_api_version": "v1beta",
        "gemini_output_mime_type": "image/png",
    }

    image_config = resolve_image_provider_config()
    assert image_config.provider_kind == "google_gemini_image"
    assert image_config.api_key == "google-secret-key"
    assert image_config.base_url is None
    assert image_config.model == "gemini-2.5-flash-image"
    assert image_config.gemini_api_version == "v1beta"
    assert image_config.gemini_output_mime_type == "image/png"

    exported = client.get("/api/settings/export")
    assert exported.status_code == 200
    document = exported.json()
    exported_profile = next(item for item in document["provider_profiles"] if item["id"] == profile_id)
    assert exported_profile["provider_type"] == "google_gemini"
    assert exported_profile["api_key"] == "google-secret-key"
    exported_image = next(item for item in document["provider_bindings"] if item["purpose"] == "image")
    assert exported_image["provider_kind"] == "google_gemini_image"

    preview = client.post("/api/settings/import/preview", json=document)
    assert preview.status_code == 200
    assert preview.json()["provider_profile_count"] >= 1


def test_resolvers_ignore_legacy_rows_after_provider_bindings_exist(configured_env: Path) -> None:
    session = get_session_factory()()
    try:
        legacy_rows = [
            AppSetting(key="text_provider_kind", value="openai"),
            AppSetting(key="text_api_key", value="legacy-text-key"),
            AppSetting(key="text_base_url", value="https://legacy-text.example/v1"),
            AppSetting(key="image_provider_kind", value="openai_images"),
            AppSetting(key="image_api_key", value="legacy-image-key"),
            AppSetting(key="image_base_url", value="https://legacy-image.example/v1"),
        ]
        profile = ProviderProfile(
            name="新供应商",
            provider_type="openai_compatible",
            base_url="https://new.example/v1",
            api_key="new-key",
            capabilities_json=["text_responses", "image_images"],
            default_models_json={},
            config_json={},
            enabled=True,
        )
        session.add_all([*legacy_rows, profile])
        session.flush()
        session.add_all(
            [
                ProviderBinding(
                    purpose="text",
                    provider_kind="openai",
                    provider_profile_id=profile.id,
                    model_settings_json={"brief_model": "new-brief", "copy_model": "new-copy"},
                    config_json={},
                ),
                ProviderBinding(
                    purpose="image",
                    provider_kind="openai_images",
                    provider_profile_id=profile.id,
                    model_settings_json={"model": "new-image"},
                    config_json={
                        "images_quality": "high",
                        "images_style": "natural",
                        "responses_background_enabled": True,
                    },
                ),
            ]
        )
        session.commit()
    finally:
        session.close()

    text_config = resolve_text_provider_config()
    assert text_config.api_key == "new-key"
    assert text_config.base_url == "https://new.example/v1"
    assert text_config.brief_model == "new-brief"

    image_config = resolve_image_provider_config()
    assert image_config.api_key == "new-key"
    assert image_config.base_url == "https://new.example/v1"
    assert image_config.model == "new-image"
    assert image_config.images_quality == "high"
    assert image_config.images_style == "natural"
    assert image_config.responses_background_enabled is False


def test_resolvers_reject_missing_models_instead_of_using_legacy_defaults(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("TEXT_BRIEF_MODEL", "legacy-env-brief")
    monkeypatch.setenv("TEXT_COPY_MODEL", "legacy-env-copy")
    monkeypatch.setenv("IMAGE_GENERATE_MODEL", "legacy-env-image")
    monkeypatch.setenv("IMAGE_RESPONSES_BACKGROUND_ENABLED", "false")
    get_settings.cache_clear()

    session = get_session_factory()()
    try:
        legacy_rows = [
            AppSetting(key="text_brief_model", value="legacy-db-brief"),
            AppSetting(key="text_copy_model", value="legacy-db-copy"),
            AppSetting(key="image_generate_model", value="legacy-db-image"),
            AppSetting(key="image_responses_background_enabled", value="false"),
        ]
        profile = ProviderProfile(
            name="无模型默认供应商",
            provider_type="openai_compatible",
            base_url="https://new.example/v1",
            api_key="new-key",
            capabilities_json=["text_responses", "image_images"],
            default_models_json={},
            config_json={},
            enabled=True,
        )
        session.add_all([*legacy_rows, profile])
        session.flush()
        session.add_all(
            [
                ProviderBinding(
                    purpose="text",
                    provider_kind="openai",
                    provider_profile_id=profile.id,
                    model_settings_json={},
                    config_json={},
                ),
                ProviderBinding(
                    purpose="image",
                    provider_kind="openai_images",
                    provider_profile_id=profile.id,
                    model_settings_json={},
                    config_json={},
                ),
            ]
        )
        session.commit()
    finally:
        session.close()

    with pytest.raises(RuntimeError) as text_error:
        resolve_text_provider_config()
    assert "文案商品理解模型未配置" in str(text_error.value)

    with pytest.raises(RuntimeError) as image_error:
        resolve_image_provider_config()
    assert "图片模型未配置" in str(image_error.value)


def test_images_api_runtime_options_normalize_and_validate_without_provider_key(configured_env: Path) -> None:
    with pytest.raises(ValueError) as error:
        normalize_config_values({"image_images_quality": "high"})
    assert "未知配置项: image_images_quality" in str(error.value)


def test_settings_api_accepts_and_validates_optional_image_tool_fields(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    initial = client.get("/api/settings")
    assert initial.status_code == 200
    initial_items = {item["key"]: item for item in initial.json()["items"]}
    assert initial_items["image_tool_allowed_fields"]["input_type"] == "multi_select"
    assert initial_items["image_tool_allowed_fields"]["value"] == [
        "model",
        "quality",
        "output_format",
        "output_compression",
        "moderation",
        "action",
        "input_fidelity",
        "partial_images",
    ]
    assert initial_items["image_tool_quality"]["category"] == "图片工具参数"
    assert initial_items["image_tool_quality"]["input_type"] == "select"
    assert initial_items["image_tool_output_compression"]["minimum"] == 0
    assert initial_items["image_tool_output_compression"]["maximum"] == 100
    assert initial_items["image_tool_background"]["input_type"] == "select"
    assert "n" not in {option["value"] for option in initial_items["image_tool_allowed_fields"]["options"]}
    assert "image_tool_n" not in initial_items

    updated = client.patch(
        "/api/settings",
        json={
            "values": {
                "image_tool_allowed_fields": ["model", "quality", "background", "n"],
                "image_tool_model": "gpt-image-2",
                "image_tool_quality": "high",
                "image_tool_output_format": "jpeg",
                "image_tool_output_compression": 82,
                "image_tool_background": "transparent",
                "image_tool_moderation": "low",
                "image_tool_action": "generate",
                "image_tool_input_fidelity": "high",
                "image_tool_partial_images": 2,
            }
        },
    )
    assert updated.status_code == 200
    settings = get_runtime_settings()
    assert settings.image_tool_allowed_fields == "model,quality,background"
    assert settings.image_tool_model == "gpt-image-2"
    assert settings.image_tool_quality == "high"
    assert settings.image_tool_output_format == "jpeg"
    assert settings.image_tool_output_compression == 82
    assert settings.image_tool_background == "transparent"
    assert settings.image_tool_moderation == "low"
    assert settings.image_tool_action == "generate"
    assert settings.image_tool_input_fidelity == "high"
    assert settings.image_tool_partial_images == 2

    invalid_number = client.patch("/api/settings", json={"values": {"image_tool_output_compression": 101}})
    assert invalid_number.status_code == 400
    assert "不能大于 100" in invalid_number.json()["detail"]

    invalid_provider_n = client.patch("/api/settings", json={"values": {"image_tool_n": 3}})
    assert invalid_provider_n.status_code == 400
    assert "未知配置项" in invalid_provider_n.json()["detail"]

    invalid_select = client.patch("/api/settings", json={"values": {"image_tool_quality": "ultra"}})
    assert invalid_select.status_code == 400
    assert "必须是以下之一" in invalid_select.json()["detail"]

    invalid_field = client.patch("/api/settings", json={"values": {"image_tool_allowed_fields": ["quality", "bogus"]}})
    assert invalid_field.status_code == 400
    assert "可用 Tool 字段包含不支持的字段" in invalid_field.json()["detail"]

    runtime = client.get("/api/settings/runtime")
    assert runtime.status_code == 200
    assert runtime.json()["image_tool_allowed_fields"] == ["model", "quality", "background"]

    cleared = client.patch("/api/settings", json={"values": {"image_tool_output_compression": ""}})
    assert cleared.status_code == 200
    assert get_runtime_settings().image_tool_output_compression is None

def test_prompt_settings_api_accepts_rejects_and_resets(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    initial = client.get("/api/settings")
    assert initial.status_code == 200
    initial_items = {item["key"]: item for item in initial.json()["items"]}
    assert initial_items["prompt_copy_system"]["category"] == "提示词"
    assert initial_items["prompt_copy_system"]["input_type"] == "textarea"
    assert initial_items["prompt_copy_system"]["secret"] is False
    assert initial_items["prompt_copy_system"]["source"] == "env_default"
    assert "editable ecommerce copy" in initial_items["prompt_copy_system"]["value"]
    assert initial_items["prompt_poster_image_edit_template"]["category"] == "提示词"
    assert initial_items["prompt_poster_image_edit_template"]["input_type"] == "textarea"
    assert "explicitly connected\nupstream context" in initial_items["prompt_poster_image_edit_template"]["value"]
    assert initial_items["prompt_poster_image_reference_policy"]["category"] == "提示词"
    assert initial_items["prompt_poster_image_reference_policy"]["input_type"] == "textarea"
    assert "actual product/subject in those images as the visual baseline" in initial_items[
        "prompt_poster_image_reference_policy"
    ]["value"]

    updated = client.patch("/api/settings", json={"values": {"prompt_copy_system": "自定义文案系统提示"}})
    assert updated.status_code == 200
    updated_items = {item["key"]: item for item in updated.json()["items"]}
    assert updated_items["prompt_copy_system"]["value"] == "自定义文案系统提示"
    assert updated_items["prompt_copy_system"]["source"] == "database"

    empty = client.patch("/api/settings", json={"values": {"prompt_copy_system": "   "}})
    assert empty.status_code == 400
    assert "不能为空" in empty.json()["detail"]

    reset = client.patch("/api/settings", json={"reset_keys": ["prompt_copy_system"]})
    assert reset.status_code == 200
    reset_items = {item["key"]: item for item in reset.json()["items"]}
    assert reset_items["prompt_copy_system"]["source"] == "env_default"
    assert "editable ecommerce copy" in reset_items["prompt_copy_system"]["value"]

def test_settings_api_rejects_invalid_effective_config(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    response = client.patch(
        "/api/settings",
        json={"values": {"image_main_image_size": "0x1024"}},
    )

    assert response.status_code == 400
    assert "主图尺寸" in response.json()["detail"]

def test_settings_api_rejects_malformed_image_sizes_before_persist(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    response = client.patch(
        "/api/settings",
        json={
            "values": {
                "image_main_image_size": "foo",
                "image_promo_poster_size": "foo",
            }
        },
    )

    assert response.status_code == 400
    assert "宽x高" in response.json()["detail"]

    session = get_session_factory()()
    try:
        assert session.get(AppSetting, "image_main_image_size") is None
        assert session.get(AppSetting, "image_promo_poster_size") is None
    finally:
        session.close()

def test_settings_api_normalizes_custom_image_sizes_for_generation(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    updated = client.patch(
        "/api/settings",
        json={
            "values": {
                "image_main_image_size": "512X512",
                "image_promo_poster_size": "1024x768",
            }
        },
    )
    assert updated.status_code == 200
    updated_items = {item["key"]: item for item in updated.json()["items"]}
    assert updated_items["image_main_image_size"]["value"] == "512x512"

    created = client.post("/api/image-sessions", json={"title": "自定义尺寸"})
    assert created.status_code == 201
    session_id = created.json()["id"]

    generated = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "生成一张自定义尺寸商品图", "size": "512x512"},
    )

    assert generated.status_code == 202
    assert generated.json()["rounds"][-1]["size"] == "512x512"


def test_runtime_image_size_env_defaults_are_generation_bounded(configured_env: Path, monkeypatch) -> None:
    monkeypatch.setenv("IMAGE_MAIN_IMAGE_SIZE", "4000x4000")
    monkeypatch.setenv("IMAGE_PROMO_POSTER_SIZE", "5000x2500")
    get_settings.cache_clear()

    settings = get_runtime_settings()

    assert settings.image_main_image_size == "3840x3840"
    assert settings.image_promo_poster_size == "3840x1920"


def test_image_generation_max_dimension_runtime_config_controls_size_bounds(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    runtime = client.get("/api/settings/runtime")
    assert runtime.status_code == 200
    assert runtime.json() == {
        "image_generation_max_dimension": 3840,
        "image_tool_allowed_fields": [
            "model",
            "quality",
            "output_format",
            "output_compression",
            "moderation",
            "action",
            "input_fidelity",
            "partial_images",
        ],
        "admin_access_required": True,
        "deletion_enabled": False,
    }

    updated = client.patch(
        "/api/settings",
        json={"values": {"image_generation_max_dimension": 2048}},
    )
    assert updated.status_code == 200
    items = {item["key"]: item for item in updated.json()["items"]}
    assert items["image_generation_max_dimension"]["value"] == 2048
    assert get_runtime_settings().image_generation_max_dimension == 2048

    created = client.post("/api/image-sessions", json={"title": "运行时尺寸上限"})
    assert created.status_code == 201
    generated = client.post(
        f"/api/image-sessions/{created.json()['id']}/generate",
        json={"prompt": "尺寸应被运行时上限校准", "size": "3840x2160"},
    )
    assert generated.status_code == 202
    assert generated.json()["rounds"][-1]["size"] == "2048x1152"

    rejected = client.patch(
        "/api/settings",
        json={"values": {"image_generation_max_dimension": 256}},
    )
    assert rejected.status_code == 400
    assert "不能小于 512" in rejected.json()["detail"]


def test_legacy_image_chat_route_is_removed(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    response = client.post(
        "/api/image-chat/generate",
        json={"prompt": "做一张白底商品图", "size": "1024x1024"},
    )
    assert response.status_code == 404
