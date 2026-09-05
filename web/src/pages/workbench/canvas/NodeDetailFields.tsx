import { ArrowUpRight, Check, Pencil, Plus, RotateCcw, Trash2, X } from "lucide-react";
import { useState, type ReactNode } from "react";
import { IconButton } from "../../../components/ui/icon-button";
import { TextArea } from "../../../components/ui/field";
import { Select } from "../../../components/ui/select";
import { tabListClassName, tabTriggerClassName } from "../../../components/ui/tabs";
import { useI18n } from "../../../lib/preferences";
import type { GraphCatalogConfigField, GraphNode, GraphProjection } from "../../../lib/types";
import { CatalogConfigFields, CatalogField, FieldGroup } from "./CatalogConfigFields";
import { catalogLabelKey, getConfigPath, patchCatalogValue, setConfigPath } from "./catalogConfig";

export function NodeDetailFields({ node, graph, onJump, onGenerateSection, fields, value, onChange, disabled }: {
  node: GraphNode;
  graph: GraphProjection;
  onJump?: (nodeId: string) => void;
  onGenerateSection?: (section: string, action: "complete" | "rewrite") => void;
  fields: GraphCatalogConfigField[];
  value: Record<string, unknown>;
  onChange: (value: Record<string, unknown>) => void;
  disabled: boolean;
}) {
  const { t } = useI18n();
  const [section, setSection] = useState("objective");
  const sources = graph.edges.filter((edge) => edge.target_node_id === node.id)
    .map((edge) => graph.nodes.find((candidate) => candidate.id === edge.source_node_id))
    .filter((source): source is GraphNode => Boolean(source));
  const patch = (path: string[], next: unknown) => onChange(patchCatalogValue(fields, value, path, next));
  const sourceLink = (source: GraphNode) => <button key={source.id} type="button" disabled={!onJump}
    onClick={() => onJump?.(source.id)} className="flex min-h-11 min-w-0 items-center gap-2 text-left text-xs text-text-secondary underline underline-offset-4 lg:min-h-8">
    <ArrowUpRight size={14} className="shrink-0" /><span className="break-words">{source.title}</span>
  </button>;
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
      {onGenerateSection ? <div className="flex min-w-0 items-center gap-2" data-section-generation>
        <div className="min-w-0 flex-1"><Select value={section} onChange={setSection} disabled={disabled} ariaLabel={t("nodeDetail.section")}
          options={[
            { value: "objective", label: t("nodeDetail.objective") }, { value: "composition", label: t("nodeDetail.scene") },
            { value: "visual_style", label: t("nodeDetail.content") }, { value: "copy", label: t("nodeDetail.text") },
          ]} /></div>
        <IconButton label={t("graph.inspector.complete")} disabled={disabled} onClick={() => onGenerateSection(section, "complete")}><Plus size={14} /></IconButton>
        <IconButton label={t("graph.inspector.rewrite")} disabled={disabled} onClick={() => onGenerateSection(section, "rewrite")}><Pencil size={14} /></IconButton>
      </div> : null}
      {render(["prompt", "design_goal"])}
      <Section title={t("nodeDetail.scene")}>
        {render(["prompt", "composition", "layout"])}
        {render(["prompt", "content", "background"])}
        <details data-plan-composition-details className="pt-1"><summary className="cursor-pointer text-xs font-semibold">{t("nodeDetail.details")}</summary>
          <div className="space-y-3 pt-3">{render(["prompt", "composition", "viewpoint"])}{render(["prompt", "composition", "product_share_percent"])}</div>
        </details>
      </Section>
      <Section title={t("nodeDetail.content")}>
        {render(["prompt", "content", "focus"])}{render(["prompt", "content", "selling_points"])}
        <details className="pt-1"><summary className="cursor-pointer text-xs font-semibold">{t("nodeDetail.details")}</summary>
          <div className="space-y-3 pt-3">{render(["prompt", "content", "decorations"])}{render(["prompt", "atmosphere"])}</div>
        </details>
      </Section>
      <Section title={t("nodeDetail.text")}>
        {textMode(["text_settings"])}
        {hasText ? <>{render(["text_settings", "language"])}{["headline", "subtitle", "body"].map((key) => render(["prompt", "text", key]))}{render(["prompt", "composition", "copy_regions"])}</> : null}
      </Section>
      <Section title={t("nodeDetail.fidelity")}>
        {display(getConfigPath(value, ["prompt", "shared_rules"])) ? <p className="whitespace-pre-wrap break-words text-xs text-text-secondary">{display(getConfigPath(value, ["prompt", "shared_rules"]))}</p> : null}
        {render(["prompt", "product_fidelity", "requirements"])}
      </Section>
      {sources.some((source) => ["creative_brief", "visual_system"].includes(source.node_type)) ? <Section title={t("nodeDetail.constraints")}>
        {sources.filter((source) => ["creative_brief", "visual_system"].includes(source.node_type)).map((source) => <div key={source.id} data-shared-source={source.id}>
          {sourceLink(source)}
          {source.node_type === "creative_brief" ? <dl className="space-y-2 text-xs">
            {([["required_elements", "graph.inspector.requiredCopy"], ["prohibitions", "nodeDetail.prohibitions"]] as const).map(([key, labelKey]) => display(source.config[key]) ? <div key={key}>
              <dt className="text-text-secondary">{t(labelKey)}</dt>
              <dd className="whitespace-pre-wrap break-words text-text-muted">{display(source.config[key])}</dd>
            </div> : null)}
          </dl> : <p className="whitespace-pre-wrap break-words text-xs text-text-muted">{display(getConfigPath(source.config, ["visual_overlay", "style"]))}</p>}
        </div>)}
      </Section> : null}
      {display(getConfigPath(value, ["prompt", "creative_boundary"])) ? <Section title={t("workflowConfirmation.creativeBoundary")}>
        <p className="whitespace-pre-wrap break-words text-xs text-text-muted">{display(getConfigPath(value, ["prompt", "creative_boundary"]))}</p>
      </Section> : null}
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
                <span className="min-w-0 break-words text-label font-semibold text-text-muted">{local ? t("nodeDetail.local") : label(field)}</span>
                {!local && !display(inherited) ? <span className="ml-auto text-label text-text-muted">{t("nodeDetail.inherited")}</span> : null}
                <IconButton label={t(local ? "nodeDetail.restore" : "nodeDetail.editLocal")} disabled={disabled} size="sm"
                  onClick={() => patch(path, local ? undefined : inherited ?? emptyValue(field))}>
                  {local ? <RotateCcw size={14} /> : <Pencil size={14} />}
                </IconButton>
              </div>
              {local ? render(path) : display(inherited) ? <p className="whitespace-pre-wrap break-words text-xs leading-5 text-text-secondary">{display(inherited)}</p> : null}
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
        </> : <TextArea aria-label={t("nodeDetail.text")} readOnly value={node.image_input?.inherited_text_settings.policy === "required" ? display(node.image_input.inherited_prompt.text) : t("nodeDetail.noText")} onChange={() => undefined} minRows={1} maxRows={4} />}
      </Section>
      <details data-image-style-details className="border-t border-border-l1 pt-3">
        <summary className="cursor-pointer text-xs font-semibold">{t("nodeDetail.styleOverride")}</summary>
        <div className="space-y-3 pt-3">
          {sources.filter((source) => source.node_type === "visual_system").map(sourceLink)}
          {(fields.find((field) => field.key === "visual_overlay")?.fields ?? []).map((field) => {
            const path = ["visual_overlay", field.key];
            const local = getConfigPath(value, path) !== undefined;
            const inherited = getConfigPath(node.image_input?.inherited_visual, [field.key]);
            return <div key={field.key} data-style-override={field.key} className="min-w-0 space-y-2">
              <div className="flex min-w-0 items-center justify-between gap-2">
                <span className="min-w-0 break-words text-xs font-semibold">{field.key === "colors" ? t("nodeDetail.palette") : label(field)}</span>
                <IconButton label={t(local ? "nodeDetail.restore" : "nodeDetail.editLocal")} disabled={disabled} size="sm"
                  onClick={() => patch(path, local ? undefined : inherited ?? [])}>
                  {local ? <RotateCcw size={14} /> : <Pencil size={14} />}
                </IconButton>
              </div>
              <p className="text-[11px] text-text-secondary">{t(local ? "nodeDetail.replacesSeries" : "nodeDetail.fromSeries")}</p>
              {field.key === "colors" ? <PaletteSummary value={inherited} /> : <p className="whitespace-pre-wrap break-words text-xs text-text-muted">{display(inherited) || t("nodeDetail.unspecified")}</p>}
              {local ? render(path) : null}
            </div>;
          })}
        </div>
      </details>
      <CatalogConfigFields fields={fields.filter((field) => !["prompt_overrides", "text_override", "visual_overlay", "delivery_spec"].includes(field.key))} value={value} onChange={onChange} disabled={disabled} />
    </>;
  }
  if (node.node_type === "creative_brief") return <>
    {render(["goal"])}
    {["key_messages", "required_elements", "prohibitions"].map((key) => {
      const field = fields.find((item) => item.key === key);
      if (!field) return null;
      const items = Array.isArray(value[key]) ? value[key].filter((item): item is string => typeof item === "string") : [];
      return <BriefEntries key={key} title={label(field)} items={items} disabled={disabled} onChange={(next) => patch([key], next)} />;
    })}
    {display(value.fact_gaps) ? <Section title={t("nodeDetail.pending")}>
      <ul className="space-y-2 text-xs text-text-secondary" data-fact-gap-summary>{(Array.isArray(value.fact_gaps) ? value.fact_gaps : []).map((gap, index) => <li key={index} className="break-words">{display(gap)}</li>)}</ul>
      {sources.filter((source) => source.node_type === "product_source").map(sourceLink)}
    </Section> : null}
    {fields.some((field) => field.control !== "hidden" && !["goal", "key_messages", "required_elements", "prohibitions", "fact_gaps"].includes(field.key)) ? <details>
      <summary className="cursor-pointer text-xs font-semibold">{t("nodeDetail.details")}</summary>
      <div className="space-y-3 pt-3"><CatalogConfigFields fields={fields.filter((field) => !["goal", "key_messages", "required_elements", "prohibitions", "fact_gaps"].includes(field.key))} value={value} onChange={onChange} disabled={disabled} /></div>
    </details> : null}
  </>;
  return <CatalogConfigFields fields={fields} value={value} onChange={onChange} disabled={disabled} />;

  function label(field: GraphCatalogConfigField) {
    const key = catalogLabelKey(field.label_key);
    return key ? t(key) : field.key;
  }
}

function PaletteSummary({ value }: { value: unknown }) {
  const { t } = useI18n();
  const colors = Array.isArray(value) ? value.filter(isRecord) : [];
  const roleLabels: Record<string, string> = {
    background: t("agentWorkbench.nodeEditor.background"), product: t("graph.inspector.role.product_identity"),
    headline: t("agentWorkbench.nodeEditor.headline"), accent: t("nodeDetail.colorAccent"),
  };
  if (!colors.length) return <p className="text-xs text-text-muted">{t("nodeDetail.unspecified")}</p>;
  return <ul className="space-y-2">{colors.map((color, index) => {
    const hex = typeof color.value === "string" ? color.value : "";
    const role = typeof color.role === "string" ? roleLabels[color.role] : undefined;
    const note = typeof color.label === "string" ? color.label : "";
    return <li key={index} className="flex min-w-0 items-center gap-2 text-xs text-text-muted">
      <span aria-hidden="true" className="h-5 w-5 shrink-0 rounded border border-border-l1" style={{ backgroundColor: /^#[0-9a-f]{6}$/i.test(hex) ? hex : undefined }} />
      <span className="min-w-0 break-words">{[role, note !== role ? note : "", hex].filter(Boolean).join(" · ")}</span>
    </li>;
  })}</ul>;
}

function BriefEntries({ title, items, disabled, onChange }: { title: string; items: string[]; disabled: boolean; onChange: (items: string[]) => void }) {
  const { t } = useI18n();
  // A new, empty row is UI state; autosave must not normalize it out while the user is preparing an entry.
  const [pending, setPending] = useState<string | null>(null);
  return <div className="min-w-0 space-y-2" data-brief-entries={title}>
    <div className="flex min-w-0 items-center justify-between gap-2">
      <span className="text-label font-semibold text-text-muted">{title}</span>
      <IconButton label={`${t("nodeDetail.add")} ${title}`} size="sm" disabled={disabled || pending !== null} onClick={() => setPending("")}><Plus size={14} /></IconButton>
    </div>
    {items.map((item, index) => <div key={index} className="flex min-w-0 items-start gap-2">
      <div className="min-w-0 flex-1"><TextArea aria-label={`${title} ${index + 1}`} value={item} minRows={1} maxRows={4} disabled={disabled}
        onChange={(text) => onChange(items.map((current, position) => position === index ? text : current))} /></div>
      <IconButton label={t("nodeDetail.remove")} size="sm" disabled={disabled} onClick={() => onChange(items.filter((_, position) => position !== index))}><Trash2 size={14} /></IconButton>
    </div>)}
    {pending !== null ? <div className="flex min-w-0 items-start gap-2" data-new-brief-entry>
      <div className="min-w-0 flex-1"><TextArea autoFocus aria-label={`${t("nodeDetail.add")} ${title}`} value={pending} onChange={setPending} minRows={1} maxRows={4} disabled={disabled} /></div>
      <IconButton label={t("nodeDetail.add")} size="sm" disabled={disabled || !pending.trim()} onClick={() => { onChange([...items, pending.trim()]); setPending(null); }}><Check size={14} /></IconButton>
      <IconButton label={t("nodeDetail.remove")} size="sm" disabled={disabled} onClick={() => setPending(null)}><X size={14} /></IconButton>
    </div> : null}
  </div>;
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return <FieldGroup title={title}>{children}</FieldGroup>;
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
