from __future__ import annotations

from dataclasses import dataclass
from typing import Any


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
            effect_result="unknown",
            reconciliation_state="unsupported",
            detail=detail,
        )


__all__ = ["ProviderEffectQueryResult"]
