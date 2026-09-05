import { describe, expect, it } from "vitest";
import type { GraphCatalogConfigField } from "../../../lib/types";
import { generationOptionFields } from "./generationOptions";
import { catalogConfigForSave, patchCatalogValue } from "./catalogConfig";

describe("provider-scoped generation fields", () => {
  const fields: GraphCatalogConfigField[] = [{ key: "generation_spec", control: "group", value_kind: "object", required: true, fields: [
    { key: "aspect_ratio", control: "aspect_ratio", value_kind: "string", required: true },
    { key: "resolution_tier", control: "select", value_kind: "string", required: true, choices: ["standard", "high", "ultra"] },
    { key: "reference_fidelity", control: "select", value_kind: "string", required: true, choices: ["low", "medium", "high"] },
  ] }];
  it("editing an available control preserves stored values of unavailable controls", () => {
    const filtered = generationOptionFields(fields, { aspect_ratio: ["1:1", "2:3"] }, false, "hero");
    const draft = { generation_spec: { aspect_ratio: "3:4", resolution_tier: "ultra", reference_fidelity: "medium" } };
    const changed = patchCatalogValue(filtered, draft, ["generation_spec", "aspect_ratio"], "2:3");
    expect(catalogConfigForSave(fields, changed)).toEqual({ generation_spec: { aspect_ratio: "2:3", resolution_tier: "ultra", reference_fidelity: "medium" } });
  });
  it("does not offer reference fidelity when runtime ignores or fixes it", () => {
    const options = { reference_fidelity: ["low", "high"] };
    for (const [hasRefs, imageType] of [[false, "hero"], [true, "selling_point"]] as const) {
      expect(generationOptionFields(fields, options, hasRefs, imageType)[0].fields?.find((field) => field.key === "reference_fidelity")?.control).toBe("hidden");
    }
    expect(generationOptionFields(fields, options, true, "hero")[0].fields?.find((field) => field.key === "reference_fidelity")?.choices).toEqual(["low", "high"]);
  });
});
