"""交付导出 ZIP。

条目 lineage 使用 ProductImageAsset id 与任务 id，不用存储路径。
"""

from __future__ import annotations

import json
import os
import tempfile
import unicodedata
import zipfile
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.delivery_renditions.contracts import DELIVERY_FORMAT_EXTENSIONS
from productflow_backend.application.media_objects import VerifiedImageMetadata, inspect_image_bytes
from productflow_backend.application.product_images.archives import GALLERY_ARCHIVE_MAX_BYTES
from productflow_backend.domain.enums import GraphArtifactType, JobStatus, MediaVerificationStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    DeliveryRenditionJob,
    Product,
    ProductImageAsset,
    WorkflowGraph,
    WorkflowGraphArtifact,
    WorkflowGraphNodeRun,
)
from productflow_backend.infrastructure.storage import LocalStorage

DELIVERY_EXPORT_MANIFEST_SCHEMA_VERSION = 1
DELIVERY_EXPORT_MAX_JOBS = 100
DELIVERY_EXPORT_MAX_BYTES = GALLERY_ARCHIVE_MAX_BYTES
_ZIP_EPOCH = (1980, 1, 1, 0, 0, 0)


@dataclass(frozen=True, slots=True)
class DeliveryExportArchive:
    path: Path
    filename: str
    manifest: dict[str, Any]


def export_delivery_rendition_jobs(
    session: Session,
    *,
    product_id: str,
    rendition_job_ids: Sequence[str],
    allow_partial: bool = False,
    storage: LocalStorage | None = None,
) -> DeliveryExportArchive:
    """从已持久化的 rendition 结果构建确定性 ZIP。

    本函数不依赖 provider 或队列。rendition job 是不可变输入边界；成功 job 的结果媒体
    在进入归档前会再测一次，使 manifest 描述实际交付的字节。
    """

    job_ids = list(rendition_job_ids)
    if not 1 <= len(job_ids) <= DELIVERY_EXPORT_MAX_JOBS:
        raise BusinessValidationError(f"一次最多导出 {DELIVERY_EXPORT_MAX_JOBS} 个交付图任务")
    if any(not isinstance(job_id, str) or not job_id.strip() for job_id in job_ids):
        raise BusinessValidationError("交付图任务 ID 不能为空")
    job_ids = [job_id.strip() for job_id in job_ids]
    if len(set(job_ids)) != len(job_ids):
        raise BusinessValidationError("交付导出不能包含重复任务 ID")

    product = session.get(Product, product_id)
    if product is None:
        raise NotFoundError("商品不存在")

    jobs = list(
        session.scalars(
            select(DeliveryRenditionJob)
            .options(
                selectinload(DeliveryRenditionJob.source_asset).selectinload(ProductImageAsset.media_object),
                selectinload(DeliveryRenditionJob.result_asset).selectinload(ProductImageAsset.media_object),
            )
            .where(
                DeliveryRenditionJob.product_id == product_id,
                DeliveryRenditionJob.id.in_(set(job_ids)),
            )
        ).all()
    )
    jobs_by_id = {job.id: job for job in jobs}
    if any(job_id not in jobs_by_id for job_id in job_ids):
        raise NotFoundError("交付图任务不存在")

    verified_total_bytes = 0
    for job_id in job_ids:
        job = jobs_by_id[job_id]
        if job.status != JobStatus.SUCCEEDED or job.result_asset is None:
            continue
        if job.result_asset.product_id != product_id or job.source_asset.product_id != product_id:
            raise ConflictError("交付图资产不属于当前商品")
        verified_total_bytes += _verified_result_byte_size(job.result_asset)
    if verified_total_bytes > DELIVERY_EXPORT_MAX_BYTES:
        max_megabytes = DELIVERY_EXPORT_MAX_BYTES // (1024 * 1024)
        raise BusinessValidationError(f"交付导出图片总大小不能超过 {max_megabytes} MiB")

    resolved_storage = storage or LocalStorage()
    used_filename_keys: set[str] = set()
    successful_items: list[dict[str, Any]] = []
    successful_files: list[tuple[str, Path, VerifiedImageMetadata]] = []
    missing_items: list[dict[str, Any]] = []
    successful_finished_at: list[datetime] = []
    total_bytes = 0
    safe_product_name = _safe_filename_component(product.name, fallback="product")

    for request_index, job_id in enumerate(job_ids, start=1):
        job = jobs_by_id[job_id]
        if job.status != JobStatus.SUCCEEDED or job.result_asset is None:
            missing_items.append(
                {
                    "job_id": job.id,
                    "status": _status_value(job.status),
                    "failure_reason": _failure_reason(job.failure_reason),
                }
            )
            continue
        if job.result_asset.product_id != product_id:
            raise ConflictError("交付图结果不属于当前商品")
        if job.source_asset.product_id != product_id:
            raise ConflictError("交付图原图不属于当前商品")
        if job.finished_at is None:
            raise ConflictError("交付图任务缺少完成时间")

        result_path, measured = _read_verified_result_media(job.result_asset, resolved_storage)
        total_bytes += measured.byte_size
        if total_bytes > DELIVERY_EXPORT_MAX_BYTES:
            max_megabytes = DELIVERY_EXPORT_MAX_BYTES // (1024 * 1024)
            raise BusinessValidationError(f"交付导出图片总大小不能超过 {max_megabytes} MiB")
        source_metadata = _asset_metadata(job.source_asset)
        lineage = _source_lineage(session, job.source_asset.id, product_id=product_id)
        image_type = _safe_filename_component(job.source_asset.image_type_key, fallback="image")
        extension = _extension_for_mime(measured.mime_type)
        requested_filename = (
            f"{safe_product_name}-{image_type}-{request_index:02d}-"
            f"{measured.width}x{measured.height}{extension}"
        )
        filename = _deduplicate_filename(requested_filename, used_filename_keys)
        successful_files.append((filename, result_path, measured))
        successful_finished_at.append(job.finished_at)
        successful_items.append(
            {
                "filename": filename,
                "product": {"id": product.id, "name": product.name},
                "graph": lineage["graph"],
                "run": lineage["run"],
                "node_run_id": lineage["node_run_id"],
                "source_asset": source_metadata,
                "rendition_job": {
                    "id": job.id,
                    "status": _status_value(job.status),
                    "spec_schema_version": job.spec_schema_version,
                    "spec_hash": job.spec_hash,
                    "created_at": _datetime_value(job.created_at),
                    "started_at": _datetime_value(job.started_at),
                    "finished_at": _datetime_value(job.finished_at),
                    "updated_at": _datetime_value(job.updated_at),
                },
                "result_asset": _asset_metadata(job.result_asset),
                "delivery_spec": job.spec_json,
                "measured": {
                    "mime_type": measured.mime_type,
                    "width": measured.width,
                    "height": measured.height,
                    "byte_size": measured.byte_size,
                    "sha256": measured.sha256,
                },
                "generated_at": _datetime_value(job.finished_at),
            }
        )

    if missing_items and not allow_partial:
        raise ConflictError("存在未成功或缺少结果的交付图任务")
    if not successful_items:
        raise ConflictError("没有可导出的成功交付图")

    manifest: dict[str, Any] = {
        "schema_version": DELIVERY_EXPORT_MANIFEST_SCHEMA_VERSION,
        "kind": "productflow.delivery_export",
        "complete": not missing_items,
        "allow_partial": allow_partial,
        "product": {"id": product.id, "name": product.name},
        "generated_at": _datetime_value(max(successful_finished_at)),
        "items": successful_items,
        "missing_items": missing_items,
    }
    archive_path = _write_deterministic_archive(
        manifest,
        successful_files,
    )
    return DeliveryExportArchive(
        path=archive_path,
        filename=f"{safe_product_name}-delivery-export.zip",
        manifest=manifest,
    )


def _read_verified_result_media(
    asset: ProductImageAsset,
    storage: LocalStorage,
) -> tuple[Path, VerifiedImageMetadata]:
    media = asset.media_object
    if media is None or media.verification_status != MediaVerificationStatus.VERIFIED:
        raise ConflictError("交付图结果媒体不可用")
    if (
        media.byte_size is None
        or media.width is None
        or media.height is None
        or media.sha256 is None
        or not media.mime_type
    ):
        raise ConflictError("交付图结果缺少核验元数据")
    try:
        result_path = storage.resolve(media.storage_path)
        content = _read_exact_bounded_file(result_path, expected_byte_size=media.byte_size)
        measured = inspect_image_bytes(content, expected_mime_type=media.mime_type)
    except (BusinessValidationError, FileNotFoundError, OSError, ValueError) as exc:
        raise ConflictError("交付图结果文件不可用") from exc
    if (
        measured.mime_type != media.mime_type
        or measured.byte_size != media.byte_size
        or measured.width != media.width
        or measured.height != media.height
        or measured.sha256 != media.sha256
    ):
        raise ConflictError("交付图结果核验元数据已变化")
    return result_path, measured


def _verified_result_byte_size(asset: ProductImageAsset) -> int:
    media = asset.media_object
    if media is None or media.verification_status != MediaVerificationStatus.VERIFIED:
        raise ConflictError("交付图结果媒体不可用")
    if (
        media.byte_size is None
        or media.width is None
        or media.height is None
        or media.sha256 is None
        or not media.mime_type
    ):
        raise ConflictError("交付图结果缺少核验元数据")
    return media.byte_size


def _source_lineage(session: Session, source_asset_id: str, *, product_id: str) -> dict[str, Any]:
    """从源 ProductImageAsset id 追溯工作流产物，不用路径。"""

    artifact = session.scalar(
        select(WorkflowGraphArtifact)
        .join(WorkflowGraphArtifact.graph)
        .options(
            selectinload(WorkflowGraphArtifact.graph),
            selectinload(WorkflowGraphArtifact.node_run).selectinload(WorkflowGraphNodeRun.graph_run),
        )
        .where(
            WorkflowGraphArtifact.product_image_asset_id == source_asset_id,
            WorkflowGraphArtifact.artifact_type == GraphArtifactType.IMAGE,
            WorkflowGraph.product_id == product_id,
        )
        .order_by(WorkflowGraphArtifact.created_at.desc(), WorkflowGraphArtifact.id.desc())
        .limit(1)
    )
    if artifact is None:
        return {"graph": None, "run": None, "node_run_id": None}
    node_run = artifact.node_run
    graph_run = node_run.graph_run if node_run is not None else None
    return {
        "graph": {"id": artifact.graph_id, "revision": artifact.graph_revision},
        "run": (
            {"id": graph_run.id, "revision": graph_run.graph_revision}
            if graph_run is not None
            else None
        ),
        "node_run_id": node_run.id if node_run is not None else None,
    }


def _asset_metadata(asset: ProductImageAsset) -> dict[str, Any]:
    media = asset.media_object
    if media is None:
        raise ConflictError("交付图资产缺少媒体")
    return {
        "id": asset.id,
        "origin_type": _enum_value(asset.origin_type),
        "image_type_key": asset.image_type_key,
        "display_name": asset.display_name,
        "original_filename": asset.original_filename,
        "parent_asset_id": asset.parent_asset_id,
        "created_at": _datetime_value(asset.created_at),
        "mime_type": media.mime_type,
        "width": media.width,
        "height": media.height,
        "byte_size": media.byte_size,
        "sha256": media.sha256,
    }


def _write_deterministic_archive(
    manifest: dict[str, Any],
    successful_files: list[tuple[str, Path, VerifiedImageMetadata]],
) -> Path:
    manifest_bytes = (
        json.dumps(manifest, ensure_ascii=False, sort_keys=True, separators=(",", ":")) + "\n"
    ).encode("utf-8")
    file_descriptor, raw_path = tempfile.mkstemp(prefix="productflow-delivery-export-", suffix=".zip")
    os.close(file_descriptor)
    archive_path = Path(raw_path)
    try:
        with zipfile.ZipFile(
            archive_path,
            mode="w",
            compression=zipfile.ZIP_DEFLATED,
            compresslevel=9,
        ) as archive:
            archive.writestr(_zip_info("manifest.json"), manifest_bytes)
            for filename, source_path, expected_metadata in successful_files:
                try:
                    content = _read_exact_bounded_file(
                        source_path,
                        expected_byte_size=expected_metadata.byte_size,
                    )
                    measured = inspect_image_bytes(
                        content,
                        expected_mime_type=expected_metadata.mime_type,
                    )
                except (BusinessValidationError, FileNotFoundError, OSError, ValueError) as exc:
                    raise ConflictError("交付图结果文件在打包时发生变化") from exc
                if measured != expected_metadata:
                    raise ConflictError("交付图结果在打包时发生变化")
                archive.writestr(_zip_info(filename), content)
    except BaseException:
        archive_path.unlink(missing_ok=True)
        raise
    return archive_path


def _read_exact_bounded_file(path: Path, *, expected_byte_size: int) -> bytes:
    """最多读取已校验大小再加一字节，避免文件漂移绕过内存上限。"""

    with path.open("rb") as source:
        content = source.read(expected_byte_size + 1)
    if len(content) != expected_byte_size:
        raise ConflictError("交付图结果文件大小已变化")
    return content


def _zip_info(filename: str) -> zipfile.ZipInfo:
    info = zipfile.ZipInfo(filename, date_time=_ZIP_EPOCH)
    info.create_system = 3
    info.external_attr = 0o600 << 16
    info.internal_attr = 0
    info.compress_type = zipfile.ZIP_DEFLATED
    info.extra = b""
    info.comment = b""
    return info


def _safe_filename_component(value: str | None, *, fallback: str) -> str:
    normalized = unicodedata.normalize("NFKC", value or "")
    chunks: list[str] = []
    current: list[str] = []
    for character in normalized:
        if character.isalnum():
            current.append(character)
        elif current:
            chunks.append("".join(current))
            current = []
    if current:
        chunks.append("".join(current))
    result = "-".join(chunks).strip("-")[:80].strip("-")
    return result or fallback


def _deduplicate_filename(filename: str, used_filename_keys: set[str]) -> str:
    filename_key = filename.casefold()
    if filename_key not in used_filename_keys:
        used_filename_keys.add(filename_key)
        return filename
    stem, extension = Path(filename).stem, Path(filename).suffix
    serial = 2
    while True:
        candidate = f"{stem}-{serial}{extension}"
        candidate_key = candidate.casefold()
        if candidate_key not in used_filename_keys:
            used_filename_keys.add(candidate_key)
            return candidate
        serial += 1


def _extension_for_mime(mime_type: str) -> str:
    normalized = mime_type.split(";", maxsplit=1)[0].strip().lower()
    for format_name, known_mime in {
        "png": "image/png",
        "jpeg": "image/jpeg",
        "webp": "image/webp",
    }.items():
        if normalized == known_mime:
            return DELIVERY_FORMAT_EXTENSIONS[format_name]
    raise ConflictError("交付图结果格式不受支持")


def _datetime_value(value: datetime | None) -> str | None:
    return value.isoformat() if value is not None else None


def _status_value(value: JobStatus) -> str:
    return value.value if isinstance(value, JobStatus) else str(value)


def _enum_value(value: object) -> str | None:
    if value is None:
        return None
    raw_value = getattr(value, "value", value)
    return raw_value if isinstance(raw_value, str) else str(raw_value)


def _failure_reason(value: str | None) -> str:
    return (value or "任务未成功").strip()[:1000]
