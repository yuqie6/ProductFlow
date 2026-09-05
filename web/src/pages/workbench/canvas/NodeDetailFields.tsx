import { Pencil, RotateCcw } from "lucide-react";
import type { ReactNode } from "react";
import { IconButton } from "../../../components/ui/icon-button";
import { tabListClassName, tabTriggerClassName } from "../../../components/ui/tabs";
import { useI18n } from "../../../lib/preferences";
import type { GraphCatalogConfigField, GraphNode } from "../../../lib/types";
import { CatalogConfigFields, CatalogField } from "./CatalogConfigFields";
import { catalogLabelKey, getConfigPath, patchCatalogValue, setConfigPath } from "./catalogConfig";

export function NodeDetailFields({ node, fields, value, onChange, disabled }: {
  node: GraphNode;
  fields: GraphCatalogConfigField[];
  value: Record<string, unknown>;
  onChange: (value: Record<string, unknown>) => void;
  disabled: boolean;
}) {
  const { t } = useI18n();
  const patch = (path: string[], next: unknown) => onChange(patchCatalogValue(fields, value, path, next));
  const render = (path: string[]) => {
    let children = fields;
    let field: GraphCatalogConfigField | undefined;
    for (const key of path) {
      field = children.find((item) => item.key === key);
      children = field?.fields ?? [];
    }
    return field ? <CatalogField key={path.join(".")} item={{ field, path }} config={value} onPatch={patch} disabled={disabled} /> : null;
  };
  const textMode = (path: string[]) => (
    <div className={tabListClassName} role="group" aria-label={t("nodeDetail.text")}>
      {(["none", "required"] as const).map((policy) => (
        <button key={policy} type="button" className={tabTriggerClassName} disabled={disabled}
          aria-pressed={(getConfigPath(value, [...path, "policy"]) ?? "none") === policy}
          onClick={() => {
            const current = getConfigPath(value, path);
            patch(path, { ...(isRecord(current) ? current : {}), policy,
              language: policy === "none" ? null : getConfigPath(value, [...path, "language"]) || "zh-CN" });
          }}>
          {t(policy === "none" ? "nodeDetail.noText" : "nodeDetail.withText")}
        </button>
      ))}
    </div>
  );
  if (node.node_type === "image_prompt") {
    const hasText = getConfigPath(value, ["text_settings", "policy"]) === "required";
    return <>
      <Section title={t("nodeDetail.objective")}>{render(["prompt", "design_goal"])}</Section>
      <Section title={t("nodeDetail.scene")}>
        {render(["prompt", "composition", "layout"])}
        {render(["prompt", "content", "background"])}
        {render(["prompt", "composition", "viewpoint"])}
        {render(["prompt", "composition", "product_share_percent"])}
      </Section>
      <Section title={t("nodeDetail.content")}>
        {render(["prompt", "content", "focus"])}{render(["prompt", "content", "selling_points"])}
      </Section>
      <Section title={t("nodeDetail.text")}>
        {textMode(["text_settings"])}
        {hasText ? <>{render(["text_settings", "language"])}{render(["prompt", "text"])}{render(["prompt", "composition", "copy_regions"])}</> : null}
      </Section>
      <details className="border-t border-border-l1 pt-3"><summary className="cursor-pointer text-xs font-semibold">{t("nodeDetail.details")}</summary>
        <div className="space-y-4 pt-4">{render(["prompt", "content", "decorations"])}{render(["prompt", "atmosphere"])}{render(["prompt", "product_fidelity"])}{render(["prompt", "shared_rules"])}{render(["prompt", "creative_boundary"])}</div>
      </details>
    </>;
  }
  if (node.node_type === "image_generation") {
    const overrideFields = fields.find((field) => field.key === "prompt_overrides")?.fields ?? [];
    const textLocal = isRecord(value.text_override);
    return <>
      <Section title={t("nodeDetail.local")}>
        <p className="text-[11px] text-text-muted">{t("nodeDetail.onlyThis")}</p>
        {[...overrideFields].sort((a, b) => Number(b.key === "content") - Number(a.key === "content")).map((group) => <details key={group.key} open={group.key === "content"}
          className="border-t border-border-l1 pt-3" data-override-group={group.key}>
          <summary className="cursor-pointer text-xs font-semibold text-text-secondary">{label(group)}</summary>
          <div className="space-y-3 pt-3">
          {(group.fields ?? []).map((field) => {
            const path = ["prompt_overrides", group.key, field.key];
            const local = getConfigPath(value, path) !== undefined;
            const inherited = getConfigPath(node.image_input?.inherited_prompt, [group.key, field.key]);
            return <div key={field.key} className="min-w-0 space-y-2" data-override-field={`${group.key}.${field.key}`}>
              <div className="flex min-w-0 items-center justify-between gap-2">
                <span className="min-w-0 break-words text-xs text-text-secondary">{local ? t("nodeDetail.local") : label(field)}</span>
                <IconButton label={t(local ? "nodeDetail.restore" : "nodeDetail.editLocal")} disabled={disabled} size="sm"
                  onClick={() => patch(path, local ? undefined : inherited ?? emptyValue(field))}>
                  {local ? <RotateCcw size={14} /> : <Pencil size={14} />}
                </IconButton>
              </div>
              {local ? render(path) : <p className="break-words whitespace-pre-wrap text-xs text-text-muted">{display(inherited) || t("nodeDetail.inherited")}</p>}
            </div>;
          })}
          </div>
        </details>)}
      </Section>
      <Section title={t("nodeDetail.text")}>
        <div className="flex items-center justify-between gap-2">
          <span className="text-xs text-text-secondary">{t(textLocal ? "nodeDetail.local" : "nodeDetail.inherited")}</span>
          <IconButton label={t(textLocal ? "nodeDetail.restore" : "nodeDetail.editLocal")} disabled={disabled} size="sm"
            onClick={() => onChange(setConfigPath(value, ["text_override"], textLocal ? undefined : {
              ...(node.image_input?.inherited_text_settings ?? { policy: "none", language: null }),
              ...(isRecord(node.image_input?.inherited_prompt.text) ? node.image_input.inherited_prompt.text : {}),
              copy_regions: getConfigPath(node.image_input?.inherited_prompt, ["composition", "copy_regions"]) ?? [],
            }))}>
            {textLocal ? <RotateCcw size={14} /> : <Pencil size={14} />}
          </IconButton>
        </div>
        {textLocal ? <>{textMode(["text_override"])}
          {getConfigPath(value, ["text_override", "policy"]) === "required" ? <>
            {["language", "headline", "subtitle", "body", "copy_regions"].map((key) => render(["text_override", key]))}
          </> : null}
        </> : <p className="whitespace-pre-wrap break-words text-xs text-text-muted">{node.image_input?.inherited_text_settings.policy === "required" ? display(node.image_input.inherited_prompt.text) : t("nodeDetail.noText")}</p>}
      </Section>
      <details className="border-t border-border-l1 pt-3">
        <summary className="cursor-pointer text-xs font-semibold">{t("nodeDetail.styleOverride")}</summary>
        <div className="space-y-3 pt-3">
          {isRecord(value.visual_overlay) ? <div className="flex justify-end">
            <IconButton label={t("nodeDetail.restore")} disabled={disabled} size="sm" onClick={() => patch(["visual_overlay"], undefined)}><RotateCcw size={14} /></IconButton>
          </div> : null}
          {render(["visual_overlay"])}
        </div>
      </details>
      <CatalogConfigFields fields={fields.filter((field) => !["prompt_overrides", "text_override", "visual_overlay"].includes(field.key))} value={value} onChange={onChange} disabled={disabled} />
    </>;
  }
  if (node.node_type === "creative_brief") return <>
    <CatalogConfigFields fields={fields.filter((field) => field.key !== "fact_gaps")} value={value} onChange={onChange} disabled={disabled} />
    <Section title={t("nodeDetail.pending")}>{render(["fact_gaps"])}</Section>
  </>;
  return <CatalogConfigFields fields={fields} value={value} onChange={onChange} disabled={disabled} />;

  function label(field: GraphCatalogConfigField) {
    const key = catalogLabelKey(field.label_key);
    return key ? t(key) : field.key;
  }
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return <section className="min-w-0 space-y-3 border-t border-border-l1 pt-4"><h3 className="text-xs font-semibold text-text-primary">{title}</h3>{children}</section>;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function display(value: unknown): string {
  if (typeof value === "string" || typeof value === "number") return String(value);
  if (Array.isArray(value)) return value.map(display).filter(Boolean).join("\n");
  if (isRecord(value)) return Object.values(value).map(display).filter(Boolean).join("\n");
  return "";
}

function emptyValue(field: GraphCatalogConfigField): unknown {
  if (field.value_kind === "string_list") return [];
  if (field.value_kind === "number") return field.min_value ?? 1;
  return "";
}
