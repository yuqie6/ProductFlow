from __future__ import annotations

from pathlib import Path

import itsdangerous.timed
from fastapi.testclient import TestClient
from helpers import _login, _unlock_settings

from productflow_backend.application.runtime_settings import get_runtime_settings
from productflow_backend.config import CONFIG_DEFINITION_BY_KEY, RUNTIME_CONFIG_KEYS
from productflow_backend.infrastructure.db.models import AppSetting, ProviderBinding
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.provider_config import (
    resolve_agent_provider_config,
    resolve_image_provider_config,
    resolve_prompt_provider_config,
)
from productflow_backend.presentation.session import MonotonicTimestampSigner


def test_auth_session_required(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    assert client.get("/api/v2/products").status_code == 401
    assert client.post("/api/auth/session", json={"admin_key": "wrong-admin-key"}).status_code == 401

    _login(client)
    authorized = client.get("/api/v2/products")
    assert authorized.status_code == 200
    assert authorized.json()["items"] == []


def test_auth_session_survives_small_wall_clock_rollback(
    configured_env: Path,
    monkeypatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    current_timestamp = 1_800_000_000
    monkeypatch.setattr(itsdangerous.timed.time, "time", lambda: current_timestamp)
    client = TestClient(create_app())
    _login(client)

    current_timestamp -= 2
    assert client.get("/api/v2/products").status_code == 200


def test_session_signer_recovers_after_large_clock_rollback(monkeypatch) -> None:
    current_timestamp = 1_800_000_000
    monkeypatch.setattr(itsdangerous.timed.time, "time", lambda: current_timestamp)
    signer = MonotonicTimestampSigner("super-secret-session-key-123")

    future_signed = signer.sign(b"payload")
    current_timestamp -= 60
    recovered_signed = signer.sign(b"payload")

    assert signer.unsign(recovered_signed) == b"payload"
    assert future_signed != recovered_signed


def test_settings_require_secondary_unlock(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)

    assert client.get("/api/settings/lock-state").json() == {"unlocked": False, "configured": True}
    locked = client.get("/api/settings")
    assert locked.status_code == 403
    assert locked.json()["detail"] == "请先解锁系统配置"
    assert client.post("/api/settings/unlock", json={"token": "wrong-token"}).status_code == 401

    _unlock_settings(client)
    config = client.get("/api/settings")
    assert config.status_code == 200
    assert "super-secret-settings-token" not in config.text
    assert "super-secret-admin-key" not in config.text

    _login(client)
    assert client.get("/api/settings/lock-state").json() == {"unlocked": False, "configured": True}


def test_admin_access_can_be_disabled_and_reenabled(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    admin_client = TestClient(app)
    _login(admin_client)
    _unlock_settings(admin_client)
    disabled = admin_client.patch("/api/settings", json={"values": {"admin_access_required": False}})
    assert disabled.status_code == 200
    assert get_runtime_settings().admin_access_required is False

    public_client = TestClient(app)
    assert public_client.get("/api/v2/products").status_code == 200
    assert public_client.get("/api/settings").status_code == 403
    _unlock_settings(public_client)
    reenabled = public_client.patch("/api/settings", json={"values": {"admin_access_required": True}})
    assert reenabled.status_code == 200

    assert TestClient(app).get("/api/v2/products").status_code == 401


def test_runtime_config_excludes_and_ignores_bootstrap_secrets(configured_env: Path) -> None:
    assert RUNTIME_CONFIG_KEYS == set(CONFIG_DEFINITION_BY_KEY)
    env_only = {
        "admin_access_key",
        "settings_access_token",
        "session_secret",
        "database_url",
        "redis_url",
    }
    assert env_only.isdisjoint(RUNTIME_CONFIG_KEYS)

    with get_session_factory()() as session:
        session.add_all(
            [
                AppSetting(key="admin_access_key", value="database-admin-key"),
                AppSetting(key="settings_access_token", value="database-settings-token"),
                AppSetting(key="session_secret", value="database-session-secret-123"),
                AppSetting(key="database_url", value="sqlite:///database-override.db"),
                AppSetting(key="redis_url", value="redis://database-override:6379/0"),
            ]
        )
        session.commit()

    settings = get_runtime_settings()
    assert settings.admin_access_key == "super-secret-admin-key"
    assert settings.settings_access_token == "super-secret-settings-token"
    assert settings.session_secret == "super-secret-session-key-123"
    assert settings.redis_url == "redis://localhost:6379/9"


def test_runtime_settings_update_reset_and_validation(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    _unlock_settings(client)

    updated = client.patch(
        "/api/settings",
        json={
            "values": {
                "image_generation_max_dimension": 2048,
                "image_tool_allowed_fields": ["quality", "background"],
            }
        },
    )
    assert updated.status_code == 200
    items = {item["key"]: item for item in updated.json()["items"]}
    assert items["image_generation_max_dimension"]["value"] == 2048
    assert items["image_generation_max_dimension"]["source"] == "database"
    assert items["image_tool_allowed_fields"]["value"] == ["quality", "background"]

    invalid = client.patch("/api/settings", json={"values": {"prompt_image_chat_template": ""}})
    assert invalid.status_code == 400
    assert "不能为空" in invalid.json()["detail"]

    removed_batch_field = client.patch(
        "/api/settings",
        json={"values": {"image_tool_allowed_fields": ["quality", "n"]}},
    )
    assert removed_batch_field.status_code == 400
    assert "不支持的字段: n" in removed_batch_field.json()["detail"]

    reset = client.patch("/api/settings", json={"reset_keys": ["image_generation_max_dimension"]})
    assert reset.status_code == 200
    reset_items = {item["key"]: item for item in reset.json()["items"]}
    assert reset_items["image_generation_max_dimension"]["source"] == "env_default"


def _configure_openai_profile(client: TestClient) -> str:
    profile = client.post(
        "/api/settings/provider-profiles",
        json={
            "name": "开发 OpenAI",
            "provider_type": "openai_compatible",
            "base_url": "https://api.example.test/v1",
            "api_key": "provider-secret",
            "capabilities": ["text_responses", "image_responses", "image_images"],
            "default_models": {},
            "config": {},
            "enabled": True,
        },
    )
    assert profile.status_code == 200, profile.text
    profile_id = profile.json()["id"]

    updates = {
        "prompt": {
            "provider_kind": "openai",
            "provider_profile_id": profile_id,
            "model_settings": {"model": "gpt-5.4-mini"},
            "config": {},
        },
        "agent": {
            "provider_kind": "openai",
            "provider_profile_id": profile_id,
            "model_settings": {"model": "gpt-5.4"},
            "config": {"reasoning_effort": "high", "text_verbosity": "medium"},
        },
        "image": {
            "provider_kind": "openai_responses",
            "provider_profile_id": profile_id,
            "model_settings": {"model": "gpt-image-2"},
            "config": {"responses_background_enabled": False},
        },
    }
    for purpose, payload in updates.items():
        response = client.patch(f"/api/settings/provider-bindings/{purpose}", json=payload)
        assert response.status_code == 200, response.text
    return profile_id


def test_current_provider_bindings_resolve_prompt_agent_and_image(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    with TestClient(app) as client:
        _login(client)
        _unlock_settings(client)
        initial = client.get("/api/settings/provider-config")
        assert initial.status_code == 200
        assert {item["purpose"] for item in initial.json()["bindings"]} == {"prompt", "agent", "image"}
        assert {item["provider_kind"] for item in initial.json()["bindings"]} == {"mock"}

        profile_id = _configure_openai_profile(client)
        blocked = client.delete(f"/api/settings/provider-profiles/{profile_id}")
        assert blocked.status_code == 400
        assert "仍被提示词、图片或 Agent 配置使用" in blocked.json()["detail"]

    prompt = resolve_prompt_provider_config()
    agent = resolve_agent_provider_config()
    image = resolve_image_provider_config()
    assert (prompt.provider_kind, prompt.model, prompt.api_key) == ("openai", "gpt-5.4-mini", "provider-secret")
    assert (agent.provider_kind, agent.model, agent.reasoning_effort) == ("openai", "gpt-5.4", "high")
    assert (image.provider_kind, image.model, image.responses_background_enabled) == (
        "openai_responses",
        "gpt-image-2",
        False,
    )


def test_masked_local_edit_requires_profile_capability_and_openai_images_binding(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    with TestClient(app) as client:
        _login(client)
        _unlock_settings(client)
        profile = client.post(
            "/api/settings/provider-profiles",
            json={
                "name": "OpenAI Images local edit",
                "provider_type": "openai_compatible",
                "base_url": "https://api.example.test/v1",
                "api_key": "provider-secret",
                "capabilities": ["image_images", "image_responses"],
                "default_models": {},
                "config": {},
                "enabled": True,
            },
        )
        assert profile.status_code == 200, profile.text
        profile_id = profile.json()["id"]

        image_binding = client.patch(
            "/api/settings/provider-bindings/image",
            json={
                "provider_kind": "openai_images",
                "provider_profile_id": profile_id,
                "model_settings": {"model": "gpt-image-1"},
                "config": {"images_quality": "high"},
            },
        )
        assert image_binding.status_code == 200, image_binding.text

        without_capability = resolve_image_provider_config()
        assert without_capability.image_mask_edit_enabled is False
        assert without_capability.masked_local_edit_available is False

        profile_update = client.patch(
            f"/api/settings/provider-profiles/{profile_id}",
            json={"capabilities": ["image_images", "image_responses", "image_mask_edit"]},
        )
        assert profile_update.status_code == 200, profile_update.text

        responses_binding = client.patch(
            "/api/settings/provider-bindings/image",
            json={
                "provider_kind": "openai_responses",
                "provider_profile_id": profile_id,
                "model_settings": {"model": "gpt-image-2"},
                "config": {"responses_background_enabled": False},
            },
        )
        assert responses_binding.status_code == 200, responses_binding.text
        non_images_binding = resolve_image_provider_config()
        assert non_images_binding.image_mask_edit_enabled is False
        assert non_images_binding.masked_local_edit_available is False

        image_binding = client.patch(
            "/api/settings/provider-bindings/image",
            json={
                "provider_kind": "openai_images",
                "provider_profile_id": profile_id,
                "model_settings": {"model": "gpt-image-1"},
                "config": {"images_quality": "high"},
            },
        )
        assert image_binding.status_code == 200, image_binding.text

    with_capability = resolve_image_provider_config()
    assert with_capability.provider_kind == "openai_images"
    assert with_capability.image_mask_edit_enabled is True
    assert with_capability.masked_local_edit_available is True


def test_settings_export_import_uses_only_v3_current_bindings(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    with TestClient(app) as client:
        _login(client)
        _unlock_settings(client)
        _configure_openai_profile(client)
        exported = client.get("/api/settings/export")
        assert exported.status_code == 200
        document = exported.json()
        assert document["metadata"]["schema_version"] == 3
        assert document["metadata"]["compatibility"] == "productflow-settings-v3"
        assert {item["purpose"] for item in document["provider_bindings"]} == {"prompt", "agent", "image"}
        assert document["provider_profiles"][0]["api_key"] == "provider-secret"
        assert "admin_access_key" not in document["runtime_config"]

        preview = client.post("/api/settings/import/preview", json=document)
        assert preview.status_code == 200
        assert preview.json()["provider_binding_purposes"] == ["agent", "image", "prompt"]
        imported = client.post("/api/settings/import", json=document)
        assert imported.status_code == 200
        assert {item["purpose"] for item in imported.json()["provider_config"]["bindings"]} == {
            "prompt",
            "agent",
            "image",
        }

        old_document = {
            **document,
            "metadata": {
                **document["metadata"],
                "schema_version": 2,
                "compatibility": "productflow-settings-v2",
            },
        }
        rejected = client.post("/api/settings/import/preview", json=old_document)
        assert rejected.status_code == 400
        assert rejected.json()["detail"] == "配置文件版本不支持"


def test_google_gemini_image_profile_and_binding_are_supported(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    with TestClient(app) as client:
        _login(client)
        _unlock_settings(client)
        profile = client.post(
            "/api/settings/provider-profiles",
            json={
                "name": "Gemini 图片",
                "provider_type": "google_gemini",
                "api_key": "gemini-secret",
                "capabilities": ["image_google_gemini"],
                "default_models": {"image_model": "gemini-2.5-flash-image"},
            },
        )
        assert profile.status_code == 200, profile.text
        binding = client.patch(
            "/api/settings/provider-bindings/image",
            json={
                "provider_kind": "google_gemini_image",
                "provider_profile_id": profile.json()["id"],
                "model_settings": {"model": "gemini-2.5-flash-image"},
                "config": {"gemini_api_version": "v1beta", "gemini_output_mime_type": "image/png"},
            },
        )
        assert binding.status_code == 200, binding.text

    resolved = resolve_image_provider_config()
    assert resolved.provider_kind == "google_gemini_image"
    assert resolved.model == "gemini-2.5-flash-image"
    assert resolved.gemini_output_mime_type == "image/png"


def test_provider_initialization_never_creates_text_binding(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    with TestClient(create_app()):
        pass
    with get_session_factory()() as session:
        purposes = set(session.scalars(ProviderBinding.__table__.select().with_only_columns(ProviderBinding.purpose)))
    assert purposes == {"prompt", "agent", "image"}
