import { ApiError } from "../../lib/api";
import type { AgentProductWorkspaceSnapshot } from "../../lib/types";

export interface PendingDraftState {
  name: string;
  idempotencyKey: string;
  conversationId?: string;
}

interface SubmitAgentProductIntakeInput {
  workspace: AgentProductWorkspaceSnapshot | null;
  createWorkspace: () => Promise<AgentProductWorkspaceSnapshot>;
  retainWorkspace: (workspace: AgentProductWorkspaceSnapshot) => void | Promise<void>;
  finalizeWorkspace: (
    workspace: AgentProductWorkspaceSnapshot,
  ) => Promise<AgentProductWorkspaceSnapshot>;
}

export function parsePendingDraft(raw: string | null): PendingDraftState | null {
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as Partial<PendingDraftState>;
    if (typeof parsed.name !== "string" || typeof parsed.idempotencyKey !== "string") {
      return null;
    }
    return {
      name: parsed.name,
      idempotencyKey: parsed.idempotencyKey,
      ...(typeof parsed.conversationId === "string" && parsed.conversationId.trim()
        ? { conversationId: parsed.conversationId }
        : {}),
    };
  } catch {
    return null;
  }
}

export function isAmbiguousFinalizeError(error: unknown): boolean {
  return !(error instanceof ApiError) || error.status >= 500;
}

export function resolveWorkspaceRestorationId(
  urlWorkspaceId: string | null | undefined,
  pendingDraft: PendingDraftState | null,
): string {
  return urlWorkspaceId?.trim() || pendingDraft?.conversationId?.trim() || "";
}

export async function submitAgentProductIntake({
  workspace,
  createWorkspace,
  retainWorkspace,
  finalizeWorkspace,
}: SubmitAgentProductIntakeInput): Promise<AgentProductWorkspaceSnapshot> {
  const targetWorkspace = workspace ?? (await createWorkspace());
  if (!workspace) await retainWorkspace(targetWorkspace);
  return finalizeWorkspace(targetWorkspace);
}
