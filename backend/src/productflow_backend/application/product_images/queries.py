from __future__ import annotations

import base64
import hashlib
import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from enum import StrEnum

from sqlalchemy import and_, func, null, or_, select
from sqlalchemy.orm import Session, aliased, selectinload

from productflow_backend.application.delivery_renditions.contracts import DeliveryRenditionStatus
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import GraphArtifactType, ProductImageOriginType
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    DeliveryRenditionJob,
    Product,
    ProductAssetFolder,
    ProductImageAsset,
    WorkflowGraphArtifact,
    WorkflowGraphNode,
)

GALLERY_CURSOR_VERSION = 1
GALLERY_DEFAULT_LIMIT = 50
GALLERY_MAX_LIMIT = 100
GALLERY_RECENT_DAYS = 30
GALLERY_UNCLASSIFIED_TYPE_KEY = "__unclassified__"

_BUSINESS_KEY_PATTERN = re.compile(r"^[a-z0-9][a-z0-9_-]*$")
_GENERATED_ORIGINS = (
    ProductImageOriginType.WORKFLOW_GENERATION,
    ProductImageOriginType.IMAGE_SESSION_ATTACH,
)


class GalleryDirectoryKind(StrEnum):
    ALL = "all"
    RECENT_GENERATED = "recent_generated"
    UPLOADS = "uploads"
    GENERATED = "generated"
    IMAGE_TYPE = "image_type"
    SOURCE = "source"
    UNORGANIZED = "unorganized"
    USER_FOLDER = "user_folder"


class GalleryAssetSort(StrEnum):
    CREATED_DESC = "created_desc"
    CREATED_ASC = "created_asc"
    NAME_ASC = "name_asc"
    NAME_DESC = "name_desc"


@dataclass(frozen=True, slots=True)
class GalleryGenerationSummary:
    workflow_id: str
    node_id: str
    node_run_id: str
    prompt_artifact_version_id: str | None
    visual_system_version_id: str | None


@dataclass(frozen=True, slots=True)
class GalleryRenditionSummary:
    job_id: str
    source_asset_id: str
    delivery_spec: dict[str, object]
    status: DeliveryRenditionStatus


@dataclass(frozen=True, slots=True)
class GalleryAssetRecord:
    asset: ProductImageAsset
    image_type_title: str | None
    generation: GalleryGenerationSummary | None
    rendition: GalleryRenditionSummary | None


@dataclass(frozen=True, slots=True)
class GalleryAssetPage:
    items: list[GalleryAssetRecord]
    next_cursor: str | None


def _gallery_asset_statement():
    direct_generation = aliased(WorkflowGraphArtifact)
    source_generation = aliased(WorkflowGraphArtifact)
    rendition_source = aliased(ProductImageAsset)
    direct_node = aliased(WorkflowGraphNode)
    source_node = aliased(WorkflowGraphNode)
    image_type_title = func.coalesce(direct_node.title, source_node.title)
    effective_workflow_id = func.coalesce(direct_generation.graph_id, source_generation.graph_id)
    effective_node_id = func.coalesce(direct_generation.node_id, source_generation.node_id)
    effective_node_run_id = func.coalesce(direct_generation.node_run_id, source_generation.node_run_id)
    effective_visual_version_id = func.coalesce(
        direct_generation.payload_json["visual_system_version_id"].as_string(),
        source_generation.payload_json["visual_system_version_id"].as_string(),
    )
    sort_name = func.lower(ProductImageAsset.display_name).label("gallery_sort_name")
    return (
        select(
            ProductImageAsset,
            sort_name,
            image_type_title.label("image_type_title"),
            effective_workflow_id,
            effective_node_id,
            effective_node_run_id,
            null().label("prompt_artifact_version_id"),
            effective_visual_version_id,
            DeliveryRenditionJob.id,
            DeliveryRenditionJob.source_asset_id,
            DeliveryRenditionJob.spec_json,
            DeliveryRenditionJob.status,
        )
        .options(
            selectinload(ProductImageAsset.media_object),
            selectinload(ProductImageAsset.user_folder),
        )
        .outerjoin(
            direct_generation,
            (direct_generation.product_image_asset_id == ProductImageAsset.id)
            & (direct_generation.artifact_type == GraphArtifactType.IMAGE),
        )
        .outerjoin(direct_node, direct_node.id == direct_generation.node_id)
        .outerjoin(
            DeliveryRenditionJob,
            DeliveryRenditionJob.result_asset_id == ProductImageAsset.id,
        )
        .outerjoin(
            rendition_source,
            rendition_source.id == DeliveryRenditionJob.source_asset_id,
        )
        .outerjoin(
            source_generation,
            (source_generation.product_image_asset_id == rendition_source.id)
            & (source_generation.artifact_type == GraphArtifactType.IMAGE),
        )
        .outerjoin(source_node, source_node.id == source_generation.node_id)
    )


@dataclass(frozen=True, slots=True)
class GalleryDirectoryCount:
    kind: GalleryDirectoryKind
    count: int


@dataclass(frozen=True, slots=True)
class GalleryImageTypeCount:
    directory_key: str
    image_type_key: str | None
    title: str
    count: int


@dataclass(frozen=True, slots=True)
class GalleryOriginCount:
    origin_type: ProductImageOriginType
    count: int


@dataclass(frozen=True, slots=True)
class GalleryFolderCount:
    id: str
    name: str
    sort_order: int
    count: int


@dataclass(frozen=True, slots=True)
class GalleryBootstrap:
    product_id: str
    cover_image_asset_id: str | None
    system_directories: list[GalleryDirectoryCount]
    image_types: list[GalleryImageTypeCount]
    origins: list[GalleryOriginCount]
    user_folders: list[GalleryFolderCount]
    unorganized_count: int


@dataclass(frozen=True, slots=True)
class _GalleryCursor:
    sort: GalleryAssetSort
    filter_hash: str
    key: str
    asset_id: str
    as_of: datetime | None


def list_gallery_assets(
    session: Session,
    *,
    product_id: str,
    directory_kind: GalleryDirectoryKind = GalleryDirectoryKind.ALL,
    directory_key: str | None = None,
    query: str = "",
    sort: GalleryAssetSort = GalleryAssetSort.CREATED_DESC,
    after: str = "",
    limit: int = GALLERY_DEFAULT_LIMIT,
    current_time: datetime | None = None,
) -> GalleryAssetPage:
    _require_product(session, product_id)
    normalized_key = _validate_directory_key(
        session,
        product_id=product_id,
        directory_kind=directory_kind,
        directory_key=directory_key,
    )
    normalized_query = query.strip()
    if len(normalized_query) > 255:
        raise BusinessValidationError("图库搜索词不能超过 255 个字符")
    if not 1 <= limit <= GALLERY_MAX_LIMIT:
        raise BusinessValidationError(f"图库分页 limit 必须在 1 到 {GALLERY_MAX_LIMIT} 之间")

    filter_hash = _gallery_filter_hash(
        product_id=product_id,
        directory_kind=directory_kind,
        directory_key=normalized_key,
        query=normalized_query,
        sort=sort,
    )
    cursor = _decode_cursor(after) if after.strip() else None
    if cursor is not None and (cursor.sort != sort or cursor.filter_hash != filter_hash):
        raise BusinessValidationError("图库分页 cursor 与当前查询条件不匹配")
    if directory_kind == GalleryDirectoryKind.RECENT_GENERATED:
        as_of = cursor.as_of if cursor is not None else _normalize_utc(current_time or now_utc())
        if as_of is None:
            raise BusinessValidationError("图库分页 cursor 缺少最近生成时间锚点")
    else:
        if cursor is not None and cursor.as_of is not None:
            raise BusinessValidationError("图库分页 cursor 与当前目录不匹配")
        as_of = None

    sort_name = func.lower(ProductImageAsset.display_name).label("gallery_sort_name")
    statement = _gallery_asset_statement().where(ProductImageAsset.product_id == product_id)
    statement = _apply_directory_filter(
        statement,
        directory_kind=directory_kind,
        directory_key=normalized_key,
        as_of=as_of,
    )
    if normalized_query:
        statement = statement.where(
            or_(
                ProductImageAsset.display_name.icontains(normalized_query, autoescape=True),
                ProductImageAsset.original_filename.icontains(normalized_query, autoescape=True),
            )
        )
    if cursor is not None:
        statement = statement.where(_seek_condition(sort=sort, cursor=cursor, sort_name=sort_name))
    statement = statement.order_by(*_sort_order(sort=sort, sort_name=sort_name)).limit(limit + 1)

    rows = list(session.execute(statement).all())
    has_more = len(rows) > limit
    page_rows = rows[:limit]
    records = [_record_from_row(row) for row in page_rows]
    next_cursor = None
    if has_more and page_rows:
        last_row = page_rows[-1]
        last_asset = last_row[0]
        sort_key = (
            _normalize_datetime(last_asset.created_at).isoformat()
            if sort in {GalleryAssetSort.CREATED_ASC, GalleryAssetSort.CREATED_DESC}
            else str(last_row[1])
        )
        next_cursor = _encode_cursor(
            _GalleryCursor(
                sort=sort,
                filter_hash=filter_hash,
                key=sort_key,
                asset_id=last_asset.id,
                as_of=as_of,
            )
        )
    return GalleryAssetPage(items=records, next_cursor=next_cursor)


def get_gallery_asset_detail(
    session: Session,
    *,
    product_id: str,
    asset_id: str,
) -> GalleryAssetRecord:
    _require_product(session, product_id)
    statement = _gallery_asset_statement().where(
        ProductImageAsset.product_id == product_id,
        ProductImageAsset.id == asset_id,
    )
    row = session.execute(statement).one_or_none()
    if row is None:
        raise NotFoundError("商品图片不存在")
    return _record_from_row(row)


def get_gallery_bootstrap(
    session: Session,
    *,
    product_id: str,
    current_time: datetime | None = None,
) -> GalleryBootstrap:
    product = _require_product(session, product_id)
    as_of = _normalize_utc(current_time or now_utc())
    base_filter = ProductImageAsset.product_id == product_id
    all_count = session.scalar(select(func.count(ProductImageAsset.id)).where(base_filter)) or 0
    unorganized_count = (
        session.scalar(
            select(func.count(ProductImageAsset.id)).where(
                base_filter,
                ProductImageAsset.user_folder_id.is_(None),
            )
        )
        or 0
    )
    recent_count = (
        session.scalar(
            select(func.count(ProductImageAsset.id)).where(
                base_filter,
                ProductImageAsset.origin_type.in_(_GENERATED_ORIGINS),
                ProductImageAsset.created_at >= as_of - timedelta(days=GALLERY_RECENT_DAYS),
            )
        )
        or 0
    )

    origin_rows = list(
        session.execute(
            select(ProductImageAsset.origin_type, func.count(ProductImageAsset.id))
            .where(base_filter)
            .group_by(ProductImageAsset.origin_type)
            .order_by(ProductImageAsset.origin_type)
        ).all()
    )
    origin_counts = {origin: count for origin, count in origin_rows}
    origins = [
        GalleryOriginCount(origin_type=origin, count=origin_counts.get(origin, 0))
        for origin in ProductImageOriginType
        if origin_counts.get(origin, 0) > 0
    ]

    image_type_rows = list(
        session.execute(
            select(
                ProductImageAsset.image_type_key,
                func.count(ProductImageAsset.id),
            )
            .where(base_filter)
            .group_by(ProductImageAsset.image_type_key)
            .order_by(ProductImageAsset.image_type_key)
        ).all()
    )
    image_types = [
        GalleryImageTypeCount(
            directory_key=image_type_key or GALLERY_UNCLASSIFIED_TYPE_KEY,
            image_type_key=image_type_key,
            title=image_type_key or "未分类",
            count=count,
        )
        for image_type_key, count in image_type_rows
    ]

    folder_rows = list(
        session.execute(
            select(
                ProductAssetFolder.id,
                ProductAssetFolder.name,
                ProductAssetFolder.sort_order,
                func.count(ProductImageAsset.id),
            )
            .outerjoin(ProductImageAsset, ProductImageAsset.user_folder_id == ProductAssetFolder.id)
            .where(ProductAssetFolder.product_id == product_id)
            .group_by(ProductAssetFolder.id)
            .order_by(
                ProductAssetFolder.sort_order,
                ProductAssetFolder.name,
                ProductAssetFolder.id,
            )
        ).all()
    )
    folders = [
        GalleryFolderCount(id=folder_id, name=name, sort_order=sort_order, count=count)
        for folder_id, name, sort_order, count in folder_rows
    ]
    generated_count = sum(origin_counts.get(origin, 0) for origin in _GENERATED_ORIGINS)
    system_directories = [
        GalleryDirectoryCount(kind=GalleryDirectoryKind.ALL, count=all_count),
        GalleryDirectoryCount(kind=GalleryDirectoryKind.RECENT_GENERATED, count=recent_count),
        GalleryDirectoryCount(
            kind=GalleryDirectoryKind.UPLOADS,
            count=origin_counts.get(ProductImageOriginType.UPLOAD, 0),
        ),
        GalleryDirectoryCount(kind=GalleryDirectoryKind.GENERATED, count=generated_count),
        GalleryDirectoryCount(kind=GalleryDirectoryKind.UNORGANIZED, count=unorganized_count),
    ]
    return GalleryBootstrap(
        product_id=product_id,
        cover_image_asset_id=product.cover_image_asset_id,
        system_directories=system_directories,
        image_types=image_types,
        origins=origins,
        user_folders=folders,
        unorganized_count=unorganized_count,
    )


def _require_product(session: Session, product_id: str) -> Product:
    product = session.get(Product, product_id)
    if product is None:
        raise NotFoundError("商品不存在")
    return product


def _validate_directory_key(
    session: Session,
    *,
    product_id: str,
    directory_kind: GalleryDirectoryKind,
    directory_key: str | None,
) -> str | None:
    normalized_key = directory_key.strip() if directory_key is not None else None
    keyed_kinds = {
        GalleryDirectoryKind.IMAGE_TYPE,
        GalleryDirectoryKind.SOURCE,
        GalleryDirectoryKind.USER_FOLDER,
    }
    if directory_kind in keyed_kinds and not normalized_key:
        raise BusinessValidationError("当前图库目录必须提供 directory_key")
    if directory_kind not in keyed_kinds and normalized_key:
        raise BusinessValidationError("当前图库目录不接受 directory_key")
    if directory_kind == GalleryDirectoryKind.IMAGE_TYPE:
        if normalized_key != GALLERY_UNCLASSIFIED_TYPE_KEY and (
            len(normalized_key or "") > 80 or _BUSINESS_KEY_PATTERN.fullmatch(normalized_key or "") is None
        ):
            raise BusinessValidationError("图片类型 directory_key 无效")
    elif directory_kind == GalleryDirectoryKind.SOURCE:
        try:
            ProductImageOriginType(normalized_key)
        except ValueError as exc:
            raise BusinessValidationError("图片来源 directory_key 无效") from exc
    elif directory_kind == GalleryDirectoryKind.USER_FOLDER:
        folder_exists = session.scalar(
            select(ProductAssetFolder.id).where(
                ProductAssetFolder.id == normalized_key,
                ProductAssetFolder.product_id == product_id,
            )
        )
        if folder_exists is None:
            raise NotFoundError("商品图片文件夹不存在")
    return normalized_key


def _apply_directory_filter(statement, *, directory_kind, directory_key, as_of):
    if directory_kind == GalleryDirectoryKind.RECENT_GENERATED:
        return statement.where(
            ProductImageAsset.origin_type.in_(_GENERATED_ORIGINS),
            ProductImageAsset.created_at >= as_of - timedelta(days=GALLERY_RECENT_DAYS),
        )
    if directory_kind == GalleryDirectoryKind.UPLOADS:
        return statement.where(ProductImageAsset.origin_type == ProductImageOriginType.UPLOAD)
    if directory_kind == GalleryDirectoryKind.GENERATED:
        return statement.where(ProductImageAsset.origin_type.in_(_GENERATED_ORIGINS))
    if directory_kind == GalleryDirectoryKind.IMAGE_TYPE:
        if directory_key == GALLERY_UNCLASSIFIED_TYPE_KEY:
            return statement.where(ProductImageAsset.image_type_key.is_(None))
        return statement.where(ProductImageAsset.image_type_key == directory_key)
    if directory_kind == GalleryDirectoryKind.SOURCE:
        return statement.where(ProductImageAsset.origin_type == ProductImageOriginType(directory_key))
    if directory_kind == GalleryDirectoryKind.UNORGANIZED:
        return statement.where(ProductImageAsset.user_folder_id.is_(None))
    if directory_kind == GalleryDirectoryKind.USER_FOLDER:
        return statement.where(ProductImageAsset.user_folder_id == directory_key)
    return statement


def _sort_order(*, sort: GalleryAssetSort, sort_name):
    if sort == GalleryAssetSort.CREATED_ASC:
        return ProductImageAsset.created_at.asc(), ProductImageAsset.id.asc()
    if sort == GalleryAssetSort.NAME_ASC:
        return sort_name.asc(), ProductImageAsset.id.asc()
    if sort == GalleryAssetSort.NAME_DESC:
        return sort_name.desc(), ProductImageAsset.id.desc()
    return ProductImageAsset.created_at.desc(), ProductImageAsset.id.desc()


def _seek_condition(*, sort: GalleryAssetSort, cursor: _GalleryCursor, sort_name):
    if sort in {GalleryAssetSort.CREATED_ASC, GalleryAssetSort.CREATED_DESC}:
        try:
            key = datetime.fromisoformat(cursor.key)
        except ValueError as exc:
            raise BusinessValidationError("图库分页 cursor 无效") from exc
        column = ProductImageAsset.created_at
    else:
        key = cursor.key
        column = sort_name
    if sort in {GalleryAssetSort.CREATED_ASC, GalleryAssetSort.NAME_ASC}:
        return or_(column > key, and_(column == key, ProductImageAsset.id > cursor.asset_id))
    return or_(column < key, and_(column == key, ProductImageAsset.id < cursor.asset_id))


def _record_from_row(row) -> GalleryAssetRecord:
    generation = None
    if row[3] is not None:
        generation = GalleryGenerationSummary(
            workflow_id=row[3],
            node_id=row[4] or "",
            node_run_id=row[5] or "",
            prompt_artifact_version_id=row[6],
            visual_system_version_id=row[7],
        )
    rendition = None
    if row[8] is not None:
        rendition = GalleryRenditionSummary(
            job_id=row[8],
            source_asset_id=row[9],
            delivery_spec=row[10],
            status=row[11],
        )
    return GalleryAssetRecord(
        asset=row[0],
        image_type_title=row[2] or row[0].image_type_key,
        generation=generation,
        rendition=rendition,
    )


def _gallery_filter_hash(
    *,
    product_id: str,
    directory_kind: GalleryDirectoryKind,
    directory_key: str | None,
    query: str,
    sort: GalleryAssetSort,
) -> str:
    encoded = json.dumps(
        {
            "product_id": product_id,
            "directory_kind": directory_kind.value,
            "directory_key": directory_key,
            "query": query,
            "sort": sort.value,
        },
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode()
    return hashlib.sha256(encoded).hexdigest()


def _encode_cursor(cursor: _GalleryCursor) -> str:
    payload = {
        "v": GALLERY_CURSOR_VERSION,
        "sort": cursor.sort.value,
        "filter": cursor.filter_hash,
        "key": cursor.key,
        "id": cursor.asset_id,
    }
    if cursor.as_of is not None:
        payload["as_of"] = _normalize_utc(cursor.as_of).isoformat()
    encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
    return base64.urlsafe_b64encode(encoded).decode().rstrip("=")


def _decode_cursor(value: str) -> _GalleryCursor:
    normalized = value.strip()
    if len(normalized) > 4096:
        raise BusinessValidationError("图库分页 cursor 无效")
    try:
        padded = normalized + "=" * (-len(normalized) % 4)
        raw = base64.b64decode(padded, altchars=b"-_", validate=True)
        decoded = json.loads(raw)
        if not isinstance(decoded, dict):
            raise ValueError
        allowed_keys = {"v", "sort", "filter", "key", "id", "as_of"}
        if set(decoded) - allowed_keys or not {"v", "sort", "filter", "key", "id"} <= set(decoded):
            raise ValueError
        if decoded["v"] != GALLERY_CURSOR_VERSION:
            raise ValueError
        sort = GalleryAssetSort(decoded["sort"])
        if not all(isinstance(decoded[field], str) and decoded[field] for field in ("filter", "key", "id")):
            raise ValueError
        if len(decoded["filter"]) != 64:
            raise ValueError
        as_of_raw = decoded.get("as_of")
        as_of = datetime.fromisoformat(as_of_raw) if isinstance(as_of_raw, str) else None
        if as_of is not None and as_of.tzinfo is None:
            raise ValueError
        return _GalleryCursor(
            sort=sort,
            filter_hash=decoded["filter"],
            key=decoded["key"],
            asset_id=decoded["id"],
            as_of=as_of,
        )
    except (TypeError, ValueError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise BusinessValidationError("图库分页 cursor 无效") from exc


def _normalize_datetime(value: datetime) -> datetime:
    return value if value.tzinfo is not None else value.replace(tzinfo=UTC)


def _normalize_utc(value: datetime) -> datetime:
    normalized = _normalize_datetime(value)
    return normalized.astimezone(UTC)


__all__ = [
    "GALLERY_DEFAULT_LIMIT",
    "GALLERY_MAX_LIMIT",
    "GALLERY_UNCLASSIFIED_TYPE_KEY",
    "GalleryAssetPage",
    "GalleryAssetRecord",
    "GalleryAssetSort",
    "GalleryBootstrap",
    "GalleryDirectoryKind",
    "GalleryRenditionSummary",
    "get_gallery_asset_detail",
    "get_gallery_bootstrap",
    "list_gallery_assets",
]
