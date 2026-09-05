import { describe, expect, it } from "vitest";

import { loadGlobalDraftSchema } from "./json-schema.js";
import { loadEvalTaskSet } from "./loader.js";
import { createStubWorld, EVAL_WORKFLOW_ID, overlayEvalPageContext } from "./stub-world.js";

describe("L1 stub world", () => {
  it("uses Go observations for intake expansion and does not invent unknown details", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === "product-intake-finalize-explicit-minimal-set")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", {});
    const before = await stub.client.productContext("conv", undefined, "detailed") as Record<string, unknown>;
    expect(before.image_type_catalog).toBeTruthy();
    expect((before.node_catalog as { nodes: unknown[] }).nodes.length).toBeGreaterThan(0);
    const params = task.reference.scripted_calls.at(-1)!.params as Parameters<typeof stub.client.finalizeProductIntake>[1];
    await stub.client.finalizeProductIntake("conv", params, "key");
    const after = await stub.client.productContext("conv", undefined, "detailed") as { intake: unknown; birth_expandable: boolean; live_graph: { nodes: unknown[]; edges: unknown[]; groups: unknown[] } };
    expect(after.birth_expandable).toBe(false);
    expect(after.intake).not.toBeNull();
    expect(after.live_graph.nodes.length).toBeGreaterThan(1);
    expect(after.live_graph.edges.length).toBeGreaterThan(0);
    expect(after.live_graph.groups.length).toBeGreaterThan(0);
    await expect(stub.client.getNodeDetail("conv", "missing")).rejects.toMatchObject({ status: 404 });
  });

  it.each(["graph-editing-delete-one-node", "graph-editing-disconnect-edge"])("observes legal structural effects for %s", async (id) => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === id)!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", {});
    await stub.client.applyGraphChangeSet("conv", task.reference.scripted_calls.at(-1)!.params as Parameters<typeof stub.client.applyGraphChangeSet>[1], "key");
    expect(stub.calls.at(-1)?.outcome).toBe("succeeded");
    const after = await stub.client.productContext("conv", undefined, "detailed") as { live_graph: { nodes: Array<{ id: string }>; edges: Array<{ id: string }> } };
    if (id.endsWith("delete-one-node")) expect(after.live_graph.nodes.some((node) => node.id === "node-image-2")).toBe(false);
    else expect(after.live_graph.edges.some((edge) => edge.id === "edge-brief-prompt")).toBe(false);
  });

  it("keeps missing structural observations unknown without inventing a graph effect", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === "graph-editing-dissolve-and-reorder")!;
    const world = worlds.get(task.world)!;
    const stub = createStubWorld(task, world, "conv", "run", {});
    const before = await stub.client.productContext("conv", undefined, "detailed");
    await expect(stub.client.applyGraphChangeSet("conv", {
      base_graph_revision: world.live_graph.revision,
      summary: "dissolve only",
      operations: [{ op: "dissolve_group", group_ref: "group-main" }],
    }, "key")).rejects.toMatchObject({ code: "eval_unobservable" });
    expect(stub.calls.at(-1)).toMatchObject({ name: "apply_graph_change_set_v1", outcome: "unknown" });
    expect(await stub.client.productContext("conv", undefined, "detailed")).toEqual(before);
  });

  it("requires explicit archived discovery and preserves Go before facts", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === "media-library-organization-restore-asset")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", {});
    const listed = await stub.client.listGlobalMediaAssets("conv", "", "", 20) as { items: unknown[] };
    expect(listed.items).toEqual([]);
    const archived = await stub.client.listGlobalMediaAssets("conv", "", "", 20, undefined, { include_archived: true }) as { items: unknown[] };
    expect(archived.items).toMatchObject([{ revision: 2, tag_names: ["归档"], is_archived: true }]);
    await expect(stub.client.inspectGlobalMediaAssets("conv", task.page_context.selected_asset_ids)).rejects.toMatchObject({ status: 404 });
  });
  it("records calls and advances the graph revision after an injected conflict", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "graph-editing-rename-node")!;
    const world = worlds.get(task.world)!;
    const stub = createStubWorld(task, world, "conversation", "run", {});

    const first = {
      base_graph_revision: 3,
      summary: "改名",
      operations: [{ op: "rename_node", node_ref: "node-prompt-1", title: "新标题" }],
    };
    await expect(stub.client.applyGraphChangeSet("conversation", first, "key-1")).rejects.toMatchObject({ status: 409 });
    const context = await stub.client.productContext("conversation", undefined, "concise") as { live_graph: { revision: number } };
    expect(context.live_graph.revision).toBe(4);
    await expect(stub.client.applyGraphChangeSet("conversation", { ...first, base_graph_revision: 4 }, "key-2"))
      .resolves.toMatchObject({ applied: true, revision: 5 });

    expect(stub.calls.filter((call) => call.name === "apply_graph_change_set_v1").map((call) => call.params))
      .toEqual([first, { ...first, base_graph_revision: 4 }]);
    expect(stub.calls.find((call) => call.name === "get_product_workflow_context_v1")?.params)
      .toEqual({ response_format: "concise" });
  });

  it("rejects stale asset revisions in global draft writes", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.skill === "media-library-organization")!;
    const world = worlds.get(task.world)!;
    const stub = createStubWorld(task, world, "conversation", "run", loadGlobalDraftSchema() as Record<string, unknown>);
    const draft = structuredClone(task.reference.scripted_calls.find((call) => call.name === "propose_global_draft")!.params) as {
      library_payload: { operations: Array<{ expected_revision: number }> };
    };
    draft.library_payload.operations[0].expected_revision = 99;
    await expect(stub.client.validateGlobalDraft("conversation", draft, undefined)).rejects.toMatchObject({ status: 409 });
  });

  it("exposes a running recent run to cancellation tasks", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "workflow-run-request-cancel-running-run")!;
    const world = worlds.get(task.world)!;
    const stub = createStubWorld(task, world, "conversation", "run", {});
    const runs = await stub.client.workflowRuns("conversation", 10) as { items: Array<{ status: string }> };
    expect(runs.items[0].status).toBe("running");
  });

  it("records execute-time workflow run params, defaulting omitted scope to graph", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "workflow-run-request-run-current-workflow")!;
    const world = worlds.get(task.world)!;
    const stub = createStubWorld(task, world, "conversation", "run", {});
    const prepared = await stub.client.prepareWorkflowRunRequest("conversation", {
      expected_workflow_revision: 3,
      task_id: null,
      source_run_id: null,
    });
    expect(stub.calls.filter((call) => call.name === "request_workflow_run_v1")).toEqual([]);
    await stub.client.executeWorkflowRunRequest("conversation", { ...prepared, force: true, document_action: "rewrite" }, "step", "key");
    expect(stub.calls.filter((call) => call.name === "request_workflow_run_v1").map((call) => call.params)).toEqual([
      { expected_workflow_revision: 3, source_run_id: null, scope: "graph", force: true, document_action: "rewrite" },
    ]);
  });

  it("overlays stale page_context revision onto the live world graph", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "workflow-run-request-run-current-workflow")!;
    const world = worlds.get(task.world)!;
    expect(task.page_context.workflow_revision).toBe(1);
    expect(overlayEvalPageContext(task, world)).toMatchObject({
      workflow_id: EVAL_WORKFLOW_ID,
      workflow_revision: 3,
    });
  });
});
