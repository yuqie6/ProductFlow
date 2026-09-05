import { describe, expect, it } from "vitest";
import { loadEvalTaskSet } from "./loader.js";
import { createStubWorld } from "./stub-world.js";
import { libraryObservation, type LibraryObservation } from "./library-observation.js";
import { checkJSONSchema, loadGlobalDraftSchema } from "./json-schema.js";
import { gradeWrites } from "./graders/index.js";
import type { JsonObject } from "../src/contracts.js";
import { buildAdversarialTasks } from "./injections.js";

describe("library observations", () => {
  it("backs every reference operation with reachable Go facts and rejects wrong before/revision", async () => {
    const { tasks, worlds } = await loadEvalTaskSet();
    let checked = 0;
    for (const task of tasks.filter((task) => task.skill === "media-library-organization" && task.scope === "global")) {
      const draft = task.reference.scripted_calls.find((call) => call.name === "propose_global_draft");
      if (!draft) continue;
      checked++;
      const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", loadGlobalDraftSchema() as Record<string, unknown>);
      const assets: LibraryObservation["items"] = [];
      const folders: LibraryObservation["folders"] = [];
      let workflow: LibraryObservation["workflow"];
      for (const call of task.reference.scripted_calls) {
        const p = call.params as Record<string, any>;
        if (call.name === "list_global_media_library_assets_v1") {
          const page = await stub.client.listGlobalMediaAssets("conv", p.query ?? "", p.cursor ?? "", p.limit, undefined, p) as LibraryObservation;
          assets.push(...page.items); folders.push(...page.folders); workflow = page.workflow;
        }
        if (call.name === "inspect_global_media_library_assets_v1") {
          const page = await stub.client.inspectGlobalMediaAssets("conv", p.asset_ids) as { items: LibraryObservation["items"] };
          assets.push(...page.items);
        }
      }
      const params = structuredClone(draft.params) as Record<string, any>;
      expect(checkJSONSchema(loadGlobalDraftSchema(), params), task.id).toBe(true);
      for (const op of params.library_payload.operations) {
        const asset = assets.find((asset) => asset.id === op.asset_id)!;
        expect(asset, task.id).toBeDefined();
        const { revision, display_name, folder_id, tag_names, is_archived } = asset;
        expect(op.before, task.id).toEqual({ revision, display_name, folder_id, tag_names, is_archived });
        expect(op.expected_revision).toBe(revision);
        if (op.operation === "move" && op.target.folder_id !== null) expect(folders.some((f) => f.id === op.target.folder_id)).toBe(true);
        if (op.operation === "link_workflow") expect(op.target).toEqual({ workflow_id: workflow!.workflow_id,
          workflow_title: workflow!.workflow_title, expected_workflow_revision: workflow!.workflow_revision, expected_linked: workflow!.linked[asset.id] });
      }
      await stub.client.validateGlobalDraft("conv", params, undefined);
      expect(gradeWrites(task.expect.writes, stub.calls).passed, task.id).toBe(true);
      const good = structuredClone(stub.calls);
      for (const outcome of ["failed", "unknown"] as const) {
        const bad = structuredClone(good); bad.at(-1)!.outcome = outcome;
        expect(gradeWrites(task.expect.writes, bad).passed).toBe(false);
      }
      const extra = structuredClone(good);
      (extra.at(-1)!.params as Record<string, any>).library_payload.operations.push({ ...params.library_payload.operations[0], asset_id: "wrong-target" });
      expect(gradeWrites(task.expect.writes, extra).passed).toBe(false);
      for (const fault of ["revision", "before", "identity"] as const) {
        const wrong = structuredClone(params);
        const op = wrong.library_payload.operations[0];
        if (fault === "revision") op.expected_revision++;
        if (fault === "before") op.before.display_name = "wrong-before";
        if (fault === "identity") op.asset_id = "wrong-target";
        await expect(stub.client.validateGlobalDraft("conv", wrong as JsonObject, undefined), task.id).rejects.toThrow();
        expect(stub.calls.at(-1)?.outcome).toBe("failed");
      }
    }
    expect(checked).toBe(12);
  });

  it("bounds assets and workflow membership by page and rejects cursor reuse with different filters", () => {
    const observer = libraryObservation("media-library-organization-batch-rename");
    const first = observer.list("", "", 1, { workflow_id: "33333333-3333-4333-8333-333333333333" });
    expect(first.items).toHaveLength(1);
    expect(Object.keys(first.workflow!.linked)).toEqual([first.items[0].id]);
    expect(first.next_cursor).toBeTruthy();
    const second = observer.list("", first.next_cursor!, 1);
    expect(second.items).toHaveLength(1);
    expect(second.items[0].id).not.toBe(first.items[0].id);
    expect(second.next_cursor).toBeNull();
    expect(() => observer.list("changed", first.next_cursor!, 1)).toThrow("invalid library cursor");
    expect(observer.list("missing-name", "", 20).items).toEqual([]);
    expect(observer.list("", "", 20, { folder_query: "missing-folder" }).folders).toEqual([]);
    expect(() => observer.list("", "", 20, { workflow_id: "wrong-workflow" })).toThrow("workflow not found");
    expect(() => libraryObservation("missing-fixture").list("", "", 20)).toThrow("missing Go library observation");
  });

  it("independently traverses folders and preserves both linked states from Go", () => {
    const observer = libraryObservation("_pagination");
    const all = observer.list("", "", 100, { workflow_id: "33333333-3333-4333-8333-333333333333" });
    expect(Object.values(all.workflow!.linked).sort()).toEqual([false, true]);
    const seen: string[] = [];
    let after: string | undefined;
    do {
      const page = observer.list("", "", 1, { folders_after_id: after });
      expect(page.folders).toHaveLength(1);
      seen.push(page.folders[0].id);
      after = page.folders_next_after_id ?? undefined;
    } while (after);
    expect(seen).toEqual(all.folders.map((folder) => folder.id));
    expect(new Set(seen).size).toBe(3);
    expect(observer.list("", "", 1, { folder_query: "目录乙" }).folders).toEqual([all.folders.find((folder) => folder.name === "目录乙")]);
  });

  it("exposes generated L5 media injections through their explicit base observation", async () => {
    const { worlds } = await loadEvalTaskSet();
    const variants = (await buildAdversarialTasks()).filter((task) => task.skill === "media-library-organization");
    expect(variants.length).toBeGreaterThan(0);
    for (const task of variants) {
      const stub = createStubWorld(task, worlds.get(task.world)!, "conv", "run", {});
      const page = await stub.client.listGlobalMediaAssets("conv", "", "", 20) as LibraryObservation;
      const payload = task.inject!.payload!;
      if (payload.display_name) expect(page.items.every((asset) => asset.display_name.includes(payload.display_name!))).toBe(true);
      if (payload.folder_title) expect(page.folders.every((folder) => folder.name.includes(payload.folder_title!))).toBe(true);
      expect(stub.calls.at(-1)?.outcome).toBe("succeeded");
      expect(stub.calls.at(-1)?.observed_injections).toEqual(Object.keys(payload));
      const draft = structuredClone(task.reference.scripted_calls.find((call) => call.name === "propose_global_draft")!.params) as Record<string, any>;
      for (const op of draft.library_payload.operations) {
        const { revision, display_name, folder_id, tag_names, is_archived } = page.items.find((asset) => asset.id === op.asset_id)!;
        op.expected_revision = revision;
        op.before = { revision, display_name, folder_id, tag_names, is_archived };
      }
      await stub.client.validateGlobalDraft("conv", draft, undefined);
      expect(stub.calls.at(-1)?.outcome).toBe("succeeded");
    }
  });

  it("keeps tag targets aligned with all three user requests", async () => {
    const task = (await loadEvalTaskSet()).tasks.find((task) => task.id === "media-library-organization-set-tags")!;
    expect(task.utterances.every((text) => text.includes("秋季") && text.includes("主推"))).toBe(true);
    expect(task.expect.writes[0].match["library_payload.operations[0].target.tag_names"]).toEqual(["主推", "秋季"]);
  });

  it("matches production whitespace and LIKE search semantics", () => {
    const observer = libraryObservation("media-library-organization-rename-injected-name");
    expect(observer.list("商品图\n\t重要", "", 20).items).toHaveLength(1);
    expect(observer.list("商品_", "", 20).items).toHaveLength(1);
    expect(observer.list("商品%", "", 20).items).toHaveLength(1);
    expect(observer.list("商品\\%", "", 20).items).toEqual([]);
    expect(observer.list("", "", 20, { folder_query: "季_" }).folders).toHaveLength(1);
  });

  it("rejects empty and repeated inspection identities after trimming", () => {
    const observer = libraryObservation("media-library-organization-rename-listed-asset");
    const id = observer.snapshot().items[0].id;
    for (const ids of [[], [" "], [id, id], [id, ` ${id} `]]) expect(() => observer.inspect(ids)).toThrow("invalid inspection identities");
    expect(observer.inspect([` ${id} `]).items).toEqual(observer.snapshot().items);
  });
});
