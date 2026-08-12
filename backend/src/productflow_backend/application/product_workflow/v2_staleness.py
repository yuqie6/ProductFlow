from __future__ import annotations

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.domain.enums import WorkflowNodeType
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import WorkflowNode


def get_image_prompt_node(session: Session, *, image_node: WorkflowNode) -> WorkflowNode:
    prompt_plan_key = image_node.config_json.get("prompt_plan_key")
    if not isinstance(prompt_plan_key, str) or not prompt_plan_key:
        raise ConflictError("图片节点缺少 prompt plan key")
    prompt_nodes = list(
        session.scalars(
            select(WorkflowNode).where(
                WorkflowNode.workflow_id == image_node.workflow_id,
                WorkflowNode.node_type == WorkflowNodeType.PROMPT_GENERATION,
            )
        )
    )
    matching = [
        node
        for node in prompt_nodes
        if node.config_json.get("prompt_plan_key") == prompt_plan_key
    ]
    if len(matching) != 1:
        raise ConflictError("图片节点必须且只能解析到一个提示词节点")
    return matching[0]


def ensure_image_prompt_references_current(
    session: Session,
    *,
    image_node: WorkflowNode,
) -> WorkflowNode:
    prompt_node = get_image_prompt_node(session, image_node=image_node)
    output = prompt_node.output_json if isinstance(prompt_node.output_json, dict) else {}
    if output.get("references_stale") is True:
        raise ConflictError("图片节点上游参考图已变化，请先重新生成提示词")
    return prompt_node


__all__ = ["ensure_image_prompt_references_current", "get_image_prompt_node"]
