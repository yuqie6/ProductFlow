import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import type { AgentTask } from "../lib/types";
import {
  agentDockListRefetchInterval,
  agentTaskWorkspaceTarget,
  isGlobalAgentDockModalTarget,
  isProductWorkbenchPath,
  openGlobalAgent,
  reconcileAgentDockListsOnControlOpen,
  shouldRenderGlobalAgentLauncher,
  TaskBoard,
  TaskList,
} from "./GlobalAgentDock";

function sampleTask(id: string, title: string, status: AgentTask["status"]): AgentTask {
  return {
    id,
    session_id: "session-1",
    conversation_id: "conv-1",
    product_id: "prod-1",
    workflow_id: null,
    title,
    goal: "生成春季商品主图并调整参数",
    summary: "已完成 2 个步骤",
    status,
    waiting_reason: null,
    failure_reason: null,
    current_turn_id: null,
    created_at: "2026-08-18T00:00:00Z",
    updated_at: "2026-08-18T00:00:00Z",
    started_at: null,
    finished_at: null,
    canceled_at: null,
  };
}

describe("GlobalAgentDock Task Views", () => {
  it("opens a product Task from its product_id when the canvas session is not in the Dock list", () => {
    const task = sampleTask("task-canvas", "确认跑图", "awaiting_confirmation");
    expect(agentTaskWorkspaceTarget(task, new Map())).toEqual({
      productId: "prod-1",
      conversationId: "conv-1",
    });

    const onOpen = vi.fn();
    const markup = renderToStaticMarkup(
      createElement(TaskList, {
        loading: false,
        tasks: [task],
        workspaceByConversationId: new Map(),
        conversationById: new Map(),
        onOpen,
        onCancel: vi.fn(),
        onPause: vi.fn(),
        onResume: vi.fn(),
        onRename: vi.fn(),
        renamingTaskId: null,
        renameError: null,
        cancelingTaskId: null,
        pausingTaskId: null,
        resumingTaskId: null,
        emptyLabel: "暂无任务",
        statusLabel: () => "等你确认",
      }),
    );

    expect(markup).not.toContain("disabled=\"\"");
    expect(markup).toContain("确认跑图");
    expect(markup).toContain("等你确认");
  });

  const dummyWorkspaceMap = new Map([
    ["conv-1", { productId: "prod-1", conversationId: "conv-1", productName: "春季卫衣", sessionId: "session-1" }],
  ]);
  const dummyConversationMap = new Map([
    ["conv-1", { conversationId: "conv-1", sessionId: "session-1", scopeType: "product_workflow" as const, productId: "prod-1", productName: "春季卫衣" }],
  ]);

  it("renders tasks in list mode with correct status and product association", () => {
    const tasks: AgentTask[] = [
      sampleTask("task-1", "优化卫衣主图", "running"),
      sampleTask("task-2", "生成详情海报", "succeeded"),
    ];

    const markup = renderToStaticMarkup(
      createElement(TaskList, {
        loading: false,
        tasks,
        workspaceByConversationId: dummyWorkspaceMap,
        conversationById: dummyConversationMap,
        onOpen: vi.fn(),
        onCancel: vi.fn(),
        onPause: vi.fn(),
        onResume: vi.fn(),
        onRename: vi.fn(),
        renamingTaskId: null,
        renameError: null,
        cancelingTaskId: null,
        pausingTaskId: null,
        resumingTaskId: null,
        emptyLabel: "暂无任务",
        statusLabel: (status) => (status === "running" ? "处理中" : "已完成"),
      }),
    );

    expect(markup).toContain("优化卫衣主图");
    expect(markup).toContain("生成详情海报");
    expect(markup).toContain("春季卫衣");
    expect(markup).toContain("处理中");
    expect(markup).toContain("已完成");
  });

  it("groups tasks into 5 semantic columns in Kanban TaskBoard mode", () => {
    const tasks: AgentTask[] = [
      sampleTask("task-1", "排队中任务", "queued"),
      sampleTask("task-2", "执行中任务", "running"),
      sampleTask("task-3", "待确认任务", "awaiting_confirmation"),
      sampleTask("task-4", "成功任务", "succeeded"),
      sampleTask("task-5", "失败任务", "failed"),
    ];

    const markup = renderToStaticMarkup(
      createElement(TaskBoard, {
        loading: false,
        tasks,
        workspaceByConversationId: dummyWorkspaceMap,
        conversationById: dummyConversationMap,
        onOpen: vi.fn(),
        onCancel: vi.fn(),
        onPause: vi.fn(),
        onResume: vi.fn(),
        onRename: vi.fn(),
        renamingTaskId: null,
        renameError: null,
        cancelingTaskId: null,
        pausingTaskId: null,
        resumingTaskId: null,
        emptyLabel: "暂无任务",
        statusLabel: (status) => status,
      }),
    );

    // 检查 5 个看板泳道标题
    expect(markup).toContain("排队与暂停");
    expect(markup).toContain("执行中");
    expect(markup).toContain("待确认与响应");
    expect(markup).toContain("已完成");
    expect(markup).toContain("失败与已取消");

    // 检查任务卡片在各自泳道中渲染
    expect(markup).toContain("排队中任务");
    expect(markup).toContain("执行中任务");
    expect(markup).toContain("待确认任务");
    expect(markup).toContain("成功任务");
    expect(markup).toContain("失败任务");
    expect(markup).toContain("春季卫衣");
  });

  it("dispatches productflow:open-agent custom event when calling openGlobalAgent in browser environment", () => {
    const originalWindow = globalThis.window;
    const listeners: Record<string, ((e: unknown) => void)[]> = {};
    const mockWindow = {
      dispatchEvent: (event: { type: string }) => {
        listeners[event.type]?.forEach((fn) => fn(event));
        return true;
      },
      addEventListener: (type: string, fn: (e: unknown) => void) => {
        listeners[type] = listeners[type] || [];
        listeners[type].push(fn);
      },
      removeEventListener: (type: string, fn: (e: unknown) => void) => {
        listeners[type] = (listeners[type] || []).filter((f) => f !== fn);
      },
    };
    (globalThis as unknown as { window: unknown }).window = mockWindow;

    const listener = vi.fn();
    mockWindow.addEventListener("productflow:open-agent", listener);

    openGlobalAgent({ tab: "tasks", sessionId: "session-xyz" });

    expect(listener).toHaveBeenCalledTimes(1);
    const event = listener.mock.calls[0][0] as { detail: { tab: string; sessionId: string } };
    expect(event.detail).toEqual({ tab: "tasks", sessionId: "session-xyz" });

    if (originalWindow) {
      (globalThis as unknown as { window: unknown }).window = originalWindow;
    } else {
      delete (globalThis as unknown as { window?: unknown }).window;
    }
  });

  it("treats portal modal targets as inside the dock interaction surface", () => {
    const modalTarget = {
      closest: (selector: string) => selector === "[data-global-agent-modal]" ? {} : null,
    } as unknown as EventTarget;
    const pageTarget = {} as EventTarget;

    expect(isGlobalAgentDockModalTarget(modalTarget)).toBe(true);
    expect(isGlobalAgentDockModalTarget(pageTarget)).toBe(false);
    expect(isGlobalAgentDockModalTarget(null)).toBe(false);
  });

  it("recognizes the current product workbench route without hiding the Dock elsewhere", () => {
    expect(isProductWorkbenchPath("/products/product-1")).toBe(true);
    expect(isProductWorkbenchPath("/products/product-1/")).toBe(true);
    expect(isProductWorkbenchPath("/products")).toBe(false);
    expect(isProductWorkbenchPath("/products/new")).toBe(false);
    expect(isProductWorkbenchPath("/products/new/agent")).toBe(false);
    expect(isProductWorkbenchPath("/settings")).toBe(false);
    expect(shouldRenderGlobalAgentLauncher("/products/product-1", false)).toBe(false);
    expect(shouldRenderGlobalAgentLauncher("/products/product-1", true)).toBe(true);
    expect(shouldRenderGlobalAgentLauncher("/products", false)).toBe(true);
    expect(shouldRenderGlobalAgentLauncher("/settings", false)).toBe(true);
  });

  it("polls Session/Task lists only after control SSE fallback", () => {
    expect(agentDockListRefetchInterval(false, false)).toBe(false);
    expect(agentDockListRefetchInterval(false, true)).toBe(false);
    expect(agentDockListRefetchInterval(true, false)).toBe(false);
    expect(agentDockListRefetchInterval(true, true)).toBe(2_000);
  });

  it("keeps fallback polling until reopened control SSE reconciles changed lists", async () => {
    let fallback: boolean;
    let visibleSessions = "session-before-error";
    let visibleTasks = "task-before-error";
    let remoteSessions = visibleSessions;
    let remoteTasks = visibleTasks;
    let resolveSessions!: () => void;
    let resolveTasks!: () => void;
    const sessionsReady = new Promise<void>((resolve) => {
      resolveSessions = resolve;
    });
    const tasksReady = new Promise<void>((resolve) => {
      resolveTasks = resolve;
    });

    fallback = true;
    remoteSessions = "session-after-error";
    remoteTasks = "task-after-error";
    const reopened = reconcileAgentDockListsOnControlOpen(
      async () => {
        await sessionsReady;
        visibleSessions = remoteSessions;
      },
      async () => {
        await tasksReady;
        visibleTasks = remoteTasks;
      },
      () => {
        fallback = false;
      },
    );

    resolveSessions();
    await Promise.resolve();
    expect(visibleSessions).toBe("session-after-error");
    expect(visibleTasks).toBe("task-before-error");
    expect(fallback).toBe(true);

    resolveTasks();
    await reopened;
    expect(visibleTasks).toBe("task-after-error");
    expect(fallback).toBe(false);
  });
});
