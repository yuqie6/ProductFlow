from __future__ import annotations

from pathlib import Path

from fastapi.testclient import TestClient
from helpers import (
    _login,
    _unlock_settings,
)

from productflow_backend.config import get_runtime_settings, get_settings
from productflow_backend.infrastructure.db.models import (
    AppSetting,
)
from productflow_backend.infrastructure.db.session import get_session_factory


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
    assert initial_items["image_provider_kind"]["value"] == "mock"
    assert initial_items["image_provider_kind"]["source"] == "env_default"
    assert initial_items["generation_max_concurrent_tasks"]["value"] == 3
    assert initial_items["image_session_stale_running_after_minutes"]["value"] == 90
    assert initial_items["image_session_stale_running_after_minutes"]["category"] == "生成队列"
    assert initial_items["image_session_stale_running_after_minutes"]["minimum"] == 1
    assert initial_items["image_session_stale_running_after_minutes"]["maximum"] == 24 * 60
    assert "progress heartbeat" in initial_items["image_session_stale_running_after_minutes"]["description"]
    assert initial_items["admin_access_required"]["value"] is True
    assert initial_items["admin_access_required"]["category"] == "安全与运维"
    assert initial_items["deletion_enabled"]["value"] is False
    assert initial_items["deletion_enabled"]["category"] == "安全与运维"

    updated = client.patch(
        "/api/settings",
        json={
            "values": {
                "image_provider_kind": "openai_responses",
                "image_api_key": "database-image-key",
                "image_generate_model": "gpt-5.4-mini",
                "generation_max_concurrent_tasks": 2,
                "image_session_stale_running_after_minutes": 75,
                "deletion_enabled": True,
            }
        },
    )
    assert updated.status_code == 200
    updated_items = {item["key"]: item for item in updated.json()["items"]}
    assert updated_items["image_provider_kind"]["value"] == "openai_responses"
    assert updated_items["image_provider_kind"]["source"] == "database"
    assert updated_items["image_api_key"]["value"] == ""
    assert updated_items["image_api_key"]["has_value"] is True
    assert get_runtime_settings().image_provider_kind == "openai_responses"
    assert get_runtime_settings().image_api_key == "database-image-key"
    assert get_runtime_settings().generation_max_concurrent_tasks == 2
    assert get_runtime_settings().image_session_stale_running_after_minutes == 75
    assert get_runtime_settings().deletion_enabled is True

    session = get_session_factory()()
    try:
        assert session.get(AppSetting, "image_provider_kind").value == "openai_responses"
        assert session.get(AppSetting, "image_session_stale_running_after_minutes").value == "75"
    finally:
        session.close()

    invalid_timeout = client.patch(
        "/api/settings",
        json={"values": {"image_session_stale_running_after_minutes": 0}},
    )
    assert invalid_timeout.status_code == 400
    assert "不能小于 1" in invalid_timeout.json()["detail"]

    reset = client.patch("/api/settings", json={"reset_keys": ["image_provider_kind"]})
    assert reset.status_code == 200
    reset_items = {item["key"]: item for item in reset.json()["items"]}
    assert reset_items["image_provider_kind"]["value"] == "mock"
    assert reset_items["image_provider_kind"]["source"] == "env_default"


def test_settings_api_accepts_and_validates_optional_image_tool_fields(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)
    _unlock_settings(client)

    initial = client.get("/api/settings")
    assert initial.status_code == 200
    initial_items = {item["key"]: item for item in initial.json()["items"]}
    assert initial_items["image_responses_background_enabled"]["input_type"] == "boolean"
    assert initial_items["image_responses_background_enabled"]["value"] is False
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
        "n",
    ]
    assert initial_items["image_tool_quality"]["category"] == "图片工具参数"
    assert initial_items["image_tool_quality"]["input_type"] == "select"
    assert initial_items["image_tool_output_compression"]["minimum"] == 0
    assert initial_items["image_tool_output_compression"]["maximum"] == 100
    assert initial_items["image_tool_background"]["input_type"] == "select"

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
                "image_tool_n": 3,
            }
        },
    )
    assert updated.status_code == 200
    settings = get_runtime_settings()
    assert settings.image_tool_allowed_fields == "model,quality,background,n"
    assert settings.image_tool_model == "gpt-image-2"
    assert settings.image_tool_quality == "high"
    assert settings.image_tool_output_format == "jpeg"
    assert settings.image_tool_output_compression == 82
    assert settings.image_tool_background == "transparent"
    assert settings.image_tool_moderation == "low"
    assert settings.image_tool_action == "generate"
    assert settings.image_tool_input_fidelity == "high"
    assert settings.image_tool_partial_images == 2
    assert settings.image_tool_n == 3

    invalid_number = client.patch("/api/settings", json={"values": {"image_tool_output_compression": 101}})
    assert invalid_number.status_code == 400
    assert "不能大于 100" in invalid_number.json()["detail"]

    invalid_select = client.patch("/api/settings", json={"values": {"image_tool_quality": "ultra"}})
    assert invalid_select.status_code == 400
    assert "必须是以下之一" in invalid_select.json()["detail"]

    invalid_field = client.patch("/api/settings", json={"values": {"image_tool_allowed_fields": ["quality", "bogus"]}})
    assert invalid_field.status_code == 400
    assert "可用 Tool 字段包含不支持的字段" in invalid_field.json()["detail"]

    runtime = client.get("/api/settings/runtime")
    assert runtime.status_code == 200
    assert runtime.json()["image_tool_allowed_fields"] == ["model", "quality", "background", "n"]

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
    assert "淘宝电商文案助手" in initial_items["prompt_copy_system"]["value"]
    assert initial_items["prompt_poster_image_edit_template"]["category"] == "提示词"
    assert initial_items["prompt_poster_image_edit_template"]["input_type"] == "textarea"
    assert "显式连接的上游上下文" in initial_items["prompt_poster_image_edit_template"]["value"]
    assert initial_items["prompt_poster_image_reference_policy"]["category"] == "提示词"
    assert initial_items["prompt_poster_image_reference_policy"]["input_type"] == "textarea"
    assert "输入图片中的商品/主体作为视觉基准" in initial_items["prompt_poster_image_reference_policy"]["value"]

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
    assert "淘宝电商文案助手" in reset_items["prompt_copy_system"]["value"]

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
            "n",
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
