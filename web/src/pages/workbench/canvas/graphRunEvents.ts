import type { GraphRun, GraphRunListResponse, WorkflowNodeStatus, WorkflowRunStatus } from "../../../lib/types";

export interface GraphRunEvent {
  schema_version: 1;
  run_id: string;
  sequence: number;
  kind: string;
  node_run_id: string | null;
  payload: Record<string, unknown>;
  created_at: string;
}

export interface GraphRunEventOptions {
  onError?: (error?: Error) => void;
  onOpen?: () => void;
  after?: number;
  createEventSource?: (url: string, init?: EventSourceInit) => GraphEventSourceLike;
}

export interface GraphEventSourceLike {
  addEventListener(type: string, listener: EventListener): void;
  removeEventListener?(type: string, listener: EventListener): void;
  close(): void;
}

/** 订阅单个图运行的追加事件。连接错误交给调用方显示，不再启动隐藏轮询。 */
export function subscribeGraphRunEvents(
  url: string,
  onEvent: (event: GraphRunEvent) => void,
  options: GraphRunEventOptions = {},
): () => void {
  if (typeof EventSource === "undefined" && !options.createEventSource) {
    options.onError?.(new Error("当前浏览器不支持图运行事件流"));
    return () => undefined;
  }
  const factory = options.createEventSource ?? ((nextURL, init) => new EventSource(nextURL, init));
  const source = factory(withCursor(url, options.after ?? 0), { withCredentials: true });
  let cursor = options.after ?? 0;
  let terminal = false;
  const handleOpen = () => options.onOpen?.();
  const handleError = () => {
    if (!terminal) options.onError?.(new Error("图运行事件流连接失败"));
  };
  const handle = (event: Event) => {
    const data = (event as Event & { data?: unknown }).data;
    if (typeof data !== "string") {
      options.onError?.(new Error("图运行事件缺少 data"));
      return;
    }
    try {
      const parsed = parseGraphRunEvent(data);
      if (parsed.sequence <= cursor) return;
      cursor = parsed.sequence;
      onEvent(parsed);
      if (isTerminalRunEventKind(parsed.kind)) {
        terminal = true;
        source.close();
      }
    } catch (error) {
      options.onError?.(error instanceof Error ? error : new Error("图运行事件无效"));
    }
  };
  source.addEventListener("open", handleOpen);
  source.addEventListener("error", handleError);
  source.addEventListener("run.event", handle);
  return () => {
    source.removeEventListener?.("open", handleOpen);
    source.removeEventListener?.("error", handleError);
    source.removeEventListener?.("run.event", handle);
    source.close();
  };
}

function isTerminalRunEventKind(kind: string): boolean {
  return kind === "run.completed" || kind === "run.failed" || kind === "run.cancelled" || kind === "run.unknown";
}

export function applyGraphRunEvent(
  previous: GraphRunListResponse | undefined,
  event: GraphRunEvent,
): GraphRunListResponse | undefined {
  if (!previous) return previous;
  const index = previous.items.findIndex((run) => run.id === event.run_id);
  if (index < 0) return previous;
  const current = previous.items[index];
  const next = applyGraphRunEventToRun(current, event);
  if (next === current) return previous;
  const items = previous.items.slice();
  items[index] = next;
  return { ...previous, items };
}

export function applyGraphRunEventToRun(run: GraphRun, event: GraphRunEvent): GraphRun {
  if (run.id !== event.run_id) return run;
  const payload = event.payload;
  if (event.kind.startsWith("run.")) {
    const status = workflowRunStatus(payload.status);
    const failureReason = nullableString(payload.failure_reason ?? payload.reason);
    const finishedAt = status && isTerminalRunStatus(status) ? event.created_at : run.finished_at;
    if (!status && failureReason === undefined && finishedAt === run.finished_at) return run;
    return {
      ...run,
      ...(status ? { status } : {}),
      ...(failureReason !== undefined ? { failure_reason: failureReason } : {}),
      ...(finishedAt !== run.finished_at ? { finished_at: finishedAt } : {}),
    };
  }
  if (!event.node_run_id) return run;
  const nodeIndex = run.node_runs.findIndex((node) => node.id === event.node_run_id);
  if (nodeIndex < 0) return run;
  const current = run.node_runs[nodeIndex];
  const status = workflowNodeStatus(payload.status);
  const attemptCount = typeof payload.attempt_count === "number"
    && Number.isSafeInteger(payload.attempt_count)
    && payload.attempt_count >= 0
    ? payload.attempt_count
    : current.attempt_count;
  const failureReason = nullableString(payload.reason ?? payload.failure_reason);
  const terminal = isTerminalNodeStatus(status);
  const progressPhase = terminal
    ? null
    : typeof payload.phase === "string" ? payload.phase : current.progress_phase;
  const output = isRecord(payload.output) ? payload.output : current.output;
  const next = {
    ...current,
    ...(status ? { status } : {}),
    ...(attemptCount !== current.attempt_count ? { attempt_count: attemptCount } : {}),
    ...(failureReason !== undefined ? { failure_reason: failureReason } : {}),
    ...(progressPhase !== current.progress_phase ? { progress_phase: progressPhase } : {}),
    ...(output !== current.output ? { output } : {}),
    ...(terminal ? { finished_at: event.created_at } : {}),
  };
  if (sameNodeRun(current, next)) return run;
  const nodeRuns = run.node_runs.slice();
  nodeRuns[nodeIndex] = next;
  return { ...run, node_runs: nodeRuns };
}

function parseGraphRunEvent(raw: string): GraphRunEvent {
  let value: unknown;
  try {
    value = JSON.parse(raw) as unknown;
  } catch {
    throw new Error("图运行事件不是有效 JSON");
  }
  if (!isRecord(value) || value.schema_version !== 1 || typeof value.run_id !== "string" || !value.run_id) {
    throw new Error("图运行事件 schema 或 run_id 无效");
  }
  if (typeof value.sequence !== "number" || !Number.isSafeInteger(value.sequence) || value.sequence <= 0 || typeof value.kind !== "string") {
    throw new Error("图运行事件 sequence 或 kind 无效");
  }
  if (!isRecord(value.payload) || typeof value.created_at !== "string" || !value.created_at) {
    throw new Error("图运行事件 payload 或 created_at 无效");
  }
  return {
    schema_version: 1,
    run_id: value.run_id,
    sequence: value.sequence,
    kind: value.kind,
    node_run_id: typeof value.node_run_id === "string" ? value.node_run_id : null,
    payload: value.payload,
    created_at: value.created_at,
  };
}

function withCursor(url: string, cursor: number): string {
  const absolute = /^[a-z][a-z\d+.-]*:\/\//i.test(url);
  const parsed = new URL(url, "http://graph-events.local");
  parsed.searchParams.set("after", String(cursor));
  return absolute ? parsed.toString() : `${parsed.pathname}${parsed.search}${parsed.hash}`;
}

function workflowRunStatus(value: unknown): WorkflowRunStatus | null {
  return value === "queued" || value === "running" || value === "succeeded" || value === "failed" || value === "cancelled" || value === "unknown"
    ? value
    : null;
}

function workflowNodeStatus(value: unknown): WorkflowNodeStatus | null {
  return value === "queued" || value === "running" || value === "succeeded" || value === "failed" || value === "cancelled" || value === "unknown" || value === "skipped"
    ? value
    : null;
}

function isTerminalRunStatus(status: WorkflowRunStatus): boolean {
  return status === "succeeded" || status === "failed" || status === "cancelled" || status === "unknown";
}

function isTerminalNodeStatus(status: WorkflowNodeStatus | null): boolean {
  return status === "succeeded" || status === "failed" || status === "cancelled" || status === "unknown" || status === "skipped";
}

function nullableString(value: unknown): string | null | undefined {
  if (value === null) return null;
  return typeof value === "string" ? value : undefined;
}

function sameNodeRun(left: GraphRun["node_runs"][number], right: GraphRun["node_runs"][number]): boolean {
  return left.status === right.status
    && left.attempt_count === right.attempt_count
    && left.failure_reason === right.failure_reason
    && left.progress_phase === right.progress_phase
    && left.output === right.output
    && left.finished_at === right.finished_at;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}
