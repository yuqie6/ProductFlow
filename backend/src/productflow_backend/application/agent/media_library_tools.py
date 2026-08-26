"""Global media-library and product-directory Agent tools."""

from __future__ import annotations

import base64
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

from sqlalchemy import and_, or_, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent.agent_context import _require_global_conversation
from productflow_backend.application.agent.conversations import get_agent_conversation_by_id_or_raise
from productflow_backend.application.agent.gallery_tools import (
    AGENT_ASSET_LIST_DEFAULT_LIMIT,
    AGENT_ASSET_MAX_BYTES,
    AgentAssetContent,
    AgentAssetPage,
)
from productflow_backend.application.agent.turn_projection import AGENT_MAX_INPUT_ASSETS
from productflow_backend.application.media_library.queries import (
    get_media_library_asset,
    list_media_library_assets,
)
from productflow_backend.application.media_library.service import validate_media_library_asset_for_use
from productflow_backend.application.media_objects import inspect_image_bytes
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import MediaLibraryAsset, Product, WorkflowGraph
from productflow_backend.infrastructure.storage import LocalStorage

AGENT_GLOBAL_PRODUCT_LIST_MAX_LIMIT = 100
AGENT_GLOBAL_PRODUCT_INSPECT_MAX = 20


@dataclass(frozen=True, slots=True)
class AgentProductPage:
    items: list[dict[str, Any]]
    next_cursor: str | None


def list_agent_global_media_assets(
    session: Session,
    *,
    conversation_id: str,
    query: str = "",
    cursor: str | None = None,
    limit: int = AGENT_ASSET_LIST_DEFAULT_LIMIT,
) -> AgentAssetPage:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_global_conversation(conversation)
    page = list_media_library_assets(
        session,
        limit=limit,
        cursor=cursor,
        include_archived=False,
        search=query,
    )
    return AgentAssetPage(
        items=[agent_media_library_asset_metadata(asset) for asset in page.items],
        next_cursor=page.next_cursor,
    )


def list_agent_global_products(
    session: Session,
    *,
    conversation_id: str,
    query: str = "",
    cursor: str | None = None,
    limit: int = AGENT_ASSET_LIST_DEFAULT_LIMIT,
) -> AgentProductPage:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_global_conversation(conversation)
    normalized_query = query.strip()
    if len(normalized_query) > 255:
        raise BusinessValidationError("商品搜索词不能超过 255 个字符")
    if not 1 <= limit <= AGENT_GLOBAL_PRODUCT_LIST_MAX_LIMIT:
        raise BusinessValidationError(
            f"商品分页 limit 必须在 1 到 {AGENT_GLOBAL_PRODUCT_LIST_MAX_LIMIT} 之间"
        )

    statement = select(Product).where(
        Product.name.icontains(normalized_query, autoescape=True)
        if normalized_query
        else True
    )
    if cursor:
        cursor_updated_at, cursor_product_id = _decode_global_product_cursor(
            cursor,
            query=normalized_query,
        )
        statement = statement.where(
            or_(
                Product.updated_at < cursor_updated_at,
                and_(
                    Product.updated_at == cursor_updated_at,
                    Product.id < cursor_product_id,
                ),
            )
        )
    rows = list(
        session.scalars(
            statement.order_by(Product.updated_at.desc(), Product.id.desc()).limit(limit + 1)
        ).all()
    )
    has_more = len(rows) > limit
    items = rows[:limit]
    summaries = _agent_global_product_summaries(session, items)
    next_cursor = (
        _encode_global_product_cursor(
            updated_at=items[-1].updated_at,
            product_id=items[-1].id,
            query=normalized_query,
        )
        if has_more and items
        else None
    )
    return AgentProductPage(items=summaries, next_cursor=next_cursor)


def inspect_agent_global_products(
    session: Session,
    *,
    conversation_id: str,
    product_ids: list[str],
) -> list[dict[str, Any]]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_global_conversation(conversation)
    normalized_ids = _normalize_global_product_ids(product_ids)
    products = list(
        session.scalars(select(Product).where(Product.id.in_(normalized_ids))).all()
    )
    if len(products) != len(normalized_ids):
        raise NotFoundError("部分商品不存在")
    summaries = _agent_global_product_summaries(session, products)
    by_id = {item["id"]: item for item in summaries}
    return [by_id[product_id] for product_id in normalized_ids]


def inspect_agent_global_media_assets(
    session: Session,
    *,
    conversation_id: str,
    asset_ids: list[str],
) -> list[dict[str, Any]]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_global_conversation(conversation)
    normalized_ids = _normalize_explicit_asset_ids(asset_ids)
    assets = list(
        session.scalars(
            select(MediaLibraryAsset)
            .options(
                selectinload(MediaLibraryAsset.media_object),
                selectinload(MediaLibraryAsset.folder),
            )
            .where(
                MediaLibraryAsset.id.in_(normalized_ids),
                MediaLibraryAsset.is_archived.is_(False),
            )
        ).all()
    )
    if len(assets) != len(normalized_ids):
        raise NotFoundError("全局素材不存在或已归档")
    by_id = {asset.id: asset for asset in assets}
    for asset in assets:
        validate_media_library_asset_for_use(asset)
    return [agent_media_library_asset_metadata(by_id[asset_id]) for asset_id in normalized_ids]


def read_agent_global_media_asset_content(
    session: Session,
    *,
    conversation_id: str,
    asset_id: str,
    storage: LocalStorage | None = None,
) -> AgentAssetContent:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_global_conversation(conversation)
    asset = get_media_library_asset(session, asset_id=asset_id.strip())
    media = validate_media_library_asset_for_use(asset)
    if media.byte_size is None or media.byte_size > AGENT_ASSET_MAX_BYTES:
        raise BusinessValidationError("图片超过 Agent 单张图片大小上限")
    resolved_storage = storage or LocalStorage()
    path = resolved_storage.resolve(media.storage_path)
    try:
        with path.open("rb") as file:
            content = file.read(AGENT_ASSET_MAX_BYTES + 1)
    except OSError as exc:
        raise NotFoundError("全局素材文件不存在") from exc
    if len(content) > AGENT_ASSET_MAX_BYTES:
        raise BusinessValidationError("图片超过 Agent 单张图片大小上限")
    actual = inspect_image_bytes(content, expected_mime_type=media.mime_type)
    if (
        actual.byte_size != media.byte_size
        or actual.width != media.width
        or actual.height != media.height
        or actual.sha256 != media.sha256
    ):
        raise ConflictError("全局素材文件与已核验媒体元数据不一致")
    return AgentAssetContent(content=content, media_type=actual.mime_type, display_name=asset.display_name)


def agent_media_library_asset_metadata(asset: MediaLibraryAsset) -> dict[str, Any]:
    media = asset.media_object
    return {
        "id": asset.id,
        "display_name": asset.display_name,
        "original_filename": asset.original_filename,
        "origin_type": asset.source_type,
        "image_type_key": None,
        "image_type_title": None,
        "user_folder_id": asset.folder_id,
        "user_folder_name": asset.folder.name if asset.folder is not None else None,
        "mime_type": media.mime_type,
        "byte_size": media.byte_size,
        "width": media.width,
        "height": media.height,
        "verification_status": media.verification_status.value,
        "parent_asset_id": None,
        "generation": None,
        "created_at": asset.created_at.isoformat(),
    }


def _agent_global_product_summaries(
    session: Session,
    products: list[Product],
) -> list[dict[str, Any]]:
    if not products:
        return []
    product_ids = [product.id for product in products]
    graphs = list(
        session.scalars(
            select(WorkflowGraph)
            .options(selectinload(WorkflowGraph.nodes))
            .where(
                WorkflowGraph.product_id.in_(product_ids),
                WorkflowGraph.active.is_(True),
            )
        ).unique().all()
    )
    graphs_by_product = {graph.product_id: graph for graph in graphs}
    return [_agent_global_product_summary(product, graphs_by_product.get(product.id)) for product in products]


def _agent_global_product_summary(
    product: Product,
    graph: WorkflowGraph | None,
) -> dict[str, Any]:
    return {
        "id": product.id,
        "name": product.name,
        "category": product.category,
        "updated_at": product.updated_at.isoformat(),
        "active_workflow": (
            {
                "id": graph.id,
                "title": graph.title,
                "revision": graph.revision,
                "node_count": len(graph.nodes),
            }
            if graph is not None
            else None
        ),
    }


def _normalize_global_product_ids(values: list[str]) -> list[str]:
    if not 1 <= len(values) <= AGENT_GLOBAL_PRODUCT_INSPECT_MAX:
        raise BusinessValidationError(
            f"product_ids 必须包含 1 到 {AGENT_GLOBAL_PRODUCT_INSPECT_MAX} 个商品"
        )
    normalized: list[str] = []
    seen: set[str] = set()
    for value in values:
        product_id = value.strip()
        if not product_id:
            raise BusinessValidationError("product_ids 不能包含空值")
        if product_id in seen:
            raise BusinessValidationError("product_ids 不能包含重复值")
        seen.add(product_id)
        normalized.append(product_id)
    return normalized


def _encode_global_product_cursor(*, updated_at: datetime, product_id: str, query: str) -> str:
    normalized_updated_at = (
        updated_at.replace(tzinfo=UTC)
        if updated_at.tzinfo is None
        else updated_at.astimezone(UTC)
    )
    payload = {
        "updated_at": normalized_updated_at.isoformat(),
        "product_id": product_id,
        "query": query,
    }
    encoded = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()
    return base64.urlsafe_b64encode(encoded).decode().rstrip("=")


def _decode_global_product_cursor(value: str, *, query: str) -> tuple[datetime, str]:
    try:
        padded = value + "=" * (-len(value) % 4)
        payload = json.loads(base64.urlsafe_b64decode(padded).decode())
        if payload.get("query") != query:
            raise ValueError
        updated_at = datetime.fromisoformat(payload["updated_at"])
        product_id = str(payload["product_id"]).strip()
        if updated_at.tzinfo is None or not product_id:
            raise ValueError
        return updated_at.astimezone(UTC), product_id
    except (ValueError, KeyError, TypeError, json.JSONDecodeError) as exc:
        raise BusinessValidationError("商品分页 cursor 无效或与当前查询条件不匹配") from exc


def _normalize_explicit_asset_ids(values: list[str]) -> list[str]:
    normalized = [value.strip() for value in values]
    if not normalized or any(not value for value in normalized):
        raise BusinessValidationError("请明确提供要查看的商品图片 ID")
    if len(normalized) > AGENT_MAX_INPUT_ASSETS:
        raise BusinessValidationError(f"单次最多查看 {AGENT_MAX_INPUT_ASSETS} 张商品图片")
    if len(set(normalized)) != len(normalized):
        raise BusinessValidationError("商品图片 ID 不能重复")
    return normalized


__all__ = [
    "AGENT_GLOBAL_PRODUCT_INSPECT_MAX",
    "AGENT_GLOBAL_PRODUCT_LIST_MAX_LIMIT",
    "AgentProductPage",
    "agent_media_library_asset_metadata",
    "inspect_agent_global_media_assets",
    "inspect_agent_global_products",
    "list_agent_global_media_assets",
    "list_agent_global_products",
    "read_agent_global_media_asset_content",
]
