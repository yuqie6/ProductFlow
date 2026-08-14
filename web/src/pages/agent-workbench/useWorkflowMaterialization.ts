import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRef } from "react";

import { api, ApiError } from "../../lib/api";
import type {
  ActiveProductWorkflowV2,
  MaterializeWorkflowDraftInput,
  WorkflowDraft,
  WorkflowDraftRevision,
  WorkflowMaterializationResult,
} from "../../lib/types";

interface StartWorkflowMaterializationInput {
  revision: WorkflowDraftRevision;
  expectedWorkflowRevision: number;
}

interface WorkflowMaterializationMutationResult {
  confirmedDraft: WorkflowDraft;
  materialization: WorkflowMaterializationResult;
}

interface WorkflowMaterializationGateway {
  confirmWorkflowDraft: typeof api.confirmWorkflowDraft;
  materializeWorkflowDraft: typeof api.materializeWorkflowDraft;
}

interface ConfirmAndMaterializeWorkflowInput extends StartWorkflowMaterializationInput {
  productId: string;
  draftId: string;
  onDraftConfirmed?: (draft: WorkflowDraft) => void;
}

interface UseWorkflowMaterializationInput {
  productId: string;
  draftId: string;
  onDraftConfirmed?: (draft: WorkflowDraft) => void;
  onConflict?: () => void | Promise<void>;
  onMaterialized?: (result: WorkflowMaterializationResult) => void;
}

export class WorkflowMaterializationProtocolError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "WorkflowMaterializationProtocolError";
  }
}

export function workflowMaterializationIdempotencyKey(draftId: string, version: number): string {
  return `agent-workspace:${draftId}:v${version}`;
}

export function workflowMaterializationInput(
  draftId: string,
  draftVersion: number,
  expectedWorkflowRevision: number,
): MaterializeWorkflowDraftInput {
  return {
    expected_draft_version: draftVersion,
    expected_workflow_revision: expectedWorkflowRevision,
    idempotency_key: workflowMaterializationIdempotencyKey(draftId, draftVersion),
  };
}

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
  const materialization = await gateway.materializeWorkflowDraft(
    productId,
    draftId,
    workflowMaterializationInput(draftId, revision.version, expectedWorkflowRevision),
  );
  return { confirmedDraft, materialization };
}

export function useWorkflowMaterialization({
  productId,
  draftId,
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
    onSuccess: async ({ materialization }) => {
      callbacksRef.current.onMaterialized?.(materialization);
      queryClient.setQueryData<ActiveProductWorkflowV2>(
        ["active-product-workflow-v2", productId],
        { latest_revision: materialization.workflow.revision, workflow: materialization.workflow },
      );
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["agent-workbench", productId] }),
        queryClient.invalidateQueries({ queryKey: ["workflow-draft", productId, draftId] }),
        queryClient.invalidateQueries({ queryKey: ["product", productId] }),
        queryClient.invalidateQueries({ queryKey: ["products"] }),
      ]);
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.status === 409) {
        await callbacksRef.current.onConflict?.();
      }
    },
  });
}
