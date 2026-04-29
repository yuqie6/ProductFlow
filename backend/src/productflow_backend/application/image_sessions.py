from __future__ import annotations

from base64 import b64encode
from collections.abc import Callable
from contextlib import suppress
from dataclasses import dataclass
from typing import Any, Literal

from dramatiq.middleware.time_limit import TimeLimitExceeded
from sqlalchemy import desc, func, select, update
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.admission import (
    ensure_generation_capacity,
    get_generation_queue_overview,
    get_generation_task_queue_metadata,
    get_queued_generation_positions,
)
from productflow_backend.application.queue_submission import enqueue_or_mark_failed
from productflow_backend.application.time import now_utc
from productflow_backend.config import normalize_image_generation_size
from productflow_backend.domain.enums import ImageSessionAssetKind, JobStatus, SourceAssetKind
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    ImageSession,
    ImageSessionAsset,
    ImageSessionGenerationTask,
    ImageSessionRound,
    Product,
    SourceAsset,
    new_id,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.image.base import image_dimensions_from_bytes, infer_extension
from productflow_backend.infrastructure.image.chat_service import ImageChatService, ImageChatTurn
from productflow_backend.infrastructure.queue import enqueue_image_session_generation_task
from productflow_backend.infrastructure.storage import LocalStorage

ATTACH_TARGET = Literal["reference", "main_source"]
DEFAULT_SESSION_TITLE = "未命名会话"
DEFAULT_ASSISTANT_MESSAGE = "已按本轮选择的图片上下文生成候选，你可以从任意候选继续。"
MAX_BRANCH_CONTEXT_IMAGES = 6
GENERIC_IMAGE_GENERATION_FAILURE = "图片生成失败，请稍后重试"
PARTIAL_IMAGE_GENERATION_FAILURE = "已生成 {completed}/{requested} 张候选，后续生成失败，请重新发起生成补齐。"
PARTIAL_IMAGE_GENERATION_TIMEOUT = "已生成 {completed}/{requested} 张候选，但任务超时，剩余候选未完成。"
UNSUPPORTED_IMAGE_TOOL_OPTION_KEYS = {"background"}


@dataclass(frozen=True, slots=True)
class ImageSessionGenerationTaskCreationResult:
    task: ImageSessionGenerationTask
    image_session: ImageSession


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


def _image_session_query():
    return (
        select(ImageSession)
        .options(
            selectinload(ImageSession.assets),
            selectinload(ImageSession.rounds).selectinload(ImageSessionRound.generated_asset),
            selectinload(ImageSession.generation_tasks),
            selectinload(ImageSession.product).selectinload(Product.source_assets),
        )
        .order_by(desc(ImageSession.updated_at))
    )


def _image_session_status_query():
    return select(ImageSession).options(selectinload(ImageSession.generation_tasks))


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
        task._queue_metadata = metadata


def _get_product_or_raise(session: Session, product_id: str) -> Product:
    product = session.scalar(
        select(Product).options(selectinload(Product.source_assets)).where(Product.id == product_id)
    )
    if product is None:
        raise NotFoundError("商品不存在")
    return product


def _session_data_url(storage: LocalStorage, path: str, mime_type: str) -> str:
    raw = storage.resolve(path).read_bytes()
    encoded = b64encode(raw).decode("utf-8")
    return f"data:{mime_type};base64,{encoded}"


def _trim_title(prompt: str) -> str:
    compact = " ".join(prompt.strip().split())
    return compact[:32] + ("..." if len(compact) > 32 else "")


def _get_product_original_assets(product: Product) -> list[SourceAsset]:
    return sorted(
        [asset for asset in product.source_assets if asset.kind == SourceAssetKind.ORIGINAL_IMAGE],
        key=lambda item: item.created_at,
        reverse=True,
    )


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
            raise ValueError("只能从会话生成图继续")
        raise ValueError("只能选择会话参考图参与本轮生成")
    return asset


def _unique_ids(ids: list[str] | None) -> list[str]:
    seen: set[str] = set()
    values: list[str] = []
    for item in ids or []:
        if item in seen:
            continue
        seen.add(item)
        values.append(item)
    return values


def _has_prior_generation_request(
    image_session: ImageSession,
    *,
    current_generation_task_id: str | None = None,
) -> bool:
    if image_session.rounds:
        return True
    if current_generation_task_id is None:
        return bool(image_session.generation_tasks)

    tasks = sorted(image_session.generation_tasks, key=lambda task: (task.created_at, task.id))
    if not tasks:
        return False
    return tasks[0].id != current_generation_task_id


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
        raise ValueError("本轮最多选择 6 张图片上下文（含分支基图）")

    if base_asset_id:
        base_asset = _find_session_asset_or_raise(
            image_session,
            base_asset_id,
            expected_kind=ImageSessionAssetKind.GENERATED_IMAGE,
        )
        normalized_base_asset_id = base_asset.id
        manual_references.append(_session_data_url(storage, base_asset.storage_path, base_asset.mime_type))

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
            _session_data_url(storage, reference_asset.storage_path, reference_asset.mime_type)
        )

    return [], manual_references[:6], None, normalized_base_asset_id, normalized_reference_ids


def _validate_generation_request(
    image_session: ImageSession,
    *,
    size: str,
    base_asset_id: str | None,
    selected_reference_asset_ids: list[str] | None,
    generation_count: int,
    tool_options: dict[str, Any] | None = None,
    current_generation_task_id: str | None = None,
) -> tuple[str, str | None, list[str]]:
    if not 1 <= generation_count <= 4:
        raise ValueError("一次生成数量必须在 1-4 张之间")
    normalized_size = normalize_image_generation_size(size)
    selected_reference_ids = _unique_ids(selected_reference_asset_ids)
    if (1 if base_asset_id else 0) + len(selected_reference_ids) > MAX_BRANCH_CONTEXT_IMAGES:
        raise ValueError("本轮最多选择 6 张图片上下文（含分支基图）")

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
    if not tool_options:
        return None
    normalized = {
        key: value
        for key, value in tool_options.items()
        if key not in UNSUPPORTED_IMAGE_TOOL_OPTION_KEYS
        and value is not None
        and not (isinstance(value, str) and not value.strip())
    }
    return normalized or None


def _provider_output_with_actual_size(
    provider_output_json: dict[str, Any] | None,
    *,
    requested_size: str,
    image_bytes: bytes,
) -> dict[str, Any]:
    output = dict(provider_output_json or {})
    dimensions = image_dimensions_from_bytes(image_bytes)
    if dimensions is None:
        return output

    actual_size = f"{dimensions[0]}x{dimensions[1]}"
    metadata = output.get("_productflow")
    metadata = dict(metadata) if isinstance(metadata, dict) else {}
    metadata["actual_image_size"] = actual_size
    if actual_size != requested_size:
        raw_notes = metadata.get("notes")
        notes = [note for note in raw_notes if isinstance(note, dict)] if isinstance(raw_notes, list) else []
        if not any(note.get("kind") == "actual_size_mismatch" for note in notes):
            notes.append(
                {
                    "kind": "actual_size_mismatch",
                    "message": f"供应商实际返回 {actual_size}，请求尺寸为 {requested_size}。",
                    "requested_size": requested_size,
                    "actual_size": actual_size,
                }
            )
        metadata["notes"] = notes
    output["_productflow"] = metadata
    return output


def list_image_sessions(
    session: Session,
    *,
    product_id: str | None = None,
) -> list[ImageSession]:
    stmt = _image_session_query()
    if product_id is None:
        stmt = stmt.where(ImageSession.product_id.is_(None))
    else:
        stmt = stmt.where(ImageSession.product_id == product_id)
    return list(session.scalars(stmt).all())


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
    product_id: str | None,
    title: str | None = None,
) -> ImageSession:
    if product_id:
        _get_product_or_raise(session, product_id)
    normalized_title = (title or DEFAULT_SESSION_TITLE).strip() or DEFAULT_SESSION_TITLE
    image_session = ImageSession(product_id=product_id, title=normalized_title)
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
    image_session = _get_image_session_or_raise(session, image_session_id)
    storage = storage or LocalStorage()
    session.delete(image_session)
    session.commit()
    storage.delete_image_session_tree(image_session_id)


def add_image_session_reference_images(
    session: Session,
    *,
    image_session_id: str,
    reference_image_uploads: list[tuple[bytes, str, str]],
    storage: LocalStorage | None = None,
) -> ImageSession:
    image_session = _get_image_session_or_raise(session, image_session_id)
    storage = storage or LocalStorage()
    for content, filename, mime_type in reference_image_uploads:
        relative_path = storage.save_image_session_reference(image_session.id, filename, content)
        session.add(
            ImageSessionAsset(
                session_id=image_session.id,
                kind=ImageSessionAssetKind.REFERENCE_UPLOAD,
                original_filename=filename,
                mime_type=mime_type or "application/octet-stream",
                storage_path=relative_path,
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
        raise ValueError("只能删除会话参考图")

    storage = storage or LocalStorage()
    storage_path = asset.storage_path
    session.delete(asset)
    image_session.updated_at = now_utc()
    session.commit()
    storage.delete_image_with_variants(storage_path)
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
) -> ImageSessionRoundGenerationResult:
    """执行一轮生图，调用 AI 并保存结果到会话。"""
    image_session = _get_image_session_or_raise(session, image_session_id)
    storage = storage or LocalStorage()
    normalized_size, normalized_base_asset_id, normalized_reference_ids = _validate_generation_request(
        image_session,
        size=size,
        base_asset_id=base_asset_id,
        selected_reference_asset_ids=selected_reference_asset_ids,
        generation_count=generation_count,
        tool_options=tool_options,
        current_generation_task_id=generation_task_id,
    )
    normalized_tool_options = _normalize_tool_options(tool_options)
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

    generation_group_id = new_id()
    service = ImageChatService()
    should_update_default_title = not image_session.rounds and image_session.title == DEFAULT_SESSION_TITLE
    completed_candidates = 0

    for candidate_index in range(1, generation_count + 1):
        relative_path: str | None = None
        try:
            if generation_task_id is not None:
                _update_image_generation_task_progress(
                    session,
                    task_id=generation_task_id,
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
            result = service.generate(
                prompt=prompt,
                size=normalized_size,
                history=history,
                manual_reference_images=manual_references,
                previous_response_id=previous_response_id,
                tool_options=normalized_tool_options,
                progress_callback=_provider_progress_callback(
                    session,
                    task_id=generation_task_id,
                    candidate_index=candidate_index,
                    generation_count=generation_count,
                    completed_candidates=completed_candidates,
                ),
            )

            relative_path = storage.save_image_session_generated(
                image_session.id,
                result.bytes_data,
                suffix=infer_extension(result.mime_type),
            )
            asset = ImageSessionAsset(
                session_id=image_session.id,
                kind=ImageSessionAssetKind.GENERATED_IMAGE,
                original_filename=(
                    f"generated-{now_utc().strftime('%Y%m%d-%H%M%S')}"
                    f"-{candidate_index}{infer_extension(result.mime_type)}"
                ),
                mime_type=result.mime_type,
                storage_path=relative_path,
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
                prompt=prompt.strip(),
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
            if should_update_default_title:
                image_session.title = _trim_title(prompt)
                should_update_default_title = False
            image_session.updated_at = now_utc()
            if generation_task_id is not None:
                task = session.get(ImageSessionGenerationTask, generation_task_id)
                if task is not None:
                    task.completed_candidates = candidate_index
                    task.active_candidate_index = None
                    task.progress_phase = "candidate_saved"
                    task.progress_updated_at = now_utc()
                    task.result_generation_group_id = generation_group_id
                    task.progress_metadata = {
                        "candidate_index": candidate_index,
                        "candidate_count": generation_count,
                        "generated_asset_id": asset.id,
                        "round_id": round_item.id,
                    }
                if task is not None and candidate_index == generation_count:
                    _finish_image_generation_task(
                        session,
                        task=task,
                        status=JobStatus.SUCCEEDED,
                        result_generation_group_id=generation_group_id,
                        is_retryable=False,
                    )
                else:
                    session.commit()
            else:
                session.commit()
            completed_candidates += 1
        except BaseException as exc:  # noqa: BLE001
            session.rollback()
            if relative_path is not None:
                with suppress(ValueError, OSError):
                    storage.delete_image_with_variants(relative_path)
            if isinstance(exc, (KeyboardInterrupt, SystemExit)):
                raise
            if generation_task_id is None:
                raise
            raise ImageSessionGenerationExecutionError(
                completed_candidates=completed_candidates,
                requested_candidates=generation_count,
                generation_group_id=generation_group_id if completed_candidates else None,
                timed_out=isinstance(exc, TimeLimitExceeded),
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
) -> ImageSessionGenerationTaskCreationResult:
    """校验并创建连续生图 durable 任务；不调用 provider。"""
    image_session = _get_image_session_or_raise(session, image_session_id)
    normalized_size, normalized_base_asset_id, normalized_reference_ids = _validate_generation_request(
        image_session,
        size=size,
        base_asset_id=base_asset_id,
        selected_reference_asset_ids=selected_reference_asset_ids,
        generation_count=generation_count,
        tool_options=tool_options,
    )
    normalized_tool_options = _normalize_tool_options(tool_options)
    ensure_generation_capacity(session)
    task = ImageSessionGenerationTask(
        session_id=image_session.id,
        status=JobStatus.QUEUED,
        prompt=prompt.strip(),
        size=normalized_size,
        base_asset_id=normalized_base_asset_id,
        selected_reference_asset_ids=normalized_reference_ids,
        tool_options=normalized_tool_options,
        generation_count=generation_count,
    )
    session.add(task)
    image_session.updated_at = now_utc()
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
    )
    enqueue_or_mark_failed(
        result.task.id,
        enqueue=enqueue or enqueue_image_session_generation_task,
        mark_failed=lambda task_id, reason: mark_image_session_generation_task_enqueue_failed(
            session,
            task_id=task_id,
            reason=reason,
        ),
    )
    session.expire_all()
    return get_image_session_detail(session, image_session_id)


def mark_image_session_generation_task_enqueue_failed(session: Session, *, task_id: str, reason: str) -> None:
    task = session.get(ImageSessionGenerationTask, task_id)
    if task is None:
        return
    task.status = JobStatus.FAILED
    task.failure_reason = reason[:1000]
    task.finished_at = now_utc()
    task.progress_phase = "enqueue_failed"
    task.progress_updated_at = task.finished_at
    task.is_retryable = True
    image_session = session.get(ImageSession, task.session_id)
    if image_session is not None:
        image_session.updated_at = now_utc()
    session.commit()


def _finish_image_generation_task(
    session: Session,
    *,
    task: ImageSessionGenerationTask,
    status: JobStatus,
    failure_reason: str | None = None,
    result_generation_group_id: str | None = None,
    is_retryable: bool,
) -> None:
    now = now_utc()
    task.status = status
    task.failure_reason = failure_reason[:1000] if failure_reason else None
    task.result_generation_group_id = result_generation_group_id
    task.is_retryable = is_retryable
    task.finished_at = now
    task.active_candidate_index = None
    task.progress_updated_at = now
    task.progress_phase = "succeeded" if status == JobStatus.SUCCEEDED else "failed"
    image_session = session.get(ImageSession, task.session_id)
    if image_session is not None:
        image_session.updated_at = now
    session.commit()


def _update_image_generation_task_progress(
    session: Session,
    *,
    task_id: str,
    phase: str,
    completed_candidates: int | None = None,
    active_candidate_index: int | None = None,
    provider_response_id: str | None = None,
    provider_response_status: str | None = None,
    progress_metadata: dict[str, Any] | None = None,
    result_generation_group_id: str | None = None,
    clear_provider_response: bool = False,
    commit: bool = True,
) -> None:
    task = session.get(ImageSessionGenerationTask, task_id)
    if task is None or task.status not in {JobStatus.QUEUED, JobStatus.RUNNING}:
        return
    task.progress_phase = phase[:64]
    task.progress_updated_at = now_utc()
    if completed_candidates is not None:
        task.completed_candidates = completed_candidates
    task.active_candidate_index = active_candidate_index
    if clear_provider_response:
        task.provider_response_id = None
        task.provider_response_status = None
    elif provider_response_id is not None:
        task.provider_response_id = provider_response_id[:255]
        if provider_response_status is not None:
            task.provider_response_status = provider_response_status[:64]
    elif provider_response_status is not None:
        task.provider_response_status = provider_response_status[:64]
    if progress_metadata is not None:
        task.progress_metadata = progress_metadata
    if result_generation_group_id is not None:
        task.result_generation_group_id = result_generation_group_id
    if commit:
        session.commit()


def _provider_progress_callback(
    session: Session,
    *,
    task_id: str | None,
    candidate_index: int,
    generation_count: int,
    completed_candidates: int,
) -> Callable[[dict[str, Any]], None] | None:
    if task_id is None:
        return None

    def callback(progress: dict[str, Any]) -> None:
        _update_image_generation_task_progress(
            session,
            task_id=task_id,
            phase="provider_polling",
            completed_candidates=completed_candidates,
            active_candidate_index=candidate_index,
            provider_response_id=progress.get("provider_response_id"),
            provider_response_status=progress.get("provider_response_status"),
            progress_metadata={
                "candidate_index": candidate_index,
                "candidate_count": generation_count,
                "provider_response": progress.get("provider_response"),
            },
        )

    return callback


def _mark_image_generation_task_running(session: Session, task: ImageSessionGenerationTask) -> bool:
    if task.status != JobStatus.QUEUED:
        return False
    result = session.execute(
        update(ImageSessionGenerationTask)
        .where(
            ImageSessionGenerationTask.id == task.id,
            ImageSessionGenerationTask.status == JobStatus.QUEUED,
        )
        .values(
            status=JobStatus.RUNNING,
            started_at=now_utc(),
            finished_at=None,
            failure_reason=None,
            progress_phase="running",
            progress_updated_at=now_utc(),
            completed_candidates=0,
            active_candidate_index=None,
            provider_response_id=None,
            provider_response_status=None,
            progress_metadata=None,
            attempts=ImageSessionGenerationTask.attempts + 1,
        )
    )
    if result.rowcount != 1:
        session.rollback()
        return False
    session.commit()
    session.refresh(task)
    return True


def _mark_image_generation_task_failed(session: Session, *, task_id: str, reason: str) -> None:
    task = session.get(ImageSessionGenerationTask, task_id)
    if task is None:
        return
    _finish_image_generation_task(
        session,
        task=task,
        status=JobStatus.FAILED,
        failure_reason=reason,
        is_retryable=False,
    )


def execute_image_session_generation_task(task_id: str) -> None:
    """Worker entry: queued -> running -> succeeded/failed; duplicate terminal messages no-op."""
    session_factory = get_session_factory()
    session = session_factory()
    try:
        task = session.get(ImageSessionGenerationTask, task_id)
        if task is None or not _mark_image_generation_task_running(session, task):
            return
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
            )
        except ImageSessionGenerationExecutionError as exc:
            session.rollback()
            reason = GENERIC_IMAGE_GENERATION_FAILURE
            if exc.completed_candidates > 0:
                template = PARTIAL_IMAGE_GENERATION_TIMEOUT if exc.timed_out else PARTIAL_IMAGE_GENERATION_FAILURE
                reason = template.format(
                    completed=exc.completed_candidates,
                    requested=exc.requested_candidates,
                )
            task = session.get(ImageSessionGenerationTask, task_id)
            if task is not None:
                _finish_image_generation_task(
                    session,
                    task=task,
                    status=JobStatus.FAILED,
                    failure_reason=reason,
                    result_generation_group_id=exc.generation_group_id,
                    is_retryable=False,
                )
            return
        except BaseException as exc:  # noqa: BLE001
            if isinstance(exc, (KeyboardInterrupt, SystemExit)):
                raise
            session.rollback()
            _mark_image_generation_task_failed(
                session,
                task_id=task_id,
                reason=GENERIC_IMAGE_GENERATION_FAILURE,
            )
            return
    finally:
        session.close()


def attach_image_session_asset_to_product(
    session: Session,
    *,
    image_session_id: str,
    asset_id: str,
    target: ATTACH_TARGET,
    product_id: str | None,
    storage: LocalStorage | None = None,
) -> Product:
    """将生图结果写回商品（设为参考图或替换主图）。"""
    image_session = _get_image_session_or_raise(session, image_session_id)
    asset = next((item for item in image_session.assets if item.id == asset_id), None)
    if asset is None:
        raise NotFoundError("会话图片不存在")
    if asset.kind != ImageSessionAssetKind.GENERATED_IMAGE:
        raise ValueError("只有生成结果可以写回商品")

    resolved_product_id = product_id or image_session.product_id
    if not resolved_product_id:
        raise ValueError("请选择要写回的商品")
    product = _get_product_or_raise(session, resolved_product_id)

    storage = storage or LocalStorage()
    image_bytes = storage.resolve(asset.storage_path).read_bytes()

    if target == "reference":
        relative_path = storage.save_reference_upload(product.id, asset.original_filename, image_bytes)
        session.add(
            SourceAsset(
                product_id=product.id,
                kind=SourceAssetKind.REFERENCE_IMAGE,
                original_filename=asset.original_filename,
                mime_type=asset.mime_type,
                storage_path=relative_path,
            )
        )
    else:
        for current_source in _get_product_original_assets(product):
            current_source.kind = SourceAssetKind.REFERENCE_IMAGE
        session.flush()
        relative_path = storage.save_product_upload(product.id, asset.original_filename, image_bytes)
        session.add(
            SourceAsset(
                product_id=product.id,
                kind=SourceAssetKind.ORIGINAL_IMAGE,
                original_filename=asset.original_filename,
                mime_type=asset.mime_type,
                storage_path=relative_path,
            )
        )
    product.updated_at = now_utc()
    session.commit()
    session.expire_all()
    return _get_product_or_raise(session, product.id)
