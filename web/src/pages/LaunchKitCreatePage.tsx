import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Info, PackagePlus } from "lucide-react";
import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";

import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import type { LaunchKitPlatform } from "../lib/types";

const categoryOptions = [
  { value: "fashion", label: "Thời trang" },
  { value: "beauty", label: "Làm đẹp" },
  { value: "electronics_accessories", label: "Phụ kiện điện tử" },
  { value: "home_goods", label: "Đồ gia dụng" },
  { value: "food", label: "Thực phẩm" },
  { value: "other", label: "Khác" },
];

export function LaunchKitCreatePage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [productName, setProductName] = useState("");
  const [categoryKey, setCategoryKey] = useState("fashion");
  const [platforms, setPlatforms] = useState<LaunchKitPlatform[]>(["shopee", "tiktok_shop"]);
  const [referenceText, setReferenceText] = useState("");
  const [referenceUrls, setReferenceUrls] = useState("");
  const [notes, setNotes] = useState("");
  const [error, setError] = useState("");

  const logoutMutation = useMutation({
    mutationFn: api.destroySession,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      navigate("/login", { replace: true });
    },
  });

  const createMutation = useMutation({
    mutationFn: api.createLaunchKit,
    onSuccess: async (kit) => {
      await queryClient.invalidateQueries({ queryKey: ["launch-kits"] });
      navigate(`/launch-kits/${kit.id}`);
    },
    onError: (mutationError) => {
      setError(mutationError instanceof ApiError ? mutationError.detail : "Không tạo được LaunchKit");
    },
  });

  const togglePlatform = (platform: LaunchKitPlatform) => {
    setPlatforms((current) => {
      if (current.includes(platform)) {
        return current.length === 1 ? current : current.filter((item) => item !== platform);
      }
      return [...current, platform];
    });
  };

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const urls = referenceUrls
      .split(/\n|,/)
      .map((item) => item.trim())
      .filter(Boolean);
    setError("");
    createMutation.mutate({
      product_name: productName.trim(),
      category_key: categoryKey,
      target_platforms: platforms,
      source_references: {
        pasted_reference_text: referenceText.trim() || null,
        reference_urls: urls,
        notes: notes.trim() || null,
      },
    });
  };

  return (
    <div className="flex min-h-screen flex-col bg-slate-50 dark:bg-[#060a12]">
      <TopNav breadcrumbs="LaunchKit mới" onHome={() => navigate("/launch-kits")} onLogout={() => logoutMutation.mutate()} />
      <main className="mx-auto w-full max-w-5xl flex-1 px-4 pt-4 pb-40 sm:px-6 lg:py-10">
        <button
          type="button"
          onClick={() => navigate("/launch-kits")}
          className="mb-4 inline-flex items-center text-sm font-semibold text-slate-500 hover:text-slate-900 dark:text-slate-400 dark:hover:text-white"
        >
          <ArrowLeft size={16} className="mr-1" /> Quay lại LaunchKit
        </button>

        <div className="grid gap-5 lg:grid-cols-[1fr_320px]">
          <form onSubmit={handleSubmit} className="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm shadow-slate-200/60 dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-black/25 lg:p-7">
            <div className="mb-6">
              <div className="mb-3 inline-flex items-center rounded-full bg-emerald-50 px-3 py-1 text-[11px] font-bold uppercase tracking-[0.2em] text-emerald-700 ring-1 ring-emerald-100 dark:bg-emerald-500/10 dark:text-emerald-200 dark:ring-emerald-400/25">
                Xuất thủ công trước
              </div>
              <h1 className="text-2xl font-semibold tracking-tight text-slate-950 dark:text-white">Tạo LaunchKit cho seller Việt Nam</h1>
              <p className="mt-2 text-sm leading-6 text-slate-600 dark:text-slate-400">
                Nhập thông tin thật của sản phẩm và ghi chú bán hàng. Kit sẽ dùng dữ liệu này để tạo nội dung, checklist và gói xuất thủ công.
              </p>
            </div>

            {error ? <div className="mb-4 rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200">{error}</div> : null}

            <div className="space-y-5">
              <label className="block">
                <span className="text-sm font-semibold text-slate-700 dark:text-slate-200">Tên sản phẩm</span>
                <input
                  value={productName}
                  onChange={(event) => setProductName(event.target.value)}
                  required
                  maxLength={255}
                  placeholder="Ví dụ: Áo khoác chống nắng nữ UPF50+"
                  className="mt-2 w-full rounded-xl border border-slate-200 bg-white px-3 py-3 text-sm text-slate-950 outline-none transition focus:border-emerald-400 focus:ring-2 focus:ring-emerald-100 dark:border-slate-700 dark:bg-slate-950 dark:text-white dark:focus:ring-emerald-400/20"
                />
              </label>

              <label className="block">
                <span className="text-sm font-semibold text-slate-700 dark:text-slate-200">Playbook ngành hàng</span>
                <select
                  value={categoryKey}
                  onChange={(event) => setCategoryKey(event.target.value)}
                  className="mt-2 w-full rounded-xl border border-slate-200 bg-white px-3 py-3 text-sm text-slate-950 outline-none transition focus:border-emerald-400 focus:ring-2 focus:ring-emerald-100 dark:border-slate-700 dark:bg-slate-950 dark:text-white dark:focus:ring-emerald-400/20"
                >
                  {categoryOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
                </select>
              </label>

              <fieldset>
                <legend className="text-sm font-semibold text-slate-700 dark:text-slate-200">Sàn mục tiêu</legend>
                <div className="mt-2 grid gap-2 sm:grid-cols-2">
                  {([
                    ["shopee", "Shopee"],
                    ["tiktok_shop", "TikTok Shop"],
                  ] as const).map(([value, label]) => (
                    <button
                      key={value}
                      type="button"
                      onClick={() => togglePlatform(value)}
                      className={`rounded-xl border px-4 py-3 text-left text-sm font-semibold transition ${
                        platforms.includes(value)
                          ? "border-emerald-300 bg-emerald-50 text-emerald-800 ring-1 ring-emerald-200 dark:border-emerald-400/45 dark:bg-emerald-500/10 dark:text-emerald-100"
                          : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-300"
                      }`}
                    >
                      {label}
                    </button>
                  ))}
                </div>
              </fieldset>

              <label className="block">
                <span className="text-sm font-semibold text-slate-700 dark:text-slate-200">Thông tin tham khảo</span>
                <textarea
                  value={referenceText}
                  onChange={(event) => setReferenceText(event.target.value)}
                  maxLength={20_000}
                  rows={6}
                  placeholder="Dán thông số nhà cung cấp, mô tả đối thủ, claim cần tránh, câu hỏi hay gặp…"
                  className="mt-2 w-full rounded-xl border border-slate-200 bg-white px-3 py-3 text-sm text-slate-950 outline-none transition focus:border-emerald-400 focus:ring-2 focus:ring-emerald-100 dark:border-slate-700 dark:bg-slate-950 dark:text-white dark:focus:ring-emerald-400/20"
                />
              </label>

              <label className="block">
                <span className="text-sm font-semibold text-slate-700 dark:text-slate-200">URL tham khảo</span>
                <textarea
                  value={referenceUrls}
                  onChange={(event) => setReferenceUrls(event.target.value)}
                  rows={3}
                  placeholder="Mỗi dòng một URL"
                  className="mt-2 w-full rounded-xl border border-slate-200 bg-white px-3 py-3 text-sm text-slate-950 outline-none transition focus:border-emerald-400 focus:ring-2 focus:ring-emerald-100 dark:border-slate-700 dark:bg-slate-950 dark:text-white dark:focus:ring-emerald-400/20"
                />
              </label>

              <label className="block">
                <span className="text-sm font-semibold text-slate-700 dark:text-slate-200">Ghi chú người bán</span>
                <textarea
                  value={notes}
                  onChange={(event) => setNotes(event.target.value)}
                  maxLength={4_000}
                  rows={4}
                  placeholder="Biên lợi nhuận, ràng buộc, claim bị cấm, lịch khuyến mãi…"
                  className="mt-2 w-full rounded-xl border border-slate-200 bg-white px-3 py-3 text-sm text-slate-950 outline-none transition focus:border-emerald-400 focus:ring-2 focus:ring-emerald-100 dark:border-slate-700 dark:bg-slate-950 dark:text-white dark:focus:ring-emerald-400/20"
                />
              </label>
            </div>

            <div className="mt-6 flex justify-end gap-3 border-t border-slate-100 pt-5 dark:border-slate-800">
              <button type="button" onClick={() => navigate("/launch-kits")} className="rounded-xl border border-slate-200 px-4 py-2.5 text-sm font-semibold text-slate-600 dark:border-slate-700 dark:text-slate-300">Hủy</button>
              <button type="submit" disabled={createMutation.isPending || productName.trim().length === 0} className="inline-flex items-center rounded-xl bg-emerald-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-emerald-600/20 transition hover:bg-emerald-500 disabled:opacity-50">
                <PackagePlus size={16} className="mr-1.5" /> {createMutation.isPending ? "Đang tạo…" : "Tạo LaunchKit"}
              </button>
            </div>
          </form>

          <aside className="h-fit rounded-3xl border border-slate-200 bg-white p-5 shadow-sm shadow-slate-200/60 dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-black/25">
            <div className="flex items-start gap-3">
              <span className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-200"><Info size={18} /></span>
              <div>
                <h2 className="text-sm font-semibold text-slate-950 dark:text-white">Giữ đúng phạm vi kế hoạch</h2>
                <ul className="mt-3 space-y-2 text-sm leading-6 text-slate-600 dark:text-slate-400">
                  <li>• V1 không phụ thuộc API Shopee/TikTok.</li>
                  <li>• Xuất thủ công là luồng chính.</li>
                  <li>• Workflow canvas nâng cao vẫn truy cập được.</li>
                  <li>• Kiểm tra sẵn sàng trước khi xuất bản.</li>
                </ul>
              </div>
            </div>
          </aside>
        </div>
      </main>
    </div>
  );
}
