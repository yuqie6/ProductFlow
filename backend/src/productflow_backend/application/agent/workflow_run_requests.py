"""Agent 只创建待确认运行请求。确认后走同一套 graph submit/retry，不另起执行模型。"""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

from sqlalchemy import and_, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent.conversations import get_agent_conversation_by_id_or_raise
from productflow_backend.application.agent.idempotency import (
    canonical_json_request_hash,
    normalize_idempotency_key,
)
from productflow_backend.application.agent.tasks import get_agent_task_or_raise
from productflow_backend.application.product_workflow.graph_commands import (
    get_active_workflow_graph,
    load_applied_graph,
)
from productflow_backend.application.product_workflow.graph_compiler import select_run_node_ids
from productflow_backend.application.product_workflow.graph_runs import (
    GRAPH_CANCELLED_REASON,
    cancel_graph_run,
    get_graph_run,
    retry_graph_run,
    submit_graph_run,
)
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import (
    AgentConversationScope,
    AgentConversationStatus,
    AgentTaskStatus,
    AgentTurnStatus,
    AgentWorkflowRunRequestStatus,
    GraphRunScope,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentTask,
    AgentTurnProjection,
    AgentWorkflowRunRequest,
    WorkflowGraph,
    new_id,
)

AGENT_WORKFLOW_RUN_REQUEST_MAX_STEP_ID_LENGTH = 120


@dataclass(frozen=True, slots=True)
class AgentWorkflowRunRequestPreparation:
    product_id: str
    workflow_id: str
    workflow_title: str
    workflow_revision: int
    runnable_node_count: int
    task_id: str | None
    source_run_id: str | None = None
    graph_id: str | None = None
    source_graph_run_id: str | None = None


@dataclass(frozen=True, slots=True)
class AgentWorkflowRunRequestReconcileResult:
    state: str
    request: AgentWorkflowRunRequest | None = None
    detail: str | None = None


@dataclass(frozen=True, slots=True)
class _ResolvedRunnable:
    public_id: str
    title: str
    revision: int
    runnable_node_count: int
    graph_id: str | None
    source_run_id: str | None
    source_graph_run_id: str | None


def prepare_agent_workflow_run_request(
    session: Session,
    *,
    conversation_id: str,
    expected_workflow_revision: int,
    task_id: str | None = None,
    source_run_id: str | None = None,
) -> AgentWorkflowRunRequestPreparation:
    """prepare：只读当前 active graph，不创建运行。"""
    conversation = _get_product_conversation(session, conversation_id)
    resolved = _prepare_current_workflow(
        session,
        product_id=conversation.product_id or "",
        expected_workflow_revision=expected_workflow_revision,
        source_run_id=source_run_id,
    )
    _validate_task_scope(
        session,
        conversation=conversation,
        task_id=task_id,
    )
    return AgentWorkflowRunRequestPreparation(
        product_id=conversation.product_id or "",
        workflow_id=resolved.public_id,
        workflow_title=resolved.title,
        workflow_revision=resolved.revision,
        runnable_node_count=resolved.runnable_node_count,
        task_id=task_id,
        source_run_id=resolved.source_run_id,
        graph_id=resolved.graph_id,
        source_graph_run_id=resolved.source_graph_run_id,
    )


def prepare_agent_global_workflow_run_request(
    session: Session,
    *,
    conversation_id: str,
    product_id: str,
    workflow_id: str,
    expected_workflow_revision: int,
    task_id: str | None = None,
    source_run_id: str | None = None,
) -> AgentWorkflowRunRequestPreparation:
    """全局 prepare：必须显式 product/workflow，不能用当前页氛围顶替目标。"""
    conversation = _get_global_conversation(session, conversation_id)
    normalized_product_id = _normalize_required_id(product_id, "product_id")
    normalized_workflow_id = _normalize_required_id(workflow_id, "workflow_id")
    _validate_task_scope(
        session,
        conversation=conversation,
        task_id=task_id,
    )
    resolved = _prepare_explicit_workflow(
        session,
        product_id=normalized_product_id,
        workflow_id=normalized_workflow_id,
        expected_workflow_revision=expected_workflow_revision,
        source_run_id=source_run_id,
    )
    return AgentWorkflowRunRequestPreparation(
        product_id=normalized_product_id,
        workflow_id=resolved.public_id,
        workflow_title=resolved.title,
        workflow_revision=resolved.revision,
        runnable_node_count=resolved.runnable_node_count,
        task_id=task_id,
        source_run_id=resolved.source_run_id,
        graph_id=resolved.graph_id,
        source_graph_run_id=resolved.source_graph_run_id,
    )


def create_agent_workflow_run_request(
    session: Session,
    *,
    conversation_id: str,
    expected_workflow_revision: int,
    workflow_id: str,
    source_step_id: str,
    idempotency_key: str,
    task_id: str | None = None,
    source_run_id: str | None = None,
) -> AgentWorkflowRunRequest:
    return _create_agent_workflow_run_request(
        session,
        conversation_id=conversation_id,
        expected_workflow_revision=expected_workflow_revision,
        workflow_id=workflow_id,
        source_step_id=source_step_id,
        idempotency_key=idempotency_key,
        task_id=task_id,
        target_product_id=None,
        request_hash_product_id=None,
        source_run_id=source_run_id,
        load_conversation=lambda lock: _get_product_conversation(session, conversation_id, lock=lock),
        prepare=lambda: prepare_agent_workflow_run_request(
            session,
            conversation_id=conversation_id,
            expected_workflow_revision=expected_workflow_revision,
            task_id=task_id,
            source_run_id=source_run_id,
        ),
    )


def create_agent_global_workflow_run_request(
    session: Session,
    *,
    conversation_id: str,
    product_id: str,
    expected_workflow_revision: int,
    workflow_id: str,
    source_step_id: str,
    idempotency_key: str,
    task_id: str | None = None,
    source_run_id: str | None = None,
) -> AgentWorkflowRunRequest:
    normalized_product_id = _normalize_required_id(product_id, "product_id")
    return _create_agent_workflow_run_request(
        session,
        conversation_id=conversation_id,
        expected_workflow_revision=expected_workflow_revision,
        workflow_id=workflow_id,
        source_step_id=source_step_id,
        idempotency_key=idempotency_key,
        task_id=task_id,
        target_product_id=normalized_product_id,
        request_hash_product_id=normalized_product_id,
        source_run_id=source_run_id,
        load_conversation=lambda lock: _get_global_conversation(session, conversation_id, lock=lock),
        prepare=lambda: prepare_agent_global_workflow_run_request(
            session,
            conversation_id=conversation_id,
            product_id=normalized_product_id,
            workflow_id=workflow_id,
            expected_workflow_revision=expected_workflow_revision,
            task_id=task_id,
            source_run_id=source_run_id,
        ),
    )


def _create_agent_workflow_run_request(
    session: Session,
    *,
    conversation_id: str,
    expected_workflow_revision: int,
    workflow_id: str,
    source_step_id: str,
    idempotency_key: str,
    task_id: str | None,
    target_product_id: str | None,
    request_hash_product_id: str | None,
    source_run_id: str | None,
    load_conversation: Callable[[bool], AgentConversation],
    prepare: Callable[[], AgentWorkflowRunRequestPreparation],
) -> AgentWorkflowRunRequest:
    """apply：写入待确认请求。同一 key 必须绑定同一 request hash。本函数 commit。"""
    normalized_key = normalize_idempotency_key(
        idempotency_key,
        field_name="工作流执行请求 idempotency key",
    )
    normalized_step_id = _normalize_source_step_id(source_step_id)
    normalized_workflow_id = _normalize_required_id(workflow_id, "workflow_id")
    normalized_source_run_id = (
        _normalize_required_id(source_run_id, "source_run_id") if source_run_id is not None else None
    )
    conversation = load_conversation(True)
    normalized_product_id = _normalize_required_id(
        target_product_id or conversation.product_id or "",
        "product_id",
    )
    request_hash = _request_hash(
        conversation_id=conversation_id,
        task_id=task_id,
        product_id=request_hash_product_id,
        workflow_id=normalized_workflow_id,
        expected_workflow_revision=expected_workflow_revision,
        source_step_id=normalized_step_id,
        source_run_id=normalized_source_run_id,
    )
    existing = session.scalar(
        select(AgentWorkflowRunRequest)
        .options(
            selectinload(AgentWorkflowRunRequest.graph),
            selectinload(AgentWorkflowRunRequest.product),
            selectinload(AgentWorkflowRunRequest.graph_run),
            selectinload(AgentWorkflowRunRequest.task),
        )
        .where(
            AgentWorkflowRunRequest.conversation_id == conversation.id,
            AgentWorkflowRunRequest.idempotency_key == normalized_key,
        )
    )
    if existing is not None:
        if existing.request_hash != request_hash:
            raise ConflictError("同一 idempotency key 不能提交不同的工作流执行请求")
        # 同 key 同 hash 回放已有请求，不另建运行。
        session.commit()
        return _load_request(session, existing.id)

    preparation = prepare()
    if preparation.product_id != normalized_product_id:
        raise ConflictError("工作流执行请求的 product_id 与当前目标不一致")
    if preparation.workflow_id != normalized_workflow_id:
        raise ConflictError("工作流执行请求的 workflow_id 与当前 active 工作流不一致")
    if preparation.graph_id is None:
        raise ConflictError("当前商品没有可执行的 schema-v3 工作流")
    request = AgentWorkflowRunRequest(
        id=new_id(),
        conversation_id=conversation.id,
        task_id=task_id,
        product_id=preparation.product_id,
        graph_id=preparation.graph_id,
        source_graph_run_id=preparation.source_graph_run_id,
        expected_workflow_revision=preparation.workflow_revision,
        idempotency_key=normalized_key,
        request_hash=request_hash,
        source_step_id=normalized_step_id,
        status=AgentWorkflowRunRequestStatus.AWAITING_CONFIRMATION,
    )
    session.add(request)
    task = _task_for_request(session, task_id)
    _mark_request_waiting(conversation, task)
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        replay = session.scalar(
            select(AgentWorkflowRunRequest).where(
                AgentWorkflowRunRequest.conversation_id == conversation.id,
                AgentWorkflowRunRequest.idempotency_key == normalized_key,
            )
        )
        if replay is not None and replay.request_hash == request_hash:
            return _load_request(session, replay.id)
        raise
    return _load_request(session, request.id)


def reconcile_agent_workflow_run_request(
    session: Session,
    *,
    conversation_id: str,
    expected_workflow_revision: int,
    workflow_id: str,
    source_step_id: str,
    idempotency_key: str,
    task_id: str | None = None,
    source_run_id: str | None = None,
) -> AgentWorkflowRunRequestReconcileResult:
    return _reconcile_agent_workflow_run_request(
        session,
        conversation_id=conversation_id,
        expected_workflow_revision=expected_workflow_revision,
        workflow_id=workflow_id,
        source_step_id=source_step_id,
        idempotency_key=idempotency_key,
        task_id=task_id,
        source_run_id=source_run_id,
        request_hash_product_id=None,
        load_conversation=lambda: _get_product_conversation(session, conversation_id),
    )


def reconcile_agent_global_workflow_run_request(
    session: Session,
    *,
    conversation_id: str,
    product_id: str,
    expected_workflow_revision: int,
    workflow_id: str,
    source_step_id: str,
    idempotency_key: str,
    task_id: str | None = None,
    source_run_id: str | None = None,
) -> AgentWorkflowRunRequestReconcileResult:
    normalized_product_id = _normalize_required_id(product_id, "product_id")
    return _reconcile_agent_workflow_run_request(
        session,
        conversation_id=conversation_id,
        expected_workflow_revision=expected_workflow_revision,
        workflow_id=workflow_id,
        source_step_id=source_step_id,
        idempotency_key=idempotency_key,
        task_id=task_id,
        source_run_id=source_run_id,
        request_hash_product_id=normalized_product_id,
        load_conversation=lambda: _get_global_conversation(session, conversation_id),
    )


def _reconcile_agent_workflow_run_request(
    session: Session,
    *,
    conversation_id: str,
    expected_workflow_revision: int,
    workflow_id: str,
    source_step_id: str,
    idempotency_key: str,
    task_id: str | None,
    request_hash_product_id: str | None,
    source_run_id: str | None,
    load_conversation: Callable[[], AgentConversation],
) -> AgentWorkflowRunRequestReconcileResult:
    """reconcile：不重放 create。hash 冲突为 conflict，缺失为 not_applied。"""
    normalized_key = normalize_idempotency_key(
        idempotency_key,
        field_name="工作流执行请求 idempotency key",
    )
    normalized_step_id = _normalize_source_step_id(source_step_id)
    normalized_workflow_id = _normalize_required_id(workflow_id, "workflow_id")
    normalized_source_run_id = (
        _normalize_required_id(source_run_id, "source_run_id") if source_run_id is not None else None
    )
    request_hash = _request_hash(
        conversation_id=conversation_id,
        task_id=task_id,
        product_id=request_hash_product_id,
        workflow_id=normalized_workflow_id,
        expected_workflow_revision=expected_workflow_revision,
        source_step_id=normalized_step_id,
        source_run_id=normalized_source_run_id,
    )
    load_conversation()
    request = session.scalar(
        select(AgentWorkflowRunRequest).where(
            AgentWorkflowRunRequest.conversation_id == conversation_id,
            AgentWorkflowRunRequest.idempotency_key == normalized_key,
        )
    )
    if request is None:
        return AgentWorkflowRunRequestReconcileResult(state="not_applied")
    if request.request_hash != request_hash:
        return AgentWorkflowRunRequestReconcileResult(
            state="conflict",
            detail="同一 idempotency key 已对应其他工作流执行请求",
        )
    return AgentWorkflowRunRequestReconcileResult(
        state="applied",
        request=_load_request(session, request.id),
    )


def get_agent_workflow_run_request(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    task_id: str | None = None,
    request_id: str | None = None,
) -> AgentWorkflowRunRequest | None:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    if product_id is None:
        if conversation.scope_type != AgentConversationScope.GLOBAL:
            raise NotFoundError("Agent workflow run request 不存在")
    elif (
        conversation.scope_type != AgentConversationScope.PRODUCT_WORKFLOW
        or conversation.product_id != product_id
    ):
        raise NotFoundError("Agent workflow run request 不存在")
    statement = (
        select(AgentWorkflowRunRequest)
        .options(
            selectinload(AgentWorkflowRunRequest.graph),
            selectinload(AgentWorkflowRunRequest.product),
            selectinload(AgentWorkflowRunRequest.graph_run),
            selectinload(AgentWorkflowRunRequest.task),
        )
        .where(AgentWorkflowRunRequest.conversation_id == conversation_id)
    )
    if task_id is not None:
        _validate_task_scope(session, conversation=conversation, task_id=task_id)
        statement = statement.where(AgentWorkflowRunRequest.task_id == task_id)
    if request_id is not None:
        statement = statement.where(AgentWorkflowRunRequest.id == request_id)
    else:
        statement = statement.order_by(
            AgentWorkflowRunRequest.created_at.desc(),
            AgentWorkflowRunRequest.id.desc(),
        ).limit(1)
    request = session.scalar(statement)
    if request is None:
        return None
    changed = _sync_request_from_workflow_run(session, request)
    if changed:
        session.commit()
        request = _load_request(session, request.id)
    return request


def get_agent_workflow_run_request_by_source_step(
    session: Session,
    *,
    conversation_id: str,
    task_id: str | None,
    source_step_id: str,
) -> AgentWorkflowRunRequest | None:
    statement = select(AgentWorkflowRunRequest).where(
        AgentWorkflowRunRequest.conversation_id == conversation_id,
        AgentWorkflowRunRequest.source_step_id == source_step_id,
    )
    if task_id is not None:
        statement = statement.where(AgentWorkflowRunRequest.task_id == task_id)
    return session.scalar(statement)


def attach_agent_workflow_run_request(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    request_id: str,
    commit: bool = True,
) -> AgentTurnProjection:
    """把待确认运行请求挂到 Turn 投影。commit=False 时由调用方持有事务。"""
    conversation_scope = (
        AgentConversation.scope_type == AgentConversationScope.GLOBAL
        if product_id is None
        else and_(
            AgentConversation.scope_type == AgentConversationScope.PRODUCT_WORKFLOW,
            AgentConversation.product_id == product_id,
        )
    )
    projection = session.scalar(
        select(AgentTurnProjection)
        .join(AgentConversation, AgentConversation.id == AgentTurnProjection.conversation_id)
        .where(
            AgentTurnProjection.id == projection_id,
            AgentTurnProjection.conversation_id == conversation_id,
            conversation_scope,
        )
        .with_for_update()
    )
    if projection is None:
        raise NotFoundError("Agent Turn 不存在")
    request = session.scalar(
        select(AgentWorkflowRunRequest).where(
            AgentWorkflowRunRequest.id == request_id,
            AgentWorkflowRunRequest.conversation_id == conversation_id,
        )
    )
    if request is None:
        raise ConflictError("Agent workflow run request 不存在")
    if projection.workflow_run_request_id not in {None, request.id}:
        raise ConflictError("Agent Turn 已关联其他工作流执行请求")
    projection.workflow_run_request_id = request.id
    projection.status = projection.status.AWAITING_CONFIRMATION
    projection.updated_at = now_utc()
    if commit:
        session.commit()
        session.refresh(projection)
    return projection


def confirm_agent_workflow_run_request(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    request_id: str,
) -> AgentWorkflowRunRequest:
    """用户确认后走同一套 graph submit/retry。已有 graph_run_id 则幂等回放。本函数 commit。"""
    request = _load_request_for_update(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        request_id=request_id,
    )
    _sync_request_from_workflow_run(session, request)
    if request.status == AgentWorkflowRunRequestStatus.CANCELLED:
        raise ConflictError("已取消的工作流执行请求不能确认")
    if request.graph_run_id is not None:
        session.commit()
        return _load_request(session, request.id)
    if request.status != AgentWorkflowRunRequestStatus.AWAITING_CONFIRMATION:
        raise ConflictError("当前工作流执行请求不在待确认状态")

    if request.graph_id is not None:
        graph = session.get(WorkflowGraph, request.graph_id)
        if graph is None or graph.product_id != request.product_id:
            raise ConflictError("工作流已经发生变化，请重新让 Agent 检查后再确认")
        if graph.revision != request.expected_workflow_revision:
            raise ConflictError("工作流已经发生变化，请重新让 Agent 检查后再确认")
        if request.source_graph_run_id is not None:
            submission = retry_graph_run(
                session,
                product_id=request.product_id,
                graph_id=request.graph_id,
                run_id=request.source_graph_run_id,
                commit=False,
            )
        else:
            submission = submit_graph_run(
                session,
                product_id=request.product_id,
                graph_id=request.graph_id,
                scope=GraphRunScope.GRAPH,
                commit=False,
            )
        request.graph_run_id = submission.run.id
        request.status = AgentWorkflowRunRequestStatus.CONFIRMED
        request.confirmed_at = now_utc()
        request.failure_reason = None
        request.updated_at = now_utc()
        _mark_turn_succeeded(request.turn_projection)
        _mark_task_running(request.task)
        request.conversation.status = AgentConversationStatus.COMPLETED
        request.conversation.updated_at = now_utc()
        session.commit()
        return _load_request(session, request.id)

    raise ConflictError("当前商品没有可执行的 schema-v3 工作流")


def cancel_agent_workflow_run_request(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    request_id: str,
) -> AgentWorkflowRunRequest:
    request = _load_request_for_update(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        request_id=request_id,
    )
    _sync_request_from_workflow_run(session, request)
    if request.status == AgentWorkflowRunRequestStatus.CANCELLED:
        session.commit()
        return _load_request(session, request.id)
    if request.graph_run_id is None:
        request.status = AgentWorkflowRunRequestStatus.CANCELLED
        request.failure_reason = GRAPH_CANCELLED_REASON
        request.finished_at = now_utc()
        request.updated_at = now_utc()
        _mark_turn_cancelled(request.turn_projection)
        _mark_task_cancelled(request.task)
        request.conversation.status = AgentConversationStatus.CANCELED
        request.conversation.updated_at = now_utc()
        session.commit()
        return _load_request(session, request.id)
    if request.graph_id is not None:
        if request.graph_run is None:
            raise ConflictError("工作流执行请求关联的运行记录不存在")
        if request.graph_run.status == WorkflowRunStatus.CANCELLED:
            _sync_request_from_workflow_run(session, request)
            session.commit()
            return _load_request(session, request.id)
        if request.graph_run.status != WorkflowRunStatus.RUNNING:
            raise ConflictError("已结束的工作流运行不能取消")
        cancel_graph_run(
            session,
            product_id=request.product_id,
            graph_id=request.graph_id,
            run_id=request.graph_run_id or request.graph_run.id,
        )
        request = _load_request_for_update(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            request_id=request_id,
        )
        _sync_request_from_workflow_run(session, request)
        session.commit()
        return _load_request(session, request.id)
    raise ConflictError("当前商品没有可执行的 schema-v3 工作流")


def _get_product_conversation(
    session: Session,
    conversation_id: str,
    *,
    lock: bool = False,
) -> AgentConversation:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    if conversation.scope_type != AgentConversationScope.PRODUCT_WORKFLOW or conversation.product_id is None:
        raise ConflictError("只有商品工作流 Agent conversation 可以请求执行工作流")
    if lock:
        locked = session.scalar(
            select(AgentConversation)
            .where(AgentConversation.id == conversation_id)
            .with_for_update()
        )
        if locked is not None:
            conversation = locked
    return conversation


def _get_global_conversation(
    session: Session,
    conversation_id: str,
    *,
    lock: bool = False,
) -> AgentConversation:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    if conversation.scope_type != AgentConversationScope.GLOBAL:
        raise ConflictError("只有全局 Agent conversation 可以请求跨商品执行工作流")
    if lock:
        locked = session.scalar(
            select(AgentConversation)
            .where(AgentConversation.id == conversation_id)
            .with_for_update()
        )
        if locked is not None:
            conversation = locked
    return conversation


def _prepare_current_workflow(
    session: Session,
    *,
    product_id: str,
    expected_workflow_revision: int,
    source_run_id: str | None = None,
) -> _ResolvedRunnable:
    if expected_workflow_revision <= 0:
        raise BusinessValidationError("expected_workflow_revision 必须大于 0")
    graph = get_active_workflow_graph(session, product_id=product_id)
    if graph is not None:
        return _resolve_graph_runnable(
            session,
            product_id=product_id,
            graph=graph,
            expected_workflow_revision=expected_workflow_revision,
            source_run_id=source_run_id,
        )
    raise ConflictError("当前商品还没有可执行的工作流")


def _prepare_explicit_workflow(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    expected_workflow_revision: int,
    source_run_id: str | None = None,
) -> _ResolvedRunnable:
    if expected_workflow_revision <= 0:
        raise BusinessValidationError("expected_workflow_revision 必须大于 0")
    graph = session.scalar(
        select(WorkflowGraph).where(WorkflowGraph.id == workflow_id, WorkflowGraph.product_id == product_id)
    )
    if graph is not None:
        return _resolve_graph_runnable(
            session,
            product_id=product_id,
            graph=graph,
            expected_workflow_revision=expected_workflow_revision,
            source_run_id=source_run_id,
        )
    raise NotFoundError("工作流不存在")


def _resolve_graph_runnable(
    session: Session,
    *,
    product_id: str,
    graph: WorkflowGraph,
    expected_workflow_revision: int,
    source_run_id: str | None,
) -> _ResolvedRunnable:
    if graph.revision != expected_workflow_revision:
        raise ConflictError("工作流 revision 已变化，请重新读取当前工作流")
    applied = load_applied_graph(session, graph)
    node_ids = select_run_node_ids(applied, scope=GraphRunScope.GRAPH, target_node_id=None)
    if not node_ids:
        raise ConflictError("当前工作流没有可运行的节点")
    source_graph_run_id = None
    if source_run_id is not None:
        source = get_graph_run(session, product_id=product_id, graph_id=graph.id, run_id=source_run_id)
        if source.status != WorkflowRunStatus.FAILED or not source.is_retryable:
            raise BusinessValidationError("只有失败且可重试的工作流运行可以再次请求执行")
        source_graph_run_id = source.id
    return _ResolvedRunnable(
        public_id=graph.id,
        title=graph.title,
        revision=graph.revision,
        runnable_node_count=len(node_ids),
        graph_id=graph.id,
        source_run_id=None,
        source_graph_run_id=source_graph_run_id,
    )


def _validate_task_scope(
    session: Session,
    *,
    conversation: AgentConversation,
    task_id: str | None,
) -> AgentTask | None:
    if task_id is None:
        return None
    task = get_agent_task_or_raise(session, task_id)
    if (
        task.session_id != conversation.session_id
        or task.conversation_id != conversation.id
        or task.product_id != conversation.product_id
    ):
        raise ConflictError("Agent Task 与当前 Agent conversation 不匹配")
    return task


def _task_for_request(session: Session, task_id: str | None) -> AgentTask | None:
    return session.get(AgentTask, task_id) if task_id is not None else None


def _mark_request_waiting(conversation: AgentConversation, task: AgentTask | None) -> None:
    now = now_utc()
    conversation.status = AgentConversationStatus.AWAITING_CONFIRMATION
    conversation.updated_at = now
    if task is not None:
        task.status = AgentTaskStatus.AWAITING_CONFIRMATION
        task.waiting_reason = "workflow_run_confirmation"
        task.failure_reason = None
        task.finished_at = None
        task.updated_at = now


def _mark_task_running(task: AgentTask | None) -> None:
    if task is None:
        return
    now = now_utc()
    task.status = AgentTaskStatus.RUNNING
    task.waiting_reason = "workflow_run_running"
    task.failure_reason = None
    task.started_at = task.started_at or now
    task.finished_at = None
    task.updated_at = now


def _mark_task_cancelled(task: AgentTask | None) -> None:
    if task is None:
        return
    now = now_utc()
    task.status = AgentTaskStatus.CANCELED
    task.waiting_reason = None
    task.canceled_at = now
    task.finished_at = now
    task.updated_at = now


def _sync_request_from_workflow_run(session: Session, request: AgentWorkflowRunRequest) -> bool:
    run = request.graph_run
    if run is None:
        return False
    now = now_utc()
    changed = False
    if run.status == WorkflowRunStatus.RUNNING:
        if request.status != AgentWorkflowRunRequestStatus.CONFIRMED:
            request.status = AgentWorkflowRunRequestStatus.CONFIRMED
            changed = True
        if request.task is not None:
            _mark_task_running(request.task)
    elif run.status == WorkflowRunStatus.SUCCEEDED:
        if request.status != AgentWorkflowRunRequestStatus.SUCCEEDED:
            request.status = AgentWorkflowRunRequestStatus.SUCCEEDED
            request.finished_at = run.finished_at or now
            changed = True
        _mark_task_finished(request.task, AgentTaskStatus.SUCCEEDED, None, run.finished_at or now)
    elif run.status == WorkflowRunStatus.FAILED:
        if request.status != AgentWorkflowRunRequestStatus.FAILED or request.failure_reason != run.failure_reason:
            request.status = AgentWorkflowRunRequestStatus.FAILED
            request.failure_reason = run.failure_reason
            request.finished_at = run.finished_at or now
            changed = True
        _mark_task_finished(request.task, AgentTaskStatus.FAILED, run.failure_reason, run.finished_at or now)
    elif run.status == WorkflowRunStatus.CANCELLED:
        if request.status != AgentWorkflowRunRequestStatus.CANCELLED:
            request.status = AgentWorkflowRunRequestStatus.CANCELLED
            request.failure_reason = run.failure_reason or GRAPH_CANCELLED_REASON
            request.finished_at = run.finished_at or now
            changed = True
        _mark_task_finished(request.task, AgentTaskStatus.CANCELED, request.failure_reason, run.finished_at or now)
    if changed:
        request.updated_at = now
    return changed


def _mark_turn_succeeded(projection: AgentTurnProjection | None) -> None:
    if projection is None:
        return
    now = now_utc()
    projection.status = AgentTurnStatus.SUCCEEDED
    projection.finished_at = projection.finished_at or now
    projection.updated_at = now


def _mark_turn_cancelled(projection: AgentTurnProjection | None) -> None:
    if projection is None:
        return
    now = now_utc()
    projection.status = AgentTurnStatus.CANCELED
    projection.finished_at = projection.finished_at or now
    projection.updated_at = now


def _mark_task_finished(
    task: AgentTask | None,
    status: AgentTaskStatus,
    failure_reason: str | None,
    finished_at: Any,
) -> None:
    if task is None:
        return
    task.status = status
    task.waiting_reason = None
    task.failure_reason = failure_reason
    task.finished_at = finished_at
    task.updated_at = now_utc()


def _load_request(session: Session, request_id: str) -> AgentWorkflowRunRequest:
    request = session.scalar(
        select(AgentWorkflowRunRequest)
        .options(
            selectinload(AgentWorkflowRunRequest.conversation),
            selectinload(AgentWorkflowRunRequest.graph),
            selectinload(AgentWorkflowRunRequest.product),
            selectinload(AgentWorkflowRunRequest.graph_run),
            selectinload(AgentWorkflowRunRequest.task),
            selectinload(AgentWorkflowRunRequest.turn_projection),
        )
        .where(AgentWorkflowRunRequest.id == request_id)
        .execution_options(populate_existing=True)
    )
    if request is None:
        raise NotFoundError("Agent workflow run request 不存在")
    return request


def _load_request_for_update(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    request_id: str,
) -> AgentWorkflowRunRequest:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    if product_id is None:
        if conversation.scope_type != AgentConversationScope.GLOBAL:
            raise NotFoundError("Agent workflow run request 不存在")
    elif (
        conversation.scope_type != AgentConversationScope.PRODUCT_WORKFLOW
        or conversation.product_id != product_id
    ):
        raise NotFoundError("Agent workflow run request 不存在")
    request_filters = [
        AgentWorkflowRunRequest.id == request_id,
        AgentWorkflowRunRequest.conversation_id == conversation_id,
    ]
    if product_id is not None:
        request_filters.append(AgentWorkflowRunRequest.product_id == product_id)
    request = session.scalar(
        select(AgentWorkflowRunRequest)
        .options(
            selectinload(AgentWorkflowRunRequest.conversation),
            selectinload(AgentWorkflowRunRequest.graph),
            selectinload(AgentWorkflowRunRequest.product),
            selectinload(AgentWorkflowRunRequest.graph_run),
            selectinload(AgentWorkflowRunRequest.task),
            selectinload(AgentWorkflowRunRequest.turn_projection),
        )
        .where(*request_filters)
        .with_for_update()
        .execution_options(populate_existing=True)
    )
    if request is None:
        raise NotFoundError("Agent workflow run request 不存在")
    return request


def _normalize_source_step_id(value: str) -> str:
    normalized = value.strip()
    if not normalized or len(normalized) > AGENT_WORKFLOW_RUN_REQUEST_MAX_STEP_ID_LENGTH:
        raise BusinessValidationError("工作流执行请求 source_step_id 无效")
    return normalized


def _normalize_required_id(value: str, field_name: str) -> str:
    normalized = value.strip()
    if not normalized or len(normalized) > 64:
        raise BusinessValidationError(f"{field_name} 无效")
    return normalized


def _request_hash(
    *,
    conversation_id: str,
    task_id: str | None,
    product_id: str | None,
    workflow_id: str,
    expected_workflow_revision: int,
    source_step_id: str,
    source_run_id: str | None = None,
) -> str:
    payload: dict[str, object] = {
        "schema_version": 1,
        "conversation_id": conversation_id,
        "task_id": task_id,
        "workflow_id": workflow_id,
        "expected_workflow_revision": expected_workflow_revision,
        "source_step_id": source_step_id,
    }
    if product_id is not None:
        payload["product_id"] = product_id
    if source_run_id is not None:
        payload["source_run_id"] = source_run_id
    return canonical_json_request_hash(payload)


__all__ = [
    "AgentWorkflowRunRequestPreparation",
    "AgentWorkflowRunRequestReconcileResult",
    "attach_agent_workflow_run_request",
    "cancel_agent_workflow_run_request",
    "confirm_agent_workflow_run_request",
    "create_agent_global_workflow_run_request",
    "create_agent_workflow_run_request",
    "get_agent_workflow_run_request",
    "get_agent_workflow_run_request_by_source_step",
    "prepare_agent_workflow_run_request",
    "prepare_agent_global_workflow_run_request",
    "reconcile_agent_global_workflow_run_request",
    "reconcile_agent_workflow_run_request",
]
