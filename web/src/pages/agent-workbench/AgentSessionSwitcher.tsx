import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  Check,
  ChevronDown,
  CircleAlert,
  Loader2,
  MessagesSquare,
  Pencil,
  Plus,
  Search,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { ConfirmDialog } from "../../components/ConfirmDialog";
import { ApiError, api } from "../../lib/api";
import { useI18n } from "../../lib/preferences";
import type { AgentConversation, AgentSession, AgentSessionConversation } from "../../lib/types";

interface AgentSessionSwitcherProps {
  conversation: AgentConversation;
  productName: string;
}

type SessionEditor = { mode: "create" | "rename"; value: string } | null;

export function selectAgentSessionConversation(
  agentSession: AgentSession,
): AgentSessionConversation | null {
  return agentSession.conversations.find(
    (conversation) => conversation.scope_type === "product_workflow" && conversation.product_id,
  ) ?? null;
}

export function AgentSessionSwitcher({ conversation, productName }: AgentSessionSwitcherProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const rootRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [editor, setEditor] = useState<SessionEditor>(null);
  const [archiveTarget, setArchiveTarget] = useState<AgentSession | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const sessionsQuery = useQuery({
    queryKey: ["agent-sessions", true],
    queryFn: () => api.listAgentSessions(true),
    staleTime: 30_000,
  });
  const currentSession = sessionsQuery.data?.items.find(
    (agentSession) => agentSession.id === conversation.session_id,
  ) ?? null;
  const visibleSessions = useMemo(() => {
    const normalizedSearch = search.trim().toLocaleLowerCase();
    const sessions = sessionsQuery.data?.items ?? [];
    if (!normalizedSearch) {
      return sessions;
    }
    return sessions.filter((agentSession) => {
      const workspaceNames = agentSession.conversations.map((item) => item.product_name).join(" ");
      return `${agentSession.title} ${workspaceNames}`.toLocaleLowerCase().includes(normalizedSearch);
    });
  }, [search, sessionsQuery.data?.items]);
  const activeSessions = visibleSessions.filter((agentSession) => agentSession.status === "active");
  const archivedSessions = visibleSessions.filter((agentSession) => agentSession.status === "archived");

  useEffect(() => {
    if (!open) {
      return;
    }
    const onPointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) {
        setOpen(false);
      }
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
      }
    };
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  useEffect(() => {
    if (open) {
      window.requestAnimationFrame(() => searchInputRef.current?.focus());
    }
  }, [open]);

  const editorMutation = useMutation({
    mutationFn: async (input: { mode: "create" | "rename"; title: string }) => {
      if (input.mode === "create") {
        return api.createAgentSession({ title: input.title });
      }
      if (!conversation.session_id) {
        throw new Error(t("agentWorkbench.session.missingCurrent"));
      }
      return api.renameAgentSession(conversation.session_id, input.title);
    },
    onSuccess: (_result, input) => {
      setEditor(null);
      setNotice(t(input.mode === "create" ? "agentWorkbench.session.created" : "agentWorkbench.session.renamed"));
      void queryClient.invalidateQueries({ queryKey: ["agent-sessions", true] });
    },
  });
  const archiveMutation = useMutation({
    mutationFn: (sessionId: string) => api.archiveAgentSession(sessionId),
    onSuccess: () => {
      setArchiveTarget(null);
      setOpen(false);
      setNotice(t("agentWorkbench.session.archived"));
      void queryClient.invalidateQueries({ queryKey: ["agent-sessions", true] });
    },
  });

  const sessionError = sessionsQuery.error instanceof ApiError
    ? sessionsQuery.error.detail
    : sessionsQuery.error instanceof Error
      ? sessionsQuery.error.message
      : null;
  const mutationError = editorMutation.error ?? archiveMutation.error;
  const mutationErrorText = mutationError instanceof ApiError
    ? mutationError.detail
    : mutationError instanceof Error
      ? mutationError.message
      : null;

  const startEditor = (mode: "create" | "rename") => {
    setNotice(null);
    editorMutation.reset();
    setOpen(true);
    setEditor({ mode, value: mode === "rename" ? currentSession?.title ?? "" : "" });
  };

  const submitEditor = () => {
    const title = editor?.value.trim() ?? "";
    if (!editor || !title || editorMutation.isPending) {
      return;
    }
    editorMutation.mutate({ mode: editor.mode, title });
  };

  const switchSession = (sessionId: string) => {
    if (!sessionId || sessionId === conversation.session_id) {
      setOpen(false);
      return;
    }
    const target = sessionsQuery.data?.items.find((agentSession) => agentSession.id === sessionId);
    const targetConversation = target ? selectAgentSessionConversation(target) : null;
    if (!targetConversation?.product_id) {
      setNotice(t("agentWorkbench.session.noConversation"));
      return;
    }
    setOpen(false);
    navigate(
      `/products/${encodeURIComponent(targetConversation.product_id)}?agent_session_id=${encodeURIComponent(sessionId)}`,
    );
  };

  const renderSessionRow = (agentSession: AgentSession) => {
    const selected = agentSession.id === conversation.session_id;
    const firstWorkspace = agentSession.conversations[0]?.product_name;
    const workspaceSummary = agentSession.conversation_count === 0
      ? t("agentWorkbench.session.noWorkspace")
      : agentSession.conversation_count === 1
        ? firstWorkspace ?? t("agentWorkbench.session.workspaceCount", { count: 1 })
        : t("agentWorkbench.session.workspaceCount", { count: agentSession.conversation_count });

    return (
      <button
        key={agentSession.id}
        type="button"
        role="option"
        aria-selected={selected}
        onClick={() => switchSession(agentSession.id)}
        className={`group flex w-full min-w-0 items-center gap-3 rounded-lg px-3 py-2.5 text-left transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 ${
          selected
            ? "bg-accent-soft text-text-primary"
            : "text-text-primary hover:bg-surface-subtle"
        }`}
      >
        <span
          aria-hidden="true"
          className={`mt-0.5 h-2 w-2 shrink-0 rounded-full ${
            selected
              ? "bg-accent"
              : agentSession.status === "active"
                ? "bg-state-success"
                : "bg-text-muted/50"
          }`}
        />
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-center gap-2">
            <span className="min-w-0 flex-1 truncate text-sm font-semibold" title={agentSession.title}>
              {agentSession.title}
            </span>
            {agentSession.status === "archived" ? (
              <span className="shrink-0 text-[10px] font-medium text-text-muted">
                {t("agentWorkbench.session.archivedLabel")}
              </span>
            ) : null}
          </span>
          <span className="mt-0.5 block truncate text-xs text-text-secondary" title={workspaceSummary}>
            {workspaceSummary}
          </span>
        </span>
        {selected ? <Check size={15} className="shrink-0 text-accent" /> : null}
      </button>
    );
  };

  const currentTitle = currentSession?.title
    ?? (conversation.session_id ? t("agentWorkbench.session.loading") : t("agentWorkbench.session.unlinked"));
  const currentStatus = currentSession?.status ?? null;

  return (
    <>
      <div ref={rootRef} data-agent-session-switcher className="relative z-20 shrink-0 border-b border-border-l1 bg-surface-base px-3 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <button
            type="button"
            aria-haspopup="dialog"
            aria-expanded={open}
            aria-controls="agent-session-menu"
            onClick={() => {
              setNotice(null);
              setOpen((isOpen) => !isOpen);
            }}
            className={`flex min-w-0 flex-1 items-center gap-3 rounded-lg border px-3 py-2 text-left transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 ${
              open
                ? "border-accent/60 bg-surface-raised"
                : "border-border-l2 bg-surface-raised hover:border-border-l3 hover:bg-surface-subtle"
            }`}
          >
            <span
              aria-hidden="true"
              className={`h-2 w-2 shrink-0 rounded-full ${
                currentStatus === "active"
                  ? "bg-state-success"
                  : currentStatus === "archived"
                    ? "bg-text-muted/60"
                    : "bg-text-muted/40"
              }`}
            />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[10px] font-semibold uppercase tracking-[0.08em] text-text-muted">
                {t("agentWorkbench.session.label")}
              </span>
              <span className="mt-0.5 block truncate text-sm font-semibold text-text-primary" title={currentTitle}>
                {currentTitle}
              </span>
              <span className="mt-0.5 block truncate text-xs text-text-secondary" title={productName}>
                {productName}
              </span>
            </span>
            <ChevronDown
              size={16}
              aria-hidden="true"
              className={`shrink-0 text-text-muted transition-transform ${open ? "rotate-180" : ""}`}
            />
          </button>
          <button
            type="button"
            onClick={() => startEditor("create")}
            aria-label={t("agentWorkbench.session.new")}
            title={t("agentWorkbench.session.new")}
            className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg border border-border-l2 bg-surface-raised text-text-secondary transition-colors hover:border-accent/50 hover:bg-accent-soft hover:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
          >
            <Plus size={17} />
          </button>
        </div>

        {open ? (
          <div
            id="agent-session-menu"
            role="dialog"
            aria-label={t("agentWorkbench.session.label")}
            className="absolute left-2 right-2 top-[calc(100%+0.5rem)] overflow-hidden rounded-xl border border-border-l2 bg-surface-raised shadow-[0_18px_48px_rgb(15_23_42_/_0.18)] dark:shadow-[0_20px_54px_rgb(0_0_0_/_0.42)]"
          >
            <div className="flex items-center gap-3 border-b border-border-l1 px-3 py-2.5">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <h3 className="truncate text-sm font-semibold text-text-primary">
                    {t("agentWorkbench.session.label")}
                  </h3>
                  <span className="rounded-full bg-surface-subtle px-1.5 py-0.5 text-[10px] font-semibold text-text-secondary">
                    {sessionsQuery.data?.items.length ?? 0}
                  </span>
                </div>
                <p className="mt-0.5 truncate text-xs text-text-secondary">
                  {t("agentWorkbench.session.menuDescription")}
                </p>
              </div>
              <div className="flex shrink-0 items-center gap-1">
                {currentSession && currentSession.status === "active" ? (
                  <>
                    <button
                      type="button"
                      onClick={() => startEditor("rename")}
                      aria-label={t("agentWorkbench.session.rename")}
                      title={t("agentWorkbench.session.rename")}
                      className="flex h-8 w-8 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
                    >
                      <Pencil size={14} />
                    </button>
                    <button
                      type="button"
                      onClick={() => setArchiveTarget(currentSession)}
                      aria-label={t("agentWorkbench.session.archive")}
                      title={t("agentWorkbench.session.archive")}
                      className="flex h-8 w-8 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-state-error/10 hover:text-state-error focus:outline-none focus-visible:ring-2 focus-visible:ring-state-error/50"
                    >
                      <Archive size={14} />
                    </button>
                  </>
                ) : null}
                <button
                  type="button"
                  onClick={() => startEditor("create")}
                  aria-label={t("agentWorkbench.session.new")}
                  title={t("agentWorkbench.session.new")}
                  className="flex h-8 w-8 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-accent-soft hover:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
                >
                  <Plus size={15} />
                </button>
                <button
                  type="button"
                  onClick={() => setOpen(false)}
                  aria-label={t("agentWorkbench.session.close")}
                  title={t("agentWorkbench.session.close")}
                  className="flex h-8 w-8 items-center justify-center rounded-md text-text-muted transition-colors hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
                >
                  <X size={15} />
                </button>
              </div>
            </div>

            <div className="border-b border-border-l1 p-2">
              <div className="flex h-9 items-center gap-2 rounded-lg border border-border-l2 bg-surface-subtle px-2.5 focus-within:border-accent focus-within:ring-2 focus-within:ring-accent/15">
                <Search size={14} aria-hidden="true" className="shrink-0 text-text-muted" />
                <input
                  ref={searchInputRef}
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder={t("agentWorkbench.session.searchPlaceholder")}
                  aria-label={t("agentWorkbench.session.search")}
                  className="min-w-0 flex-1 border-0 bg-transparent text-xs text-text-primary outline-none placeholder:text-text-muted"
                />
                {search ? (
                  <button
                    type="button"
                    onClick={() => setSearch("")}
                    aria-label={t("agentWorkbench.session.clearSearch")}
                    title={t("agentWorkbench.session.clearSearch")}
                    className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-text-muted hover:bg-surface-raised hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
                  >
                    <X size={13} />
                  </button>
                ) : null}
              </div>
            </div>

            {editor ? (
              <form
                className="border-b border-border-l1 bg-surface-subtle/60 p-3"
                onSubmit={(event) => {
                  event.preventDefault();
                  submitEditor();
                }}
              >
                <label htmlFor="agent-session-title" className="block text-[11px] font-semibold text-text-secondary">
                  {t("agentWorkbench.session.titleLabel")}
                </label>
                <div className="mt-1.5 flex min-w-0 items-center gap-2">
                  <input
                    id="agent-session-title"
                    autoFocus
                    value={editor.value}
                    onChange={(event) => setEditor({ ...editor, value: event.target.value })}
                    placeholder={t("agentWorkbench.session.titlePlaceholder")}
                    maxLength={160}
                    className="input-premium h-10 min-w-0 flex-1 px-3 text-sm"
                  />
                  <button
                    type="submit"
                    disabled={!editor.value.trim() || editorMutation.isPending}
                    className="inline-flex h-10 shrink-0 items-center gap-1.5 rounded-lg bg-accent px-3 text-xs font-semibold text-accent-fg transition-colors hover:bg-accent-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {editorMutation.isPending ? <Loader2 size={14} className="animate-spin motion-reduce:animate-none" /> : <Check size={14} />}
                    <span>{t("agentWorkbench.session.save")}</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setEditor(null);
                      editorMutation.reset();
                    }}
                    aria-label={t("agentWorkbench.session.cancel")}
                    title={t("agentWorkbench.session.cancel")}
                    className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-text-secondary transition-colors hover:bg-surface-raised hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
                  >
                    <X size={15} />
                  </button>
                </div>
              </form>
            ) : null}

            <div
              role="listbox"
              aria-label={t("agentWorkbench.session.label")}
              className="max-h-[min(22rem,calc(100dvh-16rem))] overflow-y-auto p-1.5"
            >
              {sessionsQuery.isLoading ? (
                <div className="flex items-center justify-center gap-2 px-3 py-8 text-xs text-text-secondary">
                  <Loader2 size={15} className="animate-spin motion-reduce:animate-none" />
                  <span>{t("agentWorkbench.session.loading")}</span>
                </div>
              ) : visibleSessions.length === 0 ? (
                <div className="flex flex-col items-center justify-center px-4 py-8 text-center">
                  <MessagesSquare size={20} className="text-text-muted" />
                  <p className="mt-2 text-xs font-medium text-text-secondary">
                    {search ? t("agentWorkbench.session.noMatch") : t("agentWorkbench.session.empty")}
                  </p>
                </div>
              ) : (
                <>
                  {activeSessions.length ? (
                    <div>
                      <p className="px-3 pb-1 pt-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-text-muted">
                        {t("agentWorkbench.session.activeLabel")}
                      </p>
                      {activeSessions.map(renderSessionRow)}
                    </div>
                  ) : null}
                  {archivedSessions.length ? (
                    <div className={activeSessions.length ? "mt-2 border-t border-border-l1 pt-2" : ""}>
                      <p className="px-3 pb-1 pt-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-text-muted">
                        {t("agentWorkbench.session.archivedLabel")}
                      </p>
                      {archivedSessions.map(renderSessionRow)}
                    </div>
                  ) : null}
                </>
              )}
            </div>
          </div>
        ) : null}
      </div>

      {sessionError || mutationErrorText ? (
        <div role="alert" className="flex items-start gap-1.5 border-b border-state-error/20 bg-state-error/10 px-3 py-2 text-xs leading-5 text-state-error">
          <CircleAlert size={14} className="mt-0.5 shrink-0" />
          <span className="min-w-0">{sessionError ?? mutationErrorText}</span>
        </div>
      ) : null}
      {notice ? <p className="truncate border-b border-border-l1 bg-surface-base px-3 py-1.5 text-[11px] text-text-secondary">{notice}</p> : null}

      <ConfirmDialog
        open={archiveTarget !== null}
        title={t("agentWorkbench.session.archive")}
        description={t("agentWorkbench.session.archiveConfirm", { title: archiveTarget?.title ?? "" })}
        confirmLabel={t("agentWorkbench.session.archive")}
        cancelLabel={t("common.cancel")}
        busy={archiveMutation.isPending}
        onClose={() => setArchiveTarget(null)}
        onConfirm={() => {
          if (archiveTarget) {
            archiveMutation.mutate(archiveTarget.id);
          }
        }}
      />
    </>
  );
}
