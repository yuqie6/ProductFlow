import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, Image as ImageIcon, MessagesSquare, Loader2, Plus, Settings, Trash2 } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useState } from "react";

import { TopNav } from "../components/TopNav";
import { StatusPill } from "../components/StatusPill";
import { api, ApiError } from "../lib/api";
import { formatPrice, formatShortDate } from "../lib/format";

export function ProductListPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [deleteError, setDeleteError] = useState("");
  const productsQuery = useQuery({
    queryKey: ["products"],
    queryFn: api.listProducts,
  });
  const products = productsQuery.data?.items ?? [];

  const logoutMutation = useMutation({
    mutationFn: api.destroySession,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      navigate("/login", { replace: true });
    },
  });

  const deleteProductMutation = useMutation({
    mutationFn: (productId: string) => api.deleteProduct(productId),
    onSuccess: async () => {
      setDeleteError("");
      await queryClient.invalidateQueries({ queryKey: ["products"] });
    },
    onError: (mutationError) => {
      setDeleteError(
        mutationError instanceof ApiError ? mutationError.detail : "删除商品失败",
      );
    },
  });

  const handleDeleteProduct = (productId: string, productName: string) => {
    if (!window.confirm(`确定删除「${productName}」吗？此操作不可恢复。`)) {
      return;
    }
    deleteProductMutation.mutate(productId);
  };

  return (
    <div className="flex min-h-screen flex-col bg-zinc-50/50">
      <TopNav
        onHome={() => navigate("/products")}
        onLogout={() => logoutMutation.mutate()}
      />

      <main className="mx-auto flex w-full max-w-5xl flex-1 px-6 py-12">
        <div className="w-full">
          <div className="mb-8 flex items-end justify-between">
            <div>
              <h1 className="text-2xl font-semibold tracking-tight text-zinc-900">商品列表</h1>
              <p className="mt-1 text-sm text-zinc-500">
                管理商品素材、文案版本与海报成品。
              </p>
            </div>
            <div className="flex items-center gap-3">
              <button
                type="button"
                onClick={() => navigate("/settings")}
                className="flex items-center rounded-md border border-zinc-200 bg-white px-4 py-2 text-sm font-medium text-zinc-700 transition-colors hover:border-zinc-300 hover:text-zinc-900"
              >
                <Settings size={16} className="mr-1.5" /> 配置
              </button>
              <button
                type="button"
                onClick={() => navigate("/image-chat")}
                className="flex items-center rounded-md border border-zinc-200 bg-white px-4 py-2 text-sm font-medium text-zinc-700 transition-colors hover:border-zinc-300 hover:text-zinc-900"
              >
                <MessagesSquare size={16} className="mr-1.5" /> 连续生图
              </button>
              <button
                type="button"
                onClick={() => navigate("/products/new")}
                className="flex items-center rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-800"
              >
                <Plus size={16} className="mr-1.5" /> 新建商品
              </button>
            </div>
          </div>

          {deleteError ? (
            <div className="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {deleteError}
            </div>
          ) : null}

          {productsQuery.isLoading ? (
            <div className="flex justify-center py-20 text-zinc-400">
              <Loader2 size={20} className="animate-spin" />
            </div>
          ) : productsQuery.isError ? (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              商品列表加载失败，请确认后端已启动。
            </div>
          ) : (
            <div className="overflow-hidden rounded-lg border border-zinc-200 bg-white shadow-sm">
              <table className="w-full border-collapse text-left text-sm">
                <thead>
                  <tr className="border-b border-zinc-200 bg-zinc-50/50">
                    <th className="w-[45%] px-5 py-3 font-medium text-zinc-500">商品素材</th>
                    <th className="px-5 py-3 font-medium text-zinc-500">流程状态</th>
                    <th className="px-5 py-3 font-medium text-zinc-500">最后更新</th>
                    <th className="px-5 py-3 text-right font-medium text-zinc-500">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-zinc-100">
                  {products.map((product) => (
                    <tr key={product.id} className="group transition-colors hover:bg-zinc-50">
                      <td className="px-5 py-4">
                        <div className="flex items-center space-x-3">
                          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded border border-zinc-200 bg-zinc-100 text-zinc-400">
                            <ImageIcon size={18} strokeWidth={1.5} />
                          </div>
                          <div className="min-w-0">
                            <div className="truncate font-medium text-zinc-900">{product.name}</div>
                            <div className="mt-0.5 flex space-x-3 text-xs text-zinc-500">
                              {product.category ? <span>{product.category}</span> : null}
                              {product.price ? <span>{formatPrice(product.price)}</span> : null}
                            </div>
                          </div>
                        </div>
                      </td>
                      <td className="px-5 py-4">
                        <StatusPill status={product.workflow_state} />
                      </td>
                      <td className="px-5 py-4 font-mono text-xs text-zinc-500">
                        {formatShortDate(product.updated_at)}
                      </td>
                      <td className="px-5 py-4 text-right">
                        <div className="flex items-center justify-end gap-3 opacity-0 transition-opacity group-hover:opacity-100">
                          <button
                            type="button"
                            onClick={() => handleDeleteProduct(product.id, product.name)}
                            disabled={deleteProductMutation.isPending}
                            className="inline-flex items-center text-sm font-medium text-red-500 transition-colors hover:text-red-700 disabled:opacity-50"
                          >
                            <Trash2 size={14} className="mr-1" /> 删除
                          </button>
                          <button
                            type="button"
                            onClick={() => navigate(`/products/${product.id}`)}
                            className="inline-flex items-center text-sm font-medium text-zinc-600 transition-colors hover:text-zinc-900"
                          >
                            打开 <ArrowRight size={14} className="ml-1" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </main>
    </div>
  );
}
