import {
  FolderTree,
  Grid2X2,
  List,
  Plus,
  Search,
  Upload,
  X,
} from "lucide-react";
import { useId } from "react";

import { useI18n } from "../../../../lib/preferences";
import type { GalleryAssetSort } from "../../../../lib/types";
import type { ImageExplorerView } from "./explorerState";

interface ImageExplorerToolbarProps {
  directoryLabel: string;
  search: string;
  sort: GalleryAssetSort;
  view: ImageExplorerView;
  directoryOpen: boolean;
  showDirectoryToggle: boolean;
  busy: boolean;
  onSearchChange: (value: string) => void;
  onSortChange: (value: GalleryAssetSort) => void;
  onViewChange: (value: ImageExplorerView) => void;
  onToggleDirectory: () => void;
  onCreateFolder: () => void;
  onUpload: (files: File[]) => void;
}

export function ImageExplorerToolbar({
  directoryLabel,
  search,
  sort,
  view,
  directoryOpen,
  showDirectoryToggle,
  busy,
  onSearchChange,
  onSortChange,
  onViewChange,
  onToggleDirectory,
  onCreateFolder,
  onUpload,
}: ImageExplorerToolbarProps) {
  const { t } = useI18n();
  const uploadInputId = useId();
  return (
    <div className="space-y-2 border-b border-slate-200 pb-3 dark:border-slate-800">
      <div className="flex min-w-0 items-center gap-1.5">
        {showDirectoryToggle ? (
          <button
            type="button"
            onClick={onToggleDirectory}
            className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:text-slate-950 dark:border-slate-700 dark:bg-slate-950/60 dark:text-slate-300 dark:hover:text-white"
            title={directoryOpen ? t("detail.library.closeDirectories") : t("detail.library.openDirectories")}
            aria-label={directoryOpen ? t("detail.library.closeDirectories") : t("detail.library.openDirectories")}
            aria-expanded={directoryOpen}
          >
            {directoryOpen ? <X size={15} /> : <FolderTree size={15} />}
          </button>
        ) : null}
        <div className="min-w-0 flex-1 truncate text-xs font-semibold text-slate-800 dark:text-slate-100" title={directoryLabel}>
          {directoryLabel}
        </div>
        <button
          type="button"
          onClick={onCreateFolder}
          disabled={busy}
          className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:text-slate-950 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-950/60 dark:text-slate-300 dark:hover:text-white"
          title={t("detail.library.createFolder")}
          aria-label={t("detail.library.createFolder")}
        >
          <Plus size={15} />
        </button>
        <input
          id={uploadInputId}
          type="file"
          accept="image/png,image/jpeg,image/webp"
          multiple
          disabled={busy}
          className="sr-only"
          onChange={(event) => {
            const files = Array.from(event.target.files ?? []);
            if (files.length) {
              onUpload(files);
            }
            event.currentTarget.value = "";
          }}
        />
        <label
          htmlFor={uploadInputId}
          aria-disabled={busy}
          className="inline-flex h-8 w-8 shrink-0 cursor-pointer items-center justify-center rounded-md bg-indigo-600 text-white hover:bg-indigo-500 focus-within:ring-2 focus-within:ring-indigo-400 disabled:opacity-50 aria-disabled:cursor-wait aria-disabled:opacity-50 dark:bg-violet-500 dark:hover:bg-violet-400"
          title={t("detail.library.upload")}
          aria-label={t("detail.library.upload")}
        >
          <Upload size={15} />
        </label>
      </div>

      <div className="flex min-w-0 items-center gap-1.5">
        <label className="relative min-w-0 flex-1">
          <span className="sr-only">{t("detail.library.search")}</span>
          <Search size={14} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-slate-400" />
          <input
            type="search"
            value={search}
            onChange={(event) => onSearchChange(event.target.value)}
            placeholder={t("detail.library.search")}
            className="h-8 w-full rounded-md border border-slate-200 bg-white pl-8 pr-2 text-xs text-slate-900 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100 dark:border-slate-700 dark:bg-slate-950/60 dark:text-white dark:focus:border-violet-400 dark:focus:ring-violet-500/15"
          />
        </label>
        <label className="min-w-0 max-w-[132px] flex-1">
          <span className="sr-only">{t("products.sort.label")}</span>
          <select
            value={sort}
            onChange={(event) => onSortChange(event.target.value as GalleryAssetSort)}
            className="h-8 w-full rounded-md border border-slate-200 bg-white px-2 text-xs text-slate-700 outline-none focus:border-indigo-400 dark:border-slate-700 dark:bg-slate-950/60 dark:text-slate-200 dark:focus:border-violet-400"
          >
            <option value="created_desc">{t("detail.library.sortNewest")}</option>
            <option value="created_asc">{t("detail.library.sortOldest")}</option>
            <option value="name_asc">{t("detail.library.sortNameAsc")}</option>
            <option value="name_desc">{t("detail.library.sortNameDesc")}</option>
          </select>
        </label>
        <div className="flex h-8 shrink-0 overflow-hidden rounded-md border border-slate-200 dark:border-slate-700">
          <ViewButton
            active={view === "grid"}
            label={t("detail.library.gridView")}
            onClick={() => onViewChange("grid")}
            icon={<Grid2X2 size={14} />}
          />
          <ViewButton
            active={view === "list"}
            label={t("detail.library.listView")}
            onClick={() => onViewChange("list")}
            icon={<List size={14} />}
          />
        </div>
      </div>
    </div>
  );
}

function ViewButton({ active, label, icon, onClick }: { active: boolean; label: string; icon: React.ReactNode; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`inline-flex h-full w-8 items-center justify-center ${
        active
          ? "bg-indigo-50 text-indigo-700 dark:bg-violet-500/20 dark:text-violet-100"
          : "bg-white text-slate-400 hover:text-slate-800 dark:bg-slate-950/60 dark:text-slate-500 dark:hover:text-white"
      }`}
      title={label}
      aria-label={label}
      aria-pressed={active}
    >
      {icon}
    </button>
  );
}
