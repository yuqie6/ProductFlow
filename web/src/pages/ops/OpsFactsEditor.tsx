import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { Button } from "../../components/ui/button";
import { Input, TextArea } from "../../components/ui/field";
import { api, ApiError } from "../../lib/api";
import { useI18n } from "../../lib/preferences";
import type { ProductFactsResponse } from "../../lib/types";
import { normalizeProductFactsDraft, productFactsDraft, productFactsPayload, validateProductFactsDraft } from "../workbench/canvas/graphNodeEditorDrafts";
import { OpsError, useLiveView } from "./OpsShared";

export function OpsFactsEditor({ merchantId, productId, initial }: { merchantId: string; productId: string; initial: ProductFactsResponse }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const live = useLiveView();
  const [draft, setDraft] = useState(() => productFactsDraft(initial.product, initial.fact_set));
  const [base, setBase] = useState({ version: initial.fact_set?.version ?? null, id: initial.current_fact_set_version_id ?? initial.fact_set?.id ?? null });
  const [validation, setValidation] = useState("");
  const adopt = (data: ProductFactsResponse) => {
    if (!live.current) return;
    queryClient.setQueryData(["ops-facts", merchantId, productId], data);
    if (live.current) {
      setDraft(productFactsDraft(data.product, data.fact_set));
      setBase({ version: data.fact_set?.version ?? null, id: data.current_fact_set_version_id ?? data.fact_set?.id ?? null });
      setValidation("");
    }
  };
  const save = useMutation({
    mutationFn: () => {
      const normalized = normalizeProductFactsDraft(draft);
      return api.updateOpsProductFacts(merchantId, productId, {
        expected_fact_version: base.version, expected_fact_set_version_id: base.id,
        name: normalized.name, category: normalized.category || null, price: normalized.price || null,
        source_note: normalized.source_note || null, facts: productFactsPayload(normalized),
      });
    },
    onSuccess: (data) => { if (!live.current) return; adopt(data); void queryClient.invalidateQueries({ queryKey: ["ops-products", merchantId] }); },
    onSettled: () => { if (!live.current) return; void queryClient.invalidateQueries({ queryKey: ["ops-actions", merchantId] }); },
  });
  const reload = useMutation({ mutationFn: () => api.getOpsProductFacts(merchantId, productId), onSuccess: (data) => { if (!live.current) return; adopt(data); save.reset(); } });
  const pending = save.isPending || reload.isPending;
  const conflict = save.error instanceof ApiError && save.error.status === 409;
  const edit = (patch: Partial<typeof draft>) => { setDraft((current) => ({ ...current, ...patch })); setValidation(""); if (!conflict) save.reset(); };
  return <form className="space-y-6" onSubmit={(event) => { event.preventDefault(); if (pending || conflict) return; if (validateProductFactsDraft(draft)) { setValidation(t("ops.factInvalid")); return; } setValidation(""); save.mutate(); }}>
    <div className="grid gap-5 sm:grid-cols-2">
      <Input label={t("create.productName")} value={draft.name} onChange={(event) => edit({ name: event.target.value })} required maxLength={255} disabled={pending} />
      <Input label={t("detail.inspector.category")} value={draft.category} onChange={(event) => edit({ category: event.target.value })} maxLength={120} disabled={pending} />
      <Input label={t("detail.inspector.price")} value={draft.price} onChange={(event) => edit({ price: event.target.value })} disabled={pending} />
    </div>
    <TextArea label={t("agentCreate.recipe.sourceNote")} value={draft.source_note} onChange={(value) => edit({ source_note: value })} minRows={3} disabled={pending} />
    <div className="border-t border-border-l1 pt-6">
      <h2 className="mb-4 font-semibold">{t("graph.inspector.productFacts")}</h2>
      <div className="space-y-5">{draft.facts.map((fact, index) => <fieldset key={fact.id} className="grid gap-3 border-b border-border-l1 pb-5 sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_auto]" disabled={pending}>
        <Input label={t("graph.inspector.productFactKey")} value={fact.key} onChange={(event) => edit({ facts: draft.facts.map((row, position) => position === index ? { ...row, key: event.target.value } : row) })} />
        <TextArea label={t("graph.inspector.productFactValue")} value={fact.value} onChange={(value) => edit({ facts: draft.facts.map((row, position) => position === index ? { ...row, value } : row) })} />
        <Button className="self-end justify-self-end" aria-label={t("graph.inspector.productFactRemove")} title={t("graph.inspector.productFactRemove")} onClick={() => edit({ facts: draft.facts.filter((_, position) => position !== index) })}><Trash2 size={15} aria-hidden="true" /></Button>
      </fieldset>)}</div>
      <Button className="mt-4" disabled={pending} onClick={() => edit({ facts: [...draft.facts, { id: crypto.randomUUID(), key: "", value: "", original: { key: "", value: "", source_type: "user", status: "user_declared", layer: "performance" } }] })}><Plus size={15} aria-hidden="true" />{t("graph.inspector.productFactAdd")}</Button>
    </div>
    {validation ? <p role="alert" className="text-sm text-state-error">{validation}</p> : null}
    {save.isError ? conflict ? <p role="alert" className="text-sm text-state-error">{t("ops.conflict")}</p> : <OpsError error={save.error} /> : null}
    {reload.isError ? <OpsError error={reload.error} /> : null}
    {save.isSuccess ? <p role="status" className="text-sm text-state-success">{t("graph.inspector.productFactsSaved")}</p> : null}
    <div className="flex flex-wrap gap-3"><Button type="submit" variant="primary" busy={save.isPending} disabled={pending || conflict}>{t("graph.inspector.productFactsSave")}</Button><Button disabled={pending} busy={reload.isPending} onClick={() => reload.mutate()}>{t("ops.discard")}</Button></div>
  </form>;
}
