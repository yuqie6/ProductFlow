from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, Literal

from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent.conversations import (
    get_agent_conversation_by_id_or_raise,
    get_agent_conversation_or_raise,
)
from productflow_backend.application.legacy_archives import (
    LegacyArchiveKind,
    LegacyArchiveListItem,
    get_legacy_archive_detail,
    list_legacy_archives,
    list_legacy_workflow_archive_assets,
)
from productflow_backend.application.workflow_drafts.service import (
    PRODUCT_WORKFLOW_DRAFT_RETIRED,
    get_workflow_draft_or_raise,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    LegacyCanvasAgentArchive,
    LegacyUserTemplateArchive,
    LegacyWorkflowArchive,
    Product,
    WorkflowDraft,
    WorkflowDraftLegacyArchiveSeed,
)

AGENT_LEGACY_ARCHIVE_LIST_DEFAULT_LIMIT = 20
AGENT_LEGACY_ARCHIVE_LIST_MAX_LIMIT = 50
AGENT_LEGACY_ARCHIVE_INSPECT_MAX_ITEMS = 10
AGENT_LEGACY_ARCHIVE_MAX_OUTPUT_BYTES = 256 * 1024

type AgentLegacyArchiveSection = Literal[
    "summary",
    "product",
    "workflow",
    "nodes",
    "edges",
    "creative_briefs",
    "copy_sets",
    "poster_variants",
    "source_assets",
    "runs",
    "node_runs",
    "assets",
    "thread",
    "messages",
    "tool_events",
    "plans",
    "task_plans",
    "visible_timeline",
    "template",
    "template_nodes",
    "template_edges",
    "diagnostics",
]

_WORKFLOW_SECTIONS: tuple[AgentLegacyArchiveSection, ...] = (
    "summary",
    "product",
    "workflow",
    "nodes",
    "edges",
    "creative_briefs",
    "copy_sets",
    "poster_variants",
    "source_assets",
    "runs",
    "node_runs",
    "assets",
    "diagnostics",
)
_CANVAS_SECTIONS: tuple[AgentLegacyArchiveSection, ...] = (
    "summary",
    "thread",
    "messages",
    "runs",
    "tool_events",
    "plans",
    "task_plans",
    "visible_timeline",
    "diagnostics",
)
_TEMPLATE_SECTIONS: tuple[AgentLegacyArchiveSection, ...] = (
    "summary",
    "template",
    "template_nodes",
    "template_edges",
    "diagnostics",
)
_SECTIONS_BY_KIND = {
    "workflow": _WORKFLOW_SECTIONS,
    "canvas_agent_thread": _CANVAS_SECTIONS,
    "user_template": _TEMPLATE_SECTIONS,
}


@dataclass(frozen=True, slots=True)
class LegacyArchiveRebuildResult:
    seed: WorkflowDraftLegacyArchiveSeed
    draft: WorkflowDraft
    conversation: AgentConversation
    created: bool


def create_legacy_archive_rebuild(
    session: Session,
    *,
    kind: LegacyArchiveKind,
    archive_id: str,
    target_product_id: str,
    idempotency_key: str,
) -> LegacyArchiveRebuildResult:
    _normalize_idempotency_key(idempotency_key)
    normalized_product_id = target_product_id.strip()
    if not normalized_product_id:
        raise BusinessValidationError("目标商品不能为空")
    _request_hash(
        kind=kind,
        archive_id=archive_id,
        target_product_id=normalized_product_id,
    )
    try:
        product = session.scalar(select(Product).where(Product.id == normalized_product_id).with_for_update())
        if product is None:
            raise NotFoundError("目标商品不存在")
        raise ConflictError(PRODUCT_WORKFLOW_DRAFT_RETIRED)
    except IntegrityError:
        session.rollback()
        raise
    except Exception:
        session.rollback()
        raise


def legacy_archive_seed_summary(seed: WorkflowDraftLegacyArchiveSeed) -> dict[str, Any]:
    kind, archive_id, archive = _seed_archive(seed)
    if kind == "workflow":
        counts = {
            "nodes": archive.node_count,
            "edges": archive.edge_count,
            "runs": archive.run_count,
            "node_runs": archive.node_run_count,
            "assets": archive.asset_count,
        }
        title = archive.source_title
        source_status = None
        source_product_id = archive.product_id
    elif kind == "canvas_agent_thread":
        counts = {
            "messages": archive.message_count,
            "runs": archive.run_count,
            "tool_events": archive.tool_event_count,
            "plans": archive.plan_count,
            "task_plans": archive.task_plan_count,
            "visible_events": archive.visible_event_count,
        }
        title = archive.title
        source_status = archive.source_status
        source_product_id = archive.product_id
    else:
        counts = {}
        title = archive.title
        source_status = archive.archive_status
        source_product_id = None
    return {
        "id": seed.id,
        "workflow_draft_id": seed.workflow_draft_id,
        "product_id": seed.product_id,
        "archive_kind": kind,
        "archive_id": archive_id,
        "archive_title": title,
        "archive_status": source_status,
        "source_product_id": source_product_id,
        "source_profile": archive.source_profile,
        "archive_schema_version": archive.archive_schema_version,
        "payload_sha256": archive.payload_sha256,
        "counts": counts,
        "schema_version": seed.schema_version,
        "created_at": _datetime_json(seed.created_at),
    }


def list_agent_legacy_archives(
    session: Session,
    *,
    conversation_id: str,
    kind: LegacyArchiveKind,
    query: str = "",
    after: str = "",
    limit: int = AGENT_LEGACY_ARCHIVE_LIST_DEFAULT_LIMIT,
) -> dict[str, Any]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    if not 1 <= limit <= AGENT_LEGACY_ARCHIVE_LIST_MAX_LIMIT:
        raise BusinessValidationError(f"Agent 旧归档分页 limit 必须在 1 到 {AGENT_LEGACY_ARCHIVE_LIST_MAX_LIMIT} 之间")
    page = list_legacy_archives(
        session,
        kind=kind,
        product_id=conversation.product_id if kind != "user_template" else None,
        query=query,
        after=after,
        limit=limit,
    )
    result = {
        "schema_version": 1,
        "items": [_archive_item_metadata(item) for item in page.items],
        "next_cursor": page.next_cursor,
    }
    _require_bounded_output(result)
    return result


def inspect_agent_legacy_archive(
    session: Session,
    *,
    conversation_id: str,
    kind: LegacyArchiveKind,
    archive_id: str,
    section: AgentLegacyArchiveSection,
    offset: int,
    limit: int,
) -> dict[str, Any]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    if offset < 0 or not 1 <= limit <= AGENT_LEGACY_ARCHIVE_INSPECT_MAX_ITEMS:
        raise BusinessValidationError("Agent 旧归档 inspect 分页参数无效")
    available_sections = _SECTIONS_BY_KIND.get(kind)
    if available_sections is None or section not in available_sections:
        raise BusinessValidationError("所选旧归档类型不支持该 inspect section")
    detail = get_legacy_archive_detail(
        session,
        kind=kind,
        archive_id=archive_id,
        include_assets=False,
    )
    if detail.item.product_id is not None and detail.item.product_id != conversation.product_id:
        raise NotFoundError("当前 Agent conversation 无权读取该旧归档")
    if section == "summary" and offset != 0:
        raise BusinessValidationError("旧归档 summary section 的 offset 必须为 0")

    if section == "summary":
        values: list[Any] = [
            {
                **_archive_item_metadata(detail.item),
                "source_profile": detail.source_profile,
                "source_fingerprint_sha256": detail.source_fingerprint_sha256,
                "available_sections": list(available_sections),
            }
        ]
        total = 1
    elif section == "assets":
        page = list_legacy_workflow_archive_assets(
            session,
            archive_id=archive_id,
            offset=offset,
            limit=limit,
        )
        values = [
            {
                "product_image_asset_id": asset.product_image_asset_id,
                "role": asset.role,
                "display_name": asset.display_name,
                "original_filename": asset.original_filename,
                "mime_type": asset.mime_type,
                "byte_size": asset.byte_size,
                "width": asset.width,
                "height": asset.height,
                "verification_status": asset.verification_status.value,
            }
            for asset in page.items
        ]
        total = page.total
    else:
        raw_values = _section_values(detail.payload_json, detail.diagnostics, section)
        total = len(raw_values)
        values = raw_values[offset : offset + limit]

    result = {
        "schema_version": 1,
        "archive_kind": kind,
        "archive_id": archive_id,
        "section": section,
        "offset": offset,
        "limit": limit,
        "total": total,
        "items": values,
        "has_more": offset + len(values) < total,
    }
    _require_bounded_output(result)
    return result


def _section_values(
    payload: dict[str, Any],
    retained_diagnostics: list[dict[str, Any]],
    section: AgentLegacyArchiveSection,
) -> list[Any]:
    if section == "diagnostics":
        payload_diagnostics = payload.get("diagnostics")
        values = payload_diagnostics if isinstance(payload_diagnostics, list) else retained_diagnostics
    elif section in {"template_nodes", "template_edges"}:
        template = payload.get("template_json")
        key = "nodes" if section == "template_nodes" else "edges"
        values = template.get(key) if isinstance(template, dict) else []
    else:
        value = payload.get(section)
        values = value if isinstance(value, list) else [value] if isinstance(value, dict) else []
    return [value for value in values if isinstance(value, (dict, list, str, int, float, bool)) or value is None]


def _archive_item_metadata(item: LegacyArchiveListItem) -> dict[str, Any]:
    return {
        "kind": item.kind,
        "id": item.id,
        "source_id": item.source_id,
        "source_key": item.source_key,
        "product_id": item.product_id,
        "product_name": item.product_name,
        "title": item.title,
        "description": item.description,
        "source_status": item.source_status,
        "archive_schema_version": item.archive_schema_version,
        "payload_sha256": item.payload_sha256,
        "source_updated_at": _datetime_json(item.source_updated_at),
        "created_at": _datetime_json(item.created_at),
        "counts": item.counts,
    }


def _seed_archive(
    seed: WorkflowDraftLegacyArchiveSeed,
) -> tuple[LegacyArchiveKind, str, LegacyWorkflowArchive | LegacyCanvasAgentArchive | LegacyUserTemplateArchive]:
    values = [
        ("workflow", seed.workflow_archive_id, seed.workflow_archive),
        ("canvas_agent_thread", seed.canvas_agent_archive_id, seed.canvas_agent_archive),
        ("user_template", seed.user_template_archive_id, seed.user_template_archive),
    ]
    selected = [(kind, archive_id, archive) for kind, archive_id, archive in values if archive_id is not None]
    if len(selected) != 1 or selected[0][2] is None:
        raise ConflictError("WorkflowDraft 旧归档重建 seed 不完整")
    kind, archive_id, archive = selected[0]
    return kind, archive_id, archive


def _seed_by_idempotency_key(
    session: Session,
    *,
    product_id: str,
    idempotency_key: str,
) -> WorkflowDraftLegacyArchiveSeed | None:
    return session.scalar(
        select(WorkflowDraftLegacyArchiveSeed).where(
            WorkflowDraftLegacyArchiveSeed.product_id == product_id,
            WorkflowDraftLegacyArchiveSeed.idempotency_key == idempotency_key,
        )
    )


def _load_rebuild_result(
    session: Session,
    seed_id: str,
    *,
    created: bool,
) -> LegacyArchiveRebuildResult:
    seed = session.scalar(
        select(WorkflowDraftLegacyArchiveSeed)
        .options(
            selectinload(WorkflowDraftLegacyArchiveSeed.workflow_archive),
            selectinload(WorkflowDraftLegacyArchiveSeed.canvas_agent_archive),
            selectinload(WorkflowDraftLegacyArchiveSeed.user_template_archive),
        )
        .where(WorkflowDraftLegacyArchiveSeed.id == seed_id)
    )
    if seed is None:
        raise NotFoundError("旧归档重建记录不存在")
    draft = get_workflow_draft_or_raise(
        session,
        product_id=seed.product_id,
        draft_id=seed.workflow_draft_id,
    )
    if draft.agent_conversation is None:
        raise ConflictError("旧归档重建记录缺少 Agent conversation")
    conversation = get_agent_conversation_or_raise(
        session,
        product_id=seed.product_id,
        conversation_id=draft.agent_conversation.id,
    )
    return LegacyArchiveRebuildResult(
        seed=seed,
        draft=draft,
        conversation=conversation,
        created=created,
    )


def _normalize_idempotency_key(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("idempotency key 不能为空")
    if len(normalized) > 120:
        raise BusinessValidationError("idempotency key 不能超过 120 个字符")
    return normalized


def _request_hash(*, kind: LegacyArchiveKind, archive_id: str, target_product_id: str) -> str:
    encoded = json.dumps(
        {"kind": kind, "archive_id": archive_id, "target_product_id": target_product_id},
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode()
    return hashlib.sha256(encoded).hexdigest()


def _require_bounded_output(value: dict[str, Any]) -> None:
    encoded = json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode()
    if len(encoded) > AGENT_LEGACY_ARCHIVE_MAX_OUTPUT_BYTES:
        raise ConflictError("旧归档工具结果超过 256 KiB，请缩小 limit 或选择更具体的 section")


def _datetime_json(value: datetime | None) -> str | None:
    if value is None:
        return None
    normalized = value if value.tzinfo is not None else value.replace(tzinfo=UTC)
    return normalized.astimezone(UTC).isoformat()


__all__ = [
    "AGENT_LEGACY_ARCHIVE_INSPECT_MAX_ITEMS",
    "AGENT_LEGACY_ARCHIVE_LIST_DEFAULT_LIMIT",
    "AGENT_LEGACY_ARCHIVE_LIST_MAX_LIMIT",
    "AgentLegacyArchiveSection",
    "LegacyArchiveRebuildResult",
    "create_legacy_archive_rebuild",
    "inspect_agent_legacy_archive",
    "legacy_archive_seed_summary",
    "list_agent_legacy_archives",
]
