from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

from sqlalchemy import or_, select
from sqlalchemy.orm import Session

from productflow_backend.domain.enums import AgentTurnStatus
from productflow_backend.infrastructure.agent_service import AgentServiceRequestError, AgentServiceTurnState
from productflow_backend.infrastructure.db.models import AgentTurnProjection

_PROJECTED_CANDIDATE_STATUSES = {
    AgentTurnStatus.QUEUED,
    AgentTurnStatus.RUNNING,
    AgentTurnStatus.REQUIRES_INPUT,
    AgentTurnStatus.CANCEL_REQUESTED,
    AgentTurnStatus.UNKNOWN,
}
_HARNESS_BLOCKING_STATUSES = _PROJECTED_CANDIDATE_STATUSES
_HARNESS_SAFE_TERMINAL_STATUSES = {
    AgentTurnStatus.SUCCEEDED,
    AgentTurnStatus.FAILED,
    AgentTurnStatus.CANCELED,
}


class AgentCutoverGateway(Protocol):
    def get_turn(self, *, conversation_id: str, turn_id: str) -> AgentServiceTurnState: ...


@dataclass(frozen=True, slots=True)
class AgentCatalogCutoverBlocker:
    projection_id: str
    conversation_id: str
    harness_turn_id: str | None
    projected_status: AgentTurnStatus
    harness_status: AgentTurnStatus | None
    reason: str


@dataclass(frozen=True, slots=True)
class AgentCatalogCutoverSummary:
    checked_turns: int
    blockers: tuple[AgentCatalogCutoverBlocker, ...]

    @property
    def ready(self) -> bool:
        return not self.blockers


def inspect_agent_catalog_cutover(
    session: Session,
    *,
    gateway: AgentCutoverGateway,
) -> AgentCatalogCutoverSummary:
    candidates = list(
        session.scalars(
            select(AgentTurnProjection)
            .where(
                or_(
                    AgentTurnProjection.status.in_(_PROJECTED_CANDIDATE_STATUSES),
                    (
                        (AgentTurnProjection.status == AgentTurnStatus.AWAITING_CONFIRMATION)
                        & AgentTurnProjection.workflow_draft_revision_id.is_(None)
                    ),
                )
            )
            .order_by(AgentTurnProjection.created_at.asc(), AgentTurnProjection.id.asc())
        )
    )
    blockers: list[AgentCatalogCutoverBlocker] = []
    for projection in candidates:
        if projection.harness_turn_id is None:
            blockers.append(
                _blocker(
                    projection,
                    harness_status=None,
                    reason="Turn 尚未绑定 harness_turn_id，无法确认旧工具执行是否开始",
                )
            )
            continue
        try:
            state = gateway.get_turn(
                conversation_id=projection.conversation_id,
                turn_id=projection.harness_turn_id,
            )
        except AgentServiceRequestError as exc:
            blockers.append(
                _blocker(
                    projection,
                    harness_status=None,
                    reason=f"无法向 Agent service 复核 Turn 状态: {exc.code}",
                )
            )
            continue
        if state.status in _HARNESS_BLOCKING_STATUSES:
            blockers.append(
                _blocker(
                    projection,
                    harness_status=state.status,
                    reason="Agent Turn 仍可能恢复或继续执行旧工具目录",
                )
            )
        elif state.status == AgentTurnStatus.AWAITING_CONFIRMATION:
            if projection.workflow_draft_revision_id is None:
                blockers.append(
                    _blocker(
                        projection,
                        harness_status=state.status,
                        reason="awaiting_confirmation Turn 的 WorkflowDraft artifact 尚未同步",
                    )
                )
        elif state.status not in _HARNESS_SAFE_TERMINAL_STATUSES:
            blockers.append(
                _blocker(
                    projection,
                    harness_status=state.status,
                    reason="Agent service 返回了未纳入切换合同的 Turn 状态",
                )
            )
    return AgentCatalogCutoverSummary(checked_turns=len(candidates), blockers=tuple(blockers))


def _blocker(
    projection: AgentTurnProjection,
    *,
    harness_status: AgentTurnStatus | None,
    reason: str,
) -> AgentCatalogCutoverBlocker:
    return AgentCatalogCutoverBlocker(
        projection_id=projection.id,
        conversation_id=projection.conversation_id,
        harness_turn_id=projection.harness_turn_id,
        projected_status=projection.status,
        harness_status=harness_status,
        reason=reason,
    )


__all__ = [
    "AgentCatalogCutoverBlocker",
    "AgentCatalogCutoverSummary",
    "AgentCutoverGateway",
    "inspect_agent_catalog_cutover",
]
