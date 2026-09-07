import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";

import type { AgentTurn, AgentTurnEvent } from "../../../lib/types";
import { getMerchantGeneration, isCurrentMerchantGeneration } from "../../../lib/merchantBoundary";
import {
  AGENT_TERMINAL_EVENT_KINDS,
  agentTurnNeedsEventStream,
  isAgentTurnSettled,
  type AgentTurnEventScope,
  type AgentTurnEventState,
} from "./agentEventReducer";
import {
  getConversationRuntime,
  releaseConversationRuntime,
  type ConversationConnectionState,
  type ConversationEventSourceLike,
  type ConversationEventSourceFactory,
  type ConversationRuntime,
} from "./conversation/runtime";

export type AgentEventConnectionState = ConversationConnectionState;
export type AgentEventSourceLike = ConversationEventSourceLike;
export type AgentEventSourceFactory = ConversationEventSourceFactory;

interface UseAgentTurnEventsInput {
  getEventsUrl: (turnId: string, after: number) => string;
  runId: string | null;
  turn: AgentTurn | null;
  enabled?: boolean;
  onEvent?: (event: AgentTurnEvent) => void;
  onArtifactProposed?: () => void;
  onTerminal?: () => void;
}

export interface UseAgentTurnEventsResult {
  state: AgentTurnEventState;
  connectionState: AgentEventConnectionState;
  streamError: string | null;
}

export interface AgentTurnEventStreamSpec {
  turnId: string;
  key: string;
  url: string;
  finite: boolean;
  scope: AgentTurnEventScope;
}

interface HeldTurnRuntime {
  turnId: string;
  runtime: ConversationRuntime;
  unsubscribe: () => void;
  release: () => void;
}

export { subscribeToConversationEvents as subscribeToAgentTurnEvents } from "./conversation/runtime";

export function listAgentTurnEventStreams(
  turns: readonly AgentTurn[],
  getEventsUrl: (turnId: string, after: number) => string,
): AgentTurnEventStreamSpec[] {
  const specs: AgentTurnEventStreamSpec[] = [];
  const seen = new Set<string>();
  for (const turn of turns) {
    if (!agentTurnNeedsEventStream(turn) || seen.has(turn.id)) continue;
    seen.add(turn.id);
    const url = getEventsUrl(turn.id, 0);
    if (!url) continue;
    specs.push({
      turnId: turn.id,
      key: turn.id,
      url,
      finite: isAgentTurnSettled(turn.status),
      scope: { run_id: turn.harness_run_id, turn_id: turn.harness_turn_id ?? "" },
    });
  }
  return specs;
}

export function useAgentTurnEvents({
  getEventsUrl,
  runId,
  turn,
  enabled = true,
  onEvent,
  onArtifactProposed,
  onTerminal,
}: UseAgentTurnEventsInput): UseAgentTurnEventsResult {
  const turnKey = turn?.id ?? "";
  const harnessTurnId = turn?.harness_turn_id ?? "";
  const shouldSubscribe = Boolean(enabled && agentTurnNeedsEventStream(turn));
  const finite = Boolean(turn && isAgentTurnSettled(turn.status));
  const eventURL = getEventsUrl(turnKey, 0);
  const runtime = useMemo(
    () => getConversationRuntime({
      key: turnKey,
      url: eventURL,
      scope: { run_id: runId, turn_id: harnessTurnId },
      finite,
    }),
    [eventURL, finite, harnessTurnId, runId, turnKey],
  );
  const state = useSyncExternalStore(runtime.subscribe, runtime.getSnapshot, runtime.getSnapshot);
  const [connectionState, setConnectionState] = useState<AgentEventConnectionState>("idle");
  const [streamError, setStreamError] = useState<string | null>(null);
  const callbacksRef = useRef({ onEvent, onArtifactProposed, onTerminal });
  callbacksRef.current = { onEvent, onArtifactProposed, onTerminal };
  useEffect(() => {
    if (!shouldSubscribe) {
      setConnectionState("idle");
      return;
    }
    const generation = getMerchantGeneration();
    try {
      const key = turnKey;
      const release = runtime.acquire({
        onConnectionState: setConnectionState,
        onProtocolError: (error) => setStreamError(error.message),
        onStreamError: (message) => {
          setStreamError(message);
        },
        onEvent: (event) => {
          if (!isCurrentMerchantGeneration(generation)) return;
          callbacksRef.current.onEvent?.(event);
          if (event.kind === "approval.requested") {
            callbacksRef.current.onArtifactProposed?.();
          }
          if (AGENT_TERMINAL_EVENT_KINDS.some((kind) => kind === event.kind)) {
            callbacksRef.current.onTerminal?.();
          }
        },
      });
      return () => {
        release();
        releaseConversationRuntime(key, runtime);
      };
    } catch (error) {
      setConnectionState("closed");
      setStreamError(error instanceof Error ? error.message : "Agent SSE 连接失败");
    }
  }, [eventURL, harnessTurnId, runId, runtime, shouldSubscribe, turnKey]);

  return {
    state,
    connectionState,
    streamError: state.protocol_error ?? streamError,
  };
}

export function useAgentTurnEventMap({
  getEventsUrl,
  turns,
  enabled = true,
}: {
  getEventsUrl: (turnId: string, after: number) => string;
  turns: readonly AgentTurn[];
  enabled?: boolean;
}): Record<string, AgentTurnEventState> {
  const getEventsUrlRef = useRef(getEventsUrl);
  getEventsUrlRef.current = getEventsUrl;
  const turnsRef = useRef(turns);
  turnsRef.current = turns;
  const heldRef = useRef(new Map<string, HeldTurnRuntime>());
  const [states, setStates] = useState<Record<string, AgentTurnEventState>>({});
  const streamIdentity = enabled
    ? listAgentTurnEventStreams(turns, getEventsUrl)
      .map((spec) => `${spec.key}\0${spec.scope.turn_id}\0${spec.scope.run_id ?? ""}`)
      .join("\n")
    : "";

  useEffect(() => {
    const held = heldRef.current;
    const specs = enabled ? listAgentTurnEventStreams(turnsRef.current, getEventsUrlRef.current) : [];
    const nextKeys = new Set(specs.map((spec) => spec.key));
    const publish = () => {
      setStates(snapshotHeldRuntimes(heldRef.current));
    };

    for (const spec of specs) {
      if (held.has(spec.key)) continue;
      const runtime = getConversationRuntime({
        key: spec.key,
        url: spec.url,
        scope: spec.scope,
        finite: spec.finite,
      });
      const unsubscribe = runtime.subscribe(publish);
      const release = runtime.acquire();
      held.set(spec.key, { turnId: spec.turnId, runtime, unsubscribe, release });
    }

    for (const [key, item] of [...held.entries()]) {
      if (nextKeys.has(key)) continue;
      item.unsubscribe();
      item.release();
      releaseConversationRuntime(key, item.runtime);
      held.delete(key);
    }

    publish();
  }, [enabled, streamIdentity]);

  useEffect(() => () => {
    for (const [key, item] of heldRef.current) {
      item.unsubscribe();
      item.release();
      releaseConversationRuntime(key, item.runtime);
    }
    heldRef.current.clear();
  }, []);

  return states;
}

function snapshotHeldRuntimes(held: Map<string, HeldTurnRuntime>): Record<string, AgentTurnEventState> {
  const states: Record<string, AgentTurnEventState> = {};
  for (const item of held.values()) {
    states[item.turnId] = item.runtime.getSnapshot();
  }
  return states;
}
