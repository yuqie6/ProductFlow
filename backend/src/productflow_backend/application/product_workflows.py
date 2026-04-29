from __future__ import annotations

import logging

from productflow_backend.application import product_workflow_graph
from productflow_backend.application.product_workflow_artifacts import (
    _copy_node_output,
    _create_context_copy_set,
    _fill_reference_node,
    _GeneratedWorkflowImage,
    _image_asset_output,
    _source_asset_for_poster_variant,
)
from productflow_backend.application.product_workflow_context import (
    _collect_incoming_context,
    _configured_text,
    _downstream_reference_nodes,
    _effective_product_context,
    _find_source_asset,
    _image_instruction_with_context,
    _image_size_from_config,
    _IncomingContext,
    _instruction_with_upstream_text,
    _optional_config_text,
    _output_text,
    _poster_kind_from_config,
    _product_context_values,
    _reference_assets_for_image_generation,
    _reference_image_inputs_for_copy,
    _source_asset_ids_from_config,
)
from productflow_backend.application.product_workflow_dependencies import (
    WorkflowExecutionDependencies,
    default_workflow_execution_dependencies,
)
from productflow_backend.application.product_workflow_execution import (
    WorkflowRunKickoff,
    _active_workflow_run,
    _claim_workflow_node_run,
    _execute_copy_generation,
    _execute_image_generation,
    _execute_node,
    _execute_product_context,
    _execute_product_workflow_run,
    _execute_reference_image,
    _generate_workflow_images_concurrently,
    _image_generation_filled_reference_target,
    _mark_workflow_run_failed,
    _node_has_reusable_output,
    _node_has_valid_reference_assets,
    _node_ids_to_run,
    _should_execute_missing_upstream,
    _valid_source_asset_ids,
    _workflow_run_should_enqueue,
    execute_product_workflow_run,
    mark_workflow_run_enqueue_failed,
    run_product_workflow,
    start_product_workflow_run,
    submit_product_workflow_run,
)
from productflow_backend.application.product_workflow_mutations import (
    _normalize_product_context_singleton,
    bind_workflow_node_image,
    create_workflow_edge,
    create_workflow_node,
    delete_workflow_edge,
    delete_workflow_node,
    get_or_create_product_workflow,
    update_workflow_copy_set,
    update_workflow_node,
    upload_workflow_node_image,
)
from productflow_backend.infrastructure.db.models import ProductWorkflow, WorkflowRun
from productflow_backend.infrastructure.image.factory import get_image_provider
from productflow_backend.infrastructure.text.factory import get_text_provider

logger = logging.getLogger(__name__)


def latest_workflow_runs(workflow: ProductWorkflow, limit: int = 10) -> list[WorkflowRun]:
    return product_workflow_graph.latest_workflow_runs(workflow, limit=limit)


def get_product_workflow_status(session, product_id: str) -> product_workflow_graph.ProductWorkflowStatusSnapshot:
    return product_workflow_graph.get_active_workflow_status(session, product_id)


__all__ = [
    "_GeneratedWorkflowImage",
    "_IncomingContext",
    "_active_workflow_run",
    "_claim_workflow_node_run",
    "_collect_incoming_context",
    "_configured_text",
    "_copy_node_output",
    "_create_context_copy_set",
    "_downstream_reference_nodes",
    "_effective_product_context",
    "_execute_copy_generation",
    "_execute_image_generation",
    "_execute_node",
    "_execute_product_context",
    "_execute_product_workflow_run",
    "_execute_reference_image",
    "_fill_reference_node",
    "_find_source_asset",
    "_generate_workflow_images_concurrently",
    "_image_asset_output",
    "_image_generation_filled_reference_target",
    "_image_instruction_with_context",
    "_image_size_from_config",
    "_instruction_with_upstream_text",
    "_mark_workflow_run_failed",
    "_node_has_reusable_output",
    "_node_has_valid_reference_assets",
    "_node_ids_to_run",
    "_normalize_product_context_singleton",
    "_optional_config_text",
    "_output_text",
    "_poster_kind_from_config",
    "_product_context_values",
    "_reference_assets_for_image_generation",
    "_reference_image_inputs_for_copy",
    "_should_execute_missing_upstream",
    "_source_asset_for_poster_variant",
    "_source_asset_ids_from_config",
    "_valid_source_asset_ids",
    "_workflow_run_should_enqueue",
    "WorkflowRunKickoff",
    "WorkflowExecutionDependencies",
    "bind_workflow_node_image",
    "create_workflow_edge",
    "create_workflow_node",
    "delete_workflow_edge",
    "delete_workflow_node",
    "default_workflow_execution_dependencies",
    "execute_product_workflow_run",
    "get_image_provider",
    "get_or_create_product_workflow",
    "get_product_workflow_status",
    "get_text_provider",
    "latest_workflow_runs",
    "logger",
    "mark_workflow_run_enqueue_failed",
    "run_product_workflow",
    "start_product_workflow_run",
    "submit_product_workflow_run",
    "update_workflow_copy_set",
    "update_workflow_node",
    "upload_workflow_node_image",
]
