/**
 * 确认 WorkflowDraft revision，再 persist 成 live schema-v3 图。
 *
 * 确认和 persist 是两次 ProductFlow 调用；persist 之后图才是工作台权威。浏览器不拼装节点。
 */

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRef } from "react";

import { api, ApiError } from "../../../lib/api";
import type {
  GraphProjection,
  WorkflowDraft,
  WorkflowDraftRevision,
} from "../../../lib/types";

interface StartWorkflowMaterializationInput {
  revision: WorkflowDraftRevision;
  expectedWorkflowRevision: number;
}

interface WorkflowMaterializationMutationResult {
  confirmedDraft: WorkflowDraft;
  graph: GraphProjection;
}

interface WorkflowMaterializationGateway {
  confirmWorkflowDraft: typeof api.confirmWorkflowDraft;
  persistConfirmedDraftGraph: typeof api.persistConfirmedDraftGraph;
}

interface ConfirmAndMaterializeWorkflowInput extends StartWorkflowMaterializationInput {
  productId: string;
  draftId: string;
  onDraftConfirmed?: (draft: WorkflowDraft) => void;
}

interface UseWorkflowMaterializationInput {
  productId: string;
  draftId: string;
  conversationId: string;
  onDraftConfirmed?: (draft: WorkflowDraft) => void;
  onConflict?: () => void | Promise<void>;
  onMaterialized?: (graph: GraphProjection) => void;
}

export class WorkflowMaterializationProtocolError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "WorkflowMaterializationProtocolError";
  }
}

/** 每个 Draft revision 一个 persist 身份，重试不能再建第二张图。 */
export function workflowMaterializationIdempotencyKey(draftId: string, version: number): string {
  return `agent-workspace:${draftId}:v${version}`;
}

/**
 * 先确认正在审阅的 revision，再把它 persist 成 live 图。
 * `expectedWorkflowRevision` 留给调用方合同；persist 本身按 Draft 版本幂等。
 */
export async function confirmAndMaterializeWorkflow(
  {
    productId,
    draftId,
    revision,
    expectedWorkflowRevision,
    onDraftConfirmed,
  }: ConfirmAndMaterializeWorkflowInput,
  gateway: WorkflowMaterializationGateway = api,
): Promise<WorkflowMaterializationMutationResult> {
  if (revision.draft_id !== draftId) {
    throw new WorkflowMaterializationProtocolError("确认的 WorkflowDraft revision 不属于当前草案");
  }
  const confirmedDraft = await gateway.confirmWorkflowDraft(productId, draftId, revision.version);
  const confirmedRevision = confirmedDraft.current_revision;
  if (
    !confirmedRevision ||
    confirmedRevision.id !== revision.id ||
    confirmedRevision.version !== revision.version ||
    !confirmedRevision.confirmed_at
  ) {
    throw new WorkflowMaterializationProtocolError("WorkflowDraft 确认响应与正在审阅的 revision 不一致");
  }
  onDraftConfirmed?.(confirmedDraft);
  void expectedWorkflowRevision;
  const persisted = await gateway.persistConfirmedDraftGraph(productId, draftId, revision.version);
  return { confirmedDraft, graph: persisted.graph };
}

export function useWorkflowMaterialization({
  productId,
  draftId,
  conversationId,
  onDraftConfirmed,
  onConflict,
  onMaterialized,
}: UseWorkflowMaterializationInput) {
  const queryClient = useQueryClient();
  const callbacksRef = useRef({ onDraftConfirmed, onConflict, onMaterialized });
  callbacksRef.current = { onDraftConfirmed, onConflict, onMaterialized };

  return useMutation<
    WorkflowMaterializationMutationResult,
    Error,
    StartWorkflowMaterializationInput
  >({
    mutationFn: ({ revision, expectedWorkflowRevision }) => confirmAndMaterializeWorkflow({
      productId,
      draftId,
      revision,
      expectedWorkflowRevision,
      onDraftConfirmed: callbacksRef.current.onDraftConfirmed,
    }),
    onSuccess: async ({ graph }) => {
      callbacksRef.current.onMaterialized?.(graph);
      queryClient.setQueryData(["workflow-graph", productId], graph);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["agent-workbench", productId] }),
        queryClient.invalidateQueries({ queryKey: ["agent-turns", productId, conversationId] }),
        queryClient.invalidateQueries({ queryKey: ["agent-turn", productId, conversationId] }),
        queryClient.invalidateQueries({ queryKey: ["workflow-draft", productId, draftId] }),
        queryClient.invalidateQueries({ queryKey: ["product", productId] }),
        queryClient.invalidateQueries({ queryKey: ["products"] }),
        queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] }),
      ]);
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.status === 409) {
        await callbacksRef.current.onConflict?.();
      }
    },
  });
}
