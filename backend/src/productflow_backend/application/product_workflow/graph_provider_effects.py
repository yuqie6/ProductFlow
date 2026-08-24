"""图运行 provider effect 账本：调用前记 intent；无法证明结果时标 unknown。"""

from __future__ import annotations

import hashlib
import json
from collections.abc import Iterable
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.time import now_utc
from productflow_backend.domain.durable_generation_tasks import (
    WORKFLOW_PROVIDER_EFFECT_SAFE_REQUEUE_PHASES,
    WORKFLOW_PROVIDER_EFFECT_UNKNOWN_DETAIL,
    WORKFLOW_PROVIDER_EFFECT_UNKNOWN_PHASE,
)
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowRunStatus
from productflow_backend.infrastructure.db.models import (
    WorkflowGraphNodeRun,
    WorkflowGraphProviderEffect,
    WorkflowGraphRun,
)

MAX_GRAPH_PROVIDER_EFFECT_JSON_BYTES = 64 * 1024
GRAPH_PROVIDER_EFFECT_KIND = "workflow_graph_generation"


class GraphRunEffectCrash(RuntimeError):
    """Process died after a durable effect phase was committed."""

    def __init__(self, phase: str, node_run_id: str | None = None) -> None:
        super().__init__(phase)
        self.phase = phase
        self.node_run_id = node_run_id


class GraphRunProviderUnknown(RuntimeError):
    """Provider result cannot be proved; durable UNKNOWN state is already committed."""


def graph_provider_effect_operation_key(node_run_id: str) -> str:
    return f"graph-node-run:{node_run_id}"


def graph_provider_effect_request_hash(request_json: dict[str, Any]) -> str:
    _validate_json_payload(request_json, "图运行 provider effect request")
    encoded = json.dumps(
        request_json,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
        allow_nan=False,
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def node_run_effect_is_safe_to_requeue(
    node_run: WorkflowGraphNodeRun,
    effect: WorkflowGraphProviderEffect | None,
) -> bool:
    """仅 claimed/prepared 且尚未真正调用 provider 才可安全重入队。"""

    phase = node_run.progress_phase
    if phase in WORKFLOW_PROVIDER_EFFECT_SAFE_REQUEUE_PHASES:
        if effect is None:
            return True
        return effect.effect_result == "pending" and effect.reconciliation_state == "not_requested"
    return False


def ensure_graph_provider_effect_intent(
    session: Session,
    *,
    node_run_id: str,
    attempt_id: str,
    request_hash: str,
    provider_name: str,
    request_json: dict[str, Any],
) -> bool:
    """Persist a provider-call intent before submitting the graph-run request."""

    _validate_json_payload(request_json, "图运行 provider effect intent")
    node_run = session.scalar(
        select(WorkflowGraphNodeRun)
        .where(WorkflowGraphNodeRun.id == node_run_id)
        .with_for_update()
        .execution_options(populate_existing=True)
    )
    if (
        node_run is None
        or node_run.status != WorkflowNodeStatus.RUNNING
        or node_run.active_attempt_id != attempt_id
    ):
        session.rollback()
        return False

    effect = session.scalar(
        select(WorkflowGraphProviderEffect)
        .where(WorkflowGraphProviderEffect.node_run_id == node_run_id)
        .with_for_update()
    )
    operation_key = graph_provider_effect_operation_key(node_run_id)
    if effect is None:
        session.add(
            WorkflowGraphProviderEffect(
                node_run_id=node_run_id,
                operation_key=operation_key,
                request_hash=request_hash,
                provider_name=provider_name,
                attempt_id=attempt_id,
                effect_result="pending",
                reconciliation_state="not_requested",
                request_json=request_json,
            )
        )
        session.flush()
        return True

    if (
        effect.operation_key != operation_key
        or effect.request_hash != request_hash
        or effect.provider_name != provider_name
    ):
        raise ValueError("图运行 provider effect operation identity 与请求不一致")
    if effect.effect_result in {"applied", "unknown"}:
        return False
    if effect.effect_result == "pending":
        return False

    effect.attempt_id = attempt_id
    effect.effect_result = "pending"
    effect.reconciliation_state = "not_requested"
    effect.provider_response_id = None
    effect.provider_status = None
    effect.request_json = request_json
    effect.result_json = None
    effect.detail = None
    session.flush()
    return True


def record_graph_provider_effect_result(
    session: Session,
    *,
    node_run_id: str,
    attempt_id: str,
    provider_response_id: str | None,
    provider_status: str | None,
    result_json: dict[str, Any] | None = None,
) -> bool:
    """已 unknown 的 effect 不能改成 applied；failed 保持原判。"""

    if result_json is not None:
        _validate_json_payload(result_json, "图运行 provider effect result")
    effect = _locked_effect(session, node_run_id=node_run_id)
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
    session.flush()
    return True


def mark_graph_provider_effect_unknown(
    session: Session,
    *,
    node_run_id: str,
    attempt_id: str,
    detail: str,
) -> bool:
    effect = _locked_effect(session, node_run_id=node_run_id)
    if effect is None:
        return False
    if effect.attempt_id != attempt_id:
        return False
    if effect.effect_result in {"applied", "failed"}:
        return True
    effect.effect_result = "unknown"
    effect.reconciliation_state = "unknown"
    effect.detail = detail[:1000]
    session.flush()
    return True


def mark_graph_run_provider_unknown(
    session: Session,
    *,
    run_id: str,
    node_run_id: str,
    attempt_id: str | None,
    detail: str = WORKFLOW_PROVIDER_EFFECT_UNKNOWN_DETAIL,
) -> bool:
    """把 run/node 标 UNKNOWN 且不可重试；不要把未证明的 provider 调用写成 FAILED。"""

    now = now_utc()
    run = session.scalar(
        select(WorkflowGraphRun).where(WorkflowGraphRun.id == run_id).with_for_update()
    )
    if run is None:
        return False
    if run.status == WorkflowRunStatus.UNKNOWN:
        return True
    if run.status in {WorkflowRunStatus.SUCCEEDED, WorkflowRunStatus.CANCELLED}:
        return False
    node_run = session.scalar(
        select(WorkflowGraphNodeRun)
        .where(WorkflowGraphNodeRun.id == node_run_id, WorkflowGraphNodeRun.graph_run_id == run_id)
        .with_for_update()
    )
    if node_run is None:
        return False
    if attempt_id is not None and node_run.active_attempt_id not in {None, attempt_id}:
        return False
    resolved_attempt = attempt_id or node_run.active_attempt_id
    if resolved_attempt:
        mark_graph_provider_effect_unknown(
            session,
            node_run_id=node_run.id,
            attempt_id=resolved_attempt,
            detail=detail,
        )
    node_run.status = WorkflowNodeStatus.UNKNOWN
    node_run.failure_reason = detail
    node_run.finished_at = now
    node_run.progress_phase = WORKFLOW_PROVIDER_EFFECT_UNKNOWN_PHASE
    node_run.progress_updated_at = now
    node_run.active_attempt_id = None
    _fail_sibling_active_nodes(run.node_runs, skip_id=node_run.id, reason=detail, now=now)
    run.status = WorkflowRunStatus.UNKNOWN
    run.failure_reason = detail
    run.finished_at = now
    run.is_retryable = False
    session.flush()
    return True


def reset_graph_node_run_for_safe_requeue(
    session: Session,
    node_run: WorkflowGraphNodeRun,
) -> None:
    """丢掉未提交的 pending intent，让节点回到 queued；已 unknown/applied 不得走这条。"""

    now = now_utc()
    effect = session.scalar(
        select(WorkflowGraphProviderEffect).where(
            WorkflowGraphProviderEffect.node_run_id == node_run.id
        )
    )
    if effect is not None and effect.effect_result == "pending":
        session.delete(effect)
    node_run.status = WorkflowNodeStatus.QUEUED
    node_run.active_attempt_id = None
    node_run.failure_reason = None
    node_run.finished_at = None
    node_run.progress_phase = "requeued_after_idle"
    node_run.progress_updated_at = now
    session.flush()


def load_node_run_effect(session: Session, node_run_id: str) -> WorkflowGraphProviderEffect | None:
    return session.scalar(
        select(WorkflowGraphProviderEffect).where(WorkflowGraphProviderEffect.node_run_id == node_run_id)
    )


def _fail_sibling_active_nodes(
    node_runs: Iterable[WorkflowGraphNodeRun],
    *,
    skip_id: str,
    reason: str,
    now,
) -> None:
    # 未证明的节点保持 unknown；尚未完成的兄弟节点没有 provider 结果，可以标 failed。
    for item in node_runs:
        if item.id == skip_id:
            continue
        if item.status in {WorkflowNodeStatus.QUEUED, WorkflowNodeStatus.RUNNING}:
            item.status = WorkflowNodeStatus.FAILED
            item.failure_reason = reason
            item.finished_at = now
            item.active_attempt_id = None
            item.progress_updated_at = now


def _locked_effect(session: Session, *, node_run_id: str) -> WorkflowGraphProviderEffect | None:
    return session.scalar(
        select(WorkflowGraphProviderEffect)
        .where(WorkflowGraphProviderEffect.node_run_id == node_run_id)
        .with_for_update()
    )


def _validate_json_payload(payload: dict[str, Any], label: str) -> None:
    encoded = json.dumps(
        payload,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
        allow_nan=False,
    ).encode("utf-8")
    if len(encoded) > MAX_GRAPH_PROVIDER_EFFECT_JSON_BYTES:
        raise ValueError(f"{label} 超过 {MAX_GRAPH_PROVIDER_EFFECT_JSON_BYTES} bytes")


__all__ = [
    "GRAPH_PROVIDER_EFFECT_KIND",
    "GraphRunEffectCrash",
    "GraphRunProviderUnknown",
    "ensure_graph_provider_effect_intent",
    "graph_provider_effect_operation_key",
    "graph_provider_effect_request_hash",
    "load_node_run_effect",
    "mark_graph_provider_effect_unknown",
    "mark_graph_run_provider_unknown",
    "node_run_effect_is_safe_to_requeue",
    "record_graph_provider_effect_result",
    "reset_graph_node_run_for_safe_requeue",
]
