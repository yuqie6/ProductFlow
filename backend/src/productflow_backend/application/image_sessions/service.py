"""连续生图会话：参考图、生成结果与附加到商品。

上传和生成结果使用 MediaObject。附加到商品创建 ProductImageAsset 并共享 bytes。
provider 副作用无法证明时记 unknown，不能当成 failed 重放。
"""

from __future__ import annotations

import logging
from base64 import b64encode
from collections.abc import Callable
from dataclasses import dataclass
from datetime import datetime
from typing import Any, cast

from dramatiq.middleware.time_limit import TimeLimitExceeded
from sqlalchemy import desc, func, select, update
from sqlalchemy.engine import CursorResult
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.admission import (
    ensure_generation_capacity,
    generation_running_capacity_available,
    get_generation_queue_overview,
    get_generation_task_queue_metadata,
    get_queued_generation_positions,
)
from productflow_backend.application.async_delivery import (
    delivery_key_for_actor,
    enqueue_async_dispatch_for_actor,
    requeue_async_dispatch,
    stage_async_dispatch,
)
from productflow_backend.application.image_sessions.dependencies import (
    ImageChatProviderFactory,
    default_image_session_chat_service_factory,
)
from productflow_backend.application.image_sessions.generation import (
    normalize_image_generation_tool_options,
    provider_output_with_actual_image_size,
    unique_image_generation_ids,
)
from productflow_backend.application.image_sessions.provider_effects import (
    ensure_image_session_provider_effect_intent,
    image_session_provider_effect_operation_key,
    image_session_provider_effect_request_hash,
    mark_image_session_provider_effect_failed,
    mark_image_session_provider_effect_unknown,
    record_image_session_provider_effect_progress,
    record_image_session_provider_effect_result,
)
from productflow_backend.application.media_objects import (
    prune_unreferenced_media_objects,
    stage_verified_media_object,
)
from productflow_backend.application.product_images.assets import (
    get_product_image_asset,
    stage_product_image_identity,
)
from productflow_backend.application.queue_submission import enqueue_or_mark_failed
from productflow_backend.application.storage_compensation import (
    StorageWriteCompensation,
    best_effort_storage_delete,
    compensate_storage_writes,
)
from productflow_backend.application.time import now_utc
from productflow_backend.config import normalize_image_generation_size
from productflow_backend.domain.durable_generation_tasks import (
    IMAGE_SESSION_GENERATION_TASK_CONTRACT,
    IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_DETAIL,
    IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_PHASE,
    QUEUE_UNAVAILABLE_DETAIL,
)
from productflow_backend.domain.enums import (
    ImageSessionAssetKind,
    JobStatus,
    MediaVerificationStatus,
    ProductImageOriginType,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    ImageSession,
    ImageSessionAsset,
    ImageSessionGenerationTask,
    ImageSessionRound,
    MediaObject,
    Product,
    ProductImageAsset,
    new_id,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.image.base import infer_extension
from productflow_backend.infrastructure.image.chat_types import (
    GeneratedChatImage,
    ImageChatTurn,
    ImageSessionProviderFailure,
)
from productflow_backend.infrastructure.image.failures import (
    ImageGenerationFailureDecision,
    classify_image_generation_failure,
)
from productflow_backend.infrastructure.queue import (
    enqueue_image_session_generation_task,  # noqa: F401  # 保留以兼容测试 monkeypatch
    enqueue_image_session_generation_task_later,  # noqa: F401  # 保留以兼容测试 monkeypatch
)
from productflow_backend.infrastructure.runtime_settings import get_runtime_settings
from productflow_backend.infrastructure.storage import LocalStorage

DEFAULT_SESSION_TITLE = "未命名会话"


@dataclass(frozen=True, slots=True)
class ImageSessionAssetDownload:
    storage_path: str
    original_filename: str
    mime_type: str


def get_image_session_asset_download(session: Session, *, asset_id: str) -> ImageSessionAssetDownload:
    """解析会话资源下载所需的规范媒体文件元数据。"""
    asset = session.get(ImageSessionAsset, asset_id)
    if asset is None:
        raise NotFoundError("会话图片不存在")
    media = asset.media_object
    if media is None:
        raise NotFoundError("会话图片文件不存在")
    return ImageSessionAssetDownload(
        storage_path=media.storage_path,
        original_filename=asset.original_filename,
        mime_type=media.mime_type,
    )


DEFAULT_ASSISTANT_MESSAGE = "已按本轮选择的图片上下文生成候选，你可以从任意候选继续。"
MAX_BRANCH_CONTEXT_IMAGES = 6
IMAGE_SESSION_GENERATION_MAX_ATTEMPTS = 3
IMAGE_SESSION_GENERATION_MAX_COUNT = 10
IMAGE_SESSION_IMAGES_API_N_MAX_COUNT = 10
IMAGE_SESSION_CAPACITY_RETRY_DELAY_MS = 2000
IMAGE_SESSION_CONFIRMED_PROVIDER_FAILURE_CATEGORIES = frozenset(
    {"rate_limit", "quota", "content_policy", "unsupported_parameters", "bad_request"}
)
GENERIC_IMAGE_GENERATION_FAILURE = "图片生成失败，请稍后重试"
PARTIAL_IMAGE_GENERATION_FAILURE = "已生成 {completed}/{requested} 张候选，后续生成失败，请重新发起生成补齐。"
PARTIAL_IMAGE_GENERATION_TIMEOUT = "已生成 {completed}/{requested} 张候选，但任务超时，剩余候选未完成。"
IMAGE_SESSION_CANCELLED_REASON = "已取消"

logger = logging.getLogger(__name__)


@dataclass(frozen=True, slots=True)
class ImageSessionGenerationTaskCreationResult:
    task: ImageSessionGenerationTask
    image_session: ImageSession


@dataclass(frozen=True, slots=True)
class _ImageSessionGenerationTaskClaimResult:
    claimed: bool
    should_requeue: bool = False
    attempt_id: str | None = None


@dataclass(frozen=True, slots=True)
class ImageSessionRoundGenerationResult:
    image_session: ImageSession
    generation_group_id: str


@dataclass(frozen=True, slots=True)
class ImageSessionStatusSnapshot:
    image_session: ImageSession
    rounds_count: int
    latest_round_id: str | None
    latest_generation_group_id: str | None
    provider_output_by_generation_group: dict[str, dict[str, Any] | None]


@dataclass(frozen=True, slots=True)
class ImageSessionGenerationExecutionError(Exception):
    completed_candidates: int
    requested_candidates: int
    generation_group_id: str | None
    timed_out: bool = False
    safe_reason: str | None = None
    failure_decision: ImageGenerationFailureDecision | None = None
    provider_effect_uncertain: bool = False


class ImageSessionGenerationCancelledError(Exception):
    """worker 执行中观察到耐久取消时抛出。"""


class ImageSessionGenerationStaleAttemptError(Exception):
    """worker 已不再持有这次耐久生成 attempt 时抛出。"""


def _image_session_provider_effect_request_json(
    *,
    prompt: str,
    size: str,
    history: list[ImageChatTurn],
    base_asset_id: str | None,
    selected_reference_asset_ids: list[str],
    tool_options: dict[str, Any] | None,
    provider_kind: str,
    previous_response_id: str | None,
    candidate_start_index: int,
    candidate_count: int,
) -> dict[str, Any]:
    return {
        "effect_kind": "image_session_generation",
        "provider_kind": provider_kind,
        "prompt": prompt,
        "size": size,
        "base_asset_id": base_asset_id,
        "selected_reference_asset_ids": selected_reference_asset_ids,
        "tool_options": tool_options,
        "previous_response_id": previous_response_id,
        "candidate_start_index": candidate_start_index,
        "candidate_count": candidate_count,
        "history": [
            {
                "role": turn.role,
                "content": turn.content,
                "has_image": turn.image_data_url is not None,
            }
            for turn in history[-8:]
        ],
    }


def _record_image_session_provider_effect_result(
    session: Session,
    *,
    task_id: str,
    attempt_id: str | None,
    candidate_start_index: int,
    provider_results: list[GeneratedChatImage],
    candidate_count: int,
) -> None:
    if attempt_id is None:
        raise ImageSessionGenerationStaleAttemptError()
    if not provider_results:
        raise RuntimeError("图片 provider 返回了空的候选结果")
    if len(provider_results) != candidate_count:
        raise RuntimeError("图片 provider 返回的候选数量与请求不一致")
    result = provider_results[0]
    provider_output = result.provider_output_json if isinstance(result.provider_output_json, dict) else {}
    provider_status = provider_output.get("status")
    if not isinstance(provider_status, str) or not provider_status.strip():
        provider_status = "completed"
    result_json = {
        "provider_name": result.provider_name,
        "provider_model": result.model_name,
        "provider_response_id": result.provider_response_id,
        "provider_status": provider_status[:80],
        "candidate_count": candidate_count,
        "returned_result_count": len(provider_results),
        "provider_output_keys": sorted(str(key) for key in provider_output)[:32],
    }
    if not record_image_session_provider_effect_result(
        session,
        task_id=task_id,
        attempt_id=attempt_id,
        candidate_start_index=candidate_start_index,
        provider_response_id=result.provider_response_id,
        provider_status=provider_status,
        result_json=result_json,
    ):
        raise ImageSessionGenerationStaleAttemptError()
    session.commit()
    _update_image_generation_task_progress(
        session,
        task_id=task_id,
        attempt_id=attempt_id,
        phase="provider_result_received",
        active_candidate_index=candidate_start_index,
        provider_response_id=result.provider_response_id,
        provider_response_status=provider_status,
        progress_metadata={
            "candidate_index": candidate_start_index,
            "candidate_count": candidate_count,
            "provider_effect_operation_key": image_session_provider_effect_operation_key(
                task_id,
                candidate_start_index=candidate_start_index,
                candidate_count=candidate_count,
            ),
            "effect_result": "applied",
        },
    )


def _image_session_query():
    return (
        select(ImageSession)
        .options(
            selectinload(ImageSession.assets).selectinload(ImageSessionAsset.media_object),
            selectinload(ImageSession.rounds).selectinload(ImageSessionRound.generated_asset),
            selectinload(ImageSession.generation_tasks).selectinload(ImageSessionGenerationTask.provider_effects),
        )
        .order_by(desc(ImageSession.updated_at))
    )


def _image_session_status_query():
    return select(ImageSession).options(
        selectinload(ImageSession.generation_tasks).selectinload(ImageSessionGenerationTask.provider_effects)
    )


def _get_image_session_or_raise(session: Session, image_session_id: str) -> ImageSession:
    image_session = session.scalar(_image_session_query().where(ImageSession.id == image_session_id))
    if image_session is None:
        raise NotFoundError("连续生图会话不存在")
    _attach_generation_task_queue_metadata(session, image_session)
    return image_session


def _attach_generation_task_queue_metadata(session: Session, image_session: ImageSession) -> None:
    overview = get_generation_queue_overview(session)
    queued_positions = get_queued_generation_positions(session)
    for task in image_session.generation_tasks:
        metadata = get_generation_task_queue_metadata(
            session,
            task,
            overview=overview,
            queued_positions=queued_positions,
        )
        task.__dict__["_queue_metadata"] = metadata


def _session_data_url(storage: LocalStorage, path: str, mime_type: str) -> str:
    raw = storage.resolve(path).read_bytes()
    encoded = b64encode(raw).decode("utf-8")
    return f"data:{mime_type};base64,{encoded}"


def _trim_title(prompt: str) -> str:
    compact = " ".join(prompt.strip().split())
    return compact[:32] + ("..." if len(compact) > 32 else "")


def _normalize_generation_prompt(prompt: str) -> str:
    normalized = prompt.strip()
    if not normalized:
        raise BusinessValidationError("提示词不能为空")
    return normalized


def _find_session_asset_or_raise(
    image_session: ImageSession,
    asset_id: str,
    *,
    expected_kind: ImageSessionAssetKind | None = None,
    missing_message: str = "会话图片不存在",
) -> ImageSessionAsset:
    asset = next((item for item in image_session.assets if item.id == asset_id), None)
    if asset is None:
        raise NotFoundError(missing_message)
    if expected_kind is not None and asset.kind != expected_kind:
        if expected_kind == ImageSessionAssetKind.GENERATED_IMAGE:
            raise BusinessValidationError("只能从会话生成图继续")
        raise BusinessValidationError("只能选择会话参考图参与本轮生成")
    return asset


def _unique_ids(ids: list[str] | None) -> list[str]:
    return unique_image_generation_ids(ids)


def _has_prior_generation_request(
    image_session: ImageSession,
    *,
    current_generation_task_id: str | None = None,
) -> bool:
    def has_active_task(excluded_task_id: str | None = None) -> bool:
        return any(
            task.id != excluded_task_id and IMAGE_SESSION_GENERATION_TASK_CONTRACT.is_active(task.status)
            for task in image_session.generation_tasks
        )

    if current_generation_task_id is not None:
        tasks = sorted(image_session.generation_tasks, key=lambda task: (task.created_at, task.id))
        current_task = next((task for task in tasks if task.id == current_generation_task_id), None)
        if (
            current_task is not None
            and current_task.result_generation_group_id
            and any(
                round_item.generation_group_id == current_task.result_generation_group_id
                for round_item in image_session.rounds
            )
        ):
            return False
        if image_session.rounds:
            return True
        if tasks:
            if tasks[0].id == current_generation_task_id:
                return False
            return has_active_task(excluded_task_id=current_generation_task_id)
        return False
    if image_session.rounds:
        return True
    if current_generation_task_id is None:
        return has_active_task()
    return False


def _build_branch_generation_context(
    image_session: ImageSession,
    storage: LocalStorage,
    *,
    base_asset_id: str | None,
    selected_reference_asset_ids: list[str] | None,
) -> tuple[list[ImageChatTurn], list[str], str | None, str | None, list[str]]:
    """构建卡片式分支上下文：只使用显式 base 和本轮勾选参考图。"""
    manual_references: list[str] = []
    normalized_base_asset_id: str | None = None
    selected_reference_ids = _unique_ids(selected_reference_asset_ids)
    if (1 if base_asset_id else 0) + len(selected_reference_ids) > MAX_BRANCH_CONTEXT_IMAGES:
        raise BusinessValidationError("本轮最多选择 6 张图片上下文（含分支基图）")

    if base_asset_id:
        base_asset = _find_session_asset_or_raise(
            image_session,
            base_asset_id,
            expected_kind=ImageSessionAssetKind.GENERATED_IMAGE,
        )
        normalized_base_asset_id = base_asset.id
        manual_references.append(
            _session_data_url(storage, base_asset.media_object.storage_path, base_asset.media_object.mime_type)
        )

    normalized_reference_ids: list[str] = []
    for asset_id in selected_reference_ids:
        reference_asset = _find_session_asset_or_raise(
            image_session,
            asset_id,
            expected_kind=ImageSessionAssetKind.REFERENCE_UPLOAD,
            missing_message="会话参考图不存在",
        )
        normalized_reference_ids.append(reference_asset.id)
        manual_references.append(
            _session_data_url(
                storage,
                reference_asset.media_object.storage_path,
                reference_asset.media_object.mime_type,
            )
        )

    return [], manual_references[:6], None, normalized_base_asset_id, normalized_reference_ids


def _validate_generation_request(
    image_session: ImageSession,
    *,
    session: Session | None = None,
    size: str,
    base_asset_id: str | None,
    selected_reference_asset_ids: list[str] | None,
    generation_count: int,
    tool_options: dict[str, Any] | None = None,
    current_generation_task_id: str | None = None,
    max_generation_count: int = IMAGE_SESSION_GENERATION_MAX_COUNT,
) -> tuple[str, str | None, list[str]]:
    if not 1 <= generation_count <= max_generation_count:
        raise BusinessValidationError(f"一次生成数量必须在 1-{max_generation_count} 张之间")
    normalized_size = normalize_image_generation_size(
        size,
        max_dimension=int(get_runtime_settings(session).image_generation_max_dimension),
    )
    selected_reference_ids = _unique_ids(selected_reference_asset_ids)
    if (1 if base_asset_id else 0) + len(selected_reference_ids) > MAX_BRANCH_CONTEXT_IMAGES:
        raise BusinessValidationError("本轮最多选择 6 张图片上下文（含分支基图）")

    normalized_base_asset_id: str | None = None
    if base_asset_id:
        base_asset = _find_session_asset_or_raise(
            image_session,
            base_asset_id,
            expected_kind=ImageSessionAssetKind.GENERATED_IMAGE,
        )
        normalized_base_asset_id = base_asset.id
    elif _has_prior_generation_request(image_session, current_generation_task_id=current_generation_task_id):
        raise BusinessValidationError("后续生图必须选择一张本会话已生成图片作为基图")

    normalized_reference_ids: list[str] = []
    for asset_id in selected_reference_ids:
        reference_asset = _find_session_asset_or_raise(
            image_session,
            asset_id,
            expected_kind=ImageSessionAssetKind.REFERENCE_UPLOAD,
            missing_message="会话参考图不存在",
        )
        normalized_reference_ids.append(reference_asset.id)

    return normalized_size, normalized_base_asset_id, normalized_reference_ids


def _normalize_tool_options(tool_options: dict[str, Any] | None) -> dict[str, Any] | None:
    return normalize_image_generation_tool_options(tool_options)


def _images_api_batch_count(
    *,
    provider_kind: str,
    remaining_count: int,
) -> int:
    if provider_kind != "openai_images":
        return 1
    return max(1, min(remaining_count, IMAGE_SESSION_IMAGES_API_N_MAX_COUNT))


def _provider_output_with_actual_size(
    provider_output_json: dict[str, Any] | None,
    *,
    requested_size: str,
    image_bytes: bytes,
) -> dict[str, Any]:
    return provider_output_with_actual_image_size(
        provider_output_json,
        requested_size=requested_size,
        image_bytes=image_bytes,
    )


def list_image_sessions(session: Session) -> list[ImageSession]:
    return list(session.scalars(_image_session_query()).all())


def get_image_session_detail(session: Session, image_session_id: str) -> ImageSession:
    return _get_image_session_or_raise(session, image_session_id)


def get_image_session_status(session: Session, image_session_id: str) -> ImageSessionStatusSnapshot:
    image_session = session.scalar(_image_session_status_query().where(ImageSession.id == image_session_id))
    if image_session is None:
        raise NotFoundError("连续生图会话不存在")
    _attach_generation_task_queue_metadata(session, image_session)

    rounds_count = session.scalar(
        select(func.count()).select_from(ImageSessionRound).where(ImageSessionRound.session_id == image_session.id)
    )
    latest_round_row = session.execute(
        select(ImageSessionRound.id, ImageSessionRound.generation_group_id)
        .where(ImageSessionRound.session_id == image_session.id)
        .order_by(desc(ImageSessionRound.created_at), desc(ImageSessionRound.id))
        .limit(1)
    ).first()
    result_group_ids = {
        task.result_generation_group_id
        for task in image_session.generation_tasks
        if task.result_generation_group_id is not None
    }
    provider_output_by_group: dict[str, dict[str, Any] | None] = {}
    if result_group_ids:
        for generation_group_id, provider_output_json in session.execute(
            select(ImageSessionRound.generation_group_id, ImageSessionRound.provider_output_json)
            .where(
                ImageSessionRound.session_id == image_session.id,
                ImageSessionRound.generation_group_id.in_(result_group_ids),
            )
            .order_by(desc(ImageSessionRound.created_at))
        ):
            if generation_group_id and generation_group_id not in provider_output_by_group:
                provider_output_by_group[generation_group_id] = provider_output_json

    return ImageSessionStatusSnapshot(
        image_session=image_session,
        rounds_count=int(rounds_count or 0),
        latest_round_id=latest_round_row.id if latest_round_row else None,
        latest_generation_group_id=latest_round_row.generation_group_id if latest_round_row else None,
        provider_output_by_generation_group=provider_output_by_group,
    )


def create_image_session(
    session: Session,
    *,
    title: str | None = None,
) -> ImageSession:
    normalized_title = (title or DEFAULT_SESSION_TITLE).strip() or DEFAULT_SESSION_TITLE
    image_session = ImageSession(title=normalized_title)
    session.add(image_session)
    session.commit()
    session.expire_all()
    return _get_image_session_or_raise(session, image_session.id)


def update_image_session(
    session: Session,
    *,
    image_session_id: str,
    title: str,
) -> ImageSession:
    image_session = _get_image_session_or_raise(session, image_session_id)
    image_session.title = title.strip() or DEFAULT_SESSION_TITLE
    image_session.updated_at = now_utc()
    session.commit()
    session.expire_all()
    return _get_image_session_or_raise(session, image_session.id)


def delete_image_session(
    session: Session,
    *,
    image_session_id: str,
    storage: LocalStorage | None = None,
) -> None:
    """删除会话资产行；已附加到商品的 MediaObject 因仍被引用而保留。"""

    image_session = _get_image_session_or_raise(session, image_session_id)
    session.expire(image_session, ["assets", "rounds", "generation_tasks"])
    image_session_assets = list(image_session.assets)
    storage = storage or LocalStorage()
    asset_ids = [asset.id for asset in image_session_assets]
    media_ids = {asset.media_object_id for asset in image_session_assets}
    if asset_ids:
        session.execute(
            update(ProductImageAsset)
            .where(ProductImageAsset.source_image_session_asset_id.in_(asset_ids))
            .values(source_image_session_asset_id=None)
        )
    session.delete(image_session)
    session.flush()
    deleted_media = prune_unreferenced_media_objects(session, media_ids)
    session.commit()
    # 业务行已提交；仅清理已无引用的文件。
    for media_id, storage_path in deleted_media:
        best_effort_storage_delete(
            lambda path=storage_path: storage.delete_image_with_variants(path),
            target=f"media_object_id={media_id} path={storage_path}",
        )
    best_effort_storage_delete(
        lambda: storage.remove_empty_image_session_directories(image_session_id),
        target=f"image_session_id={image_session_id} empty_directories",
    )


def add_image_session_reference_images(
    session: Session,
    *,
    image_session_id: str,
    reference_image_uploads: list[tuple[bytes, str, str]],
    storage: LocalStorage | None = None,
) -> ImageSession:
    """参考图进入 canonical MediaObject；DB 失败只收回本次写入。"""

    image_session = _get_image_session_or_raise(session, image_session_id)
    storage = storage or LocalStorage()
    with compensate_storage_writes(session) as storage_writes:
        for content, filename, mime_type in reference_image_uploads:
            media = stage_verified_media_object(
                session,
                content=content,
                filename=filename,
                expected_mime_type=mime_type,
                storage=storage,
                storage_writes=storage_writes,
            )
            session.add(
                ImageSessionAsset(
                    session_id=image_session.id,
                    kind=ImageSessionAssetKind.REFERENCE_UPLOAD,
                    original_filename=filename,
                    mime_type=media.mime_type,
                    storage_path=media.storage_path,
                    media_object=media,
                )
            )
        image_session.updated_at = now_utc()
        session.commit()
    session.expire_all()
    return _get_image_session_or_raise(session, image_session.id)


def delete_image_session_reference_image(
    session: Session,
    *,
    image_session_id: str,
    asset_id: str,
    storage: LocalStorage | None = None,
) -> ImageSession:
    image_session = _get_image_session_or_raise(session, image_session_id)
    asset = next((item for item in image_session.assets if item.id == asset_id), None)
    if asset is None:
        raise NotFoundError("会话参考图不存在")
    if asset.kind != ImageSessionAssetKind.REFERENCE_UPLOAD:
        raise BusinessValidationError("只能删除会话参考图")

    storage = storage or LocalStorage()
    storage_path = asset.media_object.storage_path
    media_id = asset.media_object_id
    session.execute(
        update(ProductImageAsset)
        .where(ProductImageAsset.source_image_session_asset_id == asset.id)
        .values(source_image_session_asset_id=None)
    )
    session.delete(asset)
    image_session.updated_at = now_utc()
    session.flush()
    deleted_media = prune_unreferenced_media_objects(session, {media_id})
    session.commit()
    if deleted_media:
        best_effort_storage_delete(
            lambda: storage.delete_image_with_variants(storage_path),
            target=f"image_session_asset_id={asset_id} path={storage_path}",
        )
    session.expire_all()
    return _get_image_session_or_raise(session, image_session.id)


def _execute_image_session_round_generation(
    session: Session,
    *,
    image_session_id: str,
    prompt: str,
    size: str,
    base_asset_id: str | None = None,
    selected_reference_asset_ids: list[str] | None = None,
    generation_count: int = 1,
    tool_options: dict[str, Any] | None = None,
    storage: LocalStorage | None = None,
    generation_task_id: str | None = None,
    generation_attempt_id: str | None = None,
    chat_service_factory: ImageChatProviderFactory | None = None,
) -> ImageSessionRoundGenerationResult:
    """执行一轮生图，调用 AI 并保存结果到会话。"""
    image_session = _get_image_session_or_raise(session, image_session_id)
    storage = storage or LocalStorage()
    generation_task = session.get(ImageSessionGenerationTask, generation_task_id) if generation_task_id else None
    if generation_task is not None and (
        generation_task.status != JobStatus.RUNNING
        or generation_task.active_attempt_id != generation_attempt_id
    ):
        raise ImageSessionGenerationStaleAttemptError()
    normalized_prompt = _normalize_generation_prompt(prompt)
    normalized_tool_options = _normalize_tool_options(tool_options)
    service = (chat_service_factory or default_image_session_chat_service_factory)()
    normalized_size, normalized_base_asset_id, normalized_reference_ids = _validate_generation_request(
        image_session,
        session=session,
        size=size,
        base_asset_id=base_asset_id,
        selected_reference_asset_ids=selected_reference_asset_ids,
        generation_count=generation_count,
        tool_options=normalized_tool_options,
        current_generation_task_id=generation_task_id,
    )
    (
        history,
        manual_references,
        previous_response_id,
        _validated_base_asset_id,
        _validated_reference_ids,
    ) = _build_branch_generation_context(
        image_session,
        storage,
        base_asset_id=normalized_base_asset_id,
        selected_reference_asset_ids=normalized_reference_ids,
    )

    generation_group_id = generation_task.result_generation_group_id if generation_task else None
    completed_candidates = 0
    if generation_task is not None:
        completed_candidates = max(0, min(generation_task.completed_candidates or 0, generation_count))
        if generation_group_id:
            saved_candidate_index = session.scalar(
                select(func.max(ImageSessionRound.candidate_index)).where(
                    ImageSessionRound.session_id == image_session.id,
                    ImageSessionRound.generation_group_id == generation_group_id,
                )
            )
            completed_candidates = max(completed_candidates, min(int(saved_candidate_index or 0), generation_count))
        if completed_candidates >= generation_count:
            _finish_image_generation_task(
                session,
                task=generation_task,
                status=JobStatus.SUCCEEDED,
                result_generation_group_id=generation_group_id,
                is_retryable=False,
                expected_attempt_id=generation_attempt_id,
            )
            session.expire_all()
            return ImageSessionRoundGenerationResult(
                image_session=_get_image_session_or_raise(session, image_session.id),
                generation_group_id=generation_group_id or new_id(),
            )
    generation_group_id = generation_group_id or new_id()
    should_update_default_title = not image_session.rounds and image_session.title == DEFAULT_SESSION_TITLE
    pending_provider_results: list[tuple[GeneratedChatImage, str]] = []

    for candidate_index in range(completed_candidates + 1, generation_count + 1):
        storage_writes = StorageWriteCompensation()
        active_provider_effect_operation_key: str | None = None
        try:
            _raise_if_image_generation_task_cancelled(
                session,
                generation_task_id,
                generation_attempt_id,
            )
            if generation_task_id is not None:
                _update_image_generation_task_progress(
                    session,
                    task_id=generation_task_id,
                    attempt_id=generation_attempt_id,
                    phase="candidate_started",
                    completed_candidates=completed_candidates,
                    active_candidate_index=candidate_index,
                    provider_response_id=None,
                    provider_response_status=None,
                    progress_metadata={
                        "candidate_index": candidate_index,
                        "candidate_count": generation_count,
                    },
                    clear_provider_response=True,
            )
            _raise_if_image_generation_task_cancelled(
                session,
                generation_task_id,
                generation_attempt_id,
            )
            if pending_provider_results:
                result, active_provider_effect_operation_key = pending_provider_results.pop(0)
            else:
                remaining_count = generation_count - candidate_index + 1
                batch_count = _images_api_batch_count(
                    provider_kind=service.provider_kind,
                    remaining_count=remaining_count,
                )
                provider_previous_response_id = previous_response_id if batch_count == 1 else None
                effect_request_json = _image_session_provider_effect_request_json(
                    prompt=normalized_prompt,
                    size=normalized_size,
                    history=history,
                    base_asset_id=normalized_base_asset_id,
                    selected_reference_asset_ids=normalized_reference_ids,
                    tool_options=normalized_tool_options,
                    provider_kind=service.provider_kind,
                    previous_response_id=provider_previous_response_id,
                    candidate_start_index=candidate_index,
                    candidate_count=batch_count,
                )
                if generation_task_id is not None:
                    if generation_attempt_id is None:
                        raise ImageSessionGenerationStaleAttemptError()
                    active_provider_effect_operation_key = image_session_provider_effect_operation_key(
                        generation_task_id,
                        candidate_start_index=candidate_index,
                        candidate_count=batch_count,
                    )
                    if not ensure_image_session_provider_effect_intent(
                        session,
                        task_id=generation_task_id,
                        attempt_id=generation_attempt_id,
                        candidate_start_index=candidate_index,
                        candidate_count=batch_count,
                        operation_key=active_provider_effect_operation_key,
                        request_hash=image_session_provider_effect_request_hash(effect_request_json),
                        provider_name=service.provider_kind,
                        request_json=effect_request_json,
                    ):
                        raise ImageSessionGenerationStaleAttemptError()
                    session.commit()
                if batch_count > 1:
                    provider_results = service.generate_many(
                        prompt=normalized_prompt,
                        size=normalized_size,
                        history=history,
                        manual_reference_images=manual_references,
                        candidate_count=batch_count,
                        tool_options=normalized_tool_options,
                    )
                    if generation_task_id is not None:
                        _record_image_session_provider_effect_result(
                            session,
                            task_id=generation_task_id,
                            attempt_id=generation_attempt_id,
                            candidate_start_index=candidate_index,
                            provider_results=provider_results,
                            candidate_count=batch_count,
                        )
                    result = provider_results[0]
                    if active_provider_effect_operation_key is None:
                        raise RuntimeError("image-session provider effect operation key missing")
                    pending_provider_results.extend(
                        (provider_result, active_provider_effect_operation_key)
                        for provider_result in provider_results[1:]
                    )
                else:
                    result = service.generate(
                        prompt=normalized_prompt,
                        size=normalized_size,
                        history=history,
                        manual_reference_images=manual_references,
                        previous_response_id=provider_previous_response_id,
                        tool_options=normalized_tool_options,
                        progress_callback=_provider_progress_callback(
                            session,
                            task_id=generation_task_id,
                            attempt_id=generation_attempt_id,
                            session_id=image_session_id,
                            candidate_index=candidate_index,
                            generation_count=generation_count,
                            completed_candidates=completed_candidates,
                        ),
                    )
                    if generation_task_id is not None:
                        _record_image_session_provider_effect_result(
                            session,
                            task_id=generation_task_id,
                            attempt_id=generation_attempt_id,
                            candidate_start_index=candidate_index,
                            provider_results=[result],
                            candidate_count=batch_count,
                        )
            locked_generation_task = _lock_image_generation_attempt(
                session,
                task_id=generation_task_id,
                attempt_id=generation_attempt_id,
            )

            original_filename = (
                f"generated-{now_utc().strftime('%Y%m%d-%H%M%S')}"
                f"-{candidate_index}{infer_extension(result.mime_type)}"
            )
            media = stage_verified_media_object(
                session,
                content=result.bytes_data,
                filename=original_filename,
                expected_mime_type=result.mime_type,
                storage=storage,
                storage_writes=storage_writes,
            )
            # 生成结果进入会话资产并共享 MediaObject；附加到商品时不再复制 bytes。
            asset = ImageSessionAsset(
                session_id=image_session.id,
                kind=ImageSessionAssetKind.GENERATED_IMAGE,
                original_filename=original_filename,
                mime_type=media.mime_type,
                storage_path=media.storage_path,
                media_object=media,
            )
            session.add(asset)
            session.flush()

            assistant_message = (
                f"已生成第 {candidate_index}/{generation_count} 张候选，你可以从任意候选继续。"
                if generation_count > 1
                else DEFAULT_ASSISTANT_MESSAGE
            )
            round_item = ImageSessionRound(
                session_id=image_session.id,
                prompt=normalized_prompt,
                assistant_message=assistant_message,
                size=normalized_size,
                model_name=result.model_name,
                provider_name=result.provider_name,
                prompt_version=result.prompt_version,
                provider_response_id=result.provider_response_id,
                previous_response_id=None,
                image_generation_call_id=result.image_generation_call_id,
                provider_request_json=result.provider_request_json,
                provider_output_json=_provider_output_with_actual_size(
                    result.provider_output_json,
                    requested_size=normalized_size,
                    image_bytes=result.bytes_data,
                ),
                generation_group_id=generation_group_id,
                candidate_index=candidate_index,
                candidate_count=generation_count,
                base_asset_id=normalized_base_asset_id,
                selected_reference_asset_ids=normalized_reference_ids,
                generated_asset_id=asset.id,
            )
            session.add(round_item)
            session.flush()
            now = now_utc()
            if should_update_default_title:
                _touch_image_session_if_present(
                    session,
                    image_session.id,
                    now=now,
                    title=_trim_title(normalized_prompt),
                )
                should_update_default_title = False
            else:
                _touch_image_session_if_present(session, image_session.id, now=now)
            if generation_task_id is not None:
                if locked_generation_task is None:
                    raise ImageSessionGenerationStaleAttemptError()
                locked_generation_task.completed_candidates = candidate_index
                locked_generation_task.active_candidate_index = None
                locked_generation_task.progress_phase = "candidate_saved"
                locked_generation_task.progress_updated_at = now_utc()
                locked_generation_task.result_generation_group_id = generation_group_id
                locked_generation_task.progress_metadata = {
                    "candidate_index": candidate_index,
                    "candidate_count": generation_count,
                    "generated_asset_id": asset.id,
                    "round_id": round_item.id,
                }
                if candidate_index == generation_count:
                    _finish_image_generation_task(
                        session,
                        task=locked_generation_task,
                        status=JobStatus.SUCCEEDED,
                        result_generation_group_id=generation_group_id,
                        is_retryable=False,
                        expected_attempt_id=generation_attempt_id,
                    )
                else:
                    session.commit()
            else:
                session.commit()
            storage_writes.release()  # 行已提交；后续候选失败不得删掉本张已入账文件。
            completed_candidates += 1
        except BaseException as exc:  # noqa: BLE001
            session.rollback()
            storage_writes.cleanup()
            if isinstance(
                exc,
                (ImageSessionGenerationCancelledError, ImageSessionGenerationStaleAttemptError),
            ):
                raise
            if isinstance(exc, (KeyboardInterrupt, SystemExit)):
                raise
            if generation_task_id is None:
                raise
            if isinstance(exc, ImageSessionProviderFailure):
                failure_decision = exc.failure_decision
                safe_reason = exc.safe_reason
            else:
                failure_decision = classify_image_generation_failure(
                    exc,
                    generic_message=GENERIC_IMAGE_GENERATION_FAILURE,
                )
                safe_reason = failure_decision.reason
            provider_effect_uncertain = False
            if active_provider_effect_operation_key is not None:
                if generation_attempt_id is None:
                    raise ImageSessionGenerationStaleAttemptError() from exc
                confirmed_no_effect = isinstance(exc, ImageSessionProviderFailure) or (
                    failure_decision is not None
                    and failure_decision.category in IMAGE_SESSION_CONFIRMED_PROVIDER_FAILURE_CATEGORIES
                )
                if confirmed_no_effect:
                    mark_image_session_provider_effect_failed(
                        session,
                        task_id=generation_task_id,
                        attempt_id=generation_attempt_id,
                        candidate_start_index=candidate_index,
                        detail=safe_reason,
                    )
                else:
                    # 请求可能已发出，无法证明 provider 副作用，不能记 failed。
                    mark_image_session_provider_effect_unknown(
                        session,
                        task_id=generation_task_id,
                        attempt_id=generation_attempt_id,
                        candidate_start_index=candidate_index,
                        detail=IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_DETAIL,
                    )
                    provider_effect_uncertain = True
                session.commit()
            raise ImageSessionGenerationExecutionError(
                completed_candidates=completed_candidates,
                requested_candidates=generation_count,
                generation_group_id=generation_group_id if completed_candidates else None,
                timed_out=isinstance(exc, TimeLimitExceeded),
                safe_reason=safe_reason,
                failure_decision=failure_decision,
                provider_effect_uncertain=provider_effect_uncertain,
            ) from exc
    session.expire_all()
    return ImageSessionRoundGenerationResult(
        image_session=_get_image_session_or_raise(session, image_session.id),
        generation_group_id=generation_group_id,
    )


def generate_image_session_round(
    session: Session,
    *,
    image_session_id: str,
    prompt: str,
    size: str,
    base_asset_id: str | None = None,
    selected_reference_asset_ids: list[str] | None = None,
    generation_count: int = 1,
    tool_options: dict[str, Any] | None = None,
    storage: LocalStorage | None = None,
    chat_service_factory: ImageChatProviderFactory | None = None,
) -> ImageSession:
    """兼容同步调用的薄封装；HTTP route 不再使用。"""
    return _execute_image_session_round_generation(
        session,
        image_session_id=image_session_id,
        prompt=prompt,
        size=size,
        base_asset_id=base_asset_id,
        selected_reference_asset_ids=selected_reference_asset_ids,
        generation_count=generation_count,
        tool_options=tool_options,
        storage=storage,
        chat_service_factory=chat_service_factory,
    ).image_session


def create_image_session_generation_task(
    session: Session,
    *,
    image_session_id: str,
    prompt: str,
    size: str,
    base_asset_id: str | None = None,
    selected_reference_asset_ids: list[str] | None = None,
    generation_count: int = 1,
    tool_options: dict[str, Any] | None = None,
    commit: bool = True,
) -> ImageSessionGenerationTaskCreationResult:
    """校验并创建连续生图 durable 任务；不调用 provider。"""
    image_session = _get_image_session_or_raise(session, image_session_id)
    normalized_prompt = _normalize_generation_prompt(prompt)
    normalized_tool_options = _normalize_tool_options(tool_options)
    normalized_size, normalized_base_asset_id, normalized_reference_ids = _validate_generation_request(
        image_session,
        session=session,
        size=size,
        base_asset_id=base_asset_id,
        selected_reference_asset_ids=selected_reference_asset_ids,
        generation_count=generation_count,
        tool_options=normalized_tool_options,
    )
    ensure_generation_capacity(session)
    task = ImageSessionGenerationTask(
        session_id=image_session.id,
        status=JobStatus.QUEUED,
        prompt=normalized_prompt,
        size=normalized_size,
        base_asset_id=normalized_base_asset_id,
        selected_reference_asset_ids=normalized_reference_ids,
        tool_options=normalized_tool_options,
        generation_count=generation_count,
    )
    session.add(task)
    image_session.updated_at = now_utc()
    session.flush()
    if commit:
        session.commit()
        session.expire_all()
    return ImageSessionGenerationTaskCreationResult(
        task=session.get(ImageSessionGenerationTask, task.id) or task,
        image_session=_get_image_session_or_raise(session, image_session.id),
    )


def submit_image_session_generation_task(
    session: Session,
    *,
    image_session_id: str,
    prompt: str,
    size: str,
    base_asset_id: str | None = None,
    selected_reference_asset_ids: list[str] | None = None,
    generation_count: int = 1,
    tool_options: dict[str, Any] | None = None,
    enqueue: Callable[[str], None] | None = None,
) -> ImageSession:
    result = create_image_session_generation_task(
        session,
        image_session_id=image_session_id,
        prompt=prompt,
        size=size,
        base_asset_id=base_asset_id,
        selected_reference_asset_ids=selected_reference_asset_ids,
        generation_count=generation_count,
        tool_options=tool_options,
        commit=False,
    )
    if enqueue is None:
        stage_async_dispatch(
            session,
            delivery_key=delivery_key_for_actor(IMAGE_SESSION_GENERATION_TASK_CONTRACT.actor_name, result.task.id),
            actor_name=IMAGE_SESSION_GENERATION_TASK_CONTRACT.actor_name,
            aggregate_id=result.task.id,
        )
        session.commit()
    else:
        session.commit()
        enqueue_or_mark_failed(
            result.task.id,
            enqueue=enqueue,
            mark_failed=lambda task_id, reason: mark_image_session_generation_task_enqueue_failed(
                session,
                task_id=task_id,
                reason=reason,
            ),
        )
    session.expire_all()
    return get_image_session_detail(session, image_session_id)


def retry_image_session_generation_task(
    session: Session,
    *,
    image_session_id: str,
    task_id: str,
    enqueue: Callable[[str], None] | None = None,
) -> ImageSession:
    _get_image_session_or_raise(session, image_session_id)
    task = session.scalar(
        select(ImageSessionGenerationTask).where(
            ImageSessionGenerationTask.id == task_id,
            ImageSessionGenerationTask.session_id == image_session_id,
        )
    )
    if task is None:
        raise NotFoundError("生成任务不存在")
    if task.status != JobStatus.FAILED:
        raise BusinessValidationError("只有失败的生成任务可以重试")
    if not task.is_retryable:
        raise BusinessValidationError("该生成任务不可重试")

    _reset_image_generation_task_for_retry(
        session,
        task=task,
        progress_phase="manual_retry_queued",
    )
    if enqueue is None:
        requeue_async_dispatch(
            session,
            delivery_key=delivery_key_for_actor(IMAGE_SESSION_GENERATION_TASK_CONTRACT.actor_name, task.id),
            actor_name=IMAGE_SESSION_GENERATION_TASK_CONTRACT.actor_name,
            aggregate_id=task.id,
        )
        session.commit()
    else:
        session.commit()
        enqueue_or_mark_failed(
            task.id,
            enqueue=enqueue,
            mark_failed=lambda queued_task_id, reason: mark_image_session_generation_task_enqueue_failed(
                session,
                task_id=queued_task_id,
                reason=reason,
            ),
        )
    session.expire_all()
    return get_image_session_detail(session, image_session_id)


def cancel_image_session_generation_task(
    session: Session,
    *,
    image_session_id: str,
    task_id: str,
) -> ImageSession:
    _get_image_session_or_raise(session, image_session_id)
    task = session.scalar(
        select(ImageSessionGenerationTask).where(
            ImageSessionGenerationTask.id == task_id,
            ImageSessionGenerationTask.session_id == image_session_id,
        )
    )
    if task is None:
        raise NotFoundError("生成任务不存在")
    if task.status == JobStatus.CANCELLED:
        return get_image_session_detail(session, image_session_id)
    if task.status in {JobStatus.SUCCEEDED, JobStatus.FAILED, JobStatus.UNKNOWN}:
        raise BusinessValidationError("已结束的生成任务不能取消")

    _finish_image_generation_task(
        session,
        task=task,
        status=JobStatus.CANCELLED,
        failure_reason=IMAGE_SESSION_CANCELLED_REASON,
        result_generation_group_id=task.result_generation_group_id,
        is_retryable=False,
    )
    session.expire_all()
    return get_image_session_detail(session, image_session_id)


def mark_image_session_generation_task_enqueue_failed(session: Session, *, task_id: str, reason: str) -> None:
    now = now_utc()
    task = session.scalar(select(ImageSessionGenerationTask).where(ImageSessionGenerationTask.id == task_id))
    if task is None:
        return
    failed = session.execute(
        update(ImageSessionGenerationTask)
        .where(
            ImageSessionGenerationTask.id == task_id,
            ImageSessionGenerationTask.status == JobStatus.QUEUED,
            ImageSessionGenerationTask.active_attempt_id.is_(None),
        )
        .values(
            status=JobStatus.FAILED,
            active_attempt_id=None,
            failure_reason=reason[:1000],
            finished_at=now,
            progress_phase="enqueue_failed",
            progress_updated_at=now,
            is_retryable=True,
        )
        .execution_options(synchronize_session=False)
    )
    if failed.rowcount == 1:
        _touch_image_session_if_present(session, task.session_id, now=now)
    session.commit()


def _touch_image_session_if_present(
    session: Session,
    image_session_id: str,
    *,
    now: datetime,
    title: str | None = None,
) -> None:
    """更新父会话时间戳，不挂载可能过期的 ImageSession ORM 行。"""

    values: dict[str, Any] = {"updated_at": now}
    if title is not None:
        values["title"] = title
    session.execute(
        update(ImageSession)
        .where(ImageSession.id == image_session_id)
        .values(**values)
        .execution_options(synchronize_session=False)
    )


def _reset_image_generation_task_for_retry(
    session: Session,
    *,
    task: ImageSessionGenerationTask,
    progress_phase: str,
    result_generation_group_id: str | None = None,
    progress_metadata: dict[str, Any] | None = None,
    expected_attempt_id: str | None = None,
) -> None:
    if expected_attempt_id is not None and (
        task.status != JobStatus.RUNNING or task.active_attempt_id != expected_attempt_id
    ):
        raise ImageSessionGenerationStaleAttemptError()
    now = now_utc()
    task.status = JobStatus.QUEUED
    task.active_attempt_id = None
    task.failure_reason = None
    task.started_at = None
    task.finished_at = None
    task.active_candidate_index = None
    task.progress_phase = progress_phase[:64]
    task.progress_updated_at = now
    task.provider_response_id = None
    task.provider_response_status = None
    task.progress_metadata = progress_metadata
    task.is_retryable = True
    if result_generation_group_id is not None:
        task.result_generation_group_id = result_generation_group_id
    _touch_image_session_if_present(session, task.session_id, now=now)
    session.commit()


def _finish_image_generation_task(
    session: Session,
    *,
    task: ImageSessionGenerationTask,
    status: JobStatus,
    failure_reason: str | None = None,
    result_generation_group_id: str | None = None,
    is_retryable: bool,
    expected_attempt_id: str | None = None,
) -> None:
    if expected_attempt_id is not None and (
        task.status != JobStatus.RUNNING or task.active_attempt_id != expected_attempt_id
    ):
        if task.status == JobStatus.CANCELLED:
            raise ImageSessionGenerationCancelledError()
        raise ImageSessionGenerationStaleAttemptError()
    now = now_utc()
    task.status = status
    task.active_attempt_id = None
    task.failure_reason = failure_reason[:1000] if failure_reason else None
    task.result_generation_group_id = result_generation_group_id
    task.is_retryable = is_retryable
    task.finished_at = now
    task.active_candidate_index = None
    task.progress_updated_at = now
    if status == JobStatus.SUCCEEDED:
        task.progress_phase = "succeeded"
    elif status == JobStatus.UNKNOWN:
        task.progress_phase = IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_PHASE
    elif status == JobStatus.CANCELLED:
        task.progress_phase = "cancelled"
    else:
        task.progress_phase = "failed"
    _touch_image_session_if_present(session, task.session_id, now=now)
    session.commit()


def _update_image_generation_task_progress(
    session: Session,
    *,
    task_id: str,
    attempt_id: str | None,
    phase: str,
    completed_candidates: int | None = None,
    active_candidate_index: int | None = None,
    provider_response_id: str | None = None,
    provider_response_status: str | None = None,
    progress_metadata: dict[str, Any] | None = None,
    result_generation_group_id: str | None = None,
    clear_provider_response: bool = False,
) -> None:
    if attempt_id is None:
        raise ImageSessionGenerationStaleAttemptError()
    values: dict[str, Any] = {
        "progress_phase": phase[:64],
        "progress_updated_at": now_utc(),
        "active_candidate_index": active_candidate_index,
    }
    if completed_candidates is not None:
        values["completed_candidates"] = completed_candidates
    if clear_provider_response:
        values["provider_response_id"] = None
        values["provider_response_status"] = None
    elif provider_response_id is not None:
        values["provider_response_id"] = provider_response_id[:255]
        if provider_response_status is not None:
            values["provider_response_status"] = provider_response_status[:64]
    elif provider_response_status is not None:
        values["provider_response_status"] = provider_response_status[:64]
    if progress_metadata is not None:
        values["progress_metadata"] = progress_metadata
    if result_generation_group_id is not None:
        values["result_generation_group_id"] = result_generation_group_id
    updated = cast(
        CursorResult[Any],
        session.execute(
            update(ImageSessionGenerationTask)
            .where(
                ImageSessionGenerationTask.id == task_id,
                ImageSessionGenerationTask.status == JobStatus.RUNNING,
                ImageSessionGenerationTask.active_attempt_id == attempt_id,
            )
            .values(**values)
            .execution_options(synchronize_session=False)
        ),
    )
    if updated.rowcount != 1:
        session.rollback()
        raise ImageSessionGenerationStaleAttemptError()
    session.commit()


def _provider_progress_callback(
    session: Session,
    *,
    task_id: str | None,
    attempt_id: str | None,
    session_id: str,
    candidate_index: int,
    generation_count: int,
    completed_candidates: int,
) -> Callable[[dict[str, Any]], None] | None:
    if task_id is None:
        return None

    def callback(progress: dict[str, Any]) -> None:
        provider_response_id = progress.get("provider_response_id")
        provider_response_status = progress.get("provider_response_status")
        if task_id is not None and attempt_id is not None and (
            provider_response_id is not None or provider_response_status is not None
        ):
            record_image_session_provider_effect_progress(
                session,
                task_id=task_id,
                attempt_id=attempt_id,
                candidate_start_index=candidate_index,
                provider_response_id=provider_response_id,
                provider_status=provider_response_status,
            )
        _update_image_generation_task_progress(
            session,
            task_id=task_id,
            attempt_id=attempt_id,
            phase="provider_polling",
            completed_candidates=completed_candidates,
            active_candidate_index=candidate_index,
            provider_response_id=provider_response_id,
            provider_response_status=provider_response_status,
            progress_metadata={
                "candidate_index": candidate_index,
                "candidate_count": generation_count,
                "provider_response": progress.get("provider_response"),
            },
        )

    callback.productflow_context = {  # type: ignore[attr-defined]
        "task_id": task_id,
        "attempt_id": attempt_id,
        "session_id": session_id,
        "candidate_index": candidate_index,
        "candidate_count": generation_count,
    }
    return callback


def _raise_if_image_generation_task_cancelled(
    session: Session,
    task_id: str | None,
    attempt_id: str | None,
) -> None:
    if task_id is None:
        return
    state = session.execute(
        select(
            ImageSessionGenerationTask.status,
            ImageSessionGenerationTask.active_attempt_id,
        ).where(ImageSessionGenerationTask.id == task_id)
    ).one_or_none()
    session.rollback()
    if state is not None and state.status == JobStatus.CANCELLED:
        raise ImageSessionGenerationCancelledError()
    if state is None or state.status != JobStatus.RUNNING or state.active_attempt_id != attempt_id:
        raise ImageSessionGenerationStaleAttemptError()


def _lock_image_generation_attempt(
    session: Session,
    *,
    task_id: str | None,
    attempt_id: str | None,
) -> ImageSessionGenerationTask | None:
    if task_id is None:
        return None
    if attempt_id is None:
        raise ImageSessionGenerationStaleAttemptError()
    task = session.scalar(
        select(ImageSessionGenerationTask)
        .where(
            ImageSessionGenerationTask.id == task_id,
            ImageSessionGenerationTask.status == JobStatus.RUNNING,
            ImageSessionGenerationTask.active_attempt_id == attempt_id,
        )
        .with_for_update()
        .execution_options(populate_existing=True)
    )
    if task is not None:
        return task
    session.rollback()
    status = session.scalar(
        select(ImageSessionGenerationTask.status).where(ImageSessionGenerationTask.id == task_id)
    )
    session.rollback()
    if status == JobStatus.CANCELLED:
        raise ImageSessionGenerationCancelledError()
    raise ImageSessionGenerationStaleAttemptError()


def _mark_image_generation_task_running(
    session: Session,
    task: ImageSessionGenerationTask,
    *,
    attempt_id: str | None = None,
) -> _ImageSessionGenerationTaskClaimResult:
    if not IMAGE_SESSION_GENERATION_TASK_CONTRACT.is_queued(task.status):
        return _ImageSessionGenerationTaskClaimResult(claimed=False)
    now = now_utc()
    if not generation_running_capacity_available(session):
        waiting = cast(
            CursorResult[Any],
            session.execute(
                update(ImageSessionGenerationTask)
                .where(
                    ImageSessionGenerationTask.id == task.id,
                    ImageSessionGenerationTask.status == JobStatus.QUEUED,
                    ImageSessionGenerationTask.active_attempt_id.is_(None),
                )
                .values(progress_phase="waiting_for_capacity", progress_updated_at=now)
                .execution_options(synchronize_session=False)
            ),
        )
        session.commit()
        return _ImageSessionGenerationTaskClaimResult(
            claimed=False,
            should_requeue=waiting.rowcount == 1,
        )
    resolved_attempt_id = attempt_id or new_id()
    result = cast(
        CursorResult[Any],
        session.execute(
            update(ImageSessionGenerationTask)
            .where(
                ImageSessionGenerationTask.id == task.id,
                ImageSessionGenerationTask.status.in_(IMAGE_SESSION_GENERATION_TASK_CONTRACT.queued_statuses),
                ImageSessionGenerationTask.active_attempt_id.is_(None),
            )
            .values(
                status=IMAGE_SESSION_GENERATION_TASK_CONTRACT.running_statuses[0],
                active_attempt_id=resolved_attempt_id,
                started_at=now,
                finished_at=None,
                failure_reason=None,
                progress_phase="running",
                progress_updated_at=now,
                active_candidate_index=None,
                provider_response_id=None,
                provider_response_status=None,
                progress_metadata=None,
                attempts=ImageSessionGenerationTask.attempts + 1,
            )
            .execution_options(synchronize_session=False)
        ),
    )
    if result.rowcount != 1:
        session.rollback()
        return _ImageSessionGenerationTaskClaimResult(claimed=False)
    session.commit()
    session.refresh(task)
    return _ImageSessionGenerationTaskClaimResult(claimed=True, attempt_id=resolved_attempt_id)


def _requeue_image_generation_task_after_capacity_wait(task_id: str) -> None:
    try:
        enqueue_async_dispatch_for_actor(
            IMAGE_SESSION_GENERATION_TASK_CONTRACT.actor_name,
            task_id,
            delay_ms=IMAGE_SESSION_CAPACITY_RETRY_DELAY_MS,
            allow_active_lease=True,
        )
    except Exception:  # noqa: BLE001
        logger.exception("连续生图等待并发容量后重新入队失败: task_id=%s", task_id)


def _handle_image_generation_task_failure(
    session: Session,
    *,
    task_id: str,
    attempt_id: str,
    reason: str,
    result_generation_group_id: str | None = None,
    failure_decision: ImageGenerationFailureDecision | None = None,
    provider_effect_uncertain: bool = False,
) -> None:
    task = _lock_image_generation_attempt(
        session,
        task_id=task_id,
        attempt_id=attempt_id,
    )
    if task is None:
        return
    if provider_effect_uncertain:
        task.progress_metadata = {
            **(task.progress_metadata if isinstance(task.progress_metadata, dict) else {}),
            "provider_effect_uncertain": True,
            "last_failure_reason": reason,
        }
        _finish_image_generation_task(
            session,
            task=task,
            status=JobStatus.UNKNOWN,
            failure_reason=IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_DETAIL,
            result_generation_group_id=result_generation_group_id,
            is_retryable=False,
            expected_attempt_id=attempt_id,
        )
        return
    retryable = failure_decision.retryable if failure_decision is not None else True
    if not retryable:
        _finish_image_generation_task(
            session,
            task=task,
            status=JobStatus.FAILED,
            failure_reason=reason,
            result_generation_group_id=result_generation_group_id,
            is_retryable=False,
            expected_attempt_id=attempt_id,
        )
        return
    if task.attempts < IMAGE_SESSION_GENERATION_MAX_ATTEMPTS:
        _reset_image_generation_task_for_retry(
            session,
            task=task,
            progress_phase="auto_retry_queued",
            result_generation_group_id=result_generation_group_id,
            progress_metadata={
                "last_failure_reason": reason,
                "last_failure_category": failure_decision.category if failure_decision is not None else "unknown",
                "last_failure_retryable": True,
                "retry_hint": failure_decision.retry_hint if failure_decision is not None else "retry_later",
                "auto_retry_attempt": task.attempts,
                "max_attempts": IMAGE_SESSION_GENERATION_MAX_ATTEMPTS,
            },
            expected_attempt_id=attempt_id,
        )
        try:
            requeue_async_dispatch(
                session,
                delivery_key=delivery_key_for_actor(IMAGE_SESSION_GENERATION_TASK_CONTRACT.actor_name, task.id),
                actor_name=IMAGE_SESSION_GENERATION_TASK_CONTRACT.actor_name,
                aggregate_id=task.id,
            )
            session.commit()
        except Exception:  # noqa: BLE001
            logger.exception("连续生图自动重试投递落库失败: task_id=%s", task.id)
            mark_image_session_generation_task_enqueue_failed(
                session,
                task_id=task_id,
                reason=QUEUE_UNAVAILABLE_DETAIL,
            )
        return

    _finish_image_generation_task(
        session,
        task=task,
        status=JobStatus.FAILED,
        failure_reason=reason,
        result_generation_group_id=result_generation_group_id,
        is_retryable=True,
        expected_attempt_id=attempt_id,
    )


def _handle_image_generation_task_failure_safely(
    session: Session,
    *,
    task_id: str,
    attempt_id: str,
    reason: str,
    result_generation_group_id: str | None = None,
    failure_decision: ImageGenerationFailureDecision | None = None,
    provider_effect_uncertain: bool = False,
) -> None:
    try:
        _handle_image_generation_task_failure(
            session,
            task_id=task_id,
            attempt_id=attempt_id,
            reason=reason,
            result_generation_group_id=result_generation_group_id,
            failure_decision=failure_decision,
            provider_effect_uncertain=provider_effect_uncertain,
        )
    except (ImageSessionGenerationCancelledError, ImageSessionGenerationStaleAttemptError):
        session.rollback()


def execute_image_session_generation_task(
    task_id: str,
    *,
    chat_service_factory: ImageChatProviderFactory | None = None,
) -> None:
    """Worker 入口：queued -> running -> succeeded/failed；重复的终态消息 no-op。"""
    session_factory = get_session_factory()
    session = session_factory()
    try:
        task = session.get(ImageSessionGenerationTask, task_id)
        if task is None:
            return
        claim = _mark_image_generation_task_running(session, task)
        if not claim.claimed:
            if claim.should_requeue:
                _requeue_image_generation_task_after_capacity_wait(task_id)
            return
        if claim.attempt_id is None:
            raise RuntimeError("claimed ImageSession generation task has no attempt id")
        try:
            _execute_image_session_round_generation(
                session,
                image_session_id=task.session_id,
                prompt=task.prompt,
                size=task.size,
                base_asset_id=task.base_asset_id,
                selected_reference_asset_ids=task.selected_reference_asset_ids or [],
                generation_count=task.generation_count,
                tool_options=task.tool_options,
                generation_task_id=task_id,
                generation_attempt_id=claim.attempt_id,
                chat_service_factory=chat_service_factory,
            )
        except ImageSessionGenerationExecutionError as exc:
            session.rollback()
            reason = exc.safe_reason or GENERIC_IMAGE_GENERATION_FAILURE
            if exc.completed_candidates > 0:
                template = PARTIAL_IMAGE_GENERATION_TIMEOUT if exc.timed_out else PARTIAL_IMAGE_GENERATION_FAILURE
                reason = template.format(
                    completed=exc.completed_candidates,
                    requested=exc.requested_candidates,
                )
            task = session.get(ImageSessionGenerationTask, task_id)
            if task is not None:
                _handle_image_generation_task_failure_safely(
                    session,
                    task_id=task.id,
                    attempt_id=claim.attempt_id,
                    reason=reason,
                    result_generation_group_id=exc.generation_group_id,
                    failure_decision=exc.failure_decision,
                    provider_effect_uncertain=exc.provider_effect_uncertain,
                )
            return
        except (ImageSessionGenerationCancelledError, ImageSessionGenerationStaleAttemptError):
            session.rollback()
            return
        except BaseException as exc:  # noqa: BLE001
            if isinstance(exc, (KeyboardInterrupt, SystemExit)):
                raise
            session.rollback()
            _handle_image_generation_task_failure_safely(
                session,
                task_id=task_id,
                attempt_id=claim.attempt_id,
                reason=GENERIC_IMAGE_GENERATION_FAILURE,
                failure_decision=classify_image_generation_failure(
                    exc,
                    generic_message=GENERIC_IMAGE_GENERATION_FAILURE,
                ),
            )
            return
    finally:
        session.close()


def attach_image_session_asset_to_product_canonical(
    session: Session,
    *,
    image_session_id: str,
    asset_id: str,
    product_id: str,
) -> ProductImageAsset:
    """把会话生成图附加为商品图片身份，共享 MediaObject，不复制 bytes。"""

    image_session = _get_image_session_or_raise(session, image_session_id)
    asset = session.scalar(
        select(ImageSessionAsset)
        .where(ImageSessionAsset.id == asset_id, ImageSessionAsset.session_id == image_session.id)
        .with_for_update()
    )
    if asset is None:
        raise NotFoundError("会话图片不存在")
    if asset.kind != ImageSessionAssetKind.GENERATED_IMAGE:
        raise BusinessValidationError("只有生成结果可以附加到商品")
    product = session.get(Product, product_id)
    if product is None:
        raise NotFoundError("商品不存在")

    media = session.get(MediaObject, asset.media_object_id)
    if media is None:
        raise ConflictError("会话图片引用的媒体对象不存在")
    if media.verification_status == MediaVerificationStatus.MISSING:
        raise BusinessValidationError("会话图片文件缺失，不能附加到商品")
    existing = session.scalar(
        select(ProductImageAsset).where(
            ProductImageAsset.product_id == product_id,
            ProductImageAsset.source_image_session_asset_id == asset.id,
        )
    )
    if existing is None:
        existing = stage_product_image_identity(
            session,
            product=product,
            media_object=media,
            origin_type=ProductImageOriginType.IMAGE_SESSION_ATTACH,
            display_name=asset.original_filename,
            original_filename=asset.original_filename,
            source_image_session_asset_id=asset.id,
        )
        product.updated_at = now_utc()
    session.commit()
    session.expire_all()
    return get_product_image_asset(session, existing.id)
