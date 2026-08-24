"""Turn 事件 SSE：只重放 PostgreSQL event store，不读 Node.js session files。"""

from __future__ import annotations

import asyncio
import json
from collections.abc import AsyncIterator

from productflow_backend.application.agent.execution import list_agent_turn_events
from productflow_backend.domain.enums import AgentTurnStatus
from productflow_backend.infrastructure.db.models import AgentTurnProjection
from productflow_backend.infrastructure.db.session import get_session_factory

_TERMINAL_TURN_STATUSES = {
    AgentTurnStatus.AWAITING_CONFIRMATION,
    AgentTurnStatus.SUCCEEDED,
    AgentTurnStatus.FAILED,
    AgentTurnStatus.CANCELED,
    AgentTurnStatus.UNKNOWN,
}


async def stream_agent_turn_events(
    *,
    projection_id: str,
    after: int,
) -> AsyncIterator[bytes]:
    """从 PostgreSQL event store 按 cursor 重放；连接断开只停止本次 generator。"""

    cursor = after
    while True:
        session = get_session_factory()()
        try:
            projection = session.get(AgentTurnProjection, projection_id)
            if projection is None:
                return
            events = list_agent_turn_events(session, projection_id=projection_id, after=cursor)
            terminal = projection.status in _TERMINAL_TURN_STATUSES
            chunks: list[bytes] = []
            for event in events:
                payload = {
                    "schema_version": event.schema_version,
                    "run_id": event.run_id,
                    "turn_id": event.turn_id,
                    "sequence": event.sequence,
                    "created_at": event.created_at.isoformat(),
                    "kind": event.kind,
                    "payload": event.payload_json,
                }
                body = json.dumps(payload, ensure_ascii=False, separators=(",", ":"))
                chunks.append(f"id: {event.sequence}\nevent: {event.kind}\ndata: {body}\n\n".encode())
                cursor = event.sequence
            if terminal and not events:
                return
        finally:
            session.close()

        for chunk in chunks:
            yield chunk
        yield b": heartbeat\n\n"
        await asyncio.sleep(0.5)
