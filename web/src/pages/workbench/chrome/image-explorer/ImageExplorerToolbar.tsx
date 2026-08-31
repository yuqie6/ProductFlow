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

import { cn } from "../../../../components/ui/cn";
import { CONTROL_CLASS } from "../../../../components/ui/field";
import { IconButton } from "../../../../components/ui/icon-button";
import { Select } from "../../../../components/ui/select";
import { Tooltip } from "../../../../components/ui/tooltip";
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
  const directoryToggleLabel = directoryOpen ? t("detail.library.closeDirectories") : t("detail.library.openDirectories");
  return (
    <div className="space-y-2 border-b border-border-l1 pb-3">
      <div className="flex min-w-0 items-center gap-1.5">
        {showDirectoryToggle ? (
          <IconButton
            label={directoryToggleLabel}
            variant="secondary"
            size="sm"
            className="h-11 w-11 lg:h-8 lg:w-8"
            aria-expanded={directoryOpen}
            onClick={onToggleDirectory}
          >
            {directoryOpen ? <X size={15} /> : <FolderTree size={15} />}
          </IconButton>
        ) : null}
        <div className="min-w-0 flex-1 truncate text-xs font-semibold text-text-primary" title={directoryLabel}>
          {directoryLabel}
        </div>
        <IconButton
          label={t("detail.library.createFolder")}
          variant="secondary"
          size="sm"
          className="h-11 w-11 lg:h-8 lg:w-8"
          disabled={busy}
          onClick={onCreateFolder}
        >
          <Plus size={15} />
        </IconButton>
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
        <Tooltip content={t("detail.library.upload")} disabled={busy}>
          <label
            htmlFor={uploadInputId}
            aria-disabled={busy}
            className="inline-flex h-11 w-11 shrink-0 cursor-pointer items-center justify-center rounded-control bg-accent text-accent-fg hover:bg-accent-strong focus-within:ring-2 focus-within:ring-focus-ring aria-disabled:cursor-wait aria-disabled:opacity-50 lg:h-8 lg:w-8"
            aria-label={t("detail.library.upload")}
          >
            <Upload size={15} />
          </label>
        </Tooltip>
      </div>

      <div className="flex min-w-0 items-center gap-1.5">
        <label className="relative min-w-0 flex-1">
          <span className="sr-only">{t("detail.library.search")}</span>
          <Search size={14} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-text-muted" />
          <input
            type="search"
            value={search}
            onChange={(event) => onSearchChange(event.target.value)}
            placeholder={t("detail.library.search")}
            className={cn(CONTROL_CLASS, "h-8 pl-8 pr-2")}
          />
        </label>
        <div className="min-w-0 max-w-[132px] flex-1">
          <Select
            size="sm"
            className="h-8"
            value={sort}
            onChange={(value) => onSortChange(value as GalleryAssetSort)}
            ariaLabel={t("products.sort.label")}
            options={[
              { value: "created_desc", label: t("detail.library.sortNewest") },
              { value: "created_asc", label: t("detail.library.sortOldest") },
              { value: "name_asc", label: t("detail.library.sortNameAsc") },
              { value: "name_desc", label: t("detail.library.sortNameDesc") },
            ]}
          />
        </div>
        <div className="flex h-8 shrink-0 overflow-hidden rounded-control border border-border-l1">
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
    <Tooltip content={label}>
      <button
        type="button"
        onClick={onClick}
        className={`inline-flex h-full w-8 items-center justify-center ${
          active
            ? "bg-accent-soft text-accent"
            : "bg-surface-raised text-text-muted hover:text-text-primary"
        }`}
        aria-label={label}
        aria-pressed={active}
      >
        {icon}
      </button>
    </Tooltip>
  );
}
