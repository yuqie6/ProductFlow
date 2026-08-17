import { Bot, Loader2, Send, Square, User } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { ApiError } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import { useI18n } from "../../lib/preferences";
import type {
  AgentPageContextSnapshotInput,
  AgentQuestionAnswer,
  AgentTaskStatus,
  AgentToolStep,
} from "../../lib/types";
import { AgentQuestionPrompt } from "./AgentQuestionPrompt";
import { AgentToolStepList } from "./AgentToolStepList";
import { AgentTurnTail } from "./AgentTurnTail";
import { GlobalLibraryOrganizationDraftCard } from "./GlobalLibraryOrganizationDraftCard";
import { useGlobalAgentConversation } from "./useGlobalAgentConversation";

interface GlobalAgentConversationPanelProps {
  conversationId: string | null;
  sessionTitle: string;
  taskId?: string | null;
  taskGoal?: string | null;
  taskStatus?: AgentTaskStatus | null;
  pageContext: AgentPageContextSnapshotInput;
}

export function GlobalAgentConversationPanel({
  conversationId,
  sessionTitle,
  taskId = null,
  taskGoal = null,
  taskStatus = null,
  pageContext,
}: GlobalAgentConversationPanelProps) {
  const { t } = useI18n();
  const [composerText, setComposerText] = useState("");
  const [answeredQuestionId, setAnsweredQuestionId] = useState<string | null>(null);
  const composerKeyRef = useRef(globalThis.crypto.randomUUID());
  const agent = useGlobalAgentConversation({
    conversationId: conversationId ?? "",
    taskId,
    taskGoal,
    taskStatus,
    pageContext,
    enabled: Boolean(conversationId),
  });
  const activeQuestion = agent.activeTurn?.question ?? null;
  const activeTurnId = agent.activeTurn?.id ?? null;
  const listRef = useRef<HTMLDivElement | null>(null);
  const previousTurnCount = useRef(0);
  const confirmationKeyRef = useRef<{ draftId: string; version: number; key: string } | null>(null);

  useEffect(() => setAnsweredQuestionId(null), [activeQuestion?.id]);
  useEffect(() => {
    if (agent.turns.length > previousTurnCount.current || agent.activeTurn) {
      const element = listRef.current;
      if (element) {
        element.scrollTop = element.scrollHeight;
      }
    }
    previousTurnCount.current = agent.turns.length;
  }, [agent.activeTurn, agent.turns.length]);

  const canSubmit = Boolean(conversationId && composerText.trim() && !agent.activeTurn);
  const submit = async () => {
    const input = composerText.trim();
    if (!input || !canSubmit || agent.submitTurnMutation.isPending) {
      return;
    }
    try {
      await agent.submitTurnMutation.mutateAsync({
        input_text: input,
        asset_ids: [],
        idempotency_key: composerKeyRef.current,
        task_id: taskId,
        page_context: { ...pageContext, captured_at: new Date().toISOString() },
      });
      setComposerText("");
      composerKeyRef.current = globalThis.crypto.randomUUID();
    } catch {
      // Keep the text and idempotency key so a failed request can be retried safely.
    }
  };
  const answerQuestion = async (answer: AgentQuestionAnswer) => {
    if (!agent.activeTurn || !activeQuestion || agent.answerQuestionMutation.isPending) {
      return;
    }
    try {
      await agent.answerQuestionMutation.mutateAsync({
        projectionId: agent.activeTurn.id,
        questionId: activeQuestion.id,
        answer,
      });
      setAnsweredQuestionId(activeQuestion.id);
    } catch {
      // The mutation surface keeps the answer available for a resume retry.
    }
  };
  const error = errorDetail(
    agent.turnsQuery.error ??
      agent.submitTurnMutation.error ??
      agent.cancelTurnMutation.error ??
      agent.resumeTurnMutation.error ??
      agent.answerQuestionMutation.error,
    t("globalAgent.requestFailed"),
  );
  const questionAnswered = Boolean(
    activeQuestion && (answeredQuestionId === activeQuestion.id || agent.activeTurn?.resume_required),
  );
  const confirmDraft = () => {
    const draft = agent.libraryOrganizationDraftQuery.data;
    const revision = draft?.current_revision;
    if (!draft || !revision || draft.status !== "awaiting_confirmation") {
      return;
    }
    const cached = confirmationKeyRef.current;
    const key =
      cached?.draftId === draft.id && cached.version === revision.version
        ? cached.key
        : globalThis.crypto.randomUUID();
    confirmationKeyRef.current = { draftId: draft.id, version: revision.version, key };
    agent.confirmLibraryOrganizationDraftMutation.mutate({
      expectedDraftVersion: revision.version,
      idempotencyKey: key,
    });
  };

  if (!conversationId) {
    return (
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-2 px-5 text-center text-text-muted">
        <Bot size={24} />
        <p className="text-sm">{t("globalAgent.noGlobalConversation")}</p>
      </div>
    );
  }

  return (
    <section data-global-agent-conversation className="flex min-h-0 flex-1 flex-col bg-surface-raised text-text-primary">
      <header className="shrink-0 border-b border-border-l1 px-4 py-3">
        <div className="flex min-w-0 items-center gap-2">
          <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-accent text-accent-fg">
            <Bot size={16} aria-hidden="true" />
          </span>
          <div className="min-w-0 flex-1">
            <h3 className="truncate text-sm font-semibold">{sessionTitle}</h3>
            <p className="truncate text-[11px] text-text-secondary">{t("globalAgent.globalScope")}</p>
          </div>
          <span className={`h-2 w-2 shrink-0 rounded-full ${agent.activeTurn ? "animate-pulse bg-accent" : "bg-state-success"}`} aria-hidden="true" />
        </div>
        <div className="mt-2 flex min-w-0 items-center gap-1.5 text-[11px] text-text-muted">
          <span className="shrink-0">{t("globalAgent.currentPage")}</span>
          <span className="truncate" title={pageContext.route}>{pageContext.route}</span>
        </div>
      </header>

      {error ? <p role="alert" className="shrink-0 border-b border-state-error/20 bg-state-error/10 px-4 py-2 text-xs leading-5 text-state-error">{error}</p> : null}

      <div ref={listRef} className="min-h-0 flex-1 overflow-y-auto px-3 py-4">
        <div className="space-y-5">
          {agent.turns.map((turn) => {
            const active = turn.id === activeTurnId;
            const waiting = active && turn.status !== "requires_input" && turn.status !== "awaiting_confirmation";
            const steps: AgentToolStep[] = turn.tool_steps ?? [];
            return (
              <div key={turn.id} className="space-y-3">
                <div className="flex justify-end gap-2">
                  <div className="min-w-0 max-w-[88%]">
                    <div className="rounded-md bg-accent px-3 py-2 text-sm leading-6 text-accent-fg">
                      <div className="whitespace-pre-wrap break-words">{turn.input_text}</div>
                    </div>
                    <div className="mt-1 text-right text-[10px] text-text-muted">{formatDateTime(turn.created_at, t.locale)}</div>
                  </div>
                  <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-surface-subtle text-text-secondary"><User size={14} /></span>
                </div>
                <div className="flex gap-2">
                  <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-text-primary text-surface-raised"><Bot size={14} /></span>
                  <div className="min-w-0 flex-1">
                    {turn.output_text || waiting ? (
                      <div className="border-l-2 border-border-l3 pl-3 text-sm leading-6">
                        {turn.output_text ? <div className="whitespace-pre-wrap break-words">{turn.output_text}</div> : <div className="flex items-center gap-2 text-text-secondary"><Loader2 size={14} className="animate-spin motion-reduce:animate-none" />{t("agentWorkbench.waitingForAgent")}</div>}
                      </div>
                    ) : null}
                    <AgentToolStepList steps={steps} live={active} />
                    <AgentTurnTail turn={turn} active={active} reviewDraft={false} />
                    {turn.library_organization_draft_revision_id &&
                    turn.library_organization_draft_revision_id ===
                      agent.libraryOrganizationDraftQuery.data?.current_revision?.id ? (
                      <GlobalLibraryOrganizationDraftCard
                        draft={agent.libraryOrganizationDraftQuery.data ?? null}
                        loading={agent.libraryOrganizationDraftQuery.isLoading}
                        error={errorDetail(
                          agent.libraryOrganizationDraftQuery.error ??
                            agent.confirmLibraryOrganizationDraftMutation.error,
                          t("globalAgent.draft.loadFailed"),
                        )}
                        busy={agent.confirmLibraryOrganizationDraftMutation.isPending}
                        onConfirm={confirmDraft}
                      />
                    ) : null}
                  </div>
                </div>
              </div>
            );
          })}
          {agent.turnsQuery.isLoading ? <div className="flex min-h-32 items-center justify-center gap-2 text-xs text-text-muted"><Loader2 size={16} className="animate-spin" />{t("app.loading")}</div> : null}
          {!agent.turnsQuery.isLoading && !agent.turns.length ? <div className="flex min-h-32 items-center justify-center px-4 text-center text-sm text-text-muted">{t("globalAgent.emptyChat")}</div> : null}
        </div>
      </div>

      {activeQuestion && agent.activeTurn ? (
        <AgentQuestionPrompt
          question={activeQuestion}
          answered={questionAnswered}
          resumeRequired={Boolean(agent.activeTurn.resume_required)}
          busy={agent.answerQuestionMutation.isPending || agent.resumeTurnMutation.isPending}
          error={errorDetail(agent.answerQuestionMutation.error, t("globalAgent.requestFailed"))}
          onAnswer={(answer) => void answerQuestion(answer)}
          onResume={() => agent.resumeTurnMutation.mutate(agent.activeTurn?.id ?? "")}
        />
      ) : null}

      <div className="shrink-0 border-t border-border-l1 bg-surface-subtle/50 p-3">
        <div className="grid grid-cols-[minmax(0,1fr)_42px] items-end gap-2 rounded-md border border-border-l2 bg-surface-raised p-2 focus-within:border-accent focus-within:ring-2 focus-within:ring-accent/15">
          <textarea
            value={composerText}
            maxLength={20_000}
            rows={1}
            onChange={(event) => setComposerText(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing && canSubmit) {
                event.preventDefault();
                void submit();
              }
            }}
            placeholder={t("globalAgent.chatPlaceholder")}
            aria-label={t("agentWorkbench.composerLabel")}
            className="max-h-28 min-h-10 resize-none border-0 bg-transparent px-1 py-2 text-sm leading-6 text-text-primary outline-none placeholder:text-text-muted"
          />
          <button
            type="button"
            onClick={agent.activeTurn ? () => agent.cancelTurnMutation.mutate(agent.activeTurn?.id ?? "") : () => void submit()}
            disabled={agent.activeTurn ? agent.cancelTurnMutation.isPending : !canSubmit || agent.submitTurnMutation.isPending}
            aria-label={agent.activeTurn ? t("agentWorkbench.cancelTurn") : t("agentWorkbench.send")}
            title={agent.activeTurn ? t("agentWorkbench.cancelTurn") : t("agentWorkbench.send")}
            className={`flex h-10 w-10 items-center justify-center rounded-md text-accent-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-not-allowed disabled:opacity-40 ${agent.activeTurn ? "bg-state-error" : "bg-accent hover:bg-accent-strong"}`}
          >
            {agent.activeTurn ? (agent.cancelTurnMutation.isPending ? <Loader2 size={16} className="animate-spin" /> : <Square size={15} fill="currentColor" />) : <Send size={17} />}
          </button>
        </div>
      </div>
    </section>
  );
}

function errorDetail(error: unknown, fallback: string): string | null {
  if (!error) {
    return null;
  }
  if (error instanceof ApiError) {
    return error.detail;
  }
  return error instanceof Error ? error.message : fallback;
}
