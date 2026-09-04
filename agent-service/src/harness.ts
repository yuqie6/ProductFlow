import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { Type, type Static } from "typebox";
import { Value } from "typebox/value";
import { sha256 } from "./contracts.js";
import { RUNTIME_POLICY } from "./runtime-policy.generated.js";
import { canonicalJSON } from "./tool-manifest.js";

const instructionSections = ["bootstrap", "execution", "verification", "failure-recovery"] as const;
type InstructionSection = typeof instructionSections[number];
const emptySurface = Type.Object({}, { additionalProperties: false });
const manifestSchema = Type.Object({
  schema_version: Type.Literal(1),
  frozen_policy_sha256: Type.String({ pattern: "^[a-f0-9]{64}$" }),
  skill_overlays: emptySurface,
  runtime_control: emptySurface,
}, { additionalProperties: false });

export interface HarnessArtifact extends Readonly<Static<typeof manifestSchema>> {
  readonly instructions: Readonly<Record<InstructionSection, string>>;
}

export interface Harness {
  readonly hash: string;
  readonly artifact: HarnessArtifact;
  readonly systemPrompt: string;
}

export function harnessRoot(): string {
  return join(dirname(fileURLToPath(import.meta.url)), "../harness");
}

/** Snapshot the deployed artifact once; existing Turns never reread editable files. */
export function loadHarness(root = harnessRoot()): Harness {
  const manifest: unknown = JSON.parse(readFileSync(join(root, "manifest.json"), "utf8"));
  if (!Value.Check(manifestSchema, manifest)) throw new Error("Invalid ProductFlow harness manifest");
  if (manifest.frozen_policy_sha256 !== sha256(RUNTIME_POLICY)) {
    throw new Error("ProductFlow harness frozen policy digest mismatch");
  }
  const instructions = {} as Record<InstructionSection, string>;
  for (const section of instructionSections) {
    const content = readFileSync(join(root, "instructions", `${section}.md`), "utf8");
    if (!content.trim()) throw new Error(`ProductFlow harness instruction is empty: ${section}`);
    instructions[section] = content;
  }
  const artifact: HarnessArtifact = Object.freeze({
    schema_version: manifest.schema_version,
    frozen_policy_sha256: manifest.frozen_policy_sha256,
    instructions: Object.freeze(instructions),
    skill_overlays: Object.freeze(manifest.skill_overlays),
    runtime_control: Object.freeze(manifest.runtime_control),
  });
  return Object.freeze({
    artifact,
    hash: sha256(canonicalJSON(artifact)),
    systemPrompt: [RUNTIME_POLICY, ...instructionSections.map((section) => instructions[section].trim())].join("\n\n"),
  });
}
