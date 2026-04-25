import { useMemo, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { ImagePlus, Loader2 } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { ImageDropZone } from "../components/ImageDropZone";
import { useOnboarding } from "../components/OnboardingGuide";
import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";

export function ProductCreatePage() {
  const navigate = useNavigate();
  const onboarding = useOnboarding();
  const [name, setName] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [error, setError] = useState("");

  const previewLabel = useMemo(() => {
    if (!file) {
      return "点击或拖拽图片到这里";
    }
    return file.name;
  }, [file]);

  const createProductMutation = useMutation({
    mutationFn: () => {
      if (!file) {
        throw new Error("请先上传商品图");
      }
      return api.createProduct({
        name,
        file,
      });
    },
    onSuccess: (product) => {
      if (onboarding.active && onboarding.step.id === "create-product-form") {
        onboarding.advance();
      }
      navigate(`/products/${product.id}`);
    },
    onError: (mutationError) => {
      if (mutationError instanceof ApiError) {
        setError(mutationError.detail);
        return;
      }
      setError(mutationError instanceof Error ? mutationError.message : "创建商品失败");
    },
  });

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError("");
    createProductMutation.mutate();
  };

  const handleImageFiles = (files: File[]) => {
    setFile(files[0] ?? null);
    setError("");
  };

  return (
    <div className="flex min-h-screen flex-col bg-zinc-50/50">
      <TopNav onHome={() => navigate("/products")} breadcrumbs="新建商品" />
      <main className="flex flex-1 items-start justify-center px-6 pt-20">
        <div className="w-full max-w-xl rounded-lg border border-zinc-200 bg-white p-8 shadow-sm">
          <div className="mb-8">
            <h2 className="text-lg font-semibold text-zinc-900">新建商品</h2>
          </div>

          <form onSubmit={handleSubmit} className="space-y-6">
            <div>
              <label className="mb-2 block text-[11px] font-semibold uppercase tracking-widest text-zinc-500">
                商品主图 <span className="text-red-500">*</span>
              </label>
              <ImageDropZone
                ariaLabel="上传商品主图"
                className="flex cursor-pointer flex-col items-center justify-center rounded-md border border-dashed border-zinc-300 p-8 text-zinc-500 transition-colors hover:border-zinc-400 hover:bg-zinc-50"
                onFiles={handleImageFiles}
              >
                {({ isDragging }) => (
                  <>
                    <ImagePlus size={24} className="mb-2 text-zinc-400" />
                    <p className="text-sm font-medium text-zinc-700">{isDragging ? "松开以上传图片" : previewLabel}</p>
                    <p className="mt-1 text-xs">JPEG/PNG/WebP，最大 5MB</p>
                  </>
                )}
              </ImageDropZone>
            </div>

            <div>
              <label className="mb-2 block text-[11px] font-semibold uppercase tracking-widest text-zinc-500">
                商品名称 <span className="text-red-500">*</span>
              </label>
              <input
                required
                type="text"
                value={name}
                onChange={(event) => setName(event.target.value)}
                className="w-full rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm transition-shadow focus:border-zinc-900 focus:outline-none focus:ring-1 focus:ring-zinc-900"
                placeholder="e.g. 春季新款复古碎花连衣裙"
              />
            </div>

            {error ? <div className="text-sm text-red-600">{error}</div> : null}

            <div className="mt-6 flex justify-end space-x-3 border-t border-zinc-100 pt-6">
              <button
                type="button"
                onClick={() => navigate("/products")}
                className="px-4 py-2 text-sm font-medium text-zinc-600 transition-colors hover:text-zinc-900"
              >
                取消
              </button>
              <button
                type="submit"
                disabled={createProductMutation.isPending}
                className="flex items-center rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-800 disabled:opacity-50"
              >
                {createProductMutation.isPending ? (
                  <Loader2 size={14} className="mr-2 animate-spin" />
                ) : null}
                创建并继续
              </button>
            </div>
          </form>
        </div>
      </main>
    </div>
  );
}
