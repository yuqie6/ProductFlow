export type GlobalAgentDockTab = "chat" | "tasks" | "sessions";

export function openGlobalAgent(options?: { tab?: GlobalAgentDockTab; sessionId?: string; taskId?: string }): void {
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent("productflow:open-agent", { detail: options }));
  }
}
