from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.product_workflow_dependencies import (
    WorkflowExecutionDependencies,
    default_workflow_execution_dependencies,
)
from productflow_backend.domain.enums import WorkflowNodeStatus
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import WorkflowNodeRun, WorkflowProviderEffect
from productflow_backend.infrastructure.provider_effects import ProviderEffectQueryResult

MAX_WORKFLOW_PROVIDER_EFFECT_JSON_BYTES = 64 * 1024
WORKFLOW_PROVIDER_EFFECT_RESULTS = {"pending", "applied", "failed", "unknown"}
WORKFLOW_PROVIDER_RECONCILIATION_STATES = {
    "not_requested",
    "applied",
    "not_applied",
    "unknown",
    "unsupported",
}


@dataclass(frozen=True, slots=True)
class WorkflowProviderEffectReconciliationResult:
    id: str
    workflow_node_run_id: str
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


def workflow_provider_effect_operation_key(node_run_id: str) -> str:
    return f"workflow-node:{node_run_id}"


def ensure_workflow_provider_effect_intent(
    session: Session,
    *,
    node_run_id: str,
    attempt_id: str,
    operation_key: str,
    effect_kind: str,
    request_hash: str,
    provider_name: str,
    request_json: dict[str, Any],
) -> bool:
    """Create the provider ledger row before the provider call is submitted."""

    _validate_json_payload(request_json, "工作流 provider effect intent")
    node_run = session.scalar(
        select(WorkflowNodeRun)
        .where(WorkflowNodeRun.id == node_run_id)
        .with_for_update()
        .execution_options(populate_existing=True)
    )
    if node_run is None or node_run.active_attempt_id != attempt_id:
        session.rollback()
        return False

    effect = session.scalar(
        select(WorkflowProviderEffect)
        .where(WorkflowProviderEffect.workflow_node_run_id == node_run_id)
        .with_for_update()
    )
    if effect is None:
        effect = WorkflowProviderEffect(
            workflow_node_run_id=node_run_id,
            operation_key=operation_key,
            effect_kind=effect_kind,
            request_hash=request_hash,
            provider_name=provider_name,
            attempt_id=attempt_id,
            effect_result="pending",
            reconciliation_state="not_requested",
            request_json=request_json,
        )
        session.add(effect)
        session.flush()
        return True

    if (
        effect.operation_key != operation_key
        or effect.effect_kind != effect_kind
        or effect.request_hash != request_hash
    ):
        raise ValueError("工作流 provider effect operation key 与请求身份不一致")
    if effect.effect_result in {"applied", "failed", "unknown"}:
        return False
    effect.provider_name = provider_name
    effect.attempt_id = attempt_id
    effect.request_json = request_json
    return True


def record_workflow_provider_effect_result(
    session: Session,
    *,
    node_run_id: str,
    attempt_id: str,
    provider_response_id: str | None,
    provider_status: str | None,
    result_json: dict[str, Any] | None = None,
) -> bool:
    """Record that the provider returned a usable result, before business persistence."""

    if result_json is not None:
        _validate_json_payload(result_json, "工作流 provider effect result")
    effect = _locked_effect(session, node_run_id)
    if effect is None or effect.attempt_id != attempt_id:
        return False
    if effect.effect_result == "unknown":
        return False
    if effect.effect_result == "failed":
        return True
    effect.effect_result = "applied"
    effect.reconciliation_state = "applied"
    effect.provider_response_id = provider_response_id
    effect.provider_status = provider_status
    effect.result_json = result_json
    effect.detail = None
    return True


def mark_workflow_provider_effect_failed(
    session: Session,
    *,
    node_run_id: str,
    attempt_id: str,
    detail: str,
) -> bool:
    effect = _locked_effect(session, node_run_id)
    if effect is None or effect.attempt_id != attempt_id:
        return False
    if effect.effect_result == "applied":
        return True
    if effect.effect_result == "unknown":
        return False
    effect.effect_result = "failed"
    effect.reconciliation_state = "not_applied"
    effect.detail = detail[:1000]
    return True


def mark_workflow_provider_effect_unknown(
    session: Session,
    *,
    node_run_id: str,
    attempt_id: str,
    detail: str,
    result_json: dict[str, Any] | None = None,
) -> bool:
    if result_json is not None:
        _validate_json_payload(result_json, "工作流 provider effect unknown result")
    effect = _locked_effect(session, node_run_id)
    if effect is None or effect.attempt_id != attempt_id:
        return False
    if effect.effect_result in {"applied", "failed"}:
        return True
    effect.effect_result = "unknown"
    effect.reconciliation_state = "unknown"
    effect.detail = detail[:1000]
    if result_json is not None:
        effect.result_json = result_json
    return True


def reconcile_workflow_provider_effect(
    session: Session,
    *,
    node_run_id: str,
    dependencies: WorkflowExecutionDependencies | None = None,
) -> WorkflowProviderEffectReconciliationResult:
    """Query a provider for an unknown effect; never call its mutation endpoint."""

    node_run = session.scalar(
        select(WorkflowNodeRun)
        .where(WorkflowNodeRun.id == node_run_id)
        .with_for_update()
    )
    if node_run is None:
        raise ConflictError("工作流节点运行不存在")
    effect = session.scalar(
        select(WorkflowProviderEffect)
        .where(WorkflowProviderEffect.workflow_node_run_id == node_run_id)
        .with_for_update()
    )
    if effect is None:
        raise ConflictError("找不到工作流 provider effect ledger")
    if node_run.status != WorkflowNodeStatus.UNKNOWN and effect.effect_result != "unknown":
        raise ConflictError("只有 unknown 工作流节点运行可以执行 provider effect 对账")
    if effect.effect_result in {"applied", "failed"}:
        return _result(effect)

    snapshot = {
        "effect_kind": effect.effect_kind,
        "operation_key": effect.operation_key,
        "request_hash": effect.request_hash,
        "provider_name": effect.provider_name,
        "provider_response_id": effect.provider_response_id,
    }
    session.commit()

    try:
        resolved_dependencies = dependencies or default_workflow_execution_dependencies()
        if snapshot["effect_kind"] == "prompt_generation":
            provider = resolved_dependencies.prompt_generation_provider()
            if provider.provider_name != snapshot["provider_name"]:
                verdict = ProviderEffectQueryResult.unsupported("当前提示词 provider 与原请求 provider 不一致")
            else:
                verdict = provider.reconcile_prompt_effect(**_provider_query_kwargs(snapshot))
        elif snapshot["effect_kind"] == "image_generation":
            provider = resolved_dependencies.image_provider()
            if provider.provider_name != snapshot["provider_name"]:
                verdict = ProviderEffectQueryResult.unsupported("当前图片 provider 与原请求 provider 不一致")
            else:
                verdict = provider.reconcile_workflow_image_effect(**_provider_query_kwargs(snapshot))
        else:
            verdict = ProviderEffectQueryResult.unsupported("未知的工作流 provider effect 类型")
    except Exception as exc:  # noqa: BLE001
        verdict = ProviderEffectQueryResult(
            effect_result="unknown",
            reconciliation_state="unknown",
            provider_response_id=snapshot["provider_response_id"],
            detail=f"初始化或查询 provider 失败: {type(exc).__name__}",
        )

    _validate_verdict(verdict)
    effect = session.scalar(
        select(WorkflowProviderEffect)
        .where(WorkflowProviderEffect.workflow_node_run_id == node_run_id)
        .with_for_update()
    )
    if effect is None:
        raise ConflictError("工作流 provider effect ledger 在对账期间被删除")
    if effect.effect_result in {"applied", "failed"}:
        return _result(effect)
    effect.effect_result = verdict.effect_result
    effect.reconciliation_state = verdict.reconciliation_state
    effect.provider_response_id = verdict.provider_response_id or effect.provider_response_id
    effect.provider_status = verdict.provider_status
    effect.result_json = verdict.result_json
    effect.detail = verdict.detail[:1000] if verdict.detail else None
    session.commit()
    return _result(effect)


def _locked_effect(session: Session, node_run_id: str) -> WorkflowProviderEffect | None:
    return session.scalar(
        select(WorkflowProviderEffect)
        .where(WorkflowProviderEffect.workflow_node_run_id == node_run_id)
        .with_for_update()
        .execution_options(populate_existing=True)
    )


def _provider_query_kwargs(snapshot: dict[str, Any]) -> dict[str, Any]:
    return {
        "operation_key": snapshot["operation_key"],
        "request_hash": snapshot["request_hash"],
        "provider_response_id": snapshot["provider_response_id"],
    }


def _validate_verdict(verdict: ProviderEffectQueryResult) -> None:
    if verdict.effect_result not in {"applied", "failed", "unknown"}:
        raise ConflictError("provider reconciliation 返回了未知 effect_result")
    if verdict.reconciliation_state not in {
        "applied",
        "not_applied",
        "unknown",
        "unsupported",
    }:
        raise ConflictError("provider reconciliation 返回了未知 reconciliation_state")
    if verdict.result_json is not None:
        _validate_json_payload(verdict.result_json, "工作流 provider reconciliation result")


def _validate_json_payload(payload: dict[str, Any], label: str) -> None:
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
    if len(encoded) > MAX_WORKFLOW_PROVIDER_EFFECT_JSON_BYTES:
        raise ValueError(f"{label} 超过大小限制")


def _result(effect: WorkflowProviderEffect) -> WorkflowProviderEffectReconciliationResult:
    return WorkflowProviderEffectReconciliationResult(
        id=effect.id,
        workflow_node_run_id=effect.workflow_node_run_id,
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
    "WorkflowProviderEffectReconciliationResult",
    "ensure_workflow_provider_effect_intent",
    "mark_workflow_provider_effect_failed",
    "mark_workflow_provider_effect_unknown",
    "reconcile_workflow_provider_effect",
    "record_workflow_provider_effect_result",
    "workflow_provider_effect_operation_key",
]
