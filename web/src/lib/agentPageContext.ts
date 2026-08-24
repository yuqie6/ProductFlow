/**
 * 当前路由的环境 Agent 页面上下文登记。
 *
 * 路由变化只更新后续 Turn 的快照，不改写 AgentTask 目标。同一时间只有一个页面 owner。
 */

import { useEffect, useId, useSyncExternalStore } from "react";

import type { AgentPageContextSnapshotInput } from "./types";

interface RegisteredPageContext {
  ownerId: string;
  context: AgentPageContextSnapshotInput;
}

const listeners = new Set<() => void>();
let registeredPageContext: RegisteredPageContext | null = null;

/** 最后注册的路由拥有这份快照；若已被其他 owner 替换，注销会被忽略。 */
export function registerAgentPageContext(
  ownerId: string,
  context: AgentPageContextSnapshotInput,
): () => void {
  registeredPageContext = { ownerId, context };
  notifyListeners();

  return () => {
    if (registeredPageContext?.ownerId !== ownerId) {
      return;
    }
    registeredPageContext = null;
    notifyListeners();
  };
}

export function getRegisteredAgentPageContext(): AgentPageContextSnapshotInput | null {
  return registeredPageContext?.context ?? null;
}

export function subscribeAgentPageContext(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useAgentPageContext(): AgentPageContextSnapshotInput | null {
  return useSyncExternalStore(
    subscribeAgentPageContext,
    getRegisteredAgentPageContext,
    () => null,
  );
}

export function useRegisterAgentPageContext(
  context: AgentPageContextSnapshotInput | null,
): void {
  const ownerId = useId();

  useEffect(() => {
    if (context === null) {
      return;
    }
    return registerAgentPageContext(ownerId, context);
  }, [context, ownerId]);
}

function notifyListeners(): void {
  listeners.forEach((listener) => listener());
}
