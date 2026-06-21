import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, ArrowRight, CheckCircle2, Clock3, PackagePlus, Save, Store, Wand2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";

import { TopNav } from "../components/TopNav";
import { api } from "../lib/api";
import { formatShortDate } from "../lib/format";
import type { LaunchKitStatus, LaunchKitSummary, StoreProfileUpdateRequest } from "../lib/types";

const PAGE_SIZE = 12;

const statusTone: Record<LaunchKitStatus, string> = {
  draft: "bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-500/10 dark:text-amber-200 dark:ring-amber-400/30",
  generating: "bg-blue-50 text-blue-700 ring-blue-200 dark:bg-blue-500/10 dark:text-blue-200 dark:ring-blue-400/30",
  ready: "bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-500/10 dark:text-emerald-200 dark:ring-emerald-400/30",
  archived: "bg-slate-100 text-slate-700 ring-slate-200 dark:bg-slate-800 dark:text-slate-200 dark:ring-slate-700",
  failed: "bg-red-50 text-red-700 ring-red-200 dark:bg-red-500/10 dark:text-red-200 dark:ring-red-400/30",
};

function statusLabel(status: LaunchKitStatus) {
  return {
    draft: "Bản nháp",
    generating: "Đang tạo",
    ready: "Sẵn sàng",
    archived: "Đã lưu trữ",
    failed: "Lỗi",
  }[status];
}

function platformLabel(platform: string) {
  return platform === "tiktok_shop" ? "TikTok Shop" : "Shopee";
}

function KitCard({ kit }: { kit: LaunchKitSummary }) {
  return (
    <Link
      to={`/launch-kits/${kit.id}`}
      className="group flex flex-col justify-between rounded-2xl border border-slate-200 bg-white p-4 shadow-sm shadow-slate-200/50 transition hover:-translate-y-0.5 hover:border-emerald-200 hover:shadow-md dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-black/20 dark:hover:border-emerald-400/40"
    >
      <div>
        <div className="mb-3 flex items-start justify-between gap-3">
          <div className="min-w-0">
            <h3 className="truncate text-base font-semibold text-slate-950 dark:text-white">{kit.product_name}</h3>
            <p className="mt-1 text-xs font-medium uppercase tracking-[0.16em] text-slate-400">{kit.category_key}</p>
          </div>
          <span className={`shrink-0 rounded-full px-2.5 py-1 text-xs font-semibold ring-1 ${statusTone[kit.status]}`}>
            {statusLabel(kit.status)}
          </span>
        </div>
        <div className="flex flex-wrap gap-2">
          {kit.target_platforms.map((platform) => (
            <span
              key={platform}
              className="inline-flex items-center rounded-full bg-slate-100 px-2.5 py-1 text-xs font-medium text-slate-600 dark:bg-slate-800 dark:text-slate-300"
            >
              <Store size={12} className="mr-1" /> {platformLabel(platform)}
            </span>
          ))}
        </div>
      </div>
      <div className="mt-5 flex items-center justify-between border-t border-slate-100 pt-3 text-sm dark:border-slate-800">
        <span className="text-slate-500 dark:text-slate-400">Cập nhật {formatShortDate(kit.updated_at)}</span>
        <span className="inline-flex items-center font-semibold text-emerald-700 transition group-hover:translate-x-0.5 dark:text-emerald-300">
          Mở <ArrowRight size={15} className="ml-1" />
        </span>
      </div>
    </Link>
  );
}

function listToText(items: string[] | undefined) {
  return (items ?? []).join("\n");
}

function textToList(value: string) {
  return value.split("\n").map((item) => item.trim()).filter(Boolean);
}

function StoreProfilePanel() {
  const queryClient = useQueryClient();
  const profileQuery = useQuery({
    queryKey: ["launch-kit-store-profile"],
    queryFn: api.getStoreProfile,
    staleTime: 60_000,
  });
  const [form, setForm] = useState<StoreProfileUpdateRequest>({
    store_name: "",
    store_tone: "",
    target_buyer: "",
    preferred_cta: "",
    warranty_notes: "",
    brand_rules: [],
    color_logo_notes: "",
    platform_preferences: {},
    default_shipping_promo_notes: "",
    prohibited_claims: [],
  });
  const [brandRulesText, setBrandRulesText] = useState("");
  const [bannedClaimsText, setBannedClaimsText] = useState("");
  const [saved, setSaved] = useState(false);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    if (!profileQuery.data || dirty) {
      return;
    }
    setForm({
      store_name: profileQuery.data.store_name ?? "",
      store_tone: profileQuery.data.store_tone ?? "",
      target_buyer: profileQuery.data.target_buyer ?? "",
      preferred_cta: profileQuery.data.preferred_cta ?? "",
      warranty_notes: profileQuery.data.warranty_notes ?? "",
      brand_rules: profileQuery.data.brand_rules ?? [],
      color_logo_notes: profileQuery.data.color_logo_notes ?? "",
      platform_preferences: profileQuery.data.platform_preferences ?? {},
      default_shipping_promo_notes: profileQuery.data.default_shipping_promo_notes ?? "",
      prohibited_claims: profileQuery.data.prohibited_claims ?? [],
    });
    setBrandRulesText(listToText(profileQuery.data.brand_rules));
    setBannedClaimsText(listToText(profileQuery.data.prohibited_claims));
  }, [profileQuery.data]);

  const mutation = useMutation({
    mutationFn: () => api.updateStoreProfile({
      ...form,
      brand_rules: textToList(brandRulesText),
      prohibited_claims: textToList(bannedClaimsText),
    }),
    onSuccess: async (updated) => {
      setDirty(false);
      setSaved(true);
      queryClient.setQueryData(["launch-kit-store-profile"], updated);
      window.setTimeout(() => setSaved(false), 2200);
    },
  });

  const update = (patch: Partial<StoreProfileUpdateRequest>) => {
    setDirty(true);
    setSaved(false);
    setForm((current) => ({ ...current, ...patch }));
  };

  return (
    <section className="mt-5 rounded-2xl border border-emerald-100 bg-emerald-50/70 p-4 shadow-sm shadow-emerald-100/40 dark:border-emerald-400/20 dark:bg-emerald-500/10 dark:shadow-black/20 lg:p-5">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="max-w-xl">
          <div className="mb-2 inline-flex items-center rounded-full bg-white px-3 py-1 text-[11px] font-bold uppercase tracking-[0.2em] text-emerald-700 ring-1 ring-emerald-100 dark:bg-slate-950/60 dark:text-emerald-200 dark:ring-emerald-400/25">
            Hồ sơ shop
          </div>
          <h2 className="text-base font-semibold text-slate-950 dark:text-white">Defaults dùng cho mọi LaunchKit mới</h2>
          <p className="mt-1 text-sm leading-6 text-slate-600 dark:text-slate-300">Lưu tone, khách mục tiêu, CTA và claim bị cấm. Ghi chú ở từng sản phẩm vẫn có thể cụ thể hơn.</p>
        </div>
        <button type="button" onClick={() => mutation.mutate()} disabled={mutation.isPending || profileQuery.isLoading} className="inline-flex items-center rounded-xl bg-emerald-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-emerald-600/20 transition hover:bg-emerald-500 disabled:opacity-50">
          <Save size={16} className="mr-1.5" /> {mutation.isPending ? "Đang lưu…" : "Lưu hồ sơ shop"}
        </button>
      </div>

      <div className="mt-4 grid gap-3 md:grid-cols-2 lg:grid-cols-3">
        <ProfileInput label="Tên shop" value={form.store_name ?? ""} onChange={(value) => update({ store_name: value })} placeholder="Mộc Home" />
        <ProfileInput label="Tone mặc định" value={form.store_tone ?? ""} onChange={(value) => update({ store_tone: value })} placeholder="ấm áp, rõ ràng, không phóng đại" />
        <ProfileInput label="Khách mục tiêu" value={form.target_buyer ?? ""} onChange={(value) => update({ target_buyer: value })} placeholder="dân văn phòng ở TP.HCM" />
        <ProfileInput label="CTA ưu tiên" value={form.preferred_cta ?? ""} onChange={(value) => update({ preferred_cta: value })} placeholder="Nhắn shop để chọn màu trước khi đặt" />
        <ProfileInput label="Bảo hành / đổi trả" value={form.warranty_notes ?? ""} onChange={(value) => update({ warranty_notes: value })} placeholder="Đổi mới trong 7 ngày nếu lỗi sản xuất" />
        <ProfileInput label="Giao hàng / khuyến mãi" value={form.default_shipping_promo_notes ?? ""} onChange={(value) => update({ default_shipping_promo_notes: value })} placeholder="Freeship cho đơn nội thành" />
        <ProfileTextarea label="Quy tắc brand" value={brandRulesText} onChange={(value) => { setDirty(true); setSaved(false); setBrandRulesText(value); }} placeholder="Mỗi dòng một quy tắc" />
        <ProfileTextarea label="Claim bị cấm" value={bannedClaimsText} onChange={(value) => { setDirty(true); setSaved(false); setBannedClaimsText(value); }} placeholder="Mỗi dòng một claim không được dùng" />
      </div>
      {saved ? <p className="mt-3 text-sm font-semibold text-emerald-700 dark:text-emerald-300">Đã lưu hồ sơ shop. LaunchKit tiếp theo sẽ dùng defaults này.</p> : null}
      {mutation.isError ? <p className="mt-3 text-sm font-semibold text-red-600 dark:text-red-300">Không lưu được hồ sơ shop.</p> : null}
    </section>
  );
}

function ProfileInput({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (value: string) => void; placeholder: string }) {
  return (
    <label className="block">
      <span className="text-xs font-semibold text-slate-600 dark:text-slate-300">{label}</span>
      <input value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} className="mt-1 w-full rounded-xl border border-emerald-100 bg-white px-3 py-2.5 text-sm text-slate-950 outline-none transition focus:border-emerald-400 dark:border-emerald-400/20 dark:bg-slate-950 dark:text-white" />
    </label>
  );
}

function ProfileTextarea({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (value: string) => void; placeholder: string }) {
  return (
    <label className="block lg:col-span-1">
      <span className="text-xs font-semibold text-slate-600 dark:text-slate-300">{label}</span>
      <textarea value={value} onChange={(event) => onChange(event.target.value)} rows={3} placeholder={placeholder} className="mt-1 w-full rounded-xl border border-emerald-100 bg-white px-3 py-2.5 text-sm text-slate-950 outline-none transition focus:border-emerald-400 dark:border-emerald-400/20 dark:bg-slate-950 dark:text-white" />
    </label>
  );
}

export function LaunchKitListPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const kitsQuery = useQuery({
    queryKey: ["launch-kits", page, PAGE_SIZE],
    queryFn: () => api.listLaunchKits({ page, page_size: PAGE_SIZE }),
    placeholderData: keepPreviousData,
    staleTime: 60_000,
  });
  const total = kitsQuery.data?.total ?? 0;
  const kits = kitsQuery.data?.items ?? [];
  const ready = kits.filter((kit) => kit.status === "ready").length;
  const active = kits.filter((kit) => kit.status === "generating").length;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const logoutMutation = useMutation({
    mutationFn: api.destroySession,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      navigate("/login", { replace: true });
    },
  });

  return (
    <div className="flex min-h-screen flex-col bg-slate-50 dark:bg-[#060a12]">
      <TopNav onHome={() => navigate("/launch-kits")} onLogout={() => logoutMutation.mutate()} />
      <main className="mx-auto w-full max-w-6xl flex-1 px-4 pt-4 pb-40 sm:px-6 lg:py-10">
        <section className="overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-sm shadow-slate-200/60 dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-black/25">
          <div className="grid gap-8 p-5 lg:grid-cols-[1.35fr_1fr] lg:p-7">
            <div>
              <div className="mb-3 inline-flex items-center rounded-full bg-emerald-50 px-3 py-1 text-[11px] font-bold uppercase tracking-[0.2em] text-emerald-700 ring-1 ring-emerald-100 dark:bg-emerald-500/10 dark:text-emerald-200 dark:ring-emerald-400/25">
                Bàn ra mắt cho seller Việt Nam
              </div>
              <h1 className="text-2xl font-semibold tracking-tight text-slate-950 dark:text-white lg:text-3xl">
                Shopee / TikTok Shop LaunchKit
              </h1>
              <p className="mt-3 max-w-2xl text-sm leading-6 text-slate-600 dark:text-slate-400">
                Chuẩn bị một bộ nội dung ra mắt có thể copy thủ công: góc bán hàng, checklist chất lượng, nội dung theo sàn và snapshot xuất bản.
              </p>
              <div className="mt-5 flex flex-wrap gap-3">
                <button
                  type="button"
                  onClick={() => navigate("/launch-kits/new")}
                  className="inline-flex items-center rounded-xl bg-emerald-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-emerald-600/20 transition hover:bg-emerald-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500"
                >
                  <PackagePlus size={16} className="mr-1.5" /> Tạo LaunchKit
                </button>
                <button
                  type="button"
                  onClick={() => navigate("/products")}
                  className="inline-flex items-center rounded-xl border border-slate-200 bg-white px-4 py-2.5 text-sm font-semibold text-slate-700 transition hover:border-slate-300 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
                >
                  Chế độ nâng cao
                </button>
              </div>
            </div>
            <div className="grid grid-cols-3 gap-3 self-end">
              <Metric icon={Wand2} label="Tổng kit" value={total} />
              <Metric icon={CheckCircle2} label="Sẵn sàng" value={ready} />
              <Metric icon={Clock3} label="Đang chạy" value={active} />
            </div>
          </div>
        </section>

        <StoreProfilePanel />

        <section className="mt-5 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm shadow-slate-200/50 dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-black/20 lg:p-5">
          <div className="mb-4 flex items-center justify-between gap-3">
            <div>
              <h2 className="text-base font-semibold text-slate-950 dark:text-white">LaunchKit</h2>
              <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">Trang {page} / {totalPages} · {total} kit</p>
            </div>
            <div className="flex gap-2">
              <button type="button" disabled={page <= 1 || kitsQuery.isFetching} onClick={() => setPage((current) => Math.max(1, current - 1))} className="rounded-lg border border-slate-200 px-3 py-2 text-sm font-semibold text-slate-600 disabled:opacity-40 dark:border-slate-700 dark:text-slate-300">Trước</button>
              <button type="button" disabled={page >= totalPages || kitsQuery.isFetching} onClick={() => setPage((current) => Math.min(totalPages, current + 1))} className="rounded-lg border border-slate-200 px-3 py-2 text-sm font-semibold text-slate-600 disabled:opacity-40 dark:border-slate-700 dark:text-slate-300">Sau</button>
            </div>
          </div>

          {kitsQuery.isError ? (
            <div className="flex items-center rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200">
              <AlertCircle size={16} className="mr-2" /> Không tải được LaunchKit. Kiểm tra backend và migration đã chạy.
            </div>
          ) : kitsQuery.isLoading ? (
            <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
              {[1, 2, 3].map((item) => <div key={item} className="h-40 rounded-2xl animate-shimmer" />)}
            </div>
          ) : kits.length === 0 ? (
            <div className="rounded-2xl border border-dashed border-slate-300 p-8 text-center dark:border-slate-700">
              <h3 className="text-lg font-semibold text-slate-950 dark:text-white">Chưa có LaunchKit</h3>
              <p className="mt-2 text-sm text-slate-500 dark:text-slate-400">Tạo kit đầu tiên từ sản phẩm thật và ghi chú bán hàng theo sàn.</p>
              <button type="button" onClick={() => navigate("/launch-kits/new")} className="mt-4 rounded-xl bg-emerald-600 px-4 py-2.5 text-sm font-semibold text-white">Tạo LaunchKit</button>
            </div>
          ) : (
            <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
              {kits.map((kit) => <KitCard key={kit.id} kit={kit} />)}
            </div>
          )}
        </section>
      </main>
    </div>
  );
}

function Metric({ icon: Icon, label, value }: { icon: typeof Wand2; label: string; value: number }) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-slate-50 p-3 dark:border-slate-700 dark:bg-slate-900/65">
      <Icon size={16} className="text-emerald-600 dark:text-emerald-300" />
      <div className="mt-3 text-2xl font-semibold text-slate-950 dark:text-white">{value}</div>
      <div className="mt-1 text-xs font-medium text-slate-500 dark:text-slate-400">{label}</div>
    </div>
  );
}
