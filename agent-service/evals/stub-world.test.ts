import { describe, expect, it } from "vitest";

import { loadGlobalDraftSchema } from "./json-schema.js";
import { loadEvalTaskSet } from "./loader.js";
import { createStubWorld, EVAL_WORKFLOW_ID, overlayEvalPageContext } from "./stub-world.js";

describe("L1 stub world", () => {
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
