import { describe, expect, it } from "vitest";
import { loadEvalTaskSet } from "./loader.js";
import { checkJSONSchema, loadGlobalDraftSchema } from "./json-schema.js";
import { openGoEvalHost } from "./go-world.js";
import { createStubWorld } from "./stub-world.js";
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
