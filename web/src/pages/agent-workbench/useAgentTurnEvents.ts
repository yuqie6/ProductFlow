import { useEffect, useReducer, useRef, useState } from "react";

import type { AgentTurn, AgentTurnEvent } from "../../lib/types";
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
  after?: number;
  createEventSource?: AgentEventSourceFactory;
  onEvent: (event: AgentTurnEvent) => void;
  onConnectionState?: (state: AgentEventConnectionState) => void;
  onProtocolError?: (error: Error) => void;
  onStreamError?: (message: string | null) => void;
}

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

export function subscribeToAgentTurnEvents(input: AgentEventSubscriptionInput): () => void {
  const createEventSource = input.createEventSource ?? createBrowserEventSource;
  let closed = false;
  let source: AgentEventSourceLike | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  let reconnectAttempt = 0;
  let cursor = input.after ?? 0;
  let reconnectScheduled = false;

  const close = () => {
    if (closed) return;
    closed = true;
    if (reconnectTimer !== undefined) clearTimeout(reconnectTimer);
    reconnectTimer = undefined;
    source?.close();
    source = null;
    input.onConnectionState?.("closed");
  };

  const scheduleReconnect = () => {
    if (closed || reconnectScheduled) return;
    reconnectScheduled = true;
    source?.close();
    source = null;
    input.onConnectionState?.("reconnecting");
    const delay = Math.min(5_000, 250 * 2 ** Math.min(reconnectAttempt, 5));
    reconnectAttempt += 1;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = undefined;
      reconnectScheduled = false;
      connect();
    }, delay);
  };

  const connect = () => {
    if (closed) return;
    input.onConnectionState?.("connecting");
    let nextSource: AgentEventSourceLike;
    try {
      nextSource = createEventSource(withEventCursor(input.url, cursor));
    } catch (error) {
      input.onStreamError?.(error instanceof Error ? error.message : "Agent SSE 连接失败");
      scheduleReconnect();
      return;
    }
    source = nextSource;
    nextSource.addEventListener("open", () => {
      if (closed || source !== nextSource) return;
      reconnectAttempt = 0;
      input.onConnectionState?.("open");
      input.onStreamError?.(null);
    });
    nextSource.addEventListener("error", (event) => {
      if (closed || source !== nextSource) return;
      const data = readEventData(event);
      if (data !== null) input.onStreamError?.(readStreamError(data));
      scheduleReconnect();
    });
    AGENT_EVENT_TYPES.forEach((eventType) => {
      nextSource.addEventListener(eventType, (event) => {
        if (closed || source !== nextSource) return;
        const data = readEventData(event);
        if (data === null) {
          input.onProtocolError?.(new Error(`Agent SSE ${eventType} 缺少 data`));
          return;
        }
        try {
          const parsed = parseAgentTurnEvent(data, eventType, input.scope);
          if (parsed.sequence <= cursor) return;
          if (parsed.sequence !== cursor + 1) {
            input.onProtocolError?.(
              new Error(`Agent SSE 事件序列断档：当前为 ${cursor}，收到 ${parsed.sequence}`),
            );
            scheduleReconnect();
            return;
          }
          cursor = parsed.sequence;
          input.onEvent(parsed);
          if (AGENT_TERMINAL_EVENT_KINDS.some((kind) => kind === parsed.kind)) close();
        } catch (error) {
          input.onProtocolError?.(error instanceof Error ? error : new Error("Agent SSE 事件无效"));
        }
      });
    });
  };

  connect();
  return close;
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
  const shouldSubscribe = Boolean(enabled && turn && agentTurnNeedsEventStream(turn) && harnessTurnId);
  const eventURL = getEventsUrl(turnKey, 0);
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
    const url = eventURL;
    try {
      return subscribeToAgentTurnEvents({
        url,
        scope: { run_id: runId, turn_id: harnessTurnId },
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
  }, [eventURL, harnessTurnId, runId, shouldSubscribe, turnKey]);

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

function withEventCursor(url: string, cursor: number): string {
  const isAbsolute = /^[a-z][a-z\d+.-]*:\/\//i.test(url);
  const parsed = new URL(url, "http://agent-events.local");
  parsed.searchParams.set("after", String(cursor));
  return isAbsolute ? parsed.toString() : `${parsed.pathname}${parsed.search}${parsed.hash}`;
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
