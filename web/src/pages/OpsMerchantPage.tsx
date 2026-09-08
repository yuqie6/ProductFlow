import * as TabsPrimitive from "@radix-ui/react-tabs";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { Image } from "lucide-react";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/field";
import { Select } from "../components/ui/select";
import { Tabs, TabList, TabTrigger, TabPanel } from "../components/ui/tabs";
import { api } from "../lib/api";
import { useI18n } from "../lib/preferences";
import type { ProductListSort } from "../lib/types";
import { OpsActions, OpsTasks } from "./ops/OpsRecords";
import { OpsQuota } from "./ops/OpsQuota";
import { OPS_PAGE_SIZE, OpsEmpty, OpsError, OpsLoading, OpsPagination, OpsShell, OpsStatus, OpsTime, productPath } from "./ops/OpsShared";

type MerchantTab = "products" | "tasks" | "quota" | "actions";
export function OpsMerchantPage() {
  const { merchantId = "" } = useParams();
  return <MerchantView key={merchantId} merchantId={merchantId} />;
}

function MerchantView({ merchantId }: { merchantId: string }) {
  const { t } = useI18n();
  const [tab, setTab] = useState<MerchantTab>("products");
  const [quotaVisited, setQuotaVisited] = useState(false);
  const merchant = useQuery({ queryKey: ["ops-merchant", merchantId], queryFn: () => api.getOpsMerchant(merchantId), retry: false });
  return <OpsShell title={merchant.data?.name ?? t("ops.merchant")} back={{ to: "/ops", label: t("ops.merchants") }}>
    {merchant.isPending ? <OpsLoading /> : merchant.isError ? <OpsError error={merchant.error} retry={() => void merchant.refetch()} /> : <>
      <div className="mb-6"><OpsStatus status={merchant.data.status} /></div>
      <Tabs value={tab} onValueChange={(value) => { if (value === "products" || value === "tasks" || value === "quota" || value === "actions") { setTab(value); if (value === "quota") setQuotaVisited(true); } }}>
        <TabList aria-label={t("ops.title")} className="mb-6 flex w-full flex-wrap sm:w-fit">{(["products", "tasks", "quota", "actions"] as const).map((value) => <TabTrigger key={value} value={value} className="min-h-11 px-2 text-[11px] sm:flex-none sm:px-3 sm:text-xs sm:whitespace-nowrap">{t(`ops.${value}`)}</TabTrigger>)}</TabList>
        <TabPanel value="products"><MerchantProducts merchantId={merchantId} /></TabPanel>
        <TabPanel value="tasks"><OpsTasks merchantId={merchantId} /></TabPanel>
        <TabPanel value="actions"><OpsActions merchantId={merchantId} /></TabPanel>
        <TabsPrimitive.Content value="quota" forceMount hidden={tab !== "quota"} className="outline-none">{quotaVisited ? <OpsQuota merchantId={merchantId} /> : null}</TabsPrimitive.Content>
      </Tabs>
    </>}
  </OpsShell>;
}

function MerchantProducts({ merchantId }: { merchantId: string }) {
  const { t } = useI18n();
  const [page, setPage] = useState(1);
  const [draft, setDraft] = useState("");
  const [q, setQ] = useState("");
  const [sort, setSort] = useState<ProductListSort>("updated_desc");
  const products = useQuery({ queryKey: ["ops-products", merchantId, page, q, sort], queryFn: () => api.listOpsProducts(merchantId, { page, page_size: OPS_PAGE_SIZE, q, sort }), retry: false });
  return <>
    <form className="mb-6 flex flex-wrap items-end gap-3" onSubmit={(event) => { event.preventDefault(); setQ(draft.trim()); setPage(1); }}>
      <div className="min-w-0 flex-1 basis-56"><Input label={t("ops.searchProducts")} value={draft} onChange={(event) => setDraft(event.target.value)} type="search" maxLength={100} /></div>
      <div className="w-44"><Select ariaLabel={t("ops.sort")} value={sort} onChange={(value) => { if (value === "updated_desc" || value === "created_desc" || value === "name_asc") { setSort(value); setPage(1); } }} options={[{ value: "updated_desc", label: t("ops.updated") }, { value: "created_desc", label: t("ops.created") }, { value: "name_asc", label: t("ops.nameAsc") }]} /></div><Button type="submit">{t("ops.search")}</Button>
    </form>
    {products.isPending ? <OpsLoading /> : products.isError ? <OpsError error={products.error} retry={() => void products.refetch()} /> : <>
      {products.data.items.length === 0 ? <OpsEmpty /> : <ul className="divide-y divide-border-l1 border-y border-border-l1">{products.data.items.map((product) => <li key={product.id} className="py-5"><Link to={productPath(merchantId, product.id)} className="flex min-w-0 items-center gap-4 rounded-control focus-visible:ring-2 focus-visible:ring-focus-ring">
        <div className="flex h-16 w-16 shrink-0 items-center justify-center overflow-hidden rounded-control border border-border-l1 bg-surface-panel">{product.cover_image_thumbnail_url ? <img src={api.toApiUrl(product.cover_image_thumbnail_url)} alt="" className="h-full w-full object-contain" loading="lazy" /> : <Image size={20} className="text-text-muted" aria-hidden="true" />}</div>
        <div className="min-w-0 flex-1"><p className="break-words text-sm font-semibold">{product.name}</p>{product.category ? <p className="mt-1 break-words text-xs text-text-muted">{product.category}</p> : null}<p className="mt-2 text-xs text-text-muted"><OpsTime value={product.updated_at} /></p></div>
      </Link></li>)}</ul>}
      <OpsPagination page={page} total={products.data.total} onPage={setPage} />
    </>}
  </>;
}
