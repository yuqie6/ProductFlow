import { useCallback, useEffect, useMemo, useState } from "react";
import type { FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Loader2, RotateCcw, Save, Settings as SettingsIcon } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import type { ConfigItem, ConfigResponse } from "../lib/types";

type DraftValue = string | boolean;

function draftFromItem(item: ConfigItem): DraftValue {
  if (item.input_type === "boolean") {
    return Boolean(item.value);
  }
  if (item.secret) {
    return "";
  }
  return item.value === null || item.value === undefined ? "" : String(item.value);
}

function draftsFromConfig(config: ConfigResponse): Record<string, DraftValue> {
  const nextDrafts: Record<string, DraftValue> = {};
  for (const item of config.items) {
    nextDrafts[item.key] = draftFromItem(item);
  }
  return nextDrafts;
}

function sourceLabel(item: ConfigItem): string {
  if (item.source === "database") {
    return "数据库";
  }
  return "env/default";
}

function sourceClassName(item: ConfigItem): string {
  if (item.source === "database") {
    return "border-emerald-200 bg-emerald-50 text-emerald-700";
  }
  return "border-zinc-200 bg-zinc-50 text-zinc-500";
}

interface ConfigFieldProps {
  item: ConfigItem;
  value: DraftValue;
  secretTouched: boolean;
  isResetting: boolean;
  onChange: (value: DraftValue, touchedSecret?: boolean) => void;
  onReset: () => void;
}

function ConfigField({ item, value, secretTouched, isResetting, onChange, onReset }: ConfigFieldProps) {
  const baseInputClass =
    "w-full rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-900 transition-shadow placeholder:text-zinc-400 focus:border-zinc-900 focus:outline-none focus:ring-1 focus:ring-zinc-900";

  const description = item.secret
    ? item.has_value
      ? `${item.description || "密钥字段不会回显。"} 留空保存时不会覆盖当前值。`
      : item.description || "密钥字段不会回显。"
    : item.description;

  return (
    <div className="grid gap-3 border-t border-zinc-100 py-5 first:border-t-0 md:grid-cols-[220px_minmax(0,1fr)]">
      <div>
        <div className="flex flex-wrap items-center gap-2">
          <label htmlFor={item.key} className="text-sm font-medium text-zinc-900">
            {item.label}
          </label>
          <span className={`rounded-full border px-2 py-0.5 text-[11px] font-medium ${sourceClassName(item)}`}>
            {sourceLabel(item)}
          </span>
        </div>
        <div className="mt-1 font-mono text-[11px] text-zinc-400">{item.key}</div>
      </div>

      <div className="space-y-2">
        {item.input_type === "select" ? (
          <select
            id={item.key}
            value={String(value)}
            onChange={(event) => onChange(event.target.value)}
            className={baseInputClass}
          >
            {item.options.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        ) : item.input_type === "textarea" ? (
          <textarea
            id={item.key}
            value={String(value)}
            onChange={(event) => onChange(event.target.value)}
            rows={3}
            className={`${baseInputClass} resize-y leading-6`}
          />
        ) : item.input_type === "boolean" ? (
          <label className="inline-flex cursor-pointer items-center gap-3 rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-700 transition-colors hover:border-zinc-300">
            <input
              id={item.key}
              type="checkbox"
              checked={Boolean(value)}
              onChange={(event) => onChange(event.target.checked)}
              className="h-4 w-4 accent-zinc-900"
            />
            <span>{Boolean(value) ? "已启用" : "已关闭"}</span>
          </label>
        ) : (
          <input
            id={item.key}
            type={item.input_type === "password" ? "password" : item.input_type === "number" ? "number" : "text"}
            value={String(value)}
            min={item.minimum ?? undefined}
            max={item.maximum ?? undefined}
            placeholder={item.secret && item.has_value ? "已有值，输入新值后覆盖" : undefined}
            onChange={(event) => onChange(event.target.value, item.secret)}
            className={baseInputClass}
            autoComplete={item.secret ? "new-password" : undefined}
          />
        )}

        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="min-h-4 text-xs leading-5 text-zinc-500">
            {description}
            {item.secret && secretTouched ? <span className="ml-2 text-amber-600">将写入新的数据库值</span> : null}
          </p>
          {item.source === "database" ? (
            <button
              type="button"
              onClick={onReset}
              disabled={isResetting}
              className="inline-flex items-center text-xs font-medium text-zinc-500 transition-colors hover:text-zinc-900 disabled:opacity-50"
            >
              {isResetting ? <Loader2 size={13} className="mr-1 animate-spin" /> : <RotateCcw size={13} className="mr-1" />}
              恢复 env/default
            </button>
          ) : null}
        </div>
      </div>
    </div>
  );
}

export function SettingsPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [drafts, setDrafts] = useState<Record<string, DraftValue>>({});
  const [secretTouched, setSecretTouched] = useState<Record<string, boolean>>({});
  const [resettingKey, setResettingKey] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [savedMessage, setSavedMessage] = useState("");

  const configQuery = useQuery({
    queryKey: ["config"],
    queryFn: api.getConfig,
  });

  const resetDraftsFromConfig = useCallback((config: ConfigResponse | undefined) => {
    if (!config) {
      return;
    }
    setDrafts(draftsFromConfig(config));
    setSecretTouched({});
  }, []);

  useEffect(() => {
    resetDraftsFromConfig(configQuery.data);
  }, [configQuery.data, resetDraftsFromConfig]);

  const groupedItems = useMemo(() => {
    const groups: Array<{ category: string; items: ConfigItem[] }> = [];
    const byCategory = new Map<string, ConfigItem[]>();
    for (const item of configQuery.data?.items ?? []) {
      if (!byCategory.has(item.category)) {
        byCategory.set(item.category, []);
        groups.push({ category: item.category, items: byCategory.get(item.category) ?? [] });
      }
      byCategory.get(item.category)?.push(item);
    }
    return groups;
  }, [configQuery.data]);

  const saveMutation = useMutation({
    mutationFn: () => {
      const values: Record<string, string | number | boolean | null> = {};
      for (const item of configQuery.data?.items ?? []) {
        if (item.secret && !secretTouched[item.key]) {
          continue;
        }
        values[item.key] = drafts[item.key] ?? "";
      }
      return api.updateConfig({ values });
    },
    onSuccess: (data) => {
      queryClient.setQueryData(["config"], data);
      setError("");
      setSavedMessage("配置已写入数据库，后续任务会优先读取数据库配置。");
    },
    onError: (mutationError) => {
      setSavedMessage("");
      if (mutationError instanceof ApiError) {
        setError(mutationError.detail);
        return;
      }
      setError(mutationError instanceof Error ? mutationError.message : "保存配置失败");
    },
  });

  const resetMutation = useMutation({
    mutationFn: (key: string) => api.updateConfig({ reset_keys: [key] }),
    onMutate: (key) => {
      setResettingKey(key);
      setError("");
      setSavedMessage("");
    },
    onSuccess: (data) => {
      queryClient.setQueryData(["config"], data);
      setSavedMessage("已删除数据库覆盖值，当前配置回退到 env/default。 ");
    },
    onError: (mutationError) => {
      if (mutationError instanceof ApiError) {
        setError(mutationError.detail);
        return;
      }
      setError(mutationError instanceof Error ? mutationError.message : "恢复配置失败");
    },
    onSettled: () => setResettingKey(null),
  });

  const logoutMutation = useMutation({
    mutationFn: api.destroySession,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      navigate("/login", { replace: true });
    },
  });

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError("");
    setSavedMessage("");
    saveMutation.mutate();
  };

  const handleDiscardDrafts = async () => {
    setError("");
    setSavedMessage("");
    const result = await configQuery.refetch();
    resetDraftsFromConfig(result.data ?? configQuery.data);
  };

  return (
    <div className="flex min-h-screen flex-col bg-zinc-50/50">
      <TopNav
        breadcrumbs="配置"
        onHome={() => navigate("/products")}
        onLogout={() => logoutMutation.mutate()}
      />

      <main className="mx-auto flex w-full max-w-5xl flex-1 px-6 py-10">
        <div className="w-full">
          <div className="mb-8 flex flex-col gap-4 border-b border-zinc-200 pb-6 md:flex-row md:items-end md:justify-between">
            <div>
              <div className="mb-3 inline-flex items-center rounded-full border border-zinc-200 bg-white px-3 py-1 text-xs font-medium text-zinc-500">
                <SettingsIcon size={13} className="mr-1.5" /> Runtime Config
              </div>
              <h1 className="text-2xl font-semibold tracking-tight text-zinc-900">系统配置</h1>
              <p className="mt-1 text-sm text-zinc-500">
                数据库配置优先生效；未写入数据库的字段继续使用 env/default 值。
              </p>
            </div>
            <button
              type="button"
              onClick={() => navigate("/products")}
              className="text-sm font-medium text-zinc-500 transition-colors hover:text-zinc-900"
            >
              返回商品列表
            </button>
          </div>

          {configQuery.isLoading ? (
            <div className="flex justify-center py-20 text-zinc-400">
              <Loader2 size={22} className="animate-spin" />
            </div>
          ) : configQuery.isError ? (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              配置加载失败，请确认后端和数据库已启动。
            </div>
          ) : (
            <form onSubmit={handleSubmit} className="space-y-8">
              <div className="overflow-hidden rounded-lg border border-zinc-200 bg-white shadow-sm">
                {groupedItems.map((group, index) => (
                  <section key={group.category} className={index === 0 ? "" : "border-t border-zinc-200"}>
                    <div className="bg-zinc-50/70 px-5 py-4">
                      <h2 className="text-sm font-semibold text-zinc-900">{group.category}</h2>
                    </div>
                    <div className="px-5">
                      {group.items.map((item) => (
                        <ConfigField
                          key={item.key}
                          item={item}
                          value={drafts[item.key] ?? draftFromItem(item)}
                          secretTouched={Boolean(secretTouched[item.key])}
                          isResetting={resettingKey === item.key}
                          onChange={(nextValue, touchedSecret) => {
                            setDrafts((current) => ({ ...current, [item.key]: nextValue }));
                            setSavedMessage("");
                            if (touchedSecret) {
                              setSecretTouched((current) => ({ ...current, [item.key]: true }));
                            }
                          }}
                          onReset={() => resetMutation.mutate(item.key)}
                        />
                      ))}
                    </div>
                  </section>
                ))}
              </div>

              {error ? (
                <div className="rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>
              ) : null}
              {savedMessage ? (
                <div className="flex items-center rounded-md border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">
                  <CheckCircle2 size={16} className="mr-2" /> {savedMessage}
                </div>
              ) : null}

              <div className="sticky bottom-0 -mx-6 border-t border-zinc-200 bg-white/90 px-6 py-4 backdrop-blur">
                <div className="mx-auto flex max-w-5xl justify-end gap-3">
                  <button
                    type="button"
                    onClick={handleDiscardDrafts}
                    disabled={configQuery.isFetching}
                    className="px-4 py-2 text-sm font-medium text-zinc-600 transition-colors hover:text-zinc-900 disabled:opacity-50"
                  >
                    {configQuery.isFetching ? "正在恢复..." : "放弃未保存修改"}
                  </button>
                  <button
                    type="submit"
                    disabled={saveMutation.isPending}
                    className="inline-flex items-center rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-800 disabled:opacity-50"
                  >
                    {saveMutation.isPending ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Save size={14} className="mr-2" />}
                    保存到数据库
                  </button>
                </div>
              </div>
            </form>
          )}
        </div>
      </main>
    </div>
  );
}
