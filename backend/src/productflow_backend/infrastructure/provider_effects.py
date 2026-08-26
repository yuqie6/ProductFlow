from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from typing import Any

MAX_PROVIDER_EFFECT_JSON_BYTES = 64 * 1024
PROVIDER_EFFECT_RESULT_PENDING = "pending"
PROVIDER_EFFECT_RESULT_APPLIED = "applied"
PROVIDER_EFFECT_RESULT_FAILED = "failed"
PROVIDER_EFFECT_RESULT_UNKNOWN = "unknown"
PROVIDER_EFFECT_RESULTS = frozenset(
    {
        PROVIDER_EFFECT_RESULT_PENDING,
        PROVIDER_EFFECT_RESULT_APPLIED,
        PROVIDER_EFFECT_RESULT_FAILED,
        PROVIDER_EFFECT_RESULT_UNKNOWN,
    }
)
PROVIDER_EFFECT_TERMINAL_RESULTS = frozenset(
    {
        PROVIDER_EFFECT_RESULT_APPLIED,
        PROVIDER_EFFECT_RESULT_FAILED,
    }
)


@dataclass(frozen=True, slots=True)
class ProviderEffectQueryResult:
    """对丢失原始响应的副作用做只读查询结论。

    unsupported 仍是 unknown：不能据此证明失败或重放请求。
    """

    effect_result: str
    reconciliation_state: str
    provider_response_id: str | None = None
    provider_status: str | None = None
    result_json: dict[str, Any] | None = None
    detail: str | None = None

    @classmethod
    def unsupported(cls, detail: str) -> ProviderEffectQueryResult:
        return cls(
            effect_result=PROVIDER_EFFECT_RESULT_UNKNOWN,
            reconciliation_state="unsupported",
            detail=detail,
        )


def canonical_provider_effect_json_hash(payload: dict[str, Any], label: str) -> str:
    """校验并计算 provider effect 使用的有界规范 JSON 哈希。"""

    return hashlib.sha256(_encode_provider_effect_json(payload, label)).hexdigest()


def validate_provider_effect_json(payload: dict[str, Any], label: str) -> None:
    """校验 provider effect JSON 可编码且不超过有界大小。"""

    _encode_provider_effect_json(payload, label)


def provider_effect_can_replay(effect_result: str) -> bool:
    """只有已证实未生效的 effect 才允许重新提交。"""

    return effect_result == PROVIDER_EFFECT_RESULT_FAILED


def provider_effect_is_pending(effect_result: str) -> bool:
    return effect_result == PROVIDER_EFFECT_RESULT_PENDING


def provider_effect_is_terminal(effect_result: str) -> bool:
    return effect_result in PROVIDER_EFFECT_TERMINAL_RESULTS


def transition_provider_effect_result(
    effect: Any,
    target: str,
    *,
    allow_unknown_resolution: bool = False,
    force_unknown: bool = False,
) -> bool:
    """按 provider effect 证据安全迁移结果状态。

    普通 provider 回调不能把 unknown 改成 applied/failed，也不能把已结束状态覆盖；
    只有显式 reconciliation 才能通过 ``allow_unknown_resolution`` 解析 unknown。
    ``force_unknown`` 仅供已越过 fencing 的迟到结果审计使用。
    """

    if target not in PROVIDER_EFFECT_RESULTS:
        raise ValueError(f"未知 provider effect result: {target}")

    current = effect.effect_result
    if target == PROVIDER_EFFECT_RESULT_PENDING:
        if current != PROVIDER_EFFECT_RESULT_FAILED:
            return False
    elif target == PROVIDER_EFFECT_RESULT_APPLIED:
        if current == PROVIDER_EFFECT_RESULT_UNKNOWN and not allow_unknown_resolution:
            return False
        if current == PROVIDER_EFFECT_RESULT_FAILED:
            return True
    elif target == PROVIDER_EFFECT_RESULT_FAILED:
        if current == PROVIDER_EFFECT_RESULT_UNKNOWN and not allow_unknown_resolution:
            return False
        if current == PROVIDER_EFFECT_RESULT_APPLIED:
            return True
    elif target == PROVIDER_EFFECT_RESULT_UNKNOWN:
        if current in PROVIDER_EFFECT_TERMINAL_RESULTS and not force_unknown:
            return True

    effect.effect_result = target
    reconciliation_state = {
        PROVIDER_EFFECT_RESULT_PENDING: "not_requested",
        PROVIDER_EFFECT_RESULT_APPLIED: "applied",
        PROVIDER_EFFECT_RESULT_FAILED: "not_applied",
        PROVIDER_EFFECT_RESULT_UNKNOWN: "unknown",
    }[target]
    if hasattr(effect, "reconciliation_state"):
        effect.reconciliation_state = reconciliation_state
    return True


def _encode_provider_effect_json(payload: dict[str, Any], label: str) -> bytes:
    try:
        encoded = json.dumps(
            payload,
            ensure_ascii=False,
            sort_keys=True,
            separators=(",", ":"),
            allow_nan=False,
        ).encode("utf-8")
    except (TypeError, ValueError) as exc:
        raise ValueError(f"{label} 不是有效 JSON") from exc
    if len(encoded) > MAX_PROVIDER_EFFECT_JSON_BYTES:
        raise ValueError(f"{label} 超过 {MAX_PROVIDER_EFFECT_JSON_BYTES} bytes")
    return encoded


__all__ = [
    "MAX_PROVIDER_EFFECT_JSON_BYTES",
    "PROVIDER_EFFECT_RESULT_APPLIED",
    "PROVIDER_EFFECT_RESULT_FAILED",
    "PROVIDER_EFFECT_RESULT_PENDING",
    "PROVIDER_EFFECT_RESULT_UNKNOWN",
    "PROVIDER_EFFECT_RESULTS",
    "PROVIDER_EFFECT_TERMINAL_RESULTS",
    "ProviderEffectQueryResult",
    "canonical_provider_effect_json_hash",
    "provider_effect_can_replay",
    "provider_effect_is_pending",
    "provider_effect_is_terminal",
    "transition_provider_effect_result",
    "validate_provider_effect_json",
]
