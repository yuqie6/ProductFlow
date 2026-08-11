"""add canonical media objects and product image assets

Revision ID: 20260811_0031
Revises: 20260806_0030
Create Date: 2026-08-11
"""

from __future__ import annotations

from pathlib import PurePosixPath
from typing import Any
from uuid import uuid4

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260811_0031"
down_revision = "20260806_0030"
branch_labels = None
depends_on = None

MEDIA_VERIFICATION_STATUS = postgresql.ENUM(
    "verified",
    "legacy_pending",
    "missing",
    name="mediaverificationstatus",
    create_type=False,
)
PRODUCT_IMAGE_ORIGIN_TYPE = postgresql.ENUM(
    "upload",
    "workflow_generation",
    "image_session_attach",
    "legacy_import",
    name="productimageorigintype",
    create_type=False,
)

SOURCE_CANONICAL_FK = "fk_source_assets_canonical_asset_id"
POSTER_CANONICAL_FK = "fk_poster_variants_canonical_asset_id"
SESSION_MEDIA_FK = "fk_image_session_assets_media_object_id"
PRODUCT_COVER_FK = "fk_products_cover_image_asset_id"


def _new_id() -> str:
    return str(uuid4())


def _filename_from_path(storage_path: str, fallback: str) -> str:
    filename = PurePosixPath(storage_path.replace("\\", "/")).name
    return filename or fallback


def _add_compatibility_columns() -> None:
    bind = op.get_bind()
    changes = (
        (
            "source_assets",
            sa.Column("canonical_asset_id", sa.String(length=36), nullable=True),
            SOURCE_CANONICAL_FK,
            "product_image_assets",
            "canonical_asset_id",
            "ix_source_assets_canonical_asset_id",
            "RESTRICT",
        ),
        (
            "poster_variants",
            sa.Column("canonical_asset_id", sa.String(length=36), nullable=True),
            POSTER_CANONICAL_FK,
            "product_image_assets",
            "canonical_asset_id",
            "ix_poster_variants_canonical_asset_id",
            "RESTRICT",
        ),
        (
            "image_session_assets",
            sa.Column("media_object_id", sa.String(length=36), nullable=True),
            SESSION_MEDIA_FK,
            "media_objects",
            "media_object_id",
            "ix_image_session_assets_media_object_id",
            "RESTRICT",
        ),
        (
            "products",
            sa.Column("cover_image_asset_id", sa.String(length=36), nullable=True),
            PRODUCT_COVER_FK,
            "product_image_assets",
            "cover_image_asset_id",
            None,
            "SET NULL",
        ),
    )
    for table_name, column, fk_name, referred_table, column_name, index_name, ondelete in changes:
        if bind.dialect.name == "sqlite":
            with op.batch_alter_table(table_name, recreate="always") as batch_op:
                batch_op.add_column(column)
                batch_op.create_foreign_key(
                    fk_name,
                    referred_table,
                    [column_name],
                    ["id"],
                    ondelete=ondelete,
                )
        else:
            op.add_column(table_name, column)
            op.create_foreign_key(
                fk_name,
                table_name,
                referred_table,
                [column_name],
                ["id"],
                ondelete=ondelete,
            )
        if index_name is not None:
            op.create_index(index_name, table_name, [column_name])


def _drop_compatibility_columns() -> None:
    bind = op.get_bind()
    changes = (
        ("products", "cover_image_asset_id", PRODUCT_COVER_FK, None),
        (
            "image_session_assets",
            "media_object_id",
            SESSION_MEDIA_FK,
            "ix_image_session_assets_media_object_id",
        ),
        (
            "poster_variants",
            "canonical_asset_id",
            POSTER_CANONICAL_FK,
            "ix_poster_variants_canonical_asset_id",
        ),
        (
            "source_assets",
            "canonical_asset_id",
            SOURCE_CANONICAL_FK,
            "ix_source_assets_canonical_asset_id",
        ),
    )
    for table_name, column_name, fk_name, index_name in changes:
        if index_name is not None:
            op.drop_index(index_name, table_name=table_name)
        if bind.dialect.name == "sqlite":
            with op.batch_alter_table(table_name, recreate="always") as batch_op:
                batch_op.drop_constraint(fk_name, type_="foreignkey")
                batch_op.drop_column(column_name)
        else:
            op.drop_constraint(fk_name, table_name, type_="foreignkey")
            op.drop_column(table_name, column_name)


def _backfill_canonical_assets(connection: sa.Connection) -> None:
    media_objects = sa.table(
        "media_objects",
        sa.column("id", sa.String(length=36)),
        sa.column("storage_path", sa.String(length=500)),
        sa.column("mime_type", sa.String(length=100)),
        sa.column("byte_size", sa.BigInteger()),
        sa.column("width", sa.Integer()),
        sa.column("height", sa.Integer()),
        sa.column("sha256", sa.String(length=64)),
        sa.column("verification_status", MEDIA_VERIFICATION_STATUS),
        sa.column("created_at", sa.DateTime(timezone=True)),
        sa.column("verified_at", sa.DateTime(timezone=True)),
    )
    product_assets = sa.table(
        "product_image_assets",
        sa.column("id", sa.String(length=36)),
        sa.column("product_id", sa.String(length=36)),
        sa.column("media_object_id", sa.String(length=36)),
        sa.column("origin_type", PRODUCT_IMAGE_ORIGIN_TYPE),
        sa.column("display_name", sa.String(length=255)),
        sa.column("original_filename", sa.String(length=255)),
        sa.column("parent_asset_id", sa.String(length=36)),
        sa.column("source_image_session_asset_id", sa.String(length=36)),
        sa.column("created_at", sa.DateTime(timezone=True)),
        sa.column("updated_at", sa.DateTime(timezone=True)),
    )
    source_assets = sa.table(
        "source_assets",
        sa.column("id", sa.String(length=36)),
        sa.column("product_id", sa.String(length=36)),
        sa.column("kind", sa.String(length=40)),
        sa.column("original_filename", sa.String(length=255)),
        sa.column("mime_type", sa.String(length=100)),
        sa.column("storage_path", sa.String(length=500)),
        sa.column("source_poster_variant_id", sa.String(length=36)),
        sa.column("canonical_asset_id", sa.String(length=36)),
        sa.column("created_at", sa.DateTime(timezone=True)),
    )
    posters = sa.table(
        "poster_variants",
        sa.column("id", sa.String(length=36)),
        sa.column("product_id", sa.String(length=36)),
        sa.column("template_name", sa.String(length=100)),
        sa.column("mime_type", sa.String(length=100)),
        sa.column("storage_path", sa.String(length=500)),
        sa.column("canonical_asset_id", sa.String(length=36)),
        sa.column("created_at", sa.DateTime(timezone=True)),
    )
    session_assets = sa.table(
        "image_session_assets",
        sa.column("id", sa.String(length=36)),
        sa.column("mime_type", sa.String(length=100)),
        sa.column("storage_path", sa.String(length=500)),
        sa.column("media_object_id", sa.String(length=36)),
        sa.column("created_at", sa.DateTime(timezone=True)),
    )
    products = sa.table(
        "products",
        sa.column("id", sa.String(length=36)),
        sa.column("cover_image_asset_id", sa.String(length=36)),
    )

    media_by_path: dict[str, str] = {}
    used_asset_ids: set[str] = set()

    def ensure_media(storage_path: str, mime_type: str, created_at: Any) -> str:
        existing_id = media_by_path.get(storage_path)
        if existing_id is not None:
            return existing_id
        media_id = _new_id()
        connection.execute(
            media_objects.insert().values(
                id=media_id,
                storage_path=storage_path,
                mime_type=mime_type,
                byte_size=None,
                width=None,
                height=None,
                sha256=None,
                verification_status="legacy_pending",
                created_at=created_at,
                verified_at=None,
            )
        )
        media_by_path[storage_path] = media_id
        return media_id

    poster_rows = {
        row["id"]: dict(row)
        for row in connection.execute(sa.select(posters)).mappings()
    }
    source_rows = [dict(row) for row in connection.execute(sa.select(source_assets)).mappings()]
    source_rows.sort(key=lambda row: (str(row["created_at"]), row["id"]))
    source_asset_by_poster: dict[str, str] = {}
    original_asset_by_product: dict[str, str] = {}

    for row in source_rows:
        media_id = ensure_media(row["storage_path"], row["mime_type"], row["created_at"])
        poster = poster_rows.get(row["source_poster_variant_id"])
        has_valid_lineage = poster is not None and poster["product_id"] == row["product_id"]
        has_invalid_lineage = row["source_poster_variant_id"] is not None and not has_valid_lineage
        origin_type = (
            "workflow_generation"
            if has_valid_lineage
            else "legacy_import"
            if has_invalid_lineage or row["kind"] == "processed_product_image"
            else "upload"
        )
        connection.execute(
            product_assets.insert().values(
                id=row["id"],
                product_id=row["product_id"],
                media_object_id=media_id,
                origin_type=origin_type,
                display_name=row["original_filename"],
                original_filename=row["original_filename"],
                parent_asset_id=None,
                source_image_session_asset_id=None,
                created_at=row["created_at"],
                updated_at=row["created_at"],
            )
        )
        connection.execute(
            source_assets.update()
            .where(source_assets.c.id == row["id"])
            .values(canonical_asset_id=row["id"])
        )
        used_asset_ids.add(row["id"])
        if has_valid_lineage:
            source_asset_by_poster[row["source_poster_variant_id"]] = row["id"]
        if row["kind"] == "original_image":
            original_asset_by_product[row["product_id"]] = row["id"]

    for row in sorted(poster_rows.values(), key=lambda value: (str(value["created_at"]), value["id"])):
        canonical_asset_id = source_asset_by_poster.get(row["id"])
        if canonical_asset_id is None:
            media_id = ensure_media(row["storage_path"], row["mime_type"], row["created_at"])
            canonical_asset_id = row["id"] if row["id"] not in used_asset_ids else _new_id()
            filename = _filename_from_path(row["storage_path"], f"{row['id']}.png")
            connection.execute(
                product_assets.insert().values(
                    id=canonical_asset_id,
                    product_id=row["product_id"],
                    media_object_id=media_id,
                    origin_type="legacy_import",
                    display_name=row["template_name"] or filename,
                    original_filename=filename,
                    parent_asset_id=None,
                    source_image_session_asset_id=None,
                    created_at=row["created_at"],
                    updated_at=row["created_at"],
                )
            )
            used_asset_ids.add(canonical_asset_id)
        connection.execute(
            posters.update()
            .where(posters.c.id == row["id"])
            .values(canonical_asset_id=canonical_asset_id)
        )

    for row in connection.execute(sa.select(session_assets)).mappings():
        media_id = ensure_media(row["storage_path"], row["mime_type"], row["created_at"])
        connection.execute(
            session_assets.update()
            .where(session_assets.c.id == row["id"])
            .values(media_object_id=media_id)
        )

    for product_id, asset_id in original_asset_by_product.items():
        connection.execute(
            products.update()
            .where(products.c.id == product_id)
            .values(cover_image_asset_id=asset_id)
        )


def upgrade() -> None:
    bind = op.get_bind()
    MEDIA_VERIFICATION_STATUS.create(bind, checkfirst=True)
    PRODUCT_IMAGE_ORIGIN_TYPE.create(bind, checkfirst=True)

    op.create_table(
        "media_objects",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("storage_path", sa.String(length=500), nullable=False),
        sa.Column("mime_type", sa.String(length=100), nullable=False),
        sa.Column("byte_size", sa.BigInteger(), nullable=True),
        sa.Column("width", sa.Integer(), nullable=True),
        sa.Column("height", sa.Integer(), nullable=True),
        sa.Column("sha256", sa.String(length=64), nullable=True),
        sa.Column("verification_status", MEDIA_VERIFICATION_STATUS, nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("verified_at", sa.DateTime(timezone=True), nullable=True),
        sa.CheckConstraint(
            "verification_status != 'verified' OR "
            "(byte_size > 0 AND width > 0 AND height > 0 AND sha256 IS NOT NULL "
            "AND length(sha256) = 64 AND verified_at IS NOT NULL)",
            name="ck_media_objects_verified_metadata",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("storage_path", name="uq_media_objects_storage_path"),
    )
    op.create_table(
        "product_image_assets",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("media_object_id", sa.String(length=36), nullable=False),
        sa.Column("origin_type", PRODUCT_IMAGE_ORIGIN_TYPE, nullable=False),
        sa.Column("display_name", sa.String(length=255), nullable=False),
        sa.Column("original_filename", sa.String(length=255), nullable=False),
        sa.Column("parent_asset_id", sa.String(length=36), nullable=True),
        sa.Column("source_image_session_asset_id", sa.String(length=36), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_product_image_assets_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["media_object_id"],
            ["media_objects.id"],
            name="fk_product_image_assets_media_object_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["parent_asset_id"],
            ["product_image_assets.id"],
            name="fk_product_image_assets_parent_asset_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["source_image_session_asset_id"],
            ["image_session_assets.id"],
            name="fk_product_image_assets_source_image_session_asset_id",
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(
        "ix_product_image_assets_product_created",
        "product_image_assets",
        ["product_id", "created_at", "id"],
    )
    op.create_index(
        "ix_product_image_assets_media_object_id",
        "product_image_assets",
        ["media_object_id"],
    )
    op.create_index(
        "ix_product_image_assets_parent_asset_id",
        "product_image_assets",
        ["parent_asset_id"],
    )
    op.create_index(
        "ix_product_image_assets_source_image_session_asset_id",
        "product_image_assets",
        ["source_image_session_asset_id"],
    )
    op.create_index(
        "uq_product_image_assets_product_session_asset",
        "product_image_assets",
        ["product_id", "source_image_session_asset_id"],
        unique=True,
    )

    _add_compatibility_columns()
    _backfill_canonical_assets(bind)


def downgrade() -> None:
    bind = op.get_bind()
    _drop_compatibility_columns()
    op.drop_index("uq_product_image_assets_product_session_asset", table_name="product_image_assets")
    op.drop_index("ix_product_image_assets_source_image_session_asset_id", table_name="product_image_assets")
    op.drop_index("ix_product_image_assets_parent_asset_id", table_name="product_image_assets")
    op.drop_index("ix_product_image_assets_media_object_id", table_name="product_image_assets")
    op.drop_index("ix_product_image_assets_product_created", table_name="product_image_assets")
    op.drop_table("product_image_assets")
    op.drop_table("media_objects")
    PRODUCT_IMAGE_ORIGIN_TYPE.drop(bind, checkfirst=True)
    MEDIA_VERIFICATION_STATUS.drop(bind, checkfirst=True)
