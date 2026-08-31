import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  Bot,
  Check,
  ChevronRight,
  ClipboardList,
  Columns2,
  LayoutGrid,
  List,
  Loader2,
  Maximize2,
  MessagesSquare,
  Minimize2,
  Minus,
  PackagePlus,
  Pause,
  Pencil,
  Play,
  Plus,
  Search,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { ApiError, api } from "../lib/api";
import { useAgentPageContext } from "../lib/agentPageContext";
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
import {
  applyDockModeWidth,
  BUBBLE_POSITION_STORAGE_KEY,
  clampBubblePosition,
  clampWindowPosition,
  DOCK_HEIGHT_STORAGE_KEY,
  DOCK_MODE_STORAGE_KEY,
  DOCK_POSITION_STORAGE_KEY,
  DOCK_WIDTH_STORAGE_KEY,
  defaultBubblePosition,
  defaultWindowPosition,
  readDockPoint,
  readDockSize,
  resizeDockWindow,
  resolveDockMode,
  type DockPoint,
  type DockSize,
  type GlobalAgentDockMode,
  type ResizeDirection,
} from "./globalAgentDockState";
import { GlobalAgentConversationPanel } from "../pages/workbench/agent/GlobalAgentConversationPanel";

type GlobalAgentDockTab = "chat" | "tasks" | "sessions";

const GLOBAL_AGENT_MODAL_SELECTOR = "[data-global-agent-modal]";

export function isGlobalAgentDockModalTarget(target: EventTarget | null): boolean {
  if (!target || (typeof target !== "object" && typeof target !== "function")) {
    return false;
  }
  const closest = (target as { closest?: unknown }).closest;
  return typeof closest === "function"
    && Boolean((closest as (selector: string) => unknown).call(target, GLOBAL_AGENT_MODAL_SELECTOR));
}

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
]);

const CANCELABLE_TASK_STATUSES = new Set<AgentTaskStatus>([
  ...ACTIVE_TASK_STATUSES,
  "paused",
]);

const PAUSABLE_TASK_STATUSES = new Set<AgentTaskStatus>([
  "queued",
  "waiting_user",
  "awaiting_confirmation",
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

/** 画布 Session 不进全局 Dock 列表。商品 Task 必须用自身 product_id 打开，不能只靠会话映射。 */
export function agentTaskWorkspaceTarget(
  task: Pick<AgentTask, "conversation_id" | "product_id">,
  workspaceByConversationId: Map<string, { productId: string; conversationId: string }>,
): AgentWorkspaceTarget | null {
  if (task.conversation_id) {
    const mapped = workspaceByConversationId.get(task.conversation_id);
    if (mapped) {
      return { productId: mapped.productId, conversationId: mapped.conversationId };
    }
  }
  if (task.product_id) {
    return { productId: task.product_id, conversationId: task.conversation_id ?? "" };
  }
  return null;
}

interface AgentConversationTarget {
  conversationId: string;
  sessionId: string;
  scopeType: "product_workflow" | "global";
  productId: string | null;
  productName: string;
}

export function openGlobalAgent(options?: { tab?: GlobalAgentDockTab; sessionId?: string; taskId?: string }) {
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent("productflow:open-agent", { detail: options }));
  }
}

/**
 * 商品工作台自带嵌入式 Agent 对话。把这条路由判断留在 Dock 内部，
 * 启动器就不会依赖路由里已经不存在的旧 URL 片段。
 */
export function isProductWorkbenchPath(pathname: string): boolean {
  const segments = pathname.split("/").filter(Boolean);
  return segments.length === 2 && segments[0] === "products" && segments[1] !== "new";
}

export function shouldRenderGlobalAgentLauncher(pathname: string, open: boolean): boolean {
  return !isProductWorkbenchPath(pathname) || open;
}

export function agentDockListRefetchInterval(open: boolean, sseFallback: boolean): number | false {
  return open && sseFallback ? 2_000 : false;
}

export async function reconcileAgentDockListsOnControlOpen(
  refetchSessions: () => Promise<unknown>,
  refetchTasks: () => Promise<unknown>,
  onReconciled: () => void,
): Promise<void> {
  await Promise.all([refetchSessions(), refetchTasks()]);
  onReconciled();
}

export function GlobalAgentDock() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const registeredPageContext = useAgentPageContext();
  const rootRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState<GlobalAgentDockTab>("chat");
  const [search, setSearch] = useState("");
  const [taskFormOpen, setTaskFormOpen] = useState(false);
  const [taskSessionId, setTaskSessionId] = useState("");
  const [taskConversationId, setTaskConversationId] = useState("");
  const [taskTitle, setTaskTitle] = useState("");
  const [taskGoal, setTaskGoal] = useState("");
  const [archiveTarget, setArchiveTarget] = useState<AgentSession | null>(null);
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null);
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const [selectedTaskSnapshot, setSelectedTaskSnapshot] = useState<AgentTask | null>(null);
  const [renamingSessionId, setRenamingSessionId] = useState<string | null>(null);
  const [renamingTaskId, setRenamingTaskId] = useState<string | null>(null);
  const [taskViewMode, setTaskViewMode] = useState<"list" | "board">("list");

  const [dockMode, setDockMode] = useState<GlobalAgentDockMode>(() => {
    try {
      return resolveDockMode(localStorage.getItem(DOCK_MODE_STORAGE_KEY));
    } catch {
      return "compact";
    }
  });

  const [windowSize, setWindowSize] = useState<DockSize>(() => {
    try {
      return readDockSize(
        localStorage.getItem(DOCK_WIDTH_STORAGE_KEY),
        localStorage.getItem(DOCK_HEIGHT_STORAGE_KEY),
      );
    } catch {
      return { width: 480, height: 680 };
    }
  });

  const [windowPos, setWindowPos] = useState<DockPoint | null>(() => {
    try {
      return readDockPoint(localStorage.getItem(DOCK_POSITION_STORAGE_KEY));
    } catch (_error) {
      void _error;
      return null;
    }
  });

  const [bubblePos, setBubblePos] = useState<DockPoint | null>(() => {
    try {
      return readDockPoint(localStorage.getItem(BUBBLE_POSITION_STORAGE_KEY));
    } catch (_error) {
      void _error;
      return null;
    }
  });

  const [isDraggingWindow, setIsDraggingWindow] = useState(false);
  const [isResizingWindow, setIsResizingWindow] = useState(false);
  const [isDraggingBubble, setIsDraggingBubble] = useState(false);
  const [controlEventsFallback, setControlEventsFallback] = useState(false);
  const [leaseHealth, setLeaseHealth] = useState<{ phase: string; executionId: string; at: number } | null>(null);

  const changeDockMode = (nextMode: GlobalAgentDockMode) => {
    setDockMode(nextMode);
    try {
      localStorage.setItem(DOCK_MODE_STORAGE_KEY, nextMode);
    } catch (_error) {
      void _error;
    }
    if (nextMode === "wide" || nextMode === "compact") {
      setWindowSize((cur) => applyDockModeWidth(cur, nextMode));
    }
  };

  // 悬浮球自由拖拽
  const handleBubblePointerDown = (event: React.PointerEvent) => {
    if (event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    const startX = event.clientX;
    const startY = event.clientY;
    const startPos = bubblePos ?? defaultBubblePosition(window.innerWidth, window.innerHeight);
    let hasMoved = false;

    const onPointerMove = (moveEvent: PointerEvent) => {
      const deltaX = moveEvent.clientX - startX;
      const deltaY = moveEvent.clientY - startY;
      if (Math.abs(deltaX) > 3 || Math.abs(deltaY) > 3) {
        hasMoved = true;
        setIsDraggingBubble(true);
      }
      setBubblePos(
        clampBubblePosition(
          { x: startPos.x + deltaX, y: startPos.y + deltaY },
          window.innerWidth,
          window.innerHeight,
        ),
      );
    };

    const onPointerUp = () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
      setIsDraggingBubble(false);
      if (!hasMoved) {
        setOpen((cur) => !cur);
      } else {
        setBubblePos((cur) => {
          if (cur) {
            try {
              localStorage.setItem(BUBBLE_POSITION_STORAGE_KEY, JSON.stringify(cur));
            } catch (_error) {
              void _error;
            }
          }
          return cur;
        });
      }
    };

    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", onPointerUp);
  };

  // 窗口顶部 Header 拖拽平移移动
  const handleHeaderPointerDown = (event: React.PointerEvent) => {
    if (dockMode === "fullscreen" || event.button !== 0) return;
    if ((event.target as HTMLElement).closest("button, a, input, textarea, select")) return;
    event.preventDefault();
    setIsDraggingWindow(true);

    const startClientX = event.clientX;
    const startClientY = event.clientY;
    const effectivePos = windowPos ?? defaultWindowPosition(window.innerWidth, window.innerHeight, windowSize);
    const startX = effectivePos.x;
    const startY = effectivePos.y;

    const onPointerMove = (moveEvent: PointerEvent) => {
      const deltaX = moveEvent.clientX - startClientX;
      const deltaY = moveEvent.clientY - startClientY;
      setWindowPos(
        clampWindowPosition(
          { x: startX + deltaX, y: startY + deltaY },
          windowSize,
          window.innerWidth,
          window.innerHeight,
        ),
      );
    };

    const onPointerUp = () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
      setIsDraggingWindow(false);
      setWindowPos((cur) => {
        if (cur) {
          try {
            localStorage.setItem(DOCK_POSITION_STORAGE_KEY, JSON.stringify(cur));
          } catch (_error) {
            void _error;
          }
        }
        return cur;
      });
    };

    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", onPointerUp);
  };

  // 窗口八个方向自由缩放
  const handleResizePointerDown = (dir: ResizeDirection, event: React.PointerEvent) => {
    if (dockMode === "fullscreen" || event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    setIsResizingWindow(true);

    const startClientX = event.clientX;
    const startClientY = event.clientY;
    const effectivePos = windowPos ?? defaultWindowPosition(window.innerWidth, window.innerHeight, windowSize);
    const startX = effectivePos.x;
    const startY = effectivePos.y;
    const startW = windowSize.width;
    const startH = windowSize.height;

    const onPointerMove = (moveEvent: PointerEvent) => {
      const deltaX = moveEvent.clientX - startClientX;
      const deltaY = moveEvent.clientY - startClientY;
      const next = resizeDockWindow(
        { width: startW, height: startH },
        { x: startX, y: startY },
        dir,
        deltaX,
        deltaY,
        window.innerWidth,
        window.innerHeight,
      );
      setWindowSize(next.size);
      setWindowPos(next.pos);
    };

    const onPointerUp = () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
      setIsResizingWindow(false);
      setWindowSize((curSize) => {
        try {
          localStorage.setItem(DOCK_WIDTH_STORAGE_KEY, String(curSize.width));
          localStorage.setItem(DOCK_HEIGHT_STORAGE_KEY, String(curSize.height));
        } catch (_error) {
          void _error;
        }
        return curSize;
      });
      setWindowPos((curPos) => {
        if (curPos) {
          try {
            localStorage.setItem(DOCK_POSITION_STORAGE_KEY, JSON.stringify(curPos));
          } catch (_error) {
            void _error;
          }
        }
        return curPos;
      });
    };

    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", onPointerUp);
  };

  useEffect(() => {
    const handleOpenAgent = (event: Event) => {
      const custom = event as CustomEvent<{ tab?: GlobalAgentDockTab; sessionId?: string; taskId?: string }>;
      setOpen(true);
      if (custom.detail?.tab) {
        setTab(custom.detail.tab);
      }
      if (custom.detail?.sessionId) {
        setSelectedSessionId(custom.detail.sessionId);
      }
      if (custom.detail?.taskId !== undefined) {
        setSelectedTaskId(custom.detail.taskId);
      }
    };
    window.addEventListener("productflow:open-agent", handleOpenAgent);
    return () => window.removeEventListener("productflow:open-agent", handleOpenAgent);
  }, []);

  const sessionsQuery = useInfiniteQuery({
    queryKey: ["agent-sessions", true],
    initialPageParam: null as string | null,
    queryFn: ({ pageParam }) => api.listAgentSessions(true, undefined, { after: pageParam }),
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    staleTime: 15_000,
    refetchInterval: agentDockListRefetchInterval(open, controlEventsFallback),
  });
  const tasksQuery = useInfiniteQuery({
    queryKey: ["agent-tasks", null, true],
    initialPageParam: null as string | null,
    queryFn: ({ pageParam }) => api.listAgentTasks({
      includeTerminal: true,
      limit: 100,
      after: pageParam,
    }),
    getNextPageParam: (lastPage) => lastPage.next_cursor,
    staleTime: 8_000,
    refetchInterval: agentDockListRefetchInterval(open, controlEventsFallback),
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
      setControlEventsFallback(false);
      return;
    }
    if (typeof EventSource === "undefined") {
      setControlEventsFallback(true);
      return;
    }
    let active = true;
    const source = new EventSource(api.agentControlEventsUrl(), { withCredentials: true });
    const invalidateLists = () => {
      void queryClient.invalidateQueries({ queryKey: ["agent-sessions"] });
      void queryClient.invalidateQueries({ queryKey: ["agent-tasks"] });
    };
    const handleOpen = () => {
      if (!active) return;
      void reconcileAgentDockListsOnControlOpen(
        () => sessionsQuery.refetch({ throwOnError: true }),
        () => tasksQuery.refetch({ throwOnError: true }),
        () => {
          if (active) setControlEventsFallback(false);
        },
      ).catch(() => {
        if (active) setControlEventsFallback(true);
      });
    };
    const handleError = () => {
      if (active) {
        setControlEventsFallback(true);
      }
    };
    source.addEventListener("open", handleOpen);
    source.addEventListener("error", handleError);
    const handleLeaseChanged = (event: Event) => {
      const data = "data" in event && typeof event.data === "string" ? event.data : "";
      if (!data || !active) return;
      try {
        const payload = JSON.parse(data) as { phase?: string; execution_id?: string };
        setLeaseHealth({
          phase: payload.phase ?? "",
          executionId: payload.execution_id ?? "",
          at: Date.now(),
        });
      } catch {
        // 控制流心跳损坏时保持上次健康快照
      }
    };
    source.addEventListener("session.changed", invalidateLists);
    source.addEventListener("task.changed", invalidateLists);
    source.addEventListener("lease.changed", handleLeaseChanged);
    return () => {
      active = false;
      source.removeEventListener("open", handleOpen);
      source.removeEventListener("error", handleError);
      source.removeEventListener("session.changed", invalidateLists);
      source.removeEventListener("task.changed", invalidateLists);
      source.removeEventListener("lease.changed", handleLeaseChanged);
      source.close();
    };
  }, [open, queryClient]);

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
      if (rootRef.current?.contains(event.target as Node) || isGlobalAgentDockModalTarget(event.target)) {
        return;
      }
      setOpen(false);
    };
    window.addEventListener("keydown", onKeyDown);
    document.addEventListener("pointerdown", onPointerDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("pointerdown", onPointerDown);
    };
  }, [open]);

  const sessions = useMemo(
    () => sessionsQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [sessionsQuery.data?.pages],
  );
  const tasks = tasksQuery.data?.pages.flatMap((page) => page.items) ?? [];
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
  const routeSessionIdRef = useRef(currentSessionId);
  const activeSessionId = selectedSessionId ?? currentSessionId ?? workspaceSessions[0]?.id ?? sessions[0]?.id ?? null;
  const activeSession = sessions.find((session) => session.id === activeSessionId) ?? null;
  const globalConversation = activeSession?.conversations.find((conversation) => conversation.scope_type === "global") ?? null;
  const globalTasksForSession = useMemo(
    () => tasks.filter((task) => {
      if (task.session_id !== activeSessionId || !task.conversation_id) {
        return false;
      }
      return conversationById.get(task.conversation_id)?.scopeType === "global";
    }),
    [activeSessionId, conversationById, tasks],
  );
  const selectedTask = tasks.find((task) => task.id === selectedTaskId) ?? (
    selectedTaskSnapshot?.id === selectedTaskId ? selectedTaskSnapshot : null
  );
  const basePageContext = useMemo(
    () => buildPageContext(location.pathname, location.search),
    [location.pathname, location.search],
  );
  const pageContext = registeredPageContext?.route === basePageContext.route
    ? registeredPageContext
    : basePageContext;
  const normalizedSearch = search.trim().toLocaleLowerCase();
  const visibleSessions = useMemo(() => {
    if (!normalizedSearch) {
      return sessions;
    }
    return sessions.filter((session) => {
      const workspaceNames = session.conversations.map((item) => item.product_name).join(" ");
      return `${session.title} ${session.summary ?? ""} ${workspaceNames}`.toLocaleLowerCase().includes(normalizedSearch);
    });
  }, [normalizedSearch, sessions]);
  const visibleTasks = useMemo(() => {
    if (!normalizedSearch) {
      return tasks;
    }
    return tasks.filter((task) => {
      const conversation = task.conversation_id ? conversationById.get(task.conversation_id) : null;
      const productName = conversation?.productName ?? "";
      return `${task.title} ${task.goal} ${task.summary ?? ""} ${productName}`.toLocaleLowerCase().includes(normalizedSearch);
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
    if (routeSessionIdRef.current === currentSessionId) {
      return;
    }
    routeSessionIdRef.current = currentSessionId;
    if (currentSessionId) {
      setSelectedSessionId(currentSessionId);
      setSelectedTaskId(null);
      setSelectedTaskSnapshot(null);
    }
  }, [currentSessionId]);

  const invalidateAgentLists = () => {
    void queryClient.invalidateQueries({ queryKey: ["agent-sessions", true] });
    void queryClient.invalidateQueries({ queryKey: ["agent-tasks"] });
  };

  const createSessionMutation = useMutation({
    mutationFn: () => api.createAgentSession(),
    onSuccess: (session) => {
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
        setSelectedTaskSnapshot(task);
        navigate(
          `/products/${encodeURIComponent(task.product_id)}?agent_session_id=${encodeURIComponent(task.session_id)}&agent_task_id=${encodeURIComponent(task.id)}`,
        );
        setOpen(false);
      } else if (workspace) {
        setSelectedTaskSnapshot(task);
        navigate(
          `/products/${encodeURIComponent(workspace.productId)}?agent_session_id=${encodeURIComponent(task.session_id)}&agent_task_id=${encodeURIComponent(task.id)}`,
        );
        setOpen(false);
      } else if (conversation?.scopeType === "global") {
        setSelectedTaskSnapshot(task);
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
  const renameSessionMutation = useMutation({
    mutationFn: ({ sessionId, title }: { sessionId: string; title: string }) =>
      api.renameAgentSession(sessionId, title),
    onSuccess: () => {
      setRenamingSessionId(null);
      invalidateAgentLists();
    },
    onError: () => setRenamingSessionId(null),
  });
  const renameTaskMutation = useMutation({
    mutationFn: ({ taskId, title }: { taskId: string; title: string }) =>
      api.renameAgentTask(taskId, title),
    onSuccess: () => {
      setRenamingTaskId(null);
      invalidateAgentLists();
    },
    onError: () => setRenamingTaskId(null),
  });
  const cancelTaskMutation = useMutation({
    mutationFn: (taskId: string) => api.cancelAgentTask(taskId),
    onSuccess: invalidateAgentLists,
  });
  const pauseTaskMutation = useMutation({
    mutationFn: (taskId: string) => api.pauseAgentTask(taskId),
    onSuccess: invalidateAgentLists,
  });
  const resumeTaskMutation = useMutation({
    mutationFn: (taskId: string) => api.resumeAgentTask(taskId),
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
  const openTask = (task: AgentTask, workspace: AgentWorkspaceTarget | null) => {
    const target = workspace ?? agentTaskWorkspaceTarget(task, workspaceByConversationId);
    if (target) {
      openWorkspace(target, task.session_id, task.id);
      return;
    }
    openGlobalConversation(task.session_id, task.id);
  };
  const openGlobalConversation = (sessionId: string, taskId: string | null = null) => {
    setSelectedSessionId(sessionId);
    setSelectedTaskId(taskId);
    setSelectedTaskSnapshot(taskId ? tasks.find((task) => task.id === taskId) ?? null : null);
    setTab("chat");
  };
  const openProductCreation = () => {
    const params = activeSession?.id
      ? new URLSearchParams({ agent_session_id: activeSession.id })
      : null;
    navigate(params ? `/products/new?${params}` : "/products/new");
    setOpen(false);
  };
  const startTaskForm = () => {
    const initialSession = workspaceSessions[0];
    setTaskSessionId(initialSession?.id ?? "");
    setTaskConversationId(initialSession ? preferredConversation(initialSession)?.conversation_id ?? "" : "");
    setTaskTitle("");
    setTaskGoal("");
    createTaskMutation.reset();
    setTaskFormOpen(true);
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
  const startSession = () => {
    if (createSessionMutation.isPending) {
      return;
    }
    setTaskFormOpen(false);
    setTab("sessions");
    createSessionMutation.reset();
    createSessionMutation.mutate();
  };

  const queryError = sessionsQuery.error ?? tasksQuery.error;
  const mutationError = createTaskMutation.error ?? createSessionMutation.error ?? archiveMutation.error
    ?? renameSessionMutation.error ?? renameTaskMutation.error ?? cancelTaskMutation.error
    ?? pauseTaskMutation.error ?? resumeTaskMutation.error;
  const errorText = queryError || mutationError
    ? errorDetail(queryError ?? mutationError, t("globalAgent.requestFailed"))
    : null;

  const isMobile = typeof window !== "undefined" && window.innerWidth < 640;

  const panelClass = dockMode === "fullscreen"
    ? "pointer-events-auto fixed inset-2 sm:inset-4 z-[70] flex flex-col overflow-hidden rounded-2xl border border-border-l2 bg-surface-raised text-text-primary shadow-[0_25px_80px_rgb(0_0_0_/_0.5)] backdrop-blur"
    : `pointer-events-auto fixed z-[70] flex flex-col overflow-hidden rounded-2xl border border-border-l2 bg-surface-raised text-text-primary shadow-[0_20px_60px_rgb(15_23_42_/_0.25)] dark:shadow-[0_24px_70px_rgb(0_0_0_/_0.55)] ${isDraggingWindow || isResizingWindow ? "select-none transition-none" : "transition-[width,height,transform] duration-150"
    }`;

  const panelStyle = dockMode === "fullscreen"
    ? undefined
    : isMobile
      ? {
        left: "12px",
        right: "12px",
        bottom: "calc(4.5rem + env(safe-area-inset-bottom))",
        maxHeight: "calc(100dvh - 5.5rem)",
        height: "75dvh",
      }
      : {
        width: `${windowSize.width}px`,
        height: `${windowSize.height}px`,
        maxWidth: "calc(100vw - 24px)",
        maxHeight: "calc(100vh - 24px)",
        left: windowPos ? `${windowPos.x}px` : undefined,
        top: windowPos ? `${windowPos.y}px` : undefined,
        right: windowPos ? undefined : "20px",
        bottom: windowPos ? undefined : "20px",
      };

  return (
    <div
      ref={rootRef}
      data-global-agent-dock
      data-agent-lease-health={leaseHealth?.phase || undefined}
      className="pointer-events-none"
    >
      {open ? (
        <section
          id="global-agent-dock-panel"
          aria-label={t("globalAgent.title")}
          style={panelStyle}
          className={panelClass}
        >
          {/* 桌面端 8 个方向自由缩放把手 */}
          {dockMode !== "fullscreen" && !isMobile ? (
            <>
              {/* 上边框 N */}
              <div
                onPointerDown={(e) => handleResizePointerDown("n", e)}
                className="group/n absolute inset-x-4 top-0 z-30 flex h-2 cursor-ns-resize items-center justify-center hover:bg-accent/20 transition-colors"
                title="上下拖拽调整高度"
              >
                <div className="h-1 w-10 rounded-full bg-border-l3/60 group-hover/n:bg-accent" />
              </div>
              {/* 下边框 S */}
              <div
                onPointerDown={(e) => handleResizePointerDown("s", e)}
                className="group/s absolute inset-x-4 bottom-0 z-30 flex h-2 cursor-ns-resize items-center justify-center hover:bg-accent/20 transition-colors"
                title="上下拖拽调整高度"
              >
                <div className="h-1 w-10 rounded-full bg-border-l3/60 group-hover/s:bg-accent" />
              </div>
              {/* 左边框 W */}
              <div
                onPointerDown={(e) => handleResizePointerDown("w", e)}
                className="group/w absolute inset-y-4 left-0 z-30 flex w-2 cursor-ew-resize items-center justify-center hover:bg-accent/20 transition-colors"
                title="左右拖拽调整宽度"
              >
                <div className="h-10 w-1 rounded-full bg-border-l3/60 group-hover/w:bg-accent" />
              </div>
              {/* 右边框 E */}
              <div
                onPointerDown={(e) => handleResizePointerDown("e", e)}
                className="group/e absolute inset-y-4 right-0 z-30 flex w-2 cursor-ew-resize items-center justify-center hover:bg-accent/20 transition-colors"
                title="左右拖拽调整宽度"
              >
                <div className="h-10 w-1 rounded-full bg-border-l3/60 group-hover/e:bg-accent" />
              </div>

              {/* 四个角缩放把手 */}
              <div
                onPointerDown={(e) => handleResizePointerDown("nw", e)}
                className="absolute -left-1 -top-1 z-40 h-4 w-4 cursor-nwse-resize rounded-tl-lg transition-colors hover:bg-accent/30"
                title="斜向缩放"
              />
              <div
                onPointerDown={(e) => handleResizePointerDown("ne", e)}
                className="absolute -right-1 -top-1 z-40 h-4 w-4 cursor-nesw-resize rounded-tr-lg transition-colors hover:bg-accent/30"
                title="斜向缩放"
              />
              <div
                onPointerDown={(e) => handleResizePointerDown("sw", e)}
                className="absolute -bottom-1 -left-1 z-40 h-4 w-4 cursor-nesw-resize rounded-bl-lg transition-colors hover:bg-accent/30"
                title="斜向缩放"
              />
              <div
                onPointerDown={(e) => handleResizePointerDown("se", e)}
                className="absolute -bottom-1 -right-1 z-40 h-4 w-4 cursor-nwse-resize rounded-br-lg transition-colors hover:bg-accent/30"
                title="斜向缩放"
              />
            </>
          ) : null}

          {/* 可拖拽移动的顶部 Header */}
          <header
            onPointerDown={handleHeaderPointerDown}
            className={`flex shrink-0 items-center gap-3 border-b border-border-l1 px-4 py-2.5 bg-surface-raised/80 select-none ${dockMode !== "fullscreen" && !isMobile ? "cursor-grab active:cursor-grabbing" : ""
              }`}
            title={dockMode !== "fullscreen" && !isMobile ? "按住可拖动窗口位置" : undefined}
          >
            <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-accent text-accent-fg shadow-sm">
              <Bot size={17} aria-hidden="true" />
            </span>
            <div className="min-w-0 flex-1">
              <h2 className="truncate text-sm font-semibold">{t("globalAgent.title")}</h2>
              <p className="truncate text-[11px] text-text-secondary">
                {t("globalAgent.activeTasks", { count: activeTaskCount })}
              </p>
            </div>
            <div className="flex items-center gap-0.5">
              <button
                type="button"
                onClick={openProductCreation}
                aria-label={t("globalAgent.newProduct")}
                title={t("globalAgent.newProduct")}
                className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-accent-soft hover:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
              >
                <PackagePlus size={15} aria-hidden="true" />
              </button>
              <button
                type="button"
                onClick={() => changeDockMode(dockMode === "wide" ? "compact" : "wide")}
                aria-label={t(dockMode === "wide" ? "globalAgent.mode.compact" : "globalAgent.mode.wide")}
                title={t(dockMode === "wide" ? "globalAgent.mode.compact" : "globalAgent.mode.wide")}
                className="hidden h-8 w-8 shrink-0 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 sm:flex"
              >
                <Columns2 size={15} aria-hidden="true" />
              </button>
              <button
                type="button"
                onClick={() => changeDockMode(dockMode === "fullscreen" ? (windowSize.width > 600 ? "wide" : "compact") : "fullscreen")}
                aria-label={t(dockMode === "fullscreen" ? "globalAgent.mode.exitFullscreen" : "globalAgent.mode.fullscreen")}
                title={t(dockMode === "fullscreen" ? "globalAgent.mode.exitFullscreen" : "globalAgent.mode.fullscreen")}
                className="hidden h-8 w-8 shrink-0 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 sm:flex"
              >
                {dockMode === "fullscreen" ? <Minimize2 size={15} aria-hidden="true" /> : <Maximize2 size={15} aria-hidden="true" />}
              </button>
              <button
                type="button"
                onClick={() => setOpen(false)}
                aria-label={t("globalAgent.minimize")}
                title={t("globalAgent.minimize")}
                className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
              >
                <Minus size={15} aria-hidden="true" />
              </button>
              <button
                type="button"
                onClick={() => setOpen(false)}
                aria-label={t("globalAgent.close")}
                title={t("globalAgent.close")}
                className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
              >
                <X size={15} aria-hidden="true" />
              </button>
            </div>
          </header>

          <div className="flex shrink-0 items-center gap-1 border-b border-border-l1 bg-surface-subtle/60 px-3 py-2" role="tablist" aria-label={t("globalAgent.views")}>
            <DockTab
              active={tab === "chat"}
              count={sessions.length}
              icon={<Bot size={14} aria-hidden="true" />}
              label={t("globalAgent.chat")}
              onClick={() => {
                setTaskFormOpen(false);
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
              onClick={tab === "tasks" ? startTaskForm : startSession}
              disabled={tab !== "tasks" && createSessionMutation.isPending}
              aria-label={tab === "tasks" ? t("globalAgent.newTask") : t("globalAgent.newSession")}
              title={tab === "tasks" ? t("globalAgent.newTask") : t("globalAgent.newSession")}
              className="ml-auto flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface-raised hover:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
            >
              {tab !== "tasks" && createSessionMutation.isPending ? (
                <Loader2 size={16} className="animate-spin motion-reduce:animate-none" aria-hidden="true" />
              ) : (
                <Plus size={16} aria-hidden="true" />
              )}
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
                  {globalTasksForSession.length ? (
                    <div className="shrink-0 border-b border-border-l1 bg-surface-subtle/30 px-3 py-2">
                      <label className="sr-only" htmlFor="global-agent-task-select">{t("globalAgent.taskScope")}</label>
                      <select
                        id="global-agent-task-select"
                        value={selectedTaskId ?? ""}
                        onChange={(event) => {
                          const taskId = event.target.value || null;
                          setSelectedTaskId(taskId);
                          setSelectedTaskSnapshot(taskId ? tasks.find((task) => task.id === taskId) ?? null : null);
                        }}
                        className="h-8 w-full rounded-md border border-border-l2 bg-surface-raised px-2.5 text-[11px] text-text-primary outline-none focus:border-accent focus:ring-2 focus:ring-accent/15"
                      >
                        <option value="">{t("globalAgent.allSessionMessages")}</option>
                        {globalTasksForSession.map((task) => (
                          <option key={task.id} value={task.id}>{task.title} · {t(TASK_STATUS_LABEL_KEYS[task.status])}</option>
                        ))}
                      </select>
                    </div>
                  ) : null}
                  <GlobalAgentConversationPanel
                    conversationId={globalConversation?.conversation_id ?? null}
                    sessionTitle={activeSession?.title ?? t("globalAgent.title")}
                    taskTitle={selectedTask?.title ?? null}
                    taskId={selectedTaskId}
                    taskGoal={selectedTask?.goal ?? null}
                    taskStatus={selectedTask?.status ?? null}
                    pageContext={pageContext}
                  />
                </div>
              </>
            ) : (
              <>
                <div className="flex shrink-0 items-center gap-2 border-b border-border-l1 p-3">
                  <label className="flex h-9 min-w-0 flex-1 items-center gap-2 rounded-md border border-border-l2 bg-surface-subtle px-2.5 focus-within:border-accent focus-within:ring-2 focus-within:ring-accent/15">
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
                  {tab === "tasks" ? (
                    <div className="flex shrink-0 items-center rounded-lg border border-border-l2 bg-surface-subtle p-0.5" role="group" aria-label={t("globalAgent.tasks")}>
                      <button
                        type="button"
                        onClick={() => setTaskViewMode("list")}
                        className={`flex h-8 items-center gap-1.5 rounded-md px-2.5 text-xs font-medium transition-colors ${taskViewMode === "list"
                            ? "bg-surface-raised text-text-primary shadow-sm"
                            : "text-text-muted hover:text-text-secondary"
                          }`}
                        title={t("globalAgent.taskView.list")}
                        aria-pressed={taskViewMode === "list"}
                      >
                        <List size={13} aria-hidden="true" />
                        <span>{t("globalAgent.taskView.list")}</span>
                      </button>
                      <button
                        type="button"
                        onClick={() => setTaskViewMode("board")}
                        className={`flex h-8 items-center gap-1.5 rounded-md px-2.5 text-xs font-medium transition-colors ${taskViewMode === "board"
                            ? "bg-surface-raised text-text-primary shadow-sm"
                            : "text-text-muted hover:text-text-secondary"
                          }`}
                        title={t("globalAgent.taskView.board")}
                        aria-pressed={taskViewMode === "board"}
                      >
                        <LayoutGrid size={13} aria-hidden="true" />
                        <span>{t("globalAgent.taskView.board")}</span>
                      </button>
                    </div>
                  ) : null}
                </div>
                {errorText ? (
                  <p role="alert" className="shrink-0 border-b border-state-error/20 bg-state-error/10 px-3 py-2 text-xs leading-5 text-state-error">
                    {errorText}
                  </p>
                ) : null}
                <div className="min-h-0 flex-1 overflow-y-auto p-2">
                  {tab === "tasks" ? (
                    <>
                      {taskViewMode === "board" ? (
                        <TaskBoard
                          loading={tasksQuery.isLoading}
                          tasks={visibleTasks}
                          workspaceByConversationId={workspaceByConversationId}
                          conversationById={conversationById}
                          onOpen={openTask}
                          onCancel={(task) => cancelTaskMutation.mutate(task.id)}
                          onPause={(task) => pauseTaskMutation.mutate(task.id)}
                          onResume={(task) => resumeTaskMutation.mutate(task.id)}
                          onRename={(taskId, title) => {
                            setRenamingTaskId(taskId);
                            renameTaskMutation.mutate({ taskId, title });
                          }}
                          renamingTaskId={renameTaskMutation.isPending ? renameTaskMutation.variables?.taskId ?? renamingTaskId : renamingTaskId}
                          renameError={renameTaskMutation.error ? errorDetail(renameTaskMutation.error, t("globalAgent.requestFailed")) : null}
                          cancelingTaskId={cancelTaskMutation.isPending ? cancelTaskMutation.variables : null}
                          pausingTaskId={pauseTaskMutation.isPending ? pauseTaskMutation.variables : null}
                          resumingTaskId={resumeTaskMutation.isPending ? resumeTaskMutation.variables : null}
                          emptyLabel={normalizedSearch ? t("globalAgent.noMatch") : t("globalAgent.noTasks")}
                          statusLabel={(status) => t(TASK_STATUS_LABEL_KEYS[status])}
                        />
                      ) : (
                        <TaskList
                          loading={tasksQuery.isLoading}
                          tasks={visibleTasks}
                          workspaceByConversationId={workspaceByConversationId}
                          conversationById={conversationById}
                          onOpen={openTask}
                          onCancel={(task) => cancelTaskMutation.mutate(task.id)}
                          onPause={(task) => pauseTaskMutation.mutate(task.id)}
                          onResume={(task) => resumeTaskMutation.mutate(task.id)}
                          onRename={(taskId, title) => {
                            setRenamingTaskId(taskId);
                            renameTaskMutation.mutate({ taskId, title });
                          }}
                          renamingTaskId={renameTaskMutation.isPending ? renameTaskMutation.variables?.taskId ?? renamingTaskId : renamingTaskId}
                          renameError={renameTaskMutation.error ? errorDetail(renameTaskMutation.error, t("globalAgent.requestFailed")) : null}
                          cancelingTaskId={cancelTaskMutation.isPending ? cancelTaskMutation.variables : null}
                          pausingTaskId={pauseTaskMutation.isPending ? pauseTaskMutation.variables : null}
                          resumingTaskId={resumeTaskMutation.isPending ? resumeTaskMutation.variables : null}
                          emptyLabel={normalizedSearch ? t("globalAgent.noMatch") : t("globalAgent.noTasks")}
                          statusLabel={(status) => t(TASK_STATUS_LABEL_KEYS[status])}
                        />
                      )}
                      {tasksQuery.hasNextPage ? (
                        <button
                          type="button"
                          onClick={() => void tasksQuery.fetchNextPage()}
                          disabled={tasksQuery.isFetchingNextPage}
                          className="mt-2 flex h-9 w-full items-center justify-center gap-2 rounded-md border border-border-l2 bg-surface-raised text-xs font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 disabled:cursor-wait disabled:opacity-60"
                        >
                          {tasksQuery.isFetchingNextPage ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : null}
                          {tasksQuery.isFetchingNextPage ? t("globalAgent.loadingMoreTasks") : t("globalAgent.loadMoreTasks")}
                        </button>
                      ) : null}
                    </>
                  ) : (
                    <>
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
                      onOpenWorkspace={(session, conversation) => {
                        if (!conversation.product_id) return;
                        openWorkspace(
                          { productId: conversation.product_id, conversationId: conversation.conversation_id },
                          session.id,
                        );
                      }}
                      onRename={(sessionId, title) => {
                        setRenamingSessionId(sessionId);
                        renameSessionMutation.mutate({ sessionId, title });
                      }}
                      renamingSessionId={renameSessionMutation.isPending ? renameSessionMutation.variables?.sessionId ?? renamingSessionId : renamingSessionId}
                      renameError={renameSessionMutation.error ? errorDetail(renameSessionMutation.error, t("globalAgent.requestFailed")) : null}
                      onArchive={setArchiveTarget}
                      emptyLabel={normalizedSearch ? t("globalAgent.noMatch") : t("globalAgent.noSessions")}
                    />
                    {sessionsQuery.hasNextPage ? (
                      <button
                        type="button"
                        onClick={() => void sessionsQuery.fetchNextPage()}
                        disabled={sessionsQuery.isFetchingNextPage}
                        className="mt-2 flex h-9 w-full items-center justify-center gap-2 rounded-md border border-border-l2 bg-surface-raised text-xs font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 disabled:cursor-wait disabled:opacity-60"
                      >
                        {sessionsQuery.isFetchingNextPage ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : null}
                        {t("agentWorkbench.session.loadMore")}
                      </button>
                    ) : null}
                    </>
                  )}
                </div>
              </>
            )}
          </div>
        </section>
      ) : null}

      {shouldRenderGlobalAgentLauncher(location.pathname, open) ? (
        <button
          type="button"
          data-global-agent-launcher
          aria-expanded={open}
          aria-controls="global-agent-dock-panel"
          aria-label={open ? t("globalAgent.close") : t("globalAgent.open")}
          title={open ? t("globalAgent.close") : t("globalAgent.open")}
          onPointerDown={handleBubblePointerDown}
          style={bubblePos ? {
            left: `${bubblePos.x}px`,
            top: `${bubblePos.y}px`,
            right: "auto",
            bottom: "auto",
          } : undefined}
          className={`pointer-events-auto fixed z-[60] flex h-12 w-12 items-center justify-center rounded-full border border-accent/30 bg-accent text-accent-fg shadow-[0_10px_28px_rgb(15_23_42_/_0.25)] dark:shadow-[0_12px_32px_rgb(0_0_0_/_0.45)] transition-transform hover:scale-105 active:scale-95 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/60 ${bubblePos ? "" : "bottom-[calc(4.25rem+env(safe-area-inset-bottom))] right-3 sm:bottom-5 sm:right-5"
            } ${isDraggingBubble ? "cursor-grabbing select-none transition-none" : "cursor-grab"}`}
        >
          <Bot size={20} aria-hidden="true" />
          {activeTaskCount > 0 ? (
            <span className="absolute -right-1 -top-1 flex h-5 min-w-5 items-center justify-center rounded-full border-2 border-surface-raised bg-state-warning px-1 text-[10px] font-bold text-text-primary">
              {activeTaskCount > 99 ? "99+" : activeTaskCount}
            </span>
          ) : null}
        </button>
      ) : null}

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
      className={`inline-flex h-8 items-center gap-1.5 rounded-md px-2.5 text-xs font-semibold transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 ${active ? "bg-surface-raised text-accent shadow-sm" : "text-text-secondary hover:bg-surface-raised/70 hover:text-text-primary"
        }`}
    >
      {icon}
      {label}
      <span className="text-[10px] font-medium text-text-muted">{count}</span>
    </button>
  );
}

export function TaskList({
  loading,
  tasks,
  workspaceByConversationId,
  conversationById,
  onOpen,
  onCancel,
  onPause,
  onResume,
  onRename,
  renamingTaskId,
  renameError,
  cancelingTaskId,
  pausingTaskId,
  resumingTaskId,
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
  onPause: (task: AgentTask) => void;
  onResume: (task: AgentTask) => void;
  onRename: (taskId: string, title: string) => void;
  renamingTaskId: string | null;
  renameError: string | null;
  cancelingTaskId: string | null;
  pausingTaskId: string | null;
  resumingTaskId: string | null;
  emptyLabel: string;
  statusLabel: (status: AgentTaskStatus) => string;
}) {
  const { t } = useI18n();
  const [editingTaskId, setEditingTaskId] = useState<string | null>(null);
  const [editingTitle, setEditingTitle] = useState("");

  useEffect(() => {
    if (editingTaskId && renamingTaskId === null) {
      setEditingTaskId(null);
      setEditingTitle("");
    }
  }, [editingTaskId, renamingTaskId]);

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
        const target = agentTaskWorkspaceTarget(task, workspaceByConversationId);
        const openable = Boolean(target || conversation?.scopeType === "global" || task.session_id);
        const cancelable = CANCELABLE_TASK_STATUSES.has(task.status);
        const pausable = PAUSABLE_TASK_STATUSES.has(task.status);
        const resumable = task.status === "paused";
        if (editingTaskId === task.id) {
          return (
            <form
              key={task.id}
              className="rounded-md border border-accent/40 bg-accent-soft/40 p-2.5"
              onSubmit={(event) => {
                event.preventDefault();
                const title = editingTitle.trim();
                if (title && renamingTaskId === null) {
                  onRename(task.id, title);
                }
              }}
            >
              <label className="sr-only" htmlFor={`global-agent-task-title-${task.id}`}>
                {t("globalAgent.taskTitle")}
              </label>
              <input
                id={`global-agent-task-title-${task.id}`}
                autoFocus
                value={editingTitle}
                maxLength={160}
                onChange={(event) => setEditingTitle(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Escape") {
                    setEditingTaskId(null);
                    setEditingTitle("");
                  }
                }}
                className="h-9 w-full rounded-md border border-border-l2 bg-surface-raised px-2.5 text-xs text-text-primary outline-none focus:border-accent focus:ring-2 focus:ring-accent/15"
              />
              {renameError ? <p role="alert" className="mt-1.5 text-[11px] leading-4 text-state-error">{renameError}</p> : null}
              <div className="mt-2 flex justify-end gap-1.5">
                <button
                  type="button"
                  onClick={() => {
                    setEditingTaskId(null);
                    setEditingTitle("");
                  }}
                  disabled={renamingTaskId === task.id}
                  aria-label={t("globalAgent.cancelRename")}
                  title={t("globalAgent.cancelRename")}
                  className="flex h-8 w-8 items-center justify-center rounded-md text-text-secondary hover:bg-surface-raised focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 disabled:opacity-50"
                >
                  <X size={14} aria-hidden="true" />
                </button>
                <button
                  type="submit"
                  disabled={!editingTitle.trim() || renamingTaskId !== null}
                  aria-label={t("globalAgent.saveTask")}
                  title={t("globalAgent.saveTask")}
                  className="flex h-8 w-8 items-center justify-center rounded-md bg-accent text-accent-fg hover:bg-accent-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {renamingTaskId === task.id ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : <Check size={14} aria-hidden="true" />}
                </button>
              </div>
            </form>
          );
        }
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
                {task.summary && task.summary !== task.goal ? (
                  <span className="mt-0.5 block truncate text-[11px] text-text-muted" title={task.summary}>
                    {task.summary}
                  </span>
                ) : null}
                {task.status === "awaiting_confirmation" ? (
                  <span className="mt-1 block text-[11px] leading-4 text-state-warning">
                    {target ? t("globalAgent.taskConfirm.workbench") : t("globalAgent.taskConfirm.chat")}
                  </span>
                ) : null}
                <span className="mt-1 block truncate text-[11px] text-text-muted">
                  {conversation?.productName ?? workspace?.productName ?? (task.product_id ? t("globalAgent.taskProductWorkspace") : t("globalAgent.noWorkspace"))}
                </span>
              </span>
              {openable ? <ChevronRight size={14} className="mt-1 shrink-0 text-text-muted opacity-0 transition-opacity group-hover:opacity-100" aria-hidden="true" /> : null}
            </button>
            <button
              type="button"
              onClick={() => {
                setEditingTaskId(task.id);
                setEditingTitle(task.title);
              }}
              aria-label={t("globalAgent.renameTask")}
              title={t("globalAgent.renameTask")}
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-text-muted opacity-70 transition-colors hover:bg-accent-soft hover:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 sm:opacity-0 sm:group-hover:opacity-100"
            >
              <Pencil size={14} aria-hidden="true" />
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
            {pausable ? (
              <button
                type="button"
                onClick={() => onPause(task)}
                disabled={pausingTaskId !== null || resumingTaskId !== null}
                aria-label={pausingTaskId === task.id ? t("globalAgent.pausingTask") : t("globalAgent.pauseTask")}
                title={pausingTaskId === task.id ? t("globalAgent.pausingTask") : t("globalAgent.pauseTask")}
                className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-text-muted opacity-70 transition-colors hover:bg-state-warning/10 hover:text-state-warning focus:outline-none focus-visible:ring-2 focus-visible:ring-state-warning/50 sm:opacity-0 sm:group-hover:opacity-100 disabled:cursor-wait disabled:opacity-100"
              >
                {pausingTaskId === task.id ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : <Pause size={14} aria-hidden="true" />}
              </button>
            ) : null}
            {resumable ? (
              <button
                type="button"
                onClick={() => onResume(task)}
                disabled={pausingTaskId !== null || resumingTaskId !== null}
                aria-label={resumingTaskId === task.id ? t("globalAgent.resumingTask") : t("globalAgent.resumeTask")}
                title={resumingTaskId === task.id ? t("globalAgent.resumingTask") : t("globalAgent.resumeTask")}
                className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-text-muted opacity-70 transition-colors hover:bg-state-success/10 hover:text-state-success focus:outline-none focus-visible:ring-2 focus-visible:ring-state-success/50 sm:opacity-0 sm:group-hover:opacity-100 disabled:cursor-wait disabled:opacity-100"
              >
                {resumingTaskId === task.id ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : <Play size={14} aria-hidden="true" />}
              </button>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

export function TaskBoard({
  loading,
  tasks,
  workspaceByConversationId,
  conversationById,
  onOpen,
  onCancel,
  onPause,
  onResume,
  onRename,
  renamingTaskId,
  renameError,
  cancelingTaskId,
  pausingTaskId,
  resumingTaskId,
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
  onPause: (task: AgentTask) => void;
  onResume: (task: AgentTask) => void;
  onRename: (taskId: string, title: string) => void;
  renamingTaskId: string | null;
  renameError: string | null;
  cancelingTaskId: string | null;
  pausingTaskId: string | null;
  resumingTaskId: string | null;
  emptyLabel: string;
  statusLabel: (status: AgentTaskStatus) => string;
}) {
  const { t } = useI18n();
  const [editingTaskId, setEditingTaskId] = useState<string | null>(null);
  const [editingTitle, setEditingTitle] = useState("");

  useEffect(() => {
    if (editingTaskId && renamingTaskId === null) {
      setEditingTaskId(null);
      setEditingTitle("");
    }
  }, [editingTaskId, renamingTaskId]);

  if (loading) {
    return <LoadingDockState label={t("app.loading")} />;
  }
  if (!tasks.length) {
    return <EmptyDockState icon={<ClipboardList size={18} />} label={emptyLabel} />;
  }

  const columns = [
    {
      id: "queued",
      title: t("globalAgent.taskBoard.colQueued"),
      tasks: tasks.filter((task) => task.status === "queued" || task.status === "paused"),
      headerTone: "border-text-muted/20 bg-surface-subtle/50",
      badgeTone: "bg-surface-raised text-text-secondary border border-border-l2",
    },
    {
      id: "running",
      title: t("globalAgent.taskBoard.colRunning"),
      tasks: tasks.filter((task) => task.status === "running"),
      headerTone: "border-accent/30 bg-accent-soft",
      badgeTone: "bg-accent text-accent-fg font-semibold",
    },
    {
      id: "awaiting",
      title: t("globalAgent.taskBoard.colAwaiting"),
      tasks: tasks.filter((task) => task.status === "waiting_user" || task.status === "awaiting_confirmation"),
      headerTone: "border-state-warning/30 bg-state-warning/10",
      badgeTone: "bg-state-warning-soft text-state-warning font-bold",
    },
    {
      id: "succeeded",
      title: t("globalAgent.taskBoard.colSucceeded"),
      tasks: tasks.filter((task) => task.status === "succeeded"),
      headerTone: "border-state-success/30 bg-state-success/10",
      badgeTone: "bg-state-success text-white font-semibold",
    },
    {
      id: "failed",
      title: t("globalAgent.taskBoard.colFailed"),
      tasks: tasks.filter((task) => task.status === "failed" || task.status === "canceled"),
      headerTone: "border-state-error/30 bg-state-error/10",
      badgeTone: "bg-state-error text-white font-semibold",
    },
  ];

  return (
    <div className="flex gap-3 overflow-x-auto pb-3 pt-1" tabIndex={0} aria-label={t("globalAgent.taskView.board")}>
      {columns.map((column) => (
        <div
          key={column.id}
          className={`flex w-64 min-w-[240px] shrink-0 flex-col rounded-xl border p-2.5 transition-colors ${column.headerTone}`}
        >
          <div className="mb-2 flex items-center justify-between px-1">
            <span className="text-xs font-semibold tracking-tight text-text-primary">{column.title}</span>
            <span className={`inline-flex h-5 min-w-5 items-center justify-center rounded-full px-1.5 text-[11px] ${column.badgeTone}`}>
              {column.tasks.length}
            </span>
          </div>

          <div className="flex-1 space-y-2 overflow-y-auto pr-0.5">
            {column.tasks.length === 0 ? (
              <div className="flex h-20 items-center justify-center rounded-lg border border-dashed border-border-l2 bg-surface/30 text-center text-[11px] text-text-muted">
                {t("globalAgent.noMatch")}
              </div>
            ) : (
              column.tasks.map((task) => {
                const workspace = task.conversation_id ? workspaceByConversationId.get(task.conversation_id) : null;
                const conversation = task.conversation_id ? conversationById.get(task.conversation_id) : null;
                const target = agentTaskWorkspaceTarget(task, workspaceByConversationId);
                const openable = Boolean(target || conversation?.scopeType === "global" || task.session_id);
                const cancelable = CANCELABLE_TASK_STATUSES.has(task.status);
                const pausable = PAUSABLE_TASK_STATUSES.has(task.status);
                const resumable = task.status === "paused";

                if (editingTaskId === task.id) {
                  return (
                    <form
                      key={task.id}
                      className="rounded-lg border border-accent/40 bg-surface-raised p-2 shadow-sm"
                      onSubmit={(event) => {
                        event.preventDefault();
                        const title = editingTitle.trim();
                        if (title && renamingTaskId === null) {
                          onRename(task.id, title);
                        }
                      }}
                    >
                      <input
                        autoFocus
                        value={editingTitle}
                        maxLength={160}
                        onChange={(event) => setEditingTitle(event.target.value)}
                        onKeyDown={(event) => {
                          if (event.key === "Escape") {
                            setEditingTaskId(null);
                            setEditingTitle("");
                          }
                        }}
                        className="h-8 w-full rounded-md border border-border-l2 bg-surface px-2 text-xs text-text-primary outline-none focus:border-accent focus:ring-1 focus:ring-accent"
                      />
                      {renameError ? <p role="alert" className="mt-1 text-[11px] text-state-error">{renameError}</p> : null}
                      <div className="mt-1.5 flex justify-end gap-1">
                        <button
                          type="button"
                          onClick={() => {
                            setEditingTaskId(null);
                            setEditingTitle("");
                          }}
                          className="flex h-7 w-7 items-center justify-center rounded text-text-secondary hover:bg-surface"
                        >
                          <X size={13} />
                        </button>
                        <button
                          type="submit"
                          disabled={!editingTitle.trim()}
                          className="flex h-7 w-7 items-center justify-center rounded bg-accent text-accent-fg hover:bg-accent-strong"
                        >
                          {renamingTaskId === task.id ? <Loader2 size={13} className="animate-spin" /> : <Check size={13} />}
                        </button>
                      </div>
                    </form>
                  );
                }

                return (
                  <div
                    key={task.id}
                    className="group flex flex-col justify-between rounded-lg border border-border-l2 bg-surface-raised p-2.5 shadow-sm transition-all hover:border-accent/40 hover:shadow"
                  >
                    <div>
                      <div className="flex items-start justify-between gap-1.5">
                        <div className="flex min-w-0 items-center gap-1.5">
                          <span className={`h-2 w-2 shrink-0 rounded-full ${TASK_STATUS_CLASSES[task.status]}`} aria-hidden="true" />
                          <h4 className="truncate text-xs font-semibold text-text-primary" title={task.title}>
                            {task.title}
                          </h4>
                        </div>
                        {openable ? (
                          <button
                            type="button"
                            onClick={() => onOpen(task, target)}
                            aria-label={task.title}
                            title={task.title}
                            className="flex h-6 w-6 shrink-0 items-center justify-center rounded text-text-muted hover:bg-surface hover:text-accent focus:outline-none focus-visible:ring-1 focus-visible:ring-accent"
                          >
                            <ChevronRight size={13} />
                          </button>
                        ) : null}
                      </div>

                      <p className="mt-1 line-clamp-2 text-[11px] leading-4 text-text-secondary" title={task.goal}>
                        {task.goal}
                      </p>
                      {task.status === "awaiting_confirmation" ? (
                        <p className="mt-1 text-[11px] leading-4 text-state-warning">
                          {target ? t("globalAgent.taskConfirm.workbench") : t("globalAgent.taskConfirm.chat")}
                        </p>
                      ) : null}

                      <div className="mt-2 flex items-center gap-1 text-[10px] text-text-muted">
                        <span className="truncate rounded bg-surface px-1.5 py-0.5 font-medium">
                          {conversation?.productName ?? workspace?.productName ?? (task.product_id ? t("globalAgent.taskProductWorkspace") : t("globalAgent.noWorkspace"))}
                        </span>
                      </div>
                    </div>

                    <div className="mt-2.5 flex items-center justify-between border-t border-border-l1 pt-1.5 text-[11px]">
                      <span className="text-text-muted font-medium">{statusLabel(task.status)}</span>
                      <div className="flex items-center gap-0.5">
                        <button
                          type="button"
                          onClick={() => {
                            setEditingTaskId(task.id);
                            setEditingTitle(task.title);
                          }}
                          aria-label={t("globalAgent.renameTask")}
                          title={t("globalAgent.renameTask")}
                          className="flex h-6 w-6 items-center justify-center rounded text-text-muted hover:bg-surface hover:text-accent"
                        >
                          <Pencil size={12} />
                        </button>
                        {pausable ? (
                          <button
                            type="button"
                            onClick={() => onPause(task)}
                            disabled={pausingTaskId !== null}
                            aria-label={t("globalAgent.pauseTask")}
                            title={t("globalAgent.pauseTask")}
                            className="flex h-6 w-6 items-center justify-center rounded text-text-muted hover:bg-surface hover:text-state-warning"
                          >
                            {pausingTaskId === task.id ? <Loader2 size={12} className="animate-spin" /> : <Pause size={12} />}
                          </button>
                        ) : null}
                        {resumable ? (
                          <button
                            type="button"
                            onClick={() => onResume(task)}
                            disabled={resumingTaskId !== null}
                            aria-label={t("globalAgent.resumeTask")}
                            title={t("globalAgent.resumeTask")}
                            className="flex h-6 w-6 items-center justify-center rounded text-text-muted hover:bg-surface hover:text-state-success"
                          >
                            {resumingTaskId === task.id ? <Loader2 size={12} className="animate-spin" /> : <Play size={12} />}
                          </button>
                        ) : null}
                        {cancelable ? (
                          <button
                            type="button"
                            onClick={() => onCancel(task)}
                            disabled={cancelingTaskId !== null}
                            aria-label={t("globalAgent.cancelTask")}
                            title={t("globalAgent.cancelTask")}
                            className="flex h-6 w-6 items-center justify-center rounded text-text-muted hover:bg-surface hover:text-state-error"
                          >
                            {cancelingTaskId === task.id ? <Loader2 size={12} className="animate-spin" /> : <X size={12} />}
                          </button>
                        ) : null}
                      </div>
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

function SessionList({
  loading,
  sessions,
  currentSessionId,
  onOpen,
  onOpenWorkspace,
  onRename,
  renamingSessionId,
  renameError,
  onArchive,
  emptyLabel,
}: {
  loading: boolean;
  sessions: AgentSession[];
  currentSessionId: string | null;
  onOpen: (session: AgentSession) => void;
  onOpenWorkspace: (session: AgentSession, conversation: AgentSession["conversations"][number]) => void;
  onRename: (sessionId: string, title: string) => void;
  renamingSessionId: string | null;
  renameError: string | null;
  onArchive: (session: AgentSession) => void;
  emptyLabel: string;
}) {
  const { t } = useI18n();
  const [editingSessionId, setEditingSessionId] = useState<string | null>(null);
  const [editingTitle, setEditingTitle] = useState("");

  useEffect(() => {
    if (editingSessionId && renamingSessionId === null) {
      setEditingSessionId(null);
      setEditingTitle("");
    }
  }, [editingSessionId, renamingSessionId]);

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
        const productWorkspaces = session.conversations.filter(
          (item) => item.scope_type === "product_workflow" && item.product_id,
        );
        const selected = currentSessionId === session.id;
        if (editingSessionId === session.id) {
          return (
            <form
              key={session.id}
              className="rounded-md border border-accent/40 bg-accent-soft/40 p-2.5"
              onSubmit={(event) => {
                event.preventDefault();
                const title = editingTitle.trim();
                if (title && renamingSessionId === null) {
                  onRename(session.id, title);
                }
              }}
            >
              <label className="sr-only" htmlFor={`global-agent-session-title-${session.id}`}>
                {t("agentWorkbench.session.titleLabel")}
              </label>
              <input
                id={`global-agent-session-title-${session.id}`}
                autoFocus
                value={editingTitle}
                maxLength={160}
                onChange={(event) => setEditingTitle(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Escape") {
                    setEditingSessionId(null);
                    setEditingTitle("");
                  }
                }}
                className="h-9 w-full rounded-md border border-border-l2 bg-surface-raised px-2.5 text-xs text-text-primary outline-none focus:border-accent focus:ring-2 focus:ring-accent/15"
              />
              {renameError ? <p role="alert" className="mt-1.5 text-[11px] leading-4 text-state-error">{renameError}</p> : null}
              <div className="mt-2 flex justify-end gap-1.5">
                <button
                  type="button"
                  onClick={() => {
                    setEditingSessionId(null);
                    setEditingTitle("");
                  }}
                  disabled={renamingSessionId === session.id}
                  aria-label={t("agentWorkbench.session.cancel")}
                  title={t("agentWorkbench.session.cancel")}
                  className="flex h-8 w-8 items-center justify-center rounded-md text-text-secondary hover:bg-surface-raised focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 disabled:opacity-50"
                >
                  <X size={14} aria-hidden="true" />
                </button>
                <button
                  type="submit"
                  disabled={!editingTitle.trim() || renamingSessionId !== null}
                  aria-label={t("agentWorkbench.session.save")}
                  title={t("agentWorkbench.session.save")}
                  className="flex h-8 w-8 items-center justify-center rounded-md bg-accent text-accent-fg hover:bg-accent-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {renamingSessionId === session.id ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : <Check size={14} aria-hidden="true" />}
                </button>
              </div>
            </form>
          );
        }
        return (
          <div key={session.id} className={`rounded-md px-2.5 py-2.5 transition-colors ${selected ? "bg-accent-soft" : "hover:bg-surface-subtle"}`}>
            <div className="group flex min-w-0 items-start gap-2">
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
                  {session.summary ? (
                    <span className="mt-0.5 block truncate text-[11px] text-text-muted" title={session.summary}>
                      {session.summary}
                    </span>
                  ) : null}
                </span>
              </button>
              {session.status === "active" ? (
                <>
                  <button
                    type="button"
                    onClick={() => {
                      setEditingSessionId(session.id);
                      setEditingTitle(session.title);
                    }}
                    aria-label={t("agentWorkbench.session.rename")}
                    title={t("agentWorkbench.session.rename")}
                    className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-text-muted opacity-70 transition-colors hover:bg-accent-soft hover:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 sm:opacity-0 sm:group-hover:opacity-100"
                  >
                    <Pencil size={14} aria-hidden="true" />
                  </button>
                  <button
                    type="button"
                    onClick={() => onArchive(session)}
                    aria-label={t("globalAgent.archiveSession")}
                    title={t("globalAgent.archiveSession")}
                    className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-text-muted opacity-70 transition-colors hover:bg-state-error/10 hover:text-state-error focus:outline-none focus-visible:ring-2 focus-visible:ring-state-error/50 sm:opacity-0 sm:group-hover:opacity-100"
                  >
                    <Archive size={14} aria-hidden="true" />
                  </button>
                </>
              ) : null}
            </div>
            {productWorkspaces.length ? (
              <div className="mt-1 space-y-0.5 border-l border-border-l2 pl-4">
                {productWorkspaces.map((workspace) => (
                  <button
                    key={workspace.conversation_id}
                    type="button"
                    onClick={() => onOpenWorkspace(session, workspace)}
                    className="group/workspace flex min-w-0 w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs text-text-secondary transition-colors hover:bg-surface-raised hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
                  >
                    <PackagePlus size={13} className="shrink-0 text-accent" aria-hidden="true" />
                    <span className="min-w-0 flex-1 truncate" title={workspace.product_name}>{workspace.product_name}</span>
                    <ChevronRight size={13} className="shrink-0 text-text-muted opacity-0 transition-opacity group-hover/workspace:opacity-100" aria-hidden="true" />
                  </button>
                ))}
              </div>
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
  if (pathname.startsWith("/media-library")) {
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
