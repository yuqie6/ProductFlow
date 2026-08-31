import { describe, expect, it } from "vitest";

import { ApiError } from "../../../lib/api";
import type { GraphProjection } from "../../../lib/types";
import {
  agentProductIntakeResumePath,
  agentProductWorkbenchPath,
  agentWorkbenchQueryKey,
  isAgentWorkbenchMissing,
  isHttpErrorStatus,
  isWorkflowGraphMissing,
  loadProductWorkbenchAgent,
  productWorkbenchRouteTarget,
  readWorkflowGraphOrNull,
  rememberAgentWorkbenchQueryData,
  resolveProductWorkbenchSurface,
  type ProductWorkbenchRouteInput,
} from "./productWorkbenchRoute";

describe("loadProductWorkbenchAgent", () => {
  it("reads the Agent workbench without creating a conversation on page load", async () => {
    const calls: string[] = [];
    const bootstrap = { mode: "agent" };
    const result = await loadProductWorkbenchAgent(
      {
        getAgentWorkbench: async (productId, sessionId) => {
          calls.push(`get:${productId}:${sessionId ?? ""}`);
          return bootstrap as never;
        },
        ensureAgentWorkbench: async (productId, sessionId) => {
          calls.push(`ensure:${productId}:${sessionId ?? ""}`);
          return bootstrap as never;
        },
      },
      "product-1",
      "session-1",
    );
    expect(calls).toEqual(["get:product-1:session-1"]);
    expect(result).toBe(bootstrap);
  });

  it("reads an existing Task-scoped workbench without creating a workspace", async () => {
    const calls: string[] = [];
    const bootstrap = { mode: "agent" };
    await loadProductWorkbenchAgent(
      {
        getAgentWorkbench: async (productId, sessionId, taskId) => {
          calls.push(`get:${productId}:${sessionId}:${taskId}`);
          return bootstrap as never;
        },
        ensureAgentWorkbench: async () => {
          calls.push("ensure");
          return bootstrap as never;
        },
      },
      "product-1",
      "session-1",
      "task-1",
    );
    expect(calls).toEqual(["get:product-1:session-1:task-1"]);
  });
});

describe("productWorkbenchRouteTarget", () => {
  it("keeps an Agent product on the workbench", () => {
    expect(productWorkbenchRouteTarget({ conversation: { id: "c1" } })).toBe("agent");
    expect(agentProductWorkbenchPath("product/1", "session/1", "task/1")).toBe(
      "/products/product%2F1?agent_session_id=session%2F1&agent_task_id=task%2F1",
    );
    expect(agentProductIntakeResumePath("conversation/1", "session/1")).toBe(
      "/products/new?workspace=conversation%2F1&agent_session_id=session%2F1",
    );
    expect(agentWorkbenchQueryKey("product-1", "session-1")).toEqual([
      "agent-workbench",
      "product-1",
      "session-1",
      null,
    ]);
  });
});

describe("resolveProductWorkbenchSurface", () => {
  const graph = { id: "graph-1" } as GraphProjection;
  const agent = {
    conversation: { id: "conversation-1" },
  } satisfies ProductWorkbenchRouteInput;

  it("keeps the Agent conversation when a v3 graph already exists", () => {
    expect(resolveProductWorkbenchSurface({
      graph,
      graphPending: false,
      graphError: null,
      agent,
      agentPending: false,
      agentError: null,
    })).toEqual({ kind: "agent", bootstrap: agent });
  });

  it("uses the graph workbench only when the product has no Agent workspace", () => {
    expect(resolveProductWorkbenchSurface({
      graph,
      graphPending: false,
      graphError: null,
      agentPending: false,
      agentError: new ApiError(409, "商品还没有 Agent 工作区"),
    })).toEqual({ kind: "graph", graph });
  });

  it("keeps unfinished empty drafts on the Agent workbench", () => {
    expect(resolveProductWorkbenchSurface({
      graphPending: false,
      graphError: new ApiError(404, "商品工作流不存在"),
      agent,
      agentPending: false,
      agentError: null,
    }).kind).toBe("agent");
  });

  it("keeps the Agent workbench when a missing graph resolved as empty instead of erroring", () => {
    expect(resolveProductWorkbenchSurface({
      graphPending: false,
      graphError: null,
      agent,
      agentPending: false,
      agentError: null,
    })).toEqual({ kind: "agent", bootstrap: agent });
  });

  it("keeps an empty draft on the Agent workbench when a v3 graph already exists", () => {
    expect(resolveProductWorkbenchSurface({
      graph,
      graphPending: false,
      graphError: null,
      agent,
      agentPending: false,
      agentError: null,
    })).toEqual({ kind: "agent", bootstrap: agent });
  });

  it("treats graph 404 and Agent 409 by status even without the ApiError class", () => {
    expect(isHttpErrorStatus({ status: 404 }, 404)).toBe(true);
    expect(isWorkflowGraphMissing({ status: 404, detail: "missing" })).toBe(true);
    expect(isAgentWorkbenchMissing({ status: 409, detail: "no workspace" })).toBe(true);
    expect(isWorkflowGraphMissing({ status: 500 })).toBe(false);
  });

  it("reads a missing workflow graph as empty instead of throwing", async () => {
    await expect(
      readWorkflowGraphOrNull(async () => {
        throw { status: 404, detail: "商品工作流不存在" };
      }),
    ).resolves.toBeNull();
    await expect(
      readWorkflowGraphOrNull(async () => graph),
    ).resolves.toBe(graph);
    await expect(
      readWorkflowGraphOrNull(async () => {
        throw { status: 500, detail: "boom" };
      }),
    ).rejects.toEqual({ status: 500, detail: "boom" });
  });

  it("waits until graph and Agent queries settle", () => {
    expect(resolveProductWorkbenchSurface({
      graph,
      graphPending: false,
      graphError: null,
      agentPending: true,
      agentError: null,
    }).kind).toBe("loading");
  });
});

describe("rememberAgentWorkbenchQueryData", () => {
  it("writes the session-scoped workbench query and the live graph", () => {
    const writes: Array<{ key: readonly unknown[]; data: unknown }> = [];
    const graph = { id: "graph-1" } as GraphProjection;
    const bootstrap = {
      product: { id: "product-1" },
      conversation: { session_id: "session-2" },
      graph,
    } as never;
    rememberAgentWorkbenchQueryData(
      (queryKey, data) => {
        writes.push({ key: queryKey, data });
      },
      bootstrap,
      "session-2",
    );
    expect(writes).toEqual([
      { key: ["agent-workbench", "product-1", "session-2", null], data: bootstrap },
      { key: ["workflow-graph", "product-1"], data: graph },
    ]);
  });
});
