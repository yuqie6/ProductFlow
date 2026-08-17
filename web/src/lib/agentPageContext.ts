import { useEffect, useId, useSyncExternalStore } from "react";

import type { AgentPageContextSnapshotInput } from "./types";

interface RegisteredPageContext {
  ownerId: string;
  context: AgentPageContextSnapshotInput;
}

const listeners = new Set<() => void>();
let registeredPageContext: RegisteredPageContext | null = null;

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
