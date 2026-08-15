from __future__ import annotations

import base64
import hashlib
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, Literal

from sqlalchemy import and_, func, or_, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.domain.enums import MediaVerificationStatus
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    LegacyCanvasAgentArchive,
    LegacyUserTemplateArchive,
    LegacyWorkflowArchive,
    LegacyWorkflowArchiveAsset,
    Product,
    ProductImageAsset,
)

LEGACY_ARCHIVE_CURSOR_VERSION = 1
LEGACY_ARCHIVE_DEFAULT_LIMIT = 30
LEGACY_ARCHIVE_MAX_LIMIT = 100

type LegacyArchiveKind = Literal["workflow", "canvas_agent_thread", "user_template"]
LEGACY_ARCHIVE_KINDS: tuple[LegacyArchiveKind, ...] = (
    "workflow",
    "canvas_agent_thread",
    "user_template",
)
_KIND_RANK = {kind: rank for rank, kind in enumerate(LEGACY_ARCHIVE_KINDS)}


@dataclass(frozen=True, slots=True)
class LegacyArchiveListItem:
    kind: LegacyArchiveKind
    id: str
    source_id: str
    source_key: str | None
    product_id: str | None
    product_name: str | None
    title: str
    description: str | None
    source_status: str | None
    archive_schema_version: int
    payload_sha256: str
    source_updated_at: datetime | None
    created_at: datetime
    counts: dict[str, int]


@dataclass(frozen=True, slots=True)
class LegacyArchivePage:
    items: list[LegacyArchiveListItem]
    next_cursor: str | None
    total: int
    kind_counts: dict[LegacyArchiveKind, int]


@dataclass(frozen=True, slots=True)
class LegacyArchiveAsset:
    product_image_asset_id: str
    role: str
    legacy_source_type: str
    legacy_source_id: str
    display_name: str
    original_filename: str
    mime_type: str
    byte_size: int | None
    width: int | None
    height: int | None
    verification_status: MediaVerificationStatus
    created_at: datetime


@dataclass(frozen=True, slots=True)
class LegacyArchiveDetail:
    item: LegacyArchiveListItem
    source_profile: str
    source_fingerprint_sha256: str
    payload_json: dict[str, Any]
    diagnostics: list[dict[str, Any]]
    assets: list[LegacyArchiveAsset]


@dataclass(frozen=True, slots=True)
class LegacyArchiveAssetPage:
    items: list[LegacyArchiveAsset]
    total: int


@dataclass(frozen=True, slots=True)
class _LegacyArchiveCursor:
    filter_hash: str
    created_at: datetime
    kind: LegacyArchiveKind
    archive_id: str


def list_legacy_archives(
    session: Session,
    *,
    kind: LegacyArchiveKind | None = None,
    product_id: str | None = None,
    query: str = "",
    after: str = "",
    limit: int = LEGACY_ARCHIVE_DEFAULT_LIMIT,
) -> LegacyArchivePage:
    if kind is not None and kind not in LEGACY_ARCHIVE_KINDS:
        raise BusinessValidationError("旧归档类型无效")
    if product_id is not None:
        _require_product(session, product_id)
    normalized_query = query.strip()
    if len(normalized_query) > 255:
        raise BusinessValidationError("旧归档搜索词不能超过 255 个字符")
    if not 1 <= limit <= LEGACY_ARCHIVE_MAX_LIMIT:
        raise BusinessValidationError(f"旧归档分页 limit 必须在 1 到 {LEGACY_ARCHIVE_MAX_LIMIT} 之间")

    filter_hash = _filter_hash(kind=kind, product_id=product_id, query=normalized_query)
    cursor = _decode_cursor(after) if after.strip() else None
    if cursor is not None and cursor.filter_hash != filter_hash:
        raise BusinessValidationError("旧归档分页 cursor 与当前查询条件不匹配")

    requested_kinds = (kind,) if kind is not None else LEGACY_ARCHIVE_KINDS
    candidates: list[LegacyArchiveListItem] = []
    for requested_kind in requested_kinds:
        candidates.extend(
            _list_kind(
                session,
                kind=requested_kind,
                product_id=product_id,
                query=normalized_query,
                cursor=cursor,
                limit=limit + 1,
            )
        )
    candidates.sort(key=_list_sort_key, reverse=True)
    selected = candidates[:limit]
    next_cursor = None
    if len(candidates) > limit and selected:
        oldest = selected[-1]
        next_cursor = _encode_cursor(
            _LegacyArchiveCursor(
                filter_hash=filter_hash,
                created_at=_normalize_utc(oldest.created_at),
                kind=oldest.kind,
                archive_id=oldest.id,
            )
        )

    kind_counts = {
        archive_kind: _count_kind(
            session,
            kind=archive_kind,
            product_id=product_id,
            query=normalized_query,
        )
        for archive_kind in LEGACY_ARCHIVE_KINDS
    }
    total = kind_counts[kind] if kind is not None else sum(kind_counts.values())
    return LegacyArchivePage(
        items=selected,
        next_cursor=next_cursor,
        total=total,
        kind_counts=kind_counts,
    )


def get_legacy_archive_detail(
    session: Session,
    *,
    kind: LegacyArchiveKind,
    archive_id: str,
    include_assets: bool = True,
) -> LegacyArchiveDetail:
    if kind == "workflow":
        statement = select(LegacyWorkflowArchive).options(
            selectinload(LegacyWorkflowArchive.product),
        )
        if include_assets:
            statement = statement.options(
                selectinload(LegacyWorkflowArchive.assets)
                .selectinload(LegacyWorkflowArchiveAsset.asset)
                .selectinload(ProductImageAsset.media_object),
            )
        archive = session.scalar(statement.where(LegacyWorkflowArchive.id == archive_id))
        if archive is None:
            raise NotFoundError("旧工作流归档不存在")
        return LegacyArchiveDetail(
            item=_workflow_item(archive, archive.product.name),
            source_profile=archive.source_profile,
            source_fingerprint_sha256=archive.source_fingerprint_sha256,
            payload_json=archive.payload_json,
            diagnostics=[],
            assets=(
                [_archive_asset(item) for item in sorted(archive.assets, key=_archive_asset_sort_key)]
                if include_assets
                else []
            ),
        )

    if kind == "user_template":
        archive = session.get(LegacyUserTemplateArchive, archive_id)
        if archive is None:
            raise NotFoundError("旧用户模板归档不存在")
        diagnostics = archive.diagnostics_json if isinstance(archive.diagnostics_json, list) else []
        return LegacyArchiveDetail(
            item=_template_item(archive),
            source_profile=archive.source_profile,
            source_fingerprint_sha256=archive.source_fingerprint_sha256,
            payload_json=archive.payload_json,
            diagnostics=[item for item in diagnostics if isinstance(item, dict)],
            assets=[],
        )

    if kind == "canvas_agent_thread":
        archive = session.scalar(
            select(LegacyCanvasAgentArchive)
            .options(selectinload(LegacyCanvasAgentArchive.product))
            .where(LegacyCanvasAgentArchive.id == archive_id)
        )
        if archive is None:
            raise NotFoundError("旧 Agent 对话归档不存在")
        return LegacyArchiveDetail(
            item=_canvas_item(archive, archive.product.name),
            source_profile=archive.source_profile,
            source_fingerprint_sha256=archive.source_fingerprint_sha256,
            payload_json=archive.payload_json,
            diagnostics=[],
            assets=[],
        )

    raise BusinessValidationError("旧归档类型无效")


def list_legacy_workflow_archive_assets(
    session: Session,
    *,
    archive_id: str,
    offset: int,
    limit: int,
) -> LegacyArchiveAssetPage:
    if offset < 0 or not 1 <= limit <= 100:
        raise BusinessValidationError("旧归档图片分页参数无效")
    if session.get(LegacyWorkflowArchive, archive_id) is None:
        raise NotFoundError("旧工作流归档不存在")
    order_columns = (
        LegacyWorkflowArchiveAsset.role,
        LegacyWorkflowArchiveAsset.legacy_source_type,
        LegacyWorkflowArchiveAsset.legacy_source_id,
        LegacyWorkflowArchiveAsset.product_image_asset_id,
    )
    statement = (
        select(LegacyWorkflowArchiveAsset)
        .options(selectinload(LegacyWorkflowArchiveAsset.asset).selectinload(ProductImageAsset.media_object))
        .where(LegacyWorkflowArchiveAsset.archive_id == archive_id)
        .order_by(*order_columns)
        .offset(offset)
        .limit(limit)
    )
    total = int(
        session.scalar(
            select(func.count())
            .select_from(LegacyWorkflowArchiveAsset)
            .where(LegacyWorkflowArchiveAsset.archive_id == archive_id)
        )
        or 0
    )
    return LegacyArchiveAssetPage(
        items=[_archive_asset(reference) for reference in session.scalars(statement)],
        total=total,
    )


def build_legacy_archive_export(detail: LegacyArchiveDetail) -> dict[str, Any]:
    item = detail.item
    return {
        "schema_version": 1,
        "archive": {
            "kind": item.kind,
            "id": item.id,
            "source_id": item.source_id,
            "source_key": item.source_key,
            "source_profile": detail.source_profile,
            "source_fingerprint_sha256": detail.source_fingerprint_sha256,
            "product_id": item.product_id,
            "product_name": item.product_name,
            "title": item.title,
            "description": item.description,
            "source_status": item.source_status,
            "archive_schema_version": item.archive_schema_version,
            "payload_sha256": item.payload_sha256,
            "source_updated_at": _datetime_json(item.source_updated_at),
            "created_at": _datetime_json(item.created_at),
            "counts": dict(sorted(item.counts.items())),
            "diagnostics": detail.diagnostics,
            "payload": detail.payload_json,
            "assets": [
                {
                    "product_image_asset_id": asset.product_image_asset_id,
                    "role": asset.role,
                    "legacy_source_type": asset.legacy_source_type,
                    "legacy_source_id": asset.legacy_source_id,
                    "display_name": asset.display_name,
                    "original_filename": asset.original_filename,
                    "mime_type": asset.mime_type,
                    "byte_size": asset.byte_size,
                    "width": asset.width,
                    "height": asset.height,
                    "verification_status": asset.verification_status,
                    "created_at": _datetime_json(asset.created_at),
                }
                for asset in detail.assets
            ],
        },
    }


def legacy_archive_export_bytes(detail: LegacyArchiveDetail) -> bytes:
    return json.dumps(
        build_legacy_archive_export(detail),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
        allow_nan=False,
    ).encode("utf-8")


def legacy_archive_export_sha256(detail: LegacyArchiveDetail) -> str:
    return hashlib.sha256(legacy_archive_export_bytes(detail)).hexdigest()


def _list_kind(
    session: Session,
    *,
    kind: LegacyArchiveKind,
    product_id: str | None,
    query: str,
    cursor: _LegacyArchiveCursor | None,
    limit: int,
) -> list[LegacyArchiveListItem]:
    if kind == "workflow":
        statement = (
            select(LegacyWorkflowArchive, Product.name)
            .join(Product, Product.id == LegacyWorkflowArchive.product_id)
            .order_by(LegacyWorkflowArchive.created_at.desc(), LegacyWorkflowArchive.id.desc())
        )
        if product_id is not None:
            statement = statement.where(LegacyWorkflowArchive.product_id == product_id)
        if query:
            statement = statement.where(
                _search_condition(
                    query,
                    LegacyWorkflowArchive.source_title,
                    LegacyWorkflowArchive.legacy_workflow_id,
                    Product.name,
                )
            )
        if cursor is not None:
            statement = statement.where(
                _seek_condition(
                    kind=kind,
                    created_column=LegacyWorkflowArchive.created_at,
                    id_column=LegacyWorkflowArchive.id,
                    cursor=cursor,
                )
            )
        return [
            _workflow_item(archive, product_name)
            for archive, product_name in session.execute(statement.limit(limit))
        ]

    if kind == "canvas_agent_thread":
        statement = (
            select(LegacyCanvasAgentArchive, Product.name)
            .join(Product, Product.id == LegacyCanvasAgentArchive.product_id)
            .order_by(LegacyCanvasAgentArchive.created_at.desc(), LegacyCanvasAgentArchive.id.desc())
        )
        if product_id is not None:
            statement = statement.where(LegacyCanvasAgentArchive.product_id == product_id)
        if query:
            statement = statement.where(
                _search_condition(
                    query,
                    LegacyCanvasAgentArchive.title,
                    LegacyCanvasAgentArchive.legacy_thread_id,
                    Product.name,
                )
            )
        if cursor is not None:
            statement = statement.where(
                _seek_condition(
                    kind=kind,
                    created_column=LegacyCanvasAgentArchive.created_at,
                    id_column=LegacyCanvasAgentArchive.id,
                    cursor=cursor,
                )
            )
        return [
            _canvas_item(archive, product_name)
            for archive, product_name in session.execute(statement.limit(limit))
        ]

    if product_id is not None:
        return []
    statement = select(LegacyUserTemplateArchive).order_by(
        LegacyUserTemplateArchive.created_at.desc(),
        LegacyUserTemplateArchive.id.desc(),
    )
    if query:
        statement = statement.where(
            _search_condition(
                query,
                LegacyUserTemplateArchive.title,
                LegacyUserTemplateArchive.description,
                LegacyUserTemplateArchive.legacy_key,
                LegacyUserTemplateArchive.legacy_template_id,
            )
        )
    if cursor is not None:
        statement = statement.where(
            _seek_condition(
                kind=kind,
                created_column=LegacyUserTemplateArchive.created_at,
                id_column=LegacyUserTemplateArchive.id,
                cursor=cursor,
            )
        )
    return [_template_item(archive) for archive in session.scalars(statement.limit(limit))]


def _count_kind(
    session: Session,
    *,
    kind: LegacyArchiveKind,
    product_id: str | None,
    query: str,
) -> int:
    if kind == "workflow":
        statement = select(func.count()).select_from(LegacyWorkflowArchive).join(Product)
        if product_id is not None:
            statement = statement.where(LegacyWorkflowArchive.product_id == product_id)
        if query:
            statement = statement.where(
                _search_condition(
                    query,
                    LegacyWorkflowArchive.source_title,
                    LegacyWorkflowArchive.legacy_workflow_id,
                    Product.name,
                )
            )
        return int(session.scalar(statement) or 0)
    if kind == "canvas_agent_thread":
        statement = select(func.count()).select_from(LegacyCanvasAgentArchive).join(Product)
        if product_id is not None:
            statement = statement.where(LegacyCanvasAgentArchive.product_id == product_id)
        if query:
            statement = statement.where(
                _search_condition(
                    query,
                    LegacyCanvasAgentArchive.title,
                    LegacyCanvasAgentArchive.legacy_thread_id,
                    Product.name,
                )
            )
        return int(session.scalar(statement) or 0)
    if product_id is not None:
        return 0
    statement = select(func.count()).select_from(LegacyUserTemplateArchive)
    if query:
        statement = statement.where(
            _search_condition(
                query,
                LegacyUserTemplateArchive.title,
                LegacyUserTemplateArchive.description,
                LegacyUserTemplateArchive.legacy_key,
                LegacyUserTemplateArchive.legacy_template_id,
            )
        )
    return int(session.scalar(statement) or 0)


def _workflow_item(archive: LegacyWorkflowArchive, product_name: str) -> LegacyArchiveListItem:
    return LegacyArchiveListItem(
        kind="workflow",
        id=archive.id,
        source_id=archive.legacy_workflow_id,
        source_key=None,
        product_id=archive.product_id,
        product_name=product_name,
        title=archive.source_title,
        description=None,
        source_status=None,
        archive_schema_version=archive.archive_schema_version,
        payload_sha256=archive.payload_sha256,
        source_updated_at=archive.source_updated_at,
        created_at=archive.created_at,
        counts={
            "nodes": archive.node_count,
            "edges": archive.edge_count,
            "runs": archive.run_count,
            "node_runs": archive.node_run_count,
            "assets": archive.asset_count,
        },
    )


def _template_item(archive: LegacyUserTemplateArchive) -> LegacyArchiveListItem:
    return LegacyArchiveListItem(
        kind="user_template",
        id=archive.id,
        source_id=archive.legacy_template_id,
        source_key=archive.legacy_key,
        product_id=None,
        product_name=None,
        title=archive.title,
        description=archive.description,
        source_status=archive.archive_status,
        archive_schema_version=archive.archive_schema_version,
        payload_sha256=archive.payload_sha256,
        source_updated_at=archive.source_updated_at,
        created_at=archive.created_at,
        counts={},
    )


def _canvas_item(archive: LegacyCanvasAgentArchive, product_name: str) -> LegacyArchiveListItem:
    return LegacyArchiveListItem(
        kind="canvas_agent_thread",
        id=archive.id,
        source_id=archive.legacy_thread_id,
        source_key=None,
        product_id=archive.product_id,
        product_name=product_name,
        title=archive.title,
        description=None,
        source_status=archive.source_status,
        archive_schema_version=archive.archive_schema_version,
        payload_sha256=archive.payload_sha256,
        source_updated_at=archive.source_updated_at,
        created_at=archive.created_at,
        counts={
            "messages": archive.message_count,
            "runs": archive.run_count,
            "tool_events": archive.tool_event_count,
            "plans": archive.plan_count,
            "task_plans": archive.task_plan_count,
            "timeline_events": archive.timeline_event_count,
            "visible_events": archive.visible_event_count,
            "technical_events": archive.technical_event_count,
        },
    )


def _archive_asset(reference: LegacyWorkflowArchiveAsset) -> LegacyArchiveAsset:
    asset = reference.asset
    media = asset.media_object
    return LegacyArchiveAsset(
        product_image_asset_id=asset.id,
        role=reference.role,
        legacy_source_type=reference.legacy_source_type,
        legacy_source_id=reference.legacy_source_id,
        display_name=asset.display_name,
        original_filename=asset.original_filename,
        mime_type=media.mime_type,
        byte_size=media.byte_size,
        width=media.width,
        height=media.height,
        verification_status=media.verification_status,
        created_at=asset.created_at,
    )


def _archive_asset_sort_key(reference: LegacyWorkflowArchiveAsset) -> tuple[str, str, str, str]:
    return (
        reference.role,
        reference.legacy_source_type,
        reference.legacy_source_id,
        reference.product_image_asset_id,
    )


def _search_condition(query: str, *columns):
    normalized = query.lower()
    return or_(
        *(func.lower(func.coalesce(column, "")).contains(normalized, autoescape=True) for column in columns)
    )


def _seek_condition(*, kind: LegacyArchiveKind, created_column, id_column, cursor: _LegacyArchiveCursor):
    kind_rank = _KIND_RANK[kind]
    cursor_rank = _KIND_RANK[cursor.kind]
    if kind_rank < cursor_rank:
        return created_column < cursor.created_at
    if kind_rank > cursor_rank:
        return created_column <= cursor.created_at
    return or_(
        created_column < cursor.created_at,
        and_(created_column == cursor.created_at, id_column < cursor.archive_id),
    )


def _list_sort_key(item: LegacyArchiveListItem) -> tuple[datetime, int, str]:
    return (_normalize_utc(item.created_at), -_KIND_RANK[item.kind], item.id)


def _filter_hash(*, kind: LegacyArchiveKind | None, product_id: str | None, query: str) -> str:
    encoded = json.dumps(
        {"kind": kind, "product_id": product_id, "query": query},
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode()
    return hashlib.sha256(encoded).hexdigest()


def _encode_cursor(cursor: _LegacyArchiveCursor) -> str:
    payload = {
        "v": LEGACY_ARCHIVE_CURSOR_VERSION,
        "filter": cursor.filter_hash,
        "created_at": _normalize_utc(cursor.created_at).isoformat(),
        "kind": cursor.kind,
        "id": cursor.archive_id,
    }
    encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
    return base64.urlsafe_b64encode(encoded).decode().rstrip("=")


def _decode_cursor(value: str) -> _LegacyArchiveCursor:
    normalized = value.strip()
    if len(normalized) > 4096:
        raise BusinessValidationError("旧归档分页 cursor 无效")
    try:
        padded = normalized + "=" * (-len(normalized) % 4)
        raw = base64.b64decode(padded, altchars=b"-_", validate=True)
        decoded = json.loads(raw)
        if not isinstance(decoded, dict) or set(decoded) != {"v", "filter", "created_at", "kind", "id"}:
            raise ValueError
        if decoded["v"] != LEGACY_ARCHIVE_CURSOR_VERSION or decoded["kind"] not in LEGACY_ARCHIVE_KINDS:
            raise ValueError
        if not all(isinstance(decoded[field], str) and decoded[field] for field in ("filter", "created_at", "id")):
            raise ValueError
        if len(decoded["filter"]) != 64:
            raise ValueError
        created_at = datetime.fromisoformat(decoded["created_at"])
        if created_at.tzinfo is None:
            raise ValueError
        return _LegacyArchiveCursor(
            filter_hash=decoded["filter"],
            created_at=_normalize_utc(created_at),
            kind=decoded["kind"],
            archive_id=decoded["id"],
        )
    except (TypeError, ValueError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise BusinessValidationError("旧归档分页 cursor 无效") from exc


def _normalize_utc(value: datetime) -> datetime:
    normalized = value if value.tzinfo is not None else value.replace(tzinfo=UTC)
    return normalized.astimezone(UTC)


def _datetime_json(value: datetime | None) -> str | None:
    return _normalize_utc(value).isoformat() if value is not None else None


def _require_product(session: Session, product_id: str) -> None:
    if session.get(Product, product_id) is None:
        raise NotFoundError("商品不存在")


__all__ = [
    "LEGACY_ARCHIVE_DEFAULT_LIMIT",
    "LEGACY_ARCHIVE_KINDS",
    "LEGACY_ARCHIVE_MAX_LIMIT",
    "LegacyArchiveAsset",
    "LegacyArchiveAssetPage",
    "LegacyArchiveDetail",
    "LegacyArchiveKind",
    "LegacyArchiveListItem",
    "LegacyArchivePage",
    "build_legacy_archive_export",
    "get_legacy_archive_detail",
    "legacy_archive_export_bytes",
    "legacy_archive_export_sha256",
    "list_legacy_archives",
    "list_legacy_workflow_archive_assets",
]
