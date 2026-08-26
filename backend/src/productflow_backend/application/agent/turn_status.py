"""Agent Turn 生命周期状态的命名分区。"""

from __future__ import annotations

from productflow_backend.domain.enums import AgentTurnStatus

# 运行中或可被恢复执行继续推进的状态。
IN_FLIGHT_TURN_STATUSES = frozenset(
    {
        AgentTurnStatus.QUEUED,
        AgentTurnStatus.RUNNING,
        AgentTurnStatus.CANCEL_REQUESTED,
    }
)

ACTIVE_TURN_STATUSES = frozenset(
    {
        *IN_FLIGHT_TURN_STATUSES,
        AgentTurnStatus.REQUIRES_INPUT,
    }
)

# 当前 Turn 在等确认时，Task 不能再开另一条 Turn。
TASK_BLOCKING_TURN_STATUSES = frozenset(
    {
        *ACTIVE_TURN_STATUSES,
        AgentTurnStatus.AWAITING_CONFIRMATION,
    }
)

# 确认等待对 runtime / 事件流是终态。它故意不进 ACTIVE_TURN_STATUSES，
# 避免租约恢复把它改成 UNKNOWN。
TERMINAL_TURN_STATUSES = frozenset(
    {
        AgentTurnStatus.AWAITING_CONFIRMATION,
        AgentTurnStatus.SUCCEEDED,
        AgentTurnStatus.FAILED,
        AgentTurnStatus.CANCELED,
        AgentTurnStatus.UNKNOWN,
    }
)

__all__ = [
    "ACTIVE_TURN_STATUSES",
    "IN_FLIGHT_TURN_STATUSES",
    "TASK_BLOCKING_TURN_STATUSES",
    "TERMINAL_TURN_STATUSES",
]
