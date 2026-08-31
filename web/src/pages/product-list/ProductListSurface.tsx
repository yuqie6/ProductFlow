import { useState, type ReactNode } from "react";
import {
  AlertCircle,
  ChevronLeft,
  ChevronRight,
  Image as ImageIcon,
  LoaderCircle,
  Plus,
  RefreshCw,
  Search,
  Trash2,
  X,
} from "lucide-react";
import { Link } from "react-router-dom";

import { Select as SelectField } from "../../components/ui/select";
import { api } from "../../lib/api";
import { formatPrice, formatShortDate } from "../../lib/format";
import { useI18n } from "../../lib/preferences";
import type { ProductListSort, ProductSummary } from "../../lib/types";

const SORT_OPTIONS = [
  { value: "updated_desc", labelKey: "products.sort.updated" },
  { value: "created_desc", labelKey: "products.sort.created" },
  { value: "name_asc", labelKey: "products.sort.name" },
] as const satisfies ReadonlyArray<{
  value: ProductListSort;
  labelKey: "products.sort.updated" | "products.sort.created" | "products.sort.name";
}>;

interface ProductListSurfaceProps {
  products: ProductSummary[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
  sort: ProductListSort;
  searchDraft: string;
  isLoading: boolean;
  isError: boolean;
  isRefreshing: boolean;
  deletionEnabled: boolean;
  isDeleting: boolean;
  deleteError: string;
  onSearchDraftChange: (value: string) => void;
  onSortChange: (sort: ProductListSort) => void;
  onPageChange: (page: number) => void;
  onClearSearch: () => void;
  onRetry: () => void;
  onDelete: (product: ProductSummary) => void;
}

export function ProductListSurface({
  products,
  total,
  page,
  pageSize,
  totalPages,
  sort,
  searchDraft,
  isLoading,
  isError,
  isRefreshing,
  deletionEnabled,
  isDeleting,
  deleteError,
  onSearchDraftChange,
  onSortChange,
  onPageChange,
  onClearSearch,
  onRetry,
  onDelete,
}: ProductListSurfaceProps) {
  const { t } = useI18n();
  const hasSearch = Boolean(searchDraft.trim());
  const rangeStart = (page - 1) * pageSize + 1;
  const rangeEnd = Math.min(page * pageSize, total);

  return (
    <section aria-labelledby="product-list-title">
      <div className="mb-3 flex min-h-14 items-center justify-between gap-4 sm:mb-[18px] sm:min-h-[60px]">
        <h1 id="product-list-title" className="min-w-0 text-[22px] font-semibold text-slate-950 sm:text-[25px] dark:!text-[#f1f2f4]">
          {t("products.title")}
        </h1>
        <Link
          to="/products/new"
          className="inline-flex min-h-[38px] shrink-0 items-center justify-center gap-1.5 rounded-md border border-indigo-600 bg-indigo-600 px-3 text-sm font-semibold text-white transition-colors hover:border-indigo-500 hover:bg-indigo-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 dark:!border-[#7b83e6] dark:!bg-[#7b83e6] dark:hover:!border-[#9298f0] dark:hover:!bg-[#9298f0] dark:focus-visible:!ring-[#7b83e6] dark:focus-visible:!ring-offset-[#0b0c0e] sm:px-3.5"
        >
          <Plus size={16} aria-hidden="true" />
          <span>{t("products.new")}</span>
        </Link>
      </div>

      <div className="relative rounded-lg border border-slate-200 bg-white dark:!border-[#292c32] dark:!bg-[#111316]">
        <div className="grid grid-cols-1 items-center gap-2.5 border-b border-slate-200 p-2.5 sm:grid-cols-[minmax(220px,360px)_minmax(0,1fr)] sm:gap-4 sm:px-3.5 sm:py-2.5 dark:!border-[#292c32]">
          <label className="relative flex min-w-0 items-center">
            <Search size={15} className="pointer-events-none absolute left-3 text-slate-400" aria-hidden="true" />
            <span className="sr-only">{t("products.searchPlaceholder")}</span>
            <input
              type="search"
              value={searchDraft}
              maxLength={100}
              autoComplete="off"
              onChange={(event) => onSearchDraftChange(event.target.value)}
              placeholder={t("products.searchPlaceholder")}
              className="h-11 w-full min-w-0 appearance-none rounded-md border border-slate-300 bg-white pr-11 pl-[34px] text-[13px] text-slate-900 outline-none transition-[border-color,box-shadow] placeholder:text-slate-400 focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 sm:h-9 sm:pr-9 dark:!border-[#373b43] dark:!bg-[#15171b] dark:!text-[#f1f2f4] dark:placeholder:!text-[#737780] dark:focus:!border-[#7b83e6] dark:focus:!ring-[#7b83e6]/20 [&::-webkit-search-cancel-button]:appearance-none"
            />
            {searchDraft ? (
              <button
                type="button"
                onClick={() => onSearchDraftChange("")}
                aria-label={t("products.searchClear")}
                title={t("products.searchClear")}
                className="absolute right-0 inline-flex h-11 w-11 items-center justify-center rounded-[5px] text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 sm:right-1 sm:h-7 sm:w-7 dark:!text-[#737780] dark:hover:!bg-[#1d2025] dark:hover:!text-[#f1f2f4]"
              >
                <X size={14} aria-hidden="true" />
              </button>
            ) : null}
          </label>

          <div className="flex min-w-0 items-center justify-between gap-3 px-0.5 sm:justify-self-end sm:pr-1">
            <div
              className="inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap text-xs tabular-nums text-slate-400 dark:!text-[#737780]"
              aria-live="polite"
            >
              {isRefreshing ? (
                <LoaderCircle
                  size={13}
                  className="animate-spin text-indigo-500 motion-reduce:animate-none dark:!text-[#7b83e6]"
                  aria-label={t("products.refreshing")}
                />
              ) : null}
              <span>{t("products.resultCount", { count: total })}</span>
            </div>
            <div className="inline-flex min-w-0 items-center gap-2 text-xs text-slate-500 dark:!text-[#a4a8b0]">
              <span className="shrink-0">{t("products.sort.label")}</span>
              <SelectField
                value={sort}
                onChange={(value) => onSortChange(value as ProductListSort)}
                options={SORT_OPTIONS.map((option) => ({ value: option.value, label: t(option.labelKey) }))}
                ariaLabel={t("products.sort.label")}
                className="w-[148px] sm:w-[158px] [&>button]:!h-11 sm:[&>button]:!h-9"
                size="sm"
              />
            </div>
          </div>
        </div>

        {deleteError ? (
          <div role="alert" className="border-b border-red-200 bg-red-50 px-4 py-2.5 text-sm text-red-700 dark:!border-[#642930] dark:!bg-[#32191d] dark:!text-[#ef8a93]">
            {deleteError}
          </div>
        ) : null}

        <div className="hidden min-h-[38px] grid-cols-[minmax(320px,1fr)_130px_150px_78px] items-center border-b border-slate-200 bg-slate-50 text-[11px] font-medium text-slate-400 lg:grid xl:grid-cols-[minmax(300px,1.05fr)_minmax(220px,.8fr)_130px_150px_78px] dark:!border-[#292c32] dark:!bg-[#17191d] dark:!text-[#737780]">
          <div className="px-[18px]">{t("products.table.product")}</div>
          <div className="hidden px-[18px] xl:block">{t("products.table.coverImage")}</div>
          <div className="px-[18px]">{t("products.table.created")}</div>
          <div className="px-[18px]">{t("products.table.updated")}</div>
          <div className="px-3 text-center">{t("products.table.actions")}</div>
        </div>

        {isLoading ? (
          <ProductListSkeleton />
        ) : isError ? (
          <StatePanel
            icon={<AlertCircle size={19} aria-hidden="true" />}
            title={t("products.loadFailed")}
            action={
              <button type="button" onClick={onRetry} className={secondaryActionClassName}>
                <RefreshCw size={15} aria-hidden="true" />
                {t("products.retry")}
              </button>
            }
          />
        ) : products.length ? (
          <div role="list" aria-label={t("products.listTitle")} className="divide-y divide-slate-200 dark:!divide-[#292c32]">
            {products.map((product) => (
              <ProductRow
                key={product.id}
                product={product}
                deletionEnabled={deletionEnabled}
                isDeleting={isDeleting}
                onDelete={() => onDelete(product)}
              />
            ))}
          </div>
        ) : hasSearch ? (
          <StatePanel
            icon={<Search size={19} aria-hidden="true" />}
            title={t("products.filteredEmptyTitle")}
            description={t("products.filteredEmptyDescription")}
            action={
              <button type="button" onClick={onClearSearch} className={secondaryActionClassName}>
                <X size={15} aria-hidden="true" />
                {t("products.searchClear")}
              </button>
            }
          />
        ) : (
          <StatePanel
            icon={<ImageIcon size={19} aria-hidden="true" />}
            title={t("products.emptyTitle")}
            description={t("products.emptyDescription")}
            action={
              <Link to="/products/new" className={primaryActionClassName}>
                <Plus size={15} aria-hidden="true" />
                {t("products.new")}
              </Link>
            }
          />
        )}

        {!isLoading && !isError && totalPages > 1 ? (
          <footer className="flex min-h-12 items-center justify-between border-t border-slate-200 px-3 py-1.5 text-xs tabular-nums text-slate-400 sm:px-[18px] dark:!border-[#292c32] dark:!text-[#737780]">
            <span>{t("products.pageRange", { start: rangeStart, end: rangeEnd, total })}</span>
            <nav className="inline-flex items-center gap-1" aria-label={t("products.paginationLabel")}>
              <button
                type="button"
                onClick={() => onPageChange(Math.max(1, page - 1))}
                disabled={isRefreshing || page <= 1}
                aria-label={t("pagination.previous")}
                title={t("pagination.previous")}
                className={paginationButtonClassName}
              >
                <ChevronLeft size={16} aria-hidden="true" />
              </button>
              <span className="min-w-12 text-center text-slate-500 dark:!text-[#a4a8b0]">
                {page} / {totalPages}
              </span>
              <button
                type="button"
                onClick={() => onPageChange(Math.min(totalPages, page + 1))}
                disabled={isRefreshing || page >= totalPages}
                aria-label={t("pagination.next")}
                title={t("pagination.next")}
                className={paginationButtonClassName}
              >
                <ChevronRight size={16} aria-hidden="true" />
              </button>
            </nav>
          </footer>
        ) : null}
      </div>
    </section>
  );
}

interface ProductRowProps {
  product: ProductSummary;
  deletionEnabled: boolean;
  isDeleting: boolean;
  onDelete: () => void;
}

function ProductRow({
  product,
  deletionEnabled,
  isDeleting,
  onDelete,
}: ProductRowProps) {
  const { locale, t } = useI18n();
  const metadata = [
    product.category,
    product.price ? formatPrice(product.price) : null,
  ].filter((value): value is string => Boolean(value));
  const metadataText = metadata.join(" · ");
  const compactMetadata = [...metadata, product.cover_image_filename].filter(
    (value): value is string => Boolean(value),
  );
  const compactMetadataText = compactMetadata.join(" · ");
  const createdDate = formatShortDate(product.created_at, locale);
  const createdTime = formatTime(product.created_at, locale);
  const updatedDate = formatShortDate(product.updated_at, locale);
  const updatedTime = formatTime(product.updated_at, locale);

  return (
    <article role="listitem" className="group relative isolate grid min-h-[112px] grid-cols-[minmax(0,1fr)_70px] items-center transition-colors hover:bg-slate-50 focus-within:bg-slate-50 lg:min-h-[88px] lg:grid-cols-[minmax(320px,1fr)_130px_150px_78px] xl:grid-cols-[minmax(300px,1.05fr)_minmax(220px,.8fr)_130px_150px_78px] dark:hover:!bg-[#181b21] dark:focus-within:!bg-[#181b21]">
      <span className="pointer-events-none absolute top-3 bottom-3 left-[-1px] z-[2] w-0.5 rounded-r-sm bg-transparent transition-colors group-hover:bg-indigo-600 group-focus-within:bg-indigo-600 dark:group-hover:!bg-[#7b83e6] dark:group-focus-within:!bg-[#7b83e6]" aria-hidden="true" />
      <Link
        to={`/products/${product.id}`}
        aria-label={t("products.openProduct", { name: product.name })}
        className="absolute inset-0 z-[1] outline-none focus-visible:shadow-[inset_0_0_0_2px_rgb(79_70_229)] dark:focus-visible:!shadow-[inset_0_0_0_2px_#7b83e6]"
      />

      <div className="relative z-0 col-start-1 row-start-1 flex min-w-0 items-center gap-3 px-3 py-3 pr-2 sm:gap-3.5 md:px-[18px] lg:gap-3.5">
        <ProductThumbnail product={product} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold text-slate-950 transition-colors group-hover:text-indigo-700 group-focus-within:text-indigo-700 sm:text-[15px] dark:!text-[#f1f2f4] dark:group-hover:!text-[#aeb2ff] dark:group-focus-within:!text-[#aeb2ff]" title={product.name}>
            {product.name}
          </div>
          {metadataText ? (
            <div className="mt-1 flex min-w-0 items-center gap-1.5 text-[11px] text-slate-400 sm:text-xs dark:!text-[#737780]">
              {compactMetadataText ? (
                <span className="hidden min-w-0 truncate sm:inline xl:hidden" title={compactMetadataText}>
                  {compactMetadataText}
                </span>
              ) : null}
              <span className="hidden min-w-0 truncate xl:inline" title={metadataText}>
                {metadataText}
              </span>
              <span className="hidden shrink-0 md:inline lg:hidden">·</span>
              <span className="shrink-0 tabular-nums lg:hidden">{updatedDate}</span>
            </div>
          ) : (
            <div className="mt-1 flex min-w-0 items-center gap-1.5 text-[11px] text-slate-400 sm:text-xs xl:hidden dark:!text-[#737780]">
              {compactMetadataText ? (
                <span className="hidden min-w-0 truncate sm:inline" title={compactMetadataText}>
                  {compactMetadataText}
                </span>
              ) : null}
              {compactMetadataText ? <span className="hidden shrink-0 md:inline lg:hidden">·</span> : null}
              <span className="shrink-0 tabular-nums lg:hidden">{updatedDate}</span>
            </div>
          )}
        </div>
      </div>

      <div className="relative z-0 hidden min-w-0 px-[18px] text-xs text-slate-500 xl:col-start-2 xl:row-start-1 xl:block dark:!text-[#a4a8b0]">
        <span className="block truncate" title={product.cover_image_filename ?? undefined}>
          {product.cover_image_filename ?? "--"}
        </span>
      </div>

      <div className="relative z-0 hidden min-w-0 px-[18px] text-xs tabular-nums text-slate-500 lg:col-start-2 lg:row-start-1 lg:block xl:col-start-3 dark:!text-[#a4a8b0]">
        <span className="block">{createdDate}</span>
        <span className="mt-0.5 block text-slate-400 dark:!text-[#737780]">{createdTime}</span>
      </div>

      <div className="relative z-0 hidden min-w-0 px-[18px] text-xs tabular-nums text-slate-500 lg:col-start-3 lg:row-start-1 lg:block xl:col-start-4 dark:!text-[#a4a8b0]">
        <span className="block">{updatedDate}</span>
        <span className="mt-0.5 block text-slate-400 dark:!text-[#737780]">{updatedTime}</span>
      </div>

      <div className="pointer-events-none relative z-10 col-start-2 row-start-1 flex h-full items-center justify-end gap-0 pr-2 lg:col-start-4 lg:gap-1 lg:pr-3 xl:col-start-5">
        <button
          type="button"
          onClick={onDelete}
          disabled={isDeleting || !deletionEnabled}
          aria-label={deletionEnabled ? t("products.deleteProduct", { name: product.name }) : t("products.deleteDisabled")}
          title={deletionEnabled ? t("products.delete") : t("products.deleteDisabled")}
          className="pointer-events-auto inline-flex h-11 w-11 items-center justify-center rounded-md border border-transparent text-slate-400 transition-[background-color,border-color,color] hover:border-red-200 hover:bg-red-50 hover:text-red-600 focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 disabled:cursor-not-allowed disabled:text-slate-300 disabled:hover:border-transparent disabled:hover:bg-transparent lg:h-[34px] lg:w-[34px] dark:!text-[#8a8f98] dark:hover:!border-[#642930] dark:hover:!bg-[#32191d] dark:hover:!text-[#ef6a75] dark:disabled:!text-[#4a4e56]"
        >
          <Trash2 size={16} aria-hidden="true" />
        </button>
        <ChevronRight
          size={17}
          className="text-slate-400 transition-[color,transform] group-hover:translate-x-0.5 group-hover:text-indigo-600 group-focus-within:translate-x-0.5 group-focus-within:text-indigo-600 motion-reduce:transform-none dark:!text-[#4a4e56] dark:group-hover:!text-[#7b83e6] dark:group-focus-within:!text-[#7b83e6]"
          aria-hidden="true"
        />
      </div>
    </article>
  );
}

function ProductThumbnail({ product }: { product: ProductSummary }) {
  const [failed, setFailed] = useState(false);
  const source = product.cover_image_thumbnail_url ?? product.cover_image_preview_url;

  return (
    <div className="flex h-20 w-20 shrink-0 items-center justify-center overflow-hidden rounded-md border border-slate-300 bg-slate-50 text-slate-400 transition-colors group-hover:border-indigo-300 group-focus-within:border-indigo-300 lg:h-16 lg:w-16 dark:!border-[#373b43] dark:!bg-[#17191d] dark:!text-[#737780] dark:group-hover:!border-[#6f76c9] dark:group-focus-within:!border-[#6f76c9]">
      {source && !failed ? (
        <img
          src={api.toApiUrl(source)}
          alt={product.cover_image_filename ?? product.name}
          className="h-full w-full object-cover"
          loading="lazy"
          decoding="async"
          onError={() => setFailed(true)}
        />
      ) : (
        <ImageIcon size={18} strokeWidth={1.5} aria-hidden="true" />
      )}
    </div>
  );
}

function ProductListSkeleton() {
  return (
    <div className="divide-y divide-slate-200 dark:!divide-[#292c32]" aria-hidden="true">
      {Array.from({ length: 7 }, (_, index) => (
        <div
          key={index}
          className="grid min-h-[112px] grid-cols-[minmax(0,1fr)_70px] items-center px-3 lg:min-h-[88px] lg:grid-cols-[minmax(320px,1fr)_130px_150px_78px] lg:px-[18px] xl:grid-cols-[minmax(300px,1.05fr)_minmax(220px,.8fr)_130px_150px_78px]"
        >
          <div className="flex items-center gap-3.5">
            <span className="h-20 w-20 shrink-0 animate-pulse rounded-md bg-slate-200 lg:h-16 lg:w-16 dark:!bg-[#24272d]" />
            <span className="grid w-[min(210px,55%)] gap-2">
              <span className="h-3.5 w-3/4 animate-pulse rounded bg-slate-200 dark:!bg-[#24272d]" />
              <span className="h-2.5 w-full animate-pulse rounded bg-slate-200 dark:!bg-[#24272d]" />
            </span>
          </div>
          <span className="hidden h-3 w-40 animate-pulse rounded bg-slate-200 xl:block dark:!bg-[#24272d]" />
          <span className="hidden h-3 w-20 animate-pulse rounded bg-slate-200 lg:block dark:!bg-[#24272d]" />
          <span className="hidden h-3 w-20 animate-pulse rounded bg-slate-200 lg:block dark:!bg-[#24272d]" />
        </div>
      ))}
    </div>
  );
}

function StatePanel({
  icon,
  title,
  description,
  action,
}: {
  icon: ReactNode;
  title: string;
  description?: string;
  action: ReactNode;
}) {
  return (
    <div className="flex min-h-[360px] items-center justify-center px-5 py-12 text-center">
      <div className="w-full max-w-sm">
        <span className="inline-flex h-10 w-10 items-center justify-center rounded-lg border border-slate-200 bg-slate-50 text-slate-500 dark:!border-[#292c32] dark:!bg-[#17191d] dark:!text-[#a4a8b0]">
          {icon}
        </span>
        <h2 className="mt-3.5 text-base font-semibold text-slate-950 dark:!text-[#f1f2f4]">{title}</h2>
        {description ? <p className="mt-1.5 text-[13px] text-slate-500 dark:!text-[#a4a8b0]">{description}</p> : null}
        <div className="mt-[18px] flex justify-center">{action}</div>
      </div>
    </div>
  );
}

function formatTime(value: string | null | undefined, locale: string): string {
  if (!value) {
    return "--";
  }
  return new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit" }).format(new Date(value));
}

const primaryActionClassName =
  "inline-flex min-h-[38px] items-center justify-center gap-1.5 rounded-md border border-indigo-600 bg-indigo-600 px-3.5 text-sm font-semibold text-white transition-colors hover:border-indigo-500 hover:bg-indigo-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 dark:!border-[#7b83e6] dark:!bg-[#7b83e6] dark:hover:!border-[#9298f0] dark:hover:!bg-[#9298f0] dark:focus-visible:!ring-[#7b83e6] dark:focus-visible:!ring-offset-[#111316]";

const secondaryActionClassName =
  "inline-flex min-h-[38px] items-center justify-center gap-1.5 rounded-md border border-slate-300 bg-white px-3.5 text-sm font-semibold text-slate-600 transition-colors hover:bg-slate-50 hover:text-slate-950 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:!border-[#373b43] dark:!bg-[#15171b] dark:!text-[#a4a8b0] dark:hover:!bg-[#1d2025] dark:hover:!text-[#f1f2f4] dark:focus-visible:!ring-[#7b83e6]";

const paginationButtonClassName =
  "inline-flex h-11 w-11 items-center justify-center rounded-md border border-slate-200 bg-white text-slate-500 transition-colors hover:border-slate-300 hover:bg-slate-50 hover:text-slate-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 disabled:cursor-not-allowed disabled:opacity-40 sm:h-[34px] sm:w-[34px] dark:!border-[#373b43] dark:!bg-[#15171b] dark:!text-[#a4a8b0] dark:hover:!bg-[#1d2025] dark:hover:!text-[#f1f2f4] dark:focus-visible:!ring-[#7b83e6]";
