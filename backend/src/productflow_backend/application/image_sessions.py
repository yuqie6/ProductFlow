from __future__ import annotations

from base64 import b64encode
from typing import Literal

from sqlalchemy import desc, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import ImageSessionAssetKind, SourceAssetKind
from productflow_backend.domain.errors import NotFoundError
from productflow_backend.infrastructure.db.models import (
    ImageSession,
    ImageSessionAsset,
    ImageSessionRound,
    Product,
    SourceAsset,
)
from productflow_backend.infrastructure.image.base import infer_extension
from productflow_backend.infrastructure.image.chat_service import ImageChatService, ImageChatTurn
from productflow_backend.infrastructure.storage import LocalStorage

ATTACH_TARGET = Literal["reference", "main_source"]
DEFAULT_SESSION_TITLE = "未命名会话"
DEFAULT_ASSISTANT_MESSAGE = "已基于当前对话继续生成一张新图，你可以继续补充修改要求。"



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


def _get_session_reference_assets(image_session: ImageSession) -> list[ImageSessionAsset]:
    return sorted(
        [asset for asset in image_session.assets if asset.kind == ImageSessionAssetKind.REFERENCE_UPLOAD],
        key=lambda item: item.created_at,
        reverse=True,
    )


def _get_product_original_assets(product: Product) -> list[SourceAsset]:
    return sorted(
        [asset for asset in product.source_assets if asset.kind == SourceAssetKind.ORIGINAL_IMAGE],
        key=lambda item: item.created_at,
        reverse=True,
    )


def _get_product_reference_assets(product: Product) -> list[SourceAsset]:
    return sorted(
        [asset for asset in product.source_assets if asset.kind == SourceAssetKind.REFERENCE_IMAGE],
        key=lambda item: item.created_at,
        reverse=True,
    )


def _build_generation_context(
    image_session: ImageSession,
    storage: LocalStorage,
) -> tuple[list[ImageChatTurn], list[str], str | None]:
    """构建生图请求上下文：历史轮次 + 参考图 + response_id 链。"""
    history: list[ImageChatTurn] = []
    rounds = sorted(image_session.rounds, key=lambda item: item.created_at)
    previous_response_id = next(
        (round_item.provider_response_id for round_item in reversed(rounds) if round_item.provider_response_id),
        None,
    )
    rounds_for_text = [] if previous_response_id else rounds[-8:]
    round_ids_for_images = set() if previous_response_id else {round_item.id for round_item in rounds[-3:]}
    for round_item in rounds_for_text:
        history.append(ImageChatTurn(role="user", content=round_item.prompt))
        image_data_url = None
        if round_item.id in round_ids_for_images:
            image_data_url = _session_data_url(
                storage,
                round_item.generated_asset.storage_path,
                round_item.generated_asset.mime_type,
            )
        history.append(
            ImageChatTurn(
                role="assistant",
                content=round_item.assistant_message,
                image_data_url=image_data_url,
            )
        )

    manual_references: list[str] = []
    if image_session.product is not None:
        originals = _get_product_original_assets(image_session.product)[:1]
        product_refs = _get_product_reference_assets(image_session.product)[:1]
        for asset in originals + product_refs:
            manual_references.append(_session_data_url(storage, asset.storage_path, asset.mime_type))

    for asset in _get_session_reference_assets(image_session)[:4]:
        manual_references.append(_session_data_url(storage, asset.storage_path, asset.mime_type))

    return history, manual_references, previous_response_id


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
    storage: LocalStorage | None = None,
) -> ImageSession:
    """执行一轮生图，调用 AI 并保存结果到会话。"""
    image_session = _get_image_session_or_raise(session, image_session_id)
    storage = storage or LocalStorage()
    history, manual_references, previous_response_id = _build_generation_context(image_session, storage)

    result = ImageChatService().generate(
        prompt=prompt,
        size=size,
        history=history,
        manual_reference_images=manual_references,
        previous_response_id=previous_response_id,
    )

    relative_path = storage.save_image_session_generated(
        image_session.id,
        result.bytes_data,
        suffix=infer_extension(result.mime_type),
    )
    asset = ImageSessionAsset(
        session_id=image_session.id,
        kind=ImageSessionAssetKind.GENERATED_IMAGE,
        original_filename=f"generated-{now_utc().strftime('%Y%m%d-%H%M%S')}{infer_extension(result.mime_type)}",
        mime_type=result.mime_type,
        storage_path=relative_path,
    )
    session.add(asset)
    session.flush()

    round_item = ImageSessionRound(
        session_id=image_session.id,
        prompt=prompt.strip(),
        assistant_message=DEFAULT_ASSISTANT_MESSAGE,
        size=size,
        model_name=result.model_name,
        provider_name=result.provider_name,
        prompt_version=result.prompt_version,
        provider_response_id=result.provider_response_id,
        previous_response_id=result.previous_response_id,
        image_generation_call_id=result.image_generation_call_id,
        provider_request_json=result.provider_request_json,
        provider_output_json=result.provider_output_json,
        generated_asset_id=asset.id,
    )
    session.add(round_item)
    if not image_session.rounds and image_session.title == DEFAULT_SESSION_TITLE:
        image_session.title = _trim_title(prompt)
    image_session.updated_at = now_utc()
    session.commit()
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
