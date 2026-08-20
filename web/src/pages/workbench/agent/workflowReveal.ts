import type { JsonValue, ProductWorkflowV2, WorkflowRevealEvent } from "../../../lib/types";
import type { WorkflowRevealVisibility } from "../canvas/V2WorkflowCanvas";

export type WorkflowRevealStreamReader = (
  after: number,
  onChunk: (chunk: string) => void,
  signal: AbortSignal,
) => Promise<void>;

export class WorkflowRevealProtocolError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "WorkflowRevealProtocolError";
  }
}

interface WorkflowRevealScope {
  materializationId: string;
  workflow: ProductWorkflowV2;
}

interface WorkflowRevealCollectorInput extends WorkflowRevealScope {
  read: WorkflowRevealStreamReader;
  signal: AbortSignal;
  maxAttempts?: number;
  retryDelayMs?: number;
}

export class WorkflowRevealSseParser {
  private buffer = "";
  private readonly folderIds: Set<string>;
  private readonly nodeIds: Set<string>;
  private readonly edgeIds: Set<string>;
  private readonly seenFolderIds = new Set<string>();
  private readonly seenNodeIds = new Set<string>();
  private readonly seenEdgeIds = new Set<string>();
  private completed = false;
  private lastPhase = 0;

  cursor = 0;

  constructor(private readonly scope: WorkflowRevealScope) {
    this.folderIds = new Set(scope.workflow.folders.map((folder) => folder.id));
    this.nodeIds = new Set(scope.workflow.nodes.map((node) => node.id));
    this.edgeIds = new Set(scope.workflow.edges.map((edge) => edge.id));
  }

  push(chunk: string): WorkflowRevealEvent[] {
    if (!chunk) return [];
    this.buffer += chunk;
    const events: WorkflowRevealEvent[] = [];
    while (true) {
      const separator = this.buffer.match(/\r?\n\r?\n/);
      if (!separator || separator.index === undefined) break;
      const block = this.buffer.slice(0, separator.index);
      this.buffer = this.buffer.slice(separator.index + separator[0].length);
      const event = this.parseBlock(block);
      if (event) events.push(event);
    }
    return events;
  }

  hasPartialFrame(): boolean {
    return Boolean(this.buffer.trim());
  }

  discardPartialFrame(): void {
    this.buffer = "";
  }

  isCompleted(): boolean {
    return this.completed;
  }

  private parseBlock(block: string): WorkflowRevealEvent | null {
    if (!block.trim()) return null;
    let eventName: string | null = null;
    let eventId: string | null = null;
    const dataLines: string[] = [];
    for (const line of block.split(/\r?\n/)) {
      if (!line || line.startsWith(":")) continue;
      const separator = line.indexOf(":");
      const field = separator < 0 ? line : line.slice(0, separator);
      const rawValue = separator < 0 ? "" : line.slice(separator + 1);
      const value = rawValue.startsWith(" ") ? rawValue.slice(1) : rawValue;
      if (field === "event") eventName = value;
      if (field === "id") eventId = value;
      if (field === "data") dataLines.push(value);
    }
    if (!dataLines.length && eventName === null && eventId === null) return null;
    if (!eventName || !eventId || !dataLines.length) {
      throw new WorkflowRevealProtocolError("工作流 reveal 事件缺少 event、id 或 data");
    }

    let candidate: unknown;
    try {
      candidate = JSON.parse(dataLines.join("\n"));
    } catch {
      throw new WorkflowRevealProtocolError("工作流 reveal 事件不是有效 JSON");
    }
    const event = parseWorkflowRevealEvent(candidate, eventName, eventId, this.scope);
    if (event.sequence <= this.cursor) return null;
    if (this.completed) {
      throw new WorkflowRevealProtocolError("工作流 reveal completed 之后仍有事件");
    }
    if (event.sequence !== this.cursor + 1) {
      throw new WorkflowRevealProtocolError(
        `工作流 reveal sequence 不连续：期望 ${this.cursor + 1}，收到 ${event.sequence}`,
      );
    }

    const phase = revealPhase(event.kind);
    if (phase < this.lastPhase) {
      throw new WorkflowRevealProtocolError("工作流 reveal 实体顺序无效");
    }
    this.lastPhase = phase;
    this.rememberEntity(event);
    this.cursor = event.sequence;
    if (event.kind === "completed") {
      this.assertCompleteCoverage();
      this.completed = true;
    }
    return event;
  }

  private rememberEntity(event: WorkflowRevealEvent): void {
    if (event.kind === "completed") return;
    const entityId = event.entity_id as string;
    const seen = event.kind === "folder"
      ? this.seenFolderIds
      : event.kind === "node"
        ? this.seenNodeIds
        : this.seenEdgeIds;
    if (seen.has(entityId)) {
      throw new WorkflowRevealProtocolError(`工作流 reveal 重复实体：${event.kind}/${entityId}`);
    }
    seen.add(entityId);
  }

  private assertCompleteCoverage(): void {
    if (
      this.seenFolderIds.size !== this.folderIds.size ||
      this.seenNodeIds.size !== this.nodeIds.size ||
      this.seenEdgeIds.size !== this.edgeIds.size
    ) {
      throw new WorkflowRevealProtocolError("工作流 reveal completed 前缺少持久化实体事件");
    }
  }
}

function revealPhase(kind: WorkflowRevealEvent["kind"]): number {
  if (kind === "folder") return 1;
  if (kind === "node") return 2;
  if (kind === "edge") return 3;
  return 4;
}

export async function collectWorkflowRevealEvents({
  materializationId,
  workflow,
  read,
  signal,
  maxAttempts = 3,
  retryDelayMs = 180,
}: WorkflowRevealCollectorInput): Promise<WorkflowRevealEvent[]> {
  const parser = new WorkflowRevealSseParser({ materializationId, workflow });
  const events: WorkflowRevealEvent[] = [];
  let attempts = 0;

  while (!parser.isCompleted()) {
    attempts += 1;
    try {
      await read(
        parser.cursor,
        (chunk) => events.push(...parser.push(chunk)),
        signal,
      );
      if (parser.hasPartialFrame()) {
        parser.discardPartialFrame();
        if (attempts >= maxAttempts) {
          throw new WorkflowRevealProtocolError("工作流 reveal 事件帧不完整");
        }
        await abortableDelay(retryDelayMs, signal);
        continue;
      }
      if (parser.isCompleted()) break;
      if (attempts >= maxAttempts) {
        throw new WorkflowRevealProtocolError("工作流 reveal 事件流未包含 completed");
      }
    } catch (error) {
      if (signal.aborted || isAbortError(error)) throw error;
      if (error instanceof WorkflowRevealProtocolError) throw error;
      parser.discardPartialFrame();
      if (attempts >= maxAttempts) throw error;
    }
    await abortableDelay(retryDelayMs, signal);
  }
  return events;
}

export function emptyWorkflowRevealVisibility(): WorkflowRevealVisibility {
  return {
    folderIds: new Set(),
    nodeIds: new Set(),
    edgeIds: new Set(),
  };
}

export function applyWorkflowRevealEvent(
  visibility: WorkflowRevealVisibility,
  event: WorkflowRevealEvent,
): WorkflowRevealVisibility {
  if (event.kind === "completed") return visibility;
  const entityId = event.entity_id as string;
  return {
    folderIds: event.kind === "folder"
      ? new Set([...visibility.folderIds, entityId])
      : visibility.folderIds,
    nodeIds: event.kind === "node"
      ? new Set([...visibility.nodeIds, entityId])
      : visibility.nodeIds,
    edgeIds: event.kind === "edge"
      ? new Set([...visibility.edgeIds, entityId])
      : visibility.edgeIds,
  };
}

export function workflowRevealEventDelay(kind: WorkflowRevealEvent["kind"]): number {
  if (kind === "folder") return 90;
  if (kind === "node") return 105;
  if (kind === "edge") return 55;
  return 80;
}

export function shouldAnimateWorkflowReveal(input: {
  enabled: boolean;
  created: boolean;
  reducedMotion: boolean;
}): boolean {
  return input.enabled && input.created && !input.reducedMotion;
}

function parseWorkflowRevealEvent(
  candidate: unknown,
  eventName: string,
  eventId: string,
  scope: WorkflowRevealScope,
): WorkflowRevealEvent {
  if (!isRecord(candidate)) {
    throw new WorkflowRevealProtocolError("工作流 reveal data 必须是对象");
  }
  const { schema_version, materialization_id, sequence, kind, entity_type, entity_id, payload, created_at } = candidate;
  if (schema_version !== 1) {
    throw new WorkflowRevealProtocolError("工作流 reveal schema_version 不受支持");
  }
  if (materialization_id !== scope.materializationId) {
    throw new WorkflowRevealProtocolError("工作流 reveal materialization_id 不匹配");
  }
  if (!Number.isInteger(sequence) || (sequence as number) <= 0 || eventId !== String(sequence)) {
    throw new WorkflowRevealProtocolError("工作流 reveal id 或 sequence 无效");
  }
  if (kind !== "folder" && kind !== "node" && kind !== "edge" && kind !== "completed") {
    throw new WorkflowRevealProtocolError(`未知工作流 reveal 事件：${String(kind)}`);
  }
  if (eventName !== kind) {
    throw new WorkflowRevealProtocolError("工作流 reveal event 名称与 data.kind 不匹配");
  }
  if (!isJsonRecord(payload) || typeof created_at !== "string" || !created_at) {
    throw new WorkflowRevealProtocolError("工作流 reveal payload 或 created_at 无效");
  }

  const expectedEntityType = kind === "completed" ? "workflow" : kind;
  if (entity_type !== expectedEntityType || typeof entity_id !== "string" || !entity_id) {
    throw new WorkflowRevealProtocolError("工作流 reveal entity scope 无效");
  }
  const validEntity = kind === "folder"
    ? scope.workflow.folders.some((folder) => folder.id === entity_id)
    : kind === "node"
      ? scope.workflow.nodes.some((node) => node.id === entity_id)
      : kind === "edge"
        ? scope.workflow.edges.some((edge) => edge.id === entity_id)
        : entity_id === scope.workflow.id &&
          payload.workflow_id === scope.workflow.id &&
          payload.workflow_revision === scope.workflow.revision;
  if (!validEntity) {
    throw new WorkflowRevealProtocolError(`工作流 reveal 引用了未知实体：${kind}/${entity_id}`);
  }

  return {
    schema_version: 1,
    materialization_id: scope.materializationId,
    sequence: sequence as number,
    kind,
    entity_type: expectedEntityType,
    entity_id,
    payload,
    created_at,
  };
}

export function abortableDelay(milliseconds: number, signal: AbortSignal): Promise<void> {
  if (signal.aborted) return Promise.reject(abortError(signal));
  if (milliseconds <= 0) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const handleAbort = () => {
      clearTimeout(timer);
      reject(abortError(signal));
    };
    const timer = setTimeout(() => {
      signal.removeEventListener("abort", handleAbort);
      resolve();
    }, milliseconds);
    signal.addEventListener("abort", handleAbort, { once: true });
  });
}

function abortError(signal: AbortSignal): Error {
  const error = new Error(signal.reason instanceof Error ? signal.reason.message : "Aborted");
  error.name = "AbortError";
  return error;
}

function isAbortError(error: unknown): boolean {
  return error instanceof Error && error.name === "AbortError";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isJsonRecord(value: unknown): value is Record<string, JsonValue> {
  return isRecord(value) && Object.values(value).every(isJsonValue);
}

function isJsonValue(value: unknown): value is JsonValue {
  if (value === null || typeof value === "string" || typeof value === "boolean") return true;
  if (typeof value === "number") return Number.isFinite(value);
  if (Array.isArray(value)) return value.every(isJsonValue);
  return isJsonRecord(value);
}
