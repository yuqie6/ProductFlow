import { useCallback, useEffect, useMemo, useState } from "react";
import type { FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Loader2,
  LockKeyhole,
  RotateCcw,
  Save,
  Settings as SettingsIcon,
} from "lucide-react";
import { useNavigate } from "react-router-dom";

import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import type { ConfigItem, ConfigResponse } from "../lib/types";

type DraftValue = string | boolean | string[];

function multiSelectValue(value: ConfigItem["value"]): string[] {
  if (Array.isArray(value)) {
    return value.map(String);
  }
  if (typeof value === "string") {
    return value
      .split(",")
      .map((part) => part.trim())
      .filter(Boolean);
  }
  return [];
}

function draftFromItem(item: ConfigItem): DraftValue {
  if (item.input_type === "boolean") {
    return Boolean(item.value);
  }
  if (item.input_type === "multi_select") {
    return multiSelectValue(item.value);
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
  compact?: boolean;
  onChange: (value: DraftValue, touchedSecret?: boolean) => void;
  onReset: () => void;
}

function ConfigField({ item, value, secretTouched, isResetting, compact = false, onChange, onReset }: ConfigFieldProps) {
  const baseInputClass =
    "w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm text-slate-950 transition-shadow placeholder:text-slate-400 focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500";
  const selectedMultiValues = Array.isArray(value) ? value : [];
  const toggleMultiValue = (optionValue: string) => {
    const selected = new Set(selectedMultiValues);
    if (selected.has(optionValue)) {
      selected.delete(optionValue);
    } else {
      selected.add(optionValue);
    }
    onChange(item.options.filter((option) => selected.has(option.value)).map((option) => option.value));
  };

  const description = item.secret
    ? item.has_value
      ? `${item.description || "密钥字段不会回显。"} 留空保存时不会覆盖当前值。`
      : item.description || "密钥字段不会回显。"
    : item.description;

  const control =
    item.input_type === "multi_select" ? (
      <div className="grid gap-2 sm:grid-cols-2">
        {item.options.map((option) => (
          <label
            key={`${item.key}-${option.value}`}
            className="inline-flex items-center gap-2 rounded-lg border border-slate-200 bg-white px-2.5 py-2 text-xs font-medium text-slate-700"
          >
            <input
              type="checkbox"
              checked={selectedMultiValues.includes(option.value)}
              onChange={() => toggleMultiValue(option.value)}
              className="h-3.5 w-3.5 accent-indigo-600"
            />
            <span>{option.label}</span>
          </label>
        ))}
      </div>
    ) : item.input_type === "select" ? (
      <select
        id={item.key}
        value={String(value)}
        onChange={(event) => onChange(event.target.value)}
        className={baseInputClass}
      >
        {item.options.map((option) => (
          <option key={`${item.key}-${option.value}`} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    ) : item.input_type === "textarea" ? (
      <textarea
        id={item.key}
        value={String(value)}
        onChange={(event) => onChange(event.target.value)}
        rows={item.key.startsWith("prompt_") ? 8 : 3}
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
        placeholder={item.secret && item.has_value ? "已有值，输入新值后覆盖" : item.description || undefined}
        onChange={(event) => onChange(event.target.value, item.secret)}
        className={baseInputClass}
        autoComplete={item.secret ? "new-password" : undefined}
      />
    );

  if (compact) {
    return (
      <div className="rounded-xl border border-slate-200 bg-white p-3">
        <div className="mb-2 flex min-w-0 items-center justify-between gap-2">
          <label htmlFor={item.key} className="truncate text-xs font-semibold text-zinc-900">
            {item.label}
          </label>
          <span className={`shrink-0 rounded-full border px-1.5 py-0.5 text-[10px] font-medium ${sourceClassName(item)}`}>
            {sourceLabel(item)}
          </span>
        </div>
        {control}
        <div className="mt-2 flex min-h-5 items-center justify-between gap-2">
          <span className="truncate font-mono text-[10px] text-zinc-400">{item.key}</span>
          {item.source === "database" ? (
            <button
              type="button"
              onClick={onReset}
              disabled={isResetting}
              className="inline-flex shrink-0 items-center text-[11px] font-medium text-zinc-500 transition-colors hover:text-zinc-900 disabled:opacity-50"
              aria-label={`恢复 ${item.label} 默认值`}
            >
              {isResetting ? <Loader2 size={12} className="animate-spin" /> : <RotateCcw size={12} />}
            </button>
          ) : null}
        </div>
      </div>
    );
  }

  return (
    <div className="grid gap-3 border-t border-slate-100 py-5 first:border-t-0 md:grid-cols-[220px_minmax(0,1fr)]">
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
        {control}

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
  const [unlockToken, setUnlockToken] = useState("");
  const [activeConfigCategory, setActiveConfigCategory] = useState<string | null>(null);

  const lockStateQuery = useQuery({
    queryKey: ["settings-lock-state"],
    queryFn: api.getSettingsLockState,
  });

  const configQuery = useQuery({
    queryKey: ["config"],
    queryFn: api.getConfig,
    enabled: Boolean(lockStateQuery.data?.unlocked),
  });

  const isCheckingLockState = lockStateQuery.isLoading || lockStateQuery.isFetching;

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

  useEffect(() => {
    if (configQuery.error instanceof ApiError && configQuery.error.status === 403) {
      queryClient.setQueryData(["settings-lock-state"], { unlocked: false, configured: true });
      queryClient.removeQueries({ queryKey: ["config"] });
    }
  }, [configQuery.error, queryClient]);

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
  const selectedCategoryIndex = activeConfigCategory
    ? groupedItems.findIndex((group) => group.category === activeConfigCategory)
    : -1;
  const activeGroupIndex = selectedCategoryIndex >= 0 ? selectedCategoryIndex : 0;
  const activeGroup = groupedItems[activeGroupIndex];
  const activePageNumber = activeGroup ? activeGroupIndex + 1 : 0;

  useEffect(() => {
    if (!groupedItems.length) {
      if (activeConfigCategory !== null) {
        setActiveConfigCategory(null);
      }
      return;
    }
    if (!activeConfigCategory || !groupedItems.some((group) => group.category === activeConfigCategory)) {
      setActiveConfigCategory(groupedItems[0].category);
    }
  }, [activeConfigCategory, groupedItems]);

  const saveMutation = useMutation({
    mutationFn: () => {
      const values: Record<string, string | number | boolean | string[] | null> = {};
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
      void queryClient.invalidateQueries({ queryKey: ["runtime-config"] });
      void queryClient.invalidateQueries({ queryKey: ["session"] });
      setError("");
      setSavedMessage("配置已写入数据库，后续任务会优先读取数据库配置。");
    },
    onError: (mutationError) => {
      setSavedMessage("");
      if (mutationError instanceof ApiError) {
        if (mutationError.status === 403) {
          void queryClient.invalidateQueries({ queryKey: ["settings-lock-state"] });
        }
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
      void queryClient.invalidateQueries({ queryKey: ["runtime-config"] });
      void queryClient.invalidateQueries({ queryKey: ["session"] });
      setSavedMessage("已删除数据库覆盖值，当前配置回退到 env/default。 ");
    },
    onError: (mutationError) => {
      if (mutationError instanceof ApiError) {
        if (mutationError.status === 403) {
          void queryClient.invalidateQueries({ queryKey: ["settings-lock-state"] });
        }
        setError(mutationError.detail);
        return;
      }
      setError(mutationError instanceof Error ? mutationError.message : "恢复配置失败");
    },
    onSettled: () => setResettingKey(null),
  });

  const unlockMutation = useMutation({
    mutationFn: () => api.unlockSettings(unlockToken),
    onSuccess: (data) => {
      queryClient.setQueryData(["settings-lock-state"], data);
      setUnlockToken("");
      setError("");
      setSavedMessage("系统配置已解锁。本次登录会话内可读取和修改运行时配置。");
      void queryClient.invalidateQueries({ queryKey: ["config"] });
    },
    onError: (mutationError) => {
      setSavedMessage("");
      if (mutationError instanceof ApiError) {
        setError(mutationError.detail);
        return;
      }
      setError(mutationError instanceof Error ? mutationError.message : "解锁配置失败");
    },
  });

  const logoutMutation = useMutation({
    mutationFn: api.destroySession,
    onSuccess: async () => {
      queryClient.removeQueries({ queryKey: ["settings-lock-state"] });
      queryClient.removeQueries({ queryKey: ["config"] });
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

  const handleUnlock = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError("");
    setSavedMessage("");
    unlockMutation.mutate();
  };

  const handleDiscardDrafts = async () => {
    setError("");
    setSavedMessage("");
    const result = await configQuery.refetch();
    resetDraftsFromConfig(result.data ?? configQuery.data);
  };

  return (
    <div className="flex min-h-screen flex-col bg-slate-50">
      <TopNav
        breadcrumbs="配置"
        onHome={() => navigate("/products")}
        onLogout={() => logoutMutation.mutate()}
      />

      <main className="mx-auto flex w-full max-w-5xl flex-1 px-6 py-8 lg:py-10">
        <div className="w-full">
          <div className="mb-8 flex flex-col gap-4 rounded-3xl border border-slate-200 bg-white p-6 shadow-sm shadow-slate-200/60 md:flex-row md:items-end md:justify-between">
            <div>
              <div className="mb-3 inline-flex items-center rounded-full border border-indigo-100 bg-indigo-50 px-3 py-1 text-xs font-semibold text-indigo-700">
                <SettingsIcon size={13} className="mr-1.5" /> Runtime Config
              </div>
              <h1 className="text-2xl font-semibold tracking-tight text-zinc-900">系统配置</h1>
              <p className="mt-1 text-sm text-slate-500">
                数据库配置优先生效，未写入数据库的字段继续使用 env/default 值。
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

          {isCheckingLockState ? (
            <div className="flex justify-center py-20 text-zinc-400">
              <Loader2 size={22} className="animate-spin" />
            </div>
          ) : lockStateQuery.isError ? (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              配置锁状态加载失败，请确认后端和数据库已启动。
            </div>
          ) : !lockStateQuery.data?.configured ? (
            <div className="rounded-2xl border border-amber-200 bg-amber-50 px-5 py-4 text-sm text-amber-800">
              设置解锁令牌未配置。请在后端环境变量中设置 SETTINGS_ACCESS_TOKEN 后重启服务，保护全局配置不被未授权修改。
            </div>
          ) : !lockStateQuery.data.unlocked ? (
            <form
              onSubmit={handleUnlock}
              className="mx-auto max-w-xl rounded-2xl border border-slate-200 bg-white p-6 shadow-sm shadow-slate-200/50"
            >
              <div className="mb-5 flex items-start gap-3">
                <span className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-indigo-50 text-indigo-600">
                  <LockKeyhole size={18} />
                </span>
                <div>
                  <h2 className="text-base font-semibold text-slate-950">需要二次令牌才能查看系统配置</h2>
                  <p className="mt-1 text-sm leading-6 text-slate-500">
                    模型、API Key、提示词和并发上限属于全局配置。请输入设置解锁令牌后再查看或修改这些配置。
                  </p>
                </div>
              </div>
              <label htmlFor="settings-token" className="text-sm font-medium text-slate-800">
                设置解锁令牌
              </label>
              <input
                id="settings-token"
                type="password"
                value={unlockToken}
                onChange={(event) => setUnlockToken(event.target.value)}
                className="mt-2 w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm text-slate-950 transition-shadow placeholder:text-slate-400 focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
                placeholder="输入 SETTINGS_ACCESS_TOKEN"
                autoComplete="current-password"
              />
              {error ? (
                <div className="mt-4 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                  {error}
                </div>
              ) : null}
              <div className="mt-5 flex justify-end">
                <button
                  type="submit"
                  disabled={unlockMutation.isPending || !unlockToken.trim()}
                  className="inline-flex items-center rounded-xl bg-indigo-600 px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-indigo-600/20 transition-colors hover:bg-indigo-500 disabled:opacity-50"
                >
                  {unlockMutation.isPending ? (
                    <Loader2 size={14} className="mr-2 animate-spin" />
                  ) : (
                    <LockKeyhole size={14} className="mr-2" />
                  )}
                  解锁配置
                </button>
              </div>
            </form>
          ) : configQuery.isLoading ? (
            <div className="flex justify-center py-20 text-zinc-400">
              <Loader2 size={22} className="animate-spin" />
            </div>
          ) : configQuery.isError ? (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {configQuery.error instanceof ApiError ? configQuery.error.detail : "配置加载失败，请确认后端和数据库已启动。"}
            </div>
          ) : (
            <form onSubmit={handleSubmit} className="space-y-6">
              {activeGroup ? (
                <>
                  <div className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm shadow-slate-200/50">
                    <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
                      <div>
                        <div className="text-sm font-semibold text-slate-950">配置分类</div>
                        <div className="mt-1 text-xs text-slate-500">
                          第 {activePageNumber} / {groupedItems.length} 页 · 当前 {activeGroup.items.length} 项
                        </div>
                      </div>
                      <div className="flex items-center gap-2">
                        <button
                          type="button"
                          onClick={() => setActiveConfigCategory(groupedItems[activeGroupIndex - 1]?.category ?? activeGroup.category)}
                          disabled={activeGroupIndex === 0}
                          className="inline-flex h-9 w-9 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-600 transition-colors hover:border-slate-300 hover:text-slate-950 disabled:opacity-40"
                          aria-label="上一类配置"
                        >
                          <ChevronLeft size={16} />
                        </button>
                        <span className="min-w-14 text-center text-xs font-semibold text-slate-500">
                          {activePageNumber}/{groupedItems.length}
                        </span>
                        <button
                          type="button"
                          onClick={() => setActiveConfigCategory(groupedItems[activeGroupIndex + 1]?.category ?? activeGroup.category)}
                          disabled={activeGroupIndex >= groupedItems.length - 1}
                          className="inline-flex h-9 w-9 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-600 transition-colors hover:border-slate-300 hover:text-slate-950 disabled:opacity-40"
                          aria-label="下一类配置"
                        >
                          <ChevronRight size={16} />
                        </button>
                      </div>
                    </div>
                    <div className="mt-4 flex gap-2 overflow-x-auto pb-1" role="tablist" aria-label="配置分类">
                      {groupedItems.map((group, index) => {
                        const active = group.category === activeGroup.category;
                        return (
                          <button
                            key={group.category}
                            type="button"
                            role="tab"
                            aria-selected={active}
                            aria-controls="settings-category-panel"
                            onClick={() => setActiveConfigCategory(group.category)}
                            className={`inline-flex shrink-0 items-center gap-2 rounded-xl border px-3 py-2 text-sm font-semibold transition-colors ${
                              active
                                ? "border-indigo-200 bg-indigo-50 text-indigo-700"
                                : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:text-slate-950"
                            }`}
                          >
                            <span>{group.category}</span>
                            <span
                              className={`rounded-full px-1.5 py-0.5 text-[11px] ${
                                active ? "bg-white text-indigo-600" : "bg-slate-100 text-slate-500"
                              }`}
                            >
                              {index + 1}/{groupedItems.length}
                            </span>
                          </button>
                        );
                      })}
                    </div>
                  </div>

                  <section
                    id="settings-category-panel"
                    role="tabpanel"
                    className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm shadow-slate-200/50"
                  >
                    <div className="border-b border-slate-100 bg-slate-50/70 px-5 py-4">
                      <h2 className="text-sm font-semibold text-slate-950">{activeGroup.category}</h2>
                      <p className="mt-1 text-xs text-slate-500">{activeGroup.items.length} 项运行时配置</p>
                    </div>
                    <div className={activeGroup.category === "图片工具参数" ? "grid gap-3 p-5 sm:grid-cols-2" : "px-5"}>
                      {activeGroup.items.map((item) => (
                        <ConfigField
                          key={item.key}
                          item={item}
                          value={drafts[item.key] ?? draftFromItem(item)}
                          secretTouched={Boolean(secretTouched[item.key])}
                          isResetting={resettingKey === item.key}
                          compact={activeGroup.category === "图片工具参数"}
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
                </>
              ) : (
                <div className="rounded-2xl border border-dashed border-slate-200 bg-white px-5 py-10 text-center text-sm text-slate-500">
                  暂无可配置项
                </div>
              )}

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
                    className="inline-flex items-center rounded-xl bg-indigo-600 px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-indigo-600/20 transition-colors hover:bg-indigo-500 disabled:opacity-50"
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
