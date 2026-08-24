"""add official workflow recipe provenance, governance, and canonical seeds

Revision ID: 20260824_0086
Revises: 20260822_0085
Create Date: 2026-08-24
"""

from __future__ import annotations

from datetime import UTC, datetime

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260824_0086"
down_revision = "20260822_0085"
branch_labels = None
depends_on = None

WORKFLOW_RECIPE_KIND = postgresql.ENUM(
    "workflow_recipe",
    "recipe_fragment",
    name="workflowrecipekind",
    create_type=False,
)
WORKFLOW_RECIPE_ORIGIN = postgresql.ENUM(
    "official",
    "user",
    name="workflowrecipeorigin",
    create_type=False,
)
WORKFLOW_RECIPE_CREATION_SOURCE = postgresql.ENUM(
    "user_extract",
    "official_seed",
    name="workflowrecipecreationsource",
    create_type=False,
)

_CATALOG_VERSION = 5
_SCHEMA_VERSION = 3

_OFFICIAL_SEEDS = (
    {
        "recipe_id": "00000000-0000-4000-8000-000000000301",
        "version_id": "00000000-0000-4000-8000-000000000401",
        "official_key": "hero",
        "title": "白底主图",
        "description": "商品居中、白底、高还原的首屏主图结构。",
        "payload_hash": "58f82460e7a1305404298bd6b24991289674b345981a568c2cf64f646ef9d6c1",
        "payload": {
            "schema_version": 3,
            "nodes": [
                {
                    "key": "prompt-hero",
                    "node_type": "prompt_generation",
                    "title": "白底主图提示词",
                    "position_x": 420,
                    "position_y": 40,
                    "group_key": "shot-hero",
                    "config": {
                        "image_type_key": "hero",
                        "prompt": {
                            "design_goal": "商品主体居中清晰，白底呈现，保持商品结构和材质高还原，不添加文字。"
                        },
                    },
                },
                {
                    "key": "image-hero-1",
                    "node_type": "image_generation",
                    "title": "白底主图 1",
                    "position_x": 760,
                    "position_y": 40,
                    "group_key": "shot-hero",
                    "config": {
                        "image_type_key": "hero",
                        "generation_spec": {
                            "aspect_ratio": "1:1",
                            "resolution_tier": "high",
                            "quality_intent": "high",
                            "reference_fidelity": "high",
                            "background_intent": "opaque",
                            "text_policy": "none",
                        },
                    },
                },
            ],
            "edges": [
                {
                    "key": "edge-prompt-hero-1",
                    "source_node_key": "prompt-hero",
                    "target_node_key": "image-hero-1",
                    "data_type": "prompt",
                    "role": "prompt",
                    "order": 0,
                }
            ],
            "groups": [
                {
                    "key": "shot-hero",
                    "title": "白底主图",
                    "member_keys": ["prompt-hero", "image-hero-1"],
                }
            ],
        },
        "governance": {
            "applicable_image_types": ["hero"],
            "required_inputs": ["product_identity"],
            "default_result": "image_generation",
            "thumbnail": None,
            "provider_sample": None,
        },
    },
    {
        "recipe_id": "00000000-0000-4000-8000-000000000302",
        "version_id": "00000000-0000-4000-8000-000000000402",
        "official_key": "detail",
        "title": "细节特写",
        "description": "突出材质、结构和工艺细节的近距离特写结构。",
        "payload_hash": "634d3733ab6c36e29e76b201f29fe3a41b4027bd977eae298ba7eeacc9b13669",
        "payload": {
            "schema_version": 3,
            "nodes": [
                {
                    "key": "prompt-detail",
                    "node_type": "prompt_generation",
                    "title": "细节特写提示词",
                    "position_x": 420,
                    "position_y": 40,
                    "group_key": "shot-detail",
                    "config": {
                        "image_type_key": "detail",
                        "prompt": {
                            "design_goal": "贴近关键结构表现材质和工艺细节，光线强调质感，不使用整件商品远景或文字。"
                        },
                    },
                },
                {
                    "key": "image-detail-1",
                    "node_type": "image_generation",
                    "title": "细节特写 1",
                    "position_x": 760,
                    "position_y": 40,
                    "group_key": "shot-detail",
                    "config": {
                        "image_type_key": "detail",
                        "generation_spec": {
                            "aspect_ratio": "1:1",
                            "resolution_tier": "high",
                            "quality_intent": "high",
                            "reference_fidelity": "high",
                            "background_intent": "auto",
                            "text_policy": "none",
                        },
                    },
                },
            ],
            "edges": [
                {
                    "key": "edge-prompt-detail-1",
                    "source_node_key": "prompt-detail",
                    "target_node_key": "image-detail-1",
                    "data_type": "prompt",
                    "role": "prompt",
                    "order": 0,
                }
            ],
            "groups": [
                {
                    "key": "shot-detail",
                    "title": "细节特写",
                    "member_keys": ["prompt-detail", "image-detail-1"],
                }
            ],
        },
        "governance": {
            "applicable_image_types": ["detail"],
            "required_inputs": ["product_identity"],
            "default_result": "image_generation",
            "thumbnail": None,
            "provider_sample": None,
        },
    },
    {
        "recipe_id": "00000000-0000-4000-8000-000000000303",
        "version_id": "00000000-0000-4000-8000-000000000403",
        "official_key": "scene",
        "title": "使用场景",
        "description": "商品作为主角融入实际使用环境的场景结构。",
        "payload_hash": "94a9d18cb89330af5dad857217bcb1ca902bbe4bf3ddeb166c88f541734562e8",
        "payload": {
            "schema_version": 3,
            "nodes": [
                {
                    "key": "prompt-scene",
                    "node_type": "prompt_generation",
                    "title": "使用场景提示词",
                    "position_x": 420,
                    "position_y": 40,
                    "group_key": "shot-scene",
                    "config": {
                        "image_type_key": "scene",
                        "prompt": {
                    "design_goal": (
                        "把商品放进真实使用场景，环境服务于商品且商品清晰可辨，"
                        "不添加文字或无依据的品牌道具。"
                    )
                        },
                    },
                },
                {
                    "key": "image-scene-1",
                    "node_type": "image_generation",
                    "title": "使用场景 1",
                    "position_x": 760,
                    "position_y": 40,
                    "group_key": "shot-scene",
                    "config": {
                        "image_type_key": "scene",
                        "generation_spec": {
                            "aspect_ratio": "4:5",
                            "resolution_tier": "high",
                            "quality_intent": "high",
                            "reference_fidelity": "high",
                            "background_intent": "auto",
                            "text_policy": "none",
                        },
                    },
                },
            ],
            "edges": [
                {
                    "key": "edge-prompt-scene-1",
                    "source_node_key": "prompt-scene",
                    "target_node_key": "image-scene-1",
                    "data_type": "prompt",
                    "role": "prompt",
                    "order": 0,
                }
            ],
            "groups": [
                {
                    "key": "shot-scene",
                    "title": "使用场景",
                    "member_keys": ["prompt-scene", "image-scene-1"],
                }
            ],
        },
        "governance": {
            "applicable_image_types": ["scene"],
            "required_inputs": ["product_identity"],
            "default_result": "image_generation",
            "thumbnail": None,
            "provider_sample": None,
        },
    },
    {
        "recipe_id": "00000000-0000-4000-8000-000000000304",
        "version_id": "00000000-0000-4000-8000-000000000404",
        "official_key": "selling_point",
        "title": "卖点信息图",
        "description": "商品为主角、短标题和卖点清晰对齐的信息图结构。",
        "payload_hash": "4093564bbcfceb63f9f08bd060cfea6c73d475aaac4a726b66f439c7a02e144a",
        "payload": {
            "schema_version": 3,
            "nodes": [
                {
                    "key": "prompt-selling_point",
                    "node_type": "prompt_generation",
                    "title": "卖点信息图提示词",
                    "position_x": 420,
                    "position_y": 40,
                    "group_key": "shot-selling_point",
                    "config": {
                        "image_type_key": "selling_point",
                        "prompt": {
                    "design_goal": (
                        "围绕商品整理一个主标题和两到四条短卖点，层次清楚、"
                        "色块克制，文字使用商品语言环境。"
                    )
                        },
                    },
                },
                {
                    "key": "image-selling_point-1",
                    "node_type": "image_generation",
                    "title": "卖点信息图 1",
                    "position_x": 760,
                    "position_y": 40,
                    "group_key": "shot-selling_point",
                    "config": {
                        "image_type_key": "selling_point",
                        "generation_spec": {
                            "aspect_ratio": "1:1",
                            "resolution_tier": "high",
                            "quality_intent": "high",
                            "reference_fidelity": "high",
                            "background_intent": "auto",
                            "text_policy": "required",
                            "text_language": "zh-CN",
                        },
                    },
                },
            ],
            "edges": [
                {
                    "key": "edge-prompt-selling_point-1",
                    "source_node_key": "prompt-selling_point",
                    "target_node_key": "image-selling_point-1",
                    "data_type": "prompt",
                    "role": "prompt",
                    "order": 0,
                }
            ],
            "groups": [
                {
                    "key": "shot-selling_point",
                    "title": "卖点信息图",
                    "member_keys": ["prompt-selling_point", "image-selling_point-1"],
                }
            ],
        },
        "governance": {
            "applicable_image_types": ["selling_point"],
            "required_inputs": ["product_identity", "product_facts", "product_locale"],
            "default_result": "image_generation",
            "thumbnail": None,
            "provider_sample": None,
        },
    },
)


def upgrade() -> None:
    bind = op.get_bind()
    WORKFLOW_RECIPE_ORIGIN.create(bind, checkfirst=True)
    WORKFLOW_RECIPE_CREATION_SOURCE.create(bind, checkfirst=True)

    with op.batch_alter_table("workflow_recipes") as batch_op:
        batch_op.add_column(
            sa.Column("origin", WORKFLOW_RECIPE_ORIGIN, nullable=False, server_default="user")
        )
        batch_op.add_column(sa.Column("official_key", sa.String(length=80), nullable=True))
        batch_op.create_index("ix_workflow_recipes_origin", ["origin"])
        batch_op.create_unique_constraint("uq_workflow_recipes_official_key", ["official_key"])
        batch_op.create_check_constraint(
            "ck_workflow_recipes_origin_key",
            "(origin = 'official' AND official_key IS NOT NULL) OR "
            "(origin = 'user' AND official_key IS NULL)",
        )

    with op.batch_alter_table("workflow_recipe_versions") as batch_op:
        batch_op.add_column(sa.Column("catalog_version", sa.Integer(), nullable=False, server_default="5"))
        batch_op.add_column(
            sa.Column("creation_source", WORKFLOW_RECIPE_CREATION_SOURCE, nullable=False, server_default="user_extract")
        )
        batch_op.add_column(sa.Column("governance_json", sa.JSON(), nullable=True))
        batch_op.create_check_constraint(
            "ck_workflow_recipe_versions_catalog_version",
            "catalog_version > 0",
        )

    recipe_table = sa.table(
        "workflow_recipes",
        sa.column("id", sa.String(length=36)),
        sa.column("kind", WORKFLOW_RECIPE_KIND),
        sa.column("origin", WORKFLOW_RECIPE_ORIGIN),
        sa.column("official_key", sa.String(length=80)),
        sa.column("current_version_id", sa.String(length=36)),
        sa.column("archived_at", sa.DateTime(timezone=True)),
        sa.column("created_at", sa.DateTime(timezone=True)),
        sa.column("updated_at", sa.DateTime(timezone=True)),
    )
    version_table = sa.table(
        "workflow_recipe_versions",
        sa.column("id", sa.String(length=36)),
        sa.column("recipe_id", sa.String(length=36)),
        sa.column("version", sa.Integer()),
        sa.column("schema_version", sa.Integer()),
        sa.column("catalog_version", sa.Integer()),
        sa.column("creation_source", WORKFLOW_RECIPE_CREATION_SOURCE),
        sa.column("title", sa.String(length=255)),
        sa.column("description", sa.Text()),
        sa.column("payload_json", sa.JSON()),
        sa.column("payload_hash", sa.String(length=64)),
        sa.column("governance_json", sa.JSON()),
        sa.column("preferred_visual_system_version_id", sa.String(length=36)),
        sa.column("created_at", sa.DateTime(timezone=True)),
    )
    now = datetime.now(UTC)
    op.bulk_insert(
        recipe_table,
        [
            {
                "id": seed["recipe_id"],
                "kind": "recipe_fragment",
                "origin": "official",
                "official_key": seed["official_key"],
                "current_version_id": None,
                "archived_at": None,
                "created_at": now,
                "updated_at": now,
            }
            for seed in _OFFICIAL_SEEDS
        ],
    )
    op.bulk_insert(
        version_table,
        [
            {
                "id": seed["version_id"],
                "recipe_id": seed["recipe_id"],
                "version": 1,
                "schema_version": _SCHEMA_VERSION,
                "catalog_version": _CATALOG_VERSION,
                "creation_source": "official_seed",
                "title": seed["title"],
                "description": seed["description"],
                "payload_json": seed["payload"],
                "payload_hash": seed["payload_hash"],
                "governance_json": seed["governance"],
                "preferred_visual_system_version_id": None,
                "created_at": now,
            }
            for seed in _OFFICIAL_SEEDS
        ],
    )
    for seed in _OFFICIAL_SEEDS:
        bind.execute(
            sa.text(
                "UPDATE workflow_recipes SET current_version_id = :version_id "
                "WHERE id = :recipe_id AND current_version_id IS NULL"
            ),
            {"version_id": seed["version_id"], "recipe_id": seed["recipe_id"]},
        )


def downgrade() -> None:
    bind = op.get_bind()
    referenced = bind.execute(
        sa.text(
            """
            SELECT versions.id
            FROM workflow_recipe_versions AS versions
            JOIN workflow_recipes AS recipes ON recipes.id = versions.recipe_id
            WHERE recipes.origin = 'official'
              AND (
                EXISTS (
                    SELECT 1
                    FROM workflow_recipe_applications AS applications
                    WHERE applications.recipe_version_id = versions.id
                )
                OR EXISTS (
                    SELECT 1
                    FROM workflow_draft_recipe_seeds AS seeds
                    WHERE seeds.recipe_version_id = versions.id
                )
              )
            LIMIT 1
            """
        )
    ).first()
    if referenced is not None:
        raise RuntimeError("官方配方版本已被 application 或 Draft seed 引用，不能 downgrade 并破坏审计")

    bind.execute(sa.text("DELETE FROM workflow_recipes WHERE origin = 'official'"))

    with op.batch_alter_table("workflow_recipe_versions") as batch_op:
        batch_op.drop_constraint("ck_workflow_recipe_versions_catalog_version", type_="check")
        batch_op.drop_column("governance_json")
        batch_op.drop_column("creation_source")
        batch_op.drop_column("catalog_version")

    with op.batch_alter_table("workflow_recipes") as batch_op:
        batch_op.drop_constraint("ck_workflow_recipes_origin_key", type_="check")
        batch_op.drop_constraint("uq_workflow_recipes_official_key", type_="unique")
        batch_op.drop_index("ix_workflow_recipes_origin")
        batch_op.drop_column("official_key")
        batch_op.drop_column("origin")

    WORKFLOW_RECIPE_CREATION_SOURCE.drop(bind, checkfirst=True)
    WORKFLOW_RECIPE_ORIGIN.drop(bind, checkfirst=True)
