from __future__ import annotations

from copy import deepcopy

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.canvas_templates import (
    CanvasTemplate,
    get_builtin_canvas_template,
    validate_canvas_template,
)
from productflow_backend.application.language_policy import (
    TemplateLanguageHints,
    language_hints_from_template_language,
)
from productflow_backend.domain.enums import WorkflowNodeType
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import Product, ProductWorkflow, WorkflowEdge, WorkflowNode

DEFAULT_PRODUCT_CREATION_CANVAS_TEMPLATE_KEYS = frozenset({"", "default", "basic", "blank", "minimal"})
TEMPLATE_METADATA_CONFIG_KEY = "_canvas_template"


def resolve_product_creation_canvas_template(canvas_template_key: str | None) -> CanvasTemplate | None:
    template_key = (canvas_template_key or "").strip()
    if template_key in DEFAULT_PRODUCT_CREATION_CANVAS_TEMPLATE_KEYS:
        return None

    template = get_builtin_canvas_template(template_key)
    if template.kind != "full_canvas":
        raise BusinessValidationError("商品创建只支持完整画布模板，节点组模板请在画布内添加")
    return template


def materialize_product_workflow_from_template(
    session: Session,
    *,
    product_id: str,
    template: CanvasTemplate,
    template_language: str | None = None,
) -> ProductWorkflow:
    validate_canvas_template(template)
    if template.kind != "full_canvas":
        raise BusinessValidationError("商品创建只支持完整画布模板，节点组模板请在画布内添加")

    product_exists = session.scalar(select(Product.id).where(Product.id == product_id))
    if product_exists is None:
        raise NotFoundError("商品不存在")

    active_workflow_id = session.scalar(
        select(ProductWorkflow.id).where(
            ProductWorkflow.product_id == product_id,
            ProductWorkflow.active.is_(True),
        )
    )
    if active_workflow_id is not None:
        raise BusinessValidationError("商品已有活动画布")

    workflow = ProductWorkflow(
        product_id=product_id,
        title=template.title,
        active=True,
    )
    session.add(workflow)
    session.flush()

    materialize_canvas_template_graph(
        session,
        workflow=workflow,
        template=template,
        template_language=template_language,
    )
    session.flush()
    return workflow


def materialize_canvas_template_graph(
    session: Session,
    *,
    workflow: ProductWorkflow,
    template: CanvasTemplate,
    position_x_offset: int = 0,
    position_y_offset: int = 0,
    existing_nodes_by_template_key: dict[str, WorkflowNode] | None = None,
    external_source_nodes_by_template_source: dict[str, WorkflowNode] | None = None,
    template_language: str | None = None,
) -> dict[str, WorkflowNode]:
    validate_canvas_template(template)
    language_hints = language_hints_from_template_language(template_language)
    nodes_by_template_key: dict[str, WorkflowNode] = dict(existing_nodes_by_template_key or {})
    for node_spec in template.nodes:
        if node_spec.key in nodes_by_template_key:
            continue
        config_json = deepcopy(node_spec.config_json)
        _apply_template_language_hints(config_json, node_type=node_spec.node_type, language_hints=language_hints)
        if template.source == "builtin":
            config_json[TEMPLATE_METADATA_CONFIG_KEY] = {
                "source": template.source,
                "template_key": template.key,
                "node_key": node_spec.key,
            }
        node = WorkflowNode(
            workflow_id=workflow.id,
            node_type=node_spec.node_type,
            title=node_spec.title,
            position_x=node_spec.position_x + position_x_offset,
            position_y=node_spec.position_y + position_y_offset,
            config_json=config_json,
        )
        session.add(node)
        nodes_by_template_key[node_spec.key] = node
    session.flush()

    for edge_spec in template.edges:
        session.add(
            WorkflowEdge(
                workflow_id=workflow.id,
                source_node_id=nodes_by_template_key[edge_spec.source_node_key].id,
                target_node_id=nodes_by_template_key[edge_spec.target_node_key].id,
                source_handle=edge_spec.source_handle,
                target_handle=edge_spec.target_handle,
            )
        )
    external_sources = external_source_nodes_by_template_source or {}
    for connection in template.default_external_connections:
        source_node = external_sources.get(connection.source)
        if source_node is None:
            raise BusinessValidationError("节点组模板需要当前画布中的商品资料节点")
        session.add(
            WorkflowEdge(
                workflow_id=workflow.id,
                source_node_id=source_node.id,
                target_node_id=nodes_by_template_key[connection.target_node_key].id,
                source_handle="output",
                target_handle="input",
            )
        )
    session.flush()
    return nodes_by_template_key


def _apply_template_language_hints(
    config_json: dict[str, object],
    *,
    node_type: WorkflowNodeType,
    language_hints: TemplateLanguageHints,
) -> None:
    if (
        node_type == WorkflowNodeType.COPY_GENERATION
        and language_hints.copy_language_hint
        and not config_json.get("copy_language_hint")
    ):
        config_json["copy_language_hint"] = language_hints.copy_language_hint
    if (
        node_type == WorkflowNodeType.IMAGE_GENERATION
        and language_hints.visible_text_language_hint
        and not config_json.get("visible_text_language_hint")
    ):
        config_json["visible_text_language_hint"] = language_hints.visible_text_language_hint
