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
    pageType: scope.scope_type === "product_workflow" ? "product_workbench" : "app",
    signal: new AbortController().signal,
    loadSkill: async (name, resourcePath) => `loaded:${name}:${resourcePath ?? "body"}`,
    recordToolFailure: () => undefined,
    askUser: async () => ({ text: "answer" }),
    requestApproval: () => undefined,
    checkpoint,
    reconcileEffect: (toolCallID) => client.reconcileTurnEffect(
      scope.conversation_id,
      "execution-test",
      { owner_id: "worker-test", lease_token: "lease-test", tool_call_id: toolCallID },
    ),
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
  run_id: "44444444-4444-4444-8444-444444444444",
  system_prompt: "ProductFlow",
  draft_schema: { type: "object" },
  current_draft_version: 1,
  has_live_graph: false,
};

describe("ProductFlow Pi tools", () => {
  it("keeps product scope tools bounded and confirmation-oriented", () => {
    const names = createProductFlowTools(runtime(baseScope)).map((tool) => tool.name).sort();
    expect(names).toContain("load_productflow_skill");
    expect(names).not.toContain("propose_workflow_draft");
    expect(names).toContain("request_workflow_run_v1");
    expect(names).toContain("finalize_product_intake_v1");
    expect(names).toContain("get_workflow_run_detail_v1");
    expect(names).not.toContain("request_global_workflow_run_v1");
    expect(names).not.toContain("apply_graph_change_set_v1");
    expect(names).not.toContain("get_node_detail_v1");
    expect(names).not.toContain("discard_workflow_proposal_v1");
    expect(names).not.toContain("cancel_workflow_run_v1");
    expect(names).not.toContain("focus_canvas_items_v1");
    expect(names).not.toContain("create_product_image_folder_v1");
    expect(names).not.toContain("rename_product_image_asset_v1");
    expect(names).not.toContain("move_product_image_assets_v1");
    expect(names).not.toContain("propose_global_draft");
    expect(names).not.toContain("list_legacy_archives_v1");
    expect(names).not.toContain("inspect_legacy_archive_v1");
  });

  it("exposes live-graph command tools and hides covering drafts", () => {
    const names = createProductFlowTools(runtime({ ...baseScope, has_live_graph: true })).map((tool) => tool.name).sort();
    expect(names).toContain("apply_graph_change_set_v1");
    expect(names).toContain("propose_graph_change_set_v1");
    expect(names).toContain("get_node_detail_v1");
    expect(names).toContain("discard_workflow_proposal_v1");
    expect(names).toContain("cancel_workflow_run_v1");
    expect(names).toContain("focus_canvas_items_v1");
    expect(names).not.toContain("confirm_graph_proposal_v1");
    expect(names).not.toContain("discard_graph_proposal_v1");
    expect(names).toContain("request_workflow_run_v1");
    expect(names).not.toContain("propose_workflow_draft");
    expect(names).not.toContain("list_legacy_archives_v1");
  });

  it("persists product intake from conversation asset IDs", async () => {
    const calls: string[] = [];
    let forwardedSelection: Record<string, unknown> | undefined;
    const client = {
      finalizeProductIntake: async (_conversationID: string, args: { selection: Record<string, unknown> }) => {
        calls.push("finalize");
        forwardedSelection = args.selection;
        return { accepted: true, intake_finalized: true, product_id: "product-1", node_count: 19, group_count: 4 };
      },
    } as unknown as ProductFlowClient;
    const tool = createProductFlowTools(runtime(baseScope, client)).find(
      (candidate) => candidate.name === "finalize_product_intake_v1",
    );
    if (!tool) throw new Error("intake tool was not registered");

    const result = await tool.execute(
      "tool-1",
      {
        selection: {
          schema_version: 1,
          delivery_preset_key: "jd_hero",
          image_types: [{ key: "hero", quantity: 2, order: 0 }],
        },
        reference_asset_ids: ["asset-1"],
      },
      undefined,
      undefined,
      {} as never,
    );

    expect(calls).toEqual(["finalize"]);
    expect(forwardedSelection).toEqual({
      schema_version: 1,
      delivery_preset_key: "jd_hero",
      image_types: [{ key: "hero", quantity: 2, order: 0 }],
    });
    expect(result.content[0]).toMatchObject({ type: "text" });
    expect(String((result.content[0] as { text: string }).text)).toContain("intake_finalized");
    expect(result.details).toMatchObject({ node_count: 19, group_count: 4 });
  });

  it("uses the global draft envelope and does not expose product-only context", () => {
    const globalScope: Scope = {
      ...baseScope,
      scope_type: "global",
      product_id: null,
      draft_schema: { type: "object" },
    };
    const names = createProductFlowTools(runtime(globalScope)).map((tool) => tool.name).sort();
    expect(names).toContain("load_productflow_skill");
    expect(names).toContain("propose_global_draft");
    expect(names).toContain("list_products_v1");
    expect(names).toContain("create_product_workspace_v1");
    expect(names).toContain("request_global_workflow_run_v1");
    expect(names).toContain("get_workflow_run_detail_v1");
    expect(names).not.toContain("request_workflow_run_v1");
    expect(names).not.toContain("get_product_workflow_context_v1");
    expect(names).not.toContain("propose_workflow_draft");
    expect(names).not.toContain("finalize_product_intake_v1");
    expect(names).not.toContain("list_legacy_archives_v1");
  });

  it("does not register legacy archive tools on any page", () => {
    const pages = ["history", "app", "product_workbench"] as const;
    for (const pageType of pages) {
      const globalNames = createProductFlowTools({
        ...runtime({ ...baseScope, scope_type: "global", product_id: null }),
        pageType,
      }).map((tool) => tool.name);
      const productNames = createProductFlowTools({
        ...runtime(baseScope),
        pageType,
      }).map((tool) => tool.name);
      expect(globalNames).not.toContain("list_legacy_archives_v1");
      expect(globalNames).not.toContain("inspect_legacy_archive_v1");
      expect(productNames).not.toContain("list_legacy_archives_v1");
      expect(productNames).not.toContain("inspect_legacy_archive_v1");
    }
  });

  it("returns bounded Skill evidence while keeping the full instruction for the model", async () => {
    const instruction = `# Workflow rules\n\n${"保持已核验事实。".repeat(4_000)}`;
    const testRuntime = {
      ...runtime(baseScope),
      loadSkill: async () => instruction,
    };
    const tool = createProductFlowTools(testRuntime).find((candidate) => candidate.name === "load_productflow_skill");
    if (!tool) throw new Error("Skill tool was not registered");

    const result = await tool.execute("skill-evidence", { skill_name: "product-intake" }, undefined, undefined, {} as never);
    const excerpt = (result.details as { instruction_excerpt?: string } | undefined)?.instruction_excerpt;
    expect(typeof excerpt).toBe("string");
    expect(Buffer.byteLength(excerpt ?? "", "utf8")).toBeLessThanOrEqual(12 << 10);
    expect(result.details).toMatchObject({ skill_name: "product-intake", instruction_truncated: true });
    expect(result.content[0]).toMatchObject({ type: "text" });
    expect((result.content[0] as { text: string }).text).toContain(instruction.slice(0, 64));
  });

  it("keeps oversized product context JSON-safe and preserves the complete Node Catalog", async () => {
    const nodeCatalog = {
      version: 3,
      nodes: [
        {
          node_type: "image_generation",
          config_fields: [
            {
              key: "generation_spec",
              fields: [{ key: "aspect_ratio" }],
            },
          ],
        },
      ],
    };
    const context = {
      schema_version: 1,
      product: {
        id: baseScope.product_id,
        name: "大上下文商品",
        category: "工业收纳",
        price: null,
        source_note: "x".repeat(100_000),
      },
      confirmed_fact_set: null,
      intake: null,
      live_graph: { revision: 1, node_count: 4, edge_count: 7, group_count: 2, nodes: [{ id: "n1" }] },
      node_catalog: nodeCatalog,
    };
    const client = {
      productContext: async () => context,
    } as unknown as ProductFlowClient;
    const tool = createProductFlowTools(runtime(baseScope, client)).find(
      (candidate) => candidate.name === "get_product_workflow_context_v1",
    );
    if (!tool) throw new Error("product context tool was not registered");

    const result = await tool.execute("large-context", {}, undefined, undefined, {} as never);
    const text = (result.content[0] as { text: string }).text;
    const parsed = JSON.parse(text) as {
      schema_version: number;
      data: { truncated?: boolean; node_catalog?: unknown; live_graph?: { edge_count?: number; node_count?: number } };
      guidance?: string;
    };
    expect(parsed.schema_version).toBe(1);
    expect(parsed.data.node_catalog).toEqual(nodeCatalog);
    expect(parsed.data.live_graph?.edge_count).toBe(7);
    expect(parsed.data.live_graph?.node_count).toBe(4);
    expect(JSON.stringify(parsed.data)).toContain("大上下文商品");
  });

  it("keeps the complete Node Catalog in a defensive product context overflow envelope", async () => {
    const nodeCatalog = {
      version: 3,
      nodes: [
        {
          node_type: "image_generation",
          config_fields: [{ key: "generation_spec", fields: [{ key: "aspect_ratio" }] }],
        },
      ],
    };
    const client = {
      productContext: async () => ({
        schema_version: 1,
        product: { source_note: "x".repeat((512 << 10) + 10_000) },
        node_catalog: nodeCatalog,
      }),
    } as unknown as ProductFlowClient;
    const tool = createProductFlowTools(runtime(baseScope, client)).find(
      (candidate) => candidate.name === "get_product_workflow_context_v1",
    );
    if (!tool) throw new Error("product context tool was not registered");

    const result = await tool.execute("overflowing-context", {}, undefined, undefined, {} as never);
    const text = (result.content[0] as { text: string }).text;
    const parsed = JSON.parse(text) as {
      schema_version: number;
      data: { truncated?: boolean; node_catalog?: unknown };
      guidance?: string;
    };

    expect(parsed.schema_version).toBe(1);
    expect(parsed.guidance).toMatch(/截断/);
    expect(parsed.data.node_catalog).toEqual(nodeCatalog);
    expect(result.details).toMatchObject({ truncated: true });
    expect(Buffer.byteLength(text, "utf8")).toBeLessThanOrEqual(96 << 10);
  });

  it("returns a legal bounded JSON contract for oversized generic results", async () => {
    const client = {
      workflowRuns: async () => ({ runs: ["x".repeat(100_000)] }),
    } as unknown as ProductFlowClient;
    const tool = createProductFlowTools(runtime(baseScope, client)).find(
      (candidate) => candidate.name === "inspect_workflow_runs_v1",
    );
    if (!tool) throw new Error("workflow runs tool was not registered");

    const result = await tool.execute("large-generic-result", { limit: 1 }, undefined, undefined, {} as never);
    const text = (result.content[0] as { text: string }).text;
    const parsed = JSON.parse(text) as {
      schema_version: number;
      data: { items?: unknown[]; total_count?: number };
      guidance?: string;
    };

    expect(parsed.schema_version).toBe(1);
    expect(parsed.guidance).toMatch(/截断/);
    expect(parsed.data.total_count).toBe(1);
    expect(result.details).toMatchObject({ truncated: true, max_bytes: 96 << 10 });
    expect(text).not.toContain("...[truncated]");
    expect(Buffer.byteLength(text, "utf8")).toBeLessThanOrEqual(96 << 10);
  });

  it("does not register propose_workflow_draft on product-workflow conversations", () => {
    const withoutGraph = createProductFlowTools(runtime(baseScope)).map((tool) => tool.name);
    const withGraph = createProductFlowTools(runtime({ ...baseScope, has_live_graph: true })).map((tool) => tool.name);
    expect(withoutGraph).not.toContain("propose_workflow_draft");
    expect(withGraph).not.toContain("propose_workflow_draft");
  });

  it("reconciles a timed-out workspace create before declaring the effect unknown", async () => {
    const calls: string[] = [];
    const unknownReasons: string[] = [];
    const client = {
      createProductWorkspace: async () => {
        calls.push("create");
        throw new ProductFlowError(504, "timeout", "ProductFlow response timed out");
      },
      reconcileTurnEffect: async () => {
        calls.push("reconcile");
        return { effect_result: "applied", reconciliation_state: "applied", result: { product_id: "product-1" } };
      },
    } as unknown as ProductFlowClient;
    const globalScope: Scope = {
      ...baseScope,
      scope_type: "global",
      product_id: null,
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
    let reconciledToolCallID = "";
    const server = createServer(async (request, response) => {
      const chunks: Buffer[] = [];
      for await (const chunk of request) chunks.push(Buffer.from(chunk));
      const idempotencyKey = request.headers["idempotency-key"];
      if (request.url?.endsWith("/product-workspaces")) {
        createCount += 1;
        createIdempotencyKey = String(idempotencyKey);
        response.writeHead(200, { "Content-Type": "application/json" });
        response.destroy();
        return;
      }
      if (request.url?.endsWith("/turn-executions/execution-test/effects/reconcile")) {
        reconcileCount += 1;
        reconciledToolCallID = (JSON.parse(Buffer.concat(chunks).toString("utf8")) as { tool_call_id: string }).tool_call_id;
        response.writeHead(200, { "Content-Type": "application/json" });
        response.end(JSON.stringify({
          effect_result: "applied",
          reconciliation_state: "applied",
          result: { product_id: "product-network-reconciled" },
        }));
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
      const globalScope: Scope = { ...baseScope, scope_type: "global", product_id: null };
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
      expect(reconciledToolCallID).toBe("tool-network-loss");
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
      reconcileTurnEffect: async () => ({ effect_result: "applied", reconciliation_state: "applied", result: { product_id: "product-1" } }),
    } as unknown as ProductFlowClient;
    const globalScope: Scope = {
      ...baseScope,
      scope_type: "global",
      product_id: null,
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
    } as unknown as ProductFlowClient;
    const globalScope: Scope = {
      ...baseScope,
      scope_type: "global",
      product_id: null,
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
      reconcileTurnEffect: async () => ({ effect_result: "unknown", reconciliation_state: "unknown", detail: "database unavailable" }),
    } as unknown as ProductFlowClient;
    const globalScope: Scope = {
      ...baseScope,
      scope_type: "global",
      product_id: null,
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

    expect(result.terminate).toBeUndefined();
    expect(result.details).toMatchObject({
      pending_confirmation: true,
      workflow_id: "workflow-1",
      workflow_title: "工作流",
      request_id: "request-1",
      expected_workflow_revision: 3,
    });
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
    let reconciledToolCallID = "";
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
      if (request.url?.endsWith("/turn-executions/execution-test/effects/reconcile")) {
        reconcileCount += 1;
        reconciledToolCallID = (JSON.parse(Buffer.concat(chunks).toString("utf8")) as { tool_call_id: string }).tool_call_id;
        response.writeHead(200, { "Content-Type": "application/json" });
        response.end(JSON.stringify({
          effect_result: "applied",
          reconciliation_state: "applied",
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
      expect(reconciledToolCallID).toBe("tool-workflow-network-loss");
      expect(result.terminate).toBeUndefined();
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

  it("does not retry a workflow mutation after Go returns unknown", async () => {
    const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
    let executeCalls = 0;
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
        executeCalls += 1;
        throw new ProductFlowError(504, "timeout", "request timed out");
      },
      reconcileTurnEffect: async () => ({ effect_result: "unknown", reconciliation_state: "unknown" }),
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
      payload: { result: "unknown", reconciliation_state: "unknown" },
    });
    expect(executeCalls).toBe(1);
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
      reconcileTurnEffect: async () => ({ effect_result: "unknown", reconciliation_state: "unknown" }),
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

  it("sends Idempotency-Key derived from toolCallID for graph mutations", async () => {
    const keys: string[] = [];
    const approvals: Array<Record<string, unknown>> = [];
    const client = {
      applyGraphChangeSet: async (_conversationID: string, _changeSet: object, idempotencyKey: string) => {
        keys.push(idempotencyKey);
        return { accepted: true, applied: true, revision: 2 };
      },
      proposeGraphChangeSet: async (_conversationID: string, _changeSet: object, idempotencyKey: string) => {
        keys.push(idempotencyKey);
        return { accepted: true, applied: false, pending_confirmation: true, proposal_id: "p1" };
      },
    } as unknown as ProductFlowClient;
    const toolRuntime = runtime({ ...baseScope, has_live_graph: true }, client);
    toolRuntime.requestApproval = (approval) => approvals.push(approval);
    const tools = createProductFlowTools(toolRuntime);
    const apply = tools.find((candidate) => candidate.name === "apply_graph_change_set_v1");
    const propose = tools.find((candidate) => candidate.name === "propose_graph_change_set_v1");
    if (!apply || !propose) throw new Error("graph mutation tools were not registered");
    const params = {
      base_graph_revision: 1,
      summary: "改名",
      operations: [{ op: "rename_node", node_ref: "n1", title: "新标题" }],
    };
    const applyResult = await apply.execute("tool-apply-1", params, undefined, undefined, {} as never);
    await propose.execute("tool-propose-1", { ...params, operations: [params.operations[0], params.operations[0]] }, undefined, undefined, {} as never);
    expect(keys).toEqual(["pi-test-tool-apply-1", "pi-test-tool-propose-1"]);
    expect(approvals).toEqual([expect.objectContaining({
      approval_id: "p1",
      approval_kind: "graph_proposal",
      proposal_id: "p1",
      pending_confirmation: true,
    })]);
    expect(applyResult.details).toMatchObject({
      operation_summaries: ["rename_node"],
      item_count: 1,
      affected_node_ids: ["n1"],
    });
  });

  it("reconciles unknown graph apply results by idempotency key", async () => {
    const unknownReasons: string[] = [];
    const client = {
      applyGraphChangeSet: async () => {
        throw new ProductFlowError(503, "timeout", "timeout");
      },
      reconcileTurnEffect: async () => ({ effect_result: "unknown", reconciliation_state: "unknown", detail: "副作用结果仍不明确" }),
    } as unknown as ProductFlowClient;
    const tool = createProductFlowTools(
      runtime({ ...baseScope, has_live_graph: true }, client, (_id, reason) => unknownReasons.push(reason ?? "")),
    ).find((candidate) => candidate.name === "apply_graph_change_set_v1");
    if (!tool) throw new Error("apply tool was not registered");
    await expect(
      tool.execute(
        "tool-apply-unknown",
        {
          base_graph_revision: 1,
          summary: "改名",
          operations: [{ op: "rename_node", node_ref: "n1", title: "新标题" }],
        },
        undefined,
        undefined,
        {} as never,
      ),
    ).rejects.toMatchObject({ code: "timeout" });
    expect(unknownReasons).toEqual(["Graph apply result is unknown"]);
  });

  it("treats a reconciled graph conflict as a failed effect", async () => {
    const unknownReasons: string[] = [];
    const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
    const client = {
      applyGraphChangeSet: async () => {
        throw new ProductFlowError(503, "timeout", "timeout");
      },
      reconcileTurnEffect: async () => ({ effect_result: "failed", reconciliation_state: "conflict", detail: "revision mismatch" }),
    } as unknown as ProductFlowClient;
    const tool = createProductFlowTools(
      runtime(
        { ...baseScope, has_live_graph: true },
        client,
        (_id, reason) => unknownReasons.push(reason ?? ""),
        async (kind, payload) => {
          checkpoints.push({ kind, payload });
        },
      ),
    ).find((candidate) => candidate.name === "apply_graph_change_set_v1");
    if (!tool) throw new Error("apply tool was not registered");
    await expect(
      tool.execute(
        "tool-apply-conflict",
        {
          base_graph_revision: 1,
          summary: "改名",
          operations: [{ op: "rename_node", node_ref: "n1", title: "新标题" }],
        },
        undefined,
        undefined,
        {} as never,
      ),
    ).rejects.toMatchObject({ code: "timeout" });
    expect(unknownReasons).toEqual([]);
    expect(checkpoints.at(-1)).toMatchObject({
      kind: "tool_effect_result",
      payload: { result: "failed", reconciliation_state: "conflict" },
    });
  });

  it("does not reconcile a ui_effect canvas focus after a 5xx", async () => {
    const calls: string[] = [];
    const client = {
      focusCanvasItems: async () => {
        calls.push("focus");
        throw new ProductFlowError(503, "timeout", "timeout");
      },
      reconcileTurnEffect: async () => {
        calls.push("reconcile");
        return { effect_result: "applied", reconciliation_state: "applied", result: { accepted: true } };
      },
    } as unknown as ProductFlowClient;
    const tool = createProductFlowTools(
      runtime({ ...baseScope, has_live_graph: true }, client),
    ).find((candidate) => candidate.name === "focus_canvas_items_v1");
    if (!tool) throw new Error("focus tool was not registered");
    await expect(
      tool.execute("tool-focus", { node_ids: ["n1"] }, undefined, undefined, {} as never),
    ).rejects.toMatchObject({ code: "timeout" });
    expect(calls).toEqual(["focus"]);
  });

  it("reads a bounded workflow run detail", async () => {
    const client = {
      workflowRunDetail: async () => ({
        run_id: "run-1",
        status: "failed",
        nodes: [{ node_id: "n1", status: "failed", failure_reason: "provider timeout" }],
      }),
    } as unknown as ProductFlowClient;
    const tool = createProductFlowTools(runtime(baseScope, client)).find(
      (candidate) => candidate.name === "get_workflow_run_detail_v1",
    );
    if (!tool) throw new Error("run detail tool was not registered");
    const result = await tool.execute("tool-run-detail", { run_id: "run-1" }, undefined, undefined, {} as never);
    const parsed = JSON.parse((result.content[0] as { text: string }).text) as { data: { status: string } };
    expect(parsed.data.status).toBe("failed");
  });

  it("writes tool_effect_intent schema v1 with complete bounded request_payload", async () => {
    const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
    const capture = async (kind: string, payload: Record<string, unknown>) => {
      checkpoints.push({ kind, payload });
    };
    const intake = createProductFlowTools(
      runtime(baseScope, {
        finalizeProductIntake: async () => ({ accepted: true }),
      } as unknown as ProductFlowClient, undefined, capture),
    ).find((candidate) => candidate.name === "finalize_product_intake_v1");
    const workspace = createProductFlowTools(
      runtime(
        { ...baseScope, scope_type: "global", product_id: null },
        { createProductWorkspace: async () => ({ product_id: "product-1" }) } as unknown as ProductFlowClient,
        undefined,
        capture,
      ),
    ).find((candidate) => candidate.name === "create_product_workspace_v1");
    const workflow = createProductFlowTools(
      runtime(baseScope, {
        prepareWorkflowRunRequest: async () => ({
          product_id: baseScope.product_id!,
          workflow_id: "workflow-1",
          workflow_title: "工作流",
          workflow_revision: 3,
          runnable_node_count: 2,
          task_id: "task-1",
          source_run_id: "run-9",
        }),
        executeWorkflowRunRequest: async () => ({ request_id: "request-1", status: "awaiting_confirmation" }),
      } as unknown as ProductFlowClient, undefined, capture),
    ).find((candidate) => candidate.name === "request_workflow_run_v1");
    const apply = createProductFlowTools(
      runtime({ ...baseScope, has_live_graph: true }, {
        applyGraphChangeSet: async () => ({ accepted: true, applied: true, revision: 2 }),
      } as unknown as ProductFlowClient, undefined, capture),
    ).find((candidate) => candidate.name === "apply_graph_change_set_v1");
    if (!intake || !workspace || !workflow || !apply) throw new Error("mutate tools were not registered");

    await intake.execute(
      "tool-intake-intent",
      {
        selection: { schema_version: 1, image_types: [{ key: "hero", quantity: 1, order: 0 }] },
        reference_asset_ids: ["asset-1"],
      },
      undefined,
      undefined,
      {} as never,
    );
    await workspace.execute("tool-workspace-intent", { name: "春季新品" }, undefined, undefined, {} as never);
    await workflow.execute(
      "tool-workflow-intent",
      {
        expected_workflow_revision: 3,
        scope: "nodes",
        node_ids: ["n1"],
        force: true,
        document_action: "rewrite",
      },
      undefined,
      undefined,
      {} as never,
    );
    await apply.execute(
      "tool-apply-intent",
      {
        base_graph_revision: 1,
        summary: "改名",
        operations: [{ op: "rename_node", node_ref: "n1", title: "新标题" }],
      },
      undefined,
      undefined,
      {} as never,
    );

    const intents = checkpoints.filter((entry) => entry.kind === "tool_effect_intent").map((entry) => entry.payload);
    expect(intents).toHaveLength(4);
    for (const intent of intents) {
      expect(Object.keys(intent).sort()).toEqual([
        "idempotency_key",
        "recovery_policy",
        "request_payload",
        "schema_version",
        "tool_call_id",
        "tool_name",
      ]);
      expect(intent.schema_version).toBe(1);
      expect(intent).not.toHaveProperty("request");
    }
    expect(intents[0]).toMatchObject({
      tool_name: "finalize_product_intake_v1",
      recovery_policy: "reconcile_then_retry",
      request_payload: {
        selection: { schema_version: 1, image_types: [{ key: "hero", quantity: 1, order: 0 }] },
        reference_asset_ids: ["asset-1"],
        task_id: null,
      },
    });
    expect(intents[1]).toMatchObject({
      tool_name: "create_product_workspace_v1",
      request_payload: { name: "春季新品" },
    });
    expect(intents[1]).not.toHaveProperty("name");
    expect(intents[2]).toMatchObject({
      tool_name: "request_workflow_run_v1",
      request_payload: {
        expected_workflow_revision: 3,
        workflow_id: "workflow-1",
        source_step_id: "tool-workflow-intent",
        task_id: "task-1",
        source_run_id: "run-9",
        scope: "nodes",
        node_id: null,
        node_ids: ["n1"],
        force: true,
        document_action: "rewrite",
      },
    });
    expect(intents[2].request_payload).not.toHaveProperty("product_id");
    expect(intents[3]).toMatchObject({
      tool_name: "apply_graph_change_set_v1",
      request_payload: {
        change_set: {
          base_graph_revision: 1,
          summary: "改名",
          operations: [{ op: "rename_node", node_ref: "n1", title: "新标题" }],
        },
      },
    });
  });
});
