"""有界 Agent 合同，以及商品/全局上下文投影。"""

from __future__ import annotations

import json
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent.conversations import (
    get_agent_conversation_by_id_or_raise,
)
from productflow_backend.application.agent.global_draft_contracts import global_agent_draft_schema
from productflow_backend.application.agent.sessions import get_agent_session_or_raise
from productflow_backend.application.agent.tasks import task_contract
from productflow_backend.application.media_library.drafts import validate_library_organization_draft
from productflow_backend.application.product_intake import (
    agent_product_image_type_catalog_json,
    workflow_intake_payload,
)
from productflow_backend.application.product_workflow.graph_commands import get_active_workflow_graph
from productflow_backend.application.product_workflow.graph_queries import project_workflow_graph
from productflow_backend.domain.enums import AgentConversationScope, MediaVerificationStatus
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.domain.graph_catalog import graph_catalog_json
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    Product,
    ProductImageAsset,
)

AGENT_TOOL_CONTRACT_VERSION = 15
AGENT_CONTEXT_MAX_BYTES = 512 * 1024

WORKFLOW_AGENT_LIVE_GRAPH_PROMPT = """你是 ProductFlow 的商品工作流协作 Agent。
当前商品已有 live schema-v3 图。用户是画布的主编辑者。

加载匹配任务的 ProductFlow Skill。只使用本轮工具列表里的工具。
不得提交第二份完整拓扑。不得编造商品事实或资产。
不得输出 base64、data URL、存储路径或内部 URL。
提案、跑图和素材整理的确认只在 ProductFlow UI 完成。
"""

GOAL_LOOP_PROMPT = """
当前是用户显式开始的 Goal，不是开聊入场券。
循环使用已有工具：request_workflow_run → 等用户在画布确认 → inspect 结果 → apply 或 propose → 再请求跑图。
不得自行宣布 Goal 完成。完成只能由用户点完成。
一次 WorkflowGraphRun 结束不等于 Goal 结束，不要接管跑图状态机。
默认仍须用户在画布确认跑图。
"""

GLOBAL_AGENT_SYSTEM_PROMPT = """你是 ProductFlow 的全局素材与工作流辅助 Agent。
作用域是整个应用，不绑定某一个商品画布。

加载匹配任务的 ProductFlow Skill。只使用本轮工具列表里的工具。
不能在全局会话上改某个商品的 live graph。
素材整理必须先提交可审阅 Draft。不得编造事实。
不得输出 base64、data URL、存储路径或内部 URL。
"""


def get_agent_contract(session: Session, conversation_id: str) -> dict[str, Any]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    return _agent_contract_for_conversation(session, conversation)


def get_agent_task_contract(session: Session, task_id: str) -> dict[str, Any]:
    task, conversation = task_contract(session, task_id)
    contract = _agent_contract_for_conversation(session, conversation)
    contract["task_id"] = task.id
    contract["task_goal"] = task.goal
    contract["harness_run_id"] = task.harness_run_id
    if conversation.scope_type == AgentConversationScope.PRODUCT_WORKFLOW:
        contract["system_prompt"] = f"{contract['system_prompt'].rstrip()}\n{GOAL_LOOP_PROMPT.strip()}\n"
    return contract


def get_agent_runtime_context(
    session: Session,
    *,
    conversation_id: str,
    task_id: str | None = None,
) -> dict[str, Any]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    if conversation.session_id is None:
        raise ConflictError("Agent conversation 尚未绑定 Session")
    agent_session = get_agent_session_or_raise(session, conversation.session_id)

    task_summary: str | None = None
    if task_id is not None:
        task, task_conversation = task_contract(session, task_id)
        if task_conversation.id != conversation.id or task.session_id != agent_session.id:
            raise ConflictError("Agent Task 与当前 Agent conversation 不匹配")
        task_summary = task.summary

    payload = {
        "schema_version": 1,
        "session_id": agent_session.id,
        "conversation_id": conversation.id,
        "task_id": task_id,
        "session_summary": agent_session.summary,
        "task_summary": task_summary,
    }
    encoded = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()
    if len(encoded) > 16 * 1024:
        raise ConflictError("Agent 运行时摘要超过上下文上限")
    return payload


def _agent_contract_for_conversation(session: Session, conversation: AgentConversation) -> dict[str, Any]:
    if conversation.scope_type == AgentConversationScope.GLOBAL:
        organization_draft = conversation.library_organization_draft
        current_revision = organization_draft.current_revision if organization_draft is not None else None
        return {
            "schema_version": 1,
            "scope_type": AgentConversationScope.GLOBAL,
            "conversation_id": conversation.id,
            "task_id": None,
            "task_goal": None,
            "product_id": None,
            "harness_run_id": conversation.harness_run_id,
            "current_draft_version": current_revision.version if current_revision is not None else 0,
            "system_prompt": GLOBAL_AGENT_SYSTEM_PROMPT,
            "draft_kind": "global",
            "draft_schema": global_agent_draft_schema(),
            "tool_contract_version": AGENT_TOOL_CONTRACT_VERSION,
            "has_live_graph": False,
        }
    if conversation.product_id is None:
        raise ConflictError("商品工作流 Agent conversation 缺少商品")
    live_graph = get_active_workflow_graph(session, product_id=conversation.product_id)
    return {
        "schema_version": 1,
        "scope_type": AgentConversationScope.PRODUCT_WORKFLOW,
        "conversation_id": conversation.id,
        "task_id": None,
        "task_goal": None,
        "product_id": conversation.product_id,
        "harness_run_id": conversation.harness_run_id,
        "current_draft_version": 0,
        "system_prompt": WORKFLOW_AGENT_LIVE_GRAPH_PROMPT,
        "draft_kind": "workflow",
        "draft_schema": {},
        "tool_contract_version": AGENT_TOOL_CONTRACT_VERSION,
        "has_live_graph": live_graph is not None,
    }


def validate_agent_library_organization_draft(
    session: Session,
    *,
    conversation_id: str,
    value: dict[str, Any],
):
    return validate_library_organization_draft(
        session,
        conversation_id=conversation_id,
        value=value,
    )


def validate_agent_global_draft(
    session: Session,
    *,
    conversation_id: str,
    value: dict[str, Any],
):
    from productflow_backend.application.agent.global_drafts import validate_global_agent_draft

    return validate_global_agent_draft(
        session,
        conversation_id=conversation_id,
        value=value,
    )


def get_agent_product_context(session: Session, conversation_id: str) -> dict[str, Any]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    product = session.scalar(
        select(Product)
        .options(selectinload(Product.current_fact_set_version))
        .where(Product.id == conversation.product_id)
    )
    if product is None:
        raise NotFoundError("商品不存在")
    intake = _load_product_intake_context(session, product=product)
    payload: dict[str, Any] = {
        "schema_version": 1,
        "product": {
            "id": product.id,
            "name": product.name,
            "category": product.category,
            "price": str(product.price) if product.price is not None else None,
            "source_note": product.source_note,
        },
        "confirmed_fact_set": (
            {
                "version": product.current_fact_set_version.version,
                "facts": product.current_fact_set_version.payload_json,
            }
            if product.current_fact_set_version is not None
            else None
        ),
        "intake": intake,
        "node_catalog": graph_catalog_json(),
        "image_type_catalog": agent_product_image_type_catalog_json(),
        "live_graph": _live_graph_agent_summary(session, product_id=product.id),
    }
    encoded = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()
    if len(encoded) > AGENT_CONTEXT_MAX_BYTES:
        raise ConflictError("商品上下文超过 Agent 工具输出上限")
    return payload


def _live_graph_agent_summary(session: Session, *, product_id: str) -> dict[str, Any] | None:
    graph = get_active_workflow_graph(session, product_id=product_id)
    if graph is None:
        return None
    projection = project_workflow_graph(session, graph)
    return {
        "id": projection.id,
        "title": projection.title,
        "schema_version": projection.schema_version,
        "revision": projection.revision,
        "nodes": [
            {
                "id": node.id,
                "node_type": node.node_type.value,
                "title": node.title,
                "config_status": node.config_status.value,
                "unused": node.unused,
                "bound_asset_id": node.bound_asset_id,
                "group_id": node.group_id,
                "has_current_artifact": node.current_artifact_id is not None,
                "incoming_roles": [edge.role.value for edge in node.incoming],
                "outgoing_roles": [edge.role.value for edge in node.outgoing],
            }
            for node in projection.nodes
        ],
        "edges": [
            {
                "id": edge.id,
                "source_node_id": edge.source_node_id,
                "target_node_id": edge.target_node_id,
                "role": edge.role.value,
                "data_type": edge.data_type.value,
            }
            for edge in projection.edges
        ],
        "groups": [
            {
                "id": group.id,
                "title": group.title,
                "member_ids": list(group.member_ids),
            }
            for group in projection.groups
        ],
    }


def get_agent_global_workflow_target(
    session: Session,
    *,
    product_id: str,
) -> AgentConversation:
    """按显式 product 解析其最新 product conversation。"""
    product = session.get(Product, product_id)
    if product is None:
        raise NotFoundError("商品不存在")
    statement = (
        select(AgentConversation)
        .where(
            AgentConversation.scope_type == AgentConversationScope.PRODUCT_WORKFLOW,
            AgentConversation.product_id == product_id,
        )
        .order_by(AgentConversation.updated_at.desc(), AgentConversation.id.desc())
    )
    conversation = session.scalar(statement.limit(1))
    if conversation is None:
        raise NotFoundError("商品没有 Agent 工作区")
    return conversation


def get_agent_global_workflow_context(
    session: Session,
    *,
    conversation_id: str,
    product_id: str,
) -> dict[str, Any]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_global_conversation(conversation)
    target = get_agent_global_workflow_target(session, product_id=product_id)
    context = get_agent_product_context(session, target.id)
    context["target"] = {
        "product_id": product_id,
        "product_conversation_id": target.id,
    }
    encoded = json.dumps(context, ensure_ascii=False, separators=(",", ":")).encode()
    if len(encoded) > AGENT_CONTEXT_MAX_BYTES:
        raise ConflictError("全局目标商品上下文超过 Agent 工具输出上限")
    return context


def _load_product_intake_context(
    session: Session,
    *,
    product: Product,
) -> dict[str, Any] | None:
    from productflow_backend.application.product_intake import parse_product_intake

    intake = parse_product_intake(product)
    if intake is None:
        return None
    asset_ids = list(intake.reference_asset_ids)
    assets = list(
        session.scalars(
            select(ProductImageAsset)
            .options(selectinload(ProductImageAsset.media_object))
            .where(ProductImageAsset.id.in_(asset_ids))
        ).all()
    )
    assets_by_id = {asset.id: asset for asset in assets}
    if set(assets_by_id) != set(asset_ids):
        raise ConflictError("商品 intake 引用了不存在的商品图片资产")
    if any(asset.product_id != product.id for asset in assets):
        raise ConflictError("商品 intake 引用了其他商品的图片资产")
    if any(asset.media_object.verification_status != MediaVerificationStatus.VERIFIED for asset in assets):
        raise ConflictError("商品 intake 引用了未通过核验的图片资产")
    return workflow_intake_payload(intake)


def _require_product_conversation(conversation: AgentConversation) -> None:
    if (
        conversation.scope_type != AgentConversationScope.PRODUCT_WORKFLOW
        or conversation.product_id is None
    ):
        raise ConflictError("当前 Agent conversation 不是商品工作流作用域")


def _require_global_conversation(conversation: AgentConversation) -> None:
    if conversation.scope_type != AgentConversationScope.GLOBAL:
        raise ConflictError("当前 Agent conversation 不是全局作用域")


__all__ = [
    "AGENT_CONTEXT_MAX_BYTES",
    "AGENT_TOOL_CONTRACT_VERSION",
    "GLOBAL_AGENT_SYSTEM_PROMPT",
    "GOAL_LOOP_PROMPT",
    "WORKFLOW_AGENT_LIVE_GRAPH_PROMPT",
    "get_agent_contract",
    "get_agent_global_workflow_context",
    "get_agent_global_workflow_target",
    "get_agent_product_context",
    "get_agent_runtime_context",
    "get_agent_task_contract",
    "validate_agent_global_draft",
    "validate_agent_library_organization_draft",
]
