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

export function excludeQuestionContinuationTurns(turns: readonly AgentTurn[]): AgentTurn[] {
  const childIds = new Set(
    turns.flatMap((turn) =>
      turn.continuation_turn_id && turn.continuation_turn_id !== turn.id ? [turn.continuation_turn_id] : [],
    ),
  );
  return turns.filter((turn) => !childIds.has(turn.id));
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

function compareTurnTime(
  left: Pick<AgentTurn, "id" | "created_at">,
  right: Pick<AgentTurn, "id" | "created_at">,
): number {
  return left.created_at.localeCompare(right.created_at) || left.id.localeCompare(right.id);
}

export function excludeSupersededTurnGroups(groups: readonly AgentTurnDisplayGroup[]): AgentTurnDisplayGroup[] {
  const cuts = groups.filter((group) => {
    if (group.latest.id === group.root.id) return false;
    return retrySourceTurnId(group.latest.idempotency_key) !== null;
  });
  if (!cuts.length) return [...groups];
  return groups.filter((group) => {
    return !cuts.some((cut) =>
      compareTurnTime(group.root, cut.root) > 0 && compareTurnTime(group.root, cut.latest) < 0,
    );
  });
}

export function visibleAgentTurnGroups(turns: readonly AgentTurn[]): AgentTurnDisplayGroup[] {
  return excludeSupersededTurnGroups(groupAgentTurnAttempts(excludeQuestionContinuationTurns(turns)));
}

export function visibleAgentTurns(turns: readonly (AgentTurn | null | undefined)[]): AgentTurn[] {
  const present: AgentTurn[] = [];
  const seen = new Set<string>();
  for (const turn of turns) {
    if (!turn || seen.has(turn.id)) continue;
    seen.add(turn.id);
    present.push(turn);
  }
  return visibleAgentTurnGroups(present).map((group) => group.latest);
}
