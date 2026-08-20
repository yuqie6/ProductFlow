import { createServer } from "node:http";
import { describe, expect, it } from "vitest";
import { ProductFlowError, type Scope } from "./contracts.js";
import { ProductFlowClient } from "./productflow.js";
import { createProductFlowTools, type ToolRuntime } from "./tools.js";

function runtime(
  scope: Scope,
  client: ProductFlowClient = {} as ProductFlowClient,
  markEffectUnknown: ToolRuntime["markEffectUnknown"] = () => undefined,
  checkpoint: ToolRuntime["checkpoint"] = async () => undefined,
): ToolRuntime {
  return {
    client,
    scope,
    signal: new AbortController().signal,
    loadSkill: async (name, resourcePath) => `loaded:${name}:${resourcePath ?? "body"}`,
    askUser: async () => ({ text: "answer" }),
    proposeArtifact: async () => undefined,
    markWorkflowRunRequested: () => undefined,
    checkpoint,
    markEffectUnknown,
    idempotencyKey: (id) => `pi-test-${id}`,
  };
}

const baseScope: Scope = {
  schema_version: 1,
  scope_type: "product_workflow",
  conversation_id: "11111111-1111-4111-8111-111111111111",
  task_id: null,
  task_goal: null,
  product_id: "22222222-2222-4222-8222-222222222222",
  workflow_draft_id: "33333333-3333-4333-8333-333333333333",
  run_id: "44444444-4444-4444-8444-444444444444",
  system_prompt: "ProductFlow",
  draft_schema: { type: "object" },
  workflow_draft_schema: { type: "object" },
  current_draft_version: 1,
};

describe("ProductFlow Pi tools", () => {
  it("keeps product scope tools bounded and confirmation-oriented", () => {
    const names = createProductFlowTools(runtime(baseScope)).map((tool) => tool.name).sort();
    expect(names).toContain("load_productflow_skill");
    expect(names).toContain("propose_workflow_draft");
    expect(names).toContain("request_workflow_run_v1");
    expect(names).not.toContain("create_product_image_folder_v1");
    expect(names).not.toContain("rename_product_image_asset_v1");
    expect(names).not.toContain("move_product_image_assets_v1");
    expect(names).not.toContain("propose_global_draft");
  });

  it("uses the global draft envelope and does not expose product-only context", () => {
    const globalScope: Scope = {
      ...baseScope,
      scope_type: "global",
      product_id: null,
      workflow_draft_id: null,
      draft_schema: { type: "object" },
      workflow_draft_schema: {},
    };
    const names = createProductFlowTools(runtime(globalScope)).map((tool) => tool.name).sort();
    expect(names).toContain("load_productflow_skill");
    expect(names).toContain("propose_global_draft");
    expect(names).toContain("list_products_v1");
    expect(names).toContain("create_product_workspace_v1");
    expect(names).not.toContain("get_product_workflow_context_v1");
    expect(names).not.toContain("propose_workflow_draft");
  });

  it("reconciles a timed-out workspace create before declaring the effect unknown", async () => {
    const calls: string[] = [];
    const unknownReasons: string[] = [];
    const client = {
      createProductWorkspace: async () => {
        calls.push("create");
        throw new ProductFlowError(504, "timeout", "ProductFlow response timed out");
      },
      reconcileProductWorkspace: async () => {
        calls.push("reconcile");
        return { state: "applied", result: { product_id: "product-1" } };
      },
    } as unknown as ProductFlowClient;
    const globalScope: Scope = {
      ...baseScope,
      scope_type: "global",
      product_id: null,
      workflow_draft_id: null,
    };
    const tool = createProductFlowTools(runtime(globalScope, client, (_id, reason) => unknownReasons.push(reason ?? "")))
      .find((candidate) => candidate.name === "create_product_workspace_v1");
    if (!tool) throw new Error("workspace tool was not registered");

    const result = await tool.execute("tool-1", { name: "商品" }, undefined, undefined, {} as never);

    expect(calls).toEqual(["create", "reconcile"]);
    expect(unknownReasons).toEqual([]);
    expect(result.details).toMatchObject({ product_workspace_created: true, reconciled: true });
  });

  it("reconciles an applied workspace when the ProductFlow response body is lost", async () => {
    let createCount = 0;
    let reconcileCount = 0;
    let createIdempotencyKey = "";
    let reconcileIdempotencyKey = "";
    const server = createServer(async (request, response) => {
      for await (const _chunk of request) {
        // Consume the request body before simulating a lost response.
      }
      const idempotencyKey = request.headers["idempotency-key"];
      if (request.url?.endsWith("/product-workspaces")) {
        createCount += 1;
        createIdempotencyKey = String(idempotencyKey);
        response.writeHead(200, { "Content-Type": "application/json" });
        response.destroy();
        return;
      }
      if (request.url?.endsWith("/product-workspaces/reconcile")) {
        reconcileCount += 1;
        reconcileIdempotencyKey = String(idempotencyKey);
        response.writeHead(200, { "Content-Type": "application/json" });
        response.end(JSON.stringify({ state: "applied", result: { product_id: "product-network-reconciled" } }));
        return;
      }
      response.writeHead(404);
      response.end();
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("test server did not bind");

    try {
      const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
      const globalScope: Scope = { ...baseScope, scope_type: "global", product_id: null, workflow_draft_id: null };
      const client = new ProductFlowClient(
        `http://127.0.0.1:${address.port}`,
        "0123456789abcdef0123456789abcdef",
        1000,
      );
      const tool = createProductFlowTools(
        runtime(globalScope, client, undefined, async (kind, payload) => {
          checkpoints.push({ kind, payload });
        }),
      ).find((candidate) => candidate.name === "create_product_workspace_v1");
      if (!tool) throw new Error("workspace tool was not registered");

      const result = await tool.execute("tool-network-loss", { name: "网络对账商品" }, undefined, undefined, {} as never);

      expect(createCount).toBe(1);
      expect(reconcileCount).toBe(1);
      expect(createIdempotencyKey).toBe("pi-test-tool-network-loss");
      expect(reconcileIdempotencyKey).toBe(createIdempotencyKey);
      expect(result.details).toMatchObject({ product_workspace_created: true, reconciled: true });
      expect(checkpoints.map((checkpoint) => checkpoint.kind)).toEqual([
        "tool_effect_intent",
        "tool_effect_result",
      ]);
      expect(checkpoints[1].payload).toMatchObject({
        result: "applied",
        reconciliation_state: "applied",
      });
    } finally {
      server.closeAllConnections();
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });

  it("records a reconciled workspace create as an applied effect", async () => {
    const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
    const client = {
      createProductWorkspace: async () => {
        throw new ProductFlowError(504, "timeout", "ProductFlow response timed out");
      },
      reconcileProductWorkspace: async () => ({ state: "applied", result: { product_id: "product-1" } }),
    } as unknown as ProductFlowClient;
    const globalScope: Scope = {
      ...baseScope,
      scope_type: "global",
      product_id: null,
      workflow_draft_id: null,
    };
    const tool = createProductFlowTools(
      runtime(globalScope, client, undefined, async (kind, payload) => {
        checkpoints.push({ kind, payload });
      }),
    ).find((candidate) => candidate.name === "create_product_workspace_v1");
    if (!tool) throw new Error("workspace tool was not registered");

    await tool.execute("tool-reconciled", { name: "商品" }, undefined, undefined, {} as never);

    expect(checkpoints.at(-1)).toMatchObject({
      kind: "tool_effect_result",
      payload: { result: "applied", reconciliation_state: "applied" },
    });
  });

  it("does not classify an applied workspace as failed when result checkpoint persistence fails", async () => {
    const calls: string[] = [];
    let checkpointCount = 0;
    const client = {
      createProductWorkspace: async () => {
        calls.push("create");
        return { product_id: "product-1" };
      },
      reconcileProductWorkspace: async () => {
        calls.push("reconcile");
        return { state: "not_applied" };
      },
    } as unknown as ProductFlowClient;
    const globalScope: Scope = {
      ...baseScope,
      scope_type: "global",
      product_id: null,
      workflow_draft_id: null,
    };
    const tool = createProductFlowTools(
      runtime(
        globalScope,
        client,
        undefined,
        async () => {
          checkpointCount += 1;
          if (checkpointCount === 2) throw new Error("checkpoint unavailable");
        },
      ),
    ).find((candidate) => candidate.name === "create_product_workspace_v1");
    if (!tool) throw new Error("workspace tool was not registered");

    await expect(tool.execute("tool-checkpoint-failure", { name: "商品" }, undefined, undefined, {} as never)).rejects.toThrow(
      "checkpoint unavailable",
    );
    expect(calls).toEqual(["create"]);
  });

  it("marks a workspace create unknown when read-only reconciliation cannot prove an outcome", async () => {
    const unknownReasons: string[] = [];
    const operations: string[] = [];
    const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
    const client = {
      createProductWorkspace: async () => {
        throw new ProductFlowError(504, "timeout", "ProductFlow response timed out");
      },
      reconcileProductWorkspace: async () => ({ state: "unknown", detail: "database unavailable" }),
    } as unknown as ProductFlowClient;
    const globalScope: Scope = {
      ...baseScope,
      scope_type: "global",
      product_id: null,
      workflow_draft_id: null,
    };
    const tool = createProductFlowTools(
      runtime(
        globalScope,
        client,
        (_id, reason) => {
          operations.push("unknown");
          unknownReasons.push(reason ?? "");
        },
        async (kind, payload) => {
          checkpoints.push({ kind, payload });
          if (kind === "tool_effect_result") operations.push("effect_checkpoint");
        },
      ),
    )
      .find((candidate) => candidate.name === "create_product_workspace_v1");
    if (!tool) throw new Error("workspace tool was not registered");

    await expect(tool.execute("tool-unknown", { name: "商品" }, undefined, undefined, {} as never)).rejects.toMatchObject({
      status: 504,
      code: "timeout",
    });
    expect(unknownReasons).toEqual(["Product workspace creation result is unknown"]);
    expect(operations).toEqual(["effect_checkpoint", "unknown"]);
    expect(checkpoints.at(-1)).toMatchObject({
      kind: "tool_effect_result",
      payload: { result: "unknown", reconciliation_state: "unknown" },
    });
  });

  it("records applied workflow request effects after ProductFlow accepts the request", async () => {
    const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
    const client = {
      prepareWorkflowRunRequest: async () => ({
        product_id: baseScope.product_id!,
        workflow_id: "workflow-1",
        workflow_title: "工作流",
        workflow_revision: 3,
        runnable_node_count: 2,
        task_id: null,
        source_run_id: null,
      }),
      executeWorkflowRunRequest: async () => ({ request_id: "request-1", status: "awaiting_confirmation" }),
    } as unknown as ProductFlowClient;
    const tool = createProductFlowTools(
      runtime(baseScope, client, undefined, async (kind, payload) => {
        checkpoints.push({ kind, payload });
      }),
    ).find((candidate) => candidate.name === "request_workflow_run_v1");
    if (!tool) throw new Error("workflow request tool was not registered");

    const result = await tool.execute("tool-run-applied", { expected_workflow_revision: 3 }, undefined, undefined, {} as never);

    expect(result.terminate).toBe(true);
    expect(checkpoints.map((entry) => [entry.kind, entry.payload.result])).toEqual([
      ["tool_effect_intent", undefined],
      ["external_job_submitted", undefined],
      ["tool_effect_result", "applied"],
    ]);
  });

  it("reconciles a workflow request when the ProductFlow response body is lost", async () => {
    let executeCount = 0;
    let reconcileCount = 0;
    let executeIdempotencyKey = "";
    let reconcileIdempotencyKey = "";
    const server = createServer(async (request, response) => {
      const chunks: Buffer[] = [];
      for await (const chunk of request) chunks.push(Buffer.from(chunk));
      const idempotencyKey = request.headers["idempotency-key"];
      if (request.url?.endsWith("/workflow-run-requests/prepare")) {
        response.writeHead(200, { "Content-Type": "application/json" });
        response.end(JSON.stringify({
          product_id: baseScope.product_id,
          workflow_id: "workflow-network-reconciled",
          workflow_title: "网络对账工作流",
          workflow_revision: 3,
          runnable_node_count: 2,
          task_id: null,
          source_run_id: null,
        }));
        return;
      }
      if (request.url?.endsWith("/workflow-run-requests")) {
        executeCount += 1;
        executeIdempotencyKey = String(idempotencyKey);
        response.writeHead(200, { "Content-Type": "application/json" });
        response.destroy();
        return;
      }
      if (request.url?.endsWith("/workflow-run-requests/reconcile")) {
        reconcileCount += 1;
        reconcileIdempotencyKey = String(idempotencyKey);
        response.writeHead(200, { "Content-Type": "application/json" });
        response.end(JSON.stringify({
          state: "applied",
          result: { request_id: "request-network-reconciled", status: "awaiting_confirmation" },
        }));
        return;
      }
      response.writeHead(404);
      response.end();
      void chunks;
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("test server did not bind");

    try {
      const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
      const client = new ProductFlowClient(
        `http://127.0.0.1:${address.port}`,
        "0123456789abcdef0123456789abcdef",
        1000,
      );
      const tool = createProductFlowTools(
        runtime(baseScope, client, undefined, async (kind, payload) => {
          checkpoints.push({ kind, payload });
        }),
      ).find((candidate) => candidate.name === "request_workflow_run_v1");
      if (!tool) throw new Error("workflow request tool was not registered");

      const result = await tool.execute("tool-workflow-network-loss", { expected_workflow_revision: 3 }, undefined, undefined, {} as never);

      expect(executeCount).toBe(1);
      expect(reconcileCount).toBe(1);
      expect(executeIdempotencyKey).toBe("pi-test-tool-workflow-network-loss");
      expect(reconcileIdempotencyKey).toBe(executeIdempotencyKey);
      expect(result.terminate).toBe(true);
      expect(result.details).toMatchObject({ pending_confirmation: true, reconciled: true });
      expect(checkpoints.map((checkpoint) => checkpoint.kind)).toEqual([
        "tool_effect_intent",
        "external_job_submitted",
        "tool_effect_result",
      ]);
      expect(checkpoints[2].payload).toMatchObject({
        result: "applied",
      });
    } finally {
      server.closeAllConnections();
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });

  it("records a failed workflow request when reconciliation proves it was not applied", async () => {
    const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
    const client = {
      prepareWorkflowRunRequest: async () => ({
        product_id: baseScope.product_id!,
        workflow_id: "workflow-1",
        workflow_title: "工作流",
        workflow_revision: 3,
        runnable_node_count: 2,
        task_id: null,
        source_run_id: null,
      }),
      executeWorkflowRunRequest: async () => {
        throw new ProductFlowError(504, "timeout", "request timed out");
      },
      reconcileWorkflowRunRequest: async () => ({ state: "not_applied" }),
    } as unknown as ProductFlowClient;
    const tool = createProductFlowTools(
      runtime(baseScope, client, undefined, async (kind, payload) => {
        checkpoints.push({ kind, payload });
      }),
    ).find((candidate) => candidate.name === "request_workflow_run_v1");
    if (!tool) throw new Error("workflow request tool was not registered");

    await expect(tool.execute("tool-run-failed", { expected_workflow_revision: 3 }, undefined, undefined, {} as never)).rejects.toMatchObject({
      code: "timeout",
    });

    expect(checkpoints.at(-1)).toMatchObject({
      kind: "tool_effect_result",
      payload: { result: "failed", reconciliation_state: "not_applied" },
    });
  });

  it("records an unknown workflow request result and stops continuation", async () => {
    const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
    const unknownReasons: string[] = [];
    const client = {
      prepareWorkflowRunRequest: async () => ({
        product_id: baseScope.product_id!,
        workflow_id: "workflow-1",
        workflow_title: "工作流",
        workflow_revision: 3,
        runnable_node_count: 2,
        task_id: null,
        source_run_id: null,
      }),
      executeWorkflowRunRequest: async () => {
        throw new ProductFlowError(504, "timeout", "request timed out");
      },
      reconcileWorkflowRunRequest: async () => ({ state: "unknown" }),
    } as unknown as ProductFlowClient;
    const tool = createProductFlowTools(
      runtime(
        baseScope,
        client,
        (_id, reason) => unknownReasons.push(reason ?? ""),
        async (kind, payload) => {
          checkpoints.push({ kind, payload });
        },
      ),
    ).find((candidate) => candidate.name === "request_workflow_run_v1");
    if (!tool) throw new Error("workflow request tool was not registered");

    await expect(tool.execute("tool-run-unknown", { expected_workflow_revision: 3 }, undefined, undefined, {} as never)).rejects.toMatchObject({
      code: "timeout",
    });

    expect(unknownReasons).toEqual(["WorkflowRun request result is unknown"]);
    expect(checkpoints.at(-1)).toMatchObject({
      kind: "tool_effect_result",
      payload: { result: "unknown", reconciliation_state: "unknown" },
    });
  });
});
