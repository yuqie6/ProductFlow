from __future__ import annotations

import pytest

from productflow_backend.application.image_sessions.failures import (
    classify_image_generation_failure,
    safe_image_generation_failure_reason,
)

GENERIC = "图片生成失败，请稍后重试"


class ProviderStatusError(RuntimeError):
    def __init__(self, message: str, *, status_code: int | None = None, code: str | None = None) -> None:
        super().__init__(message)
        self.status_code = status_code
        self.code = code


@pytest.mark.parametrize(
    ("exc", "reason"),
    [
        (
            ProviderStatusError("Rate limit reached for image generations", status_code=429),
            "图片供应商限流或配额不足，请稍后重试或降低并发后再试",
        ),
        (
            ProviderStatusError("Request blocked by content policy", status_code=400),
            "图片供应商拒绝了本次内容或安全策略，请调整提示词或参考图后重试",
        ),
        (
            ConnectionError("connection reset by peer"),
            "图片供应商连接中断，请检查网络或代理后重试",
        ),
        (
            ProviderStatusError("invalid image size 64x64", status_code=400),
            "图片生成失败：invalid image size 64x64",
        ),
        (
            ProviderStatusError("upstream service unavailable", status_code=503),
            "图片供应商服务异常，请稍后重试",
        ),
    ],
)
def test_safe_image_generation_failure_reason_categorizes_common_provider_failures(
    exc: BaseException,
    reason: str,
) -> None:
    assert safe_image_generation_failure_reason(exc, generic_message=GENERIC) == reason


def test_safe_image_generation_failure_reason_uses_exception_chain() -> None:
    cause = ProviderStatusError("Too many requests", status_code=429)
    wrapped = RuntimeError("图片供应商请求失败，请检查供应商配置后重试")
    wrapped.__cause__ = cause

    assert safe_image_generation_failure_reason(wrapped, generic_message=GENERIC) == (
        "图片供应商限流或配额不足，请稍后重试或降低并发后再试"
    )


def test_safe_image_generation_failure_reason_keeps_sensitive_unknown_errors_generic() -> None:
    assert safe_image_generation_failure_reason(
        RuntimeError("provider failed sk-test-token base_url=https://secret.example/v1 prompt=full prompt"),
        generic_message=GENERIC,
    ) == GENERIC


@pytest.mark.parametrize(
    ("exc", "category", "retryable"),
    [
        (ProviderStatusError("Too many requests", status_code=429), "rate_limit", True),
        (ConnectionError("connection reset by peer"), "connection", True),
        (TimeoutError("read timeout"), "timeout", True),
        (ProviderStatusError("upstream service unavailable", status_code=503), "provider_5xx", True),
        (ProviderStatusError("Request blocked by content policy", status_code=400), "content_policy", False),
        (ProviderStatusError("unknown parameter: background", status_code=400), "unsupported_parameters", False),
        (ProviderStatusError("bad request: provider rejected input", status_code=400), "bad_request", False),
    ],
)
def test_classify_image_generation_failure_returns_retry_decision(
    exc: BaseException,
    category: str,
    retryable: bool,
) -> None:
    decision = classify_image_generation_failure(exc, generic_message=GENERIC)

    assert decision.category == category
    assert decision.retryable is retryable
    assert decision.reason


def test_classify_image_generation_failure_uses_wrapped_cause_for_retry_decision() -> None:
    cause = ProviderStatusError("Request blocked by safety policy", status_code=400)
    wrapped = RuntimeError("图片供应商请求失败，请检查供应商配置后重试")
    wrapped.__cause__ = cause

    decision = classify_image_generation_failure(wrapped, generic_message=GENERIC)

    assert decision.category == "content_policy"
    assert decision.retryable is False
    assert decision.retry_hint == "revise_input"


def test_classify_image_generation_failure_keeps_actionable_size_detail_non_retryable() -> None:
    decision = classify_image_generation_failure(
        RuntimeError("image2 不支持 64x64，最小尺寸为 512x512"),
        generic_message=GENERIC,
    )

    assert decision.reason == "图片生成失败：image2 不支持 64x64，最小尺寸为 512x512"
    assert decision.category == "unsupported_parameters"
    assert decision.retryable is False
    assert decision.retry_hint == "check_settings"
