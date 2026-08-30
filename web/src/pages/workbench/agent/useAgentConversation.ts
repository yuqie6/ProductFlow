import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query";
import { useCallback, useEffect, useMemo } from "react";

import { api } from "../../../lib/api";
import type {
  AgentConversation,
  AgentPageContextSnapshotInput,
  AgentQuestionAnswer,
  AgentTurn,
  AgentTurnPage,
  GraphProjection,
  SubmitAgentTurnInput,
} from "../../../lib/types";
import { isAgentTurnTerminal } from "./agentEventReducer";

const AGENT_TURN_PAGE_SIZE = 20;
const AGENT_TURN_PROJECTION_POLL_MS = 1_500;

interface UseAgentConversationInput {
  productId: string;
  conversation: AgentConversation;
  graph?: GraphProjection | null;
  taskId?: string | null;
  pageContext?: AgentPageContextSnapshotInput | null;
  enabled?: boolean;
}

interface AnswerQuestionInput {
  projectionId: string;
  questionId: string;
  answer: AgentQuestionAnswer;
}

interface AnswerQuestionResult {
  answered: AgentTurn;
  continuation: AgentTurn;
}

export function agentTurnsQueryKey(productId: string, conversationId: string, taskId?: string | null) {
  return ["agent-turns", productId, conversationId, taskId ?? null] as const;
}

export function agentTurnQueryKey(productId: string, conversationId: string, projectionId: string) {
  return ["agent-turn", productId, conversationId, projectionId] as const;
}

export function flattenAgentTurnPages(pages: readonly AgentTurnPage[] | undefined): AgentTurn[] {
  if (!pages?.length) {
    return [];
  }
  const result: AgentTurn[] = [];
  const indexes = new Map<string, number>();
  [...pages].reverse().forEach((page) => {
    page.items.forEach((turn) => {
      const existingIndex = indexes.get(turn.id);
      if (existingIndex === undefined) {
        indexes.set(turn.id, result.length);
        result.push(turn);
      } else {
        result[existingIndex] = turn;
      }
    });
  });
  return result;
}

export function upsertAgentTurnPageData(
  current: InfiniteData<AgentTurnPage, string | null> | undefined,
  turn: AgentTurn,
): InfiniteData<AgentTurnPage, string | null> {
  if (!current?.pages.length) {
    return {
      pages: [{ items: [turn], next_cursor: null }],
      pageParams: [null],
    };
  }
  let found = false;
  const pages = current.pages.map((page) => ({
    ...page,
    items: page.items.map((item) => {
      if (item.id !== turn.id) {
        return item;
      }
      found = true;
      return turn;
    }),
  }));
  if (!found) {
    pages[0] = {
      ...pages[0],
      items: [...pages[0].items, turn].sort(compareAgentTurns),
    };
  }
  return { ...current, pages };
}

export function selectNewestAgentTurnProjection(
  pageTurn: AgentTurn | null,
  detailTurn: AgentTurn | undefined,
): AgentTurn | null {
  if (!pageTurn || !detailTurn || pageTurn.id !== detailTurn.id) {
    return pageTurn ?? detailTurn ?? null;
  }
  const pageTerminal = isAgentTurnTerminal(pageTurn.status);
  const detailTerminal = isAgentTurnTerminal(detailTurn.status);
  if (pageTerminal !== detailTerminal) {
    return pageTerminal ? pageTurn : detailTurn;
  }
  return detailTurn.updated_at >= pageTurn.updated_at ? detailTurn : pageTurn;
}

export function useAgentConversation({
  productId,
  conversation,
  graph = null,
  taskId = null,
  pageContext = null,
  enabled = true,
}: UseAgentConversationInput) {
  void graph;
  const queryClient = useQueryClient();
  const turnsKey = useMemo(
    () => agentTurnsQueryKey(productId, conversation.id, taskId),
    [conversation.id, productId, taskId],
  );
  const turnsQuery = useInfiniteQuery({
    queryKey: turnsKey,
    queryFn: ({ pageParam }) =>
      api.listAgentTurns(productId, conversation.id, {
        after: pageParam,
        limit: AGENT_TURN_PAGE_SIZE,
        taskId,
      }),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled,
  });
  const pageTurns = useMemo(
    () => flattenAgentTurnPages(turnsQuery.data?.pages),
    [turnsQuery.data?.pages],
  );
  const latestPageTurn = pageTurns.at(-1) ?? null;

  const latestProjectionQuery = useQuery({
    queryKey: agentTurnQueryKey(productId, conversation.id, latestPageTurn?.id ?? "none"),
    queryFn: () => api.getAgentTurn(productId, conversation.id, latestPageTurn?.id ?? ""),
    enabled: Boolean(enabled && latestPageTurn && !isAgentTurnTerminal(latestPageTurn.status)),
    refetchInterval: (query) => {
      const projection = query.state.data;
      return projection && isAgentTurnTerminal(projection.status)
        ? false
        : AGENT_TURN_PROJECTION_POLL_MS;
    },
  });
  const latestTurn = selectNewestAgentTurnProjection(
    latestPageTurn,
    latestProjectionQuery.data,
  );
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
      queryClient.setQueryData(
        agentTurnQueryKey(productId, conversation.id, turn.id),
        turn,
      );
    },
    [conversation.id, productId, queryClient, turnsKey],
  );
  const invalidateConversation = useCallback(() => {
    const jobs = [
      queryClient.invalidateQueries({ queryKey: turnsKey }),
      queryClient.invalidateQueries({ queryKey: ["agent-sessions"] }),
      queryClient.invalidateQueries({ queryKey: ["agent-tasks"] }),
      queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] }),
      queryClient.invalidateQueries({ queryKey: ["graph-runs", productId] }),
    ];
    if (taskId) {
      jobs.push(queryClient.invalidateQueries({ queryKey: ["agent-task", taskId] }));
    }
    return Promise.all(jobs);
  }, [productId, queryClient, taskId, turnsKey]);

  useEffect(() => {
    if (latestProjectionQuery.data) {
      cacheTurn(latestProjectionQuery.data);
    }
  }, [cacheTurn, latestProjectionQuery.data]);

  const submitTurnMutation = useMutation({
    mutationFn: (input: SubmitAgentTurnInput) =>
      api.submitAgentTurn(productId, conversation.id, {
        ...input,
        task_id: input.task_id ?? taskId,
        page_context: input.page_context ?? pageContext,
      }),
    onSuccess: (response) => cacheTurn(response.turn),
    onSettled: () => invalidateConversation(),
  });
  const cancelTurnMutation = useMutation({
    mutationFn: (projectionId: string) =>
      api.cancelAgentTurn(productId, conversation.id, projectionId),
    onSuccess: cacheTurn,
    onSettled: () => invalidateConversation(),
  });
  const resumeTurnMutation = useMutation({
    mutationFn: (projectionId: string) =>
      api.resumeAgentTurn(productId, conversation.id, projectionId),
    onSuccess: cacheTurn,
    onSettled: () => invalidateConversation(),
  });
  const answerQuestionMutation = useMutation<AnswerQuestionResult, Error, AnswerQuestionInput>({
    mutationFn: async ({ projectionId, questionId, answer }) => {
      const result = await api.answerAgentQuestion(
        productId,
        conversation.id,
        projectionId,
        questionId,
        answer,
      );
      cacheTurn(result.answered_turn);
      return {
        answered: result.answered_turn,
        continuation: result.continuation_turn,
      };
    },
    onSuccess: ({ answered, continuation }) => {
      cacheTurn(answered);
      if (continuation.id === answered.id) {
        cacheTurn(continuation);
      }
    },
    onSettled: () => invalidateConversation(),
  });

  return {
    turns,
    latestTurn,
    activeTurn: latestTurn && !isAgentTurnTerminal(latestTurn.status) ? latestTurn : null,
    turnsQuery,
    latestProjectionQuery,
    submitTurnMutation,
    cancelTurnMutation,
    resumeTurnMutation,
    answerQuestionMutation,
    cacheTurn,
    refreshLatestTurn: latestProjectionQuery.refetch,
  };
}

function compareAgentTurns(left: AgentTurn, right: AgentTurn): number {
  const timeOrder = left.created_at.localeCompare(right.created_at);
  return timeOrder || left.id.localeCompare(right.id);
}
