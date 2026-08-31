import { useState, type ReactNode } from "react";

import { ImageAspectRatioPicker } from "../../../components/ImageAspectRatioPicker";
import { ImageGenerationSettingsTabs, type ImageGenerationSettingsTab } from "../../../components/ImageGenerationSettingsTabs";
import { Field, Input, NumberInput, TextArea } from "../../../components/ui/field";
import { Select } from "../../../components/ui/select";
import type { TranslationKey } from "../../../lib/i18n";
import { useI18n } from "../../../lib/preferences";
import type { GraphCatalogConfigField } from "../../../lib/types";
import {
  catalogFieldVisible,
  catalogLabelKey,
  getConfigPath,
  patchCatalogValue,
  readStringListText,
  readText,
  readVisualBackground,
  visibleCatalogFields,
  visualBackgroundValueMaxLength,
  writeVisualBackground,
} from "./catalogConfig";

const SELECT_OPTION_LABEL_KEYS = {
  none: "agentWorkbench.nodeEditor.option.none",
  allowed: "agentWorkbench.nodeEditor.option.allowed",
  required: "agentWorkbench.nodeEditor.option.required",
  standard: "agentWorkbench.nodeEditor.option.standard",
  high: "agentWorkbench.nodeEditor.option.high",
  ultra: "agentWorkbench.nodeEditor.option.ultra",
  draft: "agentWorkbench.nodeEditor.option.draft",
  low: "agentWorkbench.nodeEditor.option.low",
  medium: "agentWorkbench.nodeEditor.option.medium",
  auto: "agentWorkbench.nodeEditor.option.auto",
  opaque: "agentWorkbench.nodeEditor.option.opaque",
  transparent: "agentWorkbench.nodeEditor.option.transparent",
  allow: "agentWorkbench.nodeEditor.option.allow",
  contain: "agentWorkbench.nodeEditor.option.contain",
  cover: "agentWorkbench.nodeEditor.option.cover",
  center: "agentWorkbench.nodeEditor.option.center",
  top: "agentWorkbench.nodeEditor.option.top",
  bottom: "agentWorkbench.nodeEditor.option.bottom",
  left: "agentWorkbench.nodeEditor.option.left",
  right: "agentWorkbench.nodeEditor.option.right",
} as const;

interface CatalogRenderItem {
  field: GraphCatalogConfigField;
  path: string[];
  groupLabelKey?: string | null;
}

export function CatalogConfigFields({
  fields,
  value,
  onChange,
  disabled = false,
}: {
  fields: GraphCatalogConfigField[];
  value: Record<string, unknown>;
  onChange: (value: Record<string, unknown>) => void;
  disabled?: boolean;
}) {
  const { t } = useI18n();
  const [settingsTab, setSettingsTab] = useState<ImageGenerationSettingsTab>("basic");
  const { unpaneled, basic, advanced, tabsTitleKey } = collectRenderItems(fields);
  const setPath = (path: string[], nextValue: unknown) => {
    onChange(patchCatalogValue(fields, value, path, nextValue));
  };
  return (
    <>
      {unpaneled.map((item) => (
        <CatalogField
          key={item.path.join(".")}
          item={item}
          config={value}
          onPatch={setPath}
          disabled={disabled}
        />
      ))}
      {basic.length || advanced.length ? (
        <FieldGroup title={tabsTitleKey ? t(tabsTitleKey) : t("agentWorkbench.nodeEditor.generationSettings")}>
          <ImageGenerationSettingsTabs
            value={settingsTab}
            onChange={setSettingsTab}
            basic={(
              <div className="space-y-4">
                {basic.map((item) => (
                  <CatalogField
                    key={item.path.join(".")}
                    item={item}
                    config={value}
                    onPatch={setPath}
                    disabled={disabled}
                  />
                ))}
              </div>
            )}
            advanced={(
              <div className="space-y-4">
                {advanced.map((item) => (
                  <CatalogField
                    key={item.path.join(".")}
                    item={item}
                    config={value}
                    onPatch={setPath}
                    disabled={disabled}
                  />
                ))}
              </div>
            )}
          />
        </FieldGroup>
      ) : null}
    </>
  );
}

function CatalogField({
  item,
  config,
  onPatch,
  disabled,
}: {
  item: CatalogRenderItem;
  config: Record<string, unknown>;
  onPatch: (path: string[], value: unknown) => void;
  disabled: boolean;
}) {
  const { t } = useI18n();
  const parent = item.path.length > 1 ? getConfigPath(config, item.path.slice(0, -1)) : config;
  if (!catalogFieldVisible(item.field, parent)) return null;
  const raw = getConfigPath(config, item.path);
  const label = catalogLabelKey(item.field.label_key);
  const fieldLabel = label ? t(label) : humanizeCatalogKey(item.field.key);
  const hint = catalogLabelKey(item.field.hint_key);
  const nested = visibleCatalogFields(item.field.fields ?? []);
  if (item.field.control === "group") {
    const body = (
      <>
        {hint ? <p className="text-[11px] leading-5 text-text-muted">{t(hint)}</p> : null}
        {nested.map((child) => (
          <CatalogField
            key={[...item.path, child.key].join(".")}
            item={{ field: child, path: [...item.path, child.key] }}
            config={config}
            onPatch={onPatch}
            disabled={disabled}
          />
        ))}
      </>
    );
    if (!label) return <div className="space-y-3">{body}</div>;
    return <FieldGroup title={t(label)}>{body}</FieldGroup>;
  }
  if (item.field.control === "optional_object") {
    const enabled = isRecord(raw);
    const toggleKey = catalogLabelKey(item.field.toggle_label_key) ?? label;
    return (
      <fieldset className="space-y-3 border-t border-border-l1 pt-4">
        {label ? <legend className="mb-1 text-xs font-semibold text-text-primary">{t(label)}</legend> : null}
        {toggleKey ? (
          <CheckboxField
            label={t(toggleKey)}
            checked={enabled}
            disabled={disabled}
            onChange={(nextEnabled) => onPatch(item.path, nextEnabled ? cloneDefault(item.field) : null)}
          />
        ) : null}
        {enabled ? nested.map((child) => (
          <CatalogField
            key={[...item.path, child.key].join(".")}
            item={{ field: child, path: [...item.path, child.key] }}
            config={config}
            onPatch={onPatch}
            disabled={disabled}
          />
        )) : null}
      </fieldset>
    );
  }
  if (item.field.control === "aspect_ratio") {
    return (
      <div>
        {label ? <div className="mb-2 text-[11px] font-semibold text-text-secondary">{t(label)}</div> : null}
        <ImageAspectRatioPicker
          value={readText(raw) || "1:1"}
          onChange={(aspectRatio) => onPatch(item.path, aspectRatio)}
          disabled={disabled}
        />
      </div>
    );
  }
  if (item.field.control === "checkbox") {
    return (
      <CheckboxField
        label={fieldLabel}
        checked={Boolean(raw)}
        disabled={disabled}
        onChange={(checked) => onPatch(item.path, checked)}
      />
    );
  }
  if (item.field.control === "string_list") {
    return (
      <LineListField
        label={fieldLabel}
        value={readStringListText(raw)}
        disabled={disabled}
        onChange={(next) => onPatch(item.path, next)}
      />
    );
  }
  if (item.field.control === "select") {
    const options = item.field.choices ?? [];
    const current = raw == null ? options[0] ?? "" : String(raw);
    return (
      <Field label={fieldLabel}>
        <Select
          value={current}
          options={selectOptions(options, t)}
          onChange={(next) => onPatch(item.path, next)}
          disabled={disabled}
          size="sm"
          ariaLabel={fieldLabel}
        />
      </Field>
    );
  }
  if (item.field.control === "number") {
    const numberValue = typeof raw === "number" || typeof raw === "string" || raw === null ? raw : null;
    return (
      <NumberInput
        label={fieldLabel}
        value={numberValue}
        min={item.field.min_value ?? undefined}
        max={item.field.max_value ?? undefined}
        optional={item.field.value_kind === "number_or_null"}
        disabled={disabled}
        onChange={(next) => onPatch(item.path, next)}
      />
    );
  }
  if (item.field.control === "visual_background") {
    return (
      <Input
        label={fieldLabel}
        value={readVisualBackground(raw)}
        maxLength={visualBackgroundValueMaxLength(item.field)}
        onChange={(event) => onPatch(
          item.path,
          writeVisualBackground(event.target.value, raw, t("graph.inspector.visualBackground")),
        )}
        disabled={disabled}
      />
    );
  }
  if (item.field.control === "textarea") {
    return (
      <TextArea
        label={fieldLabel}
        value={readText(raw)}
        onChange={(next) => onPatch(item.path, next)}
        minRows={3}
        maxRows={12}
        maxLength={item.field.max_length ?? undefined}
        disabled={disabled}
      />
    );
  }
  return (
    <Input
      label={fieldLabel}
      value={readText(raw)}
      maxLength={item.field.max_length ?? undefined}
      onChange={(event) => onPatch(item.path, event.target.value)}
      disabled={disabled}
    />
  );
}

function collectRenderItems(fields: GraphCatalogConfigField[]): {
  unpaneled: CatalogRenderItem[];
  basic: CatalogRenderItem[];
  advanced: CatalogRenderItem[];
  tabsTitleKey: TranslationKey | null;
} {
  const unpaneled: CatalogRenderItem[] = [];
  const basic: CatalogRenderItem[] = [];
  const advanced: CatalogRenderItem[] = [];
  let tabsTitleKey: TranslationKey | null = null;
  for (const field of visibleCatalogFields(fields)) {
    const path = [field.key];
    const children = field.fields ?? [];
    const hasPaneledChildren = children.some((child) => child.panel);
    if (hasPaneledChildren && field.control === "group") {
      tabsTitleKey = catalogLabelKey(field.label_key) ?? tabsTitleKey;
      for (const child of visibleCatalogFields(children)) {
        const item = { field: child, path: [...path, child.key] };
        if (child.panel === "advanced") advanced.push(item);
        else if (child.panel === "basic") basic.push(item);
        else unpaneled.push(item);
      }
      continue;
    }
    const item = { field, path };
    if (field.panel === "advanced") advanced.push(item);
    else if (field.panel === "basic") basic.push(item);
    else unpaneled.push(item);
  }
  return { unpaneled, basic, advanced, tabsTitleKey };
}

function cloneDefault(field: GraphCatalogConfigField): unknown {
  if (field.default != null) return JSON.parse(JSON.stringify(field.default));
  return {};
}

function FieldGroup({ title, children }: { title: string; children: ReactNode }) {
  return (
    <fieldset className="space-y-3 border-t border-border-l1 pt-4">
      {title ? <legend className="mb-1 text-xs font-semibold text-text-primary">{title}</legend> : null}
      {children}
    </fieldset>
  );
}

function CheckboxField({
  label,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="flex min-h-11 items-center gap-2 rounded-panel border border-border-l1 bg-surface-subtle px-3 text-xs font-medium text-text-secondary lg:min-h-10">
      <input type="checkbox" checked={checked} disabled={disabled} onChange={(event) => onChange(event.target.checked)} className="h-4 w-4 accent-accent" />
      <span>{label}</span>
    </label>
  );
}

function LineListField({
  label,
  value,
  disabled,
  onChange,
}: {
  label: string;
  value: string;
  disabled?: boolean;
  onChange: (value: string) => void;
}) {
  return <TextArea label={label} value={value} onChange={onChange} minRows={2} disabled={disabled} />;
}

function selectOptionLabel(option: string, t: ReturnType<typeof useI18n>["t"]): string {
  if (option === "png" || option === "jpeg" || option === "webp") return option.toUpperCase();
  const key = SELECT_OPTION_LABEL_KEYS[option as keyof typeof SELECT_OPTION_LABEL_KEYS];
  return key ? t(key) : humanizeCatalogKey(option);
}

export function humanizeCatalogKey(value: string): string {
  return value
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (character) => character.toUpperCase());
}

function selectOptions(options: readonly string[], t: ReturnType<typeof useI18n>["t"]) {
  return options.map((option) => ({ value: option, label: selectOptionLabel(option, t) }));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
