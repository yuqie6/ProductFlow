import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

import type { ProductSummary } from "../../lib/types";
import { ProductListSurface } from "./ProductListSurface";

const product: ProductSummary = {
  id: "product-1",
  name: "语义回归测试商品",
  category: "测试分类",
  price: "199.00",
  workflow_state: "poster_ready",
  latest_copy_status: "confirmed",
  latest_poster_at: "2026-08-10T08:30:00Z",
  source_image_filename: "product.png",
  source_image_download_url: "/api/products/product-1/source-image",
  source_image_preview_url: "/api/products/product-1/source-image/preview",
  source_image_thumbnail_url: "/api/products/product-1/source-image/thumbnail",
  created_at: "2026-08-09T08:30:00Z",
  updated_at: "2026-08-10T08:30:00Z",
};

function renderProductList(): string {
  return renderToStaticMarkup(
    createElement(
      MemoryRouter,
      { initialEntries: ["/products"] },
      createElement(ProductListSurface, {
        products: [product],
        total: 1,
        page: 1,
        pageSize: 20,
        totalPages: 1,
        sort: "updated_desc",
        searchDraft: "",
        isLoading: false,
        isError: false,
        isRefreshing: false,
        deletionEnabled: true,
        isDeleting: false,
        deleteError: "",
        onSearchDraftChange: () => undefined,
        onSortChange: () => undefined,
        onPageChange: () => undefined,
        onClearSearch: () => undefined,
        onRetry: () => undefined,
        onDelete: () => undefined,
      }),
    ),
  );
}

describe("ProductListSurface semantics", () => {
  it("groups products as a list without orphan table roles", () => {
    const markup = renderProductList();

    expect(markup).toContain('role="list"');
    expect(markup).toContain('role="listitem"');
    expect(markup).not.toContain('role="row"');
    expect(markup).not.toContain('role="cell"');
    expect(markup).not.toContain('role="columnheader"');
  });

  it("keeps workflow status out of the product list", () => {
    const markup = renderProductList();

    expect(markup).not.toContain("流程状态");
    expect(markup).not.toContain("已完成");
  });
});
