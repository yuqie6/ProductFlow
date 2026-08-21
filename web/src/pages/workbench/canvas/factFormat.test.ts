import { describe, expect, it } from "vitest";

import { formatFactValue, humanizeFactKey } from "./factFormat";

describe("fact formatting", () => {
  it("formats confirmed facts as readable labels rather than raw JSON", () => {
    expect(humanizeFactKey("product.material_name")).toBe("Product Material Name");
    expect(formatFactValue(
      { material_name: "Steel", washable: true, sizes: ["S", "M"] },
      "Not set",
      { true: "Yes", false: "No" },
    )).toBe("Material Name: Steel; Washable: Yes; Sizes: S · M");
  });
});
