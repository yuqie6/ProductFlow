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

import { CONTROL_CLASS } from "../../components/ui/field";
import { cn } from "../../components/ui/cn";
import { buttonVariants } from "../../components/ui/button";
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
        <h1 id="product-list-title" className="min-w-0 text-[22px] font-semibold text-text-primary sm:text-[25px] dark:text-text-primary">
          {t("products.title")}
        </h1>
        <Link
          to="/products/new"
          className={cn(buttonVariants({ variant: "primary", size: "lg" }), "shrink-0 text-sm")}
        >
          <Plus size={16} aria-hidden="true" />
          <span>{t("products.new")}</span>
        </Link>
      </div>

      <div className="relative rounded-lg border border-border-l1 bg-surface-raised dark:border-border-l1 dark:bg-surface-panel">
        <div className="grid grid-cols-1 items-center gap-2.5 border-b border-border-l1 p-2.5 sm:grid-cols-[minmax(220px,360px)_minmax(0,1fr)] sm:gap-4 sm:px-3.5 sm:py-2.5 dark:border-border-l1">
          <label className="relative flex min-w-0 items-center">
            <Search size={15} className="pointer-events-none absolute left-3 text-text-muted" aria-hidden="true" />
            <span className="sr-only">{t("products.searchPlaceholder")}</span>
            <input
              type="search"
              value={searchDraft}
              maxLength={100}
              autoComplete="off"
              onChange={(event) => onSearchDraftChange(event.target.value)}
              placeholder={t("products.searchPlaceholder")}
              className={cn(CONTROL_CLASS, "h-11 min-w-0 appearance-none pr-11 pl-[34px] text-[13px] lg:h-9 lg:pr-9 [&::-webkit-search-cancel-button]:appearance-none")}
            />
            {searchDraft ? (
              <button
                type="button"
                onClick={() => onSearchDraftChange("")}
                aria-label={t("products.searchClear")}
                title={t("products.searchClear")}
                className="absolute right-0 inline-flex h-11 w-11 items-center justify-center rounded-[5px] text-text-muted transition-colors hover:bg-surface-subtle hover:text-text-secondary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent sm:right-1 sm:h-7 sm:w-7 dark:text-text-muted dark:hover:bg-surface-subtle dark:hover:text-text-primary"
              >
                <X size={14} aria-hidden="true" />
              </button>
            ) : null}
          </label>

          <div className="flex min-w-0 items-center justify-between gap-3 px-0.5 sm:justify-self-end sm:pr-1">
            <div
              className="inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap text-xs tabular-nums text-text-muted dark:text-text-muted"
              aria-live="polite"
            >
              {isRefreshing ? (
                <LoaderCircle
                  size={13}
                  className="animate-spin text-accent motion-reduce:animate-none dark:text-accent"
                  aria-label={t("products.refreshing")}
                />
              ) : null}
              <span>{t("products.resultCount", { count: total })}</span>
            </div>
            <div className="inline-flex min-w-0 items-center gap-2 text-xs text-text-muted dark:text-text-secondary">
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
          <div role="alert" className="border-b border-state-error bg-state-error-soft px-4 py-2.5 text-sm text-state-error dark:border-state-error dark:bg-state-error-soft dark:text-state-error">
            {deleteError}
          </div>
        ) : null}

        <div className="hidden min-h-[38px] grid-cols-[minmax(320px,1fr)_130px_150px_78px] items-center border-b border-border-l1 bg-surface-base text-[11px] font-medium text-text-muted lg:grid xl:grid-cols-[minmax(300px,1.05fr)_minmax(220px,.8fr)_130px_150px_78px] dark:border-border-l1 dark:bg-surface-panel dark:text-text-muted">
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
          <div role="list" aria-label={t("products.listTitle")} className="divide-y divide-border-l1 dark:divide-border-l1">
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
          <footer className="flex min-h-12 items-center justify-between border-t border-border-l1 px-3 py-1.5 text-xs tabular-nums text-text-muted sm:px-[18px] dark:border-border-l1 dark:text-text-muted">
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
              <span className="min-w-12 text-center text-text-muted dark:text-text-secondary">
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
    <article role="listitem" className="group relative isolate grid min-h-[112px] grid-cols-[minmax(0,1fr)_70px] items-center transition-colors hover:bg-surface-base focus-within:bg-surface-base lg:min-h-[88px] lg:grid-cols-[minmax(320px,1fr)_130px_150px_78px] xl:grid-cols-[minmax(300px,1.05fr)_minmax(220px,.8fr)_130px_150px_78px] dark:hover:bg-surface-subtle dark:focus-within:bg-surface-subtle">
      <span className="pointer-events-none absolute top-3 bottom-3 left-[-1px] z-[2] w-0.5 rounded-r-sm bg-transparent transition-colors group-hover:bg-accent group-focus-within:bg-accent dark:group-hover:bg-accent-strong dark:group-focus-within:bg-accent" aria-hidden="true" />
      <Link
        to={`/products/${product.id}`}
        aria-label={t("products.openProduct", { name: product.name })}
        className="absolute inset-0 z-[1] outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-focus-ring"
      />

      <div className="relative z-0 col-start-1 row-start-1 flex min-w-0 items-center gap-3 px-3 py-3 pr-2 sm:gap-3.5 md:px-[18px] lg:gap-3.5">
        <ProductThumbnail product={product} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold text-text-primary transition-colors group-hover:text-accent group-focus-within:text-accent sm:text-[15px] dark:text-text-primary dark:group-hover:text-accent-strong dark:group-focus-within:text-accent" title={product.name}>
            {product.name}
          </div>
          {metadataText ? (
            <div className="mt-1 flex min-w-0 items-center gap-1.5 text-[11px] text-text-muted sm:text-xs dark:text-text-muted">
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
            <div className="mt-1 flex min-w-0 items-center gap-1.5 text-[11px] text-text-muted sm:text-xs xl:hidden dark:text-text-muted">
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

      <div className="relative z-0 hidden min-w-0 px-[18px] text-xs text-text-muted xl:col-start-2 xl:row-start-1 xl:block dark:text-text-secondary">
        <span className="block truncate" title={product.cover_image_filename ?? undefined}>
          {product.cover_image_filename ?? "--"}
        </span>
      </div>

      <div className="relative z-0 hidden min-w-0 px-[18px] text-xs tabular-nums text-text-muted lg:col-start-2 lg:row-start-1 lg:block xl:col-start-3 dark:text-text-secondary">
        <span className="block">{createdDate}</span>
        <span className="mt-0.5 block text-text-muted dark:text-text-muted">{createdTime}</span>
      </div>

      <div className="relative z-0 hidden min-w-0 px-[18px] text-xs tabular-nums text-text-muted lg:col-start-3 lg:row-start-1 lg:block xl:col-start-4 dark:text-text-secondary">
        <span className="block">{updatedDate}</span>
        <span className="mt-0.5 block text-text-muted dark:text-text-muted">{updatedTime}</span>
      </div>

      <div className="pointer-events-none relative z-10 col-start-2 row-start-1 flex h-full items-center justify-end gap-0 pr-2 lg:col-start-4 lg:gap-1 lg:pr-3 xl:col-start-5">
        <button
          type="button"
          onClick={onDelete}
          disabled={isDeleting || !deletionEnabled}
          aria-label={deletionEnabled ? t("products.deleteProduct", { name: product.name }) : t("products.deleteDisabled")}
          title={deletionEnabled ? t("products.delete") : t("products.deleteDisabled")}
          className="pointer-events-auto inline-flex h-11 w-11 items-center justify-center rounded-md border border-transparent text-text-muted transition-[background-color,border-color,color] hover:border-state-error hover:bg-state-error-soft hover:text-state-error focus:outline-none focus-visible:ring-2 focus-visible:ring-state-error disabled:cursor-not-allowed disabled:text-text-muted disabled:hover:border-transparent disabled:hover:bg-transparent lg:h-[34px] lg:w-[34px] dark:text-text-muted dark:hover:border-state-error dark:hover:bg-state-error-soft dark:hover:text-state-error dark:disabled:text-text-muted"
        >
          <Trash2 size={16} aria-hidden="true" />
        </button>
        <ChevronRight
          size={17}
          className="text-text-muted transition-[color,transform] group-hover:translate-x-0.5 group-hover:text-accent group-focus-within:translate-x-0.5 group-focus-within:text-accent motion-reduce:transform-none dark:text-text-muted dark:group-hover:text-accent-strong dark:group-focus-within:text-accent"
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
    <div className="flex h-20 w-20 shrink-0 items-center justify-center overflow-hidden rounded-md border border-border-l3 bg-surface-base text-text-muted transition-colors group-hover:border-accent group-focus-within:border-accent lg:h-16 lg:w-16 dark:border-border-l3 dark:bg-surface-panel dark:text-text-muted dark:group-hover:border-accent-strong dark:group-focus-within:border-accent">
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
    <div className="divide-y divide-border-l1 dark:divide-border-l1" aria-hidden="true">
      {Array.from({ length: 7 }, (_, index) => (
        <div
          key={index}
          className="grid min-h-[112px] grid-cols-[minmax(0,1fr)_70px] items-center px-3 lg:min-h-[88px] lg:grid-cols-[minmax(320px,1fr)_130px_150px_78px] lg:px-[18px] xl:grid-cols-[minmax(300px,1.05fr)_minmax(220px,.8fr)_130px_150px_78px]"
        >
          <div className="flex items-center gap-3.5">
            <span className="h-20 w-20 shrink-0 animate-pulse rounded-md bg-surface-subtle lg:h-16 lg:w-16 dark:bg-surface-subtle" />
            <span className="grid w-[min(210px,55%)] gap-2">
              <span className="h-3.5 w-3/4 animate-pulse rounded bg-surface-subtle dark:bg-surface-subtle" />
              <span className="h-2.5 w-full animate-pulse rounded bg-surface-subtle dark:bg-surface-subtle" />
            </span>
          </div>
          <span className="hidden h-3 w-40 animate-pulse rounded bg-surface-subtle xl:block dark:bg-surface-subtle" />
          <span className="hidden h-3 w-20 animate-pulse rounded bg-surface-subtle lg:block dark:bg-surface-subtle" />
          <span className="hidden h-3 w-20 animate-pulse rounded bg-surface-subtle lg:block dark:bg-surface-subtle" />
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
        <span className="inline-flex h-10 w-10 items-center justify-center rounded-lg border border-border-l1 bg-surface-base text-text-muted dark:border-border-l1 dark:bg-surface-panel dark:text-text-secondary">
          {icon}
        </span>
        <h2 className="mt-3.5 text-base font-semibold text-text-primary dark:text-text-primary">{title}</h2>
        {description ? <p className="mt-1.5 text-[13px] text-text-muted dark:text-text-secondary">{description}</p> : null}
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
  "inline-flex min-h-[38px] items-center justify-center gap-1.5 rounded-md border border-accent bg-accent px-3.5 text-sm font-semibold text-accent-fg transition-colors hover:border-accent hover:bg-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 dark:border-accent dark:bg-accent dark:hover:border-accent-strong dark:hover:bg-accent-strong dark:focus-visible:ring-accent dark:focus-visible:ring-offset-surface-panel";

const secondaryActionClassName =
  "inline-flex min-h-[38px] items-center justify-center gap-1.5 rounded-md border border-border-l3 bg-surface-raised px-3.5 text-sm font-semibold text-text-secondary transition-colors hover:bg-surface-base hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:border-border-l3 dark:bg-surface-panel dark:text-text-secondary dark:hover:bg-surface-subtle dark:hover:text-text-primary dark:focus-visible:ring-accent";

const paginationButtonClassName =
  "inline-flex h-11 w-11 items-center justify-center rounded-md border border-border-l1 bg-surface-raised text-text-muted transition-colors hover:border-border-l3 hover:bg-surface-base hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-not-allowed disabled:opacity-40 sm:h-[34px] sm:w-[34px] dark:border-border-l3 dark:bg-surface-panel dark:text-text-secondary dark:hover:bg-surface-subtle dark:hover:text-text-primary dark:focus-visible:ring-accent";
