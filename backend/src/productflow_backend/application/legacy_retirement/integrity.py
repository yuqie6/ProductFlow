from __future__ import annotations

from collections import Counter
from collections.abc import Mapping
from typing import Any

from productflow_backend.application.legacy_retirement.row_utils import (
    optional_id,
    required_id,
    row_index,
)

INTEGRITY_KEYS = (
    "workflow_missing_product",
    "node_missing_workflow",
    "edge_missing_workflow",
    "edge_missing_source_node",
    "edge_missing_target_node",
    "edge_cross_workflow_source",
    "edge_cross_workflow_target",
    "run_missing_workflow",
    "node_run_missing_run",
    "node_run_missing_node",
    "node_run_cross_workflow",
    "node_run_missing_image_session_asset",
    "product_artifact_missing_product",
    "copy_missing_brief",
    "copy_cross_product_brief",
    "poster_missing_copy",
    "poster_cross_product_copy",
    "source_missing_poster",
    "source_cross_product_poster",
    "image_session_missing_product",
    "image_session_asset_missing_session",
    "gallery_entry_missing_asset",
    "product_image_asset_missing_product",
    "product_image_asset_missing_media",
    "product_image_asset_missing_parent",
    "product_image_asset_cross_product_parent",
    "product_image_asset_missing_session_asset",
    "source_missing_canonical_asset",
    "source_cross_product_canonical_asset",
    "poster_missing_canonical_asset",
    "poster_cross_product_canonical_asset",
    "workflow_node_missing_bound_asset",
    "workflow_node_cross_product_bound_asset",
    "canvas_thread_missing_product",
    "canvas_message_missing_thread",
    "canvas_run_missing_thread",
    "canvas_tool_event_missing_run",
    "canvas_plan_missing_run",
    "canvas_plan_missing_applied_workflow",
    "canvas_plan_cross_product_workflow",
    "canvas_task_plan_missing_thread",
    "canvas_timeline_missing_thread",
    "canvas_timeline_missing_run",
    "invalid_user_template_payload",
)


def audit_integrity(rows_by_table: Mapping[str, list[dict[str, Any]]]) -> dict[str, int]:
    counts: Counter[str] = Counter()
    products = row_index(rows_by_table.get("products", []))
    workflows = row_index(rows_by_table.get("product_workflows", []))
    nodes = row_index(rows_by_table.get("workflow_nodes", []))
    runs = row_index(rows_by_table.get("workflow_runs", []))
    briefs = row_index(rows_by_table.get("creative_briefs", []))
    copies = row_index(rows_by_table.get("copy_sets", []))
    posters = row_index(rows_by_table.get("poster_variants", []))
    sessions = row_index(rows_by_table.get("image_sessions", []))
    session_assets = row_index(rows_by_table.get("image_session_assets", []))
    product_assets = row_index(rows_by_table.get("product_image_assets", []))
    media = row_index(rows_by_table.get("media_objects", []))
    canvas_threads = row_index(rows_by_table.get("canvas_agent_threads", []))
    canvas_runs = row_index(rows_by_table.get("canvas_agent_runs", []))

    for workflow in workflows.values():
        if required_id(workflow, "product_id") not in products:
            counts["workflow_missing_product"] += 1
    for node in nodes.values():
        workflow_id = required_id(node, "workflow_id")
        if workflow_id not in workflows:
            counts["node_missing_workflow"] += 1
        bound_asset_id = optional_id(node, "bound_image_asset_id")
        if bound_asset_id is not None:
            if bound_asset_id not in product_assets:
                counts["workflow_node_missing_bound_asset"] += 1
            elif workflow_id in workflows and (
                required_id(product_assets[bound_asset_id], "product_id")
                != required_id(workflows[workflow_id], "product_id")
            ):
                counts["workflow_node_cross_product_bound_asset"] += 1
    for edge in rows_by_table.get("workflow_edges", []):
        workflow_id = required_id(edge, "workflow_id")
        source_id = required_id(edge, "source_node_id")
        target_id = required_id(edge, "target_node_id")
        if workflow_id not in workflows:
            counts["edge_missing_workflow"] += 1
        if source_id not in nodes:
            counts["edge_missing_source_node"] += 1
        elif required_id(nodes[source_id], "workflow_id") != workflow_id:
            counts["edge_cross_workflow_source"] += 1
        if target_id not in nodes:
            counts["edge_missing_target_node"] += 1
        elif required_id(nodes[target_id], "workflow_id") != workflow_id:
            counts["edge_cross_workflow_target"] += 1
    for run in runs.values():
        if required_id(run, "workflow_id") not in workflows:
            counts["run_missing_workflow"] += 1
    for node_run in rows_by_table.get("workflow_node_runs", []):
        run_id = required_id(node_run, "workflow_run_id")
        node_id = required_id(node_run, "node_id")
        if run_id not in runs:
            counts["node_run_missing_run"] += 1
        if node_id not in nodes:
            counts["node_run_missing_node"] += 1
        if run_id in runs and node_id in nodes and (
            required_id(runs[run_id], "workflow_id") != required_id(nodes[node_id], "workflow_id")
        ):
            counts["node_run_cross_workflow"] += 1
        session_asset_id = optional_id(node_run, "image_session_asset_id")
        if session_asset_id is not None and session_asset_id not in session_assets:
            counts["node_run_missing_image_session_asset"] += 1

    for table_name in ("creative_briefs", "copy_sets", "poster_variants", "source_assets"):
        for row in rows_by_table.get(table_name, []):
            if required_id(row, "product_id") not in products:
                counts["product_artifact_missing_product"] += 1
    for copy in copies.values():
        brief_id = optional_id(copy, "creative_brief_id")
        if brief_id is None:
            continue
        if brief_id not in briefs:
            counts["copy_missing_brief"] += 1
        elif required_id(copy, "product_id") != required_id(briefs[brief_id], "product_id"):
            counts["copy_cross_product_brief"] += 1
    for poster in posters.values():
        copy_id = optional_id(poster, "copy_set_id")
        if copy_id is None or copy_id not in copies:
            counts["poster_missing_copy"] += 1
        elif required_id(poster, "product_id") != required_id(copies[copy_id], "product_id"):
            counts["poster_cross_product_copy"] += 1
    for source in rows_by_table.get("source_assets", []):
        poster_id = optional_id(source, "source_poster_variant_id")
        if poster_id is not None:
            if poster_id not in posters:
                counts["source_missing_poster"] += 1
            elif required_id(source, "product_id") != required_id(posters[poster_id], "product_id"):
                counts["source_cross_product_poster"] += 1
    for session in sessions.values():
        product_id = optional_id(session, "product_id")
        if product_id is not None and product_id not in products:
            counts["image_session_missing_product"] += 1
    for asset in session_assets.values():
        if required_id(asset, "session_id") not in sessions:
            counts["image_session_asset_missing_session"] += 1
    for entry in rows_by_table.get("image_gallery_entries", []):
        if required_id(entry, "image_session_asset_id") not in session_assets:
            counts["gallery_entry_missing_asset"] += 1

    for asset in product_assets.values():
        product_id = required_id(asset, "product_id")
        if product_id not in products:
            counts["product_image_asset_missing_product"] += 1
        if required_id(asset, "media_object_id") not in media:
            counts["product_image_asset_missing_media"] += 1
        parent_id = optional_id(asset, "parent_asset_id")
        if parent_id is not None:
            if parent_id not in product_assets:
                counts["product_image_asset_missing_parent"] += 1
            elif product_id != required_id(product_assets[parent_id], "product_id"):
                counts["product_image_asset_cross_product_parent"] += 1
        source_session_asset_id = optional_id(asset, "source_image_session_asset_id")
        if source_session_asset_id is not None and source_session_asset_id not in session_assets:
            counts["product_image_asset_missing_session_asset"] += 1
    for table_name, missing_key, cross_key in (
        ("source_assets", "source_missing_canonical_asset", "source_cross_product_canonical_asset"),
        ("poster_variants", "poster_missing_canonical_asset", "poster_cross_product_canonical_asset"),
    ):
        for row in rows_by_table.get(table_name, []):
            canonical_id = optional_id(row, "canonical_asset_id")
            if "canonical_asset_id" not in row or canonical_id is None:
                continue
            if canonical_id not in product_assets:
                counts[missing_key] += 1
            elif required_id(row, "product_id") != required_id(product_assets[canonical_id], "product_id"):
                counts[cross_key] += 1

    for thread in canvas_threads.values():
        if required_id(thread, "product_id") not in products:
            counts["canvas_thread_missing_product"] += 1
    for message in rows_by_table.get("canvas_agent_messages", []):
        if required_id(message, "thread_id") not in canvas_threads:
            counts["canvas_message_missing_thread"] += 1
    for run in canvas_runs.values():
        if required_id(run, "thread_id") not in canvas_threads:
            counts["canvas_run_missing_thread"] += 1
    for tool in rows_by_table.get("canvas_agent_tool_events", []):
        if required_id(tool, "run_id") not in canvas_runs:
            counts["canvas_tool_event_missing_run"] += 1
    for plan in rows_by_table.get("canvas_agent_plans", []):
        run_id = required_id(plan, "run_id")
        if run_id not in canvas_runs:
            counts["canvas_plan_missing_run"] += 1
        applied_workflow_id = optional_id(plan, "applied_workflow_id")
        if applied_workflow_id is None:
            continue
        if applied_workflow_id not in workflows:
            counts["canvas_plan_missing_applied_workflow"] += 1
        elif run_id in canvas_runs:
            thread_id = required_id(canvas_runs[run_id], "thread_id")
            if thread_id in canvas_threads and (
                required_id(canvas_threads[thread_id], "product_id")
                != required_id(workflows[applied_workflow_id], "product_id")
            ):
                counts["canvas_plan_cross_product_workflow"] += 1
    for task in rows_by_table.get("canvas_agent_task_plans", []):
        if required_id(task, "thread_id") not in canvas_threads:
            counts["canvas_task_plan_missing_thread"] += 1
    for event in rows_by_table.get("canvas_agent_timeline_events", []):
        if required_id(event, "thread_id") not in canvas_threads:
            counts["canvas_timeline_missing_thread"] += 1
        run_id = optional_id(event, "run_id")
        if run_id is not None and run_id not in canvas_runs:
            counts["canvas_timeline_missing_run"] += 1
    for template in rows_by_table.get("user_canvas_templates", []):
        if not isinstance(template.get("template_json"), Mapping):
            counts["invalid_user_template_payload"] += 1

    return {key: counts[key] for key in INTEGRITY_KEYS}


__all__ = ["INTEGRITY_KEYS", "audit_integrity"]
