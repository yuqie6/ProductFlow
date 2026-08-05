import { describe, expect, it } from "vitest";

import { parseProductListSearchParams, patchProductListSearchParams } from "./model";

describe("product list URL state", () => {
  it("parses valid values and normalizes invalid defaults", () => {
    expect(parseProductListSearchParams(new URLSearchParams("page=3&q=%20lamp%20&sort=created_desc"))).toEqual({
      page: 3,
      q: "lamp",
      sort: "created_desc",
    });
    expect(parseProductListSearchParams(new URLSearchParams("page=0&q=%20%20&sort=unknown"))).toEqual({
      page: 1,
      q: "",
      sort: "updated_desc",
    });
  });

  it("omits defaults, resets the page, and preserves unrelated params", () => {
    const current = new URLSearchParams("page=4&q=old&sort=name_asc&fixture=12");
    const next = patchProductListSearchParams(current, {
      q: "  new lamp  ",
      sort: "updated_desc",
      resetPage: true,
    });

    expect(next.get("page")).toBeNull();
    expect(next.get("q")).toBe("new lamp");
    expect(next.get("sort")).toBeNull();
    expect(next.get("fixture")).toBe("12");
  });

  it("stores only non-default page values", () => {
    expect(patchProductListSearchParams(new URLSearchParams(), { page: 1 }).has("page")).toBe(false);
    expect(patchProductListSearchParams(new URLSearchParams(), { page: 2 }).get("page")).toBe("2");
  });

  it("stores non-default sorting and removes the default", () => {
    const named = patchProductListSearchParams(new URLSearchParams("page=3"), {
      sort: "name_asc",
      resetPage: true,
    });
    expect(named.get("page")).toBeNull();
    expect(named.get("sort")).toBe("name_asc");
    expect(patchProductListSearchParams(named, { sort: "updated_desc" }).get("sort")).toBeNull();
  });
});
