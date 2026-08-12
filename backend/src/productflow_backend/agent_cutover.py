from __future__ import annotations

import json

from productflow_backend.application.agent_cutover import inspect_agent_catalog_cutover
from productflow_backend.infrastructure.agent_service import get_agent_service_client
from productflow_backend.infrastructure.db.session import get_session_factory


def main() -> int:
    session = get_session_factory()()
    try:
        summary = inspect_agent_catalog_cutover(
            session,
            gateway=get_agent_service_client(),
        )
    finally:
        session.close()
    payload = {
        "schema_version": 1,
        "ready": summary.ready,
        "checked_turns": summary.checked_turns,
        "blockers": [
            {
                "projection_id": blocker.projection_id,
                "conversation_id": blocker.conversation_id,
                "harness_turn_id": blocker.harness_turn_id,
                "projected_status": blocker.projected_status.value,
                "harness_status": blocker.harness_status.value if blocker.harness_status is not None else None,
                "reason": blocker.reason,
            }
            for blocker in summary.blockers
        ],
    }
    print(json.dumps(payload, ensure_ascii=False, separators=(",", ":")))
    return 0 if summary.ready else 1


if __name__ == "__main__":
    raise SystemExit(main())
