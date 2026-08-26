"""Turn 事件 SSE：只重放 PostgreSQL event store，不读 Node.js session files。

这是独立的事件投递面，不是 Graph 运行或 Agent 控制的透传层。
"""

from __future__ import annotations

import asyncio
import json
from collections.abc import AsyncIterator

from productflow_backend.application.agent.execution import list_agent_turn_events
from productflow_backend.application.agent.turn_status import TERMINAL_TURN_STATUSES
from productflow_backend.infrastructure.db.models import AgentTurnProjection
from productflow_backend.infrastructure.db.session import get_session_factory


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
            terminal = projection.status in TERMINAL_TURN_STATUSES
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
