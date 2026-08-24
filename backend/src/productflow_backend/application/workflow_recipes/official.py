"""Official recipe fragment definitions used by application code and drift tests."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from productflow_backend.application.workflow_recipes.contracts import (
    RecipeGovernance,
    RecipeGraphEdge,
    RecipeGraphGroup,
    RecipeGraphNode,
    RecipePayload,
    recipe_payload_hash,
)
from productflow_backend.domain.enums import GraphEdgeDataType, GraphEdgeRole, GraphNodeType

OFFICIAL_RECIPE_KEYS = ("hero", "detail", "scene", "selling_point")
OFFICIAL_RECIPE_CATALOG_VERSION = 5
OFFICIAL_RECIPE_VERSION = 1

_OFFICIAL_RECIPE_IDS = {
    "hero": "00000000-0000-4000-8000-000000000301",
    "detail": "00000000-0000-4000-8000-000000000302",
    "scene": "00000000-0000-4000-8000-000000000303",
    "selling_point": "00000000-0000-4000-8000-000000000304",
}
_OFFICIAL_VERSION_IDS = {
    "hero": "00000000-0000-4000-8000-000000000401",
    "detail": "00000000-0000-4000-8000-000000000402",
    "scene": "00000000-0000-4000-8000-000000000403",
    "selling_point": "00000000-0000-4000-8000-000000000404",
}


@dataclass(frozen=True, slots=True)
class OfficialRecipeSeed:
    official_key: str
    recipe_id: str
    version_id: str
    title: str
    description: str
    payload: RecipePayload
    governance: RecipeGovernance

    @property
    def payload_hash(self) -> str:
        return recipe_payload_hash(self.payload)


def _official_payload(
    *,
    official_key: str,
    title: str,
    prompt_goal: str,
    generation_spec: dict[str, Any],
) -> RecipePayload:
    group_key = f"shot-{official_key}"
    prompt_key = f"prompt-{official_key}"
    image_key = f"image-{official_key}-1"
    return RecipePayload(
        nodes=[
            RecipeGraphNode(
                key=prompt_key,
                node_type=GraphNodeType.PROMPT_GENERATION,
                title=f"{title}提示词",
                position_x=420,
                position_y=40,
                group_key=group_key,
                config={
                    "image_type_key": official_key,
                    "prompt": {"design_goal": prompt_goal},
                },
            ),
            RecipeGraphNode(
                key=image_key,
                node_type=GraphNodeType.IMAGE_GENERATION,
                title=f"{title} 1",
                position_x=760,
                position_y=40,
                group_key=group_key,
                config={
                    "image_type_key": official_key,
                    "generation_spec": generation_spec,
                },
            ),
        ],
        edges=[
            RecipeGraphEdge(
                key=f"edge-prompt-{official_key}-1",
                source_node_key=prompt_key,
                target_node_key=image_key,
                data_type=GraphEdgeDataType.PROMPT,
                role=GraphEdgeRole.PROMPT,
                order=0,
            )
        ],
        groups=[
            RecipeGraphGroup(
                key=group_key,
                title=title,
                member_keys=(prompt_key, image_key),
            )
        ],
    )


def _seed(
    *,
    official_key: str,
    title: str,
    description: str,
    prompt_goal: str,
    generation_spec: dict[str, Any],
    required_inputs: tuple[str, ...],
) -> OfficialRecipeSeed:
    return OfficialRecipeSeed(
        official_key=official_key,
        recipe_id=_OFFICIAL_RECIPE_IDS[official_key],
        version_id=_OFFICIAL_VERSION_IDS[official_key],
        title=title,
        description=description,
        payload=_official_payload(
            official_key=official_key,
            title=title,
            prompt_goal=prompt_goal,
            generation_spec=generation_spec,
        ),
        governance=RecipeGovernance(
            applicable_image_types=(official_key,),
            required_inputs=required_inputs,
            default_result="image_generation",
            thumbnail=None,
            provider_sample=None,
        ),
    )


OFFICIAL_RECIPE_SEEDS = (
    _seed(
        official_key="hero",
        title="白底主图",
        description="商品居中、白底、高还原的首屏主图结构。",
        prompt_goal="商品主体居中清晰，白底呈现，保持商品结构和材质高还原，不添加文字。",
        generation_spec={
            "aspect_ratio": "1:1",
            "resolution_tier": "high",
            "quality_intent": "high",
            "reference_fidelity": "high",
            "background_intent": "opaque",
            "text_policy": "none",
        },
        required_inputs=("product_identity",),
    ),
    _seed(
        official_key="detail",
        title="细节特写",
        description="突出材质、结构和工艺细节的近距离特写结构。",
        prompt_goal="贴近关键结构表现材质和工艺细节，光线强调质感，不使用整件商品远景或文字。",
        generation_spec={
            "aspect_ratio": "1:1",
            "resolution_tier": "high",
            "quality_intent": "high",
            "reference_fidelity": "high",
            "background_intent": "auto",
            "text_policy": "none",
        },
        required_inputs=("product_identity",),
    ),
    _seed(
        official_key="scene",
        title="使用场景",
        description="商品作为主角融入实际使用环境的场景结构。",
        prompt_goal="把商品放进真实使用场景，环境服务于商品且商品清晰可辨，不添加文字或无依据的品牌道具。",
        generation_spec={
            "aspect_ratio": "4:5",
            "resolution_tier": "high",
            "quality_intent": "high",
            "reference_fidelity": "high",
            "background_intent": "auto",
            "text_policy": "none",
        },
        required_inputs=("product_identity",),
    ),
    _seed(
        official_key="selling_point",
        title="卖点信息图",
        description="商品为主角、短标题和卖点清晰对齐的信息图结构。",
        prompt_goal="围绕商品整理一个主标题和两到四条短卖点，层次清楚、色块克制，文字使用商品语言环境。",
        generation_spec={
            "aspect_ratio": "1:1",
            "resolution_tier": "high",
            "quality_intent": "high",
            "reference_fidelity": "high",
            "background_intent": "auto",
            "text_policy": "required",
            "text_language": "zh-CN",
        },
        required_inputs=("product_identity", "product_facts", "product_locale"),
    ),
)


def official_recipe_seeds() -> tuple[OfficialRecipeSeed, ...]:
    """Return the immutable ordered set used by current code and tests."""

    return OFFICIAL_RECIPE_SEEDS


def official_recipe_seed(official_key: str) -> OfficialRecipeSeed:
    for seed in OFFICIAL_RECIPE_SEEDS:
        if seed.official_key == official_key:
            return seed
    raise KeyError(f"unknown official recipe key: {official_key}")


__all__ = [
    "OFFICIAL_RECIPE_CATALOG_VERSION",
    "OFFICIAL_RECIPE_KEYS",
    "OFFICIAL_RECIPE_SEEDS",
    "OFFICIAL_RECIPE_VERSION",
    "OfficialRecipeSeed",
    "official_recipe_seed",
    "official_recipe_seeds",
]
