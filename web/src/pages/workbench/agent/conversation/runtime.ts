import type { AgentTurnEvent } from "../../../../lib/types";
import {
  AGENT_UI_EVENT_KINDS,
  AGENT_SETTLED_EVENT_KINDS,
  AGENT_TERMINAL_EVENT_KINDS,
  AgentEventProtocolError,
  parseAgentTurnEvent,
  type AgentTurnEventScope,
} from "../agentEventReducer";
import { ConversationAssembler } from "./assembler";

export type ConversationConnectionState = "idle" | "connecting" | "open" | "reconnecting" | "closed";

export interface ConversationEventSourceLike {
  addEventListener(type: string, listener: EventListener): void;
  close(): void;
}

export type ConversationEventSourceFactory = (url: string) => ConversationEventSourceLike;
export type ConversationEventPageFetcher = (url: string, signal?: AbortSignal) => Promise<ConversationEventPage>;

export interface ConversationEventPage {
  items: AgentTurnEvent[];
  next_after: number;
  has_more: boolean;
  stream_state: "live" | "parked" | "terminal";
}

export interface ConversationRuntimeAcquire {
  onEvent?: (event: AgentTurnEvent) => void;
  onConnectionState?: (state: ConversationConnectionState) => void;
  onProtocolError?: (error: Error) => void;
  onStreamError?: (message: string | null) => void;
  onTerminal?: () => void;
  onArtifactProposed?: () => void;
}

export interface ConversationRuntimeOptions {
  key: string;
  url: string;
  scope: AgentTurnEventScope;
  after?: number;
  finite?: boolean;
  createEventSource?: ConversationEventSourceFactory;
  fetchEventPage?: ConversationEventPageFetcher;
}

const AGENT_EVENT_TYPES = [...AGENT_UI_EVENT_KINDS, "agent.invalid"] as const;

/**
 * One event stream and one incremental assembler per turn stream.
 * Product workbench and global Dock can acquire the same runtime without
 * opening a second EventSource or rebuilding the event snapshot.
 */
export class ConversationRuntime {
  readonly assembler: ConversationAssembler;
  private readonly consumers = new Set<ConversationRuntimeAcquire>();
  private stopStream: (() => void) | null = null;

  constructor(private options: ConversationRuntimeOptions) {
    this.assembler = new ConversationAssembler(options.key);
  }

  setFinite(finite: boolean): void {
    this.options = { ...this.options, finite };
  }

  getSnapshot = (): ReturnType<ConversationAssembler["getSnapshot"]> => this.assembler.getSnapshot();
  subscribe = (listener: () => void): (() => void) => this.assembler.subscribe(listener);

  acquire(consumer: ConversationRuntimeAcquire = {}): () => void {
    this.consumers.add(consumer);
    if (this.consumers.size === 1 && this.options.url) {
      this.startStream();
    }
    return () => {
      this.consumers.delete(consumer);
      if (this.consumers.size === 0) {
        this.stopStream?.();
        this.stopStream = null;
      }
    };
  }

  get hasConsumers(): boolean {
    return this.consumers.size > 0;
  }

  dispose(): void {
    this.stopStream?.();
    this.stopStream = null;
    this.consumers.clear();
    this.assembler.dispose();
  }

  private startStream(): void {
    this.stopStream = subscribeToConversationEvents({
      url: this.options.url,
      scope: this.options.scope,
      after: this.options.after,
      finite: this.options.finite,
      createEventSource: this.options.createEventSource,
      fetchEventPage: this.options.fetchEventPage,
      onConnectionState: (state) => this.forEachConsumer((consumer) => consumer.onConnectionState?.(state)),
      onProtocolError: (error) => this.forEachConsumer((consumer) => consumer.onProtocolError?.(error)),
      onStreamError: (message) => this.forEachConsumer((consumer) => consumer.onStreamError?.(message)),
      onEvent: (event) => {
        this.assembler.apply(event);
        this.forEachConsumer((consumer) => consumer.onEvent?.(event));
        if (event.kind === "approval.requested") {
          this.forEachConsumer((consumer) => consumer.onArtifactProposed?.());
        }
        if (AGENT_TERMINAL_EVENT_KINDS.some((kind) => kind === event.kind)) {
          this.forEachConsumer((consumer) => consumer.onTerminal?.());
        }
      },
    });
  }

  private forEachConsumer(callback: (consumer: ConversationRuntimeAcquire) => void): void {
    for (const consumer of [...this.consumers]) callback(consumer);
  }
}

const EMPTY_RUNTIME = new ConversationRuntime({
  key: "__empty__",
  url: "",
  scope: { run_id: null, turn_id: "" },
});

const runtimes = new Map<string, ConversationRuntime>();

export function getConversationRuntime(options: ConversationRuntimeOptions): ConversationRuntime {
  if (!options.key) return EMPTY_RUNTIME;
  const existing = runtimes.get(options.key);
  if (existing) {
    existing.setFinite(Boolean(options.finite));
    return existing;
  }
  const runtime = new ConversationRuntime(options);
  runtimes.set(options.key, runtime);
  return runtime;
}

export function releaseConversationRuntime(key: string, runtime: ConversationRuntime): void {
  if (!key || runtime.hasConsumers || runtimes.get(key) !== runtime) return;
  runtime.dispose();
  runtimes.delete(key);
}

interface ConversationEventSubscriptionInput {
  url: string;
  scope: AgentTurnEventScope;
  after?: number;
  finite?: boolean;
  createEventSource?: ConversationEventSourceFactory;
  fetchEventPage?: ConversationEventPageFetcher;
  onEvent: (event: AgentTurnEvent) => void;
  onConnectionState?: (state: ConversationConnectionState) => void;
  onProtocolError?: (error: Error) => void;
  onStreamError?: (message: string | null) => void;
}

export function subscribeToConversationEvents(input: ConversationEventSubscriptionInput): () => void {
  const createEventSource = input.createEventSource ?? createBrowserEventSource;
  const fetchEventPage = input.fetchEventPage ?? fetchBrowserEventPage;
  const repairController = new AbortController();
  let closed = false;
  let source: ConversationEventSourceLike | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  let reconnectAttempt = 0;
  let cursor = input.after ?? 0;
  let reconnectScheduled = false;
  let seenTerminal = false;
  let streamComplete = false;
  let generation = 0;
  let repairing = false;
  let repairFailures = 0;
  let finiteReconciliationRequested = false;
  const buffered = new Map<number, AgentTurnEvent>();
  let bufferedBytes = 0;

  const protocolFailure = (error: Error) => {
    input.onProtocolError?.(error);
    close();
  };

  const deliver = (event: AgentTurnEvent) => {
    cursor = event.sequence;
    input.onEvent(event);
    if (AGENT_SETTLED_EVENT_KINDS.some((kind) => kind === event.kind)) {
      seenTerminal = true;
    }
  };

  const drainBuffer = () => {
    while (true) {
      const next = buffered.get(cursor + 1);
      if (!next) break;
      buffered.delete(next.sequence);
      bufferedBytes -= JSON.stringify(next).length;
      deliver(next);
    }
  };

  const repairGap = async (repairGeneration: number) => {
    if (closed || repairing) return;
    repairing = true;
    try {
      let hasMore = true;
      let progressed = false;
      let reachedTail = false;
      let tailStreamState: ConversationEventPage["stream_state"] | null = null;
      while (!closed && repairGeneration === generation && hasMore) {
        const previousCursor = cursor;
        const page = await fetchEventPage(withEventPageCursor(input.url, cursor), repairController.signal);
        if (closed || repairGeneration !== generation) return;
        hasMore = page.has_more;
        tailStreamState = page.stream_state;
        for (const candidate of page.items) {
          const parsed = parseAgentTurnEvent(JSON.stringify(candidate), candidate.kind, input.scope);
          if (parsed.sequence <= cursor) continue;
          if (parsed.sequence !== cursor + 1) break;
          deliver(parsed);
          progressed = true;
          drainBuffer();
        }
        drainBuffer();
        if (hasMore && (page.next_after <= previousCursor || cursor <= previousCursor)) {
          throw new AgentEventProtocolError(`Agent 事件补洞没有推进 cursor ${previousCursor}`);
        }
        reachedTail = !hasMore;
      }
      drainBuffer();
      if (buffered.size > 0 && !buffered.has(cursor + 1)) {
        throw new AgentEventProtocolError(`Agent 事件补洞未返回 sequence ${cursor + 1}`);
      }
      if (!progressed && buffered.size > 0) {
        throw new AgentEventProtocolError(`Agent 事件补洞没有推进 cursor ${cursor}`);
      }
      if (finiteReconciliationRequested && (!reachedTail || tailStreamState !== "terminal")) {
        throw new AgentEventProtocolError("Agent 历史事件补齐后仍未到达 terminal 尾部");
      }
      repairFailures = 0;
      if (finiteReconciliationRequested || seenTerminal || streamComplete) close();
    } catch (error) {
      if (closed || repairGeneration !== generation) return;
      if (finiteReconciliationRequested || streamComplete) {
        protocolFailure(error instanceof Error ? error : new AgentEventProtocolError("Agent 历史事件补齐失败"));
        return;
      }
      repairFailures += 1;
      if (repairFailures >= 3) {
        protocolFailure(error instanceof Error ? error : new AgentEventProtocolError("Agent 事件补洞失败"));
      } else {
        scheduleReconnect();
      }
    } finally {
      repairing = false;
    }
  };

  const close = () => {
    if (closed) return;
    closed = true;
    repairController.abort();
    if (reconnectTimer !== undefined) clearTimeout(reconnectTimer);
    reconnectTimer = undefined;
    source?.close();
    source = null;
    input.onConnectionState?.("closed");
  };

  const scheduleReconnect = () => {
    if (closed || reconnectScheduled) return;
    if (input.finite || seenTerminal) {
      close();
      return;
    }
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
    const currentGeneration = ++generation;
    let nextSource: ConversationEventSourceLike;
    try {
      nextSource = createEventSource(withEventCursor(input.url, cursor));
    } catch (error) {
      input.onStreamError?.(error instanceof Error ? error.message : "Agent SSE 连接失败");
      scheduleReconnect();
      return;
    }
    source = nextSource;
    nextSource.addEventListener("open", () => {
      if (closed || source !== nextSource || currentGeneration !== generation) return;
      reconnectAttempt = 0;
      input.onConnectionState?.("open");
      input.onStreamError?.(null);
    });
    nextSource.addEventListener("error", (event) => {
      if (closed || source !== nextSource || currentGeneration !== generation) return;
      const data = readEventData(event);
      if (data !== null) input.onStreamError?.(readStreamError(data));
      if (input.finite || seenTerminal) {
        finiteReconciliationRequested = true;
        nextSource.close();
        source = null;
        input.onConnectionState?.("reconnecting");
        void repairGap(currentGeneration);
        return;
      }
      scheduleReconnect();
    });
    nextSource.addEventListener("stream.complete", () => {
      if (closed || source !== nextSource || currentGeneration !== generation) return;
      streamComplete = true;
      if (buffered.size > 0) void repairGap(currentGeneration);
      else close();
    });
    AGENT_EVENT_TYPES.forEach((eventType) => {
      nextSource.addEventListener(eventType, (event) => {
        if (closed || source !== nextSource || currentGeneration !== generation) return;
        const data = readEventData(event);
        if (data === null) {
          protocolFailure(new AgentEventProtocolError(`Agent SSE ${eventType} 缺少 data`));
          return;
        }
        try {
          const parsed = parseAgentTurnEvent(data, eventType, input.scope);
          if (parsed.sequence <= cursor) return;
          if (parsed.sequence !== cursor + 1) {
            if (!buffered.has(parsed.sequence)) {
              buffered.set(parsed.sequence, parsed);
              bufferedBytes += data.length;
            }
            if (buffered.size > 512 || bufferedBytes > 1024 * 1024) {
              protocolFailure(new AgentEventProtocolError("Agent SSE 缺口缓存超过限制"));
              return;
            }
            void repairGap(currentGeneration);
            return;
          }
          deliver(parsed);
          drainBuffer();
          if (AGENT_SETTLED_EVENT_KINDS.some((kind) => kind === parsed.kind)) {
            close();
          }
        } catch (error) {
          protocolFailure(error instanceof Error ? error : new AgentEventProtocolError("Agent SSE 事件无效"));
        }
      });
    });
  };

  connect();
  return close;
}

async function fetchBrowserEventPage(url: string, signal?: AbortSignal): Promise<ConversationEventPage> {
  const response = await fetch(url, { credentials: "include", headers: { Accept: "application/json" }, signal });
  if (!response.ok) throw new Error(`Agent 事件补洞请求失败 (${response.status})`);
  return await response.json() as ConversationEventPage;
}

function createBrowserEventSource(url: string): ConversationEventSourceLike {
  if (typeof EventSource === "undefined") throw new Error("当前浏览器不支持 Agent 事件流");
  return new EventSource(url, { withCredentials: true });
}

function withEventCursor(url: string, cursor: number): string {
  const isAbsolute = /^[a-z][a-z\d+.-]*:\/\//i.test(url);
  const parsed = new URL(url, "http://agent-events.local");
  parsed.searchParams.set("after", String(cursor));
  return isAbsolute ? parsed.toString() : `${parsed.pathname}${parsed.search}${parsed.hash}`;
}

function withEventPageCursor(url: string, cursor: number): string {
  const isAbsolute = /^[a-z][a-z\d+.-]*:\/\//i.test(url);
  const parsed = new URL(url, "http://agent-events.local");
  parsed.pathname = `${parsed.pathname.replace(/\/$/u, "")}/page`;
  parsed.search = "";
  parsed.searchParams.set("after", String(cursor));
  parsed.searchParams.set("limit", "250");
  return isAbsolute ? parsed.toString() : `${parsed.pathname}${parsed.search}`;
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
