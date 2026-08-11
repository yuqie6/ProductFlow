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
                "name": "工业极简视觉体系",
                "style": ["专业工业风格", "现代极简主义"],
                "colors": [
                    {"role": "primary", "value": "#FF6B00", "label": "工业警示橙"},
                    {"role": "secondary", "value": "#0066FF", "label": "专业工具蓝"},
                    {"role": "accent", "value": "#C0C0C0", "label": "硬质金属银"},
                    {"role": "background", "value": "#F2F2F2", "label": "工业浅灰"},
                ],
                "typography": {
                    "title_font": "思源黑体 Bold",
                    "body_font": "思源黑体 Regular",
                    "scale": {"headline": 3, "subtitle": 1.8, "body": 1},
                },
                "spacing": {
                    "min_edge_whitespace_percent": 30,
                    "principles": ["保持专业呼吸感"],
                },
                "decorations": {
                    "elements": ["细线网格", "技术参数标注线", "工业几何色块"],
                    "icon_style": "工程图感线性细线图标",
                },
                "photography": {
                    "lighting": "硬调侧逆光",
                    "depth_of_field": "中等景深",
                    "camera_parameters": ["f/11", "1/125s", "ISO 100", "85mm"],
                },
                "quality": {
                    "resolution": "4K/高清",
                    "commercial_grade": "专业产品摄影/商业广告级",
                    "realism": "超写实/照片级",
                    "minimum_quality": "high",
                },
                "product_fidelity": {
                    "preserve_shape": True,
                    "preserve_proportions": True,
                    "preserve_materials": True,
                    "requirements": ["严格保持五款收纳盘结构和刀具插入比例"],
                },
                "locked_fields": [
                    "style",
                    "colors",
                    "typography",
                    "photography",
                    "quality",
                    "product_fidelity",
                    "prohibitions",
                ],
                "variants": [
                    {
                        "key": "hero",
                        "title": "首屏主视觉",
                        "guidance": ["完整展示套装", "强调秩序与效率"],
                    }
                ],
                "prohibitions": ["不得改变商品结构", "不得编造 Logo 或认证"],
                "reference_assets": [
                    {
                        "asset_id": reference_asset_id,
                        "role": "product_identity",
                        "label": "商品形态参考",
                    }
                ],
            },
            "source_markdown": "专业工业风格、现代极简主义。",
        },
        "visual_exceptions": [],
        "prompt_plans": [
            {
                "key": "hero-prompt",
                "image_type_key": "hero",
                "title": "首屏海报提示词",
                "payload": {
                    "schema_version": 1,
                    "shared_rules": ["保持统一工业视觉体系", "商品结构以参考图为准"],
                    "design_goal": "展示完整产品构成与收纳秩序",
                    "product_fidelity": {
                        "complex_structure": True,
                        "product_present": True,
                        "picture_in_picture": "none",
                        "requirements": ["像素级还原五款收纳盘组合和刀具插入状态"],
                    },
                    "creative_boundary": ["浅灰色极简工业台面", "不得改变产品颜色与结构"],
                    "composition": {
                        "viewpoint": "俯视 45 度侧切视角",
                        "product_share_percent": 75,
                        "layout": "阶梯式居后，圆形与长条形居前，形成 V 字型环绕排布",
                        "copy_regions": ["顶部中心", "右侧"],
                    },
                    "content": {
                        "focus": ["套装完整性", "色彩对比", "收纳密度"],
                        "selling_points": ["一套搞定", "整洁高效"],
                        "background": "浅灰色干净背景，带微弱倒影",
                        "decorations": ["顶部深蓝色促销角标"],
                    },
                    "text": {
                        "headline": "硬质刀具 分类收纳",
                        "subtitle": "一站式解决 车间杂乱难题",
                        "body": None,
                    },
                    "atmosphere": {
                        "keywords": ["专业", "秩序", "高效"],
                        "lighting": "主灯从左上方照射，产生清晰的阶梯投影",
                    },
                    "visual_variant_key": "hero",
                    "fact_keys": ["product_name"],
                    "evidence_asset_ids": [reference_asset_id],
                    "images": [
                        {
                            "image_plan_key": "hero-1",
                            "instruction": "俯视 45 度，V 字形排布",
                            "viewpoint": "俯视 45 度侧切视角",
                            "composition_adjustments": ["完整套装成员全部入镜"],
                            "lighting": "左上方硬调侧逆光",
                        },
                        {
                            "image_plan_key": "hero-2",
                            "instruction": "保持商品结构，调整光位和留白",
                            "composition_adjustments": ["增加右侧文案留白"],
                            "lighting": "右后方硬调轮廓光",
                        },
                    ],
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
