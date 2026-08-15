from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass

from productflow_backend.application.legacy_retirement.contracts import SchemaProfile

LEGACY_CANVAS_PROFILE: SchemaProfile = "legacy_canvas_agent_20260518_0032"
CURRENT_CANONICAL_PROFILE: SchemaProfile = "current_canonical_20260814_0038"
PRE_REBUILD_ARCHIVE_PROFILE: SchemaProfile = "current_with_legacy_archives_20260815_0039"
PRE_AGENT_WORKSPACE_PROFILE: SchemaProfile = "current_with_legacy_archive_rebuilds_20260815_0040"
CURRENT_ARCHIVE_PROFILE: SchemaProfile = "current_with_agent_workspace_finalization_20260815_0041"
UNKNOWN_PROFILE: SchemaProfile = "unknown"

VISIBLE_CANVAS_EVENT_TYPES = frozenset(
    {
        "approval_requested",
        "approval_resolved",
        "assistant_message",
        "canvas_command_applied",
        "canvas_command_previewed",
        "error",
        "tool_call_started",
        "tool_result",
        "user_message",
        "workflow_progress",
    }
)
CANVAS_RUN_ACTIVE_STATUSES = frozenset({"running", "needs_approval", "applying", "verifying"})
CANVAS_RUN_TERMINAL_STATUSES = frozenset({"completed", "failed", "cancelled"})
CANVAS_RUN_KNOWN_STATUSES = CANVAS_RUN_ACTIVE_STATUSES | CANVAS_RUN_TERMINAL_STATUSES
CANVAS_TERMINAL_EVIDENCE_TYPES = frozenset(
    {"approval_requested", "assistant_done", "assistant_message", "error"}
)
WORKFLOW_RUN_ACTIVE_STATUSES = frozenset({"running"})
WORKFLOW_RUN_KNOWN_STATUSES = frozenset({"running", "succeeded", "failed", "cancelled"})
WORKFLOW_NODE_ACTIVE_STATUSES = frozenset({"queued", "running"})
WORKFLOW_NODE_KNOWN_STATUSES = frozenset({"idle", "queued", "running", "succeeded", "failed"})

RELEVANT_TABLES = (
    "products",
    "product_workflows",
    "workflow_nodes",
    "workflow_edges",
    "workflow_runs",
    "workflow_node_runs",
    "creative_briefs",
    "copy_sets",
    "poster_variants",
    "source_assets",
    "user_canvas_templates",
    "image_sessions",
    "image_session_assets",
    "image_gallery_entries",
    "media_objects",
    "product_image_assets",
    "legacy_workflow_archives",
    "legacy_workflow_archive_assets",
    "legacy_user_template_archives",
    "legacy_canvas_agent_archives",
    "workflow_draft_legacy_archive_seeds",
    "agent_conversations",
    "canvas_agent_threads",
    "canvas_agent_messages",
    "canvas_agent_runs",
    "canvas_agent_tool_events",
    "canvas_agent_plans",
    "canvas_agent_task_plans",
    "canvas_agent_timeline_events",
)


@dataclass(frozen=True, slots=True)
class _ProfileDefinition:
    name: SchemaProfile
    revisions: frozenset[str]
    required_columns: Mapping[str, frozenset[str]]
    forbidden_tables: frozenset[str] = frozenset()
    forbidden_columns: Mapping[str, frozenset[str]] | None = None


_PROFILE_DEFINITIONS = (
    _ProfileDefinition(
        name=LEGACY_CANVAS_PROFILE,
        revisions=frozenset({"20260518_0032"}),
        required_columns={
            "products": frozenset({"id"}),
            "product_workflows": frozenset({"id", "product_id", "active"}),
            "workflow_nodes": frozenset({"id", "workflow_id", "node_type", "status"}),
            "workflow_edges": frozenset({"id", "workflow_id", "source_node_id", "target_node_id"}),
            "workflow_runs": frozenset({"id", "workflow_id", "status"}),
            "workflow_node_runs": frozenset(
                {"id", "workflow_run_id", "node_id", "status", "image_session_asset_id"}
            ),
            "source_assets": frozenset({"id", "product_id", "storage_path", "source_poster_variant_id"}),
            "poster_variants": frozenset({"id", "product_id", "copy_set_id", "storage_path"}),
            "user_canvas_templates": frozenset({"id", "key", "schema_version", "template_json"}),
            "image_sessions": frozenset({"id", "product_id"}),
            "image_session_assets": frozenset({"id", "session_id", "storage_path"}),
            "canvas_agent_threads": frozenset({"id", "product_id", "status"}),
            "canvas_agent_messages": frozenset({"id", "thread_id", "role", "content"}),
            "canvas_agent_runs": frozenset(
                {"id", "thread_id", "status", "current_state", "checkpoint_ref", "goal_state_json"}
            ),
            "canvas_agent_tool_events": frozenset({"id", "run_id", "tool_name", "status"}),
            "canvas_agent_plans": frozenset({"id", "run_id", "status", "applied_workflow_id"}),
            "canvas_agent_task_plans": frozenset({"id", "thread_id", "status", "task_plan_json"}),
            "canvas_agent_timeline_events": frozenset(
                {"id", "thread_id", "run_id", "sequence", "type", "payload_json"}
            ),
        },
        forbidden_tables=frozenset({"media_objects", "product_image_assets"}),
        forbidden_columns={
            "product_workflows": frozenset({"schema_version"}),
            "source_assets": frozenset({"canonical_asset_id"}),
            "poster_variants": frozenset({"canonical_asset_id"}),
            "image_session_assets": frozenset({"media_object_id"}),
        },
    ),
    _ProfileDefinition(
        name=CURRENT_CANONICAL_PROFILE,
        revisions=frozenset({"20260814_0038"}),
        required_columns={
            "products": frozenset({"id", "cover_image_asset_id"}),
            "product_workflows": frozenset(
                {"id", "product_id", "active", "schema_version", "revision", "edit_version"}
            ),
            "workflow_nodes": frozenset(
                {"id", "workflow_id", "schema_version", "node_key", "folder_id", "bound_image_asset_id"}
            ),
            "workflow_edges": frozenset({"id", "workflow_id", "source_node_id", "target_node_id", "edge_key"}),
            "workflow_runs": frozenset({"id", "workflow_id", "status"}),
            "workflow_node_runs": frozenset({"id", "workflow_run_id", "node_id", "status"}),
            "source_assets": frozenset({"id", "product_id", "storage_path", "canonical_asset_id"}),
            "poster_variants": frozenset({"id", "product_id", "storage_path", "canonical_asset_id"}),
            "user_canvas_templates": frozenset({"id", "key", "schema_version", "template_json"}),
            "image_sessions": frozenset({"id", "title"}),
            "image_session_assets": frozenset({"id", "session_id", "storage_path", "media_object_id"}),
            "media_objects": frozenset({"id", "storage_path", "verification_status"}),
            "product_image_assets": frozenset({"id", "product_id", "media_object_id"}),
        },
        forbidden_tables=frozenset(
            {
                "canvas_agent_threads",
                "canvas_agent_messages",
                "canvas_agent_runs",
                "canvas_agent_tool_events",
                "canvas_agent_plans",
                "canvas_agent_task_plans",
                "canvas_agent_timeline_events",
                "legacy_workflow_archives",
                "legacy_workflow_archive_assets",
                "legacy_user_template_archives",
                "legacy_canvas_agent_archives",
                "workflow_draft_legacy_archive_seeds",
            }
        ),
        forbidden_columns={
            "products": frozenset({"user_id"}),
            "workflow_node_runs": frozenset({"image_session_asset_id"}),
            "image_sessions": frozenset({"product_id", "user_id"}),
            "image_gallery_entries": frozenset({"user_id"}),
            "user_canvas_templates": frozenset({"user_id"}),
        },
    ),
    _ProfileDefinition(
        name=PRE_REBUILD_ARCHIVE_PROFILE,
        revisions=frozenset({"20260815_0039"}),
        required_columns={
            "products": frozenset({"id", "cover_image_asset_id"}),
            "product_workflows": frozenset(
                {"id", "product_id", "active", "schema_version", "revision", "edit_version"}
            ),
            "workflow_nodes": frozenset(
                {"id", "workflow_id", "schema_version", "node_key", "folder_id", "bound_image_asset_id"}
            ),
            "workflow_edges": frozenset({"id", "workflow_id", "source_node_id", "target_node_id", "edge_key"}),
            "workflow_runs": frozenset({"id", "workflow_id", "status"}),
            "workflow_node_runs": frozenset({"id", "workflow_run_id", "node_id", "status"}),
            "source_assets": frozenset({"id", "product_id", "storage_path", "canonical_asset_id"}),
            "poster_variants": frozenset({"id", "product_id", "storage_path", "canonical_asset_id"}),
            "user_canvas_templates": frozenset({"id", "key", "schema_version", "template_json"}),
            "image_sessions": frozenset({"id", "title"}),
            "image_session_assets": frozenset({"id", "session_id", "storage_path", "media_object_id"}),
            "media_objects": frozenset({"id", "storage_path", "verification_status"}),
            "product_image_assets": frozenset({"id", "product_id", "media_object_id"}),
            "legacy_workflow_archives": frozenset(
                {
                    "id",
                    "source_profile",
                    "legacy_workflow_id",
                    "product_id",
                    "archive_schema_version",
                    "payload_json",
                    "source_fingerprint_sha256",
                    "payload_sha256",
                }
            ),
            "legacy_workflow_archive_assets": frozenset(
                {"id", "archive_id", "product_image_asset_id", "role", "legacy_source_type", "legacy_source_id"}
            ),
            "legacy_user_template_archives": frozenset(
                {
                    "id",
                    "source_profile",
                    "legacy_template_id",
                    "archive_status",
                    "archive_schema_version",
                    "payload_json",
                    "source_fingerprint_sha256",
                    "payload_sha256",
                }
            ),
            "legacy_canvas_agent_archives": frozenset(
                {
                    "id",
                    "source_profile",
                    "legacy_thread_id",
                    "product_id",
                    "archive_schema_version",
                    "payload_json",
                    "source_fingerprint_sha256",
                    "payload_sha256",
                }
            ),
        },
        forbidden_tables=frozenset(
            {
                "canvas_agent_threads",
                "canvas_agent_messages",
                "canvas_agent_runs",
                "canvas_agent_tool_events",
                "canvas_agent_plans",
                "canvas_agent_task_plans",
                "canvas_agent_timeline_events",
                "workflow_draft_legacy_archive_seeds",
            }
        ),
        forbidden_columns={
            "products": frozenset({"user_id"}),
            "workflow_node_runs": frozenset({"image_session_asset_id"}),
            "image_sessions": frozenset({"product_id", "user_id"}),
            "image_gallery_entries": frozenset({"user_id"}),
            "user_canvas_templates": frozenset({"user_id"}),
        },
    ),
)

_PRE_REBUILD_ARCHIVE_DEFINITION = _PROFILE_DEFINITIONS[-1]
_PROFILE_DEFINITIONS = (
    *_PROFILE_DEFINITIONS,
    _ProfileDefinition(
        name=PRE_AGENT_WORKSPACE_PROFILE,
        revisions=frozenset({"20260815_0040"}),
        required_columns={
            **_PRE_REBUILD_ARCHIVE_DEFINITION.required_columns,
            "workflow_draft_legacy_archive_seeds": frozenset(
                {
                    "id",
                    "workflow_draft_id",
                    "product_id",
                    "workflow_archive_id",
                    "canvas_agent_archive_id",
                    "user_template_archive_id",
                    "schema_version",
                    "idempotency_key",
                    "request_hash",
                }
            ),
        },
        forbidden_tables=_PRE_REBUILD_ARCHIVE_DEFINITION.forbidden_tables
        - {"workflow_draft_legacy_archive_seeds"},
        forbidden_columns=_PRE_REBUILD_ARCHIVE_DEFINITION.forbidden_columns,
    ),
)

_PRE_AGENT_WORKSPACE_DEFINITION = _PROFILE_DEFINITIONS[-1]
_PROFILE_DEFINITIONS = (
    *_PROFILE_DEFINITIONS,
    _ProfileDefinition(
        name=CURRENT_ARCHIVE_PROFILE,
        revisions=frozenset({"20260815_0041"}),
        required_columns={
            **_PRE_AGENT_WORKSPACE_DEFINITION.required_columns,
            "agent_conversations": frozenset(
                {
                    "id",
                    "product_id",
                    "workflow_draft_id",
                    "harness_run_id",
                    "creation_idempotency_key",
                    "creation_request_hash",
                    "intake_idempotency_key",
                    "intake_request_hash",
                }
            ),
        },
        forbidden_tables=_PRE_AGENT_WORKSPACE_DEFINITION.forbidden_tables,
        forbidden_columns=_PRE_AGENT_WORKSPACE_DEFINITION.forbidden_columns,
    ),
)


def recognize_profile(
    revisions: list[str],
    columns_by_table: Mapping[str, frozenset[str]],
    table_names: frozenset[str],
) -> tuple[SchemaProfile, list[str]]:
    revision_set = frozenset(revisions)
    revision_candidates = [definition for definition in _PROFILE_DEFINITIONS if definition.revisions == revision_set]
    if not revision_candidates:
        return UNKNOWN_PROFILE, [f"Alembic revision 不受支持: {','.join(revisions) if revisions else '<missing>'}"]

    all_diagnostics: list[str] = []
    for definition in revision_candidates:
        diagnostics = _profile_diagnostics(definition, columns_by_table, table_names)
        if not diagnostics:
            return definition.name, []
        all_diagnostics.extend(diagnostics)
    return UNKNOWN_PROFILE, sorted(set(all_diagnostics))


def _profile_diagnostics(
    definition: _ProfileDefinition,
    columns_by_table: Mapping[str, frozenset[str]],
    table_names: frozenset[str],
) -> list[str]:
    diagnostics: list[str] = []
    for table_name, required_columns in definition.required_columns.items():
        if table_name not in table_names:
            diagnostics.append(f"缺少关键表: {table_name}")
            continue
        missing = required_columns - columns_by_table.get(table_name, frozenset())
        if missing:
            diagnostics.append(f"关键表 {table_name} 缺列: {','.join(sorted(missing))}")
    for table_name in sorted(definition.forbidden_tables & table_names):
        diagnostics.append(f"存在不应属于该 profile 的表: {table_name}")
    for table_name, forbidden_columns in (definition.forbidden_columns or {}).items():
        present = forbidden_columns & columns_by_table.get(table_name, frozenset())
        if present:
            diagnostics.append(f"关键表 {table_name} 存在冲突列: {','.join(sorted(present))}")
    return diagnostics


__all__ = [
    "CANVAS_RUN_ACTIVE_STATUSES",
    "CANVAS_RUN_KNOWN_STATUSES",
    "CANVAS_TERMINAL_EVIDENCE_TYPES",
    "CURRENT_ARCHIVE_PROFILE",
    "CURRENT_CANONICAL_PROFILE",
    "LEGACY_CANVAS_PROFILE",
    "PRE_AGENT_WORKSPACE_PROFILE",
    "PRE_REBUILD_ARCHIVE_PROFILE",
    "RELEVANT_TABLES",
    "UNKNOWN_PROFILE",
    "VISIBLE_CANVAS_EVENT_TYPES",
    "WORKFLOW_NODE_ACTIVE_STATUSES",
    "WORKFLOW_NODE_KNOWN_STATUSES",
    "WORKFLOW_RUN_ACTIVE_STATUSES",
    "WORKFLOW_RUN_KNOWN_STATUSES",
    "recognize_profile",
]
