import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Store } from "lucide-react";
import { Link } from "react-router-dom";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/field";
import { Select } from "../components/ui/select";
import { api } from "../lib/api";
import { useI18n } from "../lib/preferences";
import { merchantPath, OPS_PAGE_SIZE, OpsEmpty, OpsError, OpsLoading, OpsPagination, OpsShell, OpsStatus } from "./ops/OpsShared";

export function OpsPage() {
  const { t } = useI18n();
  const [draft, setDraft] = useState("");
  const [q, setQ] = useState("");
  const [status, setStatus] = useState<"" | "active" | "suspended">("");
  const [page, setPage] = useState(1);
  const merchants = useQuery({ queryKey: ["ops-merchants", q, status, page], queryFn: () => api.listOpsMerchants({ q, status, page, page_size: OPS_PAGE_SIZE }), retry: false });
  return <OpsShell title={t("ops.merchants")}>
    <form className="mb-8 flex flex-wrap items-end gap-3" onSubmit={(event) => { event.preventDefault(); setQ(draft.trim()); setPage(1); }}>
      <div className="min-w-0 flex-1 basis-60"><Input label={t("ops.searchMerchants")} value={draft} onChange={(event) => setDraft(event.target.value)} maxLength={100} type="search" /></div>
      <div className="w-40"><Select ariaLabel={t("ops.allStatuses")} value={status || "all"} onChange={(value) => { setStatus(value === "active" || value === "suspended" ? value : ""); setPage(1); }} options={[{ value: "all", label: t("ops.allStatuses") }, { value: "active", label: t("ops.active") }, { value: "suspended", label: t("ops.suspended") }]} /></div>
      <Button type="submit">{t("ops.search")}</Button>
    </form>
    {merchants.isPending ? <OpsLoading /> : merchants.isError ? <OpsError error={merchants.error} retry={() => void merchants.refetch()} /> : <>
      {merchants.data.items.length === 0 ? <OpsEmpty /> : <ul className="divide-y divide-border-l1 border-y border-border-l1">{merchants.data.items.map((merchant) => <li key={merchant.id} className="flex flex-wrap items-center justify-between gap-4 py-5">
        <div className="flex min-w-0 items-center gap-3"><Store size={20} className="shrink-0 text-text-muted" aria-hidden="true" /><div className="min-w-0"><Link className="break-words text-base font-semibold hover:underline focus-visible:ring-2 focus-visible:ring-focus-ring" to={merchantPath(merchant.id)}>{merchant.name}</Link><div className="mt-2"><OpsStatus status={merchant.status} /></div></div></div>
        <Link to={merchantPath(merchant.id)} className="inline-flex min-h-11 shrink-0 items-center gap-2 rounded-control px-3 text-sm text-accent hover:bg-accent-soft focus-visible:ring-2 focus-visible:ring-focus-ring">{t("ops.open")}<ArrowRight size={15} aria-hidden="true" /></Link>
      </li>)}</ul>}
      <OpsPagination page={page} total={merchants.data.total} onPage={setPage} />
    </>}
  </OpsShell>;
}
