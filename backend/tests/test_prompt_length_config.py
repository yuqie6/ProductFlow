from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _unlock_settings
from sqlalchemy import func, select

from productflow_backend.infrastructure.db.models import ImageSessionGenerationTask
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.presentation.api import create_app


def test_prompt_limit_is_saved_and_enforced_before_enqueue(configured_env: Path, monkeypatch: pytest.MonkeyPatch):
    client = TestClient(create_app())
    _login(client)
    _unlock_settings(client)
    assert client.get("/api/settings/runtime").json()["image_session_prompt_max_length"] == 16_000
    settings = client.get("/api/settings").json()["items"]
    item = next(item for item in settings if item["key"] == "image_session_prompt_max_length")
    assert item["input_type"] == "number"
    assert item["category"] == "提示词"
    changed = client.patch("/api/settings", json={"values": {"image_session_prompt_max_length": 6000}})
    assert changed.status_code == 200
    assert client.get("/api/settings/runtime").json()["image_session_prompt_max_length"] == 6000
    calls = []
    monkeypatch.setattr(
        "productflow_backend.application.image_sessions.enqueue_image_session_generation_task", calls.append
    )
    session_id = client.post("/api/image-sessions", json={"title": "长度回归"}).json()["id"]
    # 超过旧的 4000 字符限制也可提交；测试只记录入队，不执行模型请求。
    accepted = client.post(f"/api/image-sessions/{session_id}/generate", json={"prompt": "像" * 5027})
    assert accepted.status_code == 202
    assert len(calls) == 1
    client.patch("/api/settings", json={"values": {"image_session_prompt_max_length": 4000}})
    rejected = client.post(f"/api/image-sessions/{session_id}/generate", json={"prompt": "像" * 5027})
    assert rejected.status_code == 422
    assert "最多允许 4000 个字符，当前 5027 个字符" in rejected.json()["detail"]
    assert "input" not in rejected.text and "ctx" not in rejected.text
    assert len(calls) == 1
    with get_session_factory()() as session:
        assert session.scalar(select(func.count()).select_from(ImageSessionGenerationTask)) == 1
    reset = client.patch("/api/settings", json={"reset_keys": ["image_session_prompt_max_length"]})
    assert reset.status_code == 200
    assert client.get("/api/settings/runtime").json()["image_session_prompt_max_length"] == 16_000


@pytest.mark.parametrize("limit", [0, 100001, "不合法"])
def test_prompt_limit_rejects_invalid_settings(configured_env: Path, limit):
    client = TestClient(create_app())
    _login(client)
    _unlock_settings(client)
    response = client.patch("/api/settings", json={"values": {"image_session_prompt_max_length": limit}})
    assert response.status_code == 400
    assert client.get("/api/settings/runtime").json()["image_session_prompt_max_length"] == 16_000


def test_validation_error_has_safe_text_and_unicode_character_count(configured_env: Path):
    client = TestClient(create_app())
    _login(client)
    _unlock_settings(client)
    client.patch("/api/settings", json={"values": {"image_session_prompt_max_length": 2}})
    # 两个 Unicode 字符通过长度校验，随后进入资源查找；三个字符在入队前被拒绝。
    accepted = client.post("/api/image-sessions/missing/generate", json={"prompt": " 😀像 "})
    assert accepted.status_code == 404
    rejected = client.post("/api/image-sessions/missing/generate", json={"prompt": "😀像素"})
    assert rejected.status_code == 422
    assert "当前 3 个字符" in rejected.json()["detail"]
    invalid_count = client.post("/api/image-sessions/missing/generate", json={"prompt": "像", "generation_count": 0})
    assert invalid_count.status_code == 422
    assert isinstance(invalid_count.json()["detail"], str)
    assert all(set(error) == {"field", "message"} for error in invalid_count.json()["errors"])
