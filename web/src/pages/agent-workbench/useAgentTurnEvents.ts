import { useEffect, useReducer, useRef, useState } from "react";

import { api } from "../../lib/api";
import type { AgentConversation, AgentTurn, AgentTurnEvent } from "../../lib/types";
import {
  AGENT_TERMINAL_EVENT_KINDS,
  agentEventReducer,
  agentTurnNeedsEventStream,
  createAgentTurnEventState,
  parseAgentTurnEvent,
  type AgentTurnEventScope,
  type AgentTurnEventState,
} from "./agentEventReducer";

const AGENT_EVENT_TYPES = [
  "turn.queued",
  "turn.started",
  "text.delta",
  "tool.step",
  "question.required",
  "question.answered",
  "turn.resume_requested",
  "turn.cancel_requested",
  "turn.requires_input",
  "artifact.proposed",
  ...AGENT_TERMINAL_EVENT_KINDS,
] as const;

export type AgentEventConnectionState = "idle" | "connecting" | "open" | "reconnecting" | "closed";

export interface AgentEventSourceLike {
  addEventListener(type: string, listener: EventListener): void;
  close(): void;
}

export type AgentEventSourceFactory = (url: string) => AgentEventSourceLike;

interface AgentEventSubscriptionInput {
  url: string;
  scope: AgentTurnEventScope;
  createEventSource?: AgentEventSourceFactory;
  onEvent: (event: AgentTurnEvent) => void;
  onConnectionState?: (state: AgentEventConnectionState) => void;
  onProtocolError?: (error: Error) => void;
  onStreamError?: (message: string) => void;
}

interface UseAgentTurnEventsInput {
  productId: string;
  conversation: AgentConversation;
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

export function subscribeToAgentTurnEvents(input: AgentEventSubscriptionInput): () => void {
  const createEventSource = input.createEventSource ?? createBrowserEventSource;
  const source = createEventSource(input.url);
  let closed = false;
  const close = () => {
    if (!closed) {
      closed = true;
      source.close();
      input.onConnectionState?.("closed");
    }
  };

  input.onConnectionState?.("connecting");
  source.addEventListener("open", () => {
    if (!closed) {
      input.onConnectionState?.("open");
    }
  });
  source.addEventListener("error", (event) => {
    if (closed) {
      return;
    }
    const data = readEventData(event);
    if (data === null) {
      input.onConnectionState?.("reconnecting");
      return;
    }
    input.onStreamError?.(readStreamError(data));
  });

  AGENT_EVENT_TYPES.forEach((eventType) => {
    source.addEventListener(eventType, (event) => {
      if (closed) {
        return;
      }
      const data = readEventData(event);
      if (data === null) {
        input.onProtocolError?.(new Error(`Agent SSE ${eventType} 缺少 data`));
        return;
      }
      try {
        const parsed = parseAgentTurnEvent(data, eventType, input.scope);
        input.onEvent(parsed);
        if (AGENT_TERMINAL_EVENT_KINDS.some((kind) => kind === parsed.kind)) {
          close();
        }
      } catch (error) {
        input.onProtocolError?.(error instanceof Error ? error : new Error("Agent SSE 事件无效"));
      }
    });
  });

  return close;
}

export function useAgentTurnEvents({
  productId,
  conversation,
  turn,
  enabled = true,
  onEvent,
  onArtifactProposed,
  onTerminal,
}: UseAgentTurnEventsInput): UseAgentTurnEventsResult {
  const turnKey = turn?.id ?? "";
  const harnessTurnId = turn?.harness_turn_id ?? "";
  const shouldSubscribe = Boolean(enabled && turn && agentTurnNeedsEventStream(turn) && harnessTurnId);
  const [state, dispatch] = useReducer(agentEventReducer, turnKey, createAgentTurnEventState);
  const [connectionState, setConnectionState] = useState<AgentEventConnectionState>("idle");
  const [streamError, setStreamError] = useState<string | null>(null);
  const callbacksRef = useRef({ onEvent, onArtifactProposed, onTerminal });
  callbacksRef.current = { onEvent, onArtifactProposed, onTerminal };

  useEffect(() => {
    if (turnKey) {
      dispatch({ type: "reset", turn_key: turnKey });
      setStreamError(null);
    }
  }, [turnKey]);

  useEffect(() => {
    if (!shouldSubscribe) {
      setConnectionState("idle");
      return;
    }
    const url = api.getAgentTurnEventsUrl(productId, conversation.id, turnKey, 0);
    try {
      return subscribeToAgentTurnEvents({
        url,
        scope: { run_id: conversation.harness_run_id, turn_id: harnessTurnId },
        onConnectionState: setConnectionState,
        onProtocolError: (error) => setStreamError(error.message),
        onStreamError: setStreamError,
        onEvent: (event) => {
          dispatch({ type: "event", event });
          callbacksRef.current.onEvent?.(event);
          if (event.kind === "artifact.proposed") {
            callbacksRef.current.onArtifactProposed?.();
          }
          if (AGENT_TERMINAL_EVENT_KINDS.some((kind) => kind === event.kind)) {
            callbacksRef.current.onTerminal?.();
          }
        },
      });
    } catch (error) {
      setConnectionState("closed");
      setStreamError(error instanceof Error ? error.message : "Agent SSE 连接失败");
    }
  }, [conversation.harness_run_id, conversation.id, harnessTurnId, productId, shouldSubscribe, turnKey]);

  return {
    state,
    connectionState,
    streamError: state.protocol_error ?? streamError,
  };
}

function createBrowserEventSource(url: string): AgentEventSourceLike {
  if (typeof EventSource === "undefined") {
    throw new Error("当前浏览器不支持 Agent 事件流");
  }
  return new EventSource(url, { withCredentials: true });
}

function readEventData(event: Event): string | null {
  const data = (event as Event & { data?: unknown }).data;
  return typeof data === "string" ? data : null;
}

function readStreamError(data: string): string {
  try {
    const parsed = JSON.parse(data) as { error?: { message?: unknown } };
    return typeof parsed.error?.message === "string" ? parsed.error.message : "Agent 事件流返回错误";
  } catch {
    return "Agent 事件流返回错误";
  }
}
