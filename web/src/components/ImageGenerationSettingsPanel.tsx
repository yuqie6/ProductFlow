import { ImageToolControls } from "./ImageToolControls";
import { ImageSizePicker } from "./ImageSizePicker";
import { Select as SelectField } from "./ui/select";
import type { ImageSizeOption } from "../lib/imageSizes";
import { formatImageSizeValue } from "../lib/imageSizes";
import { useI18n } from "../lib/preferences";
import type { ImageToolOptionKey, ImageToolOptions } from "../lib/types";

interface ImageGenerationSettingsPanelProps {
  size: string;
  sizeOptions: ImageSizeOption[];
  maxDimension: number;
  toolOptions: ImageToolOptions;
  allowedToolFields: readonly ImageToolOptionKey[];
  onSizeChange: (size: string) => void;
  onToolOptionsChange: (toolOptions: ImageToolOptions) => void;
  surface?: "card" | "plain";
  generationCount?: number;
  generationCountOptions?: readonly number[];
  generationCountLabel?: string;
  generationCountDescription?: string;
  onGenerationCountChange?: (count: number) => void;
  showToolOptions?: boolean;
}

export function ImageGenerationSettingsPanel({
  size,
  sizeOptions,
  maxDimension,
  toolOptions,
  allowedToolFields,
  onSizeChange,
  onToolOptionsChange,
  surface = "card",
  generationCount,
  generationCountOptions,
  generationCountLabel,
  generationCountDescription,
  onGenerationCountChange,
  showToolOptions = true,
}: ImageGenerationSettingsPanelProps) {
  const { t } = useI18n();
  const showCount = generationCount !== undefined && generationCountOptions?.length && onGenerationCountChange;
  const containerClassName = surface === "card" ? "rounded-2xl border border-border-l1 bg-surface-raised p-4" : "space-y-3";

  return (
    <div className={containerClassName}>
      <div className="mb-3 flex items-center justify-between gap-3">
        <div className="text-sm font-semibold text-text-primary">{t("imageSettings.title")}</div>
        <span className="text-[11px] font-medium text-text-muted">{formatImageSizeValue(size)}</span>
      </div>
      <ImageSizePicker value={size} presets={sizeOptions} maxDimension={maxDimension} onChange={onSizeChange} />
      {showCount ? (
        <label className="mt-3 block" htmlFor="image-generation-count">
          <span className="mb-1.5 block text-xs font-semibold text-text-secondary">
            {generationCountLabel ?? t("imageSettings.count")}
          </span>
          {generationCountDescription ? (
            <span className="mb-1.5 block text-[11px] leading-5 text-text-muted">{generationCountDescription}</span>
          ) : null}
          <SelectField
            id="image-generation-count"
            value={String(generationCount)}
            options={generationCountOptions.map((count) => ({
              value: String(count),
              label: t("imageSettings.candidateCount", { count }),
            }))}
            onChange={(nextValue) => onGenerationCountChange(Number(nextValue))}
          />
        </label>
      ) : null}
      {showToolOptions ? (
        <div className="mt-3">
          <ImageToolControls
            surface="plain"
            value={toolOptions}
            allowedFields={allowedToolFields}
            onChange={onToolOptionsChange}
          />
        </div>
      ) : null}
    </div>
  );
}
