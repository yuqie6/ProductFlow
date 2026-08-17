import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  Bot,
  ChevronRight,
  ClipboardList,
  Loader2,
  MessagesSquare,
  Plus,
  Search,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { ApiError, api } from "../lib/api";
import type { TranslationKey } from "../lib/i18n";
import { useI18n } from "../lib/preferences";
import type {
  AgentPageContextSnapshotInput,
  AgentSession,
  AgentTask,
  AgentTaskStatus,
  CreateAgentTaskInput,
} from "../lib/types";
import { ConfirmDialog } from "./ConfirmDialog";
import { GlobalAgentConversationPanel } from "../pages/agent-workbench/GlobalAgentConversationPanel";

type GlobalAgentDockTab = "chat" | "tasks" | "sessions";

const TASK_STATUS_LABEL_KEYS = {
  queued: "globalAgent.taskStatus.queued",
  running: "globalAgent.taskStatus.running",
  waiting_user: "globalAgent.taskStatus.waitingUser",
  awaiting_confirmation: "globalAgent.taskStatus.awaitingConfirmation",
  succeeded: "globalAgent.taskStatus.succeeded",
  failed: "globalAgent.taskStatus.failed",
  canceled: "globalAgent.taskStatus.canceled",
  paused: "globalAgent.taskStatus.paused",
  unknown: "globalAgent.taskStatus.unknown",
} satisfies Record<AgentTaskStatus, TranslationKey>;

const ACTIVE_TASK_STATUSES = new Set<AgentTaskStatus>([
  "queued",
  "running",
  "waiting_user",
  "awaiting_confirmation",
  "paused",
]);

const TASK_STATUS_CLASSES: Record<AgentTaskStatus, string> = {
  queued: "bg-text-muted/60",
  running: "bg-accent animate-pulse",
  waiting_user: "bg-state-warning",
  awaiting_confirmation: "bg-state-warning",
  succeeded: "bg-state-success",
  failed: "bg-state-error",
  canceled: "bg-text-muted/60",
  paused: "bg-state-warning",
  unknown: "bg-text-muted/60",
};

interface AgentWorkspaceTarget {
  productId: string;
  conversationId: string;
}

interface AgentConversationTarget {
  conversationId: string;
  sessionId: string;
  scopeType: "product_workflow" | "global";
  productId: string | null;
  productName: string;
}

export function GlobalAgentDock() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const rootRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState<GlobalAgentDockTab>("chat");
  const [search, setSearch] = useState("");
  const [taskFormOpen, setTaskFormOpen] = useState(false);
  const [sessionFormOpen, setSessionFormOpen] = useState(false);
  const [sessionTitle, setSessionTitle] = useState("");
  const [taskSessionId, setTaskSessionId] = useState("");
  const [taskConversationId, setTaskConversationId] = useState("");
  const [taskTitle, setTaskTitle] = useState("");
  const [taskGoal, setTaskGoal] = useState("");
  const [archiveTarget, setArchiveTarget] = useState<AgentSession | null>(null);
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null);
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);

  const sessionsQuery = useQuery({
    queryKey: ["agent-sessions", true],
    queryFn: () => api.listAgentSessions(true),
    staleTime: 15_000,
  });
  const tasksQuery = useQuery({
    queryKey: ["agent-tasks", null, true],
    queryFn: () => api.listAgentTasks({ includeTerminal: true, limit: 100 }),
    staleTime: 8_000,
  });

  useEffect(() => {
    if (!open) {
      return;
    }
    void sessionsQuery.refetch();
    void tasksQuery.refetch();
  }, [open]);

  useEffect(() => {
    if (!open) {
      return;
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
      }
    };
    const onPointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) {
        setOpen(false);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    document.addEventListener("pointerdown", onPointerDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("pointerdown", onPointerDown);
    };
  }, [open]);

  const sessions = sessionsQuery.data?.items ?? [];
  const tasks = tasksQuery.data?.items ?? [];
  const activeTaskCount = tasks.filter((task) => ACTIVE_TASK_STATUSES.has(task.status)).length;
  const workspaceByConversationId = useMemo(() => {
    const entries = sessions.flatMap((session) =>
      session.conversations
        .filter((conversation) => conversation.scope_type === "product_workflow" && conversation.product_id)
        .map((conversation) => [conversation.conversation_id, {
          productId: conversation.product_id as string,
          conversationId: conversation.conversation_id,
          productName: conversation.product_name,
          sessionId: session.id,
        }] as const),
    );
    return new Map(entries);
  }, [sessions]);
  const conversationById = useMemo(() => {
    const entries = sessions.flatMap((session) =>
      session.conversations.map((conversation) => [conversation.conversation_id, {
        conversationId: conversation.conversation_id,
        sessionId: session.id,
        scopeType: conversation.scope_type,
        productId: conversation.product_id,
        productName: conversation.product_name,
      }] satisfies [string, AgentConversationTarget]),
    );
    return new Map(entries);
  }, [sessions]);
  const workspaceSessions = useMemo(
    () => sessions.filter((session) => session.status === "active" && session.conversations.length > 0),
    [sessions],
  );
  const currentSessionId = new URLSearchParams(location.search).get("agent_session_id");
  const activeSessionId = selectedSessionId ?? currentSessionId ?? workspaceSessions[0]?.id ?? sessions[0]?.id ?? null;
  const activeSession = sessions.find((session) => session.id === activeSessionId) ?? null;
  const globalConversation = activeSession?.conversations.find((conversation) => conversation.scope_type === "global") ?? null;
  const pageContext = buildPageContext(location.pathname, location.search);
  const normalizedSearch = search.trim().toLocaleLowerCase();
  const visibleSessions = useMemo(() => {
    if (!normalizedSearch) {
      return sessions;
    }
    return sessions.filter((session) => {
      const workspaceNames = session.conversations.map((item) => item.product_name).join(" ");
      return `${session.title} ${workspaceNames}`.toLocaleLowerCase().includes(normalizedSearch);
    });
  }, [normalizedSearch, sessions]);
  const visibleTasks = useMemo(() => {
    if (!normalizedSearch) {
      return tasks;
    }
    return tasks.filter((task) => {
      const conversation = task.conversation_id ? conversationById.get(task.conversation_id) : null;
      const productName = conversation?.productName ?? "";
      return `${task.title} ${task.goal} ${productName}`.toLocaleLowerCase().includes(normalizedSearch);
    });
  }, [conversationById, normalizedSearch, tasks]);

  const selectedTaskSession = workspaceSessions.find((session) => session.id === taskSessionId) ?? null;
  const taskWorkspaces = selectedTaskSession?.conversations ?? [];

  useEffect(() => {
    if (!workspaceSessions.length) {
      setTaskSessionId("");
      setTaskConversationId("");
      return;
    }
    const selectedSession = workspaceSessions.find((session) => session.id === taskSessionId);
    if (!selectedSession) {
      const nextSession = workspaceSessions[0];
      setTaskSessionId(nextSession.id);
      setTaskConversationId(preferredConversation(nextSession)?.conversation_id ?? "");
      return;
    }
    if (!selectedSession.conversations.some((conversation) => conversation.conversation_id === taskConversationId)) {
      setTaskConversationId(preferredConversation(selectedSession)?.conversation_id ?? "");
    }
  }, [taskConversationId, taskSessionId, workspaceSessions]);

  useEffect(() => {
    if (!selectedSessionId && activeSessionId) {
      setSelectedSessionId(activeSessionId);
    }
  }, [activeSessionId, selectedSessionId]);

  const invalidateAgentLists = () => {
    void queryClient.invalidateQueries({ queryKey: ["agent-sessions", true] });
    void queryClient.invalidateQueries({ queryKey: ["agent-tasks"] });
  };

  const createSessionMutation = useMutation({
    mutationFn: () => api.createAgentSession({ title: sessionTitle.trim() }),
    onSuccess: (session) => {
      setSessionTitle("");
      setSessionFormOpen(false);
      setSelectedSessionId(session.id);
      setSelectedTaskId(null);
      setTab("chat");
      invalidateAgentLists();
    },
  });
  const createTaskMutation = useMutation<AgentTask, Error, CreateAgentTaskInput>({
    mutationFn: (input) => api.createAgentTask(input),
    onSuccess: (task) => {
      setTaskTitle("");
      setTaskGoal("");
      setTaskFormOpen(false);
      setTab("tasks");
      invalidateAgentLists();
      const workspace = task.conversation_id ? workspaceByConversationId.get(task.conversation_id) : null;
      const conversation = task.conversation_id ? conversationById.get(task.conversation_id) : null;
      if (task.product_id && task.session_id) {
        navigate(
          `/products/${encodeURIComponent(task.product_id)}?agent_session_id=${encodeURIComponent(task.session_id)}&agent_task_id=${encodeURIComponent(task.id)}`,
        );
        setOpen(false);
      } else if (workspace) {
        navigate(
          `/products/${encodeURIComponent(workspace.productId)}?agent_session_id=${encodeURIComponent(task.session_id)}&agent_task_id=${encodeURIComponent(task.id)}`,
        );
        setOpen(false);
      } else if (conversation?.scopeType === "global") {
        setSelectedSessionId(task.session_id);
        setSelectedTaskId(task.id);
        setTab("chat");
      }
    },
  });
  const archiveMutation = useMutation({
    mutationFn: (sessionId: string) => api.archiveAgentSession(sessionId),
    onSuccess: () => {
      setArchiveTarget(null);
      invalidateAgentLists();
    },
  });
  const cancelTaskMutation = useMutation({
    mutationFn: (taskId: string) => api.cancelAgentTask(taskId),
    onSuccess: invalidateAgentLists,
  });

  const openWorkspace = (target: AgentWorkspaceTarget, sessionId: string, taskId?: string) => {
    const params = new URLSearchParams({ agent_session_id: sessionId });
    if (taskId) {
      params.set("agent_task_id", taskId);
    }
    navigate(`/products/${encodeURIComponent(target.productId)}?${params}`);
    setOpen(false);
  };
  const openGlobalConversation = (sessionId: string, taskId: string | null = null) => {
    setSelectedSessionId(sessionId);
    setSelectedTaskId(taskId);
    setTab("chat");
  };
  const startTaskForm = () => {
    const initialSession = workspaceSessions[0];
    setTaskSessionId(initialSession?.id ?? "");
    setTaskConversationId(initialSession ? preferredConversation(initialSession)?.conversation_id ?? "" : "");
    setTaskTitle("");
    setTaskGoal("");
    createTaskMutation.reset();
    setTaskFormOpen(true);
    setSessionFormOpen(false);
    setTab("tasks");
  };
  const submitTask = () => {
    const title = taskTitle.trim();
    const goal = taskGoal.trim();
    if (!taskSessionId || !taskConversationId || !title || !goal || createTaskMutation.isPending) {
      return;
    }
    createTaskMutation.mutate({
      session_id: taskSessionId,
      conversation_id: taskConversationId,
      title,
      goal,
    });
  };
  const submitSession = () => {
    if (!sessionTitle.trim() || createSessionMutation.isPending) {
      return;
    }
    createSessionMutation.mutate();
  };

  const queryError = sessionsQuery.error ?? tasksQuery.error;
  const mutationError = createTaskMutation.error ?? createSessionMutation.error ?? archiveMutation.error ?? cancelTaskMutation.error;
  const errorText = queryError || mutationError
    ? errorDetail(queryError ?? mutationError, t("globalAgent.requestFailed"))
    : null;

  return (
    <div ref={rootRef} data-global-agent-dock className="pointer-events-none fixed inset-x-0 bottom-0 z-[60] sm:inset-x-auto sm:bottom-5 sm:right-5">
      {open ? (
        <section
          id="global-agent-dock-panel"
          aria-label={t("globalAgent.title")}
          className="pointer-events-auto absolute bottom-[calc(8.25rem+env(safe-area-inset-bottom))] left-3 right-3 flex max-h-[min(720px,calc(100dvh-11.5rem))] flex-col overflow-hidden rounded-lg border border-border-l2 bg-surface-raised text-text-primary shadow-[0_20px_60px_rgb(15_23_42_/_0.22)] dark:shadow-[0_24px_70px_rgb(0_0_0_/_0.46)] sm:bottom-0 sm:left-auto sm:right-0 sm:w-[min(92vw,400px)]"
        >
          <header className="flex shrink-0 items-center gap-3 border-b border-border-l1 px-4 py-3">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-accent text-accent-fg">
              <Bot size={18} aria-hidden="true" />
            </span>
            <div className="min-w-0 flex-1">
              <h2 className="truncate text-sm font-semibold">{t("globalAgent.title")}</h2>
              <p className="mt-0.5 truncate text-xs text-text-secondary">
                {t("globalAgent.activeTasks", { count: activeTaskCount })}
              </p>
            </div>
            <button
              type="button"
              onClick={() => setOpen(false)}
              aria-label={t("globalAgent.close")}
              title={t("globalAgent.close")}
              className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
            >
              <X size={17} aria-hidden="true" />
            </button>
          </header>

          <div className="flex shrink-0 items-center gap-1 border-b border-border-l1 bg-surface-subtle/60 px-3 py-2" role="tablist" aria-label={t("globalAgent.views")}>
            <DockTab
              active={tab === "chat"}
              count={sessions.length}
              icon={<Bot size={14} aria-hidden="true" />}
              label={t("globalAgent.chat")}
              onClick={() => {
                setTaskFormOpen(false);
                setSessionFormOpen(false);
                setSelectedTaskId(null);
                setTab("chat");
              }}
            />
            <DockTab
              active={tab === "tasks"}
              count={tasks.length}
              icon={<ClipboardList size={14} aria-hidden="true" />}
              label={t("globalAgent.tasks")}
              onClick={() => setTab("tasks")}
            />
            <DockTab
              active={tab === "sessions"}
              count={sessions.length}
              icon={<MessagesSquare size={14} aria-hidden="true" />}
              label={t("globalAgent.sessions")}
              onClick={() => setTab("sessions")}
            />
            <button
              type="button"
              onClick={tab === "tasks" ? startTaskForm : () => {
                setSessionFormOpen(true);
                setTaskFormOpen(false);
                setTab("sessions");
                createSessionMutation.reset();
              }}
              aria-label={tab === "tasks" ? t("globalAgent.newTask") : t("globalAgent.newSession")}
              title={tab === "tasks" ? t("globalAgent.newTask") : t("globalAgent.newSession")}
              className="ml-auto flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface-raised hover:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
            >
              <Plus size={16} aria-hidden="true" />
            </button>
          </div>

          <div className="flex min-h-0 flex-1 flex-col">
            {tab === "tasks" && taskFormOpen ? (
              <TaskForm
                busy={createTaskMutation.isPending}
                error={createTaskMutation.error ? errorDetail(createTaskMutation.error, t("globalAgent.requestFailed")) : null}
                goal={taskGoal}
                onCancel={() => setTaskFormOpen(false)}
                onGoalChange={setTaskGoal}
                onSubmit={submitTask}
                onTitleChange={setTaskTitle}
                onSessionChange={(sessionId) => {
                  const nextSession = workspaceSessions.find((session) => session.id === sessionId);
                  setTaskSessionId(sessionId);
                  setTaskConversationId(nextSession ? preferredConversation(nextSession)?.conversation_id ?? "" : "");
                }}
                onWorkspaceChange={setTaskConversationId}
                sessions={workspaceSessions}
                sessionId={taskSessionId}
                title={taskTitle}
                conversationId={taskConversationId}
                workspaces={taskWorkspaces}
              />
            ) : tab === "sessions" && sessionFormOpen ? (
              <SessionForm
                busy={createSessionMutation.isPending}
                error={createSessionMutation.error ? errorDetail(createSessionMutation.error, t("globalAgent.requestFailed")) : null}
                onCancel={() => setSessionFormOpen(false)}
                onChange={setSessionTitle}
                onSubmit={submitSession}
                value={sessionTitle}
              />
            ) : tab === "chat" ? (
              <>
                {errorText ? (
                  <p role="alert" className="shrink-0 border-b border-state-error/20 bg-state-error/10 px-3 py-2 text-xs leading-5 text-state-error">
                    {errorText}
                  </p>
                ) : null}
                <div className="flex min-h-0 flex-1 flex-col">
                  <div className="shrink-0 border-b border-border-l1 bg-surface-subtle/60 px-3 py-2">
                    <label className="sr-only" htmlFor="global-agent-session-select">{t("globalAgent.sessions")}</label>
                    <select
                      id="global-agent-session-select"
                      value={activeSession?.id ?? ""}
                      onChange={(event) => openGlobalConversation(event.target.value)}
                      className="h-9 w-full rounded-md border border-border-l2 bg-surface-raised px-2.5 text-xs text-text-primary outline-none focus:border-accent focus:ring-2 focus:ring-accent/15"
                    >
                      {sessions.map((session) => <option key={session.id} value={session.id}>{session.title}</option>)}
                    </select>
                  </div>
                  <GlobalAgentConversationPanel
                    conversationId={globalConversation?.conversation_id ?? null}
                    sessionTitle={activeSession?.title ?? t("globalAgent.title")}
                    taskId={selectedTaskId}
                    pageContext={pageContext}
                  />
                </div>
              </>
            ) : (
              <>
                <div className="shrink-0 border-b border-border-l1 p-3">
                  <label className="flex h-9 items-center gap-2 rounded-md border border-border-l2 bg-surface-subtle px-2.5 focus-within:border-accent focus-within:ring-2 focus-within:ring-accent/15">
                    <Search size={14} className="shrink-0 text-text-muted" aria-hidden="true" />
                    <span className="sr-only">{t("globalAgent.search")}</span>
                    <input
                      value={search}
                      onChange={(event) => setSearch(event.target.value)}
                      placeholder={t("globalAgent.searchPlaceholder")}
                      className="min-w-0 flex-1 border-0 bg-transparent text-xs text-text-primary outline-none placeholder:text-text-muted"
                    />
                    {search ? (
                      <button
                        type="button"
                        onClick={() => setSearch("")}
                        aria-label={t("globalAgent.clearSearch")}
                        title={t("globalAgent.clearSearch")}
                        className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-text-muted hover:bg-surface-raised hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
                      >
                        <X size={13} aria-hidden="true" />
                      </button>
                    ) : null}
                  </label>
                </div>
                {errorText ? (
                  <p role="alert" className="shrink-0 border-b border-state-error/20 bg-state-error/10 px-3 py-2 text-xs leading-5 text-state-error">
                    {errorText}
                  </p>
                ) : null}
                <div className="min-h-0 flex-1 overflow-y-auto p-2">
                  {tab === "tasks" ? (
                    <TaskList
                      loading={tasksQuery.isLoading}
                      tasks={visibleTasks}
                      workspaceByConversationId={workspaceByConversationId}
                      conversationById={conversationById}
                      onOpen={(task, workspace) => {
                        if (workspace) {
                          openWorkspace(workspace, task.session_id, task.id);
                        } else if (task.conversation_id && conversationById.get(task.conversation_id)?.scopeType === "global") {
                          openGlobalConversation(task.session_id, task.id);
                        }
                      }}
                      onCancel={(task) => cancelTaskMutation.mutate(task.id)}
                      cancelingTaskId={cancelTaskMutation.isPending ? cancelTaskMutation.variables : null}
                      emptyLabel={normalizedSearch ? t("globalAgent.noMatch") : t("globalAgent.noTasks")}
                      statusLabel={(status) => t(TASK_STATUS_LABEL_KEYS[status])}
                    />
                  ) : (
                    <SessionList
                      loading={sessionsQuery.isLoading}
                      sessions={visibleSessions}
                      currentSessionId={activeSessionId}
                      onOpen={(session) => {
                        const global = session.conversations.find((conversation) => conversation.scope_type === "global");
                        if (global) {
                          openGlobalConversation(session.id);
                          return;
                        }
                        const product = session.conversations.find((conversation) => conversation.scope_type === "product_workflow" && conversation.product_id);
                        if (product?.product_id) {
                          openWorkspace({ productId: product.product_id, conversationId: product.conversation_id }, session.id);
                        }
                      }}
                      onArchive={setArchiveTarget}
                      emptyLabel={normalizedSearch ? t("globalAgent.noMatch") : t("globalAgent.noSessions")}
                    />
                  )}
                </div>
              </>
            )}
          </div>
        </section>
      ) : null}

      <button
        type="button"
        data-global-agent-launcher
        aria-expanded={open}
        aria-controls="global-agent-dock-panel"
        aria-label={open ? t("globalAgent.close") : t("globalAgent.open")}
        title={open ? t("globalAgent.close") : t("globalAgent.open")}
        onClick={() => setOpen((current) => !current)}
        className="pointer-events-auto absolute bottom-[calc(4.25rem+env(safe-area-inset-bottom))] right-3 flex h-12 w-12 items-center justify-center rounded-full border border-accent/30 bg-accent text-accent-fg shadow-[0_10px_28px_rgb(15_23_42_/_0.2)] transition-transform hover:-translate-y-0.5 hover:bg-accent-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/60 dark:shadow-[0_12px_32px_rgb(0_0_0_/_0.42)] sm:bottom-0 sm:right-0"
      >
        <Bot size={20} aria-hidden="true" />
        {activeTaskCount > 0 ? (
          <span className="absolute -right-1 -top-1 flex h-5 min-w-5 items-center justify-center rounded-full border-2 border-surface-raised bg-state-warning px-1 text-[10px] font-bold text-text-primary">
            {activeTaskCount > 99 ? "99+" : activeTaskCount}
          </span>
        ) : null}
      </button>

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
    </div>
  );
}

function DockTab({
  active,
  count,
  icon,
  label,
  onClick,
}: {
  active: boolean;
  count: number;
  icon: ReactNode;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={`inline-flex h-8 items-center gap-1.5 rounded-md px-2.5 text-xs font-semibold transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 ${
        active ? "bg-surface-raised text-accent shadow-sm" : "text-text-secondary hover:bg-surface-raised/70 hover:text-text-primary"
      }`}
    >
      {icon}
      {label}
      <span className="text-[10px] font-medium text-text-muted">{count}</span>
    </button>
  );
}

function TaskList({
  loading,
  tasks,
  workspaceByConversationId,
  conversationById,
  onOpen,
  onCancel,
  cancelingTaskId,
  emptyLabel,
  statusLabel,
}: {
  loading: boolean;
  tasks: AgentTask[];
  workspaceByConversationId: Map<string, {
    productId: string;
    conversationId: string;
    productName: string;
    sessionId: string;
  }>;
  conversationById: Map<string, AgentConversationTarget>;
  onOpen: (task: AgentTask, workspace: AgentWorkspaceTarget | null) => void;
  onCancel: (task: AgentTask) => void;
  cancelingTaskId: string | null;
  emptyLabel: string;
  statusLabel: (status: AgentTaskStatus) => string;
}) {
  const { t } = useI18n();
  if (loading) {
    return <LoadingDockState label={t("app.loading")} />;
  }
  if (!tasks.length) {
    return <EmptyDockState icon={<ClipboardList size={18} />} label={emptyLabel} />;
  }
  return (
    <div className="space-y-1">
      {tasks.map((task) => {
        const workspace = task.conversation_id ? workspaceByConversationId.get(task.conversation_id) : null;
        const conversation = task.conversation_id ? conversationById.get(task.conversation_id) : null;
        const target = workspace
          ? { productId: workspace.productId, conversationId: workspace.conversationId }
          : null;
        const openable = Boolean(target || conversation?.scopeType === "global");
        const cancelable = ACTIVE_TASK_STATUSES.has(task.status);
        return (
          <div key={task.id} className="group flex w-full min-w-0 items-start gap-2 rounded-md px-2.5 py-2.5 transition-colors hover:bg-surface-subtle">
            <button
              type="button"
              disabled={!openable}
              onClick={() => onOpen(task, target)}
              className="flex min-w-0 flex-1 items-start gap-2.5 text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 disabled:cursor-default"
            >
            <span className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${TASK_STATUS_CLASSES[task.status]}`} aria-hidden="true" />
            <span className="min-w-0 flex-1">
              <span className="flex min-w-0 items-center gap-2">
                <span className="min-w-0 flex-1 truncate text-sm font-semibold text-text-primary" title={task.title}>
                  {task.title}
                </span>
                <span className="shrink-0 text-[10px] font-medium text-text-muted">
                  {statusLabel(task.status)}
                </span>
              </span>
              <span className="mt-0.5 block truncate text-xs text-text-secondary" title={task.goal}>
                {task.goal}
              </span>
              <span className="mt-1 block truncate text-[11px] text-text-muted">
                {conversation?.productName ?? workspace?.productName ?? t("globalAgent.noWorkspace")}
              </span>
            </span>
            {openable ? <ChevronRight size={14} className="mt-1 shrink-0 text-text-muted opacity-0 transition-opacity group-hover:opacity-100" aria-hidden="true" /> : null}
            </button>
            {cancelable ? (
              <button
                type="button"
                onClick={() => onCancel(task)}
                disabled={cancelingTaskId !== null}
                aria-label={cancelingTaskId === task.id ? t("globalAgent.cancelingTask") : t("globalAgent.cancelTask")}
                title={cancelingTaskId === task.id ? t("globalAgent.cancelingTask") : t("globalAgent.cancelTask")}
                className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-text-muted opacity-70 transition-colors hover:bg-state-error/10 hover:text-state-error focus:outline-none focus-visible:ring-2 focus-visible:ring-state-error/50 sm:opacity-0 sm:group-hover:opacity-100 disabled:cursor-wait disabled:opacity-100"
              >
                {cancelingTaskId === task.id ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : <X size={14} aria-hidden="true" />}
              </button>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

function SessionList({
  loading,
  sessions,
  currentSessionId,
  onOpen,
  onArchive,
  emptyLabel,
}: {
  loading: boolean;
  sessions: AgentSession[];
  currentSessionId: string | null;
  onOpen: (session: AgentSession) => void;
  onArchive: (session: AgentSession) => void;
  emptyLabel: string;
}) {
  const { t } = useI18n();
  if (loading) {
    return <LoadingDockState label={t("app.loading")} />;
  }
  if (!sessions.length) {
    return <EmptyDockState icon={<MessagesSquare size={18} />} label={emptyLabel} />;
  }
  return (
    <div className="space-y-1">
      {sessions.map((session) => {
        const conversation = preferredConversation(session);
        const selected = currentSessionId === session.id;
        return (
          <div
            key={session.id}
            className={`group flex min-w-0 items-start gap-2 rounded-md px-2.5 py-2.5 transition-colors ${selected ? "bg-accent-soft" : "hover:bg-surface-subtle"}`}
          >
            <button
              type="button"
              disabled={!conversation}
              onClick={() => onOpen(session)}
              className="flex min-w-0 flex-1 items-start gap-2.5 text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 disabled:cursor-default"
            >
              <span className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${selected ? "bg-accent" : session.status === "active" ? "bg-state-success" : "bg-text-muted/50"}`} aria-hidden="true" />
              <span className="min-w-0 flex-1">
                <span className="flex min-w-0 items-center gap-2">
                  <span className="min-w-0 flex-1 truncate text-sm font-semibold text-text-primary" title={session.title}>
                    {session.title}
                  </span>
                  {session.status === "archived" ? <span className="shrink-0 text-[10px] text-text-muted">{t("globalAgent.archived")}</span> : null}
                </span>
                <span className="mt-0.5 block truncate text-xs text-text-secondary">
                  {conversation?.product_name ?? t("globalAgent.noWorkspace")}
                </span>
              </span>
            </button>
            {session.status === "active" ? (
              <button
                type="button"
                onClick={() => onArchive(session)}
                aria-label={t("globalAgent.archiveSession")}
                title={t("globalAgent.archiveSession")}
                className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-text-muted opacity-70 transition-colors hover:bg-state-error/10 hover:text-state-error focus:outline-none focus-visible:ring-2 focus-visible:ring-state-error/50 sm:opacity-0 sm:group-hover:opacity-100"
              >
                <Archive size={14} aria-hidden="true" />
              </button>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

function TaskForm({
  busy,
  error,
  goal,
  onCancel,
  onGoalChange,
  onSubmit,
  onTitleChange,
  onSessionChange,
  onWorkspaceChange,
  sessions,
  sessionId,
  title,
  conversationId,
  workspaces,
}: {
  busy: boolean;
  error: string | null;
  goal: string;
  onCancel: () => void;
  onGoalChange: (value: string) => void;
  onSubmit: () => void;
  onTitleChange: (value: string) => void;
  onSessionChange: (value: string) => void;
  onWorkspaceChange: (value: string) => void;
  sessions: AgentSession[];
  sessionId: string;
  title: string;
  conversationId: string;
  workspaces: AgentSession["conversations"];
}) {
  const { t } = useI18n();
  const canSubmit = Boolean(sessionId && conversationId && title.trim() && goal.trim() && !busy);
  return (
    <div className="min-h-0 flex-1 overflow-y-auto p-3">
      <div className="mb-3 flex items-center gap-2">
        <ClipboardList size={16} className="text-accent" aria-hidden="true" />
        <h3 className="text-sm font-semibold text-text-primary">{t("globalAgent.newTask")}</h3>
      </div>
      {!sessions.length ? (
        <p className="rounded-md border border-state-warning/20 bg-state-warning/10 px-3 py-2 text-xs leading-5 text-text-secondary">
          {t("globalAgent.taskNoWorkspace")}
        </p>
      ) : (
        <div className="space-y-3">
          <label className="block">
            <span className="mb-1 block text-[11px] font-semibold text-text-secondary">{t("globalAgent.taskSession")}</span>
            <select value={sessionId} onChange={(event) => onSessionChange(event.target.value)} className="h-9 w-full rounded-md border border-border-l2 bg-surface-raised px-2.5 text-xs text-text-primary outline-none focus:border-accent focus:ring-2 focus:ring-accent/15">
              {sessions.map((session) => <option key={session.id} value={session.id}>{session.title}</option>)}
            </select>
          </label>
          <label className="block">
            <span className="mb-1 block text-[11px] font-semibold text-text-secondary">{t("globalAgent.taskWorkspace")}</span>
            <select value={conversationId} onChange={(event) => onWorkspaceChange(event.target.value)} className="h-9 w-full rounded-md border border-border-l2 bg-surface-raised px-2.5 text-xs text-text-primary outline-none focus:border-accent focus:ring-2 focus:ring-accent/15">
              {workspaces.map((workspace) => <option key={workspace.conversation_id} value={workspace.conversation_id}>{workspace.product_name}</option>)}
            </select>
          </label>
          <label className="block">
            <span className="mb-1 block text-[11px] font-semibold text-text-secondary">{t("globalAgent.taskTitle")}</span>
            <input value={title} onChange={(event) => onTitleChange(event.target.value)} placeholder={t("globalAgent.taskTitlePlaceholder")} className="h-9 w-full rounded-md border border-border-l2 bg-surface-raised px-2.5 text-xs text-text-primary outline-none placeholder:text-text-muted focus:border-accent focus:ring-2 focus:ring-accent/15" />
          </label>
          <label className="block">
            <span className="mb-1 block text-[11px] font-semibold text-text-secondary">{t("globalAgent.taskGoal")}</span>
            <textarea value={goal} onChange={(event) => onGoalChange(event.target.value)} placeholder={t("globalAgent.taskGoalPlaceholder")} rows={3} className="w-full resize-y rounded-md border border-border-l2 bg-surface-raised px-2.5 py-2 text-xs leading-5 text-text-primary outline-none placeholder:text-text-muted focus:border-accent focus:ring-2 focus:ring-accent/15" />
          </label>
          {error ? <p role="alert" className="text-xs leading-5 text-state-error">{error}</p> : null}
          <div className="flex justify-end gap-2 border-t border-border-l1 pt-3">
            <button type="button" onClick={onCancel} disabled={busy} className="inline-flex h-9 items-center rounded-md px-3 text-xs font-semibold text-text-secondary hover:bg-surface-subtle focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50">{t("common.cancel")}</button>
            <button type="button" onClick={onSubmit} disabled={!canSubmit} className="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent px-3 text-xs font-semibold text-accent-fg hover:bg-accent-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 disabled:cursor-not-allowed disabled:opacity-50">
              {busy ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : <Plus size={14} aria-hidden="true" />}
              {busy ? t("globalAgent.creating") : t("globalAgent.create")}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function SessionForm({
  busy,
  error,
  onCancel,
  onChange,
  onSubmit,
  value,
}: {
  busy: boolean;
  error: string | null;
  onCancel: () => void;
  onChange: (value: string) => void;
  onSubmit: () => void;
  value: string;
}) {
  const { t } = useI18n();
  return (
    <div className="min-h-0 flex-1 overflow-y-auto p-3">
      <div className="mb-3 flex items-center gap-2">
        <MessagesSquare size={16} className="text-accent" aria-hidden="true" />
        <h3 className="text-sm font-semibold text-text-primary">{t("globalAgent.newSession")}</h3>
      </div>
      <label className="block">
        <span className="mb-1 block text-[11px] font-semibold text-text-secondary">{t("agentWorkbench.session.titleLabel")}</span>
        <input autoFocus value={value} onChange={(event) => onChange(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") onSubmit(); }} placeholder={t("agentWorkbench.session.titlePlaceholder")} className="h-9 w-full rounded-md border border-border-l2 bg-surface-raised px-2.5 text-xs text-text-primary outline-none placeholder:text-text-muted focus:border-accent focus:ring-2 focus:ring-accent/15" />
      </label>
      {error ? <p role="alert" className="mt-2 text-xs leading-5 text-state-error">{error}</p> : null}
      <div className="mt-4 flex justify-end gap-2 border-t border-border-l1 pt-3">
        <button type="button" onClick={onCancel} disabled={busy} className="inline-flex h-9 items-center rounded-md px-3 text-xs font-semibold text-text-secondary hover:bg-surface-subtle focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50">{t("common.cancel")}</button>
        <button type="button" onClick={onSubmit} disabled={!value.trim() || busy} className="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent px-3 text-xs font-semibold text-accent-fg hover:bg-accent-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 disabled:cursor-not-allowed disabled:opacity-50">
          {busy ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : <Plus size={14} aria-hidden="true" />}
          {busy ? t("globalAgent.creating") : t("globalAgent.create")}
        </button>
      </div>
    </div>
  );
}

function EmptyDockState({ icon, label }: { icon: ReactNode; label: string }) {
  return (
    <div className="flex min-h-36 flex-col items-center justify-center gap-2 px-4 text-center text-text-muted">
      {icon}
      <p className="text-xs">{label}</p>
    </div>
  );
}

function LoadingDockState({ label }: { label: string }) {
  return (
    <div className="flex min-h-36 items-center justify-center gap-2 px-4 text-xs text-text-muted">
      <Loader2 size={16} className="animate-spin motion-reduce:animate-none" aria-hidden="true" />
      <span>{label}</span>
    </div>
  );
}

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    return error.detail;
  }
  return error instanceof Error ? error.message : fallback;
}

function preferredConversation(session: AgentSession): AgentSession["conversations"][number] | null {
  return session.conversations.find((conversation) => conversation.scope_type === "global")
    ?? session.conversations.find((conversation) => conversation.scope_type === "product_workflow" && conversation.product_id)
    ?? session.conversations[0]
    ?? null;
}

function buildPageContext(pathname: string, search: string): AgentPageContextSnapshotInput {
  const productId = routeSegment(pathname, /\/products\/([^/]+)/);
  const workflowId = routeSegment(pathname, /\/workflows\/([^/]+)/);
  let pageType = "app";
  if (pathname.startsWith("/media-library") || pathname.startsWith("/gallery")) {
    pageType = "media_library";
  } else if (workflowId) {
    pageType = "workflow";
  } else if (productId) {
    pageType = "product";
  }
  return {
    route: `${pathname}${search}`.slice(0, 512),
    page_type: pageType,
    product_id: productId,
    workflow_id: workflowId,
    selected_asset_ids: [],
    visible_asset_ids: [],
    filters: {},
    captured_at: new Date().toISOString(),
  };
}

function routeSegment(pathname: string, pattern: RegExp): string | null {
  const match = pathname.match(pattern);
  if (!match?.[1]) {
    return null;
  }
  try {
    return decodeURIComponent(match[1]);
  } catch {
    return match[1];
  }
}
