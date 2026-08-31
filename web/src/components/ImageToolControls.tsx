import { DEFAULT_IMAGE_TOOL_ALLOWED_FIELDS } from "../lib/imageToolOptions";
import { useI18n } from "../lib/preferences";
import type { ImageToolOptionKey, ImageToolOptions } from "../lib/types";
import { Field, Input } from "./ui/field";
import { Select, type SelectOption } from "./ui/select";

interface ImageToolControlsProps {
  value: ImageToolOptions;
  onChange: (value: ImageToolOptions) => void;
  surface?: "card" | "plain";
  allowedFields?: readonly ImageToolOptionKey[];
}

function parseOptionalNumber(value: string): number | null {
  if (!value.trim()) {
    return null;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : null;
}

function CompactInput({
  label,
  value,
  placeholder,
  inputMode,
  onChange,
}: {
  label: string;
  value: string | number;
  placeholder?: string;
  inputMode?: "text" | "numeric";
  onChange: (value: string) => void;
}) {
  return (
    <Input
      label={label}
      value={value}
      placeholder={placeholder}
      inputMode={inputMode}
      onChange={(event) => onChange(event.target.value)}
    />
  );
}

function CompactSelect({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: readonly SelectOption[];
  onChange: (value: string) => void;
}) {
  return (
    <Field label={label}>
      <Select value={value} options={options} onChange={onChange} ariaLabel={label} size="sm" />
    </Field>
  );
}

export function ImageToolControls({
  value,
  onChange,
  surface = "card",
  allowedFields = DEFAULT_IMAGE_TOOL_ALLOWED_FIELDS,
}: ImageToolControlsProps) {
  const { t } = useI18n();
  const update = (next: Partial<ImageToolOptions>) => onChange({ ...value, ...next });
  const allowed = new Set(allowedFields);
  if (!allowed.size) {
    return null;
  }
  const containerClassName =
    surface === "card" ? "rounded-panel border border-border-l1 bg-surface-raised p-4" : "space-y-3";
  return (
    <div className={containerClassName}>
      <div className="mb-3 text-sm font-semibold text-text-primary">{t("imageTool.provider")}</div>
      <div className="grid grid-cols-2 gap-2">
        {allowed.has("model") ? (
          <CompactInput
            label={t("imageTool.tool")}
            value={value.model ?? ""}
            placeholder={t("imageTool.default")}
            onChange={(next) => update({ model: next || null })}
          />
        ) : null}
        {allowed.has("quality") ? (
          <CompactSelect
            label={t("imageTool.quality")}
            value={value.quality ?? ""}
            onChange={(next) => update({ quality: (next || null) as ImageToolOptions["quality"] })}
            options={[
              { value: "", label: t("imageTool.default") },
              { value: "auto", label: "Auto" },
              { value: "low", label: "Low" },
              { value: "medium", label: "Medium" },
              { value: "high", label: "High" },
            ]}
          />
        ) : null}
        {allowed.has("output_format") ? (
          <CompactSelect
            label={t("imageTool.format")}
            value={value.output_format ?? ""}
            onChange={(next) => update({ output_format: (next || null) as ImageToolOptions["output_format"] })}
            options={[
              { value: "", label: t("imageTool.default") },
              { value: "png", label: "PNG" },
              { value: "jpeg", label: "JPEG" },
              { value: "webp", label: "WebP" },
            ]}
          />
        ) : null}
        {allowed.has("output_compression") ? (
          <CompactInput
            label={t("imageTool.compression")}
            value={value.output_compression ?? ""}
            inputMode="numeric"
            placeholder={t("imageTool.default")}
            onChange={(next) => update({ output_compression: parseOptionalNumber(next) })}
          />
        ) : null}
        {allowed.has("background") ? (
          <CompactSelect
            label={t("imageTool.background")}
            value={value.background ?? ""}
            onChange={(next) => update({ background: (next || null) as ImageToolOptions["background"] })}
            options={[
              { value: "", label: t("imageTool.default") },
              { value: "auto", label: "Auto" },
              { value: "opaque", label: "Opaque" },
              { value: "transparent", label: "Transparent" },
            ]}
          />
        ) : null}
        {allowed.has("moderation") ? (
          <CompactSelect
            label={t("imageTool.moderation")}
            value={value.moderation ?? ""}
            onChange={(next) => update({ moderation: (next || null) as ImageToolOptions["moderation"] })}
            options={[
              { value: "", label: t("imageTool.default") },
              { value: "auto", label: "Auto" },
              { value: "low", label: "Low" },
            ]}
          />
        ) : null}
        {allowed.has("action") ? (
          <CompactSelect
            label="Action"
            value={value.action ?? ""}
            onChange={(next) => update({ action: (next || null) as ImageToolOptions["action"] })}
            options={[
              { value: "", label: t("imageTool.default") },
              { value: "auto", label: "Auto" },
              { value: "generate", label: "Generate" },
              { value: "edit", label: "Edit" },
            ]}
          />
        ) : null}
        {allowed.has("input_fidelity") ? (
          <CompactSelect
            label="Fidelity"
            value={value.input_fidelity ?? ""}
            onChange={(next) => update({ input_fidelity: (next || null) as ImageToolOptions["input_fidelity"] })}
            options={[
              { value: "", label: t("imageTool.default") },
              { value: "low", label: "Low" },
              { value: "high", label: "High" },
            ]}
          />
        ) : null}
        {allowed.has("partial_images") ? (
          <CompactInput
            label="Partial"
            value={value.partial_images ?? ""}
            inputMode="numeric"
            placeholder={t("imageTool.default")}
            onChange={(next) => update({ partial_images: parseOptionalNumber(next) })}
          />
        ) : null}
      </div>
    </div>
  );
}
