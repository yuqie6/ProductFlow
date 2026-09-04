import { readdir, readFile } from "node:fs/promises";
import { dirname, extname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { Value } from "typebox/value";

import { validatePageContext } from "../src/contracts.js";
import { PRODUCTFLOW_SKILL_TOOL_NAME, loadSkillCatalog, type SkillCatalog } from "../src/skills.js";
import { toolManifestEntry } from "../src/tool-manifest.js";
import { EvalTaskSchema, EvalWorldSchema, type EvalTask, type EvalWorld } from "./schema.js";
import { assignedSplit } from "./split.js";

const EVALS_ROOT = dirname(fileURLToPath(import.meta.url));
const DEFAULT_TASKS_ROOT = join(EVALS_ROOT, "tasks");
const DEFAULT_WORLDS_ROOT = join(EVALS_ROOT, "worlds");

export interface EvalTaskSet {
  tasks: EvalTask[];
  worlds: Map<string, EvalWorld>;
}

export interface EvalLoaderOptions {
  tasksRoot?: string;
  worldsRoot?: string;
  catalog?: SkillCatalog;
}

export async function loadEvalTaskSet(options: EvalLoaderOptions = {}): Promise<EvalTaskSet> {
  const catalog = options.catalog ?? await loadSkillCatalog();
  const worlds = await loadEvalWorlds(options.worldsRoot);
  const tasks = await loadEvalTasks({ tasksRoot: options.tasksRoot, worlds, catalog });
  return { tasks, worlds };
}

export async function loadEvalWorlds(worldsRoot = DEFAULT_WORLDS_ROOT): Promise<Map<string, EvalWorld>> {
  const files = await jsonFiles(resolve(worldsRoot));
  if (files.length === 0) throw new Error(`Agent eval world directory has no JSON files: ${worldsRoot}`);
  const worlds = new Map<string, EvalWorld>();
  for (const file of files) {
    const value = await readJSON(file);
    assertSchema(EvalWorldSchema, value, file, "world");
    const world = value as EvalWorld;
    if (worlds.has(world.name)) throw new Error(`Duplicate Agent eval world name: ${world.name}`);
    worlds.set(world.name, world);
  }
  return worlds;
}

export async function loadEvalTasks(options: {
  tasksRoot?: string;
  worlds?: Map<string, EvalWorld>;
  catalog?: SkillCatalog;
} = {}): Promise<EvalTask[]> {
  const tasksRoot = resolve(options.tasksRoot ?? DEFAULT_TASKS_ROOT);
  const worlds = options.worlds ?? await loadEvalWorlds();
  const catalog = options.catalog ?? await loadSkillCatalog();
  const files = await jsonFiles(tasksRoot);
  if (files.length === 0) throw new Error(`Agent eval task directory has no JSON files: ${tasksRoot}`);
  const tasks: EvalTask[] = [];
  const ids = new Set<string>();
  for (const file of files) {
    const value = await readJSON(file);
    assertSchema(EvalTaskSchema, value, file, "task");
    const task = value as EvalTask;
    if (ids.has(task.id)) throw new Error(`Duplicate Agent eval task id: ${task.id}`);
    ids.add(task.id);
    const world = worlds.get(task.world);
    if (!world) throw new Error(`Agent eval task ${task.id} references unknown world: ${task.world}`);
    validateEvalTask(task, world, catalog);
    tasks.push({ ...task, split: task.split ?? assignedSplit(task.id) });
  }
  return tasks.sort((left, right) => left.id.localeCompare(right.id));
}

export function validateEvalTask(task: EvalTask, world: EvalWorld, catalog: SkillCatalog): void {
  validatePageContext(task.page_context);
  if (task.scope === "global" && task.page_context.product_id !== null) {
    throw new Error(`Global Agent eval must not set page_context.product_id: ${task.id}`);
  }
  if (task.scope === "product_workflow" && !task.page_context.product_id) {
    throw new Error(`Product Agent eval must set page_context.product_id: ${task.id}`);
  }
  if (task.reference.scripted_calls[0]?.name !== PRODUCTFLOW_SKILL_TOOL_NAME) {
    throw new Error(`Agent eval must load the skill first: ${task.id}`);
  }
  const loadedSkill = task.reference.scripted_calls[0]?.params;
  if (!loadedSkill || typeof loadedSkill !== "object" || (loadedSkill as { skill_name?: unknown }).skill_name !== task.skill) {
    throw new Error(`Agent eval must load its declared skill first: ${task.id}`);
  }
  if (!catalog.names.includes(task.skill)) {
    throw new Error(`Agent eval references unknown catalog skill: ${task.skill}`);
  }
  if (world.name !== task.world) throw new Error(`Agent eval world identity mismatch: ${task.id}`);

  const referencedTools = [
    ...task.reference.scripted_calls.map((call) => call.name),
    ...task.expect.tools.required,
    ...task.expect.tools.forbidden,
    ...task.expect.writes.map((write) => write.tool),
  ];
  for (const name of referencedTools) {
    if (!toolManifestEntry(name)) throw new Error(`Agent eval references unknown tool: ${name}`);
  }
  assertSkillOwnsReferenceTools(task, catalog);
}

function assertSkillOwnsReferenceTools(task: EvalTask, catalog: SkillCatalog): void {
  const owned = new Set(ownedToolsFromCatalogPrompt(catalog.prompt, task.skill));
  for (const call of task.reference.scripted_calls) {
    if (call.name === PRODUCTFLOW_SKILL_TOOL_NAME) continue;
    if (!owned.has(call.name)) {
      throw new Error(`Agent eval ${task.skill} expected tool ${call.name} is not in that skill's owns_tools`);
    }
  }
}

function ownedToolsFromCatalogPrompt(prompt: string, skillName: string): string[] {
  const escapedName = skillName.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
  const match = new RegExp(
    `<skill><name>${escapedName}</name>[\\s\\S]*?<owns_tools>([^<]*)</owns_tools>`,
    "u",
  ).exec(prompt);
  if (!match) throw new Error(`Skill catalog is missing owns_tools for ${skillName}`);
  return match[1].split(",").map((item) => item.trim()).filter(Boolean);
}

function assertSchema(schema: typeof EvalTaskSchema | typeof EvalWorldSchema, value: unknown, file: string, kind: string): void {
  if (!Value.Check(schema, value)) {
    throw new Error(`Invalid Agent eval ${kind} JSON: ${relative(EVALS_ROOT, file)}`);
  }
}

async function readJSON(file: string): Promise<unknown> {
  let source: string;
  try {
    source = await readFile(file, "utf8");
  } catch (error) {
    throw new Error(`Cannot read Agent eval JSON: ${file}`, { cause: error });
  }
  try {
    return JSON.parse(source) as unknown;
  } catch (error) {
    throw new Error(`Invalid JSON in Agent eval file: ${file}`, { cause: error });
  }
}

async function jsonFiles(root: string): Promise<string[]> {
  const entries = (await readdir(root, { withFileTypes: true })).sort((left, right) => left.name.localeCompare(right.name));
  const files: string[] = [];
  for (const entry of entries) {
    const path = join(root, entry.name);
    if (entry.isDirectory()) files.push(...(await jsonFiles(path)));
    else if (entry.isFile() && extname(entry.name) === ".json") files.push(path);
  }
  return files;
}
