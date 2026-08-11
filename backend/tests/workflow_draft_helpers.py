from __future__ import annotations

from copy import deepcopy
from typing import Any


def make_workflow_draft_payload(*, reference_asset_id: str = "00000000-0000-0000-0000-000000000001") -> dict[str, Any]:
    return {
        "schema_version": 1,
        "title": "工业刀具收纳图片工作流",
        "facts": [
            {
                "key": "product_name",
                "value": "硬质刀具收纳套装",
                "source_type": "user",
                "status": "confirmed",
                "requires_confirmation": True,
                "evidence_asset_ids": [reference_asset_id],
                "conflicts": [],
            }
        ],
        "required_fact_keys": ["product_name"],
        "missing_fact_keys": [],
        "reference_bindings": [
            {
                "key": "product-reference",
                "asset_id": reference_asset_id,
                "role": "product_identity",
                "label": "商品参考图",
            }
        ],
        "visual_system": {
            "mode": "draft",
            "payload": {
                "style": "professional_industrial",
                "primary_color": "#FF6B00",
                "secondary_color": "#0066FF",
            },
            "source_markdown": "专业工业风格、现代极简主义。",
        },
        "prompt_plans": [
            {
                "key": "hero-prompt",
                "image_type_key": "hero",
                "title": "首屏海报提示词",
                "payload": {
                    "design_goal": "展示完整产品构成与收纳秩序",
                    "text": {"headline": "硬质刀具 分类收纳"},
                },
            }
        ],
        "image_types": [
            {
                "key": "hero",
                "title": "首屏海报图",
                "order": 0,
                "quantity": 2,
                "prompt_plan_key": "hero-prompt",
                "images": [
                    {
                        "key": "hero-1",
                        "order": 0,
                        "variation_instruction": "俯视 45 度，V 字形排布",
                        "generation_spec": {
                            "aspect_ratio": "1:1",
                            "resolution_tier": "high",
                            "quality_intent": "high",
                            "reference_fidelity": "high",
                            "background_intent": "opaque",
                            "text_policy": "required",
                            "text_language": "zh-CN",
                        },
                        "delivery_spec": {
                            "width": 1600,
                            "height": 1600,
                            "format": "png",
                            "fit": "contain",
                            "background_color": "#F2F2F2",
                        },
                    },
                    {
                        "key": "hero-2",
                        "order": 1,
                        "variation_instruction": "保持商品结构，调整光位和留白",
                        "generation_spec": {
                            "aspect_ratio": "1:1",
                            "resolution_tier": "high",
                            "quality_intent": "high",
                            "reference_fidelity": "high",
                            "background_intent": "opaque",
                            "text_policy": "required",
                            "text_language": "zh-CN",
                        },
                    },
                ],
            }
        ],
        "folders": [
            {
                "key": "hero-folder",
                "title": "首屏海报图",
                "order": 0,
                "position_x": 360,
                "position_y": 40,
                "width": 920,
                "height": 620,
            }
        ],
        "nodes": [
            {
                "key": "product-context",
                "node_type": "product_context",
                "title": "商品事实",
                "position_x": 40,
                "position_y": 180,
            },
            {
                "key": "product-reference-node",
                "node_type": "reference_image",
                "title": "商品参考图",
                "position_x": 400,
                "position_y": 100,
                "folder_key": "hero-folder",
                "reference_key": "product-reference",
            },
            {
                "key": "hero-prompt-node",
                "node_type": "prompt_generation",
                "title": "首屏海报提示词",
                "position_x": 680,
                "position_y": 100,
                "folder_key": "hero-folder",
                "prompt_plan_key": "hero-prompt",
            },
            {
                "key": "hero-image-1-node",
                "node_type": "image_generation",
                "title": "首屏海报图 1",
                "position_x": 980,
                "position_y": 40,
                "folder_key": "hero-folder",
                "image_plan_key": "hero-1",
            },
            {
                "key": "hero-image-2-node",
                "node_type": "image_generation",
                "title": "首屏海报图 2",
                "position_x": 980,
                "position_y": 340,
                "folder_key": "hero-folder",
                "image_plan_key": "hero-2",
            },
        ],
        "edges": [
            {
                "key": "context-to-prompt",
                "source_node_key": "product-context",
                "target_node_key": "hero-prompt-node",
                "source_handle": "facts",
                "target_handle": "facts",
            },
            {
                "key": "reference-to-prompt",
                "source_node_key": "product-reference-node",
                "target_node_key": "hero-prompt-node",
                "source_handle": "asset",
                "target_handle": "reference",
            },
            {
                "key": "prompt-to-image-1",
                "source_node_key": "hero-prompt-node",
                "target_node_key": "hero-image-1-node",
                "source_handle": "prompt",
                "target_handle": "prompt",
            },
            {
                "key": "prompt-to-image-2",
                "source_node_key": "hero-prompt-node",
                "target_node_key": "hero-image-2-node",
                "source_handle": "prompt",
                "target_handle": "prompt",
            },
        ],
        "confirmation_summary": "生成 2 张首屏海报图，使用中文图片文字和统一工业视觉体系。",
    }


def clone_workflow_draft_payload(payload: dict[str, Any]) -> dict[str, Any]:
    return deepcopy(payload)
