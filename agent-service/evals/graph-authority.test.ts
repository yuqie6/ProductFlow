import { describe, expect, it } from "vitest";
import { loadEvalTaskSet } from "./loader.js";
import { openGoEvalHost } from "./go-world.js";
import { createStubWorld } from "./stub-world.js";
import type { JsonObject } from "../src/contracts.js";

describe.skipIf(process.env.PRODUCTFLOW_RUN_AGENT_EVALS_GOPG !== "1")("L1 Go graph observation", () => {
  it("observes dissolve and rejects retired config through the same host", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === "graph-editing-dissolve-and-reorder")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "graph" });
    try {
      const read = async () => await stub.client.productContext("conv", undefined, "detailed") as {
        live_graph: { revision: number; groups: Array<{ id: string }>; nodes: Array<{ id: string; title: string }> };
      };
      const before = await read();
      await stub.client.applyGraphChangeSet("conv", {
        base_graph_revision: before.live_graph.revision,
        summary: "dissolve only",
        operations: [{ op: "dissolve_group", group_ref: "group-main" }],
      }, "dissolve");
      const dissolved = await read();
      expect(dissolved.live_graph.revision).toBeGreaterThan(before.live_graph.revision);
      expect(dissolved.live_graph.groups.some((group) => group.id === "group-main")).toBe(false);
      expect(dissolved.live_graph.nodes.some((node) => node.id === "node-prompt-1")).toBe(true);

      const configTask = tasks.find((item) => item.id === "graph-editing-update-node-config")!;
      const configStub = createStubWorld(configTask, worlds.get(configTask.world)!, "conv2", "run", {});
      const configHost = await openGoEvalHost(configTask, configStub, { layer: "l1", overlay: "graph" });
      try {
        const context = await configStub.client.productContext("conv2", undefined, "detailed") as { live_graph: { revision: number } };
        await expect(configStub.client.applyGraphChangeSet("conv2", {
          base_graph_revision: context.live_graph.revision,
          summary: "wrong path",
          operations: [{ op: "update_node_config", node_ref: "node-prompt-1", config: { design_goal: "白底棚拍" } }],
        }, "illegal")).rejects.toMatchObject({ status: 400 });
        expect(configStub.calls.at(-1)?.outcome).toBe("failed");
        const afterIllegal = await configStub.client.productContext("conv2", undefined, "detailed") as { live_graph: { revision: number } };
        expect(afterIllegal.live_graph.revision).toBe(context.live_graph.revision);

        await configStub.client.applyGraphChangeSet("conv2", {
          base_graph_revision: context.live_graph.revision,
          summary: "legal nested",
          operations: [{
            op: "update_node_config",
            node_ref: "node-prompt-1",
            config: { image_type_key: "hero", prompt: { design_goal: "白底棚拍" } },
          }],
        }, "legal");
        const detail = await configStub.client.getNodeDetail("conv2", "node-prompt-1") as { config: Record<string, unknown> };
        expect(detail.config).not.toHaveProperty("design_goal");
        expect((detail.config.prompt as { design_goal: string }).design_goal).toBe("白底棚拍");
      } finally { await configHost.close(); }
    } finally { await host.close(); }
  }, 180_000);

  it("retries an injected 409 against the real current revision", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === "graph-editing-rename-node")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conversation", "run", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "graph" });
    try {
      const first = {
        base_graph_revision: 3,
        summary: "改名",
        operations: [{ op: "rename_node", node_ref: "node-prompt-1", title: "新标题" }],
      };
      await expect(stub.client.applyGraphChangeSet("conversation", first, "key-1")).rejects.toMatchObject({ status: 409 });
      const context = await stub.client.productContext("conversation", undefined, "concise") as { live_graph: { revision: number } };
      expect(context.live_graph.revision).toBeGreaterThan(0);
      const retry = { ...first, base_graph_revision: context.live_graph.revision };
      await expect(stub.client.applyGraphChangeSet("conversation", retry, "key-2")).resolves.toMatchObject({ applied: true });
      const after = await stub.client.productContext("conversation", undefined, "concise") as {
        live_graph: { revision: number; nodes: Array<{ id: string; title: string }> };
      };
      expect(after.live_graph.revision).toBeGreaterThan(context.live_graph.revision);
      expect(after.live_graph.nodes.find((node) => node.id === "node-prompt-1")?.title).toBe("新标题");
    } finally { await host.close(); }
  }, 180_000);

  it("does not accept a non-scripted illegal brief field via propose", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === "graph-editing-propose-scene-shot")!;
    const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", {});
    const host = await openGoEvalHost(task, stub, { layer: "l1", overlay: "graph" });
    try {
      const context = await stub.client.productContext("conv", undefined, "detailed") as { live_graph: { revision: number } };
      const params = {
        base_graph_revision: context.live_graph.revision,
        summary: "retired brief",
        operations: [{ op: "update_node_config", node_ref: "node-brief-1", config: { design_goals: ["x"] } }],
      } as JsonObject;
      await expect(stub.client.proposeGraphChangeSet("conv", params, "retired")).rejects.toMatchObject({ status: 409 });
      const after = await stub.client.productContext("conv", undefined, "detailed") as { live_graph: { revision: number } };
      expect(after.live_graph.revision).toBe(context.live_graph.revision);
      expect(stub.calls.filter((call) => call.name === "propose_graph_change_set_v1").at(-1)?.outcome).toBe("failed");
    } finally { await host.close(); }
  }, 180_000);
});
