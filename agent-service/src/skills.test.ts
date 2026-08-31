import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { loadSkillCatalog } from "./skills.js";

const EXAMPLE_BODY = `## 何时使用
测试。

## 前置事实
无。

## 工作循环
调用 \`ask_user\`。

## 禁止行为
不要编造工具名。

## 完成判据
测试结束。
`;

function exampleSkillMarkdown(frontmatter: string, body = EXAMPLE_BODY): string {
  return `---\n${frontmatter}\n---\n\n${body}`;
}

const VALID_EXAMPLE_FRONTMATTER = `name: example-skill
description: Example skill for testing.
triggers: ["test skill"]
owns_tools: ["ask_user"]
scope: any
version: 1`;

describe("ProductFlow Skill catalog", () => {
  it("keeps discovery metadata separate from on-demand instructions", async () => {
    const catalog = await loadSkillCatalog();

    expect(catalog.names).toEqual([
      "graph-editing",
      "media-library-organization",
      "product-intake",
      "run-diagnosis",
      "workflow-run-request",
    ]);
    expect(catalog.prompt).toContain("<name>graph-editing</name>");
    expect(catalog.prompt).toContain("<triggers>edit graph");
    expect(catalog.prompt).toContain("<owns_tools>");
    expect(catalog.prompt).not.toContain("productflow-core");
    expect(catalog.prompt).not.toContain("load it before other skills");
    expect(catalog.prompt).not.toContain("一次可逆编辑");
    await expect(catalog.load("graph-editing")).resolves.toContain("只有零匹配或多匹配时才提问");
    await expect(catalog.load("graph-editing")).resolves.toContain("一张生成图作为有界默认值");
    await expect(catalog.load("media-library-organization")).resolves.toContain("检查完成后继续原请求");
    await expect(catalog.load("product-intake")).resolves.toContain("不要用普通回复代替结构化问题");
    await expect(catalog.load("product-intake")).resolves.toContain("finalize_product_intake_v1");
    await expect(catalog.load("product-intake")).resolves.toContain("birth_expandable");
    await expect(catalog.load("product-intake")).resolves.toContain("推荐套图");
    await expect(catalog.load("graph-editing", "references/add-shot.md")).resolves.toContain("create_group");
    await expect(catalog.load("graph-editing", "references/add-shot.md")).resolves.toContain("image_prompt");
    expect(catalog.promptForScope("global")).toContain("media-library-organization");
    expect(catalog.promptForScope("global")).not.toContain("<name>graph-editing</name>");
    expect(catalog.promptForScope("product_workflow")).toContain("graph-editing");
    const readme = await readFile(join(catalog.root, "README.md"), "utf8");
    expect(readme).toContain("何时使用");
    expect(readme).toContain("evals/fixtures.ts");
  });

  it("loads static references only inside the skill references directory", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-skills-"));
    try {
      const skillDir = join(root, "example-skill");
      await mkdir(join(skillDir, "references"), { recursive: true });
      await writeFile(join(skillDir, "SKILL.md"), exampleSkillMarkdown(VALID_EXAMPLE_FRONTMATTER));
      await writeFile(join(skillDir, "references", "guide.md"), "# Guide\n");
      const catalog = await loadSkillCatalog(root);

      await expect(catalog.load("example-skill", "references/guide.md")).resolves.toBe("# Guide");
      await expect(catalog.load("example-skill", "SKILL.md")).rejects.toThrow("only exposes static references");
      await expect(catalog.load("example-skill", "references/../SKILL.md")).rejects.toThrow("relative path");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("ignores authoring markdown in the skills root", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-skills-"));
    try {
      const skillDir = join(root, "example-skill");
      await mkdir(skillDir, { recursive: true });
      await writeFile(join(skillDir, "SKILL.md"), exampleSkillMarkdown(VALID_EXAMPLE_FRONTMATTER));
      await writeFile(join(root, "README.md"), "# Authoring notes\n");
      const catalog = await loadSkillCatalog(root);
      expect(catalog.names).toEqual(["example-skill"]);
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
        exampleSkillMarkdown(`name: wrong-name
description: Example skill for testing.
triggers: ["test skill"]
owns_tools: ["ask_user"]
scope: any
version: 1`),
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
        exampleSkillMarkdown(`description: Example skill for testing.
triggers: ["test skill"]
owns_tools: ["ask_user"]
scope: any
version: 1`),
      );

      await expect(loadSkillCatalog(root)).rejects.toThrow("frontmatter must include name");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("requires scope, version, owns_tools, and the five body headings", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-skills-"));
    try {
      const skillDir = join(root, "example-skill");
      await mkdir(skillDir, { recursive: true });

      await writeFile(
        join(skillDir, "SKILL.md"),
        exampleSkillMarkdown(`name: example-skill
description: Example skill for testing.
triggers: ["test skill"]
owns_tools: ["ask_user"]
version: 1`),
      );
      await expect(loadSkillCatalog(root)).rejects.toThrow("must include scope");

      await writeFile(
        join(skillDir, "SKILL.md"),
        exampleSkillMarkdown(`name: example-skill
description: Example skill for testing.
triggers: ["test skill"]
owns_tools: ["ask_user"]
scope: any`),
      );
      await expect(loadSkillCatalog(root)).rejects.toThrow("must include version");

      await writeFile(
        join(skillDir, "SKILL.md"),
        exampleSkillMarkdown(`name: example-skill
description: Example skill for testing.
triggers: ["test skill"]
guards_tools: ["ask_user"]
scope: any
version: 1`),
      );
      await expect(loadSkillCatalog(root)).rejects.toThrow("must include owns_tools");

      await writeFile(
        join(skillDir, "SKILL.md"),
        exampleSkillMarkdown(VALID_EXAMPLE_FRONTMATTER, "# Example\n"),
      );
      await expect(loadSkillCatalog(root)).rejects.toThrow("must include ## 何时使用");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });
});
