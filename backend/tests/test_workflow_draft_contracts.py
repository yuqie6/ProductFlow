from __future__ import annotations

from copy import deepcopy

import pytest
from pydantic import ValidationError
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.workflow_drafts.contracts import (
    WORKFLOW_DRAFT_MAX_TOTAL_IMAGES,
    WorkflowDraftPayloadV1,
    workflow_draft_payload_hash,
)


def test_workflow_draft_payload_accepts_one_prompt_per_type_and_one_node_per_image() -> None:
    payload = WorkflowDraftPayloadV1.model_validate(make_workflow_draft_payload())

    assert payload.schema_version == 1
    assert sum(image_type.quantity for image_type in payload.image_types) == 2
    assert payload.referenced_asset_ids() == {"00000000-0000-0000-0000-000000000001"}


def test_workflow_draft_payload_rejects_unknown_fields() -> None:
    payload = make_workflow_draft_payload()
    payload["agent_commentary"] = "不属于 artifact 合同"

    with pytest.raises(ValidationError, match="extra_forbidden"):
        WorkflowDraftPayloadV1.model_validate(payload)


def test_workflow_draft_payload_rejects_quantity_that_does_not_match_image_plans() -> None:
    payload = make_workflow_draft_payload()
    payload["image_types"][0]["quantity"] = 1

    with pytest.raises(ValidationError, match="quantity 必须等于逐图计划数量"):
        WorkflowDraftPayloadV1.model_validate(payload)


def test_workflow_draft_payload_rejects_more_than_thirty_planned_images() -> None:
    payload = make_workflow_draft_payload()
    base_type = payload["image_types"][0]
    base_prompt = payload["prompt_plans"][0]
    base_prompt_node = next(node for node in payload["nodes"] if node["node_type"] == "prompt_generation")
    base_image_nodes = [node for node in payload["nodes"] if node["node_type"] == "image_generation"]
    base_prompt_edges = [edge for edge in payload["edges"] if edge["source_node_key"] == "hero-prompt-node"]

    payload["image_types"] = []
    payload["prompt_plans"] = []
    payload["nodes"] = [
        node
        for node in payload["nodes"]
        if node["node_type"] not in {"prompt_generation", "image_generation"}
    ]
    payload["edges"] = [edge for edge in payload["edges"] if edge not in base_prompt_edges]
    for type_index in range(6):
        image_type = deepcopy(base_type)
        image_type["key"] = f"type-{type_index}"
        image_type["order"] = type_index
        image_type["quantity"] = 6
        image_type["prompt_plan_key"] = f"prompt-{type_index}"
        image_type["images"] = []
        prompt = deepcopy(base_prompt)
        prompt["key"] = f"prompt-{type_index}"
        prompt["image_type_key"] = image_type["key"]
        prompt_node = deepcopy(base_prompt_node)
        prompt_node["key"] = f"prompt-node-{type_index}"
        prompt_node["prompt_plan_key"] = prompt["key"]
        payload["prompt_plans"].append(prompt)
        payload["nodes"].append(prompt_node)
        for image_index in range(6):
            image = deepcopy(base_type["images"][image_index % 2])
            image["key"] = f"image-{type_index}-{image_index}"
            image["order"] = image_index
            image_type["images"].append(image)
            image_node = deepcopy(base_image_nodes[image_index % 2])
            image_node["key"] = f"image-node-{type_index}-{image_index}"
            image_node["image_plan_key"] = image["key"]
            payload["nodes"].append(image_node)
            payload["edges"].append(
                {
                    "key": f"prompt-image-edge-{type_index}-{image_index}",
                    "source_node_key": prompt_node["key"],
                    "target_node_key": image_node["key"],
                }
            )
        payload["image_types"].append(image_type)

    assert sum(item["quantity"] for item in payload["image_types"]) > WORKFLOW_DRAFT_MAX_TOTAL_IMAGES
    with pytest.raises(ValidationError, match="计划图片总数不能超过"):
        WorkflowDraftPayloadV1.model_validate(payload)


def test_workflow_draft_payload_rejects_duplicate_business_keys() -> None:
    payload = make_workflow_draft_payload()
    payload["edges"][1]["key"] = payload["edges"][0]["key"]

    with pytest.raises(ValidationError, match="工作流连线 key 不能重复"):
        WorkflowDraftPayloadV1.model_validate(payload)


def test_workflow_draft_payload_requires_prompt_edge_for_every_image_node() -> None:
    payload = make_workflow_draft_payload()
    payload["edges"] = [edge for edge in payload["edges"] if edge["key"] != "prompt-to-image-2"]

    with pytest.raises(ValidationError, match="必须连接对应图片类型"):
        WorkflowDraftPayloadV1.model_validate(payload)


def test_workflow_draft_payload_allows_only_one_prompt_plan_per_image_type() -> None:
    payload = make_workflow_draft_payload()
    duplicate_prompt = deepcopy(payload["prompt_plans"][0])
    duplicate_prompt["key"] = "hero-prompt-duplicate"
    payload["prompt_plans"].append(duplicate_prompt)
    duplicate_node = deepcopy(next(node for node in payload["nodes"] if node["node_type"] == "prompt_generation"))
    duplicate_node["key"] = "hero-prompt-node-duplicate"
    duplicate_node["prompt_plan_key"] = duplicate_prompt["key"]
    payload["nodes"].append(duplicate_node)
    payload["edges"].append(
        {
            "key": "context-to-duplicate-prompt",
            "source_node_key": "product-context",
            "target_node_key": duplicate_node["key"],
        }
    )

    with pytest.raises(ValidationError, match="同一图片类型不能关联多个提示词计划"):
        WorkflowDraftPayloadV1.model_validate(payload)


def test_workflow_draft_payload_requires_product_context_for_every_prompt_node() -> None:
    payload = make_workflow_draft_payload()
    payload["edges"] = [edge for edge in payload["edges"] if edge["key"] != "context-to-prompt"]

    with pytest.raises(ValidationError, match="必须连接 product_context"):
        WorkflowDraftPayloadV1.model_validate(payload)


def test_workflow_draft_payload_rejects_unsupported_typed_edges() -> None:
    payload = make_workflow_draft_payload()
    payload["edges"].append(
        {
            "key": "image-to-reference",
            "source_node_key": "hero-image-1-node",
            "target_node_key": "product-reference-node",
        }
    )

    with pytest.raises(ValidationError, match="不支持的 v2 节点类型组合"):
        WorkflowDraftPayloadV1.model_validate(payload)


def test_workflow_draft_payload_rejects_cycles() -> None:
    payload = make_workflow_draft_payload()
    payload["edges"].extend(
        [
            {
                "key": "image-1-to-image-2",
                "source_node_key": "hero-image-1-node",
                "target_node_key": "hero-image-2-node",
            },
            {
                "key": "image-2-to-image-1",
                "source_node_key": "hero-image-2-node",
                "target_node_key": "hero-image-1-node",
            },
        ]
    )

    with pytest.raises(ValidationError, match="循环依赖"):
        WorkflowDraftPayloadV1.model_validate(payload)


def test_workflow_draft_payload_hash_is_stable_across_mapping_order() -> None:
    payload = make_workflow_draft_payload()
    reordered = dict(reversed(list(payload.items())))

    assert workflow_draft_payload_hash(payload) == workflow_draft_payload_hash(reordered)
