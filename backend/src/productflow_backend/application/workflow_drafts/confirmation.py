"""商品 WorkflowDraft 确认入口。在线写入已退休。"""

from __future__ import annotations

from typing import NoReturn

from sqlalchemy.orm import Session

from productflow_backend.application.workflow_drafts.service import PRODUCT_WORKFLOW_DRAFT_RETIRED
from productflow_backend.domain.errors import ConflictError


def confirm_product_workflow_draft(
    session: Session,
    *,
    product_id: str,
    draft_id: str,
    expected_draft_version: int,
) -> NoReturn:
    """商品路径不再确认 WorkflowDraft。"""
    del session, product_id, draft_id, expected_draft_version
    raise ConflictError(PRODUCT_WORKFLOW_DRAFT_RETIRED)


__all__ = ["confirm_product_workflow_draft"]
