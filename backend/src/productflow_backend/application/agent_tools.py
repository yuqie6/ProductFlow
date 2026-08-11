from __future__ import annotations

import base64
import hashlib
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

from sqlalchemy import and_, or_, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent_conversations import (
    AGENT_MAX_INPUT_ASSETS,
    get_agent_conversation_by_id_or_raise,
)
from productflow_backend.application.media_assets import inspect_image_bytes
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.contracts import WorkflowDraftPayloadV1
from productflow_backend.domain.enums import AgentToolMutationStatus, MediaVerificationStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentToolMutation,
    Product,
    ProductImageAsset,
)
from productflow_backend.infrastructure.storage import LocalStorage

AGENT_TOOL_CONTRACT_VERSION = 1
AGENT_ASSET_LIST_DEFAULT_LIMIT = 50
AGENT_ASSET_LIST_MAX_LIMIT = 100
AGENT_ASSET_MAX_BYTES = 20 * 1024 * 1024
AGENT_CONTEXT_MAX_BYTES = 512 * 1024
RENAME_ASSET_TOOL_NAME = "rename_product_image_asset_v1"

WORKFLOW_AGENT_SYSTEM_PROMPT = """你是 ProductFlow 的商品工作流设计 Agent。
你的作用域固定为当前 conversation、商品和 WorkflowDraft。

工作原则：
1. 先读取商品与草案上下文，核对用户已选图片类型、各自生成数量、商品事实、视觉体系和参考图。
2. 信息不足时使用 ask_user 提出直接影响成图或文案的少量问题，例如价格、风格、文字语种和禁用内容；不要重复询问已有事实。
3. 商品外观必须以用户提供的已核验参考图为依据。
   图库列表只提供元数据；仅在确有需要时 inspect 明确选中的图片，单次最多 6 张。
4. 可以为整理而修改资产显示名，不得删除素材、修改封面、臆造 Logo 或逐节点写入画布。
5. 用户确认需求后，提交完整的 propose_workflow_draft artifact。
   每个图片类型由一个提示词计划表达；同类型数量用于候选抽取，不拆成多个提示词节点。
   不同角度或不同信息任务应建为不同图片类型。
6. 最终草案必须满足工具提供的 JSON Schema，并引用当前商品真实存在的资产 ID。
   不要在文本中输出 base64、data URL、存储路径或内部 URL。
"""


@dataclass(frozen=True, slots=True)
class AgentAssetPage:
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
class AgentAssetRenameReconcileResult:
    state: str
    result: dict[str, Any] | None = None
    detail: str | None = None


def get_agent_contract(session: Session, conversation_id: str) -> dict[str, Any]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    draft = conversation.workflow_draft
    current_revision = draft.current_revision
    if current_revision is None:
        raise ConflictError("WorkflowDraft 缺少 current revision")
    return {
        "schema_version": 1,
        "conversation_id": conversation.id,
        "product_id": conversation.product_id,
        "workflow_draft_id": conversation.workflow_draft_id,
        "harness_run_id": conversation.harness_run_id,
        "current_draft_version": current_revision.version,
        "system_prompt": WORKFLOW_AGENT_SYSTEM_PROMPT,
        "workflow_draft_schema": WorkflowDraftPayloadV1.model_json_schema(),
        "tool_contract_version": AGENT_TOOL_CONTRACT_VERSION,
    }


def get_agent_product_context(session: Session, conversation_id: str) -> dict[str, Any]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    product = session.scalar(
        select(Product)
        .options(selectinload(Product.current_fact_set_version))
        .where(Product.id == conversation.product_id)
    )
    if product is None:
        raise NotFoundError("商品不存在")
    draft = conversation.workflow_draft
    revision = draft.current_revision
    if revision is None:
        raise ConflictError("WorkflowDraft 缺少 current revision")
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
        "workflow_draft": {
            "id": draft.id,
            "status": draft.status.value,
            "version": revision.version,
            "payload": revision.payload_json,
        },
    }
    encoded = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()
    if len(encoded) > AGENT_CONTEXT_MAX_BYTES:
        raise ConflictError("商品与 WorkflowDraft 上下文超过 Agent 工具输出上限")
    return payload


def list_agent_product_assets(
    session: Session,
    *,
    conversation_id: str,
    query: str = "",
    after: str = "",
    limit: int = AGENT_ASSET_LIST_DEFAULT_LIMIT,
) -> AgentAssetPage:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    if limit < 1 or limit > AGENT_ASSET_LIST_MAX_LIMIT:
        raise BusinessValidationError(
            f"图库分页 limit 必须在 1 到 {AGENT_ASSET_LIST_MAX_LIMIT} 之间"
        )
    normalized_query = query.strip()
    if len(normalized_query) > 255:
        raise BusinessValidationError("图库搜索词不能超过 255 个字符")
    cursor = _decode_asset_cursor(after) if after.strip() else None
    statement = (
        select(ProductImageAsset)
        .options(selectinload(ProductImageAsset.media_object))
        .where(ProductImageAsset.product_id == conversation.product_id)
    )
    if normalized_query:
        statement = statement.where(ProductImageAsset.display_name.ilike(f"%{normalized_query}%"))
    if cursor is not None:
        statement = statement.where(
            or_(
                ProductImageAsset.created_at > cursor[0],
                and_(
                    ProductImageAsset.created_at == cursor[0],
                    ProductImageAsset.id > cursor[1],
                ),
            )
        )
    assets = list(
        session.scalars(
            statement.order_by(ProductImageAsset.created_at.asc(), ProductImageAsset.id.asc()).limit(limit + 1)
        ).all()
    )
    has_more = len(assets) > limit
    page_assets = assets[:limit]
    return AgentAssetPage(
        items=[agent_asset_metadata(asset) for asset in page_assets],
        next_cursor=(
            _encode_asset_cursor(page_assets[-1].created_at, page_assets[-1].id)
            if has_more and page_assets
            else None
        ),
    )


def inspect_agent_product_assets(
    session: Session,
    *,
    conversation_id: str,
    asset_ids: list[str],
) -> list[dict[str, Any]]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
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


def prepare_agent_asset_rename(
    session: Session,
    *,
    conversation_id: str,
    asset_id: str,
    target_display_name: str,
) -> AgentAssetRenamePrepared:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    normalized_target = _normalize_display_name(target_display_name)
    assets = _load_scoped_assets(
        session,
        product_id=conversation.product_id,
        asset_ids=[asset_id.strip()],
    )
    return AgentAssetRenamePrepared(
        asset_id=assets[0].id,
        expected_display_name=assets[0].display_name,
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
    normalized_key = _normalize_tool_idempotency_key(idempotency_key)
    prepared = _normalize_rename_prepared(
        asset_id=asset_id,
        expected_display_name=expected_display_name,
        target_display_name=target_display_name,
    )
    request_hash = _rename_request_hash(conversation_id=conversation_id, prepared=prepared)
    conversation = _get_conversation_for_update(session, conversation_id)
    existing = session.scalar(
        select(AgentToolMutation).where(
            AgentToolMutation.conversation_id == conversation.id,
            AgentToolMutation.tool_name == RENAME_ASSET_TOOL_NAME,
            AgentToolMutation.idempotency_key == normalized_key,
        )
    )
    if existing is not None:
        if existing.request_hash != request_hash:
            raise ConflictError("同一工具 idempotency key 不能提交不同的重命名请求")
        if existing.status != AgentToolMutationStatus.APPLIED or existing.result_json is None:
            raise ConflictError("工具副作用账本未处于 applied 状态")
        session.commit()
        return dict(existing.result_json)

    asset = _get_scoped_asset_for_update(
        session,
        product_id=conversation.product_id,
        asset_id=prepared.asset_id,
    )
    if asset.display_name not in {prepared.expected_display_name, prepared.target_display_name}:
        raise ConflictError("图片显示名已被其他操作修改")
    applied = asset.display_name != prepared.target_display_name
    if applied:
        asset.display_name = prepared.target_display_name
        asset.updated_at = now_utc()
    result = {
        "asset_id": asset.id,
        "display_name": prepared.target_display_name,
        "applied": applied,
    }
    mutation = AgentToolMutation(
        conversation_id=conversation.id,
        tool_name=RENAME_ASSET_TOOL_NAME,
        idempotency_key=normalized_key,
        request_hash=request_hash,
        asset_id=asset.id,
        expected_display_name=prepared.expected_display_name,
        target_display_name=prepared.target_display_name,
        status=AgentToolMutationStatus.APPLIED,
        result_json=result,
    )
    session.add(mutation)
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        existing = session.scalar(
            select(AgentToolMutation).where(
                AgentToolMutation.conversation_id == conversation_id,
                AgentToolMutation.tool_name == RENAME_ASSET_TOOL_NAME,
                AgentToolMutation.idempotency_key == normalized_key,
            )
        )
        if existing is not None and existing.request_hash == request_hash and existing.result_json is not None:
            return dict(existing.result_json)
        raise ConflictError("重命名工具请求与已有副作用账本冲突") from None
    return result


def reconcile_agent_asset_rename(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    asset_id: str,
    expected_display_name: str,
    target_display_name: str,
) -> AgentAssetRenameReconcileResult:
    normalized_key = _normalize_tool_idempotency_key(idempotency_key)
    prepared = _normalize_rename_prepared(
        asset_id=asset_id,
        expected_display_name=expected_display_name,
        target_display_name=target_display_name,
    )
    request_hash = _rename_request_hash(conversation_id=conversation_id, prepared=prepared)
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    asset = _get_scoped_asset_for_update(
        session,
        product_id=conversation.product_id,
        asset_id=prepared.asset_id,
    )
    mutation = session.scalar(
        select(AgentToolMutation).where(
            AgentToolMutation.conversation_id == conversation.id,
            AgentToolMutation.tool_name == RENAME_ASSET_TOOL_NAME,
            AgentToolMutation.idempotency_key == normalized_key,
        )
    )
    if mutation is not None:
        if mutation.request_hash != request_hash:
            return AgentAssetRenameReconcileResult(
                state="conflict",
                detail="工具幂等键已绑定其他请求",
            )
        if mutation.status == AgentToolMutationStatus.UNKNOWN:
            return AgentAssetRenameReconcileResult(state="unknown", detail="副作用结果仍不明确")
        if (
            mutation.status == AgentToolMutationStatus.APPLIED
            and mutation.result_json is not None
            and asset.display_name == prepared.target_display_name
        ):
            return AgentAssetRenameReconcileResult(
                state="applied",
                result=dict(mutation.result_json),
                detail="重命名副作用已提交",
            )
        return AgentAssetRenameReconcileResult(
            state="conflict",
            detail="图片当前名称与副作用账本不一致",
        )
    if asset.display_name == prepared.expected_display_name:
        return AgentAssetRenameReconcileResult(
            state="not_applied",
            detail="图片仍处于副作用执行前状态",
        )
    return AgentAssetRenameReconcileResult(
        state="conflict",
        detail="图片名称既非预期旧值，且不存在匹配的副作用账本",
    )


def agent_asset_metadata(asset: ProductImageAsset) -> dict[str, Any]:
    media = asset.media_object
    return {
        "id": asset.id,
        "display_name": asset.display_name,
        "original_filename": asset.original_filename,
        "origin_type": asset.origin_type.value,
        "mime_type": media.mime_type,
        "byte_size": media.byte_size,
        "width": media.width,
        "height": media.height,
        "verification_status": media.verification_status.value,
        "created_at": asset.created_at.isoformat(),
    }


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
            .options(selectinload(ProductImageAsset.media_object))
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


def _get_scoped_asset_for_update(
    session: Session,
    *,
    product_id: str,
    asset_id: str,
) -> ProductImageAsset:
    asset = session.scalar(
        select(ProductImageAsset)
        .where(ProductImageAsset.id == asset_id, ProductImageAsset.product_id == product_id)
        .with_for_update()
    )
    if asset is None:
        raise NotFoundError("商品图片不存在")
    return asset


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


def _normalize_display_name(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("图片显示名不能为空")
    if len(normalized) > 255:
        raise BusinessValidationError("图片显示名不能超过 255 个字符")
    return normalized


def _normalize_tool_idempotency_key(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("工具 idempotency key 不能为空")
    if len(normalized.encode("utf-8")) > 200:
        raise BusinessValidationError("工具 idempotency key 不能超过 200 bytes")
    return normalized


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
        asset_id=normalized_asset_id,
        expected_display_name=_normalize_display_name(expected_display_name),
        target_display_name=_normalize_display_name(target_display_name),
    )


def _rename_request_hash(
    *,
    conversation_id: str,
    prepared: AgentAssetRenamePrepared,
) -> str:
    payload = {
        "schema_version": 1,
        "conversation_id": conversation_id,
        "tool_name": RENAME_ASSET_TOOL_NAME,
        "asset_id": prepared.asset_id,
        "expected_display_name": prepared.expected_display_name,
        "target_display_name": prepared.target_display_name,
    }
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(encoded).hexdigest()


def _encode_asset_cursor(created_at: datetime, asset_id: str) -> str:
    normalized_created_at = created_at if created_at.tzinfo is not None else created_at.replace(tzinfo=UTC)
    payload = json.dumps(
        {"created_at": normalized_created_at.isoformat(), "id": asset_id},
        sort_keys=True,
        separators=(",", ":"),
    ).encode()
    return base64.urlsafe_b64encode(payload).decode().rstrip("=")


def _decode_asset_cursor(value: str) -> tuple[datetime, str]:
    try:
        padded = value.strip() + "=" * (-len(value.strip()) % 4)
        raw = base64.b64decode(padded, altchars=b"-_", validate=True)
        decoded = json.loads(raw)
        if not isinstance(decoded, dict) or set(decoded) != {"created_at", "id"}:
            raise ValueError
        created_at = datetime.fromisoformat(decoded["created_at"])
        asset_id = decoded["id"]
        if created_at.tzinfo is None or not isinstance(asset_id, str) or not asset_id:
            raise ValueError
        return created_at, asset_id
    except (TypeError, ValueError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise BusinessValidationError("图库分页 cursor 无效") from exc


__all__ = [
    "AGENT_ASSET_LIST_DEFAULT_LIMIT",
    "AGENT_ASSET_LIST_MAX_LIMIT",
    "AGENT_TOOL_CONTRACT_VERSION",
    "RENAME_ASSET_TOOL_NAME",
    "AgentAssetContent",
    "AgentAssetPage",
    "AgentAssetRenamePrepared",
    "AgentAssetRenameReconcileResult",
    "agent_asset_metadata",
    "apply_agent_asset_rename",
    "get_agent_contract",
    "get_agent_product_context",
    "inspect_agent_product_assets",
    "list_agent_product_assets",
    "prepare_agent_asset_rename",
    "read_agent_product_asset_content",
    "reconcile_agent_asset_rename",
]
