from __future__ import annotations

import re
from collections.abc import Iterable
from dataclasses import dataclass
from typing import Literal

ImageGenerationFailureCategory = Literal[
    "rate_limit",
    "quota",
    "content_policy",
    "connection",
    "timeout",
    "provider_5xx",
    "unsupported_parameters",
    "bad_request",
    "unknown",
]
ImageGenerationFailureRetryHint = Literal["retry_later", "revise_input", "check_settings"]


@dataclass(frozen=True, slots=True)
class ImageGenerationFailureDecision:
    reason: str
    retryable: bool
    retry_hint: ImageGenerationFailureRetryHint
    category: ImageGenerationFailureCategory


_SENSITIVE_FAILURE_PATTERNS = (
    re.compile(r"sk-[a-zA-Z0-9_-]+"),
    re.compile(r"\b(api[_ -]?key|token|bearer|authorization|credential|secret)\b", re.IGNORECASE),
    re.compile(r"\b(base_url|prompt)\s*=", re.IGNORECASE),
    re.compile(r"https?://", re.IGNORECASE),
    re.compile(r"(/tmp/|traceback|stack trace)", re.IGNORECASE),
)

_NON_RETRYABLE_FAILURE_RULES: tuple[
    tuple[ImageGenerationFailureCategory, ImageGenerationFailureRetryHint, str, tuple[re.Pattern[str], ...]],
    ...,
] = (
    (
        "content_policy",
        "revise_input",
        "图片供应商拒绝了本次内容或安全策略，请调整提示词或参考图后重试",
        (
            re.compile(r"content policy|safety|moderation|policy violation|blocked|refused", re.IGNORECASE),
            re.compile(r"拒绝|安全策略|内容政策|违规|敏感内容"),
        ),
    ),
    (
        "unsupported_parameters",
        "check_settings",
        "图片供应商参数不支持，请检查尺寸、模型或高级参数后重试",
        (
            re.compile(
                r"unsupported|unknown parameter|unrecognized|unexpected|not supported|not_support",
                re.IGNORECASE,
            ),
            re.compile(r"不支持|非法|无效|参数"),
        ),
    ),
    (
        "bad_request",
        "revise_input",
        "图片供应商拒绝了本次请求，请调整提示词、参考图或参数后重试",
        (
            re.compile(r"\bbad request\b", re.IGNORECASE),
            re.compile(r"reject(ed)?|refus(ed|al)|denied", re.IGNORECASE),
            re.compile(r"请求被拒绝|拒绝请求"),
        ),
    ),
)

_RETRYABLE_FAILURE_RULES: tuple[
    tuple[ImageGenerationFailureCategory, ImageGenerationFailureRetryHint, str, tuple[re.Pattern[str], ...]],
    ...,
] = (
    (
        "rate_limit",
        "retry_later",
        "图片供应商限流或配额不足，请稍后重试或降低并发后再试",
        (
            re.compile(r"\b429\b"),
            re.compile(r"\brate[ _-]?limit(ed)?\b", re.IGNORECASE),
            re.compile(r"too many requests", re.IGNORECASE),
            re.compile(r"限流|频率"),
        ),
    ),
    (
        "quota",
        "retry_later",
        "图片供应商限流或配额不足，请稍后重试或降低并发后再试",
        (
            re.compile(r"quota|insufficient_quota", re.IGNORECASE),
            re.compile(r"配额"),
        ),
    ),
    (
        "connection",
        "retry_later",
        "图片供应商连接中断，请检查网络或代理后重试",
        (
            re.compile(r"connection reset|connection aborted|connection error|remote disconnected", re.IGNORECASE),
            re.compile(r"broken pipe|econnreset|network is unreachable|connection refused", re.IGNORECASE),
            re.compile(r"断流|连接中断|连接失败|网络不可达"),
        ),
    ),
    (
        "timeout",
        "retry_later",
        "图片供应商请求超时，请稍后重试",
        (
            re.compile(r"timeout|timed out|read timeout|connect timeout", re.IGNORECASE),
            re.compile(r"超时"),
        ),
    ),
    (
        "provider_5xx",
        "retry_later",
        "图片供应商服务异常，请稍后重试",
        (
            re.compile(r"\b5\d\d\b"),
            re.compile(r"server error|internal error|bad gateway|service unavailable|gateway timeout", re.IGNORECASE),
            re.compile(r"服务异常|服务不可用"),
        ),
    ),
)
_UNSUPPORTED_PARAMETER_PATTERNS = (
    re.compile(r"unsupported|invalid|bad request|\b400\b|unknown parameter|unrecognized|unexpected", re.IGNORECASE),
    re.compile(r"不支持|非法|无效|参数|尺寸"),
)
_ACTIONABLE_SIZE_DETAIL_PATTERNS = (
    re.compile(r"\d+\s*[x×]\s*\d+"),
    re.compile(r"最小|最大|min|max|尺寸|size", re.IGNORECASE),
)
_EXPLICIT_REJECT_PATTERNS = (
    re.compile(r"reject(ed)?|refus(ed|al)|denied|blocked", re.IGNORECASE),
    re.compile(r"拒绝|拦截"),
)


def _iter_exception_chain(exc: BaseException) -> Iterable[BaseException]:
    current: BaseException | None = exc
    seen: set[int] = set()
    while current is not None and id(current) not in seen:
        seen.add(id(current))
        yield current
        current = current.__cause__ or current.__context__


def _iter_exception_diagnostics(exc: BaseException) -> Iterable[str]:
    for current in _iter_exception_chain(exc):
        parts = [type(current).__name__, str(current)]
        status_code = getattr(current, "status_code", None)
        if status_code is not None:
            parts.append(str(status_code))
        code = getattr(current, "code", None)
        if code is not None:
            parts.append(str(code))
        response = getattr(current, "response", None)
        response_status_code = getattr(response, "status_code", None)
        if response_status_code is not None:
            parts.append(str(response_status_code))
        body = getattr(current, "body", None)
        if body is not None:
            parts.append(str(body))
        yield " ".join(part for part in parts if part)


def _iter_exception_display_messages(exc: BaseException) -> Iterable[str]:
    for current in _iter_exception_chain(exc):
        message = " ".join(str(current).strip().split())
        if message:
            yield message


def _contains_sensitive_material(message: str) -> bool:
    return any(pattern.search(message) for pattern in _SENSITIVE_FAILURE_PATTERNS)


def _decision(
    *,
    reason: str,
    retryable: bool,
    retry_hint: ImageGenerationFailureRetryHint,
    category: ImageGenerationFailureCategory,
) -> ImageGenerationFailureDecision:
    return ImageGenerationFailureDecision(
        reason=reason,
        retryable=retryable,
        retry_hint=retry_hint,
        category=category,
    )


def _categorized_failure_decision(messages: list[str]) -> ImageGenerationFailureDecision | None:
    haystack = " ".join(messages)
    for category, retry_hint, reason, patterns in _NON_RETRYABLE_FAILURE_RULES:
        if any(pattern.search(haystack) for pattern in patterns):
            return _decision(reason=reason, retryable=False, retry_hint=retry_hint, category=category)
    for category, retry_hint, reason, patterns in _RETRYABLE_FAILURE_RULES:
        if any(pattern.search(haystack) for pattern in patterns):
            return _decision(reason=reason, retryable=True, retry_hint=retry_hint, category=category)
    return None


def _uncategorized_display_decision(
    *,
    raw_message: str,
    diagnostics: list[str],
    generic_message: str,
) -> ImageGenerationFailureDecision:
    diagnostics_text = " ".join(diagnostics)
    if _contains_sensitive_material(raw_message):
        return _decision(reason=generic_message, retryable=True, retry_hint="retry_later", category="unknown")
    if all(pattern.search(raw_message) for pattern in _ACTIONABLE_SIZE_DETAIL_PATTERNS):
        return _decision(
            reason=f"图片生成失败：{raw_message[:300]}",
            retryable=False,
            retry_hint="check_settings",
            category="unsupported_parameters",
        )
    if any(pattern.search(diagnostics_text) for pattern in _UNSUPPORTED_PARAMETER_PATTERNS):
        return _decision(
            reason="图片供应商参数不支持，请检查尺寸、模型或高级参数后重试",
            retryable=False,
            retry_hint="check_settings",
            category="unsupported_parameters",
        )
    if any(pattern.search(diagnostics_text) for pattern in _EXPLICIT_REJECT_PATTERNS):
        return _decision(
            reason="图片供应商拒绝了本次请求，请调整提示词、参考图或参数后重试",
            retryable=False,
            retry_hint="revise_input",
            category="bad_request",
        )
    return _decision(
        reason=f"图片生成失败：{raw_message[:300]}",
        retryable=True,
        retry_hint="retry_later",
        category="unknown",
    )


def classify_image_generation_failure(
    exc: BaseException,
    *,
    generic_message: str,
) -> ImageGenerationFailureDecision:
    diagnostics = [" ".join(message.strip().split()) for message in _iter_exception_diagnostics(exc) if message.strip()]
    display_messages = list(_iter_exception_display_messages(exc))
    if not diagnostics and not display_messages:
        return _decision(reason=generic_message, retryable=True, retry_hint="retry_later", category="unknown")
    categorized_decision = _categorized_failure_decision(diagnostics)
    raw_message = display_messages[0] if display_messages else diagnostics[0]
    if not raw_message:
        return _decision(reason=generic_message, retryable=True, retry_hint="retry_later", category="unknown")
    if not _contains_sensitive_material(raw_message) and all(
        pattern.search(raw_message) for pattern in _ACTIONABLE_SIZE_DETAIL_PATTERNS
    ):
        return _decision(
            reason=f"图片生成失败：{raw_message[:300]}",
            retryable=False,
            retry_hint="check_settings",
            category="unsupported_parameters",
        )
    if categorized_decision is not None:
        return categorized_decision
    return _uncategorized_display_decision(
        raw_message=raw_message,
        diagnostics=diagnostics,
        generic_message=generic_message,
    )


def safe_image_generation_failure_reason(exc: BaseException, *, generic_message: str) -> str:
    return classify_image_generation_failure(exc, generic_message=generic_message).reason
