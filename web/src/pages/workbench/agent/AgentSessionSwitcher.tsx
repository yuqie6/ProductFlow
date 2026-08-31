import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  Check,
  ChevronDown,
  CircleAlert,
  MessagesSquare,
  Pencil,
  Plus,
  Search,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { ConfirmDialog } from "../../../components/ConfirmDialog";
import { Button } from "../../../components/ui/button";
import { IconButton } from "../../../components/ui/icon-button";
import { Input } from "../../../components/ui/field";
import { PanelSkeleton } from "../../../components/ui/skeleton";
import { ApiError, api } from "../../../lib/api";
import { useI18n } from "../../../lib/preferences";
import type { AgentConversation, AgentSession, AgentSessionConversation } from "../../../lib/types";

interface AgentSessionSwitcherProps {
  conversation: AgentConversation;
}

type SessionEditor = { mode: "rename"; value: string } | null;

export function selectAgentSessionConversation(
  agentSession: AgentSession,
): AgentSessionConversation | null {
  return agentSession.conversations.find(
    (conversation) => conversation.scope_type === "product_workflow" && conversation.product_id,
  ) ?? null;
}

export function AgentSessionSwitcher({ conversation }: AgentSessionSwitcherProps) {
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
  const sessionsQuery = useInfiniteQuery({
    queryKey: ["agent-sessions", true, conversation.product_id],
    initialPageParam: null as string | null,
    queryFn: ({ pageParam }) => api.listAgentSessions(true, conversation.product_id, { after: pageParam }),
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    staleTime: 30_000,
  });
  const sessionItems = useMemo(
    () => sessionsQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [sessionsQuery.data?.pages],
  );
  const currentSession = sessionItems.find(
    (agentSession) => agentSession.id === conversation.session_id,
  ) ?? null;
  const visibleSessions = useMemo(() => {
    const normalizedSearch = search.trim().toLocaleLowerCase();
    if (!normalizedSearch) {
      return sessionItems;
    }
    return sessionItems.filter((agentSession) => {
      const workspaceNames = agentSession.conversations.map((item) => item.product_name).join(" ");
      return `${agentSession.title} ${workspaceNames}`.toLocaleLowerCase().includes(normalizedSearch);
    });
  }, [search, sessionItems]);
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
    mutationFn: async (input: { mode: "rename"; title: string }) => {
      if (!conversation.session_id) {
        throw new Error(t("agentWorkbench.session.missingCurrent"));
      }
      return api.renameAgentSession(conversation.session_id, input.title);
    },
    onSuccess: () => {
      setEditor(null);
      setNotice(t("agentWorkbench.session.renamed"));
      void queryClient.invalidateQueries({ queryKey: ["agent-sessions", true, conversation.product_id] });
    },
  });
  const createSessionMutation = useMutation({
    mutationFn: () => {
      if (!conversation.product_id) {
        throw new Error(t("agentWorkbench.session.missingCurrent"));
      }
      return api.ensureAgentWorkbench(conversation.product_id, null, { newSession: true });
    },
    onSuccess: (bootstrap) => {
      setOpen(false);
      setNotice(t("agentWorkbench.session.created"));
      void queryClient.invalidateQueries({ queryKey: ["agent-sessions", true, conversation.product_id] });
      if (bootstrap.conversation.session_id) {
        navigate(
          `/products/${encodeURIComponent(conversation.product_id ?? bootstrap.product.id)}?agent_session_id=${encodeURIComponent(bootstrap.conversation.session_id)}`,
        );
      }
    },
  });
  const archiveMutation = useMutation({
    mutationFn: (sessionId: string) => api.archiveAgentSession(sessionId),
    onSuccess: () => {
      setArchiveTarget(null);
      setOpen(false);
      setNotice(t("agentWorkbench.session.archived"));
      void queryClient.invalidateQueries({ queryKey: ["agent-sessions", true, conversation.product_id] });
    },
  });

  const sessionError = sessionsQuery.error instanceof ApiError
    ? sessionsQuery.error.detail
    : sessionsQuery.error instanceof Error
      ? sessionsQuery.error.message
      : null;
  const mutationError = editorMutation.error ?? createSessionMutation.error ?? archiveMutation.error;
  const mutationErrorText = mutationError instanceof ApiError
    ? mutationError.detail
    : mutationError instanceof Error
      ? mutationError.message
      : null;

  const createSession = () => {
    if (createSessionMutation.isPending) {
      return;
    }
    setNotice(null);
    editorMutation.reset();
    createSessionMutation.reset();
    createSessionMutation.mutate();
  };

  const startEditor = () => {
    setNotice(null);
    editorMutation.reset();
    setOpen(true);
    setEditor({ mode: "rename", value: currentSession?.title ?? "" });
  };

  const submitEditor = () => {
    const title = editor?.value.trim() ?? "";
    if (!editor || !title || editorMutation.isPending) {
      return;
    }
    editorMutation.mutate({ mode: "rename", title });
  };

  const switchSession = (sessionId: string) => {
    if (!sessionId || sessionId === conversation.session_id) {
      setOpen(false);
      return;
    }
    const target = sessionItems.find((agentSession) => agentSession.id === sessionId);
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
      <div ref={rootRef} data-agent-session-switcher className="relative min-w-0 max-w-full">
        <button
          type="button"
          aria-haspopup="dialog"
          aria-expanded={open}
          aria-controls="agent-session-menu"
          onClick={() => {
            setNotice(null);
            setOpen((isOpen) => !isOpen);
          }}
          className={`-ml-1 flex min-w-0 max-w-full items-center gap-1.5 rounded-control px-1 py-0.5 text-left text-xs transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 ${
            open ? "bg-surface-subtle text-text-primary" : "text-text-secondary hover:bg-surface-subtle hover:text-text-primary"
          }`}
        >
          <span
            aria-hidden="true"
            className={`h-1.5 w-1.5 shrink-0 rounded-full ${
              currentStatus === "active"
                ? "bg-state-success"
                : currentStatus === "archived"
                  ? "bg-text-muted/60"
                  : "bg-text-muted/40"
            }`}
          />
          <span className="min-w-0 truncate" title={currentTitle}>{currentTitle}</span>
          <ChevronDown
            size={13}
            aria-hidden="true"
            className={`shrink-0 text-text-muted transition-transform ${open ? "rotate-180" : ""}`}
          />
        </button>

        {open ? (
          <div
            id="agent-session-menu"
            role="dialog"
            aria-label={t("agentWorkbench.session.label")}
            className="absolute -left-16 top-[calc(100%+0.625rem)] w-80 max-w-[calc(100vw-1rem)] overflow-hidden rounded-lg border border-border-l2 bg-surface-raised shadow-elev-3"
          >
            <div className="flex min-h-12 items-center gap-2 border-b border-border-l1 px-3 py-1.5">
              <div className="flex min-w-0 flex-1 items-center gap-2">
                <h3 className="truncate text-sm font-semibold text-text-primary">
                  {t("agentWorkbench.session.label")}
                </h3>
                <span className="rounded-full bg-surface-subtle px-1.5 py-0.5 text-[10px] font-semibold text-text-secondary">
                  {sessionItems.length}
                </span>
              </div>
              <div className="flex shrink-0 items-center gap-0.5">
                {currentSession && currentSession.status === "active" ? (
                  <>
                    <IconButton
                      label={t("agentWorkbench.session.rename")}
                      size="toolbar"
                      onClick={startEditor}
                    >
                      <Pencil size={14} />
                    </IconButton>
                    <IconButton
                      label={t("agentWorkbench.session.archive")}
                      size="toolbar"
                      onClick={() => setArchiveTarget(currentSession)}
                    >
                      <Archive size={14} />
                    </IconButton>
                  </>
                ) : null}
                <IconButton
                  label={t("agentWorkbench.session.new")}
                  size="toolbar"
                  onClick={createSession}
                  disabled={createSessionMutation.isPending}
                  busy={createSessionMutation.isPending}
                >
                  <Plus size={15} />
                </IconButton>
                <IconButton
                  label={t("agentWorkbench.session.close")}
                  size="toolbar"
                  onClick={() => setOpen(false)}
                >
                  <X size={15} />
                </IconButton>
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
                  <IconButton
                    label={t("agentWorkbench.session.clearSearch")}
                    size="sm"
                    onClick={() => setSearch("")}
                  >
                    <X size={13} />
                  </IconButton>
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
                <div className="flex min-w-0 items-end gap-2">
                  <div className="min-w-0 flex-1">
                    <Input
                      id="agent-session-title"
                      label={t("agentWorkbench.session.titleLabel")}
                      autoFocus
                      value={editor.value}
                      onChange={(event) => setEditor({ ...editor, value: event.target.value })}
                      placeholder={t("agentWorkbench.session.titlePlaceholder")}
                      maxLength={160}
                      className="h-10"
                    />
                  </div>
                  <Button
                    type="submit"
                    variant="primary"
                    size="lg"
                    className="shrink-0"
                    disabled={!editor.value.trim()}
                    busy={editorMutation.isPending}
                  >
                    {editorMutation.isPending ? null : <Check size={14} />}
                    <span>{t("agentWorkbench.session.save")}</span>
                  </Button>
                  <IconButton
                    label={t("agentWorkbench.session.cancel")}
                    size="toolbar"
                    onClick={() => {
                      setEditor(null);
                      editorMutation.reset();
                    }}
                  >
                    <X size={15} />
                  </IconButton>
                </div>
              </form>
            ) : null}

            <div
              role="listbox"
              aria-label={t("agentWorkbench.session.label")}
              className="max-h-[min(22rem,calc(100dvh-16rem))] overflow-y-auto p-1.5"
            >
              {sessionsQuery.isLoading ? (
                <div className="flex items-center justify-center px-3 py-8">
                  <PanelSkeleton compact rows={3} label={t("agentWorkbench.session.loading")} className="w-full" />
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
                  {sessionsQuery.hasNextPage ? (
                    <button
                      type="button"
                      onClick={() => void sessionsQuery.fetchNextPage()}
                      disabled={sessionsQuery.isFetchingNextPage}
                      className="mt-1 flex h-9 w-full items-center justify-center rounded-lg text-xs font-medium text-text-secondary hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 disabled:opacity-60"
                    >
                      {t("agentWorkbench.session.loadMore")}
                    </button>
                  ) : null}
                </>
              )}
            </div>
            {sessionError || mutationErrorText ? (
              <div role="alert" className="flex items-start gap-1.5 border-t border-state-error/30 bg-state-error-soft px-3 py-2 text-xs leading-5 text-state-error">
                <CircleAlert size={14} className="mt-0.5 shrink-0" />
                <span className="min-w-0">{sessionError ?? mutationErrorText}</span>
              </div>
            ) : null}
            {notice ? <p className="truncate border-t border-border-l1 px-3 py-1.5 text-[11px] text-text-secondary">{notice}</p> : null}
          </div>
        ) : null}
      </div>

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
