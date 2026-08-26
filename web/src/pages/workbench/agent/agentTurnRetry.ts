import type { AgentPageContextSnapshotInput, AgentTurn, SubmitAgentTurnInput } from "../../../lib/types";

const RETRY_IDEMPOTENCY = /^retry:([^:]+):/;

export function canRetryAgentTurn(input: {
  turn: Pick<AgentTurn, "input_text">;
}): boolean {
  return Boolean(input.turn.input_text.trim());
}

export function agentTurnRetrySubmitInput(
  turn: Pick<AgentTurn, "input_text" | "input_asset_ids" | "task_id">,
  options: {
    idempotencyKey: string;
    taskId?: string | null;
    pageContext?: AgentPageContextSnapshotInput | null;
  },
): SubmitAgentTurnInput {
  return {
    input_text: turn.input_text,
    asset_ids: [...turn.input_asset_ids],
    idempotency_key: options.idempotencyKey,
    task_id: options.taskId !== undefined ? options.taskId : turn.task_id,
    page_context: options.pageContext ?? null,
  };
}

export function retryIdempotencyKey(turnId: string): string {
  return `retry:${turnId}:${globalThis.crypto.randomUUID()}`;
}

export function retrySourceTurnId(idempotencyKey: string): string | null {
  return RETRY_IDEMPOTENCY.exec(idempotencyKey)?.[1] ?? null;
}

export interface AgentTurnDisplayGroup {
  root: AgentTurn;
  attempts: AgentTurn[];
  latest: AgentTurn;
}

export function groupAgentTurnAttempts(turns: readonly AgentTurn[]): AgentTurnDisplayGroup[] {
  const byId = new Map(turns.map((turn) => [turn.id, turn]));
  const attemptsByRoot = new Map<string, AgentTurn[]>();
  const rootOrder: string[] = [];

  const rootIdFor = (turn: AgentTurn): string => {
    const seen = new Set<string>();
    let current = turn;
    while (true) {
      const sourceId = retrySourceTurnId(current.idempotency_key);
      if (!sourceId || seen.has(sourceId)) {
        return current.id;
      }
      const source = byId.get(sourceId);
      if (!source) {
        return current.id;
      }
      seen.add(current.id);
      current = source;
    }
  };

  for (const turn of turns) {
    const rootId = rootIdFor(turn);
    const existing = attemptsByRoot.get(rootId);
    if (existing) {
      existing.push(turn);
      continue;
    }
    attemptsByRoot.set(rootId, [turn]);
    rootOrder.push(rootId);
  }

  return rootOrder.map((rootId) => {
    const attempts = attemptsByRoot.get(rootId) ?? [];
    const root = byId.get(rootId) ?? attempts[0];
    return {
      root,
      attempts,
      latest: attempts[attempts.length - 1] ?? root,
    };
  });
}
