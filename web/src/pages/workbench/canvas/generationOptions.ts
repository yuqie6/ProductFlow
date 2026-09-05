import type { GraphCatalogConfigField } from "../../../lib/types";
import { IMAGE_TYPE_FAMILY_BY_KEY } from "../../../lib/imageTypeFamilies";

export function generationOptionFields(fields: GraphCatalogConfigField[], options: Record<string, string[]>, hasReferences: boolean, imageType: unknown): GraphCatalogConfigField[] {
  const family = Object.entries(IMAGE_TYPE_FAMILY_BY_KEY).find(([key]) => key === imageType)?.[1];
  return fields.map((field) => field.key !== "generation_spec" ? field : {
    ...field,
    fields: (field.fields ?? []).map((child) => {
      const choices = options[child.key];
      if (child.key === "reference_fidelity" && (!hasReferences || family === "infographic")) return { ...child, control: "hidden" as const };
      if (!choices?.length) return { ...child, control: "hidden" as const };
      return { ...child, choices, panel: child.key === "quality_intent" ? "advanced" as const : child.panel };
    }),
  });
}
