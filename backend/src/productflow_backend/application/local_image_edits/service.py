"""局部编辑：源/结果是 ProductImageAsset，mask 是 MediaObject。

provider 边界之后若无法证明副作用，记 unknown 而不是 failed。
Adoption 只改节点 current artifact 指向的商品图片 id，不替换源图身份。
"""

from __future__ import annotations

import hashlib
import json
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any

from sqlalchemy import delete, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.async_delivery import (
    delivery_key_for_actor,
    requeue_async_dispatch,
    stage_async_dispatch,
)
from productflow_backend.application.media_objects import (
    inspect_image_bytes,
    prune_unreferenced_media_objects,
    stage_verified_media_object,
)
from productflow_backend.application.product_images.assets import stage_product_image_asset
from productflow_backend.application.storage_compensation import (
    StorageWriteCompensation,
    best_effort_storage_delete,
    compensate_storage_writes,
)
from productflow_backend.application.time import now_utc
from productflow_backend.domain.durable_generation_tasks import LOCAL_IMAGE_EDIT_TASK_CONTRACT
from productflow_backend.domain.enums import (
    GraphArtifactType,
    GraphNodeType,
    LocalImageEditTaskStatus,
    MediaVerificationStatus,
    ProductImageOriginType,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AsyncDispatch,
    LocalImageEditAdoptionEvent,
    LocalImageEditProviderAttempt,
    LocalImageEditTask,
    LocalImageEditTaskReference,
    MediaObject,
    Product,
    ProductImageAsset,
    WorkflowGraph,
    WorkflowGraphArtifact,
    WorkflowGraphNode,
    new_id,
)
from productflow_backend.infrastructure.image.base import (
    LOCAL_EDIT_MODE,
    ImageProvider,
    LocalEditCapability,
    LocalEditImage,
    LocalEditMask,
    LocalEditRequest,
    LocalEditResult,
    UnsupportedLocalEditError,
    infer_extension,
)
from productflow_backend.infrastructure.image.factory import get_image_provider
from productflow_backend.infrastructure.provider_effects import (
    PROVIDER_EFFECT_RESULT_APPLIED,
    PROVIDER_EFFECT_RESULT_FAILED,
    PROVIDER_EFFECT_RESULT_PENDING,
    PROVIDER_EFFECT_RESULT_UNKNOWN,
    PROVIDER_EFFECT_RESULTS,
    canonical_provider_effect_json_hash,
    transition_provider_effect_result,
)
from productflow_backend.infrastructure.storage import LocalStorage

from .contracts import LocalEditMaskGeometry, LocalImageEditDraft, validate_and_normalize_local_edit_mask

LOCAL_EDIT_STALE_CLAIM_AFTER = timedelta(minutes=10)
LOCAL_EDIT_MASK_FILENAME = "local-edit-mask.png"
LOCAL_EDIT_ACTOR_NAME = LOCAL_IMAGE_EDIT_TASK_CONTRACT.actor_name
LOCAL_EDIT_LIST_LIMIT = 50
LOCAL_EDIT_AUDIT_LIMIT = 50


@dataclass(frozen=True, slots=True)
class LocalImageEditSubmitResult:
    task: LocalImageEditTask
    dispatch: AsyncDispatch
    created: bool


@dataclass(frozen=True, slots=True)
class LocalImageEditClaim:
    task_id: str
    attempt_id: str
    attempt_number: int


@dataclass(frozen=True, slots=True)
class LocalImageEditExecutionResult:
    task_id: str
    attempt_id: str | None
    status: LocalImageEditTaskStatus
    result_asset_id: str | None


@dataclass(frozen=True, slots=True)
class LocalImageEditAdoptionResult:
    task_id: str
    event: LocalImageEditAdoptionEvent
    artifact: WorkflowGraphArtifact


@dataclass(frozen=True, slots=True)
class LocalImageEditRevertResult:
    task_id: str
    event: LocalImageEditAdoptionEvent
    current_artifact_id: str


@dataclass(frozen=True, slots=True)
class LocalImageEditRecoveryResult:
    task_id: str
    outcome: str
    status: LocalImageEditTaskStatus


def create_local_image_edit_task(
    session: Session,
    *,
    product_id: str,
    source_asset_id: str,
    draft: LocalImageEditDraft,
    mask_png_bytes: bytes,
    target_node_id: str | None = None,
    storage: LocalStorage | None = None,
) -> LocalImageEditTask:
    """校验并持久化草稿，附带不可变的源图与蒙版快照。"""

    storage = storage or LocalStorage()
    product = _lock_product(session, product_id)
    source_asset = _lock_source_asset(session, product_id=product_id, asset_id=source_asset_id)
    target_snapshot = _validate_target_node(
        session,
        product_id=product_id,
        source_asset=source_asset,
        target_node_id=target_node_id,
    )
    references = _lock_reference_assets(
        session,
        product_id=product_id,
        reference_asset_ids=draft.reference_asset_ids,
    )
    normalized_mask = _normalize_mask(source_asset, mask_png_bytes=mask_png_bytes, geometry=draft.mask_geometry)
    task_id = new_id()
    old_media_cleanup: list[tuple[str, str]] = []
    try:
        with compensate_storage_writes(session) as storage_writes:
            mask_media = _stage_mask_media(
                session,
                storage=storage,
                storage_writes=storage_writes,
                task_id=task_id,
                normalized_mask=normalized_mask,
            )
            task = LocalImageEditTask(
                id=task_id,
                product_id=product.id,
                source_asset_id=source_asset.id,
                source_media_sha256=source_asset.media_object.sha256 or "",
                mask_media_object_id=mask_media.id,
                target_graph_id=target_snapshot["graph_id"],
                target_node_id=target_snapshot["node_id"],
                target_graph_revision=target_snapshot["graph_revision"],
                source_artifact_id=target_snapshot["artifact_id"],
                source_artifact_asset_id=target_snapshot["artifact_asset_id"],
                source_artifact_input_digest=target_snapshot["artifact_input_digest"],
                operation=draft.operation.value,
                instruction=draft.instruction,
                source_text=draft.source_text,
                replacement_text=draft.replacement_text,
                mask_geometry_json=draft.mask_geometry.model_dump(mode="json"),
                status=LocalImageEditTaskStatus.DRAFT,
                revision=1,
                attempts=0,
                is_retryable=True,
            )
            session.add(task)
            session.flush()
            _replace_task_references(session, task_id=task.id, references=references)
            product.updated_at = now_utc()
            session.commit()
    except Exception:
        session.rollback()
        raise
    _cleanup_media_paths(storage, old_media_cleanup)
    session.expire_all()
    persisted = session.get(LocalImageEditTask, task_id)
    if persisted is None:
        raise RuntimeError("局部编辑 draft 保存后消失")
    return persisted


def update_local_image_edit_task(
    session: Session,
    *,
    task_id: str,
    expected_revision: int,
    draft: LocalImageEditDraft,
    mask_png_bytes: bytes | None = None,
    storage: LocalStorage | None = None,
) -> LocalImageEditTask:
    """用乐观 revision fencing 更新草稿的 intent/mask/references。"""

    storage = storage or LocalStorage()
    task = _lock_task(session, task_id)
    product = _lock_product(session, task.product_id)
    if task.status != LocalImageEditTaskStatus.DRAFT:
        raise ConflictError("局部编辑任务已提交，intent、mask 和 references 不可修改")
    if task.revision != expected_revision:
        raise ConflictError("局部编辑 draft 已变化，请刷新后重试")
    source_asset = _lock_source_asset(session, product_id=product.id, asset_id=task.source_asset_id)
    references = _lock_reference_assets(
        session,
        product_id=product.id,
        reference_asset_ids=draft.reference_asset_ids,
    )
    normalized_mask = (
        _normalize_mask(source_asset, mask_png_bytes=mask_png_bytes, geometry=draft.mask_geometry)
        if mask_png_bytes is not None
        else None
    )
    old_media_id = task.mask_media_object_id
    cleanup_paths: list[tuple[str, str]] = []
    try:
        with compensate_storage_writes(session) as storage_writes:
            if normalized_mask is not None:
                mask_media = _stage_mask_media(
                    session,
                    storage=storage,
                    storage_writes=storage_writes,
                    task_id=task.id,
                    normalized_mask=normalized_mask,
                )
                task.mask_media_object_id = mask_media.id
            task.operation = draft.operation.value
            task.instruction = draft.instruction
            task.source_text = draft.source_text
            task.replacement_text = draft.replacement_text
            task.mask_geometry_json = draft.mask_geometry.model_dump(mode="json")
            task.revision += 1
            _replace_task_references(session, task_id=task.id, references=references)
            session.flush()
            if normalized_mask is not None:
                cleanup_paths = prune_unreferenced_media_objects(session, {old_media_id})
            product.updated_at = now_utc()
            session.commit()
    except Exception:
        session.rollback()
        raise
    _cleanup_media_paths(storage, cleanup_paths)
    session.expire_all()
    persisted = session.get(LocalImageEditTask, task_id)
    if persisted is None:
        raise RuntimeError("局部编辑 draft 更新后消失")
    return persisted


def submit_local_image_edit_task(
    session: Session,
    *,
    task_id: str,
    idempotency_key: str,
    requested_provider_name: str,
    requested_local_edit_mode: str,
) -> LocalImageEditSubmitResult:
    """原子把草稿迁到 queued，并创建耐久 dispatch。"""

    normalized_key = _normalize_idempotency_key(idempotency_key)
    normalized_provider_name = _normalize_provider_intent_value(requested_provider_name, "provider name")
    normalized_local_edit_mode = _normalize_provider_intent_value(requested_local_edit_mode, "local edit mode")
    if normalized_local_edit_mode != LOCAL_EDIT_MODE:
        raise BusinessValidationError("局部编辑 provider 能力模式不受支持")
    task = _lock_task(session, task_id)
    product = _lock_product(session, task.product_id)
    request_hash = _local_edit_request_hash(
        session,
        task,
        requested_provider_name=normalized_provider_name,
        requested_local_edit_mode=normalized_local_edit_mode,
    )
    existing = session.scalar(
        select(LocalImageEditTask)
        .where(
            LocalImageEditTask.product_id == product.id,
            LocalImageEditTask.idempotency_key == normalized_key,
        )
        .with_for_update()
    )
    if existing is not None:
        if existing.request_hash != request_hash:
            raise ConflictError("相同 idempotency key 不能提交不同的局部编辑请求")
        dispatch = _get_or_stage_dispatch(session, existing)
        session.commit()
        return LocalImageEditSubmitResult(task=existing, dispatch=dispatch, created=False)

    if task.status != LocalImageEditTaskStatus.DRAFT:
        raise ConflictError("局部编辑任务已提交，不能重复提交新的 idempotency key")
    task.status = LocalImageEditTaskStatus.QUEUED
    task.idempotency_key = normalized_key
    task.request_hash = request_hash
    task.requested_provider_name = normalized_provider_name
    task.requested_local_edit_mode = normalized_local_edit_mode
    task.progress_phase = "queued"
    task.queued_at = now_utc()
    task.failure_reason = None
    task.is_retryable = True
    dispatch = stage_async_dispatch(
        session,
        delivery_key=delivery_key_for_actor(LOCAL_EDIT_ACTOR_NAME, task.id),
        actor_name=LOCAL_EDIT_ACTOR_NAME,
        aggregate_id=task.id,
        payload={"task_id": task.id, "request_hash": request_hash},
    )
    product.updated_at = now_utc()
    session.commit()
    return LocalImageEditSubmitResult(task=task, dispatch=dispatch, created=True)


def get_local_image_edit_capability() -> LocalEditCapability:
    return get_image_provider().local_edit_capability


def _validate_local_edit_capability_for_task(task: LocalImageEditTask, capability: LocalEditCapability) -> None:
    if not capability.supported or capability.mode is None:
        raise BusinessValidationError(capability.reason or "当前图片 provider 不支持局部编辑")
    supported_operations = {operation.value for operation in capability.operations}
    if task.operation not in supported_operations:
        raise BusinessValidationError("当前图片 provider 不支持该局部编辑操作")
    if len(task.references) > capability.max_reference_images:
        raise BusinessValidationError("局部编辑参考图数量超过当前图片 provider 能力")


def submit_queued_local_image_edit(
    session: Session,
    *,
    product_id: str,
    task_id: str,
    idempotency_key: str,
) -> LocalImageEditSubmitResult:
    task = get_local_image_edit_task(session, product_id=product_id, task_id=task_id)
    capability = get_local_image_edit_capability()
    _validate_local_edit_capability_for_task(task, capability)
    if capability.mode is None:
        raise BusinessValidationError(capability.reason or "当前图片 provider 不支持局部编辑")
    return submit_local_image_edit_task(
        session,
        task_id=task_id,
        idempotency_key=idempotency_key,
        requested_provider_name=capability.provider_name,
        requested_local_edit_mode=capability.mode,
    )


def get_local_image_edit_task(
    session: Session,
    *,
    product_id: str,
    task_id: str,
) -> LocalImageEditTask:
    """返回一条商品范围内的任务，并加载安全投影关系。"""

    task = session.scalar(
        _task_projection_query().where(
            LocalImageEditTask.product_id == product_id,
            LocalImageEditTask.id == task_id,
        )
    )
    if task is None:
        raise NotFoundError("局部编辑任务不存在")
    _attach_bounded_audit_projection(session, task)
    return task


def list_local_image_edit_tasks(
    session: Session,
    *,
    product_id: str,
    limit: int = LOCAL_EDIT_LIST_LIMIT,
) -> list[LocalImageEditTask]:
    """按最新优先列出商品范围内的局部编辑任务。"""

    if session.get(Product, product_id) is None:
        raise NotFoundError("商品不存在")
    if limit < 1 or limit > 100:
        raise BusinessValidationError("局部编辑任务 limit 必须在 1 到 100 之间")
    return list(
        session.scalars(
            _task_projection_query()
            .where(LocalImageEditTask.product_id == product_id)
            .order_by(LocalImageEditTask.created_at.desc(), LocalImageEditTask.id.desc())
            .limit(limit)
        ).all()
    )


def retry_local_image_edit_task(
    session: Session,
    *,
    task_id: str,
    expected_revision: int | None = None,
) -> LocalImageEditSubmitResult:
    """只把明确可重试的失败任务重新入队，不改其请求。"""

    task = _lock_task(session, task_id)
    if expected_revision is not None and task.revision != expected_revision:
        raise ConflictError("局部编辑任务已变化，请刷新后重试")
    if task.status != LocalImageEditTaskStatus.FAILED or not task.is_retryable:
        raise ConflictError("只有可重试的 failed 局部编辑任务才能重试")
    if task.request_hash is None or task.idempotency_key is None:
        raise ConflictError("局部编辑任务缺少不可变请求身份，不能重试")
    task.status = LocalImageEditTaskStatus.QUEUED
    task.active_attempt_id = None
    task.progress_phase = "queued"
    task.failure_reason = None
    task.is_retryable = True
    task.queued_at = now_utc()
    task.started_at = None
    task.finished_at = None
    dispatch = requeue_async_dispatch(
        session,
        delivery_key=delivery_key_for_actor(LOCAL_EDIT_ACTOR_NAME, task.id),
        actor_name=LOCAL_EDIT_ACTOR_NAME,
        aggregate_id=task.id,
        payload={"task_id": task.id, "request_hash": task.request_hash},
    )
    session.commit()
    return LocalImageEditSubmitResult(task=task, dispatch=dispatch, created=False)


def recover_local_image_edit_task(
    session: Session,
    *,
    task_id: str,
    reset_stale_running: bool,
    stale_after: timedelta = LOCAL_EDIT_STALE_CLAIM_AFTER,
    now: datetime | None = None,
) -> LocalImageEditRecoveryResult:
    """持有常驻扫描器使用的 local-edit 恢复状态机。"""

    observed_at = now or now_utc()
    task = _lock_task(session, task_id)
    if task.status == LocalImageEditTaskStatus.QUEUED:
        _requeue_dispatch(session, task)
        session.commit()
        return LocalImageEditRecoveryResult(task.id, "queued", task.status)
    if task.status != LocalImageEditTaskStatus.RUNNING:
        session.rollback()
        return LocalImageEditRecoveryResult(task.id, "ignored", task.status)
    if not reset_stale_running or not _is_stale(task.started_at, now=observed_at, stale_after=stale_after):
        session.rollback()
        return LocalImageEditRecoveryResult(task.id, "fresh", task.status)
    if task.progress_phase == "claimed":
        _mark_stale_claimed_attempt(session, task, now=observed_at)
        _requeue_dispatch(session, task)
        session.commit()
        return LocalImageEditRecoveryResult(task.id, "requeued", task.status)
    if task.progress_phase in {"provider_pending", "provider_call", "provider_result_received"}:
        # 已进入 provider 边界：无法证明副作用，不能当 failed 自动重投。
        _mark_task_unknown_locked(
            session,
            task,
            detail="provider boundary 已开始，滞留运行不能自动重投",
            now=observed_at,
        )
        session.commit()
        return LocalImageEditRecoveryResult(task.id, "unknown", task.status)
    _mark_task_unknown_locked(
        session,
        task,
        detail="局部编辑运行阶段无法识别，已停止自动重投",
        now=observed_at,
    )
    session.commit()
    return LocalImageEditRecoveryResult(task.id, "unknown", task.status)


def claim_local_image_edit_task(
    session: Session,
    *,
    task_id: str,
    now: datetime | None = None,
    stale_after: timedelta = LOCAL_EDIT_STALE_CLAIM_AFTER,
) -> LocalImageEditClaim:
    """claim 已排队工作，或安全替换过期的 pre-provider claim。"""

    now = now or now_utc()
    task = _lock_task(session, task_id)
    if task.request_hash is None:
        raise ConflictError("局部编辑任务缺少 submit request hash")
    if task.status == LocalImageEditTaskStatus.RUNNING:
        stale = _is_stale(task.started_at, now=now, stale_after=stale_after)
        if not stale:
            raise ConflictError("局部编辑任务已被其他 worker 领取")
        if task.progress_phase == "claimed":
            _mark_stale_claimed_attempt(session, task, now=now)
        elif task.progress_phase in {"provider_pending", "provider_call", "provider_result_received"}:
            # unknown：无法证明副作用，禁止当 failed claim 重试。
            _mark_task_unknown_locked(
                session,
                task,
                detail="provider boundary 已开始，滞留运行不能自动重投",
                now=now,
            )
            session.commit()
            raise ConflictError("局部编辑 provider effect 未知，任务已停止自动重试")
        else:
            raise ConflictError("局部编辑任务的运行阶段未知，不能自动重投")
    if task.status != LocalImageEditTaskStatus.QUEUED and task.status != LocalImageEditTaskStatus.RUNNING:
        raise ConflictError("局部编辑任务当前状态不能 claim")

    attempt_id = new_id()
    attempt_number = task.attempts + 1
    task.status = LocalImageEditTaskStatus.RUNNING
    task.active_attempt_id = attempt_id
    task.attempts = attempt_number
    task.progress_phase = "claimed"
    task.started_at = now
    task.finished_at = None
    task.failure_reason = None
    task.is_retryable = True
    attempt = LocalImageEditProviderAttempt(
        task_id=task.id,
        attempt_id=attempt_id,
        attempt_number=attempt_number,
        operation_key=f"local-image-edit:{task.id}",
        request_hash=task.request_hash,
        phase="claimed",
        effect_result=PROVIDER_EFFECT_RESULT_PENDING,
        provider_name="pending",
    )
    session.add(attempt)
    session.flush()
    return LocalImageEditClaim(task_id=task.id, attempt_id=attempt_id, attempt_number=attempt_number)


def execute_local_image_edit_task(
    *,
    session_factory: Callable[[], Session],
    task_id: str,
    provider: ImageProvider,
    storage: LocalStorage,
) -> LocalImageEditExecutionResult:
    """执行一条已 claim 的局部编辑；传输依赖可注入。"""

    session = session_factory()
    try:
        try:
            claim = claim_local_image_edit_task(session, task_id=task_id)
        except ConflictError:
            session.rollback()
            current = session.get(LocalImageEditTask, task_id)
            if current is None:
                raise
            if current.status == LocalImageEditTaskStatus.RUNNING or LOCAL_IMAGE_EDIT_TASK_CONTRACT.is_terminal(
                current.status
            ):
                return LocalImageEditExecutionResult(
                    task_id=current.id,
                    attempt_id=current.active_attempt_id,
                    status=current.status,
                    result_asset_id=current.result_asset_id,
                )
            raise
        session.commit()
        try:
            snapshot = _load_execution_snapshot(session, task_id=task_id, attempt_id=claim.attempt_id, storage=storage)
        except (BusinessValidationError, OSError) as exc:
            try:
                _finish_fenced_attempt(
                    session,
                    task_id=task_id,
                    attempt_id=claim.attempt_id,
                    status=LocalImageEditTaskStatus.FAILED,
                    phase="failed",
                    effect_result=PROVIDER_EFFECT_RESULT_FAILED,
                    detail=(str(exc) if isinstance(exc, BusinessValidationError) else "局部编辑媒体读取失败"),
                    retryable=False,
                )
            except ConflictError:
                session.rollback()
            return _execution_result(session, task_id=task_id, attempt_id=claim.attempt_id)
        capability = provider.local_edit_capability
        operation = snapshot["task"].operation
        expected_provider_name = snapshot["task"].requested_provider_name
        expected_local_edit_mode = snapshot["task"].requested_local_edit_mode
        if (
            not expected_provider_name
            or not expected_local_edit_mode
            or capability.provider_name != expected_provider_name
            or capability.mode != expected_local_edit_mode
        ):
            try:
                _finish_fenced_attempt(
                    session,
                    task_id=task_id,
                    attempt_id=claim.attempt_id,
                    status=LocalImageEditTaskStatus.FAILED,
                    phase="failed",
                    effect_result="unsupported",
                    detail="提交时记录的图片 provider 能力与当前绑定不一致",
                    retryable=False,
                    provider_status="capability_mismatch",
                )
            except ConflictError:
                session.rollback()
            return _execution_result(session, task_id=task_id, attempt_id=claim.attempt_id)
        supported_operations = {item.value for item in capability.operations}
        if (
            not capability.supported
            or operation not in supported_operations
            or len(snapshot["references"]) > capability.max_reference_images
        ):
            detail = capability.reason or "图片 provider 未显式支持当前局部编辑操作"
            try:
                _finish_fenced_attempt(
                    session,
                    task_id=task_id,
                    attempt_id=claim.attempt_id,
                    status=LocalImageEditTaskStatus.FAILED,
                    phase="failed",
                    effect_result="unsupported",
                    detail=detail,
                    retryable=False,
                )
            except ConflictError:
                session.rollback()
            return _execution_result(session, task_id=task_id, attempt_id=claim.attempt_id)

        request = _build_provider_request(snapshot)
        _mark_provider_pending(
            session,
            task_id=task_id,
            attempt_id=claim.attempt_id,
            provider_name=capability.provider_name,
            provider_model=None,
            request_json=_audit_request(snapshot, request),
        )
        _mark_provider_call(session, task_id=task_id, attempt_id=claim.attempt_id)
        try:
            result = provider.edit_local(request)
        except UnsupportedLocalEditError as exc:
            try:
                _finish_fenced_attempt(
                    session,
                    task_id=task_id,
                    attempt_id=claim.attempt_id,
                    status=LocalImageEditTaskStatus.FAILED,
                    phase="failed",
                    effect_result="unsupported",
                    detail=str(exc),
                    retryable=False,
                )
            except ConflictError:
                session.rollback()
            return _execution_result(session, task_id=task_id, attempt_id=claim.attempt_id)
        except Exception as exc:  # noqa: BLE001
            try:
                # 请求已发出但无法证明结果，不能记 failed。
                _finish_fenced_attempt(
                    session,
                    task_id=task_id,
                    attempt_id=claim.attempt_id,
                    status=LocalImageEditTaskStatus.UNKNOWN,
                    phase="unknown",
                    effect_result=PROVIDER_EFFECT_RESULT_UNKNOWN,
                    detail="图片 provider 请求结果未知",
                    retryable=False,
                    provider_status=type(exc).__name__[:80],
                )
            except ConflictError:
                session.rollback()
            return _execution_result(session, task_id=task_id, attempt_id=claim.attempt_id)

        return _persist_provider_result(
            session,
            storage=storage,
            task_id=task_id,
            attempt_id=claim.attempt_id,
            snapshot=snapshot,
            result=result,
        )
    finally:
        session.close()


def adopt_local_image_edit_result(
    session: Session,
    *,
    task_id: str,
    expected_current_artifact_id: str,
) -> LocalImageEditAdoptionResult:
    """把成功的 local-edit 结果显式绑成节点当前产物。"""

    task = _lock_task(session, task_id)
    if task.status != LocalImageEditTaskStatus.SUCCEEDED or task.result_asset_id is None:
        raise ConflictError("只有成功且有结果资产的局部编辑任务才能 adoption")
    if task.target_graph_id is None or task.target_node_id is None or task.source_artifact_id is None:
        raise ConflictError("局部编辑任务没有可 adoption 的目标 artifact")
    product = _lock_product(session, task.product_id)
    graph = session.scalar(
        select(WorkflowGraph)
        .where(
            WorkflowGraph.id == task.target_graph_id,
            WorkflowGraph.product_id == product.id,
            WorkflowGraph.active.is_(True),
        )
        .with_for_update()
    )
    if graph is None:
        raise ConflictError("局部编辑目标 graph 已变化或不再 active")
    node = session.scalar(
        select(WorkflowGraphNode)
        .where(
            WorkflowGraphNode.id == task.target_node_id,
            WorkflowGraphNode.graph_id == graph.id,
        )
        .with_for_update()
    )
    if node is None or node.node_type != GraphNodeType.IMAGE_GENERATION:
        raise ConflictError("局部编辑目标节点已变化")
    if node.current_artifact_id != expected_current_artifact_id or node.current_artifact_id != task.source_artifact_id:
        raise ConflictError("节点当前结果已变化，不能 adoption")
    source_artifact = session.get(WorkflowGraphArtifact, task.source_artifact_id)
    result_asset = session.get(ProductImageAsset, task.result_asset_id)
    if source_artifact is None or result_asset is None or result_asset.product_id != product.id:
        raise ConflictError("局部编辑 lineage 记录不完整")
    if (
        task.source_artifact_asset_id is None
        or source_artifact.graph_id != graph.id
        or source_artifact.node_id != node.id
        or source_artifact.product_image_asset_id != task.source_artifact_asset_id
        or task.source_artifact_asset_id != task.source_asset_id
    ):
        raise ConflictError("局部编辑 source artifact 与任务快照不一致")
    payload = _adoption_payload(task, source_artifact=source_artifact, result_asset=result_asset)
    artifact = WorkflowGraphArtifact(
        graph_id=graph.id,
        node_id=node.id,
        node_run_id=None,
        artifact_type=GraphArtifactType.IMAGE,
        schema_version=3,
        graph_revision=graph.revision,
        payload_json=payload,
        payload_hash=_graph_payload_hash(payload),
        input_digest=source_artifact.input_digest,
        product_image_asset_id=result_asset.id,
        provider_name=task.provider_name or "local_edit",
        provider_model=task.provider_model,
    )
    session.add(artifact)
    session.flush()
    event = LocalImageEditAdoptionEvent(
        product_id=product.id,
        task_id=task.id,
        graph_id=graph.id,
        node_id=node.id,
        event_type="adopt",
        from_artifact_id=source_artifact.id,
        to_artifact_id=artifact.id,
    )
    session.add(event)
    # 节点 current 指向新的商品图片 id；源图与历史产物仍保留。
    node.current_artifact_id = artifact.id
    session.commit()
    return LocalImageEditAdoptionResult(task_id=task.id, event=event, artifact=artifact)


def cancel_local_image_edit_task(
    session: Session,
    *,
    task_id: str,
    expected_revision: int | None = None,
) -> LocalImageEditTask:
    """取消工作，且不允许迟到的 provider 响应变成结果。"""

    task = _lock_task(session, task_id)
    if expected_revision is not None and task.revision != expected_revision:
        raise ConflictError("局部编辑任务已变化，请刷新后重试")
    if task.status in {
        LocalImageEditTaskStatus.SUCCEEDED,
        LocalImageEditTaskStatus.FAILED,
        LocalImageEditTaskStatus.UNKNOWN,
    }:
        raise ConflictError("已完成的局部编辑任务不能取消")
    if task.status == LocalImageEditTaskStatus.CANCELLED:
        return task
    now = now_utc()
    if task.active_attempt_id is not None:
        attempt = session.scalar(
            select(LocalImageEditProviderAttempt)
            .where(
                LocalImageEditProviderAttempt.task_id == task.id,
                LocalImageEditProviderAttempt.attempt_id == task.active_attempt_id,
            )
            .with_for_update()
        )
        if attempt is not None:
            attempt_phase = "unknown" if task.progress_phase != "claimed" else "failed"
            attempt_result = (
                PROVIDER_EFFECT_RESULT_UNKNOWN
                if attempt_phase == "unknown"
                else PROVIDER_EFFECT_RESULT_FAILED
            )
            transition_provider_effect_result(attempt, attempt_result)
            attempt.phase = attempt_phase
            attempt.detail = "局部编辑任务已取消；provider boundary 之后的结果只能作为审计"
    task.status = LocalImageEditTaskStatus.CANCELLED
    task.active_attempt_id = None
    task.progress_phase = "cancelled"
    task.failure_reason = "用户取消局部编辑任务"
    task.is_retryable = False
    task.finished_at = now
    session.commit()
    return task


def revert_local_image_edit_adoption(
    session: Session,
    *,
    adoption_event_id: str,
    expected_current_artifact_id: str,
    task_id: str | None = None,
) -> LocalImageEditRevertResult:
    """仅在已采纳产物仍为当前时追加 revert 事件。"""

    event = session.scalar(
        select(LocalImageEditAdoptionEvent)
        .where(LocalImageEditAdoptionEvent.id == adoption_event_id)
        .where(
            LocalImageEditAdoptionEvent.task_id == task_id
            if task_id is not None
            else True
        )
        .with_for_update()
    )
    if event is None or event.event_type != "adopt":
        raise NotFoundError("局部编辑 adoption 事件不存在")
    product = _lock_product(session, event.product_id)
    graph = session.scalar(
        select(WorkflowGraph)
        .where(
            WorkflowGraph.id == event.graph_id,
            WorkflowGraph.product_id == product.id,
            WorkflowGraph.active.is_(True),
        )
        .with_for_update()
    )
    if graph is None:
        raise ConflictError("adoption 所属 graph 已变化或不再 active")
    node = session.scalar(
        select(WorkflowGraphNode)
        .where(WorkflowGraphNode.id == event.node_id, WorkflowGraphNode.graph_id == graph.id)
        .with_for_update()
    )
    if (
        node is None
        or node.current_artifact_id != expected_current_artifact_id
        or node.current_artifact_id != event.to_artifact_id
    ):
        raise ConflictError("节点当前结果已变化，不能 revert")
    revert_event = LocalImageEditAdoptionEvent(
        product_id=event.product_id,
        task_id=event.task_id,
        graph_id=event.graph_id,
        node_id=event.node_id,
        event_type="revert",
        from_artifact_id=event.to_artifact_id,
        to_artifact_id=event.from_artifact_id,
        related_event_id=event.id,
    )
    session.add(revert_event)
    node.current_artifact_id = event.from_artifact_id
    session.commit()
    return LocalImageEditRevertResult(
        task_id=event.task_id,
        event=revert_event,
        current_artifact_id=event.from_artifact_id,
    )


def _lock_product(session: Session, product_id: str) -> Product:
    product = session.scalar(select(Product).where(Product.id == product_id).with_for_update())
    if product is None:
        raise NotFoundError("商品不存在")
    return product


def _lock_task(session: Session, task_id: str) -> LocalImageEditTask:
    task = session.scalar(
        select(LocalImageEditTask)
        .options(selectinload(LocalImageEditTask.references))
        .where(LocalImageEditTask.id == task_id)
        .execution_options(populate_existing=True)
        .with_for_update()
    )
    if task is None:
        raise NotFoundError("局部编辑任务不存在")
    return task


def _task_projection_query():
    return select(LocalImageEditTask).options(
        selectinload(LocalImageEditTask.source_asset).selectinload(ProductImageAsset.media_object),
        selectinload(LocalImageEditTask.result_asset).selectinload(ProductImageAsset.media_object),
        selectinload(LocalImageEditTask.references)
        .selectinload(LocalImageEditTaskReference.asset)
        .selectinload(ProductImageAsset.media_object),
    )


def _attach_bounded_audit_projection(session: Session, task: LocalImageEditTask) -> None:
    attempts = list(
        session.scalars(
            select(LocalImageEditProviderAttempt)
            .options(
                selectinload(LocalImageEditProviderAttempt.late_result_asset).selectinload(
                    ProductImageAsset.media_object
                )
            )
            .where(LocalImageEditProviderAttempt.task_id == task.id)
            .order_by(
                LocalImageEditProviderAttempt.attempt_number.desc(),
                LocalImageEditProviderAttempt.id.desc(),
            )
            .limit(LOCAL_EDIT_AUDIT_LIMIT)
        ).all()
    )
    events = list(
        session.scalars(
            select(LocalImageEditAdoptionEvent)
            .where(LocalImageEditAdoptionEvent.task_id == task.id)
            .order_by(LocalImageEditAdoptionEvent.created_at.desc(), LocalImageEditAdoptionEvent.id.desc())
            .limit(LOCAL_EDIT_AUDIT_LIMIT)
        ).all()
    )
    task._local_image_edit_provider_attempts_projection = attempts
    task._local_image_edit_adoption_events_projection = events


def _lock_source_asset(session: Session, *, product_id: str, asset_id: str) -> ProductImageAsset:
    asset = session.scalar(
        select(ProductImageAsset)
        .options(selectinload(ProductImageAsset.media_object))
        .where(ProductImageAsset.id == asset_id, ProductImageAsset.product_id == product_id)
        .with_for_update()
    )
    if asset is None:
        raise NotFoundError("商品图片不存在")
    media = asset.media_object
    if media is None or media.verification_status != MediaVerificationStatus.VERIFIED:
        raise BusinessValidationError("局部编辑源图片必须是已核验图片")
    if not media.sha256 or not media.width or not media.height:
        raise BusinessValidationError("局部编辑源图片缺少不可变媒体快照")
    return asset


def _validate_target_node(
    session: Session,
    *,
    product_id: str,
    source_asset: ProductImageAsset,
    target_node_id: str | None,
) -> dict[str, Any]:
    if target_node_id is None:
        return {
            "graph_id": None,
            "node_id": None,
            "graph_revision": None,
            "artifact_id": None,
            "artifact_asset_id": None,
            "artifact_input_digest": None,
        }
    node = session.scalar(
        select(WorkflowGraphNode)
        .join(WorkflowGraph, WorkflowGraph.id == WorkflowGraphNode.graph_id)
        .options(selectinload(WorkflowGraphNode.current_artifact))
        .where(
            WorkflowGraphNode.id == target_node_id,
            WorkflowGraph.product_id == product_id,
            WorkflowGraph.active.is_(True),
        )
        .with_for_update()
    )
    if node is None or node.node_type != GraphNodeType.IMAGE_GENERATION:
        raise ConflictError("局部编辑 target 必须是当前商品 active graph 的 image_generation 节点")
    artifact = node.current_artifact
    if artifact is None or artifact.artifact_type != GraphArtifactType.IMAGE:
        raise ConflictError("局部编辑 target 必须有当前 image artifact")
    if artifact.product_image_asset_id != source_asset.id:
        raise ConflictError("局部编辑 source asset 必须等于 target 当前 artifact asset")
    if not artifact.input_digest or len(artifact.input_digest) != 64:
        raise ConflictError("局部编辑 target 当前 artifact 缺少 input digest")
    return {
        "graph_id": node.graph_id,
        "node_id": node.id,
        "graph_revision": _graph_revision(session, node.graph_id),
        "artifact_id": artifact.id,
        "artifact_asset_id": artifact.product_image_asset_id,
        "artifact_input_digest": artifact.input_digest,
    }


def _graph_revision(session: Session, graph_id: str) -> int:
    revision = session.scalar(select(WorkflowGraph.revision).where(WorkflowGraph.id == graph_id))
    if revision is None:
        raise ConflictError("局部编辑 target graph 不存在")
    return revision


def _lock_reference_assets(
    session: Session,
    *,
    product_id: str,
    reference_asset_ids: Sequence[str],
) -> list[ProductImageAsset]:
    normalized_ids = tuple(reference_asset_ids)
    if len(normalized_ids) > 6:
        raise BusinessValidationError("局部编辑参考图不能超过 6 张")
    if len(set(normalized_ids)) != len(normalized_ids):
        raise BusinessValidationError("局部编辑参考图不能重复")
    if not normalized_ids:
        return []
    assets = list(
        session.scalars(
            select(ProductImageAsset)
            .options(selectinload(ProductImageAsset.media_object))
            .where(ProductImageAsset.product_id == product_id, ProductImageAsset.id.in_(normalized_ids))
            .order_by(ProductImageAsset.id)
            .with_for_update()
        )
    )
    if {asset.id for asset in assets} != set(normalized_ids):
        raise NotFoundError("局部编辑参考图不存在")
    for asset in assets:
        media = asset.media_object
        if (
            media is None
            or media.verification_status != MediaVerificationStatus.VERIFIED
            or not media.sha256
            or len(media.sha256) != 64
            or not media.byte_size
            or media.byte_size <= 0
            or not media.width
            or media.width <= 0
            or not media.height
            or media.height <= 0
        ):
            raise BusinessValidationError("局部编辑参考图必须是有完整核验元数据的图片")
    by_id = {asset.id: asset for asset in assets}
    return [by_id[asset_id] for asset_id in normalized_ids]


def _normalize_mask(
    source_asset: ProductImageAsset,
    *,
    mask_png_bytes: bytes,
    geometry: LocalEditMaskGeometry,
):
    media = source_asset.media_object
    if media.width is None or media.height is None:
        raise BusinessValidationError("局部编辑源图片缺少尺寸")
    try:
        return validate_and_normalize_local_edit_mask(
            source_width=media.width,
            source_height=media.height,
            mask_png_bytes=mask_png_bytes,
            geometry=geometry,
        )
    except ValueError as exc:
        raise BusinessValidationError(str(exc)) from exc


def _stage_mask_media(
    session: Session,
    *,
    storage: LocalStorage,
    storage_writes: StorageWriteCompensation,
    task_id: str,
    normalized_mask: Any,
) -> MediaObject:
    media = stage_verified_media_object(
        session,
        content=normalized_mask.bytes_data,
        filename=f"{task_id}-{LOCAL_EDIT_MASK_FILENAME}",
        expected_mime_type="image/png",
        storage=storage,
        storage_writes=storage_writes,
    )
    return media


def _replace_task_references(
    session: Session,
    *,
    task_id: str,
    references: Sequence[ProductImageAsset],
) -> None:
    session.execute(
        delete(LocalImageEditTaskReference).where(LocalImageEditTaskReference.task_id == task_id)
    )
    session.flush()
    session.add_all(
        LocalImageEditTaskReference(task_id=task_id, asset_id=asset.id, sort_order=index)
        for index, asset in enumerate(references)
    )
    session.flush()


def _local_edit_request_hash(
    session: Session,
    task: LocalImageEditTask,
    *,
    requested_provider_name: str | None = None,
    requested_local_edit_mode: str | None = None,
) -> str:
    mask = session.get(MediaObject, task.mask_media_object_id)
    if mask is None or not mask.sha256:
        raise ConflictError("局部编辑 mask 媒体快照不存在")
    reference_ids = [
        reference.asset_id
        for reference in sorted(task.references, key=lambda item: (item.sort_order, item.asset_id))
    ]
    provider_name = requested_provider_name or task.requested_provider_name
    local_edit_mode = requested_local_edit_mode or task.requested_local_edit_mode
    if not provider_name or not local_edit_mode:
        raise ConflictError("局部编辑任务缺少提交时的 provider 能力快照")
    return canonical_provider_effect_json_hash(
        {
            "task_revision": task.revision,
            "source_asset_id": task.source_asset_id,
            "source_media_sha256": task.source_media_sha256,
            "operation": task.operation,
            "instruction": task.instruction,
            "source_text": task.source_text,
            "replacement_text": task.replacement_text,
            "mask_media_sha256": mask.sha256,
            "mask_geometry": task.mask_geometry_json,
            "reference_asset_ids": reference_ids,
            "provider_intent": {
                "provider_name": provider_name,
                "local_edit_mode": local_edit_mode,
            },
            "target": {
                "graph_id": task.target_graph_id,
                "node_id": task.target_node_id,
                "graph_revision": task.target_graph_revision,
                "source_artifact_id": task.source_artifact_id,
                "source_artifact_asset_id": task.source_artifact_asset_id,
                "source_artifact_input_digest": task.source_artifact_input_digest,
            },
        },
        "局部编辑 provider effect request",
    )


def _normalize_provider_intent_value(value: str, label: str) -> str:
    normalized = value.strip()
    if not normalized or len(normalized) > 80:
        raise BusinessValidationError(f"局部编辑 {label} 无效")
    return normalized


def _get_or_stage_dispatch(session: Session, task: LocalImageEditTask) -> AsyncDispatch:
    dispatch = session.scalar(
        select(AsyncDispatch).where(
            AsyncDispatch.delivery_key == delivery_key_for_actor(LOCAL_EDIT_ACTOR_NAME, task.id)
        )
    )
    if dispatch is not None:
        return dispatch
    return stage_async_dispatch(
        session,
        delivery_key=delivery_key_for_actor(LOCAL_EDIT_ACTOR_NAME, task.id),
        actor_name=LOCAL_EDIT_ACTOR_NAME,
        aggregate_id=task.id,
        payload={"task_id": task.id, "request_hash": task.request_hash},
    )


def _requeue_dispatch(session: Session, task: LocalImageEditTask) -> AsyncDispatch:
    return requeue_async_dispatch(
        session,
        delivery_key=delivery_key_for_actor(LOCAL_EDIT_ACTOR_NAME, task.id),
        actor_name=LOCAL_EDIT_ACTOR_NAME,
        aggregate_id=task.id,
        payload={"task_id": task.id, "request_hash": task.request_hash},
    )


def _normalize_idempotency_key(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("局部编辑 idempotency key 不能为空")
    if len(normalized) > 120:
        raise BusinessValidationError("局部编辑 idempotency key 不能超过 120 个字符")
    return normalized


def _is_stale(started_at: datetime | None, *, now: datetime, stale_after: timedelta) -> bool:
    if started_at is None:
        return True
    if started_at.tzinfo is None:
        started_at = started_at.replace(tzinfo=UTC)
    return now - started_at >= stale_after


def _mark_stale_claimed_attempt(session: Session, task: LocalImageEditTask, *, now: datetime) -> None:
    if task.active_attempt_id is not None:
        attempt = session.scalar(
            select(LocalImageEditProviderAttempt)
            .where(
                LocalImageEditProviderAttempt.task_id == task.id,
                LocalImageEditProviderAttempt.attempt_id == task.active_attempt_id,
            )
            .with_for_update()
        )
        if attempt is not None:
            if not transition_provider_effect_result(attempt, PROVIDER_EFFECT_RESULT_FAILED):
                raise ConflictError("局部编辑 provider effect 已知晓，不能安全重入队")
            attempt.phase = "failed"
            attempt.detail = "worker claim 已过期，provider boundary 尚未开始"
    task.active_attempt_id = None
    task.progress_phase = None
    task.started_at = None
    task.updated_at = now
    task.status = LocalImageEditTaskStatus.QUEUED


def _mark_task_unknown_locked(
    session: Session,
    task: LocalImageEditTask,
    *,
    detail: str,
    now: datetime,
) -> None:
    if task.active_attempt_id is not None:
        attempt = session.scalar(
            select(LocalImageEditProviderAttempt)
            .where(
                LocalImageEditProviderAttempt.task_id == task.id,
                LocalImageEditProviderAttempt.attempt_id == task.active_attempt_id,
            )
            .with_for_update()
        )
        if attempt is not None:
            transition_provider_effect_result(attempt, PROVIDER_EFFECT_RESULT_UNKNOWN)
            attempt.phase = "unknown"
            attempt.detail = detail
    task.status = LocalImageEditTaskStatus.UNKNOWN
    task.active_attempt_id = None
    task.progress_phase = "unknown_provider_effect"
    task.failure_reason = detail
    task.is_retryable = False
    task.finished_at = now


def _load_execution_snapshot(
    session: Session,
    *,
    task_id: str,
    attempt_id: str,
    storage: LocalStorage,
) -> dict[str, Any]:
    task = session.scalar(
        select(LocalImageEditTask)
        .options(
            selectinload(LocalImageEditTask.source_asset).selectinload(ProductImageAsset.media_object),
            selectinload(LocalImageEditTask.mask_media_object),
            selectinload(LocalImageEditTask.references).selectinload(LocalImageEditTaskReference.asset).selectinload(
                ProductImageAsset.media_object
            ),
        )
        .where(LocalImageEditTask.id == task_id)
    )
    if task is None:
        raise NotFoundError("局部编辑任务不存在")
    if task.status != LocalImageEditTaskStatus.RUNNING or task.active_attempt_id != attempt_id:
        raise ConflictError("局部编辑 attempt 已失效")
    source_media = task.source_asset.media_object
    mask_media = task.mask_media_object
    if source_media is None or mask_media is None:
        raise BusinessValidationError("局部编辑源图或 mask 媒体不存在")
    source_bytes = storage.resolve(source_media.storage_path).read_bytes()
    mask_bytes = storage.resolve(mask_media.storage_path).read_bytes()
    if hashlib.sha256(source_bytes).hexdigest() != task.source_media_sha256:
        raise BusinessValidationError("局部编辑源图版本已变化")
    if not mask_media.sha256 or hashlib.sha256(mask_bytes).hexdigest() != mask_media.sha256:
        raise BusinessValidationError("局部编辑 mask 版本已变化")
    inspect_image_bytes(source_bytes, expected_mime_type=source_media.mime_type)
    inspect_image_bytes(mask_bytes, expected_mime_type="image/png")
    if source_media.verification_status != MediaVerificationStatus.VERIFIED:
        raise BusinessValidationError("局部编辑源图未通过媒体核验")
    reference_bytes: dict[str, bytes] = {}
    for reference in task.references:
        media = reference.asset.media_object
        if media is None or media.verification_status != MediaVerificationStatus.VERIFIED or not media.sha256:
            raise BusinessValidationError("局部编辑参考图媒体不存在")
        bytes_data = storage.resolve(media.storage_path).read_bytes()
        if len(bytes_data) != media.byte_size or hashlib.sha256(bytes_data).hexdigest() != media.sha256:
            raise BusinessValidationError("局部编辑参考图版本已变化")
        inspect_image_bytes(bytes_data, expected_mime_type=media.mime_type)
        reference_bytes[reference.asset_id] = bytes_data
    return {
        "task": task,
        "source_media": source_media,
        "source_bytes": source_bytes,
        "mask_media": mask_media,
        "mask_bytes": mask_bytes,
        "references": list(task.references),
        "reference_bytes": reference_bytes,
        "storage": storage,
    }


def _build_provider_request(snapshot: dict[str, Any]) -> LocalEditRequest:
    task: LocalImageEditTask = snapshot["task"]
    source_media: MediaObject = snapshot["source_media"]
    references: list[LocalImageEditTaskReference] = snapshot["references"]
    source_width = source_media.width or 0
    source_height = source_media.height or 0
    return LocalEditRequest(
        source_image=LocalEditImage(
            bytes_data=snapshot["source_bytes"],
            mime_type=source_media.mime_type,
            filename=task.source_asset.original_filename,
        ),
        instruction=_provider_instruction(task),
        operation=task.operation,
        mask=LocalEditMask(bytes_data=snapshot["mask_bytes"], mime_type="image/png"),
        reference_images=tuple(
            LocalEditImage(
                bytes_data=snapshot["reference_bytes"][reference.asset_id],
                mime_type=reference.asset.media_object.mime_type,
                filename=reference.asset.original_filename,
            )
            for reference in references
        ),
        size=f"{source_width}x{source_height}",
    )


def _provider_instruction(task: LocalImageEditTask) -> str:
    if task.operation == "replace_text":
        context = f"；补充要求：{task.instruction}" if task.instruction else ""
        return (
            f"将图中原文字“{task.source_text}”替换为“{task.replacement_text}”，"
            f"只修改 mask 指定区域，不改变其他内容{context}。"
        )
    return task.instruction or ""


def _audit_request(snapshot: dict[str, Any], request: LocalEditRequest) -> dict[str, Any]:
    task: LocalImageEditTask = snapshot["task"]
    source_media: MediaObject = snapshot["source_media"]
    mask_media: MediaObject = snapshot["mask_media"]
    return {
        "operation": task.operation,
        "provider_intent": {
            "provider_name": task.requested_provider_name,
            "local_edit_mode": task.requested_local_edit_mode,
        },
        "size": request.size,
        "source": {
            "mime_type": source_media.mime_type,
            "width": source_media.width,
            "height": source_media.height,
            "sha256": task.source_media_sha256,
        },
        "mask": {
            "mime_type": mask_media.mime_type,
            "width": mask_media.width,
            "height": mask_media.height,
            "sha256": mask_media.sha256,
        },
        "reference_asset_ids": [reference.asset_id for reference in snapshot["references"]],
    }


def _mark_provider_pending(
    session: Session,
    *,
    task_id: str,
    attempt_id: str,
    provider_name: str,
    provider_model: str | None,
    request_json: dict[str, Any],
) -> None:
    task, attempt = _lock_fenced_attempt(session, task_id=task_id, attempt_id=attempt_id)
    task.progress_phase = "provider_pending"
    task.provider_name = provider_name
    task.provider_model = provider_model
    attempt.provider_name = provider_name
    attempt.provider_model = provider_model
    attempt.phase = "provider_pending"
    attempt.request_json = request_json
    session.commit()


def _mark_provider_call(session: Session, *, task_id: str, attempt_id: str) -> None:
    task, attempt = _lock_fenced_attempt(session, task_id=task_id, attempt_id=attempt_id)
    task.progress_phase = "provider_call"
    attempt.phase = "provider_call"
    session.commit()


def _lock_fenced_attempt(
    session: Session,
    *,
    task_id: str,
    attempt_id: str,
) -> tuple[LocalImageEditTask, LocalImageEditProviderAttempt]:
    task = session.scalar(
        select(LocalImageEditTask)
        .where(
            LocalImageEditTask.id == task_id,
            LocalImageEditTask.status == LocalImageEditTaskStatus.RUNNING,
            LocalImageEditTask.active_attempt_id == attempt_id,
        )
        .with_for_update()
    )
    if task is None:
        raise ConflictError("局部编辑 attempt 已失效")
    attempt = session.scalar(
        select(LocalImageEditProviderAttempt)
        .where(
            LocalImageEditProviderAttempt.task_id == task_id,
            LocalImageEditProviderAttempt.attempt_id == attempt_id,
        )
        .with_for_update()
    )
    if attempt is None:
        raise ConflictError("局部编辑 attempt 审计记录不存在")
    return task, attempt


def _finish_fenced_attempt(
    session: Session,
    *,
    task_id: str,
    attempt_id: str,
    status: LocalImageEditTaskStatus,
    phase: str,
    effect_result: str,
    detail: str,
    retryable: bool,
    provider_status: str | None = None,
    provider_response_id: str | None = None,
) -> None:
    task, attempt = _lock_fenced_attempt(session, task_id=task_id, attempt_id=attempt_id)
    now = now_utc()
    if effect_result in PROVIDER_EFFECT_RESULTS and not transition_provider_effect_result(
        attempt, effect_result
    ):
        raise ConflictError("局部编辑 provider effect 状态不能被当前结果覆盖")
    task.status = status
    task.active_attempt_id = None
    task.progress_phase = phase
    task.failure_reason = detail if status != LocalImageEditTaskStatus.SUCCEEDED else None
    task.is_retryable = retryable
    task.finished_at = now
    task.provider_status = provider_status
    task.provider_response_id = provider_response_id
    attempt.phase = phase
    if effect_result not in PROVIDER_EFFECT_RESULTS:
        attempt.effect_result = effect_result
    attempt.detail = detail
    attempt.provider_status = provider_status
    attempt.provider_response_id = provider_response_id
    session.commit()


def _persist_provider_result(
    session: Session,
    *,
    storage: LocalStorage,
    task_id: str,
    attempt_id: str,
    snapshot: dict[str, Any],
    result: LocalEditResult,
) -> LocalImageEditExecutionResult:
    if len(result.images) != 1:
        try:
            _finish_fenced_attempt(
                session,
                task_id=task_id,
                attempt_id=attempt_id,
                status=LocalImageEditTaskStatus.UNKNOWN,
                phase="unknown",
                effect_result=PROVIDER_EFFECT_RESULT_UNKNOWN,
                detail="图片 provider 返回的局部编辑结果数量无法确认",
                retryable=False,
                provider_status=result.provider_status,
                provider_response_id=result.provider_response_id,
            )
        except ConflictError:
            session.rollback()
            _record_late_provider_result(
                session,
                task_id=task_id,
                attempt_id=attempt_id,
                result=result,
            )
        return _execution_result(session, task_id=task_id, attempt_id=attempt_id)
    output = result.images[0]
    try:
        with compensate_storage_writes(session) as storage_writes:
            task, attempt = _lock_fenced_attempt(session, task_id=task_id, attempt_id=attempt_id)
            product = _lock_product(session, task.product_id)
            source_asset = _lock_source_asset(session, product_id=product.id, asset_id=task.source_asset_id)
            if source_asset.media_object.sha256 != task.source_media_sha256:
                raise BusinessValidationError("provider 返回前源图版本已变化")
            # 结果进入商品库并 parent 到源图；不替换源 ProductImageAsset。
            asset = stage_product_image_asset(
                session,
                product=product,
                content=output.bytes_data,
                filename=f"local-edit-{task.id}{infer_extension(output.mime_type)}",
                expected_mime_type=output.mime_type,
                display_name=f"局部编辑：{source_asset.display_name}",
                origin_type=ProductImageOriginType.LOCAL_EDIT,
                storage=storage,
                storage_writes=storage_writes,
                parent_asset_id=source_asset.id,
                image_type_key=source_asset.image_type_key,
            )
            session.flush()
            if not transition_provider_effect_result(attempt, PROVIDER_EFFECT_RESULT_APPLIED):
                raise ConflictError("局部编辑 provider effect 已未知，不能采用结果")
            if attempt.effect_result != PROVIDER_EFFECT_RESULT_APPLIED:
                raise ConflictError("局部编辑 provider effect 未处于可采用状态")
            task.status = LocalImageEditTaskStatus.SUCCEEDED
            task.active_attempt_id = None
            task.progress_phase = "provider_result_received"
            task.failure_reason = None
            task.is_retryable = False
            task.finished_at = now_utc()
            task.result_asset_id = asset.id
            task.provider_name = attempt.provider_name
            task.provider_model = result.model
            task.provider_response_id = result.provider_response_id
            task.provider_status = result.provider_status
            attempt.phase = "succeeded"
            attempt.provider_name = task.provider_name or attempt.provider_name
            attempt.provider_model = result.model
            attempt.provider_response_id = result.provider_response_id
            attempt.provider_status = result.provider_status
            attempt.effective_parameters_json = _sanitize_audit_json(result.effective_parameters)
            attempt.result_json = _sanitize_audit_json(result.provider_output_json or {})
            session.commit()
    except ConflictError:
        session.rollback()
        _record_late_provider_result(
            session,
            task_id=task_id,
            attempt_id=attempt_id,
            result=result,
        )
        return _execution_result(session, task_id=task_id, attempt_id=attempt_id)
    except Exception:
        session.rollback()
        _mark_unknown_after_save_failure(
            session,
            task_id=task_id,
            attempt_id=attempt_id,
            detail="provider 结果已返回，但结果资产保存状态无法确认",
        )
        return _execution_result(session, task_id=task_id, attempt_id=attempt_id)
    return _execution_result(session, task_id=task_id, attempt_id=attempt_id)


def _mark_unknown_after_save_failure(
    session: Session,
    *,
    task_id: str,
    attempt_id: str,
    detail: str,
) -> None:
    """provider 已返回但资产保存无法确认：unknown，不是 failed。"""

    try:
        task, attempt = _lock_fenced_attempt(session, task_id=task_id, attempt_id=attempt_id)
    except ConflictError:
        return
    now = now_utc()
    task.status = LocalImageEditTaskStatus.UNKNOWN
    task.active_attempt_id = None
    task.progress_phase = "unknown_provider_effect"
    task.failure_reason = detail
    task.is_retryable = False
    task.finished_at = now
    transition_provider_effect_result(attempt, PROVIDER_EFFECT_RESULT_UNKNOWN)
    attempt.phase = "unknown"
    attempt.detail = detail
    session.commit()


def _record_late_provider_result(
    session: Session,
    *,
    task_id: str,
    attempt_id: str,
    result: LocalEditResult,
) -> None:
    """fencing 变更后记录哈希，不插入素材库资产。"""

    attempt = session.scalar(
        select(LocalImageEditProviderAttempt)
        .where(
            LocalImageEditProviderAttempt.task_id == task_id,
            LocalImageEditProviderAttempt.attempt_id == attempt_id,
        )
        .with_for_update()
    )
    if (
        attempt is None
        or attempt.phase in {"succeeded", "provider_result_received"}
        or attempt.late_result_asset_id is not None
    ):
        return
    task = session.get(LocalImageEditTask, task_id)
    if task is None:
        return
    late_audit: dict[str, Any] = dict(result.provider_output_json or {})
    if len(result.images) == 1:
        output = result.images[0]
        late_audit["late_image_sha256"] = hashlib.sha256(output.bytes_data).hexdigest()
        late_audit["late_image_byte_size"] = len(output.bytes_data)
        late_audit["late_image_mime_type"] = output.mime_type
    try:
        attempt.phase = "provider_result_received"
        transition_provider_effect_result(
            attempt,
            PROVIDER_EFFECT_RESULT_UNKNOWN,
            force_unknown=True,
        )
        attempt.provider_model = result.model
        attempt.provider_response_id = result.provider_response_id
        attempt.provider_status = result.provider_status
        attempt.effective_parameters_json = _sanitize_audit_json(result.effective_parameters)
        attempt.result_json = _sanitize_audit_json(late_audit)
        attempt.late_result_asset_id = None
        attempt.detail = "provider 已返回，但任务 fencing 已失效；结果未采用"
        session.commit()
    except Exception:  # noqa: BLE001
        session.rollback()
        attempt = session.scalar(
            select(LocalImageEditProviderAttempt)
            .where(
                LocalImageEditProviderAttempt.task_id == task_id,
                LocalImageEditProviderAttempt.attempt_id == attempt_id,
            )
            .with_for_update()
        )
        if attempt is None:
            return
        attempt.phase = "provider_result_received"
        transition_provider_effect_result(
            attempt,
            PROVIDER_EFFECT_RESULT_UNKNOWN,
            force_unknown=True,
        )
        attempt.provider_model = result.model
        attempt.provider_response_id = result.provider_response_id
        attempt.provider_status = result.provider_status
        attempt.effective_parameters_json = _sanitize_audit_json(result.effective_parameters)
        attempt.result_json = _sanitize_audit_json(result.provider_output_json or {})
        attempt.detail = "provider 已返回，但晚到结果审计保存失败；结果未采用"
        session.commit()


def _execution_result(session: Session, *, task_id: str, attempt_id: str) -> LocalImageEditExecutionResult:
    task = session.get(LocalImageEditTask, task_id)
    if task is None:
        raise NotFoundError("局部编辑任务不存在")
    return LocalImageEditExecutionResult(
        task_id=task.id,
        attempt_id=attempt_id,
        status=task.status,
        result_asset_id=task.result_asset_id,
    )


def _adoption_payload(
    task: LocalImageEditTask,
    *,
    source_artifact: WorkflowGraphArtifact,
    result_asset: ProductImageAsset,
) -> dict[str, Any]:
    return {
        "kind": "local_image_edit",
        "task_id": task.id,
        "source_asset_id": task.source_asset_id,
        "source_artifact_id": source_artifact.id,
        "source_artifact_input_digest": source_artifact.input_digest,
        "source_graph_revision": source_artifact.graph_revision,
        "result_asset_id": result_asset.id,
        "operation": task.operation,
        "mask_media_object_id": task.mask_media_object_id,
        "task_revision": task.revision,
    }


def _sanitize_audit_json(value: Any) -> Any:
    if isinstance(value, bytes):
        return "<redacted-bytes>"
    if isinstance(value, dict):
        sanitized: dict[str, Any] = {}
        for key, item in value.items():
            normalized_key = str(key).lower()
            if normalized_key in {
                "api_key",
                "authorization",
                "access_token",
                "secret",
                "token",
                "b64_json",
                "base64",
                "bytes_data",
            }:
                continue
            sanitized[str(key)] = _sanitize_audit_json(item)
        return sanitized
    if isinstance(value, (list, tuple)):
        return [_sanitize_audit_json(item) for item in value]
    if isinstance(value, str) and value.startswith("data:"):
        return "<redacted-data-url>"
    return value


def _graph_payload_hash(value: dict[str, Any]) -> str:
    return hashlib.sha256(json.dumps(value, ensure_ascii=False, sort_keys=True).encode("utf-8")).hexdigest()


def _cleanup_media_paths(storage: LocalStorage, cleanup_paths: Sequence[tuple[str, str]]) -> None:
    for media_id, storage_path in cleanup_paths:
        best_effort_storage_delete(
            lambda path=storage_path: storage.delete_image_with_variants(path),
            target=f"local_edit_mask_media_id={media_id} path={storage_path}",
        )


__all__ = [
    "LOCAL_EDIT_ACTOR_NAME",
    "LOCAL_EDIT_STALE_CLAIM_AFTER",
    "LocalImageEditAdoptionResult",
    "LocalImageEditClaim",
    "LocalImageEditExecutionResult",
    "LocalImageEditRecoveryResult",
    "LocalImageEditRevertResult",
    "LocalImageEditSubmitResult",
    "adopt_local_image_edit_result",
    "claim_local_image_edit_task",
    "cancel_local_image_edit_task",
    "create_local_image_edit_task",
    "execute_local_image_edit_task",
    "get_local_image_edit_task",
    "list_local_image_edit_tasks",
    "recover_local_image_edit_task",
    "revert_local_image_edit_adoption",
    "retry_local_image_edit_task",
    "submit_local_image_edit_task",
    "update_local_image_edit_task",
]
