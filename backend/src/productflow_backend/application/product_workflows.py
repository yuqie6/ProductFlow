from __future__ import annotations

from productflow_backend.application import product_workflow_graph
from productflow_backend.application.canvas_templates import list_builtin_canvas_templates
from productflow_backend.application.product_workflow_execution import (
    WorkflowRunKickoff,
    cancel_product_workflow_run,
    execute_product_workflow_run,
    mark_workflow_run_enqueue_failed,
    retry_product_workflow_run,
    run_product_workflow,
    start_product_workflow_run,
    submit_product_workflow_run,
)
from productflow_backend.application.product_workflow_mutations import (
    apply_node_group_template_to_workflow,
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


def latest_workflow_runs(workflow: ProductWorkflow, limit: int = 10) -> list[WorkflowRun]:
    return product_workflow_graph.latest_workflow_runs(workflow, limit=limit)


def get_product_workflow_status(session, product_id: str) -> product_workflow_graph.ProductWorkflowStatusSnapshot:
    return product_workflow_graph.get_active_workflow_status(session, product_id)


__all__ = [
    "WorkflowRunKickoff",
    "apply_node_group_template_to_workflow",
    "bind_workflow_node_image",
    "cancel_product_workflow_run",
    "create_workflow_edge",
    "create_workflow_node",
    "delete_workflow_edge",
    "delete_workflow_node",
    "execute_product_workflow_run",
    "get_or_create_product_workflow",
    "get_product_workflow_status",
    "latest_workflow_runs",
    "list_builtin_canvas_templates",
    "mark_workflow_run_enqueue_failed",
    "retry_product_workflow_run",
    "run_product_workflow",
    "start_product_workflow_run",
    "submit_product_workflow_run",
    "update_workflow_copy_set",
    "update_workflow_node",
    "upload_workflow_node_image",
]
