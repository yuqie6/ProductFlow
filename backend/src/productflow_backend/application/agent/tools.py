"""Agent 工具面：有界只读上下文，以及账本+request hash 的 prepare/apply/reconcile。"""

from __future__ import annotations

import base64
import hashlib
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

from pydantic import ValidationError
from sqlalchemy import and_, or_, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent.conversations import (
    AGENT_MAX_INPUT_ASSETS,
    get_agent_conversation_by_id_or_raise,
)
from productflow_backend.application.agent.global_draft_contracts import global_agent_draft_schema
from productflow_backend.application.agent.product_intake import (
    AgentProductSelectionV1,
    agent_product_image_type_catalog_json,
    workflow_intake_payload,
)
from productflow_backend.application.agent.product_workspaces import (
    finalize_agent_product_workspace_intake_from_assets,
)
from productflow_backend.application.agent.sessions import get_agent_session_or_raise
from productflow_backend.application.agent.tasks import task_contract
from productflow_backend.application.media_library.drafts import validate_library_organization_draft
from productflow_backend.application.media_library.queries import get_media_library_asset, list_media_library_assets
from productflow_backend.application.media_library.service import validate_media_library_asset_for_use
from productflow_backend.application.media_objects import inspect_image_bytes
from productflow_backend.application.product_images.mutations import (
    GALLERY_MOVE_MAX_ASSETS,
    GalleryAssetMove,
    normalize_gallery_display_name,
    normalize_gallery_folder_name,
    stage_create_gallery_folder,
    stage_move_gallery_assets,
    stage_rename_gallery_asset,
    stage_rename_gallery_folder,
)
from productflow_backend.application.product_images.queries import (
    GalleryAssetRecord,
    GalleryAssetSort,
    GalleryDirectoryKind,
    list_gallery_assets,
)
from productflow_backend.application.product_workflow.graph_commands import get_active_workflow_graph
from productflow_backend.application.product_workflow.graph_proposals import (
    apply_agent_graph_change_set,
    parse_agent_change_set,
    propose_graph_change_set,
)
from productflow_backend.application.product_workflow.graph_queries import project_workflow_graph
from productflow_backend.application.workflow_drafts.contracts import (
    WorkflowDraftPayloadV1,
)
from productflow_backend.domain.enums import (
    AgentConversationScope,
    AgentToolMutationStatus,
    MediaVerificationStatus,
)
from productflow_backend.domain.errors import (
    BusinessValidationError,
    ConflictError,
    NotFoundError,
)
from productflow_backend.domain.graph_catalog import graph_catalog_json
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentToolMutation,
    MediaLibraryAsset,
    Product,
    ProductAssetFolder,
    ProductImageAsset,
    WorkflowGraph,
    new_id,
)
from productflow_backend.infrastructure.storage import LocalStorage

AGENT_TOOL_CONTRACT_VERSION = 13
AGENT_ASSET_LIST_DEFAULT_LIMIT = 50
AGENT_ASSET_LIST_MAX_LIMIT = 100
AGENT_ASSET_MAX_BYTES = 20 * 1024 * 1024
AGENT_CONTEXT_MAX_BYTES = 512 * 1024
AGENT_GLOBAL_PRODUCT_LIST_MAX_LIMIT = 100
AGENT_GLOBAL_PRODUCT_INSPECT_MAX = 20
RENAME_ASSET_TOOL_NAME = "rename_product_image_asset_v1"
CREATE_FOLDER_TOOL_NAME = "create_product_image_folder_v1"
RENAME_FOLDER_TOOL_NAME = "rename_product_image_folder_v1"
MOVE_ASSETS_TOOL_NAME = "move_product_image_assets_v1"
APPLY_GRAPH_TOOL_NAME = "apply_graph_change_set_v1"
PROPOSE_GRAPH_TOOL_NAME = "propose_graph_change_set_v1"

WORKFLOW_AGENT_LIVE_GRAPH_PROMPT = """你是 ProductFlow 的商品工作流协作 Agent。
当前商品已经有一份 live schema-v3 工作流图。用户是画布的主编辑者。

执行顺序：
1. 调用 load_productflow_skill 加载 productflow-core。缺 intake 时再加载 product-intake。
2. 调用 get_product_workflow_context_v1，核对商品事实、intake、参考图、node_catalog 和 live_graph。
   node_catalog 的 config_fields 是 Inspector 与节点配置写入的唯一来源。
   live_graph 给出当前节点、连线角色和配置状态，不含完整配置正文。
3. 只有缺少会改变运行或解释结果的事实时才使用 ask_user。
4. 商品外观必须以已核验参考图为依据。确有需要时 inspect 明确选中的图片，单次最多 6 张。
   用户可以在本对话继续上传图片；本轮附件 ID 是权威输入。
5. 用户明确要求的、可逆的单次改图（改一个节点配置、连一条边、断一条边、改名）：
   调用 apply_graph_change_set_v1，且 operations 只能有一条。一次撤销能收回。
6. 多节点重构、批量删除、覆盖预设：调用 propose_graph_change_set_v1，在画布上留下未应用幽灵预览。
   不要声称已经改图。确认和取消只在画布上。
7. 用户要求运行时，使用 request_workflow_run_v1 创建待确认运行请求；不要声称已经开始运行。
8. 解释节点、检查配置缺口、对照 Catalog 可以直接做。

不可违反：
- 不得提交第二份完整拓扑去覆盖现图。
- 不得删除素材、臆造资产或商品事实，不得输出 base64、data URL、存储路径和内部 URL。
- 提案层不能运行。确认和取消提案只在画布完成。

成功条件：准确解释当前图、指出未配置或未使用节点；单次改图立即可见；批量改图先预览；用户要求时提交可确认的运行请求。
"""

GLOBAL_AGENT_SYSTEM_PROMPT = """你是 ProductFlow 的全局素材与工作流辅助 Agent。
你的作用域是整个 ProductFlow 应用，不绑定某一个商品或当前页面。

工作原则：
1. 当前页面只帮助理解“这些图片”和用户当下的工作位置。
2. 查询素材时优先使用全局素材库的列表和明确图片的 inspect；不要凭文件名猜测图片内容。
3. 你可以读取全局素材库、商品和目标商品 live graph 的有界元数据；用户明确要求查看图片时，单次最多 inspect 6 张。
4. 列表结果不代表完整业务事实。比较运行状态时，先取得明确的 product / workflow ID，再使用有界运行检查。
5. 整理、归档、改名、文件夹等素材副作用，必须先 propose_global_draft
   （draft_kind=library_organization），等待用户确认。纯查询不要调用整理工具。
6. 不能在全局会话上改某个商品的 live graph。用户要设计或修改某个商品工作流时，
   先 inspect_global_workflow_context_v1 核对 product_id 和 live_graph，
   然后请用户进入该商品工作台对话，或调用 create_product_workspace_v1 开一条新的画布会话。
7. 用户明确要求创建商品时，调用 create_product_workspace_v1。
   该工具创建 Product、live 图和归属该商品的新画布会话，不上传参考图、不写 intake、不启动运行。
   成功后请用户进入商品工作台对话上传参考图并说明需求。
8. 不要输出 base64、data URL、存储路径或内部 URL；用资产名称、来源和可验证的对象 ID 描述结果。
"""


@dataclass(frozen=True, slots=True)
class AgentAssetPage:
    items: list[dict[str, Any]]
    next_cursor: str | None


@dataclass(frozen=True, slots=True)
class AgentProductPage:
    items: list[dict[str, Any]]
    next_cursor: str | None


@dataclass(frozen=True, slots=True)
class AgentAssetContent:
    content: bytes
    media_type: str
    display_name: str


@dataclass(frozen=True, slots=True)
class AgentAssetRenamePrepared:
    asset_id: str
    expected_display_name: str
    target_display_name: str


@dataclass(frozen=True, slots=True)
class AgentFolderCreatePrepared:
    folder_id: str
    name: str


@dataclass(frozen=True, slots=True)
class AgentFolderRenamePrepared:
    folder_id: str
    expected_name: str
    target_name: str


@dataclass(frozen=True, slots=True)
class AgentAssetMovePrepared:
    moves: tuple[GalleryAssetMove, ...]
    target_folder_id: str | None


@dataclass(frozen=True, slots=True)
class AgentToolReconcileResult:
    state: str
    result: dict[str, Any] | None = None
    detail: str | None = None


AgentAssetRenameReconcileResult = AgentToolReconcileResult


def get_agent_contract(session: Session, conversation_id: str) -> dict[str, Any]:
    """发给 adapter 的工具合同。live graph 存在时改用协作 prompt，禁止再提交覆盖现图的 WorkflowDraft。"""
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    return _agent_contract_for_conversation(session, conversation)


def get_agent_task_contract(session: Session, task_id: str) -> dict[str, Any]:
    """Task 合同把 goal 和 task-specific harness_run_id 绑到这一条 Task；page context 不改写 goal。"""
    task, conversation = task_contract(session, task_id)
    contract = _agent_contract_for_conversation(session, conversation)
    contract["task_id"] = task.id
    contract["task_goal"] = task.goal
    contract["harness_run_id"] = task.harness_run_id
    return contract


def get_agent_runtime_context(
    session: Session,
    *,
    conversation_id: str,
    task_id: str | None = None,
) -> dict[str, Any]:
    """Return the bounded operational summaries needed to resume a Turn."""
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
            "workflow_draft_id": None,
            "harness_run_id": conversation.harness_run_id,
            "current_draft_version": current_revision.version if current_revision is not None else 0,
            "system_prompt": GLOBAL_AGENT_SYSTEM_PROMPT,
            "draft_kind": "global",
            "draft_schema": global_agent_draft_schema(),
            "workflow_draft_schema": {},
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
        "workflow_draft_id": conversation.workflow_draft_id,
        "harness_run_id": conversation.harness_run_id,
        "current_draft_version": 0,
        "system_prompt": WORKFLOW_AGENT_LIVE_GRAPH_PROMPT,
        "draft_kind": "workflow",
        "draft_schema": {},
        "workflow_draft_schema": {},
        "tool_contract_version": AGENT_TOOL_CONTRACT_VERSION,
        "has_live_graph": live_graph is not None,
    }


def validate_agent_workflow_draft(
    session: Session,
    *,
    conversation_id: str,
    value: dict[str, Any],
) -> WorkflowDraftPayloadV1:
    """商品路径不再接受 WorkflowDraft。"""
    from productflow_backend.application.workflow_drafts.service import PRODUCT_WORKFLOW_DRAFT_RETIRED

    del session, conversation_id, value
    raise ConflictError(PRODUCT_WORKFLOW_DRAFT_RETIRED)


def finalize_agent_product_intake(
    session: Session,
    *,
    conversation_id: str,
    selection: dict[str, Any],
    reference_asset_ids: list[str],
    idempotency_key: str,
    task_id: str | None = None,
) -> dict[str, Any]:
    """把本轮参考图与图片类型写入不可变 intake。不创建第二份商品，也不启动运行。"""
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    try:
        parsed = AgentProductSelectionV1.model_validate(selection)
    except ValidationError as exc:
        raise BusinessValidationError("图片类型选择不符合 AgentProductSelectionV1") from exc
    if len(reference_asset_ids) != len(set(reference_asset_ids)):
        raise BusinessValidationError("参考图资产不能重复")
    creation = finalize_agent_product_workspace_intake_from_assets(
        session,
        conversation_id=conversation_id,
        selection=parsed,
        reference_asset_ids=reference_asset_ids,
        idempotency_key=idempotency_key,
        task_id=task_id,
    )
    return {
        "accepted": True,
        "intake_finalized": True,
        "product_id": creation.product.id,
        "workflow_draft_id": creation.conversation.workflow_draft_id,
        "reference_asset_ids": [asset.id for asset in creation.created_assets],
        "intake": creation.product.intake_json,
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
    """有界业务事实投影。live_graph 不含完整配置正文，也不重建模型 transcript。"""
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
    workflow_draft_id: str | None = None,
) -> AgentConversation:
    """Resolve one explicit product to its latest product conversation."""
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
    if workflow_draft_id is not None:
        statement = statement.where(AgentConversation.workflow_draft_id == workflow_draft_id)
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
        "workflow_draft_id": target.workflow_draft_id,
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
    from productflow_backend.application.agent.product_intake import parse_product_intake

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


def list_agent_product_assets(
    session: Session,
    *,
    conversation_id: str,
    directory_kind: GalleryDirectoryKind = GalleryDirectoryKind.ALL,
    directory_key: str | None = None,
    query: str = "",
    sort: GalleryAssetSort = GalleryAssetSort.CREATED_DESC,
    after: str = "",
    limit: int = AGENT_ASSET_LIST_DEFAULT_LIMIT,
) -> AgentAssetPage:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    page = list_gallery_assets(
        session,
        product_id=conversation.product_id,
        directory_kind=directory_kind,
        directory_key=directory_key,
        query=query,
        sort=sort,
        after=after,
        limit=limit,
    )
    return AgentAssetPage(
        items=[agent_gallery_asset_metadata(record) for record in page.items],
        next_cursor=page.next_cursor,
    )


def inspect_agent_product_assets(
    session: Session,
    *,
    conversation_id: str,
    asset_ids: list[str],
) -> list[dict[str, Any]]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    normalized_ids = _normalize_explicit_asset_ids(asset_ids)
    assets = _load_scoped_assets(
        session,
        product_id=conversation.product_id,
        asset_ids=normalized_ids,
    )
    by_id = {asset.id: asset for asset in assets}
    return [agent_asset_metadata(by_id[asset_id]) for asset_id in normalized_ids]


def read_agent_product_asset_content(
    session: Session,
    *,
    conversation_id: str,
    asset_id: str,
    storage: LocalStorage | None = None,
) -> AgentAssetContent:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    assets = _load_scoped_assets(
        session,
        product_id=conversation.product_id,
        asset_ids=[asset_id.strip()],
    )
    asset = assets[0]
    media = asset.media_object
    if media.byte_size is None or media.byte_size > AGENT_ASSET_MAX_BYTES:
        raise BusinessValidationError("图片超过 Agent 单张图片大小上限")
    resolved_storage = storage or LocalStorage()
    path = resolved_storage.resolve(media.storage_path)
    try:
        with path.open("rb") as file:
            content = file.read(AGENT_ASSET_MAX_BYTES + 1)
    except OSError as exc:
        raise NotFoundError("商品图片文件不存在") from exc
    if len(content) > AGENT_ASSET_MAX_BYTES:
        raise BusinessValidationError("图片超过 Agent 单张图片大小上限")
    actual = inspect_image_bytes(content, expected_mime_type=media.mime_type)
    if (
        actual.byte_size != media.byte_size
        or actual.width != media.width
        or actual.height != media.height
        or actual.sha256 != media.sha256
    ):
        raise ConflictError("商品图片文件与已核验媒体元数据不一致")
    return AgentAssetContent(
        content=content,
        media_type=actual.mime_type,
        display_name=asset.display_name,
    )


def list_agent_global_media_assets(
    session: Session,
    *,
    conversation_id: str,
    query: str = "",
    cursor: str | None = None,
    limit: int = AGENT_ASSET_LIST_DEFAULT_LIMIT,
) -> AgentAssetPage:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_global_conversation(conversation)
    page = list_media_library_assets(
        session,
        limit=limit,
        cursor=cursor,
        include_archived=False,
        search=query,
    )
    return AgentAssetPage(
        items=[agent_media_library_asset_metadata(asset) for asset in page.items],
        next_cursor=page.next_cursor,
    )


def list_agent_global_products(
    session: Session,
    *,
    conversation_id: str,
    query: str = "",
    cursor: str | None = None,
    limit: int = AGENT_ASSET_LIST_DEFAULT_LIMIT,
) -> AgentProductPage:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_global_conversation(conversation)
    normalized_query = query.strip()
    if len(normalized_query) > 255:
        raise BusinessValidationError("商品搜索词不能超过 255 个字符")
    if not 1 <= limit <= AGENT_GLOBAL_PRODUCT_LIST_MAX_LIMIT:
        raise BusinessValidationError(
            f"商品分页 limit 必须在 1 到 {AGENT_GLOBAL_PRODUCT_LIST_MAX_LIMIT} 之间"
        )

    statement = select(Product).where(
        Product.name.icontains(normalized_query, autoescape=True)
        if normalized_query
        else True
    )
    if cursor:
        cursor_updated_at, cursor_product_id = _decode_global_product_cursor(
            cursor,
            query=normalized_query,
        )
        statement = statement.where(
            or_(
                Product.updated_at < cursor_updated_at,
                and_(
                    Product.updated_at == cursor_updated_at,
                    Product.id < cursor_product_id,
                ),
            )
        )
    rows = list(
        session.scalars(
            statement.order_by(Product.updated_at.desc(), Product.id.desc()).limit(limit + 1)
        ).all()
    )
    has_more = len(rows) > limit
    items = rows[:limit]
    summaries = _agent_global_product_summaries(session, items)
    next_cursor = (
        _encode_global_product_cursor(
            updated_at=items[-1].updated_at,
            product_id=items[-1].id,
            query=normalized_query,
        )
        if has_more and items
        else None
    )
    return AgentProductPage(items=summaries, next_cursor=next_cursor)


def inspect_agent_global_products(
    session: Session,
    *,
    conversation_id: str,
    product_ids: list[str],
) -> list[dict[str, Any]]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_global_conversation(conversation)
    normalized_ids = _normalize_global_product_ids(product_ids)
    products = list(
        session.scalars(select(Product).where(Product.id.in_(normalized_ids))).all()
    )
    if len(products) != len(normalized_ids):
        raise NotFoundError("部分商品不存在")
    summaries = _agent_global_product_summaries(session, products)
    by_id = {item["id"]: item for item in summaries}
    return [by_id[product_id] for product_id in normalized_ids]


def _agent_global_product_summaries(
    session: Session,
    products: list[Product],
) -> list[dict[str, Any]]:
    if not products:
        return []
    product_ids = [product.id for product in products]
    graphs = list(
        session.scalars(
            select(WorkflowGraph)
            .options(selectinload(WorkflowGraph.nodes))
            .where(
                WorkflowGraph.product_id.in_(product_ids),
                WorkflowGraph.active.is_(True),
            )
        ).unique().all()
    )
    graphs_by_product = {graph.product_id: graph for graph in graphs}
    return [_agent_global_product_summary(product, graphs_by_product.get(product.id)) for product in products]


def _agent_global_product_summary(
    product: Product,
    graph: WorkflowGraph | None,
) -> dict[str, Any]:
    return {
        "id": product.id,
        "name": product.name,
        "category": product.category,
        "updated_at": product.updated_at.isoformat(),
        "active_workflow": (
            {
                "id": graph.id,
                "title": graph.title,
                "revision": graph.revision,
                "node_count": len(graph.nodes),
            }
            if graph is not None
            else None
        ),
    }


def _normalize_global_product_ids(values: list[str]) -> list[str]:
    if not 1 <= len(values) <= AGENT_GLOBAL_PRODUCT_INSPECT_MAX:
        raise BusinessValidationError(
            f"product_ids 必须包含 1 到 {AGENT_GLOBAL_PRODUCT_INSPECT_MAX} 个商品"
        )
    normalized: list[str] = []
    seen: set[str] = set()
    for value in values:
        product_id = value.strip()
        if not product_id:
            raise BusinessValidationError("product_ids 不能包含空值")
        if product_id in seen:
            raise BusinessValidationError("product_ids 不能包含重复值")
        seen.add(product_id)
        normalized.append(product_id)
    return normalized


def _encode_global_product_cursor(*, updated_at: datetime, product_id: str, query: str) -> str:
    normalized_updated_at = (
        updated_at.replace(tzinfo=UTC)
        if updated_at.tzinfo is None
        else updated_at.astimezone(UTC)
    )
    payload = {
        "updated_at": normalized_updated_at.isoformat(),
        "product_id": product_id,
        "query": query,
    }
    encoded = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()
    return base64.urlsafe_b64encode(encoded).decode().rstrip("=")


def _decode_global_product_cursor(value: str, *, query: str) -> tuple[datetime, str]:
    try:
        padded = value + "=" * (-len(value) % 4)
        payload = json.loads(base64.urlsafe_b64decode(padded).decode())
        if payload.get("query") != query:
            raise ValueError
        updated_at = datetime.fromisoformat(payload["updated_at"])
        product_id = str(payload["product_id"]).strip()
        if updated_at.tzinfo is None or not product_id:
            raise ValueError
        return updated_at.astimezone(UTC), product_id
    except (ValueError, KeyError, TypeError, json.JSONDecodeError) as exc:
        raise BusinessValidationError("商品分页 cursor 无效或与当前查询条件不匹配") from exc


def inspect_agent_global_media_assets(
    session: Session,
    *,
    conversation_id: str,
    asset_ids: list[str],
) -> list[dict[str, Any]]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_global_conversation(conversation)
    normalized_ids = _normalize_explicit_asset_ids(asset_ids)
    assets = list(
        session.scalars(
            select(MediaLibraryAsset)
            .options(
                selectinload(MediaLibraryAsset.media_object),
                selectinload(MediaLibraryAsset.folder),
            )
            .where(
                MediaLibraryAsset.id.in_(normalized_ids),
                MediaLibraryAsset.is_archived.is_(False),
            )
        ).all()
    )
    if len(assets) != len(normalized_ids):
        raise NotFoundError("全局素材不存在或已归档")
    by_id = {asset.id: asset for asset in assets}
    for asset in assets:
        validate_media_library_asset_for_use(asset)
    return [agent_media_library_asset_metadata(by_id[asset_id]) for asset_id in normalized_ids]


def read_agent_global_media_asset_content(
    session: Session,
    *,
    conversation_id: str,
    asset_id: str,
    storage: LocalStorage | None = None,
) -> AgentAssetContent:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_global_conversation(conversation)
    asset = get_media_library_asset(session, asset_id=asset_id.strip())
    media = validate_media_library_asset_for_use(asset)
    if media.byte_size is None or media.byte_size > AGENT_ASSET_MAX_BYTES:
        raise BusinessValidationError("图片超过 Agent 单张图片大小上限")
    resolved_storage = storage or LocalStorage()
    path = resolved_storage.resolve(media.storage_path)
    try:
        with path.open("rb") as file:
            content = file.read(AGENT_ASSET_MAX_BYTES + 1)
    except OSError as exc:
        raise NotFoundError("全局素材文件不存在") from exc
    if len(content) > AGENT_ASSET_MAX_BYTES:
        raise BusinessValidationError("图片超过 Agent 单张图片大小上限")
    actual = inspect_image_bytes(content, expected_mime_type=media.mime_type)
    if (
        actual.byte_size != media.byte_size
        or actual.width != media.width
        or actual.height != media.height
        or actual.sha256 != media.sha256
    ):
        raise ConflictError("全局素材文件与已核验媒体元数据不一致")
    return AgentAssetContent(content=content, media_type=actual.mime_type, display_name=asset.display_name)


def prepare_agent_asset_rename(
    session: Session,
    *,
    conversation_id: str,
    asset_id: str,
    target_display_name: str,
) -> AgentAssetRenamePrepared:
    """prepare：只读计算目标，不写账本。"""
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    normalized_target = normalize_gallery_display_name(target_display_name)
    asset = _get_scoped_asset(session, product_id=conversation.product_id, asset_id=asset_id)
    return AgentAssetRenamePrepared(
        asset_id=asset.id,
        expected_display_name=asset.display_name,
        target_display_name=normalized_target,
    )


def apply_agent_asset_rename(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    asset_id: str,
    expected_display_name: str,
    target_display_name: str,
) -> dict[str, Any]:
    """apply：key+request hash 命中 applied 账本则回放。本函数 commit。"""
    normalized_key = _normalize_tool_idempotency_key(idempotency_key)
    prepared = _normalize_rename_prepared(
        asset_id=asset_id,
        expected_display_name=expected_display_name,
        target_display_name=target_display_name,
    )
    conversation = _get_conversation_for_update(session, conversation_id)
    prepared_json = _prepared_document(
        conversation,
        operation="rename_asset",
        before={"asset_id": prepared.asset_id, "display_name": prepared.expected_display_name},
        target={"asset_id": prepared.asset_id, "display_name": prepared.target_display_name},
    )
    request_hash = _tool_request_hash(tool_name=RENAME_ASSET_TOOL_NAME, prepared_json=prepared_json)
    replay = _replay_existing_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=RENAME_ASSET_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
    )
    if replay is not None:
        session.commit()
        return replay
    applied = prepared.expected_display_name != prepared.target_display_name
    asset = stage_rename_gallery_asset(
        session,
        product_id=conversation.product_id,
        asset_id=prepared.asset_id,
        expected_display_name=prepared.expected_display_name,
        display_name=prepared.target_display_name,
    )
    result = {
        "asset_id": asset.id,
        "display_name": prepared.target_display_name,
        "applied": applied,
    }
    return _commit_tool_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=RENAME_ASSET_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
        prepared_json=prepared_json,
        result=result,
        asset_id=asset.id,
        expected_display_name=prepared.expected_display_name,
        target_display_name=prepared.target_display_name,
    )


def reconcile_agent_asset_rename(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    asset_id: str,
    expected_display_name: str,
    target_display_name: str,
) -> AgentToolReconcileResult:
    """reconcile：不重放 mutation。账本缺失且对象仍是 before 则为 not_applied。"""
    normalized_key = _normalize_tool_idempotency_key(idempotency_key)
    prepared = _normalize_rename_prepared(
        asset_id=asset_id,
        expected_display_name=expected_display_name,
        target_display_name=target_display_name,
    )
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    prepared_json = _prepared_document(
        conversation,
        operation="rename_asset",
        before={"asset_id": prepared.asset_id, "display_name": prepared.expected_display_name},
        target={"asset_id": prepared.asset_id, "display_name": prepared.target_display_name},
    )
    request_hash = _tool_request_hash(tool_name=RENAME_ASSET_TOOL_NAME, prepared_json=prepared_json)
    ledger_result = _reconcile_from_ledger(
        session,
        conversation_id=conversation.id,
        tool_name=RENAME_ASSET_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
    )
    if ledger_result is not None:
        return ledger_result
    asset = _get_scoped_asset(session, product_id=conversation.product_id, asset_id=prepared.asset_id)
    if asset.display_name == prepared.expected_display_name:
        return AgentToolReconcileResult(
            state="not_applied",
            detail="图片仍处于副作用执行前状态",
        )
    return AgentToolReconcileResult(
        state="conflict",
        detail="图片名称既非预期旧值，且不存在匹配的副作用账本",
    )


def prepare_agent_folder_create(
    session: Session,
    *,
    conversation_id: str,
    name: str,
) -> AgentFolderCreatePrepared:
    """prepare：预分配稳定 folder_id，供 apply 幂等使用。"""
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    normalized_name = normalize_gallery_folder_name(name)
    existing = session.scalar(
        select(ProductAssetFolder.id).where(
            ProductAssetFolder.product_id == conversation.product_id,
            ProductAssetFolder.name == normalized_name,
        )
    )
    if existing is not None:
        raise ConflictError("当前商品已存在同名文件夹")
    return AgentFolderCreatePrepared(folder_id=new_id(), name=normalized_name)


def apply_agent_folder_create(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    folder_id: str,
    name: str,
) -> dict[str, Any]:
    """apply：先回放账本，再创建文件夹。本函数 commit。"""
    normalized_key = _normalize_tool_idempotency_key(idempotency_key)
    prepared = _normalize_folder_create_prepared(folder_id=folder_id, name=name)
    conversation = _get_conversation_for_update(session, conversation_id)
    prepared_json = _prepared_document(
        conversation,
        operation="create_folder",
        before={"folder_id": prepared.folder_id, "exists": False},
        target={"folder_id": prepared.folder_id, "name": prepared.name},
    )
    request_hash = _tool_request_hash(tool_name=CREATE_FOLDER_TOOL_NAME, prepared_json=prepared_json)
    replay = _replay_existing_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=CREATE_FOLDER_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
    )
    if replay is not None:
        session.commit()
        return replay
    folder = stage_create_gallery_folder(
        session,
        product_id=conversation.product_id,
        folder_id=prepared.folder_id,
        name=prepared.name,
    )
    result = {
        "folder_id": folder.id,
        "name": folder.name,
        "sort_order": folder.sort_order,
        "applied": True,
    }
    return _commit_tool_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=CREATE_FOLDER_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
        prepared_json=prepared_json,
        result=result,
    )


def reconcile_agent_folder_create(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    folder_id: str,
    name: str,
) -> AgentToolReconcileResult:
    """reconcile：不重放创建命令。"""
    normalized_key = _normalize_tool_idempotency_key(idempotency_key)
    prepared = _normalize_folder_create_prepared(folder_id=folder_id, name=name)
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    prepared_json = _prepared_document(
        conversation,
        operation="create_folder",
        before={"folder_id": prepared.folder_id, "exists": False},
        target={"folder_id": prepared.folder_id, "name": prepared.name},
    )
    request_hash = _tool_request_hash(tool_name=CREATE_FOLDER_TOOL_NAME, prepared_json=prepared_json)
    ledger_result = _reconcile_from_ledger(
        session,
        conversation_id=conversation.id,
        tool_name=CREATE_FOLDER_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
    )
    if ledger_result is not None:
        return ledger_result
    folder = session.scalar(
        select(ProductAssetFolder).where(
            ProductAssetFolder.id == prepared.folder_id,
            ProductAssetFolder.product_id == conversation.product_id,
        )
    )
    if folder is None:
        return AgentToolReconcileResult(state="not_applied", detail="文件夹尚未创建")
    return AgentToolReconcileResult(state="conflict", detail="稳定文件夹 ID 已存在但缺少匹配账本")


def prepare_agent_folder_rename(
    session: Session,
    *,
    conversation_id: str,
    folder_id: str,
    target_name: str,
) -> AgentFolderRenamePrepared:
    """prepare：只读计算文件夹改名目标。"""
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    folder = _get_scoped_folder(session, product_id=conversation.product_id, folder_id=folder_id)
    return AgentFolderRenamePrepared(
        folder_id=folder.id,
        expected_name=folder.name,
        target_name=normalize_gallery_folder_name(target_name),
    )


def apply_agent_folder_rename(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    folder_id: str,
    expected_name: str,
    target_name: str,
) -> dict[str, Any]:
    """apply：账本命中则回放。本函数 commit。"""
    normalized_key = _normalize_tool_idempotency_key(idempotency_key)
    prepared = _normalize_folder_rename_prepared(
        folder_id=folder_id,
        expected_name=expected_name,
        target_name=target_name,
    )
    conversation = _get_conversation_for_update(session, conversation_id)
    prepared_json = _prepared_document(
        conversation,
        operation="rename_folder",
        before={"folder_id": prepared.folder_id, "name": prepared.expected_name},
        target={"folder_id": prepared.folder_id, "name": prepared.target_name},
    )
    request_hash = _tool_request_hash(tool_name=RENAME_FOLDER_TOOL_NAME, prepared_json=prepared_json)
    replay = _replay_existing_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=RENAME_FOLDER_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
    )
    if replay is not None:
        session.commit()
        return replay
    folder = stage_rename_gallery_folder(
        session,
        product_id=conversation.product_id,
        folder_id=prepared.folder_id,
        expected_name=prepared.expected_name,
        name=prepared.target_name,
    )
    result = {
        "folder_id": folder.id,
        "name": folder.name,
        "applied": prepared.expected_name != prepared.target_name,
    }
    return _commit_tool_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=RENAME_FOLDER_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
        prepared_json=prepared_json,
        result=result,
    )


def reconcile_agent_folder_rename(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    folder_id: str,
    expected_name: str,
    target_name: str,
) -> AgentToolReconcileResult:
    """reconcile：不重放改名。"""
    normalized_key = _normalize_tool_idempotency_key(idempotency_key)
    prepared = _normalize_folder_rename_prepared(
        folder_id=folder_id,
        expected_name=expected_name,
        target_name=target_name,
    )
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    prepared_json = _prepared_document(
        conversation,
        operation="rename_folder",
        before={"folder_id": prepared.folder_id, "name": prepared.expected_name},
        target={"folder_id": prepared.folder_id, "name": prepared.target_name},
    )
    request_hash = _tool_request_hash(tool_name=RENAME_FOLDER_TOOL_NAME, prepared_json=prepared_json)
    ledger_result = _reconcile_from_ledger(
        session,
        conversation_id=conversation.id,
        tool_name=RENAME_FOLDER_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
    )
    if ledger_result is not None:
        return ledger_result
    folder = _get_scoped_folder(
        session,
        product_id=conversation.product_id,
        folder_id=prepared.folder_id,
    )
    if folder.name == prepared.expected_name:
        return AgentToolReconcileResult(state="not_applied", detail="文件夹仍处于改名前状态")
    return AgentToolReconcileResult(state="conflict", detail="文件夹名称已变化且缺少匹配账本")


def prepare_agent_asset_move(
    session: Session,
    *,
    conversation_id: str,
    asset_ids: list[str],
    target_folder_id: str | None,
) -> AgentAssetMovePrepared:
    """prepare：捕获移动前 folder_id，作为 apply 的 expected 状态。"""
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    normalized_ids = _normalize_move_asset_ids(asset_ids)
    normalized_target = _normalize_optional_id(target_folder_id, field_name="目标文件夹 ID")
    if normalized_target is not None:
        _get_scoped_folder(
            session,
            product_id=conversation.product_id,
            folder_id=normalized_target,
        )
    assets = _load_scoped_asset_metadata(
        session,
        product_id=conversation.product_id,
        asset_ids=normalized_ids,
    )
    by_id = {asset.id: asset for asset in assets}
    return AgentAssetMovePrepared(
        moves=tuple(
            GalleryAssetMove(asset_id=asset_id, expected_folder_id=by_id[asset_id].user_folder_id)
            for asset_id in normalized_ids
        ),
        target_folder_id=normalized_target,
    )


def apply_agent_asset_move(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    moves: list[GalleryAssetMove],
    target_folder_id: str | None,
) -> dict[str, Any]:
    """apply：账本命中则回放。本函数 commit。"""
    normalized_key = _normalize_tool_idempotency_key(idempotency_key)
    prepared = _normalize_asset_move_prepared(moves=moves, target_folder_id=target_folder_id)
    conversation = _get_conversation_for_update(session, conversation_id)
    before = {
        "moves": [
            {"asset_id": move.asset_id, "folder_id": move.expected_folder_id}
            for move in prepared.moves
        ]
    }
    target = {
        "asset_ids": [move.asset_id for move in prepared.moves],
        "folder_id": prepared.target_folder_id,
    }
    prepared_json = _prepared_document(conversation, operation="move_assets", before=before, target=target)
    request_hash = _tool_request_hash(tool_name=MOVE_ASSETS_TOOL_NAME, prepared_json=prepared_json)
    replay = _replay_existing_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=MOVE_ASSETS_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
    )
    if replay is not None:
        session.commit()
        return replay
    assets = stage_move_gallery_assets(
        session,
        product_id=conversation.product_id,
        moves=list(prepared.moves),
        folder_id=prepared.target_folder_id,
    )
    result = {
        "asset_ids": [asset.id for asset in assets],
        "folder_id": prepared.target_folder_id,
        "applied": any(
            move.expected_folder_id != prepared.target_folder_id for move in prepared.moves
        ),
    }
    return _commit_tool_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=MOVE_ASSETS_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
        prepared_json=prepared_json,
        result=result,
    )


def reconcile_agent_asset_move(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    moves: list[GalleryAssetMove],
    target_folder_id: str | None,
) -> AgentToolReconcileResult:
    """reconcile：不重放移动。"""
    normalized_key = _normalize_tool_idempotency_key(idempotency_key)
    prepared = _normalize_asset_move_prepared(moves=moves, target_folder_id=target_folder_id)
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    before = {
        "moves": [
            {"asset_id": move.asset_id, "folder_id": move.expected_folder_id}
            for move in prepared.moves
        ]
    }
    target = {
        "asset_ids": [move.asset_id for move in prepared.moves],
        "folder_id": prepared.target_folder_id,
    }
    prepared_json = _prepared_document(conversation, operation="move_assets", before=before, target=target)
    request_hash = _tool_request_hash(tool_name=MOVE_ASSETS_TOOL_NAME, prepared_json=prepared_json)
    ledger_result = _reconcile_from_ledger(
        session,
        conversation_id=conversation.id,
        tool_name=MOVE_ASSETS_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
    )
    if ledger_result is not None:
        return ledger_result
    assets = _load_scoped_asset_metadata(
        session,
        product_id=conversation.product_id,
        asset_ids=[move.asset_id for move in prepared.moves],
    )
    current = {asset.id: asset.user_folder_id for asset in assets}
    if all(current[move.asset_id] == move.expected_folder_id for move in prepared.moves):
        return AgentToolReconcileResult(state="not_applied", detail="图片仍处于移动前目录")
    return AgentToolReconcileResult(state="conflict", detail="图片目录已变化且缺少匹配账本")


def agent_asset_metadata(asset: ProductImageAsset) -> dict[str, Any]:
    media = asset.media_object
    return {
        "id": asset.id,
        "display_name": asset.display_name,
        "original_filename": asset.original_filename,
        "origin_type": asset.origin_type.value,
        "image_type_key": asset.image_type_key,
        "image_type_title": None,
        "user_folder_id": asset.user_folder_id,
        "user_folder_name": asset.user_folder.name if asset.user_folder is not None else None,
        "mime_type": media.mime_type,
        "byte_size": media.byte_size,
        "width": media.width,
        "height": media.height,
        "verification_status": media.verification_status.value,
        "parent_asset_id": asset.parent_asset_id,
        "generation": None,
        "created_at": asset.created_at.isoformat(),
    }


def agent_gallery_asset_metadata(record: GalleryAssetRecord) -> dict[str, Any]:
    metadata = agent_asset_metadata(record.asset)
    metadata["image_type_title"] = record.image_type_title
    metadata["generation"] = (
        {
            "workflow_id": record.generation.workflow_id,
            "node_id": record.generation.node_id,
            "node_run_id": record.generation.node_run_id,
            "prompt_artifact_version_id": record.generation.prompt_artifact_version_id,
            "visual_system_version_id": record.generation.visual_system_version_id,
        }
        if record.generation is not None
        else None
    )
    return metadata


def agent_media_library_asset_metadata(asset: MediaLibraryAsset) -> dict[str, Any]:
    media = asset.media_object
    return {
        "id": asset.id,
        "display_name": asset.display_name,
        "original_filename": asset.original_filename,
        "origin_type": asset.source_type,
        "image_type_key": None,
        "image_type_title": None,
        "user_folder_id": asset.folder_id,
        "user_folder_name": asset.folder.name if asset.folder is not None else None,
        "mime_type": media.mime_type,
        "byte_size": media.byte_size,
        "width": media.width,
        "height": media.height,
        "verification_status": media.verification_status.value,
        "parent_asset_id": None,
        "generation": None,
        "created_at": asset.created_at.isoformat(),
    }


def _require_product_conversation(conversation: AgentConversation) -> None:
    if (
        conversation.scope_type != AgentConversationScope.PRODUCT_WORKFLOW
        or conversation.product_id is None
    ):
        raise ConflictError("当前 Agent conversation 不是商品工作流作用域")


def _require_global_conversation(conversation: AgentConversation) -> None:
    if conversation.scope_type != AgentConversationScope.GLOBAL:
        raise ConflictError("当前 Agent conversation 不是全局作用域")


def _load_scoped_assets(
    session: Session,
    *,
    product_id: str,
    asset_ids: list[str],
) -> list[ProductImageAsset]:
    normalized_ids = _normalize_explicit_asset_ids(asset_ids)
    assets = list(
        session.scalars(
            select(ProductImageAsset)
            .options(
                selectinload(ProductImageAsset.media_object),
                selectinload(ProductImageAsset.user_folder),
            )
            .where(
                ProductImageAsset.product_id == product_id,
                ProductImageAsset.id.in_(normalized_ids),
            )
        ).all()
    )
    if len(assets) != len(normalized_ids):
        raise NotFoundError("商品图片不存在")
    if any(asset.media_object.verification_status != MediaVerificationStatus.VERIFIED for asset in assets):
        raise BusinessValidationError("Agent 只能读取已通过媒体核验的商品图片")
    return assets


def _load_scoped_asset_metadata(
    session: Session,
    *,
    product_id: str,
    asset_ids: list[str],
) -> list[ProductImageAsset]:
    assets = list(
        session.scalars(
            select(ProductImageAsset)
            .options(
                selectinload(ProductImageAsset.media_object),
                selectinload(ProductImageAsset.user_folder),
            )
            .where(
                ProductImageAsset.product_id == product_id,
                ProductImageAsset.id.in_(asset_ids),
            )
        )
    )
    if len(assets) != len(asset_ids):
        raise NotFoundError("商品图片不存在")
    return assets


def _get_scoped_asset(
    session: Session,
    *,
    product_id: str,
    asset_id: str,
) -> ProductImageAsset:
    normalized_id = _normalize_required_id(asset_id, field_name="商品图片 ID")
    asset = session.scalar(
        select(ProductImageAsset)
        .options(selectinload(ProductImageAsset.media_object), selectinload(ProductImageAsset.user_folder))
        .where(ProductImageAsset.id == normalized_id, ProductImageAsset.product_id == product_id)
    )
    if asset is None:
        raise NotFoundError("商品图片不存在")
    return asset


def _get_scoped_folder(
    session: Session,
    *,
    product_id: str,
    folder_id: str,
) -> ProductAssetFolder:
    normalized_id = _normalize_required_id(folder_id, field_name="文件夹 ID")
    folder = session.scalar(
        select(ProductAssetFolder)
        .where(ProductAssetFolder.id == normalized_id, ProductAssetFolder.product_id == product_id)
    )
    if folder is None:
        raise NotFoundError("商品图片文件夹不存在")
    return folder


def _get_conversation_for_update(session: Session, conversation_id: str) -> AgentConversation:
    conversation = session.scalar(
        select(AgentConversation).where(AgentConversation.id == conversation_id).with_for_update()
    )
    if conversation is None:
        raise NotFoundError("Agent conversation 不存在")
    return conversation


def _normalize_explicit_asset_ids(values: list[str]) -> list[str]:
    normalized = [value.strip() for value in values]
    if not normalized or any(not value for value in normalized):
        raise BusinessValidationError("请明确提供要查看的商品图片 ID")
    if len(normalized) > AGENT_MAX_INPUT_ASSETS:
        raise BusinessValidationError(f"单次最多查看 {AGENT_MAX_INPUT_ASSETS} 张商品图片")
    if len(set(normalized)) != len(normalized):
        raise BusinessValidationError("商品图片 ID 不能重复")
    return normalized


def _normalize_tool_idempotency_key(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("工具 idempotency key 不能为空")
    if len(normalized.encode("utf-8")) > 200:
        raise BusinessValidationError("工具 idempotency key 不能超过 200 bytes")
    return normalized


def _normalize_required_id(value: str, *, field_name: str) -> str:
    normalized = value.strip()
    if not normalized or len(normalized) > 36:
        raise BusinessValidationError(f"{field_name}无效")
    return normalized


def _normalize_optional_id(value: str | None, *, field_name: str) -> str | None:
    if value is None:
        return None
    return _normalize_required_id(value, field_name=field_name)


def _normalize_rename_prepared(
    *,
    asset_id: str,
    expected_display_name: str,
    target_display_name: str,
) -> AgentAssetRenamePrepared:
    normalized_asset_id = asset_id.strip()
    if not normalized_asset_id:
        raise BusinessValidationError("商品图片 ID 不能为空")
    return AgentAssetRenamePrepared(
        asset_id=_normalize_required_id(normalized_asset_id, field_name="商品图片 ID"),
        expected_display_name=normalize_gallery_display_name(expected_display_name),
        target_display_name=normalize_gallery_display_name(target_display_name),
    )


def _normalize_folder_create_prepared(*, folder_id: str, name: str) -> AgentFolderCreatePrepared:
    return AgentFolderCreatePrepared(
        folder_id=_normalize_required_id(folder_id, field_name="文件夹 ID"),
        name=normalize_gallery_folder_name(name),
    )


def _normalize_folder_rename_prepared(
    *,
    folder_id: str,
    expected_name: str,
    target_name: str,
) -> AgentFolderRenamePrepared:
    return AgentFolderRenamePrepared(
        folder_id=_normalize_required_id(folder_id, field_name="文件夹 ID"),
        expected_name=normalize_gallery_folder_name(expected_name),
        target_name=normalize_gallery_folder_name(target_name),
    )


def _normalize_move_asset_ids(values: list[str]) -> list[str]:
    if not 1 <= len(values) <= GALLERY_MOVE_MAX_ASSETS:
        raise BusinessValidationError(f"单次必须移动 1 到 {GALLERY_MOVE_MAX_ASSETS} 张图片")
    normalized = [_normalize_required_id(value, field_name="商品图片 ID") for value in values]
    if len(set(normalized)) != len(normalized):
        raise BusinessValidationError("移动列表不能包含重复图片 ID")
    return normalized


def _normalize_asset_move_prepared(
    *,
    moves: list[GalleryAssetMove],
    target_folder_id: str | None,
) -> AgentAssetMovePrepared:
    normalized_ids = _normalize_move_asset_ids([move.asset_id for move in moves])
    normalized_moves = tuple(
        GalleryAssetMove(
            asset_id=asset_id,
            expected_folder_id=_normalize_optional_id(
                move.expected_folder_id,
                field_name="expected folder ID",
            ),
        )
        for asset_id, move in zip(normalized_ids, moves, strict=True)
    )
    return AgentAssetMovePrepared(
        moves=normalized_moves,
        target_folder_id=_normalize_optional_id(target_folder_id, field_name="目标文件夹 ID"),
    )


def _prepared_document(
    conversation: AgentConversation,
    *,
    operation: str,
    before: dict[str, Any],
    target: dict[str, Any],
) -> dict[str, Any]:
    return {
        "schema_version": 1,
        "operation": operation,
        "scope": {
            "conversation_id": conversation.id,
            "product_id": conversation.product_id,
        },
        "before": before,
        "target": target,
    }


def _tool_request_hash(*, tool_name: str, prepared_json: dict[str, Any]) -> str:
    payload = {"tool_name": tool_name, "prepared": prepared_json}
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(encoded).hexdigest()


def _get_tool_mutation(
    session: Session,
    *,
    conversation_id: str,
    tool_name: str,
    idempotency_key: str,
) -> AgentToolMutation | None:
    return session.scalar(
        select(AgentToolMutation).where(
            AgentToolMutation.conversation_id == conversation_id,
            AgentToolMutation.tool_name == tool_name,
            AgentToolMutation.idempotency_key == idempotency_key,
        )
    )


def _replay_existing_mutation(
    session: Session,
    *,
    conversation_id: str,
    tool_name: str,
    idempotency_key: str,
    request_hash: str,
) -> dict[str, Any] | None:
    """同一 key 必须绑定同一 request hash，否则冲突而不是覆盖。"""
    mutation = _get_tool_mutation(
        session,
        conversation_id=conversation_id,
        tool_name=tool_name,
        idempotency_key=idempotency_key,
    )
    if mutation is None:
        return None
    if mutation.request_hash != request_hash:
        raise ConflictError("同一工具 idempotency key 不能提交不同请求")
    if mutation.status != AgentToolMutationStatus.APPLIED or mutation.result_json is None:
        raise ConflictError("工具副作用账本未处于 applied 状态")
    return dict(mutation.result_json)


def _reconcile_from_ledger(
    session: Session,
    *,
    conversation_id: str,
    tool_name: str,
    idempotency_key: str,
    request_hash: str,
) -> AgentToolReconcileResult | None:
    """账本 UNKNOWN 或未终态时保持 unknown，不能降成 failed。"""
    mutation = _get_tool_mutation(
        session,
        conversation_id=conversation_id,
        tool_name=tool_name,
        idempotency_key=idempotency_key,
    )
    if mutation is None:
        return None
    if mutation.request_hash != request_hash:
        return AgentToolReconcileResult(state="conflict", detail="工具幂等键已绑定其他请求")
    if mutation.status == AgentToolMutationStatus.APPLIED and mutation.result_json is not None:
        return AgentToolReconcileResult(
            state="applied",
            result=dict(mutation.result_json),
            detail="工具副作用已提交",
        )
    if mutation.status == AgentToolMutationStatus.UNKNOWN:
        return AgentToolReconcileResult(state="unknown", detail="副作用结果仍不明确")
    # 账本存在但未 applied，仍无法证明业务对象状态。
    return AgentToolReconcileResult(state="unknown", detail="副作用账本尚未形成终态")


def _commit_tool_mutation(
    session: Session,
    *,
    conversation_id: str,
    tool_name: str,
    idempotency_key: str,
    request_hash: str,
    prepared_json: dict[str, Any],
    result: dict[str, Any],
    asset_id: str | None = None,
    expected_display_name: str | None = None,
    target_display_name: str | None = None,
) -> dict[str, Any]:
    """写入 applied 账本并 commit。并发冲突时只回放已 applied 的同一 hash。"""
    session.add(
        AgentToolMutation(
            conversation_id=conversation_id,
            tool_name=tool_name,
            idempotency_key=idempotency_key,
            request_hash=request_hash,
            asset_id=asset_id,
            expected_display_name=expected_display_name,
            target_display_name=target_display_name,
            prepared_json=prepared_json,
            status=AgentToolMutationStatus.APPLIED,
            result_json=result,
        )
    )
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        existing = _get_tool_mutation(
            session,
            conversation_id=conversation_id,
            tool_name=tool_name,
            idempotency_key=idempotency_key,
        )
        if (
            existing is not None
            and existing.request_hash == request_hash
            and existing.status == AgentToolMutationStatus.APPLIED
            and existing.result_json is not None
        ):
            return dict(existing.result_json)
        raise ConflictError("工具请求与现有对象或副作用账本冲突") from None
    return result


def apply_agent_graph_change_set_tool(
    session: Session,
    *,
    conversation_id: str,
    change_set: dict[str, Any],
    idempotency_key: str,
) -> dict[str, Any]:
    """立即应用单次可逆改图。账本命中则回放。本函数 commit。"""
    normalized_key = _normalize_tool_idempotency_key(idempotency_key)
    conversation = _get_conversation_for_update(session, conversation_id)
    _require_product_conversation(conversation)
    parsed = parse_agent_change_set(change_set)
    prepared_json = _prepared_document(
        conversation,
        operation=APPLY_GRAPH_TOOL_NAME,
        before={"base_graph_revision": parsed.base_graph_revision},
        target=parsed.model_dump(mode="json"),
    )
    request_hash = _tool_request_hash(tool_name=APPLY_GRAPH_TOOL_NAME, prepared_json=prepared_json)
    replay = _replay_existing_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=APPLY_GRAPH_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
    )
    if replay is not None:
        session.commit()
        return replay
    graph = apply_agent_graph_change_set(
        session,
        conversation_id=conversation_id,
        change_set=parsed,
        commit=False,
    )
    result = {
        "accepted": True,
        "applied": True,
        "graph_id": graph.id,
        "revision": graph.revision,
        "summary": parsed.summary,
    }
    return _commit_tool_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=APPLY_GRAPH_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
        prepared_json=prepared_json,
        result=result,
    )


def propose_agent_graph_change_set_tool(
    session: Session,
    *,
    conversation_id: str,
    change_set: dict[str, Any],
    idempotency_key: str,
) -> dict[str, Any]:
    """只留下未应用幽灵预览，不改 live graph。本函数 commit。"""
    normalized_key = _normalize_tool_idempotency_key(idempotency_key)
    conversation = _get_conversation_for_update(session, conversation_id)
    _require_product_conversation(conversation)
    parsed = parse_agent_change_set(change_set)
    prepared_json = _prepared_document(
        conversation,
        operation=PROPOSE_GRAPH_TOOL_NAME,
        before={"base_graph_revision": parsed.base_graph_revision},
        target=parsed.model_dump(mode="json"),
    )
    request_hash = _tool_request_hash(tool_name=PROPOSE_GRAPH_TOOL_NAME, prepared_json=prepared_json)
    replay = _replay_existing_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=PROPOSE_GRAPH_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
    )
    if replay is not None:
        session.commit()
        return replay
    proposal = propose_graph_change_set(
        session,
        conversation_id=conversation_id,
        change_set=parsed,
        commit=False,
    )
    result = {
        "accepted": True,
        "applied": False,
        "pending_confirmation": True,
        "proposal_id": proposal.id,
        "graph_id": proposal.graph_id,
        "base_graph_revision": proposal.base_graph_revision,
        "summary": proposal.summary,
    }
    return _commit_tool_mutation(
        session,
        conversation_id=conversation.id,
        tool_name=PROPOSE_GRAPH_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
        prepared_json=prepared_json,
        result=result,
    )


def reconcile_agent_graph_change_set_tool(
    session: Session,
    *,
    conversation_id: str,
    change_set: dict[str, Any],
    idempotency_key: str,
    tool_name: str,
) -> AgentToolReconcileResult:
    """图变更对账：不重放 apply/propose。"""
    if tool_name not in {APPLY_GRAPH_TOOL_NAME, PROPOSE_GRAPH_TOOL_NAME}:
        raise BusinessValidationError("不支持的图变更对账工具")
    normalized_key = _normalize_tool_idempotency_key(idempotency_key)
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    parsed = parse_agent_change_set(change_set)
    prepared_json = _prepared_document(
        conversation,
        operation=tool_name,
        before={"base_graph_revision": parsed.base_graph_revision},
        target=parsed.model_dump(mode="json"),
    )
    request_hash = _tool_request_hash(tool_name=tool_name, prepared_json=prepared_json)
    ledger_result = _reconcile_from_ledger(
        session,
        conversation_id=conversation.id,
        tool_name=tool_name,
        idempotency_key=normalized_key,
        request_hash=request_hash,
    )
    if ledger_result is not None:
        return ledger_result
    return AgentToolReconcileResult(state="not_applied", detail="图变更副作用尚未提交")


__all__ = [
    "AGENT_ASSET_LIST_DEFAULT_LIMIT",
    "AGENT_ASSET_LIST_MAX_LIMIT",
    "AGENT_GLOBAL_PRODUCT_INSPECT_MAX",
    "AGENT_GLOBAL_PRODUCT_LIST_MAX_LIMIT",
    "AGENT_TOOL_CONTRACT_VERSION",
    "CREATE_FOLDER_TOOL_NAME",
    "MOVE_ASSETS_TOOL_NAME",
    "RENAME_FOLDER_TOOL_NAME",
    "RENAME_ASSET_TOOL_NAME",
    "AgentAssetContent",
    "AgentAssetPage",
    "AgentProductPage",
    "AgentAssetRenamePrepared",
    "AgentAssetRenameReconcileResult",
    "AgentAssetMovePrepared",
    "AgentFolderCreatePrepared",
    "AgentFolderRenamePrepared",
    "AgentToolReconcileResult",
    "agent_asset_metadata",
    "agent_gallery_asset_metadata",
    "agent_media_library_asset_metadata",
    "apply_agent_asset_move",
    "apply_agent_asset_rename",
    "apply_agent_folder_create",
    "apply_agent_folder_rename",
    "finalize_agent_product_intake",
    "get_agent_contract",
    "get_agent_runtime_context",
    "get_agent_task_contract",
    "get_agent_product_context",
    "get_agent_global_workflow_context",
    "get_agent_global_workflow_target",
    "inspect_agent_global_media_assets",
    "inspect_agent_global_products",
    "inspect_agent_product_assets",
    "list_agent_global_media_assets",
    "list_agent_global_products",
    "list_agent_product_assets",
    "prepare_agent_asset_move",
    "prepare_agent_asset_rename",
    "prepare_agent_folder_create",
    "prepare_agent_folder_rename",
    "read_agent_product_asset_content",
    "read_agent_global_media_asset_content",
    "reconcile_agent_asset_move",
    "reconcile_agent_asset_rename",
    "reconcile_agent_folder_create",
    "reconcile_agent_folder_rename",
    "validate_agent_workflow_draft",
    "validate_agent_library_organization_draft",
    "validate_agent_global_draft",
    "APPLY_GRAPH_TOOL_NAME",
    "PROPOSE_GRAPH_TOOL_NAME",
    "apply_agent_graph_change_set_tool",
    "propose_agent_graph_change_set_tool",
    "reconcile_agent_graph_change_set_tool",
]
