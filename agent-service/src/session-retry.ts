/**
 * 把 ProductFlow 的「重试前面一轮」接到 Pi 的 session 树上：
 * 切在源轮 user 消息之前，再 prompt 同一句。后轮不进模型上下文。
 */

export const PRODUCTFLOW_TURN_CUSTOM_TYPE = "productflow.turn";

const RETRY_IDEMPOTENCY = /^retry:([^:]+):/;

export function retrySourceTurnId(idempotencyKey: string): string | null {
  return RETRY_IDEMPOTENCY.exec(idempotencyKey)?.[1] ?? null;
}

export interface SessionEntryView {
  id: string;
  parentId: string | null;
  type: string;
  customType?: string;
  data?: unknown;
  messageRole?: string;
  messageText?: string;
}

export type RetryBranchAction =
  | { kind: "none" }
  | { kind: "reset" }
  | { kind: "branch"; entryId: string };

export interface RetrySessionManager {
  branch(id: string): void;
  resetLeaf(): void;
  appendCustomEntry(customType: string, data?: unknown): string;
  getEntries(): readonly unknown[];
  getLeafId(): string | null;
}

export function sessionEntryView(entry: unknown): SessionEntryView | null {
  if (!entry || typeof entry !== "object") return null;
  const record = entry as {
    id?: unknown;
    parentId?: unknown;
    type?: unknown;
    customType?: unknown;
    data?: unknown;
    message?: { role?: unknown; content?: unknown };
  };
  if (typeof record.id !== "string") return null;
  const parentId = record.parentId === null || typeof record.parentId === "string" ? record.parentId : null;
  const type = typeof record.type === "string" ? record.type : "";
  const view: SessionEntryView = {
    id: record.id,
    parentId,
    type,
  };
  if (typeof record.customType === "string") view.customType = record.customType;
  if (record.data !== undefined) view.data = record.data;
  if (record.message && typeof record.message === "object") {
    if (typeof record.message.role === "string") view.messageRole = record.message.role;
    view.messageText = messageText(record.message.content);
  }
  return view;
}

export function retryBranchAction(input: {
  idempotencyKey: string;
  inputText: string;
  entries: readonly SessionEntryView[];
  leafId: string | null;
}): RetryBranchAction {
  const sourceId = retrySourceTurnId(input.idempotencyKey);
  if (!sourceId) return { kind: "none" };
  const byId = new Map(input.entries.map((entry) => [entry.id, entry]));
  const marked = findTurnMarker(input.entries, sourceId);
  if (marked) return branchFromParent(marked.parentId, byId);
  const path = leafPath(byId, input.leafId);
  const user = path.find((entry) => entry.type === "message" && entry.messageRole === "user" && userTextMatches(entry.messageText ?? "", input.inputText));
  if (!user) return { kind: "none" };
  return branchFromParent(user.parentId, byId);
}

export function applyRetryBranch(manager: Pick<RetrySessionManager, "branch" | "resetLeaf">, action: RetryBranchAction): void {
  if (action.kind === "reset") manager.resetLeaf();
  if (action.kind === "branch") manager.branch(action.entryId);
}

export function prepareSessionForTurn(manager: RetrySessionManager, input: {
  turnId: string;
  projectionId: string | null;
  idempotencyKey: string;
  inputText: string;
}): RetryBranchAction {
  const entries = manager.getEntries().map(sessionEntryView).filter((entry): entry is SessionEntryView => entry !== null);
  const action = retryBranchAction({
    idempotencyKey: input.idempotencyKey,
    inputText: input.inputText,
    entries,
    leafId: manager.getLeafId(),
  });
  applyRetryBranch(manager, action);
  manager.appendCustomEntry(PRODUCTFLOW_TURN_CUSTOM_TYPE, {
    turn_id: input.turnId,
    projection_id: input.projectionId,
  });
  return action;
}

function findTurnMarker(entries: readonly SessionEntryView[], sourceId: string): SessionEntryView | null {
  for (const entry of entries) {
    if (entry.type !== "custom" || entry.customType !== PRODUCTFLOW_TURN_CUSTOM_TYPE) continue;
    const data = entry.data;
    if (!data || typeof data !== "object") continue;
    const record = data as { turn_id?: unknown; projection_id?: unknown };
    if (record.projection_id === sourceId || record.turn_id === sourceId) return entry;
  }
  return null;
}

function branchFromParent(parentId: string | null, byId: Map<string, SessionEntryView>): RetryBranchAction {
  if (parentId === null) return { kind: "reset" };
  if (!byId.has(parentId)) return { kind: "none" };
  return { kind: "branch", entryId: parentId };
}

function leafPath(byId: Map<string, SessionEntryView>, leafId: string | null): SessionEntryView[] {
  const path: SessionEntryView[] = [];
  const seen = new Set<string>();
  let current = leafId;
  while (current) {
    if (seen.has(current)) break;
    seen.add(current);
    const entry = byId.get(current);
    if (!entry) break;
    path.push(entry);
    current = entry.parentId;
  }
  return path.reverse();
}

function userTextMatches(messageTextValue: string, inputText: string): boolean {
  const prompt = inputText.trim();
  if (!prompt) return false;
  const text = messageTextValue.trim();
  return text === prompt || text.startsWith(`${prompt}\n\nProductFlow selected asset IDs`);
}

function messageText(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return content.map((part) => {
    if (typeof part === "string") return part;
    if (part && typeof part === "object" && "text" in part && typeof (part as { text?: unknown }).text === "string") {
      return (part as { text: string }).text;
    }
    return "";
  }).join("");
}
