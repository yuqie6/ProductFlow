import { describe, expect, it } from "vitest";
import { loadEvalTaskSet } from "./loader.js";
import { checkJSONSchema, loadGlobalDraftSchema } from "./json-schema.js";
import { openGoEvalHost } from "./go-world.js";
import { createStubWorld, EVAL_ASSET_ID } from "./stub-world.js";
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
});
