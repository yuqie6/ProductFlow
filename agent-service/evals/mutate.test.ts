import { readFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

import { mutationDefinitions } from "./mutate.js";

const skillsRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../.pi/skills");

describe("eval mutations", () => {
  it("keeps four concrete mutation anchors tied to focused tasks", async () => {
    expect(mutationDefinitions.map((item) => item.id)).toEqual([
      "remove-unconfirmed-finalize-guard",
      "rename-node-example-to-add-node",
      "remove-read-context-first",
      "swap-apply-propose-guidance",
    ]);
    for (const mutation of mutationDefinitions) {
      expect(mutation.task).toMatch(new RegExp(`^${mutation.skill}-`));
      await expect(readFile(join(skillsRoot, mutation.file), "utf8")).resolves.toContain("---");
    }
  });
});
