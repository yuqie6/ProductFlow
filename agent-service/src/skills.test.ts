import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { loadSkillCatalog } from "./skills.js";

describe("ProductFlow Skill catalog", () => {
  it("keeps discovery metadata separate from on-demand instructions", async () => {
    const catalog = await loadSkillCatalog();

    expect(catalog.names).toEqual([
      "media-library-organization",
      "product-intake",
      "productflow-core",
      "workflow-run-request",
    ]);
    expect(catalog.prompt).toContain("<name>productflow-core</name>");
    expect(catalog.prompt).not.toContain("ProductFlow backend owns scope");
    await expect(catalog.load("productflow-core")).resolves.toContain("ProductFlow backend owns scope");
    await expect(catalog.load("product-intake")).resolves.toContain("finalize_product_intake_v1");
    await expect(catalog.load("product-intake")).resolves.toContain("birth_expandable");
    await expect(catalog.load("productflow-core")).resolves.toContain("create_node");
    await expect(catalog.load("productflow-core", "references/add-shot.md")).resolves.toContain("create_group");
  });

  it("loads static references only inside the skill references directory", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-skills-"));
    try {
      const skillDir = join(root, "example-skill");
      await mkdir(join(skillDir, "references"), { recursive: true });
      await writeFile(
        join(skillDir, "SKILL.md"),
        "---\nname: example-skill\ndescription: Example skill for testing.\n---\n\n# Example\n",
      );
      await writeFile(join(skillDir, "references", "guide.md"), "# Guide\n");
      const catalog = await loadSkillCatalog(root);

      await expect(catalog.load("example-skill", "references/guide.md")).resolves.toBe("# Guide");
      await expect(catalog.load("example-skill", "SKILL.md")).rejects.toThrow("only exposes static references");
      await expect(catalog.load("example-skill", "references/../SKILL.md")).rejects.toThrow("relative path");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("requires the standard skill name and directory contract", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-skills-"));
    try {
      const skillDir = join(root, "wrong-directory");
      await mkdir(skillDir, { recursive: true });
      await writeFile(
        join(skillDir, "SKILL.md"),
        "---\nname: wrong-name\ndescription: Example skill for testing.\n---\n\n# Example\n",
      );

      await expect(loadSkillCatalog(root)).rejects.toThrow("must match its directory");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("requires name in frontmatter instead of accepting Pi's directory fallback", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-skills-"));
    try {
      const skillDir = join(root, "example-skill");
      await mkdir(skillDir, { recursive: true });
      await writeFile(
        join(skillDir, "SKILL.md"),
        "---\ndescription: Example skill for testing.\n---\n\n# Example\n",
      );

      await expect(loadSkillCatalog(root)).rejects.toThrow("frontmatter must include name");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });
});
