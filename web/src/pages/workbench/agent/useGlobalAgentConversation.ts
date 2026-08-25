import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef } from "react";

import { api } from "../../../lib/api";
import type {
  AgentPageContextSnapshotInput,
  AgentQuestionAnswer,
  AgentTaskStatus,
  AgentTurn,
  AgentTurnPage,
  AgentWorkflowRunRequest,
  LibraryOrganizationDraft,
  SubmitAgentTurnInput,
} from "../../../lib/types";
import {
  flattenAgentTurnPages,
  selectNewestAgentTurnProjection,
  upsertAgentTurnPageData,
} from "./useAgentConversation";
import { isAgentTurnTerminal } from "./agentEventReducer";

const PAGE_SIZE = 20;
const PROJECTION_POLL_MS = 1_500;

interface UseGlobalAgentConversationInput {
  conversationId: string;
  taskId?: string | null;
  taskGoal?: string | null;
  taskStatus?: AgentTaskStatus | null;
  pageContext?: AgentPageContextSnapshotInput | null;
  enabled?: boolean;
}

interface AnswerQuestionInput {
  projectionId: string;
  questionId: string;
  answer: AgentQuestionAnswer;
}

export function globalAgentTurnsQueryKey(conversationId: string, taskId?: string | null) {
  return ["global-agent-turns", conversationId, taskId ?? null] as const;
}

export function globalAgentTurnQueryKey(conversationId: string, projectionId: string) {
  return ["global-agent-turn", conversationId, projectionId] as const;
}

export function globalLibraryOrganizationDraftQueryKey(conversationId: string) {
  return ["global-library-organization-draft", conversationId] as const;
}

export function globalWorkflowRunRequestQueryKey(conversationId: string, taskId?: string | null) {
  return ["global-workflow-run-request", conversationId, taskId ?? null] as const;
}

export function initialGlobalTaskTurnInput(
  conversationId: string,
  taskId: string,
  taskGoal: string,
  pageContext?: AgentPageContextSnapshotInput | null,
): SubmitAgentTurnInput {
  return {
    input_text: taskGoal.trim(),
    asset_ids: [],
    task_id: taskId,
    idempotency_key: `initial:${conversationId}:${taskId}`,
    page_context: pageContext
      ? { ...pageContext, captured_at: new Date().toISOString() }
      : null,
  };
}

export function selectLatestGlobalAgentTurn(
  turns: readonly AgentTurn[],
  taskId: string | null | undefined,
): AgentTurn | null {
  for (let index = turns.length - 1; index >= 0; index -= 1) {
    const turn = turns[index];
    if (turn.task_id === (taskId ?? null)) {
      return turn;
    }
  }
  return null;
}

export function useGlobalAgentConversation({
  conversationId,
  taskId = null,
  taskGoal = null,
  taskStatus = null,
  pageContext = null,
  enabled = true,
}: UseGlobalAgentConversationInput) {
  const queryClient = useQueryClient();
  const autoStartKeyRef = useRef<string | null>(null);
  const turnsKey = useMemo(
    () => globalAgentTurnsQueryKey(conversationId, taskId),
    [conversationId, taskId],
  );

  const turnsQuery = useInfiniteQuery({
    queryKey: turnsKey,
    queryFn: ({ pageParam }) =>
      api.listGlobalAgentTurns(conversationId, {
        after: pageParam,
        limit: PAGE_SIZE,
        taskId,
      }),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: Boolean(enabled && conversationId),
  });
  const pageTurns = useMemo(
    () => flattenAgentTurnPages(turnsQuery.data?.pages),
    [turnsQuery.data?.pages],
  );
  const interactionTurns = useMemo(
    () => pageTurns.filter((turn) => turn.task_id === (taskId ?? null)),
    [pageTurns, taskId],
  );
  const latestPageTurn = selectLatestGlobalAgentTurn(interactionTurns, taskId);
  const latestProjectionQuery = useQuery({
    queryKey: globalAgentTurnQueryKey(conversationId, latestPageTurn?.id ?? "none"),
    queryFn: () => api.getGlobalAgentTurn(conversationId, latestPageTurn?.id ?? ""),
    enabled: Boolean(enabled && conversationId && latestPageTurn && !isAgentTurnTerminal(latestPageTurn.status)),
    refetchInterval: (query) => {
      const projection = query.state.data;
      return projection && isAgentTurnTerminal(projection.status) ? false : PROJECTION_POLL_MS;
    },
  });
  const latestTurn = selectNewestAgentTurnProjection(latestPageTurn, latestProjectionQuery.data);
  const libraryOrganizationDraftQuery = useQuery({
    queryKey: globalLibraryOrganizationDraftQueryKey(conversationId),
    queryFn: () => api.getGlobalLibraryOrganizationDraft(conversationId),
    enabled: Boolean(
      enabled && conversationId && interactionTurns.some((turn) => turn.library_organization_draft_revision_id),
    ),
  });
  const workflowRunRequestId = useMemo(
    () => [...interactionTurns].reverse().find((turn) => turn.workflow_run_request_id)?.workflow_run_request_id ?? null,
    [interactionTurns],
  );
  const workflowRunRequestQuery = useQuery({
    queryKey: globalWorkflowRunRequestQueryKey(conversationId, taskId),
    queryFn: () => api.getGlobalWorkflowRunRequest(conversationId, taskId),
    enabled: Boolean(enabled && conversationId && workflowRunRequestId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "awaiting_confirmation" || status === "confirmed" ? 1_500 : false;
    },
  });
  const turns = useMemo(
    () =>
      latestTurn && latestPageTurn && latestTurn.id === latestPageTurn.id
        ? pageTurns.map((turn) => (turn.id === latestTurn.id ? latestTurn : turn))
        : pageTurns,
    [latestPageTurn, latestTurn, pageTurns],
  );

  const cacheTurn = useCallback(
    (turn: AgentTurn) => {
      queryClient.setQueryData<InfiniteData<AgentTurnPage, string | null>>(turnsKey, (current) =>
        upsertAgentTurnPageData(current, turn),
      );
      queryClient.setQueryData(globalAgentTurnQueryKey(conversationId, turn.id), turn);
    },
    [conversationId, queryClient, turnsKey],
  );

  const submitTurnMutation = useMutation({
    mutationFn: (input: SubmitAgentTurnInput) =>
      api.submitGlobalAgentTurn(conversationId, {
        ...input,
        task_id: input.task_id ?? taskId,
        page_context: input.page_context ?? pageContext,
      }),
    onSuccess: (response) => cacheTurn(response.turn),
    onSettled: () => Promise.all([
      queryClient.invalidateQueries({ queryKey: turnsKey }),
      queryClient.invalidateQueries({ queryKey: ["agent-sessions"] }),
    ]),
  });
  const initialTaskKey = taskId && taskStatus === "queued" ? `${conversationId}:${taskId}` : null;
  useEffect(() => {
    if (
      !enabled ||
      !initialTaskKey ||
      !taskId ||
      !taskGoal?.trim() ||
      !turnsQuery.isSuccess ||
      pageTurns.length > 0 ||
      submitTurnMutation.isPending ||
      autoStartKeyRef.current === initialTaskKey
    ) {
      return;
    }
    autoStartKeyRef.current = initialTaskKey;
    submitTurnMutation.mutate(
      initialGlobalTaskTurnInput(conversationId, taskId, taskGoal, pageContext),
    );
  }, [
    conversationId,
    enabled,
    initialTaskKey,
    pageContext,
    pageTurns.length,
    submitTurnMutation,
    taskGoal,
    taskId,
    turnsQuery.isSuccess,
  ]);
  const cancelTurnMutation = useMutation({
    mutationFn: (projectionId: string) => api.cancelGlobalAgentTurn(conversationId, projectionId),
    onSuccess: cacheTurn,
    onSettled: () => queryClient.invalidateQueries({ queryKey: turnsKey }),
  });
  const resumeTurnMutation = useMutation({
    mutationFn: (projectionId: string) => api.resumeGlobalAgentTurn(conversationId, projectionId),
    onSuccess: cacheTurn,
    onSettled: () => queryClient.invalidateQueries({ queryKey: turnsKey }),
  });
  const answerQuestionMutation = useMutation({
    mutationFn: async ({ projectionId, questionId, answer }: AnswerQuestionInput) => {
      const result = await api.answerGlobalAgentQuestion(
        conversationId,
        projectionId,
        questionId,
        answer,
      );
      cacheTurn(result.answered_turn);
      cacheTurn(result.continuation_turn);
      return {
        answered: result.answered_turn,
        continuation: result.continuation_turn,
      };
    },
    onSuccess: ({ answered, continuation }) => {
      cacheTurn(answered);
      cacheTurn(continuation);
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: turnsKey }),
  });
  const confirmLibraryOrganizationDraftMutation = useMutation<
    LibraryOrganizationDraft,
    Error,
    { expectedDraftVersion: number; idempotencyKey: string }
  >({
    mutationFn: ({ expectedDraftVersion, idempotencyKey }) =>
      api.confirmGlobalLibraryOrganizationDraft(
        conversationId,
        expectedDraftVersion,
        idempotencyKey,
      ),
    onSuccess: (draft) => {
      queryClient.setQueryData(globalLibraryOrganizationDraftQueryKey(conversationId), draft);
      void queryClient.invalidateQueries({ queryKey: turnsKey });
      void queryClient.invalidateQueries({ queryKey: ["agent-tasks"] });
      void queryClient.invalidateQueries({ queryKey: ["agent-sessions"] });
    },
  });
  const confirmWorkflowRunRequestMutation = useMutation<AgentWorkflowRunRequest, Error, string>({
    mutationFn: (requestId) => api.confirmGlobalWorkflowRunRequest(conversationId, requestId),
    onSuccess: (request) => {
      queryClient.setQueryData(globalWorkflowRunRequestQueryKey(conversationId, taskId), request);
      void queryClient.invalidateQueries({ queryKey: turnsKey });
      void queryClient.invalidateQueries({ queryKey: ["agent-tasks"] });
      void queryClient.invalidateQueries({ queryKey: ["agent-sessions"] });
    },
  });
  const cancelWorkflowRunRequestMutation = useMutation<AgentWorkflowRunRequest, Error, string>({
    mutationFn: (requestId) => api.cancelGlobalWorkflowRunRequest(conversationId, requestId),
    onSuccess: (request) => {
      queryClient.setQueryData(globalWorkflowRunRequestQueryKey(conversationId, taskId), request);
      void queryClient.invalidateQueries({ queryKey: turnsKey });
      void queryClient.invalidateQueries({ queryKey: ["agent-tasks"] });
      void queryClient.invalidateQueries({ queryKey: ["agent-sessions"] });
    },
  });

  return {
    turns,
    latestTurn,
    activeTurn: latestTurn && !isAgentTurnTerminal(latestTurn.status) ? latestTurn : null,
    turnsQuery,
    latestProjectionQuery,
    refreshLatestTurn: latestProjectionQuery.refetch,
    libraryOrganizationDraftQuery,
    workflowRunRequestId,
    workflowRunRequestQuery,
    submitTurnMutation,
    cancelTurnMutation,
    resumeTurnMutation,
    answerQuestionMutation,
    confirmLibraryOrganizationDraftMutation,
    confirmWorkflowRunRequestMutation,
    cancelWorkflowRunRequestMutation,
  };
}
