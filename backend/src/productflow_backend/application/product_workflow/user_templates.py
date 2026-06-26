from __future__ import annotations

from typing import Any

from pydantic import BaseModel, ConfigDict, Field, field_validator
from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.canvas_templates import (
    CanvasTemplate,
    CanvasTemplateEdgeSpec,
    CanvasTemplateNodeSpec,
    CanvasTemplateScenario,
    CanvasTemplateScenarioMetadata,
)
from productflow_backend.application.copy_payloads import normalize_copy_node_config
from productflow_backend.application.image_generation_core import normalize_image_generation_tool_options
from productflow_backend.application.product_workflow import graph as product_workflow_graph
from productflow_backend.application.product_workflow.context import image_size_from_config
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import WorkflowNodeType
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import UserCanvasTemplate, WorkflowEdge, WorkflowNode, new_id

USER_TEMPLATE_KEY_PREFIX = "user:"
USER_TEMPLATE_SCHEMA_VERSION = 1
USER_TEMPLATE_SCENARIO = CanvasTemplateScenario.MAIN_IMAGE

ARTIFACT_SPECIFIC_CONFIG_KEYS = frozenset(
    {
        "copy_set_id",
        "creative_brief_id",
        "download_url",
        "filled_reference_node_ids",
        "filled_source_asset_ids",
        "generated_poster_variant_ids",
        "node_id",
        "poster_variant_id",
        "poster_variant_ids",
        "preview_url",
        "product_id",
        "source_asset_id",
        "source_asset_ids",
        "source_poster_variant_id",
        "storage_path",
        "thumbnail_url",
        "workflow_id",
    }
)
SYSTEM_TEMPLATE_CONFIG_KEYS = frozenset({"_canvas_template"})

ARTIFACT_SPECIFIC_KEY_SUFFIXES = ("_id", "_ids", "_url", "_path")


class UserCanvasTemplateNodePayload(BaseModel):
    model_config = ConfigDict(frozen=True)

    key: str
    node_type: WorkflowNodeType
    title: str
    position_x: int = 0
    position_y: int = 0
    config_json: dict[str, Any] = Field(default_factory=dict)

    @field_validator("config_json")
    @classmethod
    def validate_config_json(cls, value: dict[str, Any]) -> dict[str, Any]:
        return dict(value)


class UserCanvasTemplateEdgePayload(BaseModel):
    model_config = ConfigDict(frozen=True)

    source_node_key: str
    target_node_key: str
    source_handle: str | None = "output"
    target_handle: str | None = "input"


class UserCanvasTemplatePayload(BaseModel):
    model_config = ConfigDict(frozen=True)

    version: int = USER_TEMPLATE_SCHEMA_VERSION
    kind: str = "node_group"
    nodes: tuple[UserCanvasTemplateNodePayload, ...]
    edges: tuple[UserCanvasTemplateEdgePayload, ...] = ()


def user_canvas_template_to_canvas_template(row: UserCanvasTemplate) -> CanvasTemplate:
    payload = _parse_template_payload(row)
    return CanvasTemplate(
        key=row.key,
        version=payload.version,
        kind="node_group",
        title=row.title,
        description=row.description or "",
        source="user",
        user_template_id=row.id,
        scenario=CanvasTemplateScenarioMetadata(
            scenario=USER_TEMPLATE_SCENARIO,
            title="用户模板",
            description="用户保存的节点组",
            ecommerce_stage="自定义",
            tags=("用户模板",),
        ),
        nodes=tuple(
            CanvasTemplateNodeSpec(
                key=node.key,
                node_type=node.node_type,
                title=node.title,
                position_x=node.position_x,
                position_y=node.position_y,
                config_json=node.config_json,
                size=_node_size(node.node_type, node.config_json),
            )
            for node in payload.nodes
        ),
        edges=tuple(
            CanvasTemplateEdgeSpec(
                source_node_key=edge.source_node_key,
                target_node_key=edge.target_node_key,
                source_handle=edge.source_handle,
                target_handle=edge.target_handle,
            )
            for edge in payload.edges
        ),
    )


def list_canvas_templates(session: Session) -> list[CanvasTemplate]:
    from productflow_backend.application.canvas_templates import list_builtin_canvas_templates

    user_templates = session.scalars(
        select(UserCanvasTemplate)
        .where(UserCanvasTemplate.archived_at.is_(None))
        .order_by(UserCanvasTemplate.created_at.desc(), UserCanvasTemplate.id.desc())
    ).all()
    return [*list_builtin_canvas_templates(), *(user_canvas_template_to_canvas_template(row) for row in user_templates)]


def get_canvas_template(session: Session, template_key: str) -> CanvasTemplate:
    from productflow_backend.application.canvas_templates import get_builtin_canvas_template

    key = template_key.strip()
    if key.startswith(USER_TEMPLATE_KEY_PREFIX):
        row = _get_active_user_template_by_key(session, key)
        return user_canvas_template_to_canvas_template(row)
    return get_builtin_canvas_template(key)


def create_user_canvas_template_from_workflow_nodes(
    session: Session,
    *,
    product_id: str,
    title: str,
    description: str | None,
    node_ids: list[str],
) -> UserCanvasTemplate:
    clean_title = title.strip()
    if not clean_title:
        raise BusinessValidationError("模板名称不能为空")
    if not node_ids:
        raise BusinessValidationError("请选择要保存的节点")
    if len(set(node_ids)) != len(node_ids):
        raise BusinessValidationError("保存模板的节点不能重复")

    workflow = product_workflow_graph.get_active_workflow(session, product_id)
    if workflow is None:
        product_workflow_graph.get_product_or_raise(session, product_id)
        raise BusinessValidationError("需要先创建或打开画布后才能保存模板")

    workflow_nodes_by_id = {node.id: node for node in workflow.nodes}
    unknown_node_ids = [node_id for node_id in node_ids if node_id not in workflow_nodes_by_id]
    if unknown_node_ids:
        raise BusinessValidationError("保存模板包含不属于当前画布的节点")

    selected_nodes = [workflow_nodes_by_id[node_id] for node_id in node_ids]
    if any(node.node_type == WorkflowNodeType.PRODUCT_CONTEXT for node in selected_nodes):
        raise BusinessValidationError("节点组模板不能包含商品资料节点")

    min_x = min(node.position_x for node in selected_nodes)
    min_y = min(node.position_y for node in selected_nodes)
    node_keys_by_id = {node.id: f"node_{index + 1}" for index, node in enumerate(selected_nodes)}
    payload = UserCanvasTemplatePayload(
        nodes=tuple(
            UserCanvasTemplateNodePayload(
                key=node_keys_by_id[node.id],
                node_type=node.node_type,
                title=node.title,
                position_x=node.position_x - min_x,
                position_y=node.position_y - min_y,
                config_json=extract_reusable_node_config(node),
            )
            for node in selected_nodes
        ),
        edges=tuple(_selected_internal_edges(workflow.edges, node_keys_by_id)),
    )

    template = UserCanvasTemplate(
        id=new_id(),
        title=clean_title,
        description=(description or "").strip() or None,
        kind="node_group",
        schema_version=USER_TEMPLATE_SCHEMA_VERSION,
        template_json=payload.model_dump(mode="json"),
    )
    template.key = f"{USER_TEMPLATE_KEY_PREFIX}{template.id}"
    session.add(template)
    session.flush()

    user_canvas_template_to_canvas_template(template)
    session.commit()
    session.expire_all()
    return _get_user_template_or_raise(session, template.id)


def rename_user_canvas_template(
    session: Session,
    *,
    template_id: str,
    title: str | None,
    description: str | None,
) -> UserCanvasTemplate:
    template = _get_user_template_or_raise(session, template_id)
    if template.archived_at is not None:
        raise NotFoundError("用户模板不存在")
    if title is not None:
        clean_title = title.strip()
        if not clean_title:
            raise BusinessValidationError("模板名称不能为空")
        template.title = clean_title
    if description is not None:
        template.description = description.strip() or None
    template.updated_at = now_utc()
    session.commit()
    session.expire_all()
    return _get_user_template_or_raise(session, template_id)


def archive_user_canvas_template(session: Session, *, template_id: str) -> None:
    template = _get_user_template_or_raise(session, template_id)
    if template.archived_at is None:
        template.archived_at = now_utc()
        template.updated_at = template.archived_at
        session.commit()


def _parse_template_payload(row: UserCanvasTemplate) -> UserCanvasTemplatePayload:
    if row.kind != "node_group" or row.schema_version != USER_TEMPLATE_SCHEMA_VERSION:
        raise BusinessValidationError("用户模板版本不支持")
    payload = UserCanvasTemplatePayload.model_validate(row.template_json)
    if payload.version != USER_TEMPLATE_SCHEMA_VERSION or payload.kind != "node_group":
        raise BusinessValidationError("用户模板版本不支持")
    return payload


def _get_user_template_or_raise(session: Session, template_id: str) -> UserCanvasTemplate:
    template = session.get(UserCanvasTemplate, template_id)
    if template is None:
        raise NotFoundError("用户模板不存在")
    return template


def _get_active_user_template_by_key(session: Session, key: str) -> UserCanvasTemplate:
    template = session.scalar(
        select(UserCanvasTemplate).where(
            UserCanvasTemplate.key == key,
            UserCanvasTemplate.archived_at.is_(None),
        )
    )
    if template is None:
        raise BusinessValidationError("画布模板不存在")
    return template


def _node_size(node_type: WorkflowNodeType, config_json: dict[str, Any]) -> str | None:
    if node_type != WorkflowNodeType.IMAGE_GENERATION:
        return None
    raw_size = config_json.get("size")
    return raw_size if isinstance(raw_size, str) and raw_size else None


def _selected_internal_edges(
    edges: list[WorkflowEdge],
    node_keys_by_id: dict[str, str],
) -> list[UserCanvasTemplateEdgePayload]:
    template_edges: list[UserCanvasTemplateEdgePayload] = []
    for edge in edges:
        source_key = node_keys_by_id.get(edge.source_node_id)
        target_key = node_keys_by_id.get(edge.target_node_id)
        if source_key is None or target_key is None:
            continue
        template_edges.append(
            UserCanvasTemplateEdgePayload(
                source_node_key=source_key,
                target_node_key=target_key,
                source_handle=edge.source_handle,
                target_handle=edge.target_handle,
            )
        )
    return template_edges


def extract_reusable_node_config(node: WorkflowNode) -> dict[str, Any]:
    reusable_config = _sanitize_reusable_config(node.config_json or {})
    return _normalize_template_node_config(node.node_type, reusable_config)


def _normalize_template_node_config(node_type: WorkflowNodeType, config_json: dict[str, Any]) -> dict[str, Any]:
    config = dict(config_json)
    if node_type == WorkflowNodeType.IMAGE_GENERATION:
        try:
            normalized_size = image_size_from_config(config)
        except ValueError as exc:
            raise BusinessValidationError(str(exc)) from exc
        if normalized_size is not None:
            config["size"] = normalized_size
        if "tool_options" in config:
            raw_tool_options = config.get("tool_options")
            config["tool_options"] = normalize_image_generation_tool_options(
                raw_tool_options if isinstance(raw_tool_options, dict) else None
            )
    if node_type == WorkflowNodeType.COPY_GENERATION:
        try:
            config = normalize_copy_node_config(config).model_dump(mode="json")
        except ValueError as exc:
            raise BusinessValidationError(str(exc)) from exc
    return config


def _sanitize_reusable_config(value: Any, *, path: tuple[str, ...] = ()) -> Any:
    if isinstance(value, dict):
        sanitized: dict[str, Any] = {}
        for key, nested_value in value.items():
            if not isinstance(key, str):
                raise BusinessValidationError("模板配置包含不可复用的产物数据")
            if key in SYSTEM_TEMPLATE_CONFIG_KEYS:
                continue
            normalized_key = key.lower()
            if normalized_key in ARTIFACT_SPECIFIC_CONFIG_KEYS:
                continue
            if normalized_key.endswith(ARTIFACT_SPECIFIC_KEY_SUFFIXES):
                raise BusinessValidationError("模板配置包含不可复用的产物数据")
            sanitized[key] = _sanitize_reusable_config(nested_value, path=(*path, key))
        return sanitized
    if isinstance(value, list):
        return [_sanitize_reusable_config(item, path=path) for item in value]
    return value
