from __future__ import annotations

from base64 import b64encode
from contextlib import suppress
from typing import Literal

from sqlalchemy import desc, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.time import now_utc
from productflow_backend.config import normalize_image_generation_size
from productflow_backend.domain.enums import ImageSessionAssetKind, SourceAssetKind
from productflow_backend.domain.errors import NotFoundError
from productflow_backend.infrastructure.db.models import (
    ImageSession,
    ImageSessionAsset,
    ImageSessionRound,
    Product,
    SourceAsset,
    new_id,
)
from productflow_backend.infrastructure.image.base import infer_extension
from productflow_backend.infrastructure.image.chat_service import ImageChatService, ImageChatTurn
from productflow_backend.infrastructure.storage import LocalStorage

ATTACH_TARGET = Literal["reference", "main_source"]
DEFAULT_SESSION_TITLE = "未命名会话"
DEFAULT_ASSISTANT_MESSAGE = "已按本轮选择的图片上下文生成候选，你可以从任意候选继续。"
MAX_BRANCH_CONTEXT_IMAGES = 6


def _image_session_query():
    return (
        select(ImageSession)
        .options(
            selectinload(ImageSession.assets),
            selectinload(ImageSession.rounds).selectinload(ImageSessionRound.generated_asset),
            selectinload(ImageSession.product).selectinload(Product.source_assets),
        )
        .order_by(desc(ImageSession.updated_at))
    )


def _get_image_session_or_raise(session: Session, image_session_id: str) -> ImageSession:
    image_session = session.scalar(_image_session_query().where(ImageSession.id == image_session_id))
    if image_session is None:
        raise NotFoundError("连续生图会话不存在")
    return image_session


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


def generate_image_session_round(
    session: Session,
    *,
    image_session_id: str,
    prompt: str,
    size: str,
    base_asset_id: str | None = None,
    selected_reference_asset_ids: list[str] | None = None,
    generation_count: int = 1,
    storage: LocalStorage | None = None,
) -> ImageSession:
    """执行一轮生图，调用 AI 并保存结果到会话。"""
    image_session = _get_image_session_or_raise(session, image_session_id)
    storage = storage or LocalStorage()
    if not 1 <= generation_count <= 4:
        raise ValueError("一次生成数量必须在 1-4 张之间")
    normalized_size = normalize_image_generation_size(size)
    (
        history,
        manual_references,
        previous_response_id,
        normalized_base_asset_id,
        normalized_reference_ids,
    ) = _build_branch_generation_context(
        image_session,
        storage,
        base_asset_id=base_asset_id,
        selected_reference_asset_ids=selected_reference_asset_ids,
    )

    generation_group_id = new_id()
    service = ImageChatService()
    saved_generated_paths: list[str] = []
    try:
        for candidate_index in range(1, generation_count + 1):
            result = service.generate(
                prompt=prompt,
                size=normalized_size,
                history=history,
                manual_reference_images=manual_references,
                previous_response_id=previous_response_id,
            )

            relative_path = storage.save_image_session_generated(
                image_session.id,
                result.bytes_data,
                suffix=infer_extension(result.mime_type),
            )
            saved_generated_paths.append(relative_path)
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
                provider_output_json=result.provider_output_json,
                generation_group_id=generation_group_id,
                candidate_index=candidate_index,
                candidate_count=generation_count,
                base_asset_id=normalized_base_asset_id,
                selected_reference_asset_ids=normalized_reference_ids,
                generated_asset_id=asset.id,
            )
            session.add(round_item)
        if not image_session.rounds and image_session.title == DEFAULT_SESSION_TITLE:
            image_session.title = _trim_title(prompt)
        image_session.updated_at = now_utc()
        session.commit()
    except Exception:
        session.rollback()
        for path in saved_generated_paths:
            with suppress(ValueError, OSError):
                storage.delete_image_with_variants(path)
        raise
    session.expire_all()
    return _get_image_session_or_raise(session, image_session.id)


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
