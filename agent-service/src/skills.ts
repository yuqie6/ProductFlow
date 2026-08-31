/**
 * 打包的 ProductFlow Skill 目录。
 *
 * 加载 Skill 只读 `.pi/skills` 下的静态文件，不能访问业务数据、存储、供应商，也不能执行脚本。
 */

import { lstat, readdir, readFile } from "node:fs/promises";
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { loadSkillsFromDir } from "@earendil-works/pi-coding-agent";
import { sha256 } from "./contracts.js";
import { toolManifestEntry } from "./tool-manifest.js";

const MAX_SKILL_BODY_BYTES = 64 << 10;
const MAX_SKILL_RESOURCE_BYTES = 2 << 20;
const MAX_SKILL_CATALOG_BYTES = 8 << 20;
const SKILL_NAME_PATTERN = /^[a-z0-9]+(?:-[a-z0-9]+)*$/u;

export const PRODUCTFLOW_SKILL_TOOL_NAME = "load_productflow_skill" as const;

interface SkillDescriptor {
  name: string;
  description: string;
  triggers: string[];
  guardsTools: string[];
  scope: SkillScope;
  version: number;
  filePath: string;
  baseDir: string;
}

export type SkillScope = "global" | "product_workflow" | "any";

export interface SkillCatalog {
  root: string;
  hash: string;
  names: string[];
  prompt: string;
  promptForScope(scope: "global" | "product_workflow"): string;
  load(name: string, resourcePath?: string): Promise<string>;
}

export async function loadSkillCatalog(skillRoot?: string): Promise<SkillCatalog> {
  const root = resolve(skillRoot ?? join(dirname(fileURLToPath(import.meta.url)), "../.pi/skills"));
  const discovered = loadSkillsFromDir({ dir: root, source: "productflow" });
  const diagnostics = discovered.diagnostics.filter((diagnostic) => {
    const path = diagnostic.path ?? "";
    return path === "" || basename(path) === "SKILL.md";
  });
  if (diagnostics.length > 0) {
    throw new Error(formatDiagnostics(diagnostics));
  }

  const descriptors = discovered.skills
    .filter((skill) => basename(skill.filePath) === "SKILL.md")
    .map((skill): SkillDescriptor => ({
      name: skill.name,
      description: skill.description.trim(),
      triggers: [],
      guardsTools: [],
      scope: "any",
      version: 1,
      filePath: resolve(skill.filePath),
      baseDir: resolve(skill.baseDir),
    }))
    .sort((left, right) => left.name.localeCompare(right.name));
  if (descriptors.length === 0) throw new Error(`ProductFlow Skill directory is empty: ${root}`);

  const names = new Set<string>();
  const canonicalParts: string[] = [];
  let catalogBytes = 0;
  for (const descriptor of descriptors) {
    const source = await readFile(descriptor.filePath, "utf8");
    const frontmatterName = readFrontmatterField(source, "name");
    if (frontmatterName === null) {
      throw new Error(`ProductFlow Skill frontmatter must include name: ${descriptor.name}`);
    }
    if (frontmatterName !== descriptor.name) {
      throw new Error(`ProductFlow Skill frontmatter name must match its directory: ${descriptor.name}`);
    }
    descriptor.triggers = readFrontmatterList(source, "triggers");
    descriptor.guardsTools = readFrontmatterList(source, "guards_tools");
    descriptor.scope = readSkillScope(source, descriptor.name);
    descriptor.version = readSkillVersion(source, descriptor.name);
    assertSkillBodyStructure(source, descriptor.name);
    validateDescriptor(root, descriptor, names);
    const files = await collectFiles(descriptor.baseDir, descriptor.baseDir, root);
    for (const file of files) {
      catalogBytes += file.bytes.byteLength;
      if (catalogBytes > MAX_SKILL_CATALOG_BYTES) {
        throw new Error(`ProductFlow Skill catalog exceeds ${MAX_SKILL_CATALOG_BYTES} bytes`);
      }
      canonicalParts.push(`${file.relativePath}\n${file.bytes.toString("base64")}`);
    }
  }

  const byName = new Map(descriptors.map((descriptor) => [descriptor.name, descriptor]));
  return {
    root,
    hash: sha256(canonicalParts.sort().join("\n\n")),
    names: descriptors.map((descriptor) => descriptor.name),
    prompt: formatSkillCatalogPrompt(descriptors),
    promptForScope: (scope) => formatSkillCatalogPrompt(descriptors.filter((descriptor) => descriptor.scope === "any" || descriptor.scope === scope)),
    load: async (name, resourcePath) => loadSkill(byName, name, resourcePath),
  };
}

interface CatalogFile {
  relativePath: string;
  bytes: Buffer;
}

async function collectFiles(directory: string, skillRoot: string, catalogRoot: string): Promise<CatalogFile[]> {
  const entries = (await readdir(directory, { withFileTypes: true })).sort((left, right) => left.name.localeCompare(right.name));
  const files: CatalogFile[] = [];
  for (const entry of entries) {
    const path = join(directory, entry.name);
    if (entry.isSymbolicLink()) throw new Error(`ProductFlow Skill catalog cannot contain symlinks: ${path}`);
    if (entry.isDirectory()) {
      const relativeToSkill = toPosix(relative(skillRoot, path));
      if (relativeToSkill === "scripts" || relativeToSkill === "assets") continue;
      files.push(...(await collectFiles(path, skillRoot, catalogRoot)));
      continue;
    }
    if (!entry.isFile()) continue;
    const relativeToSkill = toPosix(relative(skillRoot, path));
    if (relativeToSkill !== "SKILL.md" && !relativeToSkill.startsWith("references/")) continue;
    const bytes = await readFile(path);
    if (bytes.byteLength > MAX_SKILL_RESOURCE_BYTES) {
      throw new Error(`ProductFlow Skill resource exceeds ${MAX_SKILL_RESOURCE_BYTES} bytes: ${path}`);
    }
    files.push({ relativePath: toPosix(relative(catalogRoot, path)), bytes });
  }
  return files;
}

function validateDescriptor(root: string, descriptor: SkillDescriptor, names: Set<string>): void {
  const parentName = basename(descriptor.baseDir);
  if (descriptor.name.length < 1 || descriptor.name.length > 64 || !SKILL_NAME_PATTERN.test(descriptor.name)) {
    throw new Error(`ProductFlow Skill name is invalid: ${descriptor.name}`);
  }
  if (descriptor.name !== parentName) {
    throw new Error(`ProductFlow Skill name must match its directory: ${descriptor.name} != ${parentName}`);
  }
  if (descriptor.description.length < 1 || descriptor.description.length > 1024) {
    throw new Error(`ProductFlow Skill description is invalid: ${descriptor.name}`);
  }
  if (descriptor.triggers.length === 0) {
    throw new Error(`ProductFlow Skill frontmatter must include triggers: ${descriptor.name}`);
  }
  if (descriptor.guardsTools.length === 0) {
    throw new Error(`ProductFlow Skill frontmatter must include guards_tools: ${descriptor.name}`);
  }
  for (const toolName of descriptor.guardsTools) {
    if (!toolManifestEntry(toolName)) {
      throw new Error(`ProductFlow Skill owns unknown tool ${toolName}: ${descriptor.name}`);
    }
  }
  if (!isWithin(root, descriptor.baseDir) || !isWithin(root, descriptor.filePath)) {
    throw new Error(`ProductFlow Skill path escapes the catalog root: ${descriptor.name}`);
  }
  if (names.has(descriptor.name)) throw new Error(`Duplicate ProductFlow Skill name: ${descriptor.name}`);
  names.add(descriptor.name);
}

async function loadSkill(
  byName: Map<string, SkillDescriptor>,
  name: string,
  resourcePath?: string,
): Promise<string> {
  const descriptor = byName.get(name.trim());
  if (!descriptor) throw new Error(`Unknown ProductFlow Skill: ${name}`);
  const path = resourcePath === undefined ? descriptor.filePath : resolveReferencePath(descriptor, resourcePath);
  const stat = await lstat(path);
  if (!stat.isFile() || stat.isSymbolicLink()) {
    throw new Error(`ProductFlow Skill resource is not a regular file: ${resourcePath ?? "SKILL.md"}`);
  }
  if (stat.size > (resourcePath === undefined ? MAX_SKILL_BODY_BYTES : MAX_SKILL_RESOURCE_BYTES)) {
    throw new Error(`ProductFlow Skill resource exceeds its size limit: ${resourcePath ?? "SKILL.md"}`);
  }
  const content = await readFile(path, "utf8");
  return resourcePath === undefined ? stripFrontmatter(content).trim() : content.trim();
}

function resolveReferencePath(descriptor: SkillDescriptor, resourcePath: string): string {
  const normalized = resourcePath.trim().replaceAll("\\", "/");
  if (!normalized || isAbsolute(normalized) || normalized.split("/").some((part) => part === ".." || part === "")) {
    throw new Error("ProductFlow Skill resource_path must be a relative path");
  }
  if (!normalized.startsWith("references/")) {
    throw new Error("ProductFlow Skill only exposes static references/ resources");
  }
  const path = resolve(descriptor.baseDir, normalized);
  if (!isWithin(join(descriptor.baseDir, "references"), path)) {
    throw new Error("ProductFlow Skill resource_path escapes references/");
  }
  return path;
}

function stripFrontmatter(content: string): string {
  const match = /^---\r?\n[\s\S]*?\r?\n---\r?\n?/u.exec(content);
  if (!match) throw new Error("ProductFlow Skill is missing YAML frontmatter");
  return content.slice(match[0].length);
}

function readFrontmatterField(content: string, field: string): string | null {
  const normalized = content.replace(/\r\n?/gu, "\n");
  const match = /^---\n([\s\S]*?)\n---(?:\n|$)/u.exec(normalized);
  if (!match) return null;
  const fieldMatch = new RegExp(`^${field}\\s*:\\s*(.*?)\\s*$`, "mu").exec(match[1]);
  if (!fieldMatch) return null;
  const value = fieldMatch[1].trim();
  if (!value) return null;
  if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
    return value.slice(1, -1);
  }
  return value;
}

function readFrontmatterList(content: string, field: string): string[] {
  const normalized = content.replace(/\r\n?/gu, "\n");
  const match = /^---\n([\s\S]*?)\n---(?:\n|$)/u.exec(normalized);
  if (!match) return [];
  const lines = match[1].split("\n");
  const fieldIndex = lines.findIndex((line) => new RegExp(`^${field}\\s*:`).test(line));
  if (fieldIndex < 0) return [];
  const value = lines[fieldIndex].replace(new RegExp(`^${field}\\s*:`), "").trim();
  if (value) {
    try {
      const parsed: unknown = JSON.parse(value);
      if (Array.isArray(parsed) && parsed.every((item): item is string => typeof item === "string" && item.trim() !== "")) {
        return parsed.map((item) => item.trim());
      }
    } catch {
      return [];
    }
    return [];
  }
  const values: string[] = [];
  for (const line of lines.slice(fieldIndex + 1)) {
    if (!/^\s+-\s+/u.test(line)) break;
    const item = line.replace(/^\s+-\s+/u, "").trim();
    if (item) values.push(item.replace(/^(["'])(.*)\1$/u, "$2"));
  }
  return values;
}

function formatSkillCatalogPrompt(descriptors: SkillDescriptor[]): string {
  const lines = [
    "ProductFlow Skills use the Agent Skills progressive-disclosure contract.",
    `Use ${PRODUCTFLOW_SKILL_TOOL_NAME} with an exact skill_name when the task matches a skill description and its triggers. The tool returns trusted versioned instructions only; it cannot access ProductFlow data, storage, providers, databases, or execute code. If the loaded skill references references/, load that static text with resource_path when needed.`,
    "<available_productflow_skills>",
  ];
  for (const descriptor of descriptors) {
    lines.push(
      `  <skill><name>${escapeXML(descriptor.name)}</name><description>${escapeXML(descriptor.description)}</description><triggers>${descriptor.triggers.map(escapeXML).join(" | ")}</triggers><guards_tools>${descriptor.guardsTools.map(escapeXML).join(", ")}</guards_tools><scope>${descriptor.scope}</scope></skill>`,
    );
  }
  lines.push("</available_productflow_skills>");
  return lines.join("\n");
}

const REQUIRED_SKILL_HEADINGS = ["## 何时使用", "## 前置事实", "## 工作循环", "## 禁止行为", "## 完成判据"] as const;

function assertSkillBodyStructure(content: string, name: string): void {
  const body = stripFrontmatter(content);
  for (const heading of REQUIRED_SKILL_HEADINGS) {
    if (!body.includes(heading)) {
      throw new Error(`ProductFlow Skill body must include ${heading}: ${name}`);
    }
  }
}

function readSkillScope(content: string, name: string): SkillScope {
  const value = readFrontmatterField(content, "scope");
  if (value === null) {
    throw new Error(`ProductFlow Skill frontmatter must include scope: ${name}`);
  }
  if (value === "global" || value === "product_workflow" || value === "any") return value;
  throw new Error(`ProductFlow Skill scope is invalid: ${value}`);
}

function readSkillVersion(content: string, name: string): number {
  const raw = readFrontmatterField(content, "version");
  if (raw === null) {
    throw new Error(`ProductFlow Skill frontmatter must include version: ${name}`);
  }
  const version = Number(raw);
  if (!Number.isInteger(version) || version < 1) {
    throw new Error(`ProductFlow Skill version is invalid: ${raw}`);
  }
  return version;
}

function escapeXML(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&apos;");
}

function formatDiagnostics(diagnostics: Array<{ message: string; path?: string }>): string {
  return diagnostics.map((diagnostic) => `${diagnostic.path ?? "Skill"}: ${diagnostic.message}`).join("; ");
}

function isWithin(root: string, candidate: string): boolean {
  const rootPath = resolve(root);
  const candidatePath = resolve(candidate);
  const relativePath = relative(rootPath, candidatePath);
  return relativePath === "" || (!relativePath.startsWith(`..${sep}`) && relativePath !== ".." && !isAbsolute(relativePath));
}

function toPosix(value: string): string {
  return value.split(sep).join("/");
}
