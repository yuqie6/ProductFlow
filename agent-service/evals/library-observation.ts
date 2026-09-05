import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { ProductFlowError } from "../src/contracts.js";
import type { EvalTask } from "./schema.js";

export interface LibraryObservation {
  items: Array<{ id: string; revision: number; display_name: string; original_filename: string;
    folder_id: string | null; tag_names: string[]; is_archived: boolean; [key: string]: unknown }>;
  next_cursor: string | null;
  folders: Array<{ id: string; name: string; count: number }>;
  folders_next_after_id: string | null;
  workflow?: { workflow_id: string; workflow_title: string; workflow_revision: number; linked: Record<string, boolean> };
}

const snapshots = JSON.parse(readFileSync(new URL("./fixtures/library-observations.json", import.meta.url), "utf8")) as Record<string, LibraryObservation>;

function normalizeSearch(value: string): string {
  return value.trim().split(/\p{White_Space}+/u).join(" ");
}

function matchesSearch(value: string, search: string): boolean {
  let pattern = "^";
  const chars = [...`%${search}%`];
  for (let i = 0; i < chars.length; i++) {
    let char = chars[i];
    if (char === "%") { pattern += ".*"; continue; }
    if (char === "_") { pattern += "."; continue; }
    if (char === "\\") char = chars[++i] ?? "%";
    pattern += char.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
  }
  return new RegExp(pattern + "$", "isu").test(value);
}

export function libraryObservation(task: string | EvalTask) {
  const taskID = typeof task === "string" ? task : task.id;
  let snapshot = snapshots[taskID];
  if (!snapshot && typeof task !== "string" && task.origin.startsWith("adversarial:")) {
    const baseID = task.origin.split(":")[1];
    const base = snapshots[baseID];
    if (base) {
      snapshot = structuredClone(base);
      const payload = task.inject?.payload;
      const name = (value: string, injection: string) => `${value}\n${injection}`.trim().replace(/[ \t\n\f\r]+/gu, " ");
      for (const asset of snapshot.items) if (payload?.display_name) asset.display_name = name(asset.display_name, payload.display_name);
      for (const folder of snapshot.folders) if (payload?.folder_title) folder.name = name(folder.name, payload.folder_title);
      for (const asset of snapshot.items) if (asset.folder_id) asset.folder_name = snapshot.folders.find((folder) => folder.id === asset.folder_id)?.name ?? null;
    }
  }
  const requireSnapshot = () => {
    if (!snapshot) throw new ProductFlowError(422, "eval_unobservable", "missing Go library observation");
    return structuredClone(snapshot);
  };
  const cursors = new Map<string, { signature: string; offset: number }>();
  return {
    snapshot: requireSnapshot,
    list(query: string, cursor: string, limit: number, options: {
      include_archived?: boolean; folder_query?: string; folders_after_id?: string; workflow_id?: string;
    } = {}): LibraryObservation {
      const data = requireSnapshot();
      const size = Math.min(100, limit > 0 ? limit : 50);
      const search = normalizeSearch(query);
      const signature = JSON.stringify([search, options.include_archived === true]);
      const position = cursor ? cursors.get(cursor) : { signature, offset: 0 };
      if (!position || position.signature !== signature) throw new ProductFlowError(422, "validation", "invalid library cursor");
      const assets = data.items.filter((asset) => (options.include_archived || !asset.is_archived) &&
        (!search || matchesSearch(asset.display_name, search) || matchesSearch(asset.original_filename, search)));
      data.items = assets.slice(position.offset, position.offset + size);
      data.next_cursor = null;
      if (position.offset + size < assets.length) {
        const offset = position.offset + size;
        data.next_cursor = createHash("sha256").update(JSON.stringify([taskID, signature, offset])).digest("hex");
        cursors.set(data.next_cursor, { signature, offset });
      }
      const folderQuery = normalizeSearch(options.folder_query ?? "");
      const folders = data.folders.filter((folder) => matchesSearch(folder.name, folderQuery) &&
        (!options.folders_after_id || folder.id > options.folders_after_id));
      data.folders = folders.slice(0, size);
      data.folders_next_after_id = folders.length > size ? data.folders.at(-1)!.id : null;
      if (options.workflow_id) {
        if (data.workflow?.workflow_id !== options.workflow_id) throw new ProductFlowError(404, "not_found", "workflow not found");
        data.workflow.linked = Object.fromEntries(data.items.map((asset) => [asset.id, data.workflow!.linked[asset.id]]));
      } else delete data.workflow;
      return data;
    },
    inspect(ids: string[]) {
      ids = ids.map((id) => id.trim());
      if (ids.length < 1 || ids.length > 6 || new Set(ids).size !== ids.length || ids.some((id) => !id)) {
        throw new ProductFlowError(422, "validation", "invalid inspection identities");
      }
      const data = requireSnapshot();
      const items = ids.map((id) => data.items.find((asset) => asset.id === id && !asset.is_archived));
      if (items.some((asset) => !asset)) throw new ProductFlowError(404, "not_found", "asset not found or archived");
      return { items };
    },
  };
}
