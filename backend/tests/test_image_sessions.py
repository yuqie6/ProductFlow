from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path

import pytest
from dramatiq.middleware.time_limit import TimeLimitExceeded
from fastapi.testclient import TestClient
from helpers import (
    _enable_deletion,
    _execute_workflow_queue_inline,
    _login,
    _make_demo_image_bytes,
    _make_demo_image_bytes_with_size,
    _read_image_size,
)

from productflow_backend.infrastructure.db.models import (
    ImageSession,
    ImageSessionAsset,
    ImageSessionGenerationTask,
    ImageSessionRound,
)


@pytest.fixture(autouse=True)
def _execute_workflow_queue_inline_fixture(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep API workflow tests deterministic while production delivery goes through Dramatiq."""

    _execute_workflow_queue_inline(monkeypatch)
    from productflow_backend.application.image_sessions import execute_image_session_generation_task

    monkeypatch.setattr(
        "productflow_backend.application.image_sessions.enqueue_image_session_generation_task",
        execute_image_session_generation_task,
    )


def test_image_session_rounds_support_same_conversation(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)

    _login(client)

    created = client.post("/api/image-sessions", json={"title": "护手霜连续生图"})
    assert created.status_code == 201
    session_id = created.json()["id"]

    first = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={
            "prompt": "做一张奶油质感的护手霜广告图，柔光，白底，产品居中",
            "size": "1024x1024",
        },
    )
    assert first.status_code == 202
    first_payload = first.json()
    assert len(first_payload["rounds"]) == 1
    assert first_payload["rounds"][0]["generated_asset"]["download_url"].startswith("/api/image-session-assets/")
    assert first_payload["rounds"][0]["generated_asset"]["preview_url"].endswith("variant=preview")
    assert first_payload["rounds"][0]["generated_asset"]["thumbnail_url"].endswith("variant=thumbnail")
    thumbnail = client.get(first_payload["rounds"][0]["generated_asset"]["thumbnail_url"])
    assert thumbnail.status_code == 200
    assert max(_read_image_size(thumbnail.content)) <= 320

    upload = client.post(
        f"/api/image-sessions/{session_id}/reference-images",
        files={"reference_images": ("sample.png", _make_demo_image_bytes(), "image/png")},
    )
    assert upload.status_code == 200
    upload_payload = upload.json()
    assert any(asset["kind"] == "reference_upload" for asset in upload_payload["assets"])

    second = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={
            "prompt": "保持同样产品和光线，把背景改成浴室台面，增加一点水珠",
            "size": "1024x1024",
        },
    )
    assert second.status_code == 202
    second_payload = second.json()
    assert len(second_payload["rounds"]) == 2
    assert second_payload["rounds"][-1]["provider_name"] == "mock"
    assert second_payload["rounds"][-1]["assistant_message"].startswith("已按本轮选择的图片上下文")
    assert second_payload["rounds"][-1]["previous_response_id"] is None
    assert second_payload["rounds"][-1]["base_asset_id"] is None
    assert second_payload["rounds"][-1]["selected_reference_asset_ids"] == []


def test_image_session_generate_returns_queued_task_without_waiting_for_provider(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    sent: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.application.image_sessions.enqueue_image_session_generation_task",
        lambda task_id: sent.append(task_id),
    )
    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "异步提交"})
    assert created.status_code == 201
    response = client.post(
        f"/api/image-sessions/{created.json()['id']}/generate",
        json={"prompt": "只创建任务，不等待 provider", "size": "1024x1024"},
    )

    assert response.status_code == 202
    payload = response.json()
    assert payload["rounds"] == []
    assert len(payload["generation_tasks"]) == 1
    task = payload["generation_tasks"][0]
    assert task["status"] == "queued"
    assert task["prompt"] == "只创建任务，不等待 provider"
    assert task["completed_candidates"] == 0
    assert task["active_candidate_index"] is None
    assert task["progress_phase"] is None
    assert task["progress_updated_at"] is None
    assert task["provider_response_id"] is None
    assert task["provider_response_status"] is None
    assert task["progress_metadata"] is None
    assert task["queue_active_count"] == 1
    assert task["queue_running_count"] == 0
    assert task["queue_queued_count"] == 1
    assert task["queued_ahead_count"] == 0
    assert task["queue_position"] == 1
    assert sent == [task["id"]]
    db_session.expire_all()
    persisted = db_session.get(ImageSessionGenerationTask, task["id"])
    assert persisted is not None
    assert persisted.status == "queued"


def test_image_session_status_returns_lightweight_task_snapshot(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.image_sessions import execute_image_session_generation_task
    from productflow_backend.presentation.api import create_app

    sent: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.application.image_sessions.enqueue_image_session_generation_task",
        lambda task_id: sent.append(task_id),
    )
    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "轻量状态"})
    assert created.status_code == 201
    session_id = created.json()["id"]
    submitted = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "只轮询状态", "size": "1024x1024"},
    )
    assert submitted.status_code == 202
    task_id = submitted.json()["generation_tasks"][0]["id"]

    queued_status = client.get(f"/api/image-sessions/{session_id}/status")
    assert queued_status.status_code == 200
    queued_payload = queued_status.json()
    assert "assets" not in queued_payload
    assert "rounds" not in queued_payload
    assert queued_payload["rounds_count"] == 0
    assert queued_payload["latest_round_id"] is None
    assert queued_payload["has_active_generation_task"] is True
    assert queued_payload["generation_tasks"][0]["id"] == task_id
    assert queued_payload["generation_tasks"][0]["status"] == "queued"
    assert queued_payload["generation_tasks"][0]["queue_position"] == 1
    assert sent == [task_id]

    execute_image_session_generation_task(task_id)

    completed_status = client.get(f"/api/image-sessions/{session_id}/status")
    assert completed_status.status_code == 200
    completed_payload = completed_status.json()
    assert completed_payload["rounds_count"] == 1
    assert completed_payload["latest_round_id"]
    assert completed_payload["latest_generation_group_id"]
    assert completed_payload["has_active_generation_task"] is False
    assert completed_payload["generation_tasks"][0]["status"] == "succeeded"
    assert completed_payload["generation_tasks"][0]["completed_candidates"] == 1
    assert completed_payload["generation_tasks"][0]["progress_phase"] == "succeeded"
    assert completed_payload["generation_tasks"][0]["progress_updated_at"] is not None
    assert completed_payload["generation_tasks"][0]["result_generation_group_id"] == completed_payload[
        "latest_generation_group_id"
    ]


def test_image_session_generation_accepts_per_request_tool_options_and_exposes_provider_notes(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.infrastructure.image.chat_service import GeneratedChatImage
    from productflow_backend.presentation.api import create_app

    calls: list[dict | None] = []

    def generate_with_note(self, **kwargs) -> GeneratedChatImage:
        calls.append(kwargs.get("tool_options"))
        return GeneratedChatImage(
            bytes_data=_make_demo_image_bytes_with_size(1024, 1024),
            mime_type="image/png",
            model_name="mock-image-chat-v1",
            provider_name="mock",
            prompt_version="test-v1",
            size=kwargs["size"],
            generated_at=datetime.now(UTC),
            provider_request_json={"tool_options": kwargs.get("tool_options")},
            provider_output_json={
                "_productflow": {
                    "notes": [{"kind": "fallback", "message": "供应商不支持部分参数，已按基础参数完成。"}]
                }
            },
        )

    monkeypatch.setattr(
        "productflow_backend.infrastructure.image.chat_service.ImageChatService.generate",
        generate_with_note,
    )
    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "每轮参数"})
    assert created.status_code == 201
    response = client.post(
        f"/api/image-sessions/{created.json()['id']}/generate",
        json={
            "prompt": "每轮覆盖 tool 参数",
            "size": "1024x1024",
            "tool_options": {
                "model": "gpt-image-2",
                "quality": "high",
                "output_format": "webp",
                "output_compression": 72,
                "background": "transparent",
                "moderation": "low",
                "action": "generate",
                "input_fidelity": "high",
                "partial_images": 1,
                "n": 2,
            },
        },
    )

    assert response.status_code == 202
    payload = response.json()
    expected_options = {
        "model": "gpt-image-2",
        "quality": "high",
        "output_format": "webp",
        "output_compression": 72,
        "background": "transparent",
        "moderation": "low",
        "action": "generate",
        "input_fidelity": "high",
        "partial_images": 1,
        "n": 2,
    }
    assert calls == [expected_options]
    assert payload["generation_tasks"][0]["tool_options"] == expected_options
    assert payload["generation_tasks"][0]["provider_notes"] == ["供应商不支持部分参数，已按基础参数完成。"]
    assert payload["rounds"][0]["provider_notes"] == ["供应商不支持部分参数，已按基础参数完成。"]
    assert payload["rounds"][0]["actual_size"] == "1024x1024"

    db_session.expire_all()
    task = db_session.get(ImageSessionGenerationTask, payload["generation_tasks"][0]["id"])
    assert task is not None
    assert task.tool_options == expected_options

    invalid = client.post(
        f"/api/image-sessions/{created.json()['id']}/generate",
        json={
            "prompt": "非法 tool 参数",
            "size": "1024x1024",
            "tool_options": {"output_compression": 101},
        },
    )
    assert invalid.status_code == 422


def test_image_session_generation_exposes_actual_size_when_provider_downscales(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.infrastructure.image.chat_service import GeneratedChatImage
    from productflow_backend.presentation.api import create_app

    def generate_downscaled(self, **kwargs) -> GeneratedChatImage:
        return GeneratedChatImage(
            bytes_data=_make_demo_image_bytes_with_size(1024, 1024),
            mime_type="image/png",
            model_name="mock-image-chat-v1",
            provider_name="mock",
            prompt_version="test-v1",
            size=kwargs["size"],
            generated_at=datetime.now(UTC),
            provider_request_json={"size": kwargs["size"]},
            provider_output_json={},
        )

    monkeypatch.setattr(
        "productflow_backend.infrastructure.image.chat_service.ImageChatService.generate",
        generate_downscaled,
    )
    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "尺寸反馈"})
    assert created.status_code == 201
    response = client.post(
        f"/api/image-sessions/{created.json()['id']}/generate",
        json={"prompt": "请求 2K 但供应商返回 1K", "size": "2048x2048"},
    )

    assert response.status_code == 202
    round_payload = response.json()["rounds"][0]
    assert round_payload["size"] == "2048x2048"
    assert round_payload["actual_size"] == "1024x1024"
    assert round_payload["provider_notes"] == ["供应商实际返回 1024x1024，请求尺寸为 2048x2048。"]


def test_image_session_generate_enqueue_failure_marks_task_failed(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    def fail_enqueue(task_id: str) -> None:
        raise RuntimeError(f"redis down for {task_id}")

    monkeypatch.setattr(
        "productflow_backend.application.image_sessions.enqueue_image_session_generation_task",
        fail_enqueue,
    )
    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "入队失败"})
    assert created.status_code == 201
    response = client.post(
        f"/api/image-sessions/{created.json()['id']}/generate",
        json={"prompt": "入队失败应落库", "size": "1024x1024"},
    )

    assert response.status_code == 503
    assert response.json()["detail"] == "任务队列暂不可用，请稍后重试"
    db_session.expire_all()
    tasks = db_session.query(ImageSessionGenerationTask).all()
    assert len(tasks) == 1
    assert tasks[0].status == "failed"
    assert tasks[0].failure_reason == "任务队列暂不可用，请稍后重试"


def test_image_session_worker_failure_uses_generic_safe_reason(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    def fail_generate(*args, **kwargs) -> None:
        raise RuntimeError("provider raw secret sk-test path=/tmp/provider-traceback")

    monkeypatch.setattr(
        "productflow_backend.infrastructure.image.chat_service.ImageChatService.generate",
        fail_generate,
    )
    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "provider 失败"})
    assert created.status_code == 201
    response = client.post(
        f"/api/image-sessions/{created.json()['id']}/generate",
        json={"prompt": "这次 provider 会失败", "size": "1024x1024"},
    )

    assert response.status_code == 202
    payload = response.json()
    assert payload["generation_tasks"][0]["status"] == "failed"
    assert payload["generation_tasks"][0]["failure_reason"] == "图片生成失败，请稍后重试"
    assert "sk-test" not in payload["generation_tasks"][0]["failure_reason"]
    db_session.expire_all()
    task = db_session.get(ImageSessionGenerationTask, payload["generation_tasks"][0]["id"])
    assert task is not None
    assert task.failure_reason == "图片生成失败，请稍后重试"


def test_image_session_worker_timeout_after_partial_success_persists_completed_candidates(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.image_sessions import (
        create_image_session,
        create_image_session_generation_task,
        execute_image_session_generation_task,
    )
    from productflow_backend.infrastructure.image.chat_service import GeneratedChatImage

    calls = {"count": 0}

    def generate_then_timeout(self, **kwargs):
        calls["count"] += 1
        if calls["count"] == 2:
            raise TimeLimitExceeded()
        return GeneratedChatImage(
            bytes_data=_make_demo_image_bytes_with_size(1024, 1024),
            mime_type="image/png",
            model_name="mock-image-chat-v1",
            provider_name="mock",
            prompt_version="test-v1",
            size=kwargs["size"],
            generated_at=datetime.now(UTC),
            provider_request_json={"size": kwargs["size"], "candidate": calls["count"]},
            provider_output_json={},
        )

    monkeypatch.setattr(
        "productflow_backend.infrastructure.image.chat_service.ImageChatService.generate",
        generate_then_timeout,
    )

    image_session = create_image_session(db_session, product_id=None, title="部分成功超时")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="生成两张，第二张超时",
        size="1024x1024",
        generation_count=2,
    )

    execute_image_session_generation_task(result.task.id)

    db_session.expire_all()
    task = db_session.get(ImageSessionGenerationTask, result.task.id)
    rounds = (
        db_session.query(ImageSessionRound)
        .filter(ImageSessionRound.session_id == image_session.id)
        .order_by(ImageSessionRound.candidate_index)
        .all()
    )

    assert task is not None
    assert task.status == "failed"
    assert task.failure_reason == "已生成 1/2 张候选，但任务超时，剩余候选未完成。"
    assert task.result_generation_group_id is not None
    assert task.completed_candidates == 1
    assert task.active_candidate_index is None
    assert task.progress_phase == "failed"
    assert task.progress_updated_at is not None
    assert task.finished_at is not None
    assert task.is_retryable is False
    assert len(rounds) == 1
    assert rounds[0].candidate_index == 1
    assert rounds[0].candidate_count == 2
    assert rounds[0].generation_group_id == task.result_generation_group_id
    assert Path(configured_env, rounds[0].generated_asset.storage_path).exists()


def test_image_session_worker_marks_task_failed_when_time_limit_raises_outside_candidate_loop(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.image_sessions import (
        create_image_session,
        create_image_session_generation_task,
        execute_image_session_generation_task,
    )

    monkeypatch.setattr(
        "productflow_backend.application.image_sessions._execute_image_session_round_generation",
        lambda *args, **kwargs: (_ for _ in ()).throw(TimeLimitExceeded()),
    )

    image_session = create_image_session(db_session, product_id=None, title="整体超时")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="进入候选循环前超时",
        size="1024x1024",
    )

    execute_image_session_generation_task(result.task.id)

    db_session.expire_all()
    task = db_session.get(ImageSessionGenerationTask, result.task.id)

    assert task is not None
    assert task.status == "failed"
    assert task.failure_reason == "图片生成失败，请稍后重试"
    assert task.finished_at is not None
    assert task.is_retryable is False


def test_image_session_worker_persists_provider_progress_heartbeat(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.image_sessions import (
        create_image_session,
        create_image_session_generation_task,
        execute_image_session_generation_task,
    )
    from productflow_backend.infrastructure.image.chat_service import GeneratedChatImage

    def generate_with_progress(self, **kwargs):
        kwargs["progress_callback"](
            {
                "provider_response_id": "resp_background",
                "provider_response_status": "in_progress",
                "provider_response": {"id": "resp_background", "status": "in_progress"},
            }
        )
        kwargs["progress_callback"](
            {
                "provider_response_id": "resp_background",
                "provider_response_status": "completed",
                "provider_response": {"id": "resp_background", "status": "completed"},
            }
        )
        return GeneratedChatImage(
            bytes_data=_make_demo_image_bytes_with_size(1024, 1024),
            mime_type="image/png",
            model_name="mock-image-chat-v1",
            provider_name="mock",
            prompt_version="test-v1",
            size=kwargs["size"],
            generated_at=datetime.now(UTC),
            provider_response_id="resp_background",
            provider_request_json={"size": kwargs["size"]},
            provider_output_json={"id": "resp_background", "status": "completed"},
        )

    monkeypatch.setattr(
        "productflow_backend.infrastructure.image.chat_service.ImageChatService.generate",
        generate_with_progress,
    )

    image_session = create_image_session(db_session, product_id=None, title="provider progress")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="provider polling 更新 heartbeat",
        size="1024x1024",
    )

    execute_image_session_generation_task(result.task.id)

    db_session.expire_all()
    task = db_session.get(ImageSessionGenerationTask, result.task.id)

    assert task is not None
    assert task.status == "succeeded"
    assert task.completed_candidates == 1
    assert task.provider_response_id == "resp_background"
    assert task.provider_response_status == "completed"
    assert task.progress_updated_at is not None
    assert task.progress_metadata["candidate_index"] == 1
    assert task.progress_metadata["candidate_count"] == 1
    assert task.progress_metadata["generated_asset_id"]
    assert task.progress_metadata["round_id"]


def test_image_session_worker_duplicate_message_noops_terminal_task(
    configured_env: Path,
    db_session,
) -> None:
    from productflow_backend.application.image_sessions import (
        create_image_session,
        create_image_session_generation_task,
        execute_image_session_generation_task,
    )

    image_session = create_image_session(db_session, product_id=None, title="重复消息")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="重复 worker 消息只执行一次",
        size="1024x1024",
    )

    execute_image_session_generation_task(result.task.id)
    execute_image_session_generation_task(result.task.id)

    db_session.expire_all()
    task = db_session.get(ImageSessionGenerationTask, result.task.id)
    rounds = db_session.query(ImageSessionRound).filter(ImageSessionRound.session_id == image_session.id).all()
    assert task is not None
    assert task.status == "succeeded"
    assert len(rounds) == 1


def test_image_session_worker_duplicate_message_noops_running_task(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.image_sessions import (
        create_image_session,
        create_image_session_generation_task,
        execute_image_session_generation_task,
    )
    from productflow_backend.domain.enums import JobStatus

    image_session = create_image_session(db_session, product_id=None, title="running 重复消息")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="running 状态不应重复执行",
        size="1024x1024",
    )
    result.task.status = JobStatus.RUNNING
    db_session.commit()
    calls: list[object] = []
    monkeypatch.setattr(
        "productflow_backend.infrastructure.image.chat_service.ImageChatService.generate",
        lambda *args, **kwargs: calls.append((args, kwargs)),
    )

    execute_image_session_generation_task(result.task.id)

    db_session.expire_all()
    task = db_session.get(ImageSessionGenerationTask, result.task.id)
    rounds = db_session.query(ImageSessionRound).filter(ImageSessionRound.session_id == image_session.id).all()
    assert task is not None
    assert task.status == "running"
    assert rounds == []
    assert calls == []


def test_image_session_branch_uses_selected_base_and_references_only(configured_env: Path, db_session) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "分支测试"})
    assert created.status_code == 201
    session_id = created.json()["id"]

    first = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "第一张基础图", "size": "1024x1024"},
    )
    assert first.status_code == 202
    first_asset_id = first.json()["rounds"][-1]["generated_asset"]["id"]

    later = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "后续但不应被自动继承的图", "size": "1024x1024"},
    )
    assert later.status_code == 202

    upload = client.post(
        f"/api/image-sessions/{session_id}/reference-images",
        files=[
            ("reference_images", ("ref-a.png", _make_demo_image_bytes(), "image/png")),
            ("reference_images", ("ref-b.png", _make_demo_image_bytes(), "image/png")),
        ],
    )
    assert upload.status_code == 200
    reference_ids = [asset["id"] for asset in upload.json()["assets"] if asset["kind"] == "reference_upload"]
    assert len(reference_ids) == 2

    branched = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={
            "prompt": "只从第一张和第二张参考图继续",
            "size": "1024x1024",
            "base_asset_id": first_asset_id,
            "selected_reference_asset_ids": [reference_ids[1]],
            "generation_count": 1,
        },
    )
    assert branched.status_code == 202
    payload = branched.json()
    branch_round = payload["rounds"][-1]
    assert branch_round["base_asset_id"] == first_asset_id
    assert branch_round["selected_reference_asset_ids"] == [reference_ids[1]]
    assert branch_round["previous_response_id"] is None
    assert branch_round["generation_group_id"]
    assert branch_round["candidate_index"] == 1
    assert branch_round["candidate_count"] == 1

    db_session.expire_all()
    persisted = db_session.get(ImageSessionRound, branch_round["id"])
    assert persisted is not None
    assert persisted.base_asset_id == first_asset_id
    assert persisted.selected_reference_asset_ids == [reference_ids[1]]
    assert persisted.provider_request_json == {
        "prompt": "只从第一张和第二张参考图继续",
        "size": "1024x1024",
        "history_count": 0,
        "manual_reference_count": 2,
        "previous_response_id": None,
    }


def test_image_session_branch_validates_asset_scope_and_kind(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "校验"})
    other_created = client.post("/api/image-sessions", json={"title": "其它会话"})
    assert created.status_code == 201
    assert other_created.status_code == 201
    session_id = created.json()["id"]
    other_session_id = other_created.json()["id"]

    generated = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "生成图", "size": "1024x1024"},
    )
    other_generated = client.post(
        f"/api/image-sessions/{other_session_id}/generate",
        json={"prompt": "其它生成图", "size": "1024x1024"},
    )
    assert generated.status_code == 202
    assert other_generated.status_code == 202
    generated_asset_id = generated.json()["rounds"][-1]["generated_asset"]["id"]
    other_generated_asset_id = other_generated.json()["rounds"][-1]["generated_asset"]["id"]

    upload = client.post(
        f"/api/image-sessions/{session_id}/reference-images",
        files={"reference_images": ("ref.png", _make_demo_image_bytes(), "image/png")},
    )
    other_upload = client.post(
        f"/api/image-sessions/{other_session_id}/reference-images",
        files={"reference_images": ("other-ref.png", _make_demo_image_bytes(), "image/png")},
    )
    assert upload.status_code == 200
    assert other_upload.status_code == 200
    reference_asset_id = next(asset["id"] for asset in upload.json()["assets"] if asset["kind"] == "reference_upload")
    other_reference_asset_id = next(
        asset["id"] for asset in other_upload.json()["assets"] if asset["kind"] == "reference_upload"
    )

    base_wrong_session = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "错会话基图", "size": "1024x1024", "base_asset_id": other_generated_asset_id},
    )
    assert base_wrong_session.status_code == 404
    assert base_wrong_session.json()["detail"] == "会话图片不存在"

    base_wrong_kind = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "错类型基图", "size": "1024x1024", "base_asset_id": reference_asset_id},
    )
    assert base_wrong_kind.status_code == 400
    assert base_wrong_kind.json()["detail"] == "只能从会话生成图继续"

    reference_wrong_session = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={
            "prompt": "错会话参考图",
            "size": "1024x1024",
            "selected_reference_asset_ids": [other_reference_asset_id],
        },
    )
    assert reference_wrong_session.status_code == 404
    assert reference_wrong_session.json()["detail"] == "会话参考图不存在"

    reference_wrong_kind = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={
            "prompt": "错类型参考图",
            "size": "1024x1024",
            "selected_reference_asset_ids": [generated_asset_id],
        },
    )
    assert reference_wrong_kind.status_code == 400
    assert reference_wrong_kind.json()["detail"] == "只能选择会话参考图参与本轮生成"

    too_many_upload = client.post(
        f"/api/image-sessions/{session_id}/reference-images",
        files=[
            ("reference_images", (f"ref-{index}.png", _make_demo_image_bytes(), "image/png"))
            for index in range(6)
        ],
    )
    assert too_many_upload.status_code == 200
    reference_ids = [
        asset["id"] for asset in too_many_upload.json()["assets"] if asset["kind"] == "reference_upload"
    ][-6:]
    too_many = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={
            "prompt": "上下文太多",
            "size": "1024x1024",
            "base_asset_id": generated_asset_id,
            "selected_reference_asset_ids": reference_ids,
        },
    )
    assert too_many.status_code == 400
    assert too_many.json()["detail"] == "本轮最多选择 6 张图片上下文（含分支基图）"

    bad_count = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "数量非法", "size": "1024x1024", "generation_count": 5},
    )
    assert bad_count.status_code == 422


def test_image_session_multi_candidate_generation_persists_one_round_per_candidate(
    configured_env: Path,
    db_session,
) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "多候选"})
    assert created.status_code == 201
    session_id = created.json()["id"]

    generated = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "同一提示词出三张候选", "size": "1024x1024", "generation_count": 3},
    )
    assert generated.status_code == 202
    rounds = generated.json()["rounds"]
    assert len(rounds) == 3
    group_ids = {round_item["generation_group_id"] for round_item in rounds}
    assert len(group_ids) == 1
    assert [round_item["candidate_index"] for round_item in rounds] == [1, 2, 3]
    assert all(round_item["candidate_count"] == 3 for round_item in rounds)
    assert len({round_item["generated_asset"]["id"] for round_item in rounds}) == 3

    db_session.expire_all()
    persisted_rounds = (
        db_session.query(ImageSessionRound)
        .filter(ImageSessionRound.session_id == session_id)
        .order_by(ImageSessionRound.candidate_index)
        .all()
    )
    assert len(persisted_rounds) == 3
    assert {round_item.generation_group_id for round_item in persisted_rounds} == group_ids
    assert [round_item.candidate_index for round_item in persisted_rounds] == [1, 2, 3]


def test_image_session_worker_actor_uses_internal_failsafe_time_limit(configured_env: Path) -> None:
    from productflow_backend.workers import (
        IMAGE_SESSION_WORKER_FAILSAFE_TIME_LIMIT_MS,
        get_image_session_worker_failsafe_time_limit_ms,
        run_image_session_generation_task,
    )

    assert get_image_session_worker_failsafe_time_limit_ms() == 24 * 60 * 60 * 1000
    assert IMAGE_SESSION_WORKER_FAILSAFE_TIME_LIMIT_MS == 24 * 60 * 60 * 1000
    assert run_image_session_generation_task.options["time_limit"] == IMAGE_SESSION_WORKER_FAILSAFE_TIME_LIMIT_MS


def test_image_session_generation_accepts_custom_size_and_rejects_invalid_dimensions(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)

    _login(client)

    created = client.post("/api/image-sessions", json={"title": "自定义尺寸"})
    assert created.status_code == 201
    session_id = created.json()["id"]

    generated = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "做一张 16:9 展示图", "size": "1280x720"},
    )
    assert generated.status_code == 202
    assert generated.json()["rounds"][-1]["size"] == "1280x720"

    zero = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "尺寸非法", "size": "0x720"},
    )
    assert zero.status_code == 422
    assert "宽高必须大于 0" in zero.text

    oversized = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "尺寸过大", "size": "5000x5000"},
    )
    assert oversized.status_code == 202
    assert oversized.json()["rounds"][-1]["size"] == "3840x3840"


def test_image_session_reference_image_can_be_deleted(configured_env: Path, db_session) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "参考图删除"})
    assert created.status_code == 201
    session_id = created.json()["id"]
    upload = client.post(
        f"/api/image-sessions/{session_id}/reference-images",
        files={"reference_images": ("sample.png", _make_demo_image_bytes(), "image/png")},
    )
    assert upload.status_code == 200
    reference_asset = next(asset for asset in upload.json()["assets"] if asset["kind"] == "reference_upload")

    db_session.expire_all()
    persisted_asset = db_session.get(ImageSessionAsset, reference_asset["id"])
    assert persisted_asset is not None
    reference_path = Path(configured_env) / persisted_asset.storage_path
    assert reference_path.exists()

    deleted = client.delete(f"/api/image-sessions/{session_id}/reference-images/{reference_asset['id']}")
    assert deleted.status_code == 200
    assert all(asset["id"] != reference_asset["id"] for asset in deleted.json()["assets"])

    db_session.expire_all()
    assert db_session.get(ImageSessionAsset, reference_asset["id"]) is None
    assert not reference_path.exists()

def test_image_session_can_be_deleted_with_files(configured_env: Path, db_session) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "整会话删除"})
    assert created.status_code == 201
    session_id = created.json()["id"]
    upload = client.post(
        f"/api/image-sessions/{session_id}/reference-images",
        files={"reference_images": ("sample.png", _make_demo_image_bytes(), "image/png")},
    )
    assert upload.status_code == 200
    generated = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "做一张白底商品图", "size": "1024x1024"},
    )
    assert generated.status_code == 202

    db_session.expire_all()
    asset_paths = [
        Path(configured_env) / asset.storage_path
        for asset in db_session.query(ImageSessionAsset).filter(ImageSessionAsset.session_id == session_id).all()
    ]
    assert asset_paths
    assert all(path.exists() for path in asset_paths)
    session_root = Path(configured_env) / "image_sessions" / session_id
    assert session_root.exists()

    _enable_deletion(client)
    deleted = client.delete(f"/api/image-sessions/{session_id}")
    assert deleted.status_code == 204

    listed = client.get("/api/image-sessions")
    assert listed.status_code == 200
    assert all(item["id"] != session_id for item in listed.json()["items"])

    db_session.expire_all()
    assert db_session.get(ImageSession, session_id) is None
    assert all(not path.exists() for path in asset_paths)
    assert not session_root.exists()

def test_image_session_result_can_write_back_to_product(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    create_product_response = client.post(
        "/api/products",
        data={"name": "护手霜", "category": "个护", "price": "59.00"},
        files={"image": ("cream.png", _make_demo_image_bytes(), "image/png")},
    )
    assert create_product_response.status_code == 201
    product_id = create_product_response.json()["id"]

    created = client.post("/api/image-sessions", json={"product_id": product_id})
    assert created.status_code == 201
    session_id = created.json()["id"]

    generated = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "做一张高级浴室台面护手霜广告图", "size": "1024x1024"},
    )
    assert generated.status_code == 202
    generated_payload = generated.json()
    generated_asset_id = generated_payload["rounds"][-1]["generated_asset"]["id"]

    attach_reference = client.post(
        f"/api/image-sessions/{session_id}/assets/{generated_asset_id}/attach-to-product",
        json={"target": "reference"},
    )
    assert attach_reference.status_code == 200
    assert attach_reference.json()["message"] == "已加入商品参考图"

    product_after_reference = client.get(f"/api/products/{product_id}")
    assert product_after_reference.status_code == 200
    reference_assets = [
        asset for asset in product_after_reference.json()["source_assets"] if asset["kind"] == "reference_image"
    ]
    assert len(reference_assets) >= 1

    attach_main = client.post(
        f"/api/image-sessions/{session_id}/assets/{generated_asset_id}/attach-to-product",
        json={"target": "main_source"},
    )
    assert attach_main.status_code == 200
    assert attach_main.json()["message"] == "已设为商品主图"

    product_after_main = client.get(f"/api/products/{product_id}")
    assert product_after_main.status_code == 200
    original_assets = [
        asset for asset in product_after_main.json()["source_assets"] if asset["kind"] == "original_image"
    ]
    all_reference_assets = [
        asset for asset in product_after_main.json()["source_assets"] if asset["kind"] == "reference_image"
    ]
    assert len(original_assets) == 1
    assert len(all_reference_assets) >= 2
