"""Named partitions for Agent Turn lifecycle status handling."""

from __future__ import annotations

from productflow_backend.domain.enums import AgentTurnStatus

# These statuses may still be advanced by a runtime or recovered execution.
IN_FLIGHT_TURN_STATUSES = frozenset(
    {
        AgentTurnStatus.QUEUED,
        AgentTurnStatus.RUNNING,
        AgentTurnStatus.CANCEL_REQUESTED,
    }
)

TURN_INPUT_WAIT_STATUSES = frozenset(
    {
        AgentTurnStatus.REQUIRES_INPUT,
        AgentTurnStatus.AWAITING_CONFIRMATION,
    }
)

ACTIVE_TURN_STATUSES = frozenset(
    {
        *IN_FLIGHT_TURN_STATUSES,
        AgentTurnStatus.REQUIRES_INPUT,
    }
)

# A Task cannot accept another Turn while its current Turn awaits confirmation.
TASK_BLOCKING_TURN_STATUSES = frozenset(
    {
        *ACTIVE_TURN_STATUSES,
        AgentTurnStatus.AWAITING_CONFIRMATION,
    }
)

# A confirmation wait is terminal for runtime/event-stream purposes. It is
# intentionally excluded from ACTIVE_TURN_STATUSES so lease recovery does not
# turn it into UNKNOWN.
TERMINAL_TURN_STATUSES = frozenset(
    {
        AgentTurnStatus.AWAITING_CONFIRMATION,
        AgentTurnStatus.SUCCEEDED,
        AgentTurnStatus.FAILED,
        AgentTurnStatus.CANCELED,
        AgentTurnStatus.UNKNOWN,
    }
)

POLLABLE_TURN_STATUSES = IN_FLIGHT_TURN_STATUSES

# Lease recovery may only classify these statuses after an expired execution.
EXECUTION_RECOVERABLE_TURN_STATUSES = ACTIVE_TURN_STATUSES


__all__ = [
    "ACTIVE_TURN_STATUSES",
    "EXECUTION_RECOVERABLE_TURN_STATUSES",
    "IN_FLIGHT_TURN_STATUSES",
    "POLLABLE_TURN_STATUSES",
    "TASK_BLOCKING_TURN_STATUSES",
    "TERMINAL_TURN_STATUSES",
    "TURN_INPUT_WAIT_STATUSES",
]
