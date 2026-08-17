from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from typing import Any

from pydantic import ValidationError
from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent_sessions import new_agent_session
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.contracts import (
    VisualSystemDraftPayload,
    workflow_draft_payload_hash,
)
from productflow_backend.application.workflow_drafts.service import parse_workflow_draft_payload_or_raise
from productflow_backend.application.workflow_recipes.contracts import (
    RecipePayloadV1,
    recipe_payload_dict,
    recipe_payload_hash,
)
from productflow_backend.application.workflow_recipes.extractor import (
    RecipeSourceType,
    extract_recipe_payload,
)
from productflow_backend.domain.enums import (
    AgentConversationStatus,
    WorkflowDraftStatus,
    WorkflowRecipeKind,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    ImagePromptArtifact,
    Product,
    ProductImageAsset,
    ProductWorkflow,
    VisualSystemVersion,
    WorkflowDraft,
    WorkflowDraftRecipeSeed,
    WorkflowNodeRun,
    WorkflowRecipe,
    WorkflowRecipeVersion,
    WorkflowRun,
    new_id,
)


@dataclass(frozen=True, slots=True)
class WorkflowRecipeArchiveResult:
    recipe: WorkflowRecipe
    changed: bool


@dataclass(frozen=True, slots=True)
class WorkflowRecipeApplicationResult:
    recipe: WorkflowRecipe
    recipe_version: WorkflowRecipeVersion
    draft: WorkflowDraft
    conversation: AgentConversation
    created: bool


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
    query = _workflow_recipe_summary_query()
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
    if recipe.current_version is None:
        raise ConflictError("工作流配方缺少 current version")
    return recipe


def create_workflow_recipe(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    source_type: RecipeSourceType,
    folder_id: str | None,
    node_ids: list[str],
    expected_edit_version: int,
    title: str,
    description: str | None,
    preferred_visual_system_version_id: str | None = None,
) -> WorkflowRecipe:
    normalized_title = _normalize_title(title)
    normalized_description = _normalize_description(description)
    try:
        workflow = _lock_source_workflow(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            expected_edit_version=expected_edit_version,
        )
        payload = _extract_from_source(
            session,
            workflow=workflow,
            source_type=source_type,
            folder_id=folder_id,
            node_ids=node_ids,
        )
        visual_version_id = _resolve_preferred_visual_version_id(
            session,
            workflow=workflow,
            requested_id=preferred_visual_system_version_id,
        )
        recipe = WorkflowRecipe(kind=_kind_for_source(source_type))
        session.add(recipe)
        session.flush()
        version = WorkflowRecipeVersion(
            recipe_id=recipe.id,
            version=1,
            schema_version=1,
            title=normalized_title,
            description=normalized_description,
            payload_json=recipe_payload_dict(payload),
            payload_hash=recipe_payload_hash(payload),
            preferred_visual_system_version_id=visual_version_id,
        )
        session.add(version)
        session.flush()
        recipe.current_version_id = version.id
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
    folder_id: str | None,
    node_ids: list[str],
    expected_edit_version: int,
    title: str,
    description: str | None,
    preferred_visual_system_version_id: str | None = None,
) -> WorkflowRecipe:
    normalized_title = _normalize_title(title)
    normalized_description = _normalize_description(description)
    try:
        recipe = session.scalar(
            select(WorkflowRecipe)
            .options(selectinload(WorkflowRecipe.current_version))
            .where(WorkflowRecipe.id == recipe_id)
            .with_for_update()
        )
        if recipe is None:
            raise NotFoundError("工作流配方不存在")
        if recipe.archived_at is not None:
            raise ConflictError("已归档工作流配方不能追加版本")
        current_version = recipe.current_version
        if current_version is None or current_version.version != expected_recipe_version:
            raise ConflictError("工作流配方版本已变化，请刷新后重试")
        if recipe.kind != _kind_for_source(source_type):
            raise BusinessValidationError("配方 kind 与本次保存来源不一致")

        workflow = _lock_source_workflow(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            expected_edit_version=expected_edit_version,
        )
        payload = _extract_from_source(
            session,
            workflow=workflow,
            source_type=source_type,
            folder_id=folder_id,
            node_ids=node_ids,
        )
        visual_version_id = _resolve_preferred_visual_version_id(
            session,
            workflow=workflow,
            requested_id=preferred_visual_system_version_id,
        )
        version = WorkflowRecipeVersion(
            recipe_id=recipe.id,
            version=current_version.version + 1,
            schema_version=1,
            title=normalized_title,
            description=normalized_description,
            payload_json=recipe_payload_dict(payload),
            payload_hash=recipe_payload_hash(payload),
            preferred_visual_system_version_id=visual_version_id,
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


def apply_workflow_recipe(
    session: Session,
    *,
    product_id: str,
    recipe_id: str,
    expected_recipe_version: int,
    idempotency_key: str,
) -> WorkflowRecipeApplicationResult:
    normalized_key = _normalize_idempotency_key(idempotency_key)
    request_hash = _recipe_application_request_hash(
        product_id=product_id,
        recipe_id=recipe_id,
        expected_recipe_version=expected_recipe_version,
    )
    try:
        product = session.scalar(select(Product).where(Product.id == product_id).with_for_update())
        if product is None:
            raise NotFoundError("商品不存在")
        existing_seed = _recipe_seed_by_idempotency_key(
            session,
            product_id=product_id,
            idempotency_key=normalized_key,
        )
        if existing_seed is not None:
            if existing_seed.request_hash != request_hash:
                raise ConflictError("相同 idempotency key 不能应用不同的工作流配方")
            session.commit()
            return _load_recipe_application(session, existing_seed.id, created=False)

        recipe = session.scalar(
            select(WorkflowRecipe)
            .options(selectinload(WorkflowRecipe.current_version))
            .where(WorkflowRecipe.id == recipe_id)
            .with_for_update()
        )
        if recipe is None:
            raise NotFoundError("工作流配方不存在")
        if recipe.archived_at is not None:
            raise ConflictError("已归档工作流配方不能应用")
        recipe_version = recipe.current_version
        if recipe_version is None or recipe_version.version != expected_recipe_version:
            raise ConflictError("工作流配方版本已变化，请刷新后重试")
        parse_recipe_payload_or_raise(recipe_version)

        base_workflow = None
        if recipe.kind == WorkflowRecipeKind.RECIPE_FRAGMENT:
            base_workflow = session.scalar(
                select(ProductWorkflow)
                .where(
                    ProductWorkflow.product_id == product_id,
                    ProductWorkflow.schema_version == 2,
                    ProductWorkflow.active.is_(True),
                )
                .with_for_update()
            )

        draft = WorkflowDraft(product_id=product_id, status=WorkflowDraftStatus.COLLECTING)
        session.add(draft)
        session.flush()
        seed = WorkflowDraftRecipeSeed(
            workflow_draft_id=draft.id,
            recipe_version_id=recipe_version.id,
            product_id=product_id,
            base_workflow_id=base_workflow.id if base_workflow is not None else None,
            base_workflow_revision=base_workflow.revision if base_workflow is not None else None,
            schema_version=1,
            idempotency_key=normalized_key,
            request_hash=request_hash,
        )
        conversation_id = new_id()
        agent_session = new_agent_session(title=recipe_version.title)
        session.add(agent_session)
        session.flush()
        conversation = AgentConversation(
            id=conversation_id,
            session_id=agent_session.id,
            product_id=product_id,
            workflow_draft_id=draft.id,
            harness_run_id=conversation_id,
            status=AgentConversationStatus.COLLECTING,
        )
        session.add_all([seed, conversation])
        session.commit()
    except IntegrityError:
        session.rollback()
        existing_seed = _recipe_seed_by_idempotency_key(
            session,
            product_id=product_id,
            idempotency_key=normalized_key,
        )
        if existing_seed is not None:
            if existing_seed.request_hash != request_hash:
                raise ConflictError("相同 idempotency key 不能应用不同的工作流配方") from None
            return _load_recipe_application(session, existing_seed.id, created=False)
        raise
    except Exception:
        session.rollback()
        raise
    return _load_recipe_application(session, seed.id, created=True)


def parse_recipe_payload_or_raise(version: WorkflowRecipeVersion) -> RecipePayloadV1:
    try:
        payload = RecipePayloadV1.model_validate(version.payload_json)
    except ValidationError as exc:
        raise ConflictError("工作流配方 payload 不符合 schema version 1") from exc
    if recipe_payload_hash(payload) != version.payload_hash:
        raise ConflictError("工作流配方 payload hash 不一致")
    return payload


def _recipe_seed_by_idempotency_key(
    session: Session,
    *,
    product_id: str,
    idempotency_key: str,
) -> WorkflowDraftRecipeSeed | None:
    return session.scalar(
        select(WorkflowDraftRecipeSeed).where(
            WorkflowDraftRecipeSeed.product_id == product_id,
            WorkflowDraftRecipeSeed.idempotency_key == idempotency_key,
        )
    )


def _load_recipe_application(
    session: Session,
    seed_id: str,
    *,
    created: bool,
) -> WorkflowRecipeApplicationResult:
    seed = session.scalar(
        select(WorkflowDraftRecipeSeed)
        .options(
            selectinload(WorkflowDraftRecipeSeed.recipe_version).selectinload(
                WorkflowRecipeVersion.recipe
            ),
            selectinload(WorkflowDraftRecipeSeed.workflow_draft).selectinload(
                WorkflowDraft.revisions
            ),
            selectinload(WorkflowDraftRecipeSeed.workflow_draft).selectinload(
                WorkflowDraft.recipe_seed
            ),
            selectinload(WorkflowDraftRecipeSeed.workflow_draft).selectinload(
                WorkflowDraft.agent_conversation
            ),
        )
        .where(WorkflowDraftRecipeSeed.id == seed_id)
    )
    if seed is None:
        raise NotFoundError("工作流配方应用记录不存在")
    draft = seed.workflow_draft
    conversation = draft.agent_conversation
    if conversation is None:
        raise ConflictError("工作流配方应用缺少 Agent conversation")
    recipe_version = seed.recipe_version
    return WorkflowRecipeApplicationResult(
        recipe=recipe_version.recipe,
        recipe_version=recipe_version,
        draft=draft,
        conversation=conversation,
        created=created,
    )


def _lock_source_workflow(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    expected_edit_version: int,
) -> ProductWorkflow:
    if session.get(Product, product_id) is None:
        raise NotFoundError("商品不存在")
    workflow = session.scalar(
        select(ProductWorkflow)
        .options(
            selectinload(ProductWorkflow.folders),
            selectinload(ProductWorkflow.nodes),
            selectinload(ProductWorkflow.edges),
            selectinload(ProductWorkflow.prompt_artifacts).selectinload(ImagePromptArtifact.versions),
            selectinload(ProductWorkflow.source_draft_revision),
            selectinload(ProductWorkflow.visual_system_version),
            selectinload(ProductWorkflow.materialization),
        )
        .where(
            ProductWorkflow.id == workflow_id,
            ProductWorkflow.product_id == product_id,
        )
        .with_for_update()
    )
    if workflow is None:
        raise NotFoundError("商品工作流不存在")
    if workflow.schema_version != 2:
        raise ConflictError("工作流配方只能从 schema-v2 工作流保存")
    if not workflow.active:
        raise ConflictError("工作流配方只能从 active schema-v2 工作流保存")
    if workflow.edit_version != expected_edit_version:
        raise ConflictError("工作流 edit version 已变化，请刷新后重试")
    if workflow.source_draft_revision is None or workflow.visual_system_version is None:
        raise ConflictError("schema-v2 工作流缺少 Draft 或 VisualSystem lineage")
    return workflow


def _extract_from_source(
    session: Session,
    *,
    workflow: ProductWorkflow,
    source_type: RecipeSourceType,
    folder_id: str | None,
    node_ids: list[str],
) -> RecipePayloadV1:
    revision = workflow.source_draft_revision
    visual_version = workflow.visual_system_version
    assert revision is not None
    assert visual_version is not None
    source_artifact = parse_workflow_draft_payload_or_raise(revision.payload_json)
    if workflow_draft_payload_hash(source_artifact) != revision.payload_hash:
        raise ConflictError("source WorkflowDraft payload hash 不一致")
    try:
        visual_payload = VisualSystemDraftPayload.model_validate(visual_version.payload_json)
    except ValidationError as exc:
        raise ConflictError("source VisualSystem payload 不符合 schema version 1") from exc
    if _json_hash(visual_payload.model_dump(mode="json")) != visual_version.payload_hash:
        raise ConflictError("source VisualSystem payload hash 不一致")
    entity_ids = _source_entity_ids(session, workflow=workflow)
    return extract_recipe_payload(
        workflow=workflow,
        visual_system_payload=visual_payload,
        source_type=source_type,
        folder_id=folder_id,
        node_ids=node_ids,
        forbidden_entity_ids=entity_ids,
    )


def _source_entity_ids(session: Session, *, workflow: ProductWorkflow) -> set[str]:
    entity_ids = {
        workflow.product_id,
        workflow.id,
        workflow.source_draft_revision_id,
        workflow.visual_system_version_id,
        *(folder.id for folder in workflow.folders),
        *(node.id for node in workflow.nodes),
        *(edge.id for edge in workflow.edges),
        *(artifact.id for artifact in workflow.prompt_artifacts),
        *(
            version.id
            for artifact in workflow.prompt_artifacts
            for version in artifact.versions
        ),
    }
    revision = workflow.source_draft_revision
    if revision is not None:
        entity_ids.add(revision.id)
        entity_ids.add(revision.draft_id)
    if workflow.materialization is not None:
        entity_ids.add(workflow.materialization.id)
    entity_ids.update(
        session.scalars(
            select(ProductImageAsset.id).where(ProductImageAsset.product_id == workflow.product_id)
        )
    )
    run_ids = set(
        session.scalars(select(WorkflowRun.id).where(WorkflowRun.workflow_id == workflow.id))
    )
    entity_ids.update(run_ids)
    if run_ids:
        entity_ids.update(
            session.scalars(
                select(WorkflowNodeRun.id).where(WorkflowNodeRun.workflow_run_id.in_(run_ids))
            )
        )
    return {entity_id for entity_id in entity_ids if entity_id is not None}


def _resolve_preferred_visual_version_id(
    session: Session,
    *,
    workflow: ProductWorkflow,
    requested_id: str | None,
) -> str:
    version_id = requested_id or workflow.visual_system_version_id
    if version_id is None or session.get(VisualSystemVersion, version_id) is None:
        raise NotFoundError("首选视觉体系版本不存在")
    return version_id


def _kind_for_source(source_type: RecipeSourceType) -> WorkflowRecipeKind:
    return (
        WorkflowRecipeKind.WORKFLOW_RECIPE
        if source_type == "workflow"
        else WorkflowRecipeKind.RECIPE_FRAGMENT
    )


def _normalize_title(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("配方名称不能为空")
    if len(normalized) > 255:
        raise BusinessValidationError("配方名称不能超过 255 个字符")
    return normalized


def _normalize_description(value: str | None) -> str | None:
    if value is None:
        return None
    normalized = value.strip()
    if not normalized:
        return None
    if len(normalized) > 4000:
        raise BusinessValidationError("配方描述不能超过 4000 个字符")
    return normalized


def _normalize_idempotency_key(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("idempotency key 不能为空")
    if len(normalized) > 120:
        raise BusinessValidationError("idempotency key 不能超过 120 个字符")
    return normalized


def _recipe_application_request_hash(
    *,
    product_id: str,
    recipe_id: str,
    expected_recipe_version: int,
) -> str:
    return _json_hash(
        {
            "product_id": product_id,
            "recipe_id": recipe_id,
            "expected_recipe_version": expected_recipe_version,
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
    "workflow_recipe_query",
]
