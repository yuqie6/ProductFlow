"""工作流子图库：WorkflowMediaLibraryAsset 使用关联。

关联不拥有另一份媒体副本。工作流节点、封面、参考绑定和交付 lineage 使用
ProductImageAsset id；必要时先把全局素材收录为商品图片。
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime

from sqlalchemy import func, select
from sqlalchemy.orm import Session, aliased, selectinload

from productflow_backend.application.media_library.service import (
    collect_media_library_assets_to_product,
    validate_media_library_asset_for_use,
)
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    MediaLibraryAsset,
    MediaLibraryAssetTag,
    ProductImageAsset,
    WorkflowGraph,
    WorkflowMediaLibraryAsset,
)

MAX_WORKFLOW_LIBRARY_ASSETS = 100


@dataclass(frozen=True, slots=True)
class WorkflowMediaLibraryAssetRecord:
    """全局素材加上可选的商品侧图片身份。"""

    asset: MediaLibraryAsset
    product_image_asset_id: str | None
    linked_at: datetime


def _workflow_query(*, product_id: str, workflow_id: str):
    return select(WorkflowGraph).where(
        WorkflowGraph.id == workflow_id,
        WorkflowGraph.product_id == product_id,
    )


def _require_workflow(session: Session, *, product_id: str, workflow_id: str) -> WorkflowGraph:
    workflow = session.scalar(_workflow_query(product_id=product_id, workflow_id=workflow_id))
    if workflow is None:
        raise NotFoundError("工作流不存在")
    return workflow


def _linked_asset_query(*, workflow_id: str, product_id: str):
    product_library_asset = aliased(ProductImageAsset)
    product_source_asset = aliased(ProductImageAsset)
    return (
        select(
            MediaLibraryAsset,
            product_library_asset.id,
            product_source_asset.id,
            WorkflowMediaLibraryAsset.created_at,
        )
        .join(
            WorkflowMediaLibraryAsset,
            WorkflowMediaLibraryAsset.media_library_asset_id == MediaLibraryAsset.id,
        )
        .outerjoin(
            product_library_asset,
            (product_library_asset.source_library_asset_id == MediaLibraryAsset.id)
            & (product_library_asset.product_id == product_id),
        )
        .outerjoin(
            product_source_asset,
            (product_source_asset.id == MediaLibraryAsset.source_product_asset_id)
            & (product_source_asset.product_id == product_id),
        )
        .where(WorkflowMediaLibraryAsset.workflow_id == workflow_id)
        .options(
            selectinload(MediaLibraryAsset.media_object),
            selectinload(MediaLibraryAsset.folder),
            selectinload(MediaLibraryAsset.tag_assignments).selectinload(MediaLibraryAssetTag.tag),
        )
        .order_by(
            WorkflowMediaLibraryAsset.created_at.desc(),
            WorkflowMediaLibraryAsset.media_library_asset_id.desc(),
        )
    )


def list_workflow_media_library_assets(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    limit: int = MAX_WORKFLOW_LIBRARY_ASSETS,
) -> list[WorkflowMediaLibraryAssetRecord]:
    """列出工作流关联的全局素材，并投影到商品侧 ProductImageAsset id。"""

    _require_workflow(session, product_id=product_id, workflow_id=workflow_id)
    bounded_limit = min(max(limit, 1), MAX_WORKFLOW_LIBRARY_ASSETS)
    rows = session.execute(
        _linked_asset_query(workflow_id=workflow_id, product_id=product_id).limit(bounded_limit)
    ).all()
    return [
        WorkflowMediaLibraryAssetRecord(
            asset=asset,
            product_image_asset_id=product_image_asset_id or source_product_asset_id,
            linked_at=linked_at,
        )
        for asset, product_image_asset_id, source_product_asset_id, linked_at in rows
    ]


def sync_workflow_media_library_assets(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    media_library_asset_ids: list[str],
    commit: bool = True,
) -> list[WorkflowMediaLibraryAssetRecord]:
    """写入使用关联；缺商品图片时先收录，仍不复制 bytes。"""

    if not media_library_asset_ids:
        raise BusinessValidationError("至少选择一个素材")
    if len(media_library_asset_ids) > MAX_WORKFLOW_LIBRARY_ASSETS:
        raise BusinessValidationError(f"一次最多关联 {MAX_WORKFLOW_LIBRARY_ASSETS} 个素材")
    normalized_ids = list(dict.fromkeys(media_library_asset_ids))
    if len(normalized_ids) != len(media_library_asset_ids):
        raise BusinessValidationError("关联请求包含重复素材")
    if any(not asset_id or len(asset_id) > 36 for asset_id in normalized_ids):
        raise BusinessValidationError("素材 ID 无效")

    _require_workflow(session, product_id=product_id, workflow_id=workflow_id)

    library_assets = list(
        session.scalars(
            select(MediaLibraryAsset)
            .where(MediaLibraryAsset.id.in_(normalized_ids))
            .options(
                selectinload(MediaLibraryAsset.media_object),
                selectinload(MediaLibraryAsset.source_image_session_asset),
                selectinload(MediaLibraryAsset.source_product_asset),
            )
        ).all()
    )
    if len(library_assets) != len(normalized_ids):
        raise NotFoundError("素材库资产不存在")
    for library_asset in library_assets:
        validate_media_library_asset_for_use(library_asset)
    same_product_source_ids = {
        asset.id
        for asset in library_assets
        if asset.source_product_asset is not None and asset.source_product_asset.product_id == product_id
    }
    ids_to_collect = [asset_id for asset_id in normalized_ids if asset_id not in same_product_source_ids]
    if ids_to_collect:
        # 工作流面对的身份仍是 ProductImageAsset；收录幂等且保留 source_library_asset_id。
        collect_media_library_assets_to_product(
            session,
            product_id=product_id,
            library_asset_ids=ids_to_collect,
            commit=False,
        )

    workflow = session.scalar(
        _workflow_query(product_id=product_id, workflow_id=workflow_id).with_for_update()
    )
    if workflow is None:
        raise NotFoundError("工作流不存在")
    existing_ids = set(
        session.scalars(
            select(WorkflowMediaLibraryAsset.media_library_asset_id).where(
                WorkflowMediaLibraryAsset.workflow_id == workflow.id,
                WorkflowMediaLibraryAsset.media_library_asset_id.in_(normalized_ids),
            )
        ).all()
    )
    for asset_id in normalized_ids:
        if asset_id not in existing_ids:
            session.add(
                WorkflowMediaLibraryAsset(
                    workflow_id=workflow.id,
                    media_library_asset_id=asset_id,
                )
            )
    if commit:
        session.commit()
    return list_workflow_media_library_assets(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
    )


def remove_workflow_media_library_asset(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    media_library_asset_id: str,
) -> None:
    """只删除工作流关联，不删全局素材、商品图片或 MediaObject。"""
    _require_workflow(session, product_id=product_id, workflow_id=workflow_id)
    link = session.scalar(
        select(WorkflowMediaLibraryAsset).where(
            WorkflowMediaLibraryAsset.workflow_id == workflow_id,
            WorkflowMediaLibraryAsset.media_library_asset_id == media_library_asset_id,
        )
    )
    if link is None:
        raise NotFoundError("工作流素材关联不存在")
    session.delete(link)
    session.commit()


def count_workflow_media_library_links(session: Session, *, media_library_asset_id: str) -> int:
    return int(
        session.scalar(
            select(func.count())
            .select_from(WorkflowMediaLibraryAsset)
            .where(WorkflowMediaLibraryAsset.media_library_asset_id == media_library_asset_id)
        )
        or 0
    )


__all__ = [
    "MAX_WORKFLOW_LIBRARY_ASSETS",
    "WorkflowMediaLibraryAssetRecord",
    "count_workflow_media_library_links",
    "list_workflow_media_library_assets",
    "remove_workflow_media_library_asset",
    "sync_workflow_media_library_assets",
]
