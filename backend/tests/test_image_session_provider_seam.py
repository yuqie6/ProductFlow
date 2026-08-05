from __future__ import annotations

from datetime import UTC, datetime
from io import BytesIO

import pytest
from dramatiq.middleware.time_limit import TimeLimitExceeded
from PIL import Image

from productflow_backend.application.image_session_dependencies import (
    IMAGE_SESSION_TEXT_OUTPUT_FAILURE_REASON,
    GeneratedChatImage,
    ImageSessionProviderFailure,
)
from productflow_backend.application.image_sessions import (
    create_image_session,
    create_image_session_generation_task,
    execute_image_session_generation_task,
)
from productflow_backend.infrastructure.db.models import ImageSessionGenerationTask, ImageSessionRound
from productflow_backend.infrastructure.image.chat_service import ImageChatService
from productflow_backend.infrastructure.provider_config import ResolvedImageProviderConfig


def _make_png_bytes() -> bytes:
    buffer = BytesIO()
    Image.new("RGB", (32, 32), (240, 240, 240)).save(buffer, format="PNG")
    return buffer.getvalue()


def _generated_image(*, candidate: int) -> GeneratedChatImage:
    return GeneratedChatImage(
        bytes_data=_make_png_bytes(),
        mime_type="image/png",
        model_name="fake-image-model",
        provider_name="fake",
        prompt_version="fake-v1",
        size="1024x1024",
        generated_at=datetime.now(UTC),
        provider_request_json={"candidate": candidate},
        provider_output_json={"candidate": candidate},
    )


class FakeChatService:
    provider_kind = "fake"

    def __init__(self, outcomes: list[object], *, repeat_last: bool = False) -> None:
        self.outcomes = list(outcomes)
        self.repeat_last = repeat_last
        self.calls = 0
        self._last_outcome: object | None = None

    def generate(self, **kwargs) -> GeneratedChatImage:
        self.calls += 1
        if self.outcomes:
            self._last_outcome = self.outcomes.pop(0)
        elif not self.repeat_last:
            raise AssertionError("fake chat service ran out of outcomes")
        outcome = self._last_outcome
        if isinstance(outcome, BaseException):
            raise outcome
        if not isinstance(outcome, GeneratedChatImage):
            raise AssertionError(f"unexpected fake outcome: {outcome!r}")
        return outcome

    def generate_many(self, **kwargs) -> list[GeneratedChatImage]:
        raise AssertionError("fake seam tests should use the single-candidate path")


def test_image_session_executor_accepts_fake_service_and_persists_result(
    configured_env,
    db_session,
) -> None:
    image_session = create_image_session(db_session, title="fake provider success")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="使用 fake service 生成",
        size="1024x1024",
    )
    fake = FakeChatService([_generated_image(candidate=1)])
    factory_calls = 0

    def factory() -> FakeChatService:
        nonlocal factory_calls
        factory_calls += 1
        return fake

    execute_image_session_generation_task(
        result.task.id,
        chat_service_factory=factory,
    )

    db_session.expire_all()
    task = db_session.get(ImageSessionGenerationTask, result.task.id)
    rounds = (
        db_session.query(ImageSessionRound)
        .filter(ImageSessionRound.session_id == image_session.id)
        .order_by(ImageSessionRound.candidate_index)
        .all()
    )

    assert task is not None
    assert task.status == "succeeded"
    assert task.failure_reason is None
    assert task.completed_candidates == 1
    assert factory_calls == 1
    assert fake.calls == 1
    assert len(rounds) == 1
    assert rounds[0].provider_name == "fake"
    assert rounds[0].provider_request_json == {"candidate": 1}


def test_image_session_typed_provider_failure_reaches_terminal_safe_reason(
    configured_env,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    sent: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.application.image_sessions.enqueue_image_session_generation_task",
        lambda task_id: sent.append(task_id),
    )
    image_session = create_image_session(db_session, title="typed provider failure")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="供应商只返回文字",
        size="1024x1024",
    )
    fake = FakeChatService(
        [ImageSessionProviderFailure(IMAGE_SESSION_TEXT_OUTPUT_FAILURE_REASON)],
        repeat_last=True,
    )

    for _ in range(3):
        execute_image_session_generation_task(
            result.task.id,
            chat_service_factory=lambda: fake,
        )

    db_session.expire_all()
    task = db_session.get(ImageSessionGenerationTask, result.task.id)

    assert task is not None
    assert task.status == "failed"
    assert task.failure_reason == IMAGE_SESSION_TEXT_OUTPUT_FAILURE_REASON
    assert task.is_retryable is True
    assert task.progress_phase == "failed"
    assert sent == [result.task.id, result.task.id]
    assert fake.calls == 3


def test_image_session_fake_rate_limit_keeps_existing_classifier_and_retry_metadata(
    configured_env,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    sent: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.application.image_sessions.enqueue_image_session_generation_task",
        lambda task_id: sent.append(task_id),
    )
    image_session = create_image_session(db_session, title="fake rate limit")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="触发限流分类",
        size="1024x1024",
    )
    fake = FakeChatService([RuntimeError("429 rate limit")])

    execute_image_session_generation_task(
        result.task.id,
        chat_service_factory=lambda: fake,
    )

    db_session.expire_all()
    task = db_session.get(ImageSessionGenerationTask, result.task.id)

    assert task is not None
    assert task.status == "queued"
    assert task.failure_reason is None
    assert task.is_retryable is True
    assert task.progress_metadata == {
        "last_failure_reason": "图片供应商限流或配额不足，请稍后重试或降低并发后再试",
        "last_failure_category": "rate_limit",
        "last_failure_retryable": True,
        "retry_hint": "retry_later",
        "auto_retry_attempt": 1,
        "max_attempts": 3,
    }
    assert sent == [result.task.id]


def test_image_session_fake_partial_failure_retries_remaining_candidate_without_duplicate(
    configured_env,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    sent: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.application.image_sessions.enqueue_image_session_generation_task",
        lambda task_id: sent.append(task_id),
    )
    image_session = create_image_session(db_session, title="fake partial retry")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="生成两张候选",
        size="1024x1024",
        generation_count=2,
    )
    fake = FakeChatService(
        [_generated_image(candidate=1), TimeLimitExceeded(), _generated_image(candidate=2)],
    )

    execute_image_session_generation_task(
        result.task.id,
        chat_service_factory=lambda: fake,
    )
    execute_image_session_generation_task(
        result.task.id,
        chat_service_factory=lambda: fake,
    )

    db_session.expire_all()
    task = db_session.get(ImageSessionGenerationTask, result.task.id)
    rounds = (
        db_session.query(ImageSessionRound)
        .filter(ImageSessionRound.session_id == image_session.id)
        .order_by(ImageSessionRound.candidate_index)
        .all()
    )

    assert task is not None
    assert task.status == "succeeded"
    assert task.failure_reason is None
    assert task.attempts == 2
    assert task.completed_candidates == 2
    assert task.result_generation_group_id is not None
    assert sent == [result.task.id]
    assert fake.calls == 3
    assert [round_item.candidate_index for round_item in rounds] == [1, 2]
    assert {round_item.generation_group_id for round_item in rounds} == {task.result_generation_group_id}


def test_image_chat_service_normalizes_responses_text_only_output(
    configured_env,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.infrastructure.image.responses_provider import PROVIDER_TEXT_OUTPUT_MESSAGE

    def raise_text_output(self, **kwargs):
        raise RuntimeError(PROVIDER_TEXT_OUTPUT_MESSAGE)

    monkeypatch.setattr(
        "productflow_backend.infrastructure.image.responses_provider.OpenAIResponsesImageClient.generate_image",
        raise_text_output,
    )
    service = ImageChatService(
        ResolvedImageProviderConfig(
            provider_kind="openai_responses",
            model="fake-responses-model",
            api_key="fake-api-key",
        )
    )

    with pytest.raises(ImageSessionProviderFailure) as raised:
        service.generate(
            prompt="供应商只返回文字",
            size="1024x1024",
            history=[],
            manual_reference_images=[],
        )

    assert raised.value.safe_reason == IMAGE_SESSION_TEXT_OUTPUT_FAILURE_REASON
    assert isinstance(raised.value.__cause__, RuntimeError)
    assert str(raised.value.__cause__) == PROVIDER_TEXT_OUTPUT_MESSAGE
