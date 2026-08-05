import type { ProductListSort } from "../../lib/types";

export const PRODUCT_LIST_SORTS = ["updated_desc", "created_desc", "name_asc"] as const;
export const DEFAULT_PRODUCT_LIST_SORT: ProductListSort = "updated_desc";

export interface ProductListQueryState {
  page: number;
  q: string;
  sort: ProductListSort;
}

export interface ProductListSearchPatch {
  page?: number;
  q?: string;
  sort?: ProductListSort;
  resetPage?: boolean;
}

function isProductListSort(value: string | null): value is ProductListSort {
  return PRODUCT_LIST_SORTS.some((sort) => sort === value);
}

export function parseProductListSearchParams(searchParams: URLSearchParams): ProductListQueryState {
  const rawPage = Number(searchParams.get("page"));
  const page = Number.isInteger(rawPage) && rawPage >= 1 ? rawPage : 1;
  const rawSort = searchParams.get("sort");

  return {
    page,
    q: searchParams.get("q")?.trim() ?? "",
    sort: isProductListSort(rawSort) ? rawSort : DEFAULT_PRODUCT_LIST_SORT,
  };
}

export function patchProductListSearchParams(
  searchParams: URLSearchParams,
  patch: ProductListSearchPatch,
): URLSearchParams {
  const next = new URLSearchParams(searchParams);

  if (patch.resetPage) {
    next.delete("page");
  }
  if (patch.page !== undefined) {
    if (Number.isInteger(patch.page) && patch.page > 1) {
      next.set("page", String(patch.page));
    } else {
      next.delete("page");
    }
  }
  if ("q" in patch) {
    const query = patch.q?.trim() ?? "";
    if (query) {
      next.set("q", query);
    } else {
      next.delete("q");
    }
  }
  if ("sort" in patch) {
    if (patch.sort && patch.sort !== DEFAULT_PRODUCT_LIST_SORT) {
      next.set("sort", patch.sort);
    } else {
      next.delete("sort");
    }
  }

  return next;
}
