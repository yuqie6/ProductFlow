from __future__ import annotations

import base64
import csv
import io
import json
from collections import Counter
from collections.abc import Mapping, Sequence
from datetime import UTC, datetime
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

import sqlalchemy as sa
from sqlalchemy.engine import Connection, Engine

from productflow_backend.application.legacy_retirement.audit import audit_legacy_retirement_connection
from productflow_backend.application.legacy_retirement.contracts import (
    ArchiveItemKind,
    LegacyArchiveAssetDeclaration,
    LegacyArchiveExportPage,
    LegacyArchiveSnapshot,
    LegacyArchiveSnapshotDiagnostic,
    LegacyRetirementAuditReport,
    archive_export_page_sha256,
    canonical_json_bytes,
    canonical_json_value,
    canonical_sha256,
)
from productflow_backend.application.legacy_retirement.profiles import (
    UNKNOWN_PROFILE,
    VISIBLE_CANVAS_EVENT_TYPES,
)
from productflow_backend.application.legacy_retirement.row_utils import optional_id, required_id, status_key
from productflow_backend.application.legacy_retirement.source import open_legacy_read_only_connection
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError

DEFAULT_ARCHIVE_EXPORT_PAGE_SIZE = 25
MAX_ARCHIVE_EXPORT_PAGE_SIZE = 100
MAX_ARCHIVE_CURSOR_LENGTH = 4096

_ARCHIVE_SENSITIVE_KEYS = frozenset(
    {
        "access_token",
        "api_key",
        "apikey",
        "authorization",
        "bearer_token",
        "checkpoint_ref",
        "client_secret",
        "id_token",
        "password",
        "provider_output_json",
        "provider_request_json",
        "raw_request",
        "raw_response",
        "refresh_token",
        "secret",
        "token",
    }
)
_ARCHIVE_STORAGE_PATH_KEYS = frozenset({"file_path", "local_path", "storage_path"})

_WORKFLOW_FIELDS = (
    "id",
    "product_id",
    "title",
    "active",
    "schema_version",
    "revision",
    "edit_version",
    "created_at",
    "updated_at",
)
_NODE_FIELDS = (
    "id",
    "workflow_id",
    "node_key",
    "node_type",
    "title",
    "schema_version",
    "status",
    "position_x",
    "position_y",
    "folder_id",
    "bound_image_asset_id",
    "config_json",
    "output_json",
    "failure_reason",
    "last_run_at",
    "created_at",
    "updated_at",
)
_EDGE_FIELDS = (
    "id",
    "workflow_id",
    "edge_key",
    "source_node_id",
    "target_node_id",
    "source_handle",
    "target_handle",
    "created_at",
)
_WORKFLOW_RUN_FIELDS = (
    "id",
    "workflow_id",
    "status",
    "failure_reason",
    "is_retryable",
    "progress_metadata",
    "started_at",
    "finished_at",
)
_NODE_RUN_FIELDS = (
    "id",
    "workflow_run_id",
    "node_id",
    "status",
    "output_json",
    "failure_reason",
    "copy_set_id",
    "poster_variant_id",
    "image_session_asset_id",
    "started_at",
    "finished_at",
)
_PRODUCT_FIELDS = (
    "id",
    "name",
    "category",
    "price",
    "source_note",
    "current_confirmed_copy_set_id",
    "created_at",
    "updated_at",
)
_BRIEF_FIELDS = (
    "id",
    "product_id",
    "payload",
    "provider_name",
    "model_name",
    "prompt_version",
    "created_at",
)
_COPY_FIELDS = (
    "id",
    "product_id",
    "creative_brief_id",
    "status",
    "structured_payload",
    "model_structured_payload",
    "provider_name",
    "model_name",
    "prompt_version",
    "edited_at",
    "confirmed_at",
    "created_at",
    "updated_at",
)
_POSTER_FIELDS = (
    "id",
    "product_id",
    "copy_set_id",
    "kind",
    "template_name",
    "mime_type",
    "width",
    "height",
    "canonical_asset_id",
    "created_at",
)
_SOURCE_ASSET_FIELDS = (
    "id",
    "product_id",
    "kind",
    "original_filename",
    "mime_type",
    "source_poster_variant_id",
    "canonical_asset_id",
    "created_at",
)
_IMAGE_SESSION_FIELDS = ("id", "product_id", "title", "created_at", "updated_at")
_IMAGE_SESSION_ASSET_FIELDS = (
    "id",
    "session_id",
    "kind",
    "original_filename",
    "mime_type",
    "media_object_id",
    "created_at",
)
_TEMPLATE_FIELDS = (
    "id",
    "key",
    "kind",
    "title",
    "description",
    "schema_version",
    "archived_at",
    "created_at",
    "updated_at",
)
_THREAD_FIELDS = (
    "id",
    "product_id",
    "title",
    "status",
    "last_message_at",
    "created_at",
    "updated_at",
)
_MESSAGE_FIELDS = ("id", "thread_id", "role", "content", "created_at")
_CANVAS_RUN_FIELDS = (
    "id",
    "thread_id",
    "status",
    "current_state",
    "failure_reason",
    "iteration_count",
    "created_at",
    "updated_at",
)
_TOOL_EVENT_FIELDS = (
    "id",
    "run_id",
    "tool_name",
    "permission_tier",
    "status",
    "created_at",
    "finished_at",
)
_PLAN_FIELDS = (
    "id",
    "run_id",
    "status",
    "template_key",
    "plan_json",
    "validation_json",
    "workflow_revision",
    "applied_workflow_id",
    "created_at",
    "approved_at",
    "applied_at",
)
_TASK_PLAN_FIELDS = ("id", "thread_id", "status", "task_plan_json", "created_at")
_VISIBLE_EVENT_PAYLOAD_FIELDS: dict[str, tuple[str, ...]] = {
    "approval_requested": ("intent", "plan_id", "rationale", "template_key", "validation", "workflow_revision"),
    "approval_resolved": ("plan_id", "status"),
    "assistant_message": ("message",),
    "canvas_command_applied": (
        "command",
        "command_id",
        "diff",
        "issues",
        "mode",
        "risk_tier",
        "status",
        "undo",
        "workflow_revision_after",
        "workflow_revision_before",
    ),
    "canvas_command_previewed": (
        "command",
        "command_id",
        "diff",
        "issues",
        "mode",
        "risk_tier",
        "status",
        "undo",
        "workflow_revision_after",
        "workflow_revision_before",
    ),
    "error": ("message",),
    "tool_call_started": (
        "permission_tier",
        "requires_approval",
        "status",
        "tool_event_id",
        "tool_name",
    ),
    "tool_result": (
        "permission_tier",
        "requires_approval",
        "status",
        "tool_event_id",
        "tool_name",
    ),
    "user_message": ("message",),
    "workflow_progress": ("message", "node_run_count", "node_status_counts", "status", "workflow_run_id"),
}


def export_legacy_archive_page(
    engine: Engine,
    *,
    storage_root: Path,
    kind: ArchiveItemKind,
    limit: int = DEFAULT_ARCHIVE_EXPORT_PAGE_SIZE,
    after: str = "",
    source_id: str | None = None,
    generated_at: datetime | None = None,
) -> LegacyArchiveExportPage:
    if limit < 1 or limit > MAX_ARCHIVE_EXPORT_PAGE_SIZE:
        raise BusinessValidationError(f"归档导出 page size 必须在 1 到 {MAX_ARCHIVE_EXPORT_PAGE_SIZE} 之间")
    normalized_source_id = source_id.strip() if source_id else None
    normalized_after = after.strip()
    if normalized_source_id and normalized_after:
        raise BusinessValidationError("单项归档导出不能同时提供 resume cursor")

    timestamp = generated_at or datetime.now(UTC)
    with open_legacy_read_only_connection(engine) as connection:
        report = audit_legacy_retirement_connection(
            connection,
            storage_root=storage_root,
            generated_at=timestamp,
        )
        if report.source.schema_profile == UNKNOWN_PROFILE:
            raise BusinessValidationError("未知 schema profile 不能生成归档快照")

        summary_by_id = _source_summaries(report, kind)
        source_ids = sorted(summary_by_id)
        if normalized_source_id is not None:
            if normalized_source_id not in summary_by_id:
                raise NotFoundError("未找到可归档的旧记录")
            selected_ids = [normalized_source_id]
            next_cursor = None
        else:
            last_id = _decode_cursor(
                normalized_after,
                kind=kind,
                source_profile=report.source.schema_profile,
                source_report_sha256=report.report_sha256,
            )
            selected_ids = [
                source_item_id
                for source_item_id in source_ids
                if last_id is None or source_item_id > last_id
            ]
            has_more = len(selected_ids) > limit
            selected_ids = selected_ids[:limit]
            next_cursor = (
                _encode_cursor(
                    kind=kind,
                    source_profile=report.source.schema_profile,
                    source_report_sha256=report.report_sha256,
                    last_id=selected_ids[-1],
                )
                if has_more and selected_ids
                else None
            )

        tables = _ReflectedTables(connection)
        snapshots = [
            _build_snapshot(
                connection,
                tables=tables,
                report=report,
                kind=kind,
                source_id=item_id,
                summary=summary_by_id[item_id],
            )
            for item_id in selected_ids
        ]
        provisional = LegacyArchiveExportPage(
            generated_at=timestamp,
            source_profile=report.source.schema_profile,
            source_report_sha256=report.report_sha256,
            source_fingerprint_sha256=report.source_fingerprint_sha256,
            blocking_issue_codes=sorted(issue.code for issue in report.issues if issue.severity == "blocking"),
            kind=kind,
            requested_after=normalized_after or None,
            next_cursor=next_cursor,
            items=snapshots,
            page_sha256="0" * 64,
        )
        return provisional.model_copy(update={"page_sha256": archive_export_page_sha256(provisional)})


def render_archive_export_csv(page: LegacyArchiveExportPage) -> str:
    output = io.StringIO(newline="")
    writer = csv.DictWriter(
        output,
        fieldnames=(
            "kind",
            "source_id",
            "product_id",
            "title",
            "source_updated_at",
            "source_fingerprint_sha256",
            "payload_sha256",
            "counts",
            "asset_declaration_count",
            "diagnostic_codes",
        ),
    )
    writer.writeheader()
    for snapshot in page.items:
        writer.writerow(
            {
                "kind": snapshot.kind,
                "source_id": snapshot.source_id,
                "product_id": snapshot.product_id or "",
                "title": snapshot.title,
                "source_updated_at": snapshot.source_updated_at.isoformat() if snapshot.source_updated_at else "",
                "source_fingerprint_sha256": snapshot.source_fingerprint_sha256,
                "payload_sha256": snapshot.payload_sha256,
                "counts": json.dumps(snapshot.counts, ensure_ascii=False, sort_keys=True, separators=(",", ":")),
                "asset_declaration_count": len(snapshot.asset_declarations),
                "diagnostic_codes": ",".join(diagnostic.code for diagnostic in snapshot.diagnostics),
            }
        )
    return output.getvalue()


def _source_summaries(report: LegacyRetirementAuditReport, kind: ArchiveItemKind) -> dict[str, Any]:
    if kind == "workflow":
        return {item.workflow_id: item for item in report.workflows.items if item.archive_candidate}
    if kind == "user_template":
        return {item.template_id: item for item in report.user_templates.items}
    return {item.thread_id: item for item in report.canvas_agent.items}


def _build_snapshot(
    connection: Connection,
    *,
    tables: _ReflectedTables,
    report: LegacyRetirementAuditReport,
    kind: ArchiveItemKind,
    source_id: str,
        summary: Any,
) -> LegacyArchiveSnapshot:
    if kind == "workflow":
        return _build_workflow_snapshot(
            connection,
            tables=tables,
            report=report,
            workflow_id=source_id,
            summary=summary,
        )
    if kind == "user_template":
        return _build_template_snapshot(
            connection,
            tables=tables,
            report=report,
            template_id=source_id,
            summary=summary,
        )
    return _build_canvas_snapshot(
        connection,
        tables=tables,
        report=report,
        thread_id=source_id,
        summary=summary,
    )


def _build_workflow_snapshot(
    connection: Connection,
    *,
    tables: _ReflectedTables,
    report: LegacyRetirementAuditReport,
    workflow_id: str,
    summary: Any,
) -> LegacyArchiveSnapshot:
    workflow = _one_row(connection, tables["product_workflows"], "id", workflow_id)
    product_id = required_id(workflow, "product_id")
    product_rows = _rows(connection, tables["products"], product_id, key="id")
    nodes = _rows(connection, tables["workflow_nodes"], workflow_id, key="workflow_id")
    edges = _rows(connection, tables["workflow_edges"], workflow_id, key="workflow_id")
    runs = _rows(connection, tables["workflow_runs"], workflow_id, key="workflow_id")
    run_ids = [required_id(row, "id") for row in runs]
    node_runs = _rows_grouped(connection, tables["workflow_node_runs"], run_ids, key="workflow_run_id")
    briefs = _rows_optional(connection, tables.optional("creative_briefs"), product_id, key="product_id")
    copies = _rows_optional(connection, tables.optional("copy_sets"), product_id, key="product_id")
    posters = _rows(connection, tables["poster_variants"], product_id, key="product_id")
    source_assets = _rows(connection, tables["source_assets"], product_id, key="product_id")
    canonical_asset_table = tables.optional("product_image_assets")
    canonical_assets = (
        _rows(connection, canonical_asset_table, product_id, key="product_id")
        if canonical_asset_table is not None
        else []
    )
    session_table = tables["image_sessions"]
    session_asset_table = tables["image_session_assets"]
    if "product_id" in session_table.c:
        sessions = _rows(connection, session_table, product_id, key="product_id")
        session_assets = _rows_grouped(
            connection,
            session_asset_table,
            [required_id(row, "id") for row in sessions],
            key="session_id",
        )
    else:
        linked_session_asset_ids = sorted(
            {
                source_id
                for row in canonical_assets
                if (source_id := optional_id(row, "source_image_session_asset_id")) is not None
            }
        )
        session_assets = _rows_for_ids(connection, session_asset_table, linked_session_asset_ids)
        sessions = _rows_for_ids(
            connection,
            session_table,
            sorted({required_id(row, "session_id") for row in session_assets}),
        )
    media_table = tables.optional("media_objects")
    media_by_id = (
        {
            required_id(row, "id"): row
            for row in _all_rows(connection, media_table)
        }
        if media_table is not None
        else {}
    )
    media_rows = [
        media_by_id[media_id]
        for row in canonical_assets
        if (media_id := optional_id(row, "media_object_id")) in media_by_id
    ]
    fingerprint_rows = {
        "products": product_rows,
        "product_workflows": [workflow],
        "workflow_nodes": nodes,
        "workflow_edges": edges,
        "workflow_runs": runs,
        "workflow_node_runs": node_runs,
        "image_sessions": sessions,
        "image_session_assets": session_assets,
        "product_image_assets": canonical_assets,
        "media_objects": media_rows,
        "creative_briefs": briefs,
        "copy_sets": copies,
        "poster_variants": posters,
        "source_assets": source_assets,
    }
    _assert_source_fingerprint(fingerprint_rows, summary.source_fingerprint_sha256)

    canonical_asset_id_by_session_asset_id = {
        source_id: required_id(row, "id")
        for row in canonical_assets
        if (source_id := optional_id(row, "source_image_session_asset_id")) is not None
    }
    declarations = _asset_declarations(
        product_id,
        source_assets=source_assets,
        posters=posters,
        session_assets=session_assets,
        canonical_asset_id_by_session_asset_id=canonical_asset_id_by_session_asset_id,
    )
    if len(declarations) != summary.legacy_asset_count:
        raise RuntimeError("归档快照资产声明数与只读审计不一致")
    media_id_by_asset_id = {
        required_id(row, "id"): required_id(row, "media_object_id")
        for row in canonical_assets
    }
    declared_media_ids = {
        media_id_by_asset_id[declaration.source_canonical_asset_id]
        for declaration in declarations
        if declaration.source_canonical_asset_id in media_id_by_asset_id
    }
    diagnostics = _asset_diagnostics(report, declarations, declared_media_ids=declared_media_ids)
    payload = canonical_json_value(
        {
            "schema_version": 1,
            "source_profile": report.source.schema_profile,
            "product": _project(product_rows[0] if product_rows else {}, _PRODUCT_FIELDS),
            "workflow": _project(workflow, _WORKFLOW_FIELDS),
            "nodes": [_project(row, _NODE_FIELDS) for row in _ordered_rows(nodes, "created_at", "id")],
            "edges": [_project(row, _EDGE_FIELDS) for row in _ordered_rows(edges, "created_at", "id")],
            "runs": [_project(row, _WORKFLOW_RUN_FIELDS) for row in _ordered_rows(runs, "started_at", "id")],
            "node_runs": [
                _project(row, _NODE_RUN_FIELDS)
                for row in _ordered_rows(node_runs, "started_at", "workflow_run_id", "id")
            ],
            "creative_briefs": [
                _project(row, _BRIEF_FIELDS) for row in _ordered_rows(briefs, "created_at", "id")
            ],
            "copy_sets": [_project(row, _COPY_FIELDS) for row in _ordered_rows(copies, "created_at", "id")],
            "poster_variants": [
                _project(row, _POSTER_FIELDS) for row in _ordered_rows(posters, "created_at", "id")
            ],
            "source_assets": [
                _project(row, _SOURCE_ASSET_FIELDS)
                for row in _ordered_rows(source_assets, "created_at", "id")
            ],
            "image_sessions": [
                _project(row, _IMAGE_SESSION_FIELDS)
                for row in _ordered_rows(sessions, "created_at", "id")
            ],
            "image_session_assets": [
                _project(row, _IMAGE_SESSION_ASSET_FIELDS)
                for row in _ordered_rows(session_assets, "created_at", "id")
            ],
            "asset_sources": [declaration.model_dump(mode="json") for declaration in declarations],
            "diagnostics": [diagnostic.model_dump(mode="json") for diagnostic in diagnostics],
        }
    )
    return LegacyArchiveSnapshot(
        kind="workflow",
        source_profile=report.source.schema_profile,
        source_id=workflow_id,
        product_id=product_id,
        title=str(workflow.get("title") or "旧工作流"),
        source_updated_at=workflow.get("updated_at"),
        payload_json=payload,
        source_fingerprint_sha256=summary.source_fingerprint_sha256,
        payload_sha256=canonical_sha256(payload),
        counts={
            "node_count": len(nodes),
            "edge_count": len(edges),
            "run_count": len(runs),
            "node_run_count": len(node_runs),
            "asset_declaration_count": len(declarations),
        },
        asset_declarations=declarations,
        diagnostics=diagnostics,
    )


def _build_template_snapshot(
    connection: Connection,
    *,
    tables: _ReflectedTables,
    report: LegacyRetirementAuditReport,
    template_id: str,
    summary: Any,
) -> LegacyArchiveSnapshot:
    row = _one_row(connection, tables["user_canvas_templates"], "id", template_id)
    _assert_source_fingerprint(row, summary.source_fingerprint_sha256)
    template_json = row.get("template_json")
    diagnostics: list[LegacyArchiveSnapshotDiagnostic] = []
    if not isinstance(template_json, Mapping):
        diagnostics.append(
            LegacyArchiveSnapshotDiagnostic(
                code="invalid_user_template_payload",
                count=1,
                detail="旧用户模板 payload 不是 JSON object，按损坏记录归档。",
            )
        )
        template_payload: dict[str, Any] | None = None
    else:
        template_payload = _archive_json_value(template_json)
    payload = canonical_json_value(
        {
            "schema_version": 1,
            "source_profile": report.source.schema_profile,
            "template": _project(row, _TEMPLATE_FIELDS),
            "template_json": template_payload,
            "diagnostics": [diagnostic.model_dump(mode="json") for diagnostic in diagnostics],
        }
    )
    return LegacyArchiveSnapshot(
        kind="user_template",
        source_profile=report.source.schema_profile,
        source_id=template_id,
        title=str(row.get("title") or row.get("key") or "旧用户模板"),
        source_updated_at=row.get("updated_at"),
        payload_json=payload,
        source_fingerprint_sha256=summary.source_fingerprint_sha256,
        payload_sha256=canonical_sha256(payload),
        counts={"diagnostic_count": len(diagnostics)},
        diagnostics=diagnostics,
    )


def _build_canvas_snapshot(
    connection: Connection,
    *,
    tables: _ReflectedTables,
    report: LegacyRetirementAuditReport,
    thread_id: str,
    summary: Any,
) -> LegacyArchiveSnapshot:
    thread = _one_row(connection, tables["canvas_agent_threads"], "id", thread_id)
    messages = _rows(connection, tables["canvas_agent_messages"], thread_id, key="thread_id")
    runs = _rows(connection, tables["canvas_agent_runs"], thread_id, key="thread_id")
    run_ids = [required_id(row, "id") for row in runs]
    tools = _rows_grouped(connection, tables["canvas_agent_tool_events"], sorted(run_ids), key="run_id")
    plans = _rows_grouped(connection, tables["canvas_agent_plans"], sorted(run_ids), key="run_id")
    tasks = _rows(connection, tables["canvas_agent_task_plans"], thread_id, key="thread_id")
    timeline = _rows(connection, tables["canvas_agent_timeline_events"], thread_id, key="thread_id")
    fingerprint_rows = {
        "canvas_agent_threads": [thread],
        "canvas_agent_messages": messages,
        "canvas_agent_runs": runs,
        "canvas_agent_tool_events": tools,
        "canvas_agent_plans": plans,
        "canvas_agent_task_plans": tasks,
        "canvas_agent_timeline_events": timeline,
    }
    _assert_source_fingerprint(fingerprint_rows, summary.source_fingerprint_sha256)

    visible_timeline = [
        _safe_visible_event(row)
        for row in sorted(timeline, key=_timeline_sort_key)
        if status_key(row.get("type")) in VISIBLE_CANVAS_EVENT_TYPES
    ]
    event_counts = Counter(status_key(row.get("type")) for row in timeline)
    technical_counts = {
        event_type: count
        for event_type, count in sorted(event_counts.items())
        if event_type not in VISIBLE_CANVAS_EVENT_TYPES
    }
    technical_events = [
        row
        for row in timeline
        if status_key(row.get("type")) not in VISIBLE_CANVAS_EVENT_TYPES
    ]
    diagnostics: list[LegacyArchiveSnapshotDiagnostic] = []
    if summary.runs_without_terminal_evidence:
        diagnostics.append(
            LegacyArchiveSnapshotDiagnostic(
                code="canvas_run_terminal_evidence_missing",
                count=summary.runs_without_terminal_evidence,
                detail="部分旧 Agent run 没有用户可见终态事件，保留源状态并标记诊断。",
            )
        )
    payload = canonical_json_value(
        {
            "schema_version": 1,
            "source_profile": report.source.schema_profile,
            "thread": _project(thread, _THREAD_FIELDS),
            "messages": [
                _project(row, _MESSAGE_FIELDS)
                for row in _ordered_rows(messages, "created_at", "id")
            ],
            "runs": [
                _project(row, _CANVAS_RUN_FIELDS)
                for row in _ordered_rows(runs, "created_at", "id")
            ],
            "tool_events": [
                _project(row, _TOOL_EVENT_FIELDS)
                for row in _ordered_rows(tools, "created_at", "id")
            ],
            "plans": [
                _project(row, _PLAN_FIELDS)
                for row in _ordered_rows(plans, "created_at", "id")
            ],
            "task_plans": [
                _project(row, _TASK_PLAN_FIELDS)
                for row in _ordered_rows(tasks, "created_at", "id")
            ],
            "visible_timeline": visible_timeline,
            "technical_event_type_counts": technical_counts,
            "technical_events_source_sha256": canonical_sha256(technical_events),
            "diagnostics": [diagnostic.model_dump(mode="json") for diagnostic in diagnostics],
        }
    )
    return LegacyArchiveSnapshot(
        kind="canvas_agent_thread",
        source_profile=report.source.schema_profile,
        source_id=thread_id,
        product_id=required_id(thread, "product_id"),
        title=str(thread.get("title") or "旧 Agent 对话"),
        source_updated_at=thread.get("updated_at"),
        payload_json=payload,
        source_fingerprint_sha256=summary.source_fingerprint_sha256,
        payload_sha256=canonical_sha256(payload),
        counts={
            "message_count": len(messages),
            "run_count": len(runs),
            "tool_event_count": len(tools),
            "plan_count": len(plans),
            "task_plan_count": len(tasks),
            "timeline_event_count": len(timeline),
            "visible_event_count": len(visible_timeline),
            "technical_event_count": sum(technical_counts.values()),
        },
        diagnostics=diagnostics,
    )


def _asset_declarations(
    product_id: str,
    *,
    source_assets: Sequence[dict[str, Any]],
    posters: Sequence[dict[str, Any]],
    session_assets: Sequence[dict[str, Any]],
    canonical_asset_id_by_session_asset_id: Mapping[str, str],
) -> list[LegacyArchiveAssetDeclaration]:
    declarations = [
        LegacyArchiveAssetDeclaration(
            role=str(row.get("kind") or "source_asset"),
            legacy_source_type="source_asset",
            legacy_source_id=required_id(row, "id"),
            product_id=product_id,
            source_canonical_asset_id=optional_id(row, "canonical_asset_id"),
        )
        for row in source_assets
    ]
    declarations.extend(
        LegacyArchiveAssetDeclaration(
            role="poster",
            legacy_source_type="poster_variant",
            legacy_source_id=required_id(row, "id"),
            product_id=product_id,
            source_canonical_asset_id=optional_id(row, "canonical_asset_id"),
        )
        for row in posters
    )
    declarations.extend(
        LegacyArchiveAssetDeclaration(
            role=str(row.get("kind") or "image_session_asset"),
            legacy_source_type="image_session_asset",
            legacy_source_id=required_id(row, "id"),
            product_id=product_id,
            source_canonical_asset_id=canonical_asset_id_by_session_asset_id.get(required_id(row, "id")),
        )
        for row in session_assets
    )
    declarations.sort(key=lambda item: (item.legacy_source_type, item.legacy_source_id, item.role))
    return declarations


def _asset_diagnostics(
    report: LegacyRetirementAuditReport,
    declarations: Sequence[LegacyArchiveAssetDeclaration],
    *,
    declared_media_ids: set[str],
) -> list[LegacyArchiveSnapshotDiagnostic]:
    declaration_keys = {
        (
            {
                "source_asset": "source_assets",
                "poster_variant": "poster_variants",
                "image_session_asset": "image_session_assets",
            }[item.legacy_source_type],
            item.legacy_source_id,
        )
        for item in declarations
    }
    declaration_keys.update(("media_objects", media_id) for media_id in declared_media_ids)
    diagnostics: list[LegacyArchiveSnapshotDiagnostic] = []
    missing_mapping = sum(item.source_canonical_asset_id is None for item in declarations)
    if missing_mapping:
        diagnostics.append(
            LegacyArchiveSnapshotDiagnostic(
                code="canonical_asset_mapping_absent_in_source",
                count=missing_mapping,
                detail="源 profile 尚无 canonical asset ID；backfill 必须从目标 lineage 显式解析。",
            )
        )
    media_problem_counts = Counter(
        problem.reason
        for problem in report.media.problems
        if (problem.record_type, problem.record_id) in declaration_keys
    )
    for reason, count in sorted(media_problem_counts.items()):
        diagnostics.append(
            LegacyArchiveSnapshotDiagnostic(
                code=f"asset_media_{reason}",
                count=count,
                detail=(
                    "归档声明的媒体文件缺失，历史下载必须显示 unavailable。"
                    if reason == "missing"
                    else "归档声明含非法 storage path，不能据此建立下载关系。"
                ),
            )
        )
    return diagnostics


def _safe_visible_event(row: Mapping[str, Any]) -> dict[str, Any]:
    event_type = status_key(row.get("type"))
    payload = row.get("payload_json")
    safe_payload = (
        _project(payload, _VISIBLE_EVENT_PAYLOAD_FIELDS.get(event_type, ()))
        if isinstance(payload, Mapping)
        else {}
    )
    return canonical_json_value(
        {
            "id": row.get("id"),
            "run_id": row.get("run_id"),
            "sequence": row.get("sequence"),
            "type": event_type,
            "payload": safe_payload,
            "created_at": row.get("created_at"),
        }
    )


def _project(row: Mapping[str, Any], fields: Sequence[str]) -> dict[str, Any]:
    return canonical_json_value(
        {
            field: _archive_json_value(row[field], field_name=field)
            for field in fields
            if field in row
        }
    )


def _ordered_rows(rows: Sequence[dict[str, Any]], *keys: str) -> list[dict[str, Any]]:
    return sorted(
        rows,
        key=lambda row: (
            *(canonical_json_bytes(row.get(key)) for key in keys),
            canonical_json_bytes(row),
        ),
    )


def _timeline_sort_key(row: Mapping[str, Any]) -> tuple[int, bytes, bytes, bytes]:
    raw_sequence = row.get("sequence")
    try:
        sequence = int(raw_sequence)
    except (TypeError, ValueError):
        sequence = 0
    return (
        sequence,
        canonical_json_bytes(row.get("created_at")),
        canonical_json_bytes(row.get("id")),
        canonical_json_bytes(row),
    )


def _archive_json_value(value: Any, *, field_name: str | None = None) -> Any:
    normalized_field = field_name.lower().replace("-", "_") if field_name else ""
    if _is_sensitive_archive_field(normalized_field):
        return {"_archive_omitted": "sensitive_field"}
    if _is_storage_path_field(normalized_field):
        return {"_archive_omitted": "storage_path"}
    if isinstance(value, Mapping):
        return canonical_json_value(
            {
                str(key): _archive_json_value(item, field_name=str(key))
                for key, item in value.items()
            }
        )
    if isinstance(value, (list, tuple)):
        return [_archive_json_value(item) for item in value]
    if isinstance(value, str):
        if value.lstrip().lower().startswith("data:"):
            return {"_archive_omitted": "inline_data"}
        if _is_url_field(normalized_field):
            parsed = urlsplit(value)
            is_safe_https = (
                parsed.scheme == "https"
                and bool(parsed.netloc)
                and parsed.username is None
                and parsed.password is None
                and not parsed.query
                and not parsed.fragment
            )
            if value and not is_safe_https:
                return {"_archive_omitted": "unsafe_url"}
    return canonical_json_value(value)


def _is_sensitive_archive_field(field_name: str) -> bool:
    return field_name in _ARCHIVE_SENSITIVE_KEYS or field_name.endswith(
        ("_access_token", "_api_key", "_authorization", "_password", "_refresh_token", "_secret")
    )


def _is_storage_path_field(field_name: str) -> bool:
    return field_name in _ARCHIVE_STORAGE_PATH_KEYS or field_name.endswith(
        ("_file_path", "_local_path", "_storage_path")
    )


def _is_url_field(field_name: str) -> bool:
    return field_name in {"uri", "url"} or field_name.endswith(("_uri", "_url"))


def _assert_source_fingerprint(value: Any, expected: str) -> None:
    if canonical_sha256(value) != expected:
        raise RuntimeError("归档快照查询与只读审计 source fingerprint 不一致")


class _ReflectedTables:
    def __init__(self, connection: Connection) -> None:
        self._connection = connection
        self._metadata = sa.MetaData()
        self._tables: dict[str, sa.Table] = {}
        self._table_names = frozenset(sa.inspect(connection).get_table_names())

    def __getitem__(self, table_name: str) -> sa.Table:
        table = self._tables.get(table_name)
        if table is None:
            table = sa.Table(table_name, self._metadata, autoload_with=self._connection)
            self._tables[table_name] = table
        return table

    def optional(self, table_name: str) -> sa.Table | None:
        if table_name not in self._table_names:
            return None
        return self[table_name]


def _one_row(connection: Connection, table: sa.Table, key: str, value: str) -> dict[str, Any]:
    row = connection.execute(sa.select(table).where(table.c[key] == value)).mappings().one()
    return dict(row)


def _rows(connection: Connection, table: sa.Table, value: str, *, key: str) -> list[dict[str, Any]]:
    rows = [dict(row) for row in connection.execute(sa.select(table).where(table.c[key] == value)).mappings()]
    rows.sort(key=canonical_json_bytes)
    return rows


def _rows_if_column(connection: Connection, table: sa.Table, value: str, *, key: str) -> list[dict[str, Any]]:
    if key not in table.c:
        return []
    return _rows(connection, table, value, key=key)


def _rows_optional(
    connection: Connection,
    table: sa.Table | None,
    value: str,
    *,
    key: str,
) -> list[dict[str, Any]]:
    if table is None:
        return []
    return _rows(connection, table, value, key=key)


def _rows_grouped(connection: Connection, table: sa.Table, values: Sequence[str], *, key: str) -> list[dict[str, Any]]:
    return [row for value in values for row in _rows(connection, table, value, key=key)]


def _rows_for_ids(connection: Connection, table: sa.Table, values: Sequence[str]) -> list[dict[str, Any]]:
    if not values:
        return []
    rows = [
        dict(row)
        for row in connection.execute(sa.select(table).where(table.c.id.in_(values))).mappings()
    ]
    rows.sort(key=canonical_json_bytes)
    return rows


def _all_rows(connection: Connection, table: sa.Table) -> list[dict[str, Any]]:
    rows = [dict(row) for row in connection.execute(sa.select(table)).mappings()]
    rows.sort(key=canonical_json_bytes)
    return rows


def _encode_cursor(
    *,
    kind: ArchiveItemKind,
    source_profile: str,
    source_report_sha256: str,
    last_id: str,
) -> str:
    payload = canonical_json_bytes(
        {
            "version": 1,
            "kind": kind,
            "source_profile": source_profile,
            "source_report_sha256": source_report_sha256,
            "last_id": last_id,
        }
    )
    return base64.urlsafe_b64encode(payload).decode("ascii").rstrip("=")


def _decode_cursor(
    value: str,
    *,
    kind: ArchiveItemKind,
    source_profile: str,
    source_report_sha256: str,
) -> str | None:
    normalized = value.strip()
    if not normalized:
        return None
    if len(normalized) > MAX_ARCHIVE_CURSOR_LENGTH:
        raise BusinessValidationError("归档 resume cursor 无效")
    try:
        padding = "=" * (-len(normalized) % 4)
        raw = base64.b64decode(normalized + padding, altchars=b"-_", validate=True)
        payload = json.loads(raw)
        if not isinstance(payload, dict) or set(payload) != {
            "version",
            "kind",
            "source_profile",
            "source_report_sha256",
            "last_id",
        }:
            raise ValueError
        if payload["version"] != 1:
            raise ValueError
        if payload["kind"] != kind or payload["source_profile"] != source_profile:
            raise ValueError
        if payload["source_report_sha256"] != source_report_sha256:
            raise BusinessValidationError("归档 resume cursor 对应的源审计报告已变化，请重新开始 dry-run")
        last_id = payload["last_id"]
        if not isinstance(last_id, str) or not last_id:
            raise ValueError
        return last_id
    except BusinessValidationError:
        raise
    except (ValueError, TypeError, json.JSONDecodeError, UnicodeDecodeError) as exc:
        raise BusinessValidationError("归档 resume cursor 无效") from exc


__all__ = [
    "DEFAULT_ARCHIVE_EXPORT_PAGE_SIZE",
    "MAX_ARCHIVE_CURSOR_LENGTH",
    "MAX_ARCHIVE_EXPORT_PAGE_SIZE",
    "export_legacy_archive_page",
    "render_archive_export_csv",
]
