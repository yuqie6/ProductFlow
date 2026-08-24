from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from typing import Any

from pydantic import ValidationError
from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.product_workflow.graph_apply import AppliedGraph
from productflow_backend.application.product_workflow.graph_commands import load_applied_graph
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_recipes.contracts import (
    RECIPE_SCHEMA_VERSION,
    RecipeGovernance,
    RecipePayload,
    parse_recipe_governance,
    recipe_governance_dict,
    recipe_payload_dict,
    recipe_payload_hash,
)
from productflow_backend.application.workflow_recipes.extract import RecipeSourceType, extract_recipe_payload
from productflow_backend.application.workflow_recipes.live_apply import (
    RecipeApplyMode,
    RecipeApplyPreview,
    apply_recipe_plan,
    plan_recipe_payload,
    recipe_application_summary,
)
from productflow_backend.domain.enums import (
    WorkflowRecipeCreationSource,
    WorkflowRecipeKind,
    WorkflowRecipeOrigin,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.domain.graph_catalog import GRAPH_CATALOG_VERSION
from productflow_backend.infrastructure.db.models import (
    Product,
    VisualSystemVersion,
    WorkflowGraph,
    WorkflowRecipe,
    WorkflowRecipeApplication,
    WorkflowRecipeVersion,
)


@dataclass(frozen=True, slots=True)
class WorkflowRecipeArchiveResult:
    recipe: WorkflowRecipe
    changed: bool


@dataclass(frozen=True, slots=True)
class WorkflowRecipeApplicationResult:
    recipe: WorkflowRecipe
    recipe_version: WorkflowRecipeVersion
    graph: WorkflowGraph
    applied: AppliedGraph
    mode: RecipeApplyMode
    created: bool
    added_node_ids: tuple[str, ...]
    added_edge_ids: tuple[str, ...]
    updated_node_ids: tuple[str, ...]
    preview_graph_revision: int | None
    preview_digest: str | None
    required_bindings: tuple[str, ...]


def _workflow_recipe_summary_query():
    return select(WorkflowRecipe).options(selectinload(WorkflowRecipe.current_version))


def workflow_recipe_query():
    return _workflow_recipe_summary_query().options(
        selectinload(WorkflowRecipe.versions),
    )


def list_workflow_recipes(
    session: Session,
    *,
    include_archived: bool = False,
) -> list[WorkflowRecipe]:
    query = _workflow_recipe_summary_query().where(
        WorkflowRecipe.origin == WorkflowRecipeOrigin.USER
    )
    if not include_archived:
        query = query.where(WorkflowRecipe.archived_at.is_(None))
    return list(
        session.scalars(
            query.order_by(WorkflowRecipe.updated_at.desc(), WorkflowRecipe.id)
        ).unique()
    )


def get_workflow_recipe_or_raise(session: Session, *, recipe_id: str) -> WorkflowRecipe:
    recipe = session.scalar(workflow_recipe_query().where(WorkflowRecipe.id == recipe_id))
    if recipe is None:
        raise NotFoundError("工作流配方不存在")
    _reject_official_recipe(recipe)
    if recipe.current_version is None:
        raise ConflictError("工作流配方缺少 current version")
    return recipe


def _reject_official_recipe(recipe: WorkflowRecipe) -> None:
    if recipe.origin is WorkflowRecipeOrigin.OFFICIAL:
        raise NotFoundError("工作流配方不存在")


def create_workflow_recipe(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    source_type: RecipeSourceType,
    group_id: str | None,
    node_ids: list[str],
    expected_graph_revision: int,
    title: str,
    description: str | None,
    preferred_visual_system_version_id: str | None = None,
) -> WorkflowRecipe:
    try:
        payload = _extract_live_recipe_payload(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            source_type=source_type,
            group_id=group_id,
            node_ids=node_ids,
            expected_graph_revision=expected_graph_revision,
        )
        preferred = _optional_visual_system_version(
            session,
            preferred_visual_system_version_id=preferred_visual_system_version_id,
        )
        recipe = WorkflowRecipe(
            kind=_recipe_kind(source_type),
            origin=WorkflowRecipeOrigin.USER,
            official_key=None,
        )
        session.add(recipe)
        session.flush()
        version = _new_recipe_version(
            recipe_id=recipe.id,
            version=1,
            title=title,
            description=description,
            payload=payload,
            preferred_visual_system_version_id=preferred,
            catalog_version=GRAPH_CATALOG_VERSION,
            creation_source=WorkflowRecipeCreationSource.USER_EXTRACT,
            governance=None,
        )
        session.add(version)
        session.flush()
        recipe.current_version_id = version.id
        recipe.updated_at = now_utc()
        session.commit()
    except Exception:
        session.rollback()
        raise
    session.expire_all()
    return get_workflow_recipe_or_raise(session, recipe_id=recipe.id)


def append_workflow_recipe_version(
    session: Session,
    *,
    recipe_id: str,
    expected_recipe_version: int,
    product_id: str,
    workflow_id: str,
    source_type: RecipeSourceType,
    group_id: str | None,
    node_ids: list[str],
    expected_graph_revision: int,
    title: str,
    description: str | None,
    preferred_visual_system_version_id: str | None = None,
) -> WorkflowRecipe:
    try:
        recipe = session.scalar(
            select(WorkflowRecipe)
            .options(selectinload(WorkflowRecipe.current_version))
            .where(WorkflowRecipe.id == recipe_id)
            .with_for_update()
        )
        if recipe is None:
            raise NotFoundError("工作流配方不存在")
        _reject_official_recipe(recipe)
        if recipe.archived_at is not None:
            raise ConflictError("已归档工作流配方不能追加版本")
        current_version = recipe.current_version
        if current_version is None or current_version.version != expected_recipe_version:
            raise ConflictError("工作流配方版本已变化，请刷新后重试")
        payload = _extract_live_recipe_payload(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            source_type=source_type,
            group_id=group_id,
            node_ids=node_ids,
            expected_graph_revision=expected_graph_revision,
        )
        preferred = _optional_visual_system_version(
            session,
            preferred_visual_system_version_id=preferred_visual_system_version_id,
        )
        version = _new_recipe_version(
            recipe_id=recipe.id,
            version=current_version.version + 1,
            title=title,
            description=description,
            payload=payload,
            preferred_visual_system_version_id=preferred,
            catalog_version=GRAPH_CATALOG_VERSION,
            creation_source=WorkflowRecipeCreationSource.USER_EXTRACT,
            governance=None,
        )
        session.add(version)
        session.flush()
        recipe.current_version_id = version.id
        recipe.updated_at = now_utc()
        session.commit()
    except Exception:
        session.rollback()
        raise
    session.expire_all()
    return get_workflow_recipe_or_raise(session, recipe_id=recipe_id)


def _extract_live_recipe_payload(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    source_type: RecipeSourceType,
    group_id: str | None,
    node_ids: list[str],
    expected_graph_revision: int,
) -> RecipePayload:
    graph = session.scalar(
        select(WorkflowGraph)
        .where(WorkflowGraph.id == workflow_id, WorkflowGraph.product_id == product_id)
        .with_for_update()
    )
    if graph is None:
        raise NotFoundError("商品工作流不存在")
    if not graph.active:
        raise ConflictError("只能从 active schema-v3 工作流保存配方")
    if graph.revision != expected_graph_revision:
        raise ConflictError("工作流已变化，请刷新后重试")
    return extract_recipe_payload(
        load_applied_graph(session, graph),
        source_type=source_type,
        group_id=group_id,
        node_ids=node_ids,
    )


def _new_recipe_version(
    *,
    recipe_id: str,
    version: int,
    title: str,
    description: str | None,
    payload: RecipePayload,
    preferred_visual_system_version_id: str | None,
    catalog_version: int,
    creation_source: WorkflowRecipeCreationSource,
    governance: RecipeGovernance | None,
) -> WorkflowRecipeVersion:
    normalized_title = title.strip()
    if not normalized_title:
        raise BusinessValidationError("配方名称不能为空")
    normalized_description = description.strip() if description else None
    return WorkflowRecipeVersion(
        recipe_id=recipe_id,
        version=version,
        schema_version=RECIPE_SCHEMA_VERSION,
        catalog_version=catalog_version,
        creation_source=creation_source,
        title=normalized_title,
        description=normalized_description or None,
        payload_json=recipe_payload_dict(payload),
        payload_hash=recipe_payload_hash(payload),
        governance_json=recipe_governance_dict(governance),
        preferred_visual_system_version_id=preferred_visual_system_version_id,
    )


def _recipe_kind(source_type: RecipeSourceType) -> WorkflowRecipeKind:
    return WorkflowRecipeKind.WORKFLOW_RECIPE if source_type == "workflow" else WorkflowRecipeKind.RECIPE_FRAGMENT


def _optional_visual_system_version(
    session: Session,
    *,
    preferred_visual_system_version_id: str | None,
) -> str | None:
    if preferred_visual_system_version_id is None:
        return None
    version = session.get(VisualSystemVersion, preferred_visual_system_version_id)
    if version is None:
        raise NotFoundError("视觉系统版本不存在")
    return version.id


def archive_workflow_recipe(
    session: Session,
    *,
    recipe_id: str,
    expected_recipe_version: int,
) -> WorkflowRecipeArchiveResult:
    try:
        recipe = session.scalar(
            select(WorkflowRecipe)
            .options(selectinload(WorkflowRecipe.current_version))
            .where(WorkflowRecipe.id == recipe_id)
            .with_for_update()
        )
        if recipe is None:
            raise NotFoundError("工作流配方不存在")
        _reject_official_recipe(recipe)
        current_version = recipe.current_version
        if current_version is None or current_version.version != expected_recipe_version:
            raise ConflictError("工作流配方版本已变化，请刷新后重试")
        changed = recipe.archived_at is None
        if changed:
            recipe.archived_at = now_utc()
            recipe.updated_at = now_utc()
        session.commit()
    except Exception:
        session.rollback()
        raise
    session.expire_all()
    return WorkflowRecipeArchiveResult(
        recipe=get_workflow_recipe_or_raise(session, recipe_id=recipe_id),
        changed=changed,
    )


def preview_workflow_recipe(
    session: Session,
    *,
    product_id: str,
    recipe_id: str,
    expected_recipe_version: int,
) -> RecipeApplyPreview:
    product = session.get(Product, product_id)
    if product is None:
        raise NotFoundError("商品不存在")
    recipe = get_workflow_recipe_or_raise(session, recipe_id=recipe_id)
    if recipe.archived_at is not None:
        raise ConflictError("已归档工作流配方不能应用")
    recipe_version = recipe.current_version
    if recipe_version is None or recipe_version.version != expected_recipe_version:
        raise ConflictError("工作流配方版本已变化，请刷新后重试")
    payload = parse_recipe_payload_or_raise(recipe_version)
    required_bindings = _recipe_required_bindings(recipe_version)
    return plan_recipe_payload(
        session,
        product_id=product_id,
        recipe_id=recipe.id,
        payload=payload,
        recipe_kind=recipe.kind,
        recipe_version=recipe_version.version,
        summary=recipe_application_summary(recipe_version.title),
        payload_hash=recipe_version.payload_hash,
        required_bindings=required_bindings,
    ).preview()


def apply_workflow_recipe(
    session: Session,
    *,
    product_id: str,
    recipe_id: str,
    expected_recipe_version: int,
    expected_graph_revision: int,
    preview_digest: str,
    idempotency_key: str,
) -> WorkflowRecipeApplicationResult:
    normalized_key = _normalize_idempotency_key(idempotency_key)
    normalized_digest = _normalize_preview_digest(preview_digest)
    request_hash = _recipe_application_request_hash(
        product_id=product_id,
        recipe_id=recipe_id,
        expected_recipe_version=expected_recipe_version,
        expected_graph_revision=expected_graph_revision,
        preview_digest=normalized_digest,
    )
    try:
        product = session.scalar(select(Product).where(Product.id == product_id).with_for_update())
        if product is None:
            raise NotFoundError("商品不存在")
        existing_application = _recipe_application_by_idempotency_key(
            session,
            product_id=product_id,
            idempotency_key=normalized_key,
        )
        if existing_application is not None:
            if existing_application.request_hash != request_hash:
                raise ConflictError("相同 idempotency key 不能应用不同的工作流配方")
            session.commit()
            return _load_recipe_graph_application(session, existing_application.id, created=False)

        recipe = session.scalar(
            select(WorkflowRecipe)
            .options(selectinload(WorkflowRecipe.current_version))
            .where(WorkflowRecipe.id == recipe_id)
            .with_for_update()
        )
        if recipe is None:
            raise NotFoundError("工作流配方不存在")
        _reject_official_recipe(recipe)
        if recipe.archived_at is not None:
            raise ConflictError("已归档工作流配方不能应用")
        recipe_version = recipe.current_version
        if recipe_version is None or recipe_version.version != expected_recipe_version:
            raise ConflictError("工作流配方版本已变化，请刷新后重试")
        payload = parse_recipe_payload_or_raise(recipe_version)
        required_bindings = _recipe_required_bindings(recipe_version)
        _lock_active_workflow_graph(session, product_id=product_id)
        plan = plan_recipe_payload(
            session,
            product_id=product_id,
            recipe_id=recipe.id,
            payload=payload,
            recipe_kind=recipe.kind,
            recipe_version=recipe_version.version,
            summary=recipe_application_summary(recipe_version.title),
            payload_hash=recipe_version.payload_hash,
            required_bindings=required_bindings,
            expected_graph_revision=expected_graph_revision,
        )
        if plan.preview_digest != normalized_digest:
            raise ConflictError("配方预览已变化，请重新预览后重试")
        command, mode, added_nodes, added_edges, updated_nodes = apply_recipe_plan(
            session,
            product_id=product_id,
            plan=plan,
            commit=False,
        )
        application = WorkflowRecipeApplication(
            product_id=product_id,
            recipe_version_id=recipe_version.id,
            graph_id=command.graph.id,
            operation_group_id=command.operation_group.id,
            mode=mode,
            idempotency_key=normalized_key,
            request_hash=request_hash,
            added_node_ids_json=list(added_nodes),
            added_edge_ids_json=list(added_edges),
            schema_version=2,
            preview_graph_revision=plan.base_graph_revision,
            preview_digest=plan.preview_digest,
            updated_node_ids_json=list(updated_nodes),
            required_bindings_json=list(required_bindings),
        )
        session.add(application)
        session.commit()
        application_id = application.id
    except IntegrityError:
        session.rollback()
        existing_application = _recipe_application_by_idempotency_key(
            session,
            product_id=product_id,
            idempotency_key=normalized_key,
        )
        if existing_application is not None:
            if existing_application.request_hash != request_hash:
                raise ConflictError("相同 idempotency key 不能应用不同的工作流配方") from None
            return _load_recipe_graph_application(session, existing_application.id, created=False)
        raise
    except Exception:
        session.rollback()
        raise
    return _load_recipe_graph_application(session, application_id, created=True)


def parse_recipe_payload_or_raise(version: WorkflowRecipeVersion) -> RecipePayload:
    try:
        payload = RecipePayload.model_validate(version.payload_json)
    except ValidationError as exc:
        raise ConflictError("工作流配方内容无效") from exc
    if recipe_payload_hash(payload) != version.payload_hash:
        raise ConflictError("工作流配方 payload hash 不一致")
    return payload


def _recipe_required_bindings(version: WorkflowRecipeVersion) -> tuple[str, ...]:
    try:
        governance = parse_recipe_governance(version.governance_json)
    except ValidationError as exc:
        raise ConflictError("工作流配方治理元数据无效") from exc
    return tuple(governance.required_inputs) if governance is not None else ()


def _lock_active_workflow_graph(session: Session, *, product_id: str) -> WorkflowGraph | None:
    return session.scalar(
        select(WorkflowGraph)
        .where(WorkflowGraph.product_id == product_id, WorkflowGraph.active.is_(True))
        .with_for_update()
    )


def _recipe_application_by_idempotency_key(
    session: Session,
    *,
    product_id: str,
    idempotency_key: str,
) -> WorkflowRecipeApplication | None:
    return session.scalar(
        select(WorkflowRecipeApplication).where(
            WorkflowRecipeApplication.product_id == product_id,
            WorkflowRecipeApplication.idempotency_key == idempotency_key,
        )
    )


def _load_recipe_graph_application(
    session: Session,
    application_id: str,
    *,
    created: bool,
) -> WorkflowRecipeApplicationResult:
    application = session.scalar(
        select(WorkflowRecipeApplication)
        .options(
            selectinload(WorkflowRecipeApplication.recipe_version).selectinload(
                WorkflowRecipeVersion.recipe
            ),
            selectinload(WorkflowRecipeApplication.graph),
        )
        .where(WorkflowRecipeApplication.id == application_id)
    )
    if application is None:
        raise NotFoundError("工作流配方应用记录不存在")
    graph = application.graph
    applied = load_applied_graph(session, graph)
    recipe_version = application.recipe_version
    return WorkflowRecipeApplicationResult(
        recipe=recipe_version.recipe,
        recipe_version=recipe_version,
        graph=graph,
        applied=applied,
        mode=application.mode,  # type: ignore[arg-type]
        created=created,
        added_node_ids=tuple(application.added_node_ids_json or ()),
        added_edge_ids=tuple(application.added_edge_ids_json or ()),
        updated_node_ids=tuple(application.updated_node_ids_json or ()),
        preview_graph_revision=application.preview_graph_revision,
        preview_digest=application.preview_digest,
        required_bindings=tuple(application.required_bindings_json or ()),
    )


def _normalize_idempotency_key(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("idempotency key 不能为空")
    if len(normalized) > 120:
        raise BusinessValidationError("idempotency key 不能超过 120 个字符")
    return normalized


def _normalize_preview_digest(value: str) -> str:
    normalized = value.strip().lower()
    if len(normalized) != 64 or any(character not in "0123456789abcdef" for character in normalized):
        raise BusinessValidationError("preview digest 必须是 64 位十六进制字符串")
    return normalized


def _recipe_application_request_hash(
    *,
    product_id: str,
    recipe_id: str,
    expected_recipe_version: int,
    expected_graph_revision: int,
    preview_digest: str,
) -> str:
    return _json_hash(
        {
            "product_id": product_id,
            "recipe_id": recipe_id,
            "expected_recipe_version": expected_recipe_version,
            "expected_graph_revision": expected_graph_revision,
            "preview_digest": preview_digest,
        }
    )


def _json_hash(payload: dict[str, Any]) -> str:
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


__all__ = [
    "WorkflowRecipeArchiveResult",
    "WorkflowRecipeApplicationResult",
    "apply_workflow_recipe",
    "append_workflow_recipe_version",
    "archive_workflow_recipe",
    "create_workflow_recipe",
    "get_workflow_recipe_or_raise",
    "list_workflow_recipes",
    "parse_recipe_payload_or_raise",
    "preview_workflow_recipe",
    "workflow_recipe_query",
]
