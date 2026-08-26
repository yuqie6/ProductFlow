"""连续生图 provider 副作用账本。

intent 必须在发请求前落库。pending 不得重放。unknown 表示无法证明副作用，不能当成 failed。
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.image_sessions.dependencies import (
    ImageChatProviderFactory,
    default_image_session_chat_service_factory,
)
from productflow_backend.domain.enums import JobStatus
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import ImageSessionGenerationTask, ImageSessionProviderEffect
from productflow_backend.infrastructure.provider_effects import (
    PROVIDER_EFFECT_RESULT_APPLIED,
    PROVIDER_EFFECT_RESULT_FAILED,
    PROVIDER_EFFECT_RESULT_PENDING,
    PROVIDER_EFFECT_RESULT_UNKNOWN,
    PROVIDER_EFFECT_RESULTS,
    ProviderEffectQueryResult,
    canonical_provider_effect_json_hash,
    provider_effect_can_replay,
    provider_effect_is_terminal,
    transition_provider_effect_result,
    validate_provider_effect_json,
)

IMAGE_SESSION_PROVIDER_RECONCILIATION_STATES = {
    "not_requested",
    "applied",
    "not_applied",
    "unknown",
    "unsupported",
}


@dataclass(frozen=True, slots=True)
class ImageSessionProviderEffectReconciliationResult:
    id: str
    generation_task_id: str
    candidate_start_index: int
    candidate_count: int
    operation_key: str
    effect_kind: str
    request_hash: str
    provider_name: str
    effect_result: str
    reconciliation_state: str
    provider_response_id: str | None
    provider_status: str | None
    result_json: dict[str, Any] | None
    detail: str | None
    created_at: Any
    updated_at: Any


def image_session_provider_effect_operation_key(
    task_id: str,
    *,
    candidate_start_index: int,
    candidate_count: int,
) -> str:
    return f"image-session-task:{task_id}:candidates:{candidate_start_index}-{candidate_count}"


def image_session_provider_effect_request_hash(request_json: dict[str, Any]) -> str:
    return canonical_provider_effect_json_hash(request_json, "连续生图 provider effect request")


def ensure_image_session_provider_effect_intent(
    session: Session,
    *,
    task_id: str,
    attempt_id: str,
    candidate_start_index: int,
    candidate_count: int,
    operation_key: str,
    request_hash: str,
    provider_name: str,
    request_json: dict[str, Any],
) -> bool:
    """在提交图片请求之前持久化 provider-call intent。"""

    validate_provider_effect_json(request_json, "连续生图 provider effect intent")
    task = session.scalar(
        select(ImageSessionGenerationTask)
        .where(ImageSessionGenerationTask.id == task_id)
        .with_for_update()
        .execution_options(populate_existing=True)
    )
    if task is None or task.status != JobStatus.RUNNING or task.active_attempt_id != attempt_id:
        session.rollback()
        return False

    effect = session.scalar(
        select(ImageSessionProviderEffect)
        .where(
            ImageSessionProviderEffect.generation_task_id == task_id,
            ImageSessionProviderEffect.candidate_start_index == candidate_start_index,
        )
        .with_for_update()
    )
    if effect is None:
        effect = ImageSessionProviderEffect(
            generation_task_id=task_id,
            candidate_start_index=candidate_start_index,
            candidate_count=candidate_count,
            operation_key=operation_key,
            request_hash=request_hash,
            provider_name=provider_name,
            attempt_id=attempt_id,
            effect_result=PROVIDER_EFFECT_RESULT_PENDING,
            reconciliation_state="not_requested",
            request_json=request_json,
        )
        session.add(effect)
        session.flush()
        return True

    if (
        effect.candidate_count != candidate_count
        or effect.operation_key != operation_key
        or effect.request_hash != request_hash
        or effect.provider_name != provider_name
    ):
        raise ValueError("连续生图 provider effect operation identity 与请求不一致")
    if not provider_effect_can_replay(effect.effect_result):
        return False

    effect.attempt_id = attempt_id
    if not transition_provider_effect_result(effect, PROVIDER_EFFECT_RESULT_PENDING):
        return False
    effect.provider_response_id = None
    effect.provider_status = None
    effect.request_json = request_json
    effect.result_json = None
    effect.detail = None
    return True


def record_image_session_provider_effect_result(
    session: Session,
    *,
    task_id: str,
    attempt_id: str,
    candidate_start_index: int,
    provider_response_id: str | None,
    provider_status: str | None,
    result_json: dict[str, Any] | None = None,
) -> bool:
    """在本地资产落地之前持久化 provider 结果证据。"""

    if result_json is not None:
        validate_provider_effect_json(result_json, "连续生图 provider effect result")
    effect = _locked_effect(session, task_id=task_id, candidate_start_index=candidate_start_index)
    if effect is None or effect.attempt_id != attempt_id:
        return False
    if effect.effect_result == PROVIDER_EFFECT_RESULT_FAILED:
        return True
    if not transition_provider_effect_result(effect, PROVIDER_EFFECT_RESULT_APPLIED):
        return False
    effect.provider_response_id = provider_response_id
    effect.provider_status = provider_status
    effect.result_json = result_json
    effect.detail = None
    return True


def record_image_session_provider_effect_progress(
    session: Session,
    *,
    task_id: str,
    attempt_id: str,
    candidate_start_index: int,
    provider_response_id: str | None,
    provider_status: str | None,
) -> bool:
    """在 effect 仍未解决时持久化 provider 轮询身份。"""

    effect = _locked_effect(session, task_id=task_id, candidate_start_index=candidate_start_index)
    if effect is None or effect.attempt_id != attempt_id:
        return False
    if provider_effect_is_terminal(effect.effect_result):
        return True
    if provider_response_id is not None:
        effect.provider_response_id = provider_response_id
    if provider_status is not None:
        effect.provider_status = provider_status
    effect.result_json = {
        "provider_response_id": effect.provider_response_id,
        "provider_status": effect.provider_status,
    }
    return True


def mark_image_session_provider_effect_failed(
    session: Session,
    *,
    task_id: str,
    attempt_id: str,
    candidate_start_index: int,
    detail: str,
) -> bool:
    """仅在能证明未生效时记 failed；unknown/applied 不得覆盖。"""

    effect = _locked_effect(session, task_id=task_id, candidate_start_index=candidate_start_index)
    if effect is None or effect.attempt_id != attempt_id:
        return False
    if effect.effect_result == PROVIDER_EFFECT_RESULT_APPLIED:
        return True
    if not transition_provider_effect_result(effect, PROVIDER_EFFECT_RESULT_FAILED):
        return False
    effect.detail = detail[:1000]
    return True


def mark_image_session_provider_effect_unknown(
    session: Session,
    *,
    task_id: str,
    attempt_id: str,
    candidate_start_index: int,
    detail: str,
) -> bool:
    """无法证明 provider 是否生效时保留 unknown，禁止当失败重试。"""

    effect = _locked_effect(session, task_id=task_id, candidate_start_index=candidate_start_index)
    if effect is None or effect.attempt_id != attempt_id:
        return False
    if provider_effect_is_terminal(effect.effect_result):
        return True
    if not transition_provider_effect_result(effect, PROVIDER_EFFECT_RESULT_UNKNOWN):
        return False
    effect.detail = detail[:1000]
    return True


def reconcile_image_session_provider_effect(
    session: Session,
    *,
    task_id: str,
    candidate_start_index: int,
    image_session_id: str | None = None,
    chat_service_factory: ImageChatProviderFactory | None = None,
) -> ImageSessionProviderEffectReconciliationResult:
    """查询已有 unknown 副作用，不提交新请求。查不到仍保持 unknown。"""

    task_query = select(ImageSessionGenerationTask).where(ImageSessionGenerationTask.id == task_id)
    if image_session_id is not None:
        task_query = task_query.where(ImageSessionGenerationTask.session_id == image_session_id)
    task = session.scalar(task_query.with_for_update())
    if task is None:
        raise ConflictError("连续生图任务不存在")
    effect = _locked_effect(session, task_id=task_id, candidate_start_index=candidate_start_index)
    if effect is None:
        raise ConflictError("找不到连续生图 provider effect ledger")
    if task.status != JobStatus.UNKNOWN and effect.effect_result != PROVIDER_EFFECT_RESULT_UNKNOWN:
        raise ConflictError("只有 unknown 连续生图任务可以执行 provider effect 对账")
    if provider_effect_is_terminal(effect.effect_result):
        return _result(effect)

    snapshot = {
        "operation_key": effect.operation_key,
        "request_hash": effect.request_hash,
        "provider_name": effect.provider_name,
        "provider_response_id": effect.provider_response_id,
    }
    session.commit()

    try:
        service = (chat_service_factory or default_image_session_chat_service_factory)()
        if getattr(service, "provider_kind", None) != snapshot["provider_name"]:
            verdict = ProviderEffectQueryResult.unsupported("当前图片会话 provider 与原请求 provider 不一致")
        else:
            reconcile = getattr(service, "reconcile_generation_effect", None)
            if not callable(reconcile):
                verdict = ProviderEffectQueryResult.unsupported("当前图片会话 provider 不支持查询原请求")
            else:
                verdict = reconcile(
                    operation_key=snapshot["operation_key"],
                    request_hash=snapshot["request_hash"],
                    provider_response_id=snapshot["provider_response_id"],
                )
    except Exception as exc:  # noqa: BLE001
        verdict = ProviderEffectQueryResult(
            effect_result=PROVIDER_EFFECT_RESULT_UNKNOWN,
            reconciliation_state="unknown",
            provider_response_id=snapshot["provider_response_id"],
            detail=f"初始化或查询图片会话 provider 失败: {type(exc).__name__}",
        )

    _validate_verdict(verdict)
    task = session.scalar(
        select(ImageSessionGenerationTask)
        .where(ImageSessionGenerationTask.id == task_id)
        .with_for_update()
    )
    if task is None:
        raise ConflictError("连续生图任务在对账期间被删除")
    effect = _locked_effect(session, task_id=task_id, candidate_start_index=candidate_start_index)
    if effect is None:
        raise ConflictError("连续生图 provider effect ledger 在对账期间被删除")
    if provider_effect_is_terminal(effect.effect_result):
        return _result(effect)
    if not transition_provider_effect_result(
        effect,
        verdict.effect_result,
        allow_unknown_resolution=True,
    ):
        raise ConflictError("连续生图 provider reconciliation 无法迁移 effect 状态")
    effect.reconciliation_state = verdict.reconciliation_state
    effect.provider_response_id = verdict.provider_response_id or effect.provider_response_id
    effect.provider_status = verdict.provider_status
    effect.result_json = verdict.result_json
    effect.detail = verdict.detail[:1000] if verdict.detail else None
    session.commit()
    return _result(effect)


def _locked_effect(
    session: Session,
    *,
    task_id: str,
    candidate_start_index: int,
) -> ImageSessionProviderEffect | None:
    return session.scalar(
        select(ImageSessionProviderEffect)
        .where(
            ImageSessionProviderEffect.generation_task_id == task_id,
            ImageSessionProviderEffect.candidate_start_index == candidate_start_index,
        )
        .with_for_update()
        .execution_options(populate_existing=True)
    )


def _validate_verdict(verdict: ProviderEffectQueryResult) -> None:
    if verdict.effect_result not in PROVIDER_EFFECT_RESULTS - {PROVIDER_EFFECT_RESULT_PENDING}:
        raise ConflictError("图片会话 provider reconciliation 返回了未知 effect_result")
    if verdict.reconciliation_state not in IMAGE_SESSION_PROVIDER_RECONCILIATION_STATES - {"not_requested"}:
        raise ConflictError("图片会话 provider reconciliation 返回了未知 reconciliation_state")
    if verdict.result_json is not None:
        validate_provider_effect_json(verdict.result_json, "连续生图 provider reconciliation result")


def _result(effect: ImageSessionProviderEffect) -> ImageSessionProviderEffectReconciliationResult:
    return ImageSessionProviderEffectReconciliationResult(
        id=effect.id,
        generation_task_id=effect.generation_task_id,
        candidate_start_index=effect.candidate_start_index,
        candidate_count=effect.candidate_count,
        operation_key=effect.operation_key,
        effect_kind=effect.effect_kind,
        request_hash=effect.request_hash,
        provider_name=effect.provider_name,
        effect_result=effect.effect_result,
        reconciliation_state=effect.reconciliation_state,
        provider_response_id=effect.provider_response_id,
        provider_status=effect.provider_status,
        result_json=effect.result_json,
        detail=effect.detail,
        created_at=effect.created_at,
        updated_at=effect.updated_at,
    )


__all__ = [
    "ImageSessionProviderEffectReconciliationResult",
    "ensure_image_session_provider_effect_intent",
    "image_session_provider_effect_operation_key",
    "image_session_provider_effect_request_hash",
    "mark_image_session_provider_effect_failed",
    "mark_image_session_provider_effect_unknown",
    "record_image_session_provider_effect_progress",
    "record_image_session_provider_effect_result",
    "reconcile_image_session_provider_effect",
]
