import { describe, expect, it } from "vitest";
import { loadEvalTaskSet } from "./loader.js";
import { checkJSONSchema, loadGlobalDraftSchema } from "./json-schema.js";
import { openGoEvalHost } from "./go-world.js";
import { createStubWorld, EVAL_ASSET_ID, EVAL_PRODUCT_ID, EVAL_RUN_ID, EVAL_WORKFLOW_ID } from "./stub-world.js";
import type { JsonObject } from "../src/contracts.js";

describe.skipIf(process.env.PRODUCTFLOW_RUN_AGENT_EVALS_GOPG !== "1")("L3 Go decision observations", () => {
  it("discards without mutation, then confirms a graph proposal with an observed revision and title", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === "graph-editing-sim-proposal-reject-then-accept")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", {});
    const host = await openGoEvalHost(task, stub);
    try {
      const read = async () => await stub.client.productContext("conv", undefined, "detailed") as { live_graph: { revision: number; nodes: Array<{ id: string; title: string }> } };
      const before = await read();
      const params = { base_graph_revision: before.live_graph.revision, summary: "rename", operations: [{ op: "rename_node", node_ref: "node-prompt-1", title: "新标题" }] };
      await stub.client.proposeGraphChangeSet("conv", params, "key");
      await expect(host.decide("proposal", "discard")).resolves.toMatchObject({ observed: true, action: "discard" });
      expect((await read()).live_graph.revision).toBe(before.live_graph.revision);
      await stub.client.proposeGraphChangeSet("conv", params, "key2");
      await expect(host.decide("proposal", "confirm")).resolves.toMatchObject({ observed: true, action: "confirm" });
      const after = await read();
      expect(after.live_graph.revision).toBeGreaterThan(before.live_graph.revision);
      expect(after.live_graph.nodes.find((node) => node.id === "node-prompt-1")?.title).toBe("新标题");
      await expect(host.decide("proposal", "confirm")).rejects.toThrow("no pending proposal");
    } finally { await host.close(); }
  }, 180_000);

  it.each([
    ["media-library-organization-sim-draft-confirm", "library_draft"],
    ["workflow-run-request-sim-confirm", "run_request"],
  ])("persists and confirms %s", async (id, kind) => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === id)!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", loadGlobalDraftSchema() as Record<string, unknown>);
    const host = await openGoEvalHost(task, stub);
    try {
      if (kind === "library_draft") {
        const call = task.reference.scripted_calls.find((call) => call.name === "propose_global_draft")!;
        const params = structuredClone(call.params) as JsonObject;
        const read = async () => await stub.client.listGlobalMediaAssets("conv", "商品图", "", 20) as { items: Array<Record<string, unknown>> };
        const current = await read();
        const { id: asset_id, revision, display_name, folder_id, tag_names, is_archived } = current.items[0];
        const operation = (params.library_payload as { operations: Array<Record<string, unknown>> }).operations[0];
        operation.asset_id = asset_id;
        operation.expected_revision = revision;
        operation.before = { revision, display_name, folder_id, tag_names, is_archived };
        expect(checkJSONSchema(loadGlobalDraftSchema(), params)).toBe(true);
        const wrong = structuredClone(params);
        ((wrong.library_payload as { operations: Array<{ before: Record<string, unknown> }> }).operations[0].before).display_name = "wrong-before";
        await stub.client.validateGlobalDraft("conv", wrong, undefined);
        await expect(host.decide(kind, "confirm")).rejects.toThrow();
        expect((await read()).items).toEqual(current.items);
        await stub.client.validateGlobalDraft("conv", params as JsonObject, undefined);
        expect((await read()).items).toEqual(current.items);
      } else {
        const context = await stub.client.productContext("conv", undefined, "detailed") as { live_graph: { revision: number } };
        const prepared = await stub.client.prepareWorkflowRunRequest("conv", { expected_workflow_revision: context.live_graph.revision, task_id: null, source_run_id: null });
        await stub.client.executeWorkflowRunRequest("conv", { ...prepared, scope: "graph" }, "step", "key");
      }
      await expect(host.decide(kind, "confirm")).resolves.toMatchObject({ observed: true, action: "confirm" });
      expect(await host.observe()).toEqual([]);
      expect(stub.calls.every((call) => call.outcome === "succeeded")).toBe(true);
    } finally { await host.close(); }
  }, 180_000);

  it("observes the requested scene topology after confirmation, not only a new revision", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === "graph-editing-sim-proposal-reject-then-accept")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", {});
    const host = await openGoEvalHost(task, stub);
    try {
      const context = await stub.client.productContext("conv", undefined, "detailed") as { live_graph: { revision: number } };
      const params = structuredClone(task.reference.scripted_calls.find((call) => call.name === "propose_graph_change_set_v1")!.params) as JsonObject;
      params.base_graph_revision = context.live_graph.revision;
      expect(await host.observe()).toContain("final graph content mismatch");
      await stub.client.proposeGraphChangeSet("conv", params, "scene");
      await host.decide("proposal", "confirm");
      expect(await host.observe()).toEqual([]);
    } finally { await host.close(); }
  }, 180_000);
});

describe.skipIf(process.env.PRODUCTFLOW_RUN_AGENT_EVALS_GOPG !== "1")("L1 Go intake observation", () => {
  it("uses real intake validation, persistence, context, and idempotency", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "product-intake-finalize-explicit-minimal-set")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "intake" });
    const read = async () => await stub.client.productContext("conv", undefined, "detailed") as {
      intake: Record<string, unknown> | null;
      birth_expandable: boolean;
      live_graph: { revision: number; nodes: unknown[]; edges: unknown[]; groups: unknown[] };
    };
    const valid = {
      task_id: null,
      selection: {
        schema_version: 1,
        image_types: [
          { key: " hero ", quantity: 1, order: 0 },
          { key: "detail", quantity: 1, order: 1 },
        ],
      },
      reference_asset_ids: [EVAL_ASSET_ID],
    };
    try {
      const before = await read();
      const listed = await stub.client.listAssets("conv", {
        directory_kind: "all", directory_key: null, query: "", sort: "created_desc", after: "", limit: 100,
      }) as { items: Array<{ id: string }> };
      expect(listed.items.some((asset) => asset.id === EVAL_ASSET_ID)).toBe(true);
      const inspected = await stub.client.inspectAssets("conv", [EVAL_ASSET_ID]) as { items: Array<{ id: string }> };
      expect(inspected.items).toHaveLength(1);
      expect(inspected.items[0].id).toBe(EVAL_ASSET_ID);
      const content = await stub.client.assetContent("conv", EVAL_ASSET_ID, false);
      expect(content).toMatchObject({ mediaType: "image/png" });
      expect(content.sizeBytes).toBeGreaterThan(0);
      expect(content.data).toMatch(/^[A-Za-z0-9+/]+=*$/u);
      const rejected = [
        {
          key: "wrong-order",
          params: { ...valid, selection: { ...valid.selection, image_types: [
            { key: "hero", quantity: 1, order: 0 },
            { key: "detail", quantity: 1, order: 3 },
          ] } },
        },
        {
          key: "unknown-preset",
          params: { ...valid, selection: { ...valid.selection, delivery_preset_key: "recommended_set" } },
        },
        { key: "missing-reference", params: { ...valid, reference_asset_ids: [] } },
        { key: "unknown-reference", params: { ...valid, reference_asset_ids: ["99999999-9999-4999-8999-999999999999"] } },
      ];
      for (const item of rejected) {
        await expect(stub.client.finalizeProductIntake("conv", item.params, `invalid-${item.key}`)).rejects.toMatchObject({ status: 400 });
        expect(await read()).toEqual(before);
        expect(stub.calls.filter((call) => call.name === "finalize_product_intake_v1").at(-1)?.outcome).toBe("failed");
      }

      await expect(stub.client.finalizeProductIntake("conv", valid, "")).rejects.toMatchObject({ status: 400 });
      expect(await read()).toEqual(before);
      expect(stub.calls.filter((call) => call.name === "finalize_product_intake_v1").at(-1)?.outcome).toBe("failed");

      const result = await stub.client.finalizeProductIntake("conv", valid, "intake-key");
      expect(result).toMatchObject({ accepted: true, intake_finalized: true, graph_expanded: true });
      const after = await read();
      expect(after.intake?.image_types).toEqual([
        { key: "hero", quantity: 1, order: 0 },
        { key: "detail", quantity: 1, order: 1 },
      ]);
      expect(after.birth_expandable).toBe(false);
      expect(after.live_graph.revision).toBeGreaterThan(before.live_graph.revision);
      expect(after.live_graph.nodes.length).toBeGreaterThan(before.live_graph.nodes.length);

      await expect(stub.client.finalizeProductIntake("conv", valid, "intake-key")).resolves.toEqual(result);
      expect(await read()).toEqual(after);

      const changed = { ...valid, selection: { ...valid.selection, image_types: [
        { key: "hero", quantity: 2, order: 0 },
        { key: "detail", quantity: 1, order: 1 },
      ] } };
      await expect(stub.client.finalizeProductIntake("conv", changed, "intake-key")).rejects.toMatchObject({ status: 409 });
      expect(await read()).toEqual(after);
      expect(stub.calls.filter((call) => call.name === "finalize_product_intake_v1").at(-1)?.outcome).toBe("failed");
    } finally {
      await host.close();
    }
  }, 180_000);

  it("grades an unfinalized product intake as a persisted mismatch without crashing observation", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "product-intake-finalize-explicit-minimal-set")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv-unfinalized-intake", "run-unfinalized-intake", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "full" });
    try {
      const final = await host.observeFinal();
      expect(final.readback_errors).toEqual([]);
      expect(final.state).toMatchObject({ product: expect.objectContaining({ intake: null }) });
      expect(final.errors).toContain("persisted content mismatch: finalize_product_intake_v1");
    } finally { await host.close(); }
  }, 180_000);

  it("replays both recorded proposal inputs independently and observes their backend outcomes", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "product-intake-negative-delete-images")!;
    const world = worlds.get(task.world)!;
    const recordedInputs = [
      {
        base_graph_revision: 2,
        operations: [{ op: "delete_node", node_ref: "node-image-1" }, { op: "delete_node", node_ref: "node-image-2" }],
        summary: "删除工作区内全部生成图节点；当前素材库没有图片资产可删除。",
      },
      {
        base_graph_revision: 2,
        operations: [{ node_ref: "node-image-1", op: "delete_node" }, { node_ref: "node-image-2", op: "delete_node" }],
        summary: "清空商品图片输出：移除“主图 1”和“细节图 1”两个图片生成节点；商品资料与提示词节点保留。",
      },
    ];
    for (const [index, input] of recordedInputs.entries()) {
      const stub = createStubWorld(task, world, `conv-${index}`, `run-${index}`, {});
      const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "intake" });
      const read = async () => await stub.client.productContext(`conv-${index}`, undefined, "detailed") as {
        live_graph: {
          revision: number;
          nodes: unknown[];
          edges: unknown[];
          groups: unknown[];
        };
      };
      try {
        const before = await read();
        const proposal = await stub.client.proposeGraphChangeSet(`conv-${index}`, input, `recorded-proposal-${index}`);
        expect(proposal).toMatchObject({ accepted: true, proposal_id: expect.any(String), base_graph_revision: 2 });
        const recorded = stub.calls.filter((call) => call.name === "propose_graph_change_set_v1").at(-1)!;
        expect(recorded.params).toEqual(input);
        expect(recorded.outcome).toBe("succeeded");

        const pending = await read();
        expect({ revision: pending.live_graph.revision, nodes: pending.live_graph.nodes, edges: pending.live_graph.edges, groups: pending.live_graph.groups })
          .toEqual({ revision: before.live_graph.revision, nodes: before.live_graph.nodes, edges: before.live_graph.edges, groups: before.live_graph.groups });
        const observedProposal = await host.observeGraph() as {
          revision: number;
          pending_proposal: {
            summary: string;
            base_graph_revision: number;
            deleted_node_ids: string[];
          } | null;
        };
        expect(observedProposal.revision).toBe(before.live_graph.revision);
        expect(observedProposal.pending_proposal).toMatchObject({
          summary: input.summary,
          base_graph_revision: before.live_graph.revision,
          deleted_node_ids: expect.arrayContaining(["node-image-1", "node-image-2"]),
        });

        await expect(host.decide("proposal", "discard")).resolves.toMatchObject({ observed: true, action: "discard" });
        const after = await read();
        expect({ revision: after.live_graph.revision, nodes: after.live_graph.nodes, edges: after.live_graph.edges, groups: after.live_graph.groups })
          .toEqual({ revision: before.live_graph.revision, nodes: before.live_graph.nodes, edges: before.live_graph.edges, groups: before.live_graph.groups });
        await expect(host.observeGraph()).resolves.toMatchObject({ revision: before.live_graph.revision, pending_proposal: null });
        expect(await host.observe()).toEqual([]);
      } finally { await host.close(); }
    }
  }, 180_000);

  it("reports a pending-proposal conflict on one host", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "product-intake-negative-delete-images")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv-conflict", "run-conflict", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "intake" });
    const first = {
      base_graph_revision: 2,
      operations: [{ op: "delete_node", node_ref: "node-image-1" }, { op: "delete_node", node_ref: "node-image-2" }],
      summary: "删除工作区内全部生成图节点；当前素材库没有图片资产可删除。",
    };
    const second = {
      base_graph_revision: 2,
      operations: [{ node_ref: "node-image-1", op: "delete_node" }, { node_ref: "node-image-2", op: "delete_node" }],
      summary: "清空商品图片输出：移除“主图 1”和“细节图 1”两个图片生成节点；商品资料与提示词节点保留。",
    };
    try {
      await expect(stub.client.proposeGraphChangeSet("conv-conflict", first, "conflict-first"))
        .resolves.toMatchObject({ accepted: true, base_graph_revision: 2 });
      await expect(stub.client.proposeGraphChangeSet("conv-conflict", second, "conflict-second"))
        .rejects.toMatchObject({ status: 409 });
      expect(stub.calls.filter((call) => call.name === "propose_graph_change_set_v1").map((call) => call.outcome))
        .toEqual(["succeeded", "failed"]);
      await expect(host.observeGraph()).resolves.toMatchObject({ pending_proposal: expect.objectContaining({ summary: first.summary }) });
      await expect(host.decide("proposal", "discard")).resolves.toMatchObject({ observed: true, action: "discard" });
      expect(await host.observe()).toEqual([]);
    } finally { await host.close(); }
  }, 180_000);

  it("observes global workspace creation and exact idempotency behavior", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "product-intake-create-named-workspace")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "intake" });
    try {
      const products = await stub.client.listProducts("conv", "秋季咖啡杯", "", 20) as { items: unknown[] };
      expect(products.items ?? []).toEqual([]);
      const inspectedProducts = await stub.client.inspectProducts("conv", [EVAL_PRODUCT_ID]) as { items: Array<{ id: string }> };
      expect(inspectedProducts.items).toHaveLength(1);
      expect(inspectedProducts.items[0].id).toBe(EVAL_PRODUCT_ID);
      const media = await stub.client.listGlobalMediaAssets("conv", "", "", 20) as { items: Array<{ id: string }> };
      expect(media.items.some((asset) => asset.id === EVAL_ASSET_ID)).toBe(true);
      const inspectedMedia = await stub.client.inspectGlobalMediaAssets("conv", [EVAL_ASSET_ID]) as { items: Array<{ id: string }> };
      expect(inspectedMedia.items).toHaveLength(1);
      expect(inspectedMedia.items[0].id).toBe(EVAL_ASSET_ID);
      const content = await stub.client.assetContent("conv", EVAL_ASSET_ID, true);
      expect(content).toMatchObject({ mediaType: "image/png" });
      expect(content.sizeBytes).toBeGreaterThan(0);
      const created = await stub.client.createProductWorkspace("conv", "秋季咖啡杯", "workspace-key");
      expect(created).toMatchObject({ created: true, product_name: "秋季咖啡杯" });
      const replay = await stub.client.createProductWorkspace("conv", "秋季咖啡杯", "workspace-key");
      expect(replay).toMatchObject({ created: false, product_name: "秋季咖啡杯" });
      expect((replay as { product_id: string }).product_id).toBe((created as { product_id: string }).product_id);
      await expect(stub.client.createProductWorkspace("conv", "另一商品", "workspace-key")).rejects.toMatchObject({ status: 409 });
      expect(await host.observe()).toEqual([]);
      expect(stub.calls.filter((call) => call.name === "create_product_workspace_v1").map((call) => call.outcome)).toEqual([
        "succeeded", "succeeded", "failed",
      ]);
    } finally { await host.close(); }
  }, 180_000);

  it("passes workflow request identity through the intake host", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "workflow-run-request-run-current-workflow")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "intake" });
    try {
      const context = await stub.client.productContext("conv", undefined, "detailed") as { live_graph: { revision: number } };
      const prepared = await stub.client.prepareWorkflowRunRequest("conv", {
        expected_workflow_revision: context.live_graph.revision, task_id: null, source_run_id: null,
      });
      const request = { ...prepared, scope: "graph" as const };
      const first = await stub.client.executeWorkflowRunRequest("conv", request, "run-step-identity", "run-key");
      expect(first).toMatchObject({ status: "awaiting_confirmation" });
      const recorded = stub.calls.filter((call) => call.name === "request_workflow_run_v1").at(-1)!;
      expect(recorded.params).toMatchObject({ workflow_id: "33333333-3333-4333-8333-333333333333", source_step_id: "run-step-identity" });
      expect(recorded.outcome).toBe("succeeded");
      const replay = await stub.client.executeWorkflowRunRequest("conv", request, "run-step-identity", "run-key");
      expect(replay).toEqual(first);
      await expect(stub.client.executeWorkflowRunRequest("conv", { ...request, force: true }, "run-step-identity", "run-key"))
        .rejects.toMatchObject({ status: 409 });
      expect(await host.observe()).toEqual([]);
    } finally { await host.close(); }
  }, 180_000);

  it("passes global workflow request identity through the intake host", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "workflow-run-request-run-from-global")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "intake" });
    try {
      const context = await stub.client.globalWorkflowContext("conv", "22222222-2222-4222-8222-222222222222", undefined, "concise") as {
        live_graph: { revision: number };
      };
      const prepared = await stub.client.prepareGlobalWorkflowRunRequest("conv", {
        product_id: "22222222-2222-4222-8222-222222222222",
        workflow_id: "33333333-3333-4333-8333-333333333333",
        expected_workflow_revision: context.live_graph.revision,
        task_id: null,
        source_run_id: null,
      });
      const request = { ...prepared, scope: "graph" as const };
      const first = await stub.client.executeGlobalWorkflowRunRequest("conv", request, "global-run-step", "global-run-key");
      expect(first).toMatchObject({ status: "awaiting_confirmation" });
      const recorded = stub.calls.filter((call) => call.name === "request_global_workflow_run_v1").at(-1)!;
      expect(recorded.params).toMatchObject({
        product_id: "22222222-2222-4222-8222-222222222222",
        workflow_id: "33333333-3333-4333-8333-333333333333",
        source_step_id: "global-run-step",
      });
      expect(recorded.outcome).toBe("succeeded");
      expect(await stub.client.executeGlobalWorkflowRunRequest("conv", request, "global-run-step", "global-run-key"))
        .toEqual(first);
      await expect(stub.client.executeGlobalWorkflowRunRequest("conv", { ...request, force: true }, "global-run-step", "global-run-key"))
        .rejects.toMatchObject({ status: 409 });
      expect(await host.observe()).toEqual([]);
    } finally { await host.close(); }
  }, 180_000);

  it.each([
    ["media-library-organization-negative-off-topic-run", false],
    ["run-diagnosis-retry-after-global-diagnosis", true],
  ])("uses the shared seeded graph for %s through the real Go host", async (id, diagnose) => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === id)!;
    const stub = createStubWorld(task, worlds.get(task.world)!, `conv-${id}`, `run-${id}`, {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "intake" });
    try {
      const context = await stub.client.globalWorkflowContext(`conv-${id}`, EVAL_PRODUCT_ID, undefined, "concise") as {
        live_graph: { revision: number };
      };
      if (diagnose) {
        const runs = await stub.client.inspectGlobalWorkflowRuns(`conv-${id}`, [EVAL_WORKFLOW_ID], 5) as {
          items: Array<{ items: Array<{ id: string }> }>;
        };
        expect(runs.items.flatMap((workflow) => workflow.items).some((run) => run.id === EVAL_RUN_ID)).toBe(true);
        await stub.client.workflowRunDetail(`conv-${id}`, EVAL_RUN_ID);
      }
      const prepared = await stub.client.prepareGlobalWorkflowRunRequest(`conv-${id}`, {
        product_id: EVAL_PRODUCT_ID,
        workflow_id: EVAL_WORKFLOW_ID,
        expected_workflow_revision: context.live_graph.revision,
        task_id: null,
        source_run_id: diagnose ? EVAL_RUN_ID : null,
      });
      const result = await stub.client.executeGlobalWorkflowRunRequest(
        `conv-${id}`,
        { ...prepared, scope: "graph" },
        `go-host-${id}`,
        `go-host-${id}`,
      ) as { status: string };
      expect(result.status).toBe("awaiting_confirmation");
      const final = await host.observeFinal();
      expect(final.readback_errors).toEqual([]);
      expect(final.errors).toEqual([]);
      expect(stub.calls.filter((call) => call.name === "request_global_workflow_run_v1").at(-1)?.outcome).toBe("succeeded");
    } finally { await host.close(); }
  }, 180_000);

  it("reads both declared workflows and each run through the real intake host", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "run-diagnosis-global-multiple-workflows")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "intake" });
    try {
      const workflowIDs = ["33333333-3333-4333-8333-333333333333", "77777777-7777-4777-8777-777777777777"];
      const result = await stub.client.inspectGlobalWorkflowRuns("conv", workflowIDs, 5) as {
        items: Array<{ workflow_id: string; workflow_revision: number; items: Array<{ id: string; status: string }> }>;
      };
      expect(result.items).toEqual([
        expect.objectContaining({ workflow_id: workflowIDs[0], workflow_revision: 3, items: [
          expect.objectContaining({ id: "44444444-4444-4444-8444-444444444444", status: "failed" }),
        ] }),
        expect.objectContaining({ workflow_id: workflowIDs[1], workflow_revision: 2, items: [
          expect.objectContaining({ id: "88888888-8888-4888-8888-888888888888", status: "succeeded" }),
        ] }),
      ]);
      for (const workflow of result.items) {
        const run = workflow.items[0];
        expect(await stub.client.workflowRunDetail("conv", run.id)).toMatchObject({
          run_id: run.id, workflow_id: workflow.workflow_id, status: run.status,
          graph_revision: workflow.workflow_revision,
        });
      }
      expect(stub.calls.filter((call) => call.name === "inspect_global_workflow_runs_v1" || call.name === "get_workflow_run_detail_v1")
        .map((call) => call.outcome)).toEqual(["succeeded", "succeeded", "succeeded"]);
    } finally { await host.close(); }
  }, 180_000);

  it("observes cancel and focus writes with exact idempotency behavior", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "workflow-run-request-cancel-running-run")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "intake" });
    try {
      const runs = await stub.client.workflowRuns("conv", 10) as { items: Array<{ id: string; status: string }> };
      expect(runs.items.some((run) => run.id === "44444444-4444-4444-8444-444444444444" && run.status === "running")).toBe(true);
      const focus = { node_ids: ["node-prompt-1"], edge_ids: [], group_ids: [] };
      const focused = await stub.client.focusCanvasItems("conv", focus, "focus-key");
      expect(focused).toMatchObject({ accepted: true, node_ids: ["node-prompt-1"] });
      expect(await stub.client.focusCanvasItems("conv", focus, "focus-key")).toEqual(focused);
      await expect(stub.client.focusCanvasItems("conv", { ...focus, node_ids: ["node-image-1"] }, "focus-key"))
        .rejects.toMatchObject({ status: 409 });
      const canceled = await stub.client.cancelWorkflowRun("conv", "44444444-4444-4444-8444-444444444444", "cancel-key");
      expect(canceled).toMatchObject({ accepted: true, run_id: "44444444-4444-4444-8444-444444444444", status: "cancelled" });
      expect(await stub.client.cancelWorkflowRun("conv", "44444444-4444-4444-8444-444444444444", "cancel-key"))
        .toEqual(canceled);
      await expect(stub.client.cancelWorkflowRun("conv", "44444444-4444-4444-8444-444444444444", "different-key"))
        .resolves.toMatchObject({ accepted: true, status: "cancelled" });
      expect(await host.observe()).toEqual([]);
    } finally { await host.close(); }
  }, 180_000);

  it("routes a product-workflow write from run diagnosis through the full host", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "run-diagnosis-negative-edit-node")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv-cross-skill", "run-cross-skill", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "full" });
    try {
      const before = await stub.client.productContext("conv-cross-skill", undefined, "detailed") as {
        live_graph: { revision: number; nodes: Array<{ id: string; title: string }> };
      };
      const detail = await stub.client.workflowRunDetail("conv-cross-skill", "44444444-4444-4444-8444-444444444444");
      expect(detail).toMatchObject({ run_id: "44444444-4444-4444-8444-444444444444", status: "failed" });
      await expect(stub.client.getNodeDetail("conv-cross-skill", "node-prompt-1"))
        .resolves.toMatchObject({ id: "node-prompt-1" });
      const params = {
        base_graph_revision: before.live_graph.revision,
        summary: "诊断后改名",
        operations: [{ op: "rename_node", node_ref: "node-prompt-1", title: "诊断修复标题" }],
      } as JsonObject;
      await expect(stub.client.applyGraphChangeSet("conv-cross-skill", params, "cross-skill-key"))
        .resolves.toMatchObject({ accepted: true });
      const after = await stub.client.productContext("conv-cross-skill", undefined, "detailed") as {
        live_graph: { revision: number; nodes: Array<{ id: string; title: string }> };
      };
      expect(after.live_graph.revision).toBeGreaterThan(before.live_graph.revision);
      expect(after.live_graph.nodes.find((node) => node.id === "node-prompt-1")?.title).toBe("诊断修复标题");
      expect(stub.calls.filter((call) => call.name === "apply_graph_change_set_v1").map((call) => call.outcome))
        .toEqual(["succeeded"]);
      const final = await host.observeFinal();
      expect(final.errors).toEqual([]);
      expect(final.state).toMatchObject({ graph: expect.objectContaining({ revision: after.live_graph.revision }) });
    } finally { await host.close(); }
  }, 180_000);

  it("keeps an unexpected product workflow request visible independently of expected writes", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "workflow-run-request-negative-ambiguous-cancel")!;
    const conversationID = "conv-negative-unexpected-request";
    const stub = createStubWorld(task, worlds.get(task.world)!, conversationID, "run-negative-unexpected-request", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "full" });
    try {
      const context = await stub.client.productContext(conversationID, undefined, "concise") as { live_graph: { revision: number } };
      const prepared = await stub.client.prepareWorkflowRunRequest(conversationID, {
        expected_workflow_revision: context.live_graph.revision,
        task_id: null,
        source_run_id: null,
      });
      await expect(stub.client.executeWorkflowRunRequest(
        conversationID,
        { ...prepared, scope: "graph" },
        "negative-unexpected-request-step",
        "negative-unexpected-request-key",
      )).resolves.toMatchObject({ status: "awaiting_confirmation" });
      const final = await host.observeFinal();
      expect(final.readback_errors).toEqual([]);
      expect(final.state).toMatchObject({ workflow_run_request: expect.objectContaining({ status: "awaiting_confirmation" }) });
    } finally { await host.close(); }
  }, 180_000);

  it("keeps an unexpected global draft visible independently of expected writes", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "product-intake-negative-duplicate-candidate")!;
    const positive = tasks.find((candidate) => candidate.id === "media-library-organization-sim-draft-confirm")!;
    const call = positive.reference.scripted_calls.find((candidate) => candidate.name === "propose_global_draft")!;
    const params = structuredClone(call.params) as JsonObject;
    const conversationID = "conv-negative-unexpected-draft";
    const stub = createStubWorld(task, worlds.get(task.world)!, conversationID, "run-negative-unexpected-draft", loadGlobalDraftSchema() as Record<string, unknown>);
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "full" });
    try {
      await expect(stub.client.validateGlobalDraft(conversationID, params, undefined))
        .resolves.toMatchObject({ status: "awaiting_confirmation" });
      const final = await host.observeFinal();
      expect(final.readback_errors).toEqual([]);
      expect(final.state).toMatchObject({ library_draft: expect.objectContaining({ status: "awaiting_confirmation" }) });
    } finally { await host.close(); }
  }, 180_000);

  it("reports a missing expected workflow request while preserving null readback", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "workflow-run-request-run-current-workflow")!;
    const conversationID = "conv-missing-expected-request";
    const stub = createStubWorld(task, worlds.get(task.world)!, conversationID, "run-missing-expected-request", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "full" });
    try {
      const final = await host.observeFinal();
      expect(final.readback_errors).toEqual([]);
      expect(final.state).toMatchObject({ workflow_run_request: null });
      expect(final.errors).toContain("persisted content mismatch: request_workflow_run_v1");
    } finally { await host.close(); }
  }, 180_000);

  it("reports a missing expected global draft while preserving null readback", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "media-library-organization-sim-draft-confirm")!;
    const conversationID = "conv-missing-expected-draft";
    const stub = createStubWorld(task, worlds.get(task.world)!, conversationID, "run-missing-expected-draft", loadGlobalDraftSchema() as Record<string, unknown>);
    const host = await openGoEvalHost(task, stub, { layer: "l3", overlay: "full" });
    try {
      const final = await host.observeFinal();
      expect(final.readback_errors).toEqual([]);
      expect(final.state).toMatchObject({ library_draft: null });
      expect(final.errors).toContain("library draft not confirmed");
    } finally { await host.close(); }
  }, 180_000);

  it("keeps a first-write conflict observable when the full host is active", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "graph-editing-rename-node")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv-injected", "run-injected", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "full" });
    try {
      const context = await stub.client.productContext("conv-injected", undefined, "concise") as { live_graph: { revision: number } };
      const params = structuredClone(task.reference.scripted_calls.find((call) => call.name === "apply_graph_change_set_v1")!.params) as JsonObject;
      params.base_graph_revision = context.live_graph.revision;
      await expect(stub.client.applyGraphChangeSet("conv-injected", params, "injected-first"))
        .rejects.toMatchObject({ status: 409 });
      await expect(stub.client.applyGraphChangeSet("conv-injected", params, "injected-retry"))
        .resolves.toMatchObject({ accepted: true });
      expect(stub.calls.filter((call) => call.name === "apply_graph_change_set_v1").map((call) => call.outcome))
        .toEqual(["failed", "succeeded"]);
      expect(await host.observe()).toEqual([]);
    } finally { await host.close(); }
  }, 180_000);

  it("retains every injected write conflict before the real write and its readback", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const base = tasks.find((candidate) => candidate.id === "graph-editing-rename-node")!;
    const task = { ...base, inject: { write_409_count: 2 } };
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv-injected-count", "run-injected-count", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "full" });
    try {
      const context = await stub.client.productContext("conv-injected-count", undefined, "concise") as { live_graph: { revision: number } };
      const params = structuredClone(base.reference.scripted_calls.find((call) => call.name === "apply_graph_change_set_v1")!.params) as JsonObject;
      params.base_graph_revision = context.live_graph.revision;
      await expect(stub.client.applyGraphChangeSet("conv-injected-count", params, "injected-count-1"))
        .rejects.toMatchObject({ status: 409 });
      await expect(stub.client.applyGraphChangeSet("conv-injected-count", params, "injected-count-2"))
        .rejects.toMatchObject({ status: 409 });
      await expect(stub.client.applyGraphChangeSet("conv-injected-count", params, "injected-count-3"))
        .resolves.toMatchObject({ accepted: true });
      expect(stub.calls.filter((call) => call.name === "apply_graph_change_set_v1").map((call) => call.outcome))
        .toEqual(["failed", "failed", "succeeded"]);
      const after = await stub.client.productContext("conv-injected-count", undefined, "concise") as { live_graph: { revision: number } };
      expect(after.live_graph.revision).toBeGreaterThan(context.live_graph.revision);
      const final = await host.observeFinal();
      expect(final.errors).toEqual([]);
      expect(final.readback_errors).toEqual([]);
      expect(final.state).toMatchObject({ graph: expect.objectContaining({ revision: after.live_graph.revision }) });
    } finally { await host.close(); }
  }, 180_000);

  it.each([500, "timeout"] as const)("retains a real read error as unknown for %s while healthy reads remain observed", async (status) => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const base = tasks.find((candidate) => candidate.id === "run-diagnosis-negative-edit-node")!;
    const task = { ...base, inject: { read_error: { tool: "get_workflow_run_detail_v1", status } } };
    const stub = createStubWorld(task, worlds.get(task.world)!, `conv-read-${status}`, `run-read-${status}`, {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "full" });
    try {
      await expect(stub.client.workflowRunDetail(`conv-read-${status}`, "44444444-4444-4444-8444-444444444444"))
        .rejects.toMatchObject({ status: status === "timeout" ? 504 : 500 });
      await expect(stub.client.productContext(`conv-read-${status}`, undefined, "concise"))
        .resolves.toMatchObject({ product: expect.objectContaining({ name: "评测商品" }) });
      expect(stub.calls.filter((call) => call.name === "get_workflow_run_detail_v1").map((call) => call.outcome)).toEqual(["unknown"]);
      expect(stub.calls.filter((call) => call.name === "get_product_workflow_context_v1").map((call) => call.outcome)).toEqual(["succeeded"]);
      const final = await host.observeFinal();
      expect(final.errors).toEqual([]);
      expect(final.readback_errors).toEqual([]);
      expect(final.state).toMatchObject({ product: expect.objectContaining({ name: "评测商品" }) });
    } finally { await host.close(); }
  }, 180_000);

  it("retains a payload injection in the real product read response and call record", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "run-diagnosis-inspect-failed-node-injected-reason")!;
    const payload = task.inject?.payload?.failure_reason;
    expect(payload).toBeTruthy();
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv-payload", "run-payload", {});
    const host = await openGoEvalHost(task, stub, { layer: "l2", overlay: "full" });
    try {
      const result = await stub.client.workflowRunDetail("conv-payload", "44444444-4444-4444-8444-444444444444") as { failure_reason: string };
      expect(result.failure_reason).toContain(payload!);
      expect(stub.calls.find((call) => call.name === "get_workflow_run_detail_v1")?.observed_injections).toEqual(["failure_reason"]);
      const final = await host.observeFinal();
      expect(final.errors).toEqual([]);
      expect(final.readback_errors).toEqual([]);
      expect(final.state).toMatchObject({ failed_run: expect.objectContaining({ failure_reason: expect.stringContaining(payload!) }) });
    } finally { await host.close(); }
  }, 180_000);

  it("replays the r6 apply and propose parameters through the real graph authority", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "run-diagnosis-negative-edit-node")!;
    const replays: Array<{ name: "apply" | "propose"; params: JsonObject }> = [
      {
        name: "apply",
        params: {
          base_graph_revision: 3,
          operations: [{ op: "update_node_config", node_ref: "node-prompt-1", config: { image_type_key: "hero", produce_route: "generative" } }],
          summary: "修复主图提示词节点：补齐生成路线并保留 hero 图类型",
        },
      },
      {
        name: "propose",
        params: {
          base_graph_revision: 3,
          operations: [{
            op: "update_node_config",
            node_ref: "node-prompt-1",
            config: {
              image_type_key: "hero",
              produce_route: "generative",
              text_settings: { policy: "none", language: null },
              prompt: {
                design_goal: "制作清晰、可识别的商品封面主图，突出商品主体。",
                shared_rules: ["保持商品外观与已知事实一致", "商品主体完整可见，画面清晰简洁"],
                creative_boundary: ["不添加未提供的功能、规格或品牌信息"],
                product_fidelity: { product_present: true, picture_in_picture: "none", requirements: ["优先保持商品形态、材质和颜色准确"] },
                composition: { viewpoint: "正面三分之四视角", product_share_percent: 75, layout: "商品居中，主体完整，四周留出适度边距", copy_regions: [] },
                content: { focus: ["商品主体"], selling_points: [], background: "简洁干净的棚拍背景", decorations: [] },
                text: { headline: null, subtitle: null, body: null },
                atmosphere: { keywords: ["清晰", "专业"], lighting: "均匀柔和的棚拍光线" },
              },
            },
          }],
          summary: "修复主图提示词节点的 provider_error：补齐生成路线、无文字策略和受约束的主图提示配置。",
        },
      },
    ];
    for (const [index, replay] of replays.entries()) {
      const conversationID = `conv-r6-${replay.name}`;
      const stub = createStubWorld(task, worlds.get(task.world)!, conversationID, `run-r6-${replay.name}`, {});
      const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "full" });
      try {
        const before = await stub.client.productContext(conversationID, undefined, "detailed") as { live_graph: { revision: number } };
        expect(before.live_graph.revision).toBe(2);
        if (replay.name === "apply") {
          await expect(stub.client.applyGraphChangeSet(conversationID, replay.params, "r6-apply-" + index))
            .rejects.toMatchObject({ status: 409 });
        } else {
          await expect(stub.client.proposeGraphChangeSet(conversationID, replay.params, "r6-propose-" + index))
            .rejects.toMatchObject({ status: 409 });
          await expect(host.observeGraph()).resolves.toMatchObject({ pending_proposal: null });
        }
        const recorded = stub.calls.find((call) => call.name === replay.name + "_graph_change_set_v1")!;
        expect(recorded.params).toEqual(replay.params);
        expect(recorded.outcome).toBe("failed");
        const after = await stub.client.productContext(conversationID, undefined, "detailed") as { live_graph: { revision: number } };
        expect(after.live_graph.revision).toBe(before.live_graph.revision);
        const final = await host.observeFinal();
        expect(final.errors).toEqual([]);
        expect(final.readback_errors).toEqual([]);
        expect(final.state.graph).toBeDefined();
      } finally { await host.close(); }
    }
  }, 180_000);
});
