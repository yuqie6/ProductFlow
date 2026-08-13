from __future__ import annotations

from typing import TYPE_CHECKING

from productflow_backend.application.product_workflow.execution import (
    WorkflowRunKickoff,
    cancel_product_workflow_run,
    execute_product_workflow_node_run,
    execute_product_workflow_run,
    mark_workflow_run_enqueue_failed,
    retry_product_workflow_run,
    run_product_workflow,
    start_product_workflow_run,
    submit_product_workflow_run,
)
from productflow_backend.application.product_workflow.folders import (
    WorkflowCanvasMutationResult,
    WorkflowNodePosition,
    create_workflow_folder,
    dissolve_workflow_folder,
    rename_workflow_folder,
    set_workflow_folder_members,
    translate_workflow_folder,
    update_workflow_node_layout,
)
from productflow_backend.application.product_workflow.graph import (
    get_active_workflow_status as _get_active_workflow_status,
)
from productflow_backend.application.product_workflow.graph import (
    latest_workflow_runs as _latest_workflow_runs,
)
from productflow_backend.application.product_workflow.mutations import (
    AppliedWorkflowTemplateGroup,
    apply_node_group_template_to_workflow,
    bind_workflow_node_image,
    create_workflow_edge,
    create_workflow_node,
    delete_workflow_edge,
    delete_workflow_node,
    duplicate_workflow_node_group,
    get_or_create_product_workflow,
    materialize_node_group_template_to_workflow,
    update_workflow_copy_set,
    update_workflow_node,
    upload_workflow_node_image,
)
from productflow_backend.application.product_workflow.node_config import normalize_workflow_node_config
from productflow_backend.application.product_workflow.user_templates import (
    archive_user_canvas_template,
    create_user_canvas_template_from_workflow_nodes,
    list_canvas_templates,
    rename_user_canvas_template,
)
from productflow_backend.application.product_workflow.v2_reference_bindings import (
    V2ReferenceBindingResult,
    bind_v2_reference_node_asset,
)
from productflow_backend.application.product_workflow.v2_runs import (
    V2WorkflowNodeRunSubmission,
    get_v2_workflow_node_run,
    submit_v2_workflow_node_run,
)

if TYPE_CHECKING:
    from productflow_backend.application.product_workflow.graph import ProductWorkflowStatusSnapshot
    from productflow_backend.infrastructure.db.models import ProductWorkflow, WorkflowRun


def latest_workflow_runs(workflow: ProductWorkflow, limit: int = 10) -> list[WorkflowRun]:
    return _latest_workflow_runs(workflow, limit=limit)


def get_product_workflow_status(session, product_id: str) -> ProductWorkflowStatusSnapshot:
    return _get_active_workflow_status(session, product_id)


__all__ = [
    "WorkflowRunKickoff",
    "V2WorkflowNodeRunSubmission",
    "V2ReferenceBindingResult",
    "AppliedWorkflowTemplateGroup",
    "WorkflowCanvasMutationResult",
    "WorkflowNodePosition",
    "apply_node_group_template_to_workflow",
    "archive_user_canvas_template",
    "bind_workflow_node_image",
    "bind_v2_reference_node_asset",
    "create_user_canvas_template_from_workflow_nodes",
    "cancel_product_workflow_run",
    "create_workflow_edge",
    "create_workflow_folder",
    "create_workflow_node",
    "delete_workflow_edge",
    "delete_workflow_node",
    "dissolve_workflow_folder",
    "duplicate_workflow_node_group",
    "execute_product_workflow_run",
    "execute_product_workflow_node_run",
    "get_or_create_product_workflow",
    "get_product_workflow_status",
    "get_v2_workflow_node_run",
    "latest_workflow_runs",
    "list_canvas_templates",
    "mark_workflow_run_enqueue_failed",
    "materialize_node_group_template_to_workflow",
    "normalize_workflow_node_config",
    "retry_product_workflow_run",
    "rename_workflow_folder",
    "rename_user_canvas_template",
    "run_product_workflow",
    "start_product_workflow_run",
    "submit_product_workflow_run",
    "submit_v2_workflow_node_run",
    "set_workflow_folder_members",
    "translate_workflow_folder",
    "update_workflow_copy_set",
    "update_workflow_node",
    "update_workflow_node_layout",
    "upload_workflow_node_image",
]
