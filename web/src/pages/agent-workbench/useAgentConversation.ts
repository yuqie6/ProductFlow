import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef } from "react";

import { api } from "../../lib/api";
import type {
  AgentConversation,
  AgentQuestionAnswer,
  AgentTurn,
  AgentTurnPage,
  SubmitAgentTurnInput,
  WorkflowDraft,
  WorkflowDraftLegacyArchiveSeed,
} from "../../lib/types";
import { isAgentTurnTerminal } from "./agentEventReducer";

export const INITIAL_AGENT_TURN_TEXT =
  "请读取我已提交的商品参考图和图片需求，整理生成工作流所需信息；只补问当前确实缺失且会影响工作流的内容。";
export const INITIAL_ARCHIVE_REBUILD_TURN_TEXT =
  "请读取当前 WorkflowDraft 的 legacy_archive_seed，按需分段检查这份只读归档和必要的参考图，结合当前商品重新形成工作流；只补问确实影响结果的信息，确认无误后再提交工作流方案。";

const AGENT_TURN_PAGE_SIZE = 20;
const AGENT_TURN_PROJECTION_POLL_MS = 1_500;

interface UseAgentConversationInput {
  productId: string;
  conversation: AgentConversation;
  workflowDraft: WorkflowDraft;
  enabled?: boolean;
}

interface AnswerQuestionInput {
  projectionId: string;
  questionId: string;
  answer: AgentQuestionAnswer;
}

interface AnswerQuestionResult {
  answered: AgentTurn;
  resumed: AgentTurn;
}

export class AgentResumeAfterAnswerError extends Error {
  answered: AgentTurn;
  cause: unknown;

  constructor(answered: AgentTurn, cause: unknown) {
    super(cause instanceof Error ? cause.message : "Agent 回答已保存，但恢复执行失败");
    this.name = "AgentResumeAfterAnswerError";
    this.answered = answered;
    this.cause = cause;
  }
}

export function agentTurnsQueryKey(productId: string, conversationId: string) {
  return ["agent-turns", productId, conversationId] as const;
}

export function agentTurnQueryKey(productId: string, conversationId: string, projectionId: string) {
  return ["agent-turn", productId, conversationId, projectionId] as const;
}

export function initialAgentTurnInput(
  conversationId: string,
  referenceAssetIds: readonly string[],
  legacyArchiveSeed: WorkflowDraftLegacyArchiveSeed | null = null,
): SubmitAgentTurnInput {
  return {
    input_text: legacyArchiveSeed ? INITIAL_ARCHIVE_REBUILD_TURN_TEXT : INITIAL_AGENT_TURN_TEXT,
    asset_ids: legacyArchiveSeed ? [] : [...referenceAssetIds],
    idempotency_key: `initial:${conversationId}`,
  };
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
  workflowDraft,
  enabled = true,
}: UseAgentConversationInput) {
  const queryClient = useQueryClient();
  const turnsKey = useMemo(
    () => agentTurnsQueryKey(productId, conversation.id),
    [conversation.id, productId],
  );
  const autoStartKeyRef = useRef<string | null>(null);

  const turnsQuery = useInfiniteQuery({
    queryKey: turnsKey,
    queryFn: ({ pageParam }) =>
      api.listAgentTurns(productId, conversation.id, {
        after: pageParam,
        limit: AGENT_TURN_PAGE_SIZE,
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

  useEffect(() => {
    if (latestProjectionQuery.data) {
      cacheTurn(latestProjectionQuery.data);
    }
  }, [cacheTurn, latestProjectionQuery.data]);

  const initialInput = useMemo(
    () =>
      initialAgentTurnInput(
        conversation.id,
        workflowDraft.intake?.reference_asset_ids ?? [],
        workflowDraft.legacy_archive_seed,
      ),
    [conversation.id, workflowDraft.intake?.reference_asset_ids, workflowDraft.legacy_archive_seed],
  );

  const initialTurnMutation = useMutation({
    mutationFn: () => api.submitAgentTurn(productId, conversation.id, initialInput),
    onSuccess: (response) => cacheTurn(response.turn),
    onSettled: () => queryClient.invalidateQueries({ queryKey: turnsKey }),
  });

  useEffect(() => {
    const key = initialInput.idempotency_key;
    if (
      !enabled ||
      !turnsQuery.isSuccess ||
      pageTurns.length > 0 ||
      initialTurnMutation.isPending ||
      autoStartKeyRef.current === key
    ) {
      return;
    }
    autoStartKeyRef.current = key;
    initialTurnMutation.mutate();
  }, [enabled, initialInput.idempotency_key, initialTurnMutation, pageTurns.length, turnsQuery.isSuccess]);

  const submitTurnMutation = useMutation({
    mutationFn: (input: SubmitAgentTurnInput) =>
      api.submitAgentTurn(productId, conversation.id, input),
    onSuccess: (response) => cacheTurn(response.turn),
    onSettled: () => queryClient.invalidateQueries({ queryKey: turnsKey }),
  });
  const cancelTurnMutation = useMutation({
    mutationFn: (projectionId: string) =>
      api.cancelAgentTurn(productId, conversation.id, projectionId),
    onSuccess: cacheTurn,
    onSettled: () => queryClient.invalidateQueries({ queryKey: turnsKey }),
  });
  const resumeTurnMutation = useMutation({
    mutationFn: (projectionId: string) =>
      api.resumeAgentTurn(productId, conversation.id, projectionId),
    onSuccess: cacheTurn,
    onSettled: () => queryClient.invalidateQueries({ queryKey: turnsKey }),
  });
  const answerQuestionMutation = useMutation<AnswerQuestionResult, Error, AnswerQuestionInput>({
    mutationFn: async ({ projectionId, questionId, answer }) => {
      const answered = await api.answerAgentQuestion(
        productId,
        conversation.id,
        projectionId,
        questionId,
        answer,
      );
      cacheTurn(answered);
      if (!answered.resume_required) {
        return { answered, resumed: answered };
      }
      try {
        const resumed = await api.resumeAgentTurn(productId, conversation.id, projectionId);
        return { answered, resumed };
      } catch (error) {
        throw new AgentResumeAfterAnswerError(answered, error);
      }
    },
    onSuccess: ({ resumed }) => cacheTurn(resumed),
    onError: (error) => {
      if (error instanceof AgentResumeAfterAnswerError) {
        cacheTurn(error.answered);
      }
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: turnsKey }),
  });

  return {
    turns,
    latestTurn,
    activeTurn: latestTurn && !isAgentTurnTerminal(latestTurn.status) ? latestTurn : null,
    turnsQuery,
    latestProjectionQuery,
    initialTurnMutation,
    retryInitialTurn: () => {
      if (!initialTurnMutation.isPending) {
        initialTurnMutation.mutate();
      }
    },
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
