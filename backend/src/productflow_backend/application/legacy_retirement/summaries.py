from __future__ import annotations

from collections import Counter
from collections.abc import Mapping
from pathlib import Path
from typing import Any

from productflow_backend.application.legacy_retirement.contracts import (
    CanvasAgentAuditSummary,
    CanvasAgentThreadSourceSummary,
    MediaAuditSummary,
    MediaPathProblem,
    SchemaProfile,
    UserTemplateAuditSummary,
    UserTemplateSourceSummary,
    WorkflowAuditSummary,
    WorkflowSourceSummary,
    canonical_json_bytes,
    canonical_sha256,
)
from productflow_backend.application.legacy_retirement.profiles import (
    CANVAS_TERMINAL_EVIDENCE_TYPES,
    CURRENT_ARCHIVE_PROFILE,
    CURRENT_CANONICAL_PROFILE,
    LEGACY_CANVAS_PROFILE,
    PRE_AGENT_WORKSPACE_PROFILE,
    PRE_REBUILD_ARCHIVE_PROFILE,
    VISIBLE_CANVAS_EVENT_TYPES,
)
from productflow_backend.application.legacy_retirement.row_utils import (
    as_bool,
    count_field,
    group_rows,
    optional_id,
    optional_int,
    required_id,
    row_index,
    status_key,
)


def audit_workflows(
    profile: SchemaProfile,
    rows_by_table: Mapping[str, list[dict[str, Any]]],
) -> WorkflowAuditSummary:
    workflows = rows_by_table.get("product_workflows", [])
    nodes_by_workflow = group_rows(rows_by_table.get("workflow_nodes", []), "workflow_id")
    edges_by_workflow = group_rows(rows_by_table.get("workflow_edges", []), "workflow_id")
    runs_by_workflow = group_rows(rows_by_table.get("workflow_runs", []), "workflow_id")
    node_runs_by_run = group_rows(rows_by_table.get("workflow_node_runs", []), "workflow_run_id")
    product_rows = group_rows(rows_by_table.get("products", []), "id")
    product_artifact_tables = ("creative_briefs", "copy_sets", "poster_variants", "source_assets")
    product_artifacts = {
        table_name: group_rows(rows_by_table.get(table_name, []), "product_id")
        for table_name in product_artifact_tables
    }
    sessions_by_product = group_rows(rows_by_table.get("image_sessions", []), "product_id")
    session_assets_by_session = group_rows(rows_by_table.get("image_session_assets", []), "session_id")
    sessions_by_id = row_index(rows_by_table.get("image_sessions", []))
    session_assets_by_id = row_index(rows_by_table.get("image_session_assets", []))
    canonical_assets_by_product = group_rows(rows_by_table.get("product_image_assets", []), "product_id")
    media_by_id = row_index(rows_by_table.get("media_objects", []))

    items: list[WorkflowSourceSummary] = []
    for workflow in workflows:
        workflow_id = required_id(workflow, "id")
        product_id = required_id(workflow, "product_id")
        nodes = nodes_by_workflow.get(workflow_id, [])
        edges = edges_by_workflow.get(workflow_id, [])
        runs = runs_by_workflow.get(workflow_id, [])
        node_runs = [row for run in runs for row in node_runs_by_run.get(required_id(run, "id"), [])]
        legacy_assets = [
            *product_artifacts["poster_variants"].get(product_id, []),
            *product_artifacts["source_assets"].get(product_id, []),
        ]
        canonical_assets = canonical_assets_by_product.get(product_id, [])
        product_sessions = sessions_by_product.get(product_id, [])
        if profile == LEGACY_CANVAS_PROFILE:
            session_assets = [
                asset
                for session in product_sessions
                for asset in session_assets_by_session.get(required_id(session, "id"), [])
            ]
        else:
            linked_session_asset_ids = {
                source_id
                for asset in canonical_assets
                if (source_id := optional_id(asset, "source_image_session_asset_id")) is not None
            }
            session_assets = [
                asset
                for asset_id, asset in session_assets_by_id.items()
                if asset_id in linked_session_asset_ids
            ]
            linked_session_ids = {required_id(asset, "session_id") for asset in session_assets}
            product_sessions = [
                session
                for session_id, session in sessions_by_id.items()
                if session_id in linked_session_ids
            ]
        canonical_media = [
            media_by_id[media_id]
            for asset in canonical_assets
            if (media_id := optional_id(asset, "media_object_id")) in media_by_id
        ]
        fingerprint_rows: dict[str, list[dict[str, Any]]] = {
            "products": product_rows.get(product_id, []),
            "product_workflows": [workflow],
            "workflow_nodes": nodes,
            "workflow_edges": edges,
            "workflow_runs": runs,
            "workflow_node_runs": node_runs,
            "image_sessions": product_sessions,
            "image_session_assets": session_assets,
            "product_image_assets": canonical_assets,
            "media_objects": canonical_media,
        }
        for table_name in product_artifact_tables:
            fingerprint_rows[table_name] = product_artifacts[table_name].get(product_id, [])

        schema_version = optional_int(workflow.get("schema_version"))
        archive_candidate = profile == LEGACY_CANVAS_PROFILE or schema_version == 1
        items.append(
            WorkflowSourceSummary(
                workflow_id=workflow_id,
                product_id=product_id,
                schema_version=schema_version,
                active=as_bool(workflow.get("active")),
                archive_candidate=archive_candidate,
                node_count=len(nodes),
                edge_count=len(edges),
                run_count=len(runs),
                node_run_count=len(node_runs),
                legacy_asset_count=len(legacy_assets) + len(session_assets),
                canonical_asset_count=len(canonical_assets),
                node_status_counts=count_field(nodes, "status"),
                run_status_counts=count_field(runs, "status"),
                node_run_status_counts=count_field(node_runs, "status"),
                source_fingerprint_sha256=canonical_sha256(fingerprint_rows),
            )
        )

    items.sort(key=lambda item: item.workflow_id)
    return WorkflowAuditSummary(
        workflow_count=len(items),
        archive_candidate_count=sum(item.archive_candidate for item in items),
        active_workflow_count=sum(item.active for item in items),
        node_type_counts=count_field(rows_by_table.get("workflow_nodes", []), "node_type"),
        node_status_counts=count_field(rows_by_table.get("workflow_nodes", []), "status"),
        run_status_counts=count_field(rows_by_table.get("workflow_runs", []), "status"),
        node_run_status_counts=count_field(rows_by_table.get("workflow_node_runs", []), "status"),
        items=items,
    )


def audit_user_templates(rows_by_table: Mapping[str, list[dict[str, Any]]]) -> UserTemplateAuditSummary:
    items = [
        UserTemplateSourceSummary(
            template_id=required_id(row, "id"),
            key=str(row.get("key") or ""),
            schema_version=optional_int(row.get("schema_version")),
            archived=row.get("archived_at") is not None,
            source_fingerprint_sha256=canonical_sha256(row),
        )
        for row in rows_by_table.get("user_canvas_templates", [])
    ]
    items.sort(key=lambda item: item.template_id)
    return UserTemplateAuditSummary(
        template_count=len(items),
        archived_count=sum(item.archived for item in items),
        items=items,
    )


def audit_canvas_agent(
    rows_by_table: Mapping[str, list[dict[str, Any]]],
    table_names: frozenset[str],
) -> CanvasAgentAuditSummary:
    threads = rows_by_table.get("canvas_agent_threads", [])
    messages_by_thread = group_rows(rows_by_table.get("canvas_agent_messages", []), "thread_id")
    runs_by_thread = group_rows(rows_by_table.get("canvas_agent_runs", []), "thread_id")
    tools_by_run = group_rows(rows_by_table.get("canvas_agent_tool_events", []), "run_id")
    plans_by_run = group_rows(rows_by_table.get("canvas_agent_plans", []), "run_id")
    tasks_by_thread = group_rows(rows_by_table.get("canvas_agent_task_plans", []), "thread_id")
    timeline_by_thread = group_rows(rows_by_table.get("canvas_agent_timeline_events", []), "thread_id")

    items: list[CanvasAgentThreadSourceSummary] = []
    for thread in threads:
        thread_id = required_id(thread, "id")
        runs = runs_by_thread.get(thread_id, [])
        run_ids = {required_id(run, "id") for run in runs}
        tools = [row for run_id in sorted(run_ids) for row in tools_by_run.get(run_id, [])]
        plans = [row for run_id in sorted(run_ids) for row in plans_by_run.get(run_id, [])]
        messages = messages_by_thread.get(thread_id, [])
        tasks = tasks_by_thread.get(thread_id, [])
        timeline = timeline_by_thread.get(thread_id, [])
        event_counts = count_field(timeline, "type")
        terminal_evidence_by_run: dict[str, int] = Counter(
            required_id(event, "run_id")
            for event in timeline
            if optional_id(event, "run_id") is not None
            and status_key(event.get("type")) in CANVAS_TERMINAL_EVIDENCE_TYPES
        )
        missing_terminal = sum(
            1
            for run in runs
            if terminal_evidence_by_run.get(required_id(run, "id"), 0) == 0
        )
        fingerprint_rows = {
            "canvas_agent_threads": [thread],
            "canvas_agent_messages": messages,
            "canvas_agent_runs": runs,
            "canvas_agent_tool_events": tools,
            "canvas_agent_plans": plans,
            "canvas_agent_task_plans": tasks,
            "canvas_agent_timeline_events": timeline,
        }
        items.append(
            CanvasAgentThreadSourceSummary(
                thread_id=thread_id,
                product_id=required_id(thread, "product_id"),
                status=status_key(thread.get("status")),
                message_count=len(messages),
                run_count=len(runs),
                tool_event_count=len(tools),
                plan_count=len(plans),
                task_plan_count=len(tasks),
                timeline_event_count=len(timeline),
                visible_event_count=sum(event_counts.get(name, 0) for name in VISIBLE_CANVAS_EVENT_TYPES),
                technical_event_count=sum(
                    count for name, count in event_counts.items() if name not in VISIBLE_CANVAS_EVENT_TYPES
                ),
                runs_without_terminal_evidence=missing_terminal,
                run_status_counts=count_field(runs, "status"),
                event_type_counts=event_counts,
                source_fingerprint_sha256=canonical_sha256(fingerprint_rows),
            )
        )
    items.sort(key=lambda item: item.thread_id)

    timeline_counts = count_field(rows_by_table.get("canvas_agent_timeline_events", []), "type")
    canvas_tables = (
        "canvas_agent_threads",
        "canvas_agent_messages",
        "canvas_agent_runs",
        "canvas_agent_tool_events",
        "canvas_agent_plans",
        "canvas_agent_task_plans",
        "canvas_agent_timeline_events",
    )
    return CanvasAgentAuditSummary(
        present="canvas_agent_threads" in table_names,
        thread_count=len(threads),
        message_count=len(rows_by_table.get("canvas_agent_messages", [])),
        run_count=len(rows_by_table.get("canvas_agent_runs", [])),
        tool_event_count=len(rows_by_table.get("canvas_agent_tool_events", [])),
        plan_count=len(rows_by_table.get("canvas_agent_plans", [])),
        task_plan_count=len(rows_by_table.get("canvas_agent_task_plans", [])),
        timeline_event_count=len(rows_by_table.get("canvas_agent_timeline_events", [])),
        thread_status_counts=count_field(threads, "status"),
        run_status_counts=count_field(rows_by_table.get("canvas_agent_runs", []), "status"),
        tool_status_counts=count_field(rows_by_table.get("canvas_agent_tool_events", []), "status"),
        plan_status_counts=count_field(rows_by_table.get("canvas_agent_plans", []), "status"),
        task_plan_status_counts=count_field(rows_by_table.get("canvas_agent_task_plans", []), "status"),
        visible_event_type_counts={
            key: value for key, value in timeline_counts.items() if key in VISIBLE_CANVAS_EVENT_TYPES
        },
        technical_event_type_counts={
            key: value for key, value in timeline_counts.items() if key not in VISIBLE_CANVAS_EVENT_TYPES
        },
        source_bytes_by_table={
            table_name: sum(len(canonical_json_bytes(row)) for row in rows_by_table.get(table_name, []))
            for table_name in canvas_tables
            if table_name in table_names
        },
        items=items,
    )


def audit_media(
    profile: SchemaProfile,
    rows_by_table: Mapping[str, list[dict[str, Any]]],
    storage_root: Path,
) -> MediaAuditSummary:
    root = storage_root.expanduser().resolve()
    if profile in {
        CURRENT_CANONICAL_PROFILE,
        PRE_REBUILD_ARCHIVE_PROFILE,
        PRE_AGENT_WORKSPACE_PROFILE,
        CURRENT_ARCHIVE_PROFILE,
    }:
        source_tables = ("media_objects",)
    elif profile == LEGACY_CANVAS_PROFILE:
        source_tables = ("source_assets", "poster_variants", "image_session_assets")
    else:
        source_tables = ()

    records = [
        (table_name, required_id(row, "id"), str(row.get("storage_path") or ""))
        for table_name in source_tables
        for row in rows_by_table.get(table_name, [])
    ]
    problems: list[MediaPathProblem] = []
    present_count = 0
    for table_name, record_id, storage_path in records:
        resolved = _resolve_storage_path(root, storage_path)
        if resolved is None:
            problems.append(
                MediaPathProblem(
                    record_type=table_name,
                    record_id=record_id,
                    storage_path=storage_path,
                    reason="invalid_path",
                )
            )
        elif resolved.is_file():
            present_count += 1
        else:
            problems.append(
                MediaPathProblem(
                    record_type=table_name,
                    record_id=record_id,
                    storage_path=storage_path,
                    reason="missing",
                )
            )
    problems.sort(key=lambda item: (item.reason, item.record_type, item.record_id))
    missing_count = sum(problem.reason == "missing" for problem in problems)
    invalid_count = sum(problem.reason == "invalid_path" for problem in problems)
    return MediaAuditSummary(
        storage_root=str(root),
        storage_root_exists=root.is_dir(),
        declared_record_count=len(records),
        unique_path_count=len({storage_path for _, _, storage_path in records}),
        present_record_count=present_count,
        missing_record_count=missing_count,
        invalid_path_record_count=invalid_count,
        verification_status_counts=count_field(rows_by_table.get("media_objects", []), "verification_status"),
        problems=problems,
    )


def empty_workflow_summary() -> WorkflowAuditSummary:
    return WorkflowAuditSummary(
        workflow_count=0,
        archive_candidate_count=0,
        active_workflow_count=0,
        node_type_counts={},
        node_status_counts={},
        run_status_counts={},
        node_run_status_counts={},
        items=[],
    )


def empty_template_summary() -> UserTemplateAuditSummary:
    return UserTemplateAuditSummary(template_count=0, archived_count=0, items=[])


def empty_canvas_summary(*, present: bool) -> CanvasAgentAuditSummary:
    return CanvasAgentAuditSummary(
        present=present,
        thread_count=0,
        message_count=0,
        run_count=0,
        tool_event_count=0,
        plan_count=0,
        task_plan_count=0,
        timeline_event_count=0,
        thread_status_counts={},
        run_status_counts={},
        tool_status_counts={},
        plan_status_counts={},
        task_plan_status_counts={},
        visible_event_type_counts={},
        technical_event_type_counts={},
        source_bytes_by_table={},
        items=[],
    )


def _resolve_storage_path(root: Path, storage_path: str) -> Path | None:
    relative = Path(storage_path)
    if not storage_path or relative.is_absolute():
        return None
    resolved = (root / relative).resolve()
    try:
        resolved.relative_to(root)
    except ValueError:
        return None
    return resolved


__all__ = [
    "audit_canvas_agent",
    "audit_media",
    "audit_user_templates",
    "audit_workflows",
    "empty_canvas_summary",
    "empty_template_summary",
    "empty_workflow_summary",
]
