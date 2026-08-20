from __future__ import annotations

from datetime import UTC, datetime
from io import BytesIO

import pytest
from dramatiq.middleware.time_limit import TimeLimitExceeded
from PIL import Image
from sqlalchemy import select

from productflow_backend.application.image_session_dependencies import (
    IMAGE_SESSION_TEXT_OUTPUT_FAILURE_REASON,
    GeneratedChatImage,
    ImageSessionProviderFailure,
)
from productflow_backend.application.image_session_provider_effects import (
    reconcile_image_session_provider_effect,
)
from productflow_backend.application.image_sessions import (
    create_image_session,
    create_image_session_generation_task,
    execute_image_session_generation_task,
)
from productflow_backend.domain.durable_generation_tasks import IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_DETAIL
from productflow_backend.infrastructure.db.models import (
    ImageSessionGenerationTask,
    ImageSessionProviderEffect,
    ImageSessionRound,
)
from productflow_backend.infrastructure.image.chat_service import ImageChatService
from productflow_backend.infrastructure.provider_config import ResolvedImageProviderConfig
from productflow_backend.infrastructure.provider_effects import ProviderEffectQueryResult


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
    effects = db_session.scalars(
        select(ImageSessionProviderEffect).where(
            ImageSessionProviderEffect.generation_task_id == result.task.id,
        )
    ).all()
    assert len(effects) == 1
    assert effects[0].effect_result == "applied"
    assert effects[0].operation_key == f"image-session-task:{result.task.id}:candidates:1-1"


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


def test_image_session_fake_partial_failure_stops_with_unknown_provider_effect(
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

    execute_image_session_generation_task(result.task.id, chat_service_factory=lambda: fake)
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
    assert task.status == "unknown"
    assert task.failure_reason == IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_DETAIL
    assert task.progress_phase == "unknown_provider_effect"
    assert task.attempts == 1
    assert task.completed_candidates == 1
    assert task.result_generation_group_id is not None
    assert sent == []
    assert fake.calls == 2
    assert [round_item.candidate_index for round_item in rounds] == [1]
    effects = db_session.scalars(
        select(ImageSessionProviderEffect)
        .where(ImageSessionProviderEffect.generation_task_id == result.task.id)
        .order_by(ImageSessionProviderEffect.candidate_start_index)
    ).all()
    assert [(effect.candidate_start_index, effect.effect_result) for effect in effects] == [
        (1, "applied"),
        (2, "unknown"),
    ]


def test_image_session_provider_effect_reconciliation_is_read_only_and_keeps_task_unknown(
    configured_env,
    db_session,
) -> None:
    class ReconcilingFakeChatService(FakeChatService):
        provider_kind = "fake"

        def __init__(self) -> None:
            super().__init__([RuntimeError("connection reset by peer")])
            self.reconcile_calls = 0

        def generate(self, **kwargs) -> GeneratedChatImage:
            self.calls += 1
            callback = kwargs.get("progress_callback")
            if callback is not None:
                callback(
                    {
                        "provider_response_id": "resp-image-session-reconcile",
                        "provider_response_status": "in_progress",
                    }
                )
            raise RuntimeError("connection reset by peer")

        def reconcile_generation_effect(
            self,
            *,
            operation_key: str,
            request_hash: str,
            provider_response_id: str | None,
        ) -> ProviderEffectQueryResult:
            self.reconcile_calls += 1
            assert operation_key
            assert len(request_hash) == 64
            assert provider_response_id == "resp-image-session-reconcile"
            return ProviderEffectQueryResult(
                effect_result="failed",
                reconciliation_state="not_applied",
                provider_status="rejected",
                result_json={"provider_status": "rejected"},
                detail="provider 查询确认请求未应用",
            )

    image_session = create_image_session(db_session, title="image provider reconcile")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="确认 provider effect",
        size="1024x1024",
    )
    fake = ReconcilingFakeChatService()
    execute_image_session_generation_task(result.task.id, chat_service_factory=lambda: fake)

    reconciliation = reconcile_image_session_provider_effect(
        db_session,
        image_session_id=image_session.id,
        task_id=result.task.id,
        candidate_start_index=1,
        chat_service_factory=lambda: fake,
    )

    db_session.expire_all()
    task = db_session.get(ImageSessionGenerationTask, result.task.id)
    effect = db_session.scalar(
        select(ImageSessionProviderEffect).where(
            ImageSessionProviderEffect.generation_task_id == result.task.id,
            ImageSessionProviderEffect.candidate_start_index == 1,
        )
    )
    assert task is not None
    assert task.status == "unknown"
    assert effect is not None
    assert reconciliation.effect_result == "failed"
    assert reconciliation.reconciliation_state == "not_applied"
    assert effect.effect_result == "failed"
    assert fake.calls == 1
    assert fake.reconcile_calls == 1


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
