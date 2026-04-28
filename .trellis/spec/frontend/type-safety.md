# Frontend Type Safety

> TypeScript and API typing conventions used by ProductFlow.

---

## Overview

The frontend uses strict TypeScript. `web/tsconfig.app.json` sets `strict: true`, `allowJs: false`,
`isolatedModules: true`, `moduleResolution: "Bundler"`, and `jsx: "react-jsx"`. The build command in `web/package.json`
runs TypeScript checks before Vite build:

```bash
pnpm --dir web build
# tsc --noEmit -p tsconfig.app.json && tsc --noEmit -p tsconfig.node.json && vite build
```

Runtime API typing is centralized in:

- `web/src/lib/types.ts`
- `web/src/lib/api.ts`

---

## API DTO Types

`web/src/lib/types.ts` mirrors backend Pydantic response/request shapes. It intentionally preserves backend field names,
including `snake_case`:

- `ProductSummary.workflow_state`
- `CopySet.creative_brief_id`
- `JobRun.failure_reason`
- `ImageSessionRound.provider_response_id`
- `SessionState.access_required`
- `RuntimeConfig.admin_access_required`
- `ConfigUpdateRequest.reset_keys`

Do not silently convert these to camelCase in frontend types unless the API layer also performs explicit mapping.

String union types mirror backend enums:

```ts
export type ProductWorkflowState = "draft" | "copy_ready" | "poster_ready" | "failed";
export type JobStatus = "queued" | "running" | "succeeded" | "failed";
```

If backend enum values in `backend/src/productflow_backend/domain/enums.py` change, update these unions and all UI maps
such as `StatusPill.tsx::CONFIG`.

---

## API Client Typing

`web/src/lib/api.ts` exposes typed methods on the `api` object. The internal `request<T>(...)` returns a `Promise<T>` and
throws typed `ApiError` on non-2xx responses.

Examples:

```ts
getProduct(productId: string): Promise<ProductDetail> {
  return request(`/api/products/${productId}`);
}

updateConfig(payload: ConfigUpdateRequest): Promise<ConfigResponse> {
  return request("/api/settings", {
    method: "PATCH",
    body: JSON.stringify(payload),
  });
}
```

Form uploads build `FormData` in API methods such as `createProduct(...)`, `addReferenceImages(...)`, and
`addImageSessionReferenceImages(...)`. The fetch wrapper omits `Content-Type` for `FormData` so the browser can set the
multipart boundary.

---

## Local Types

Use local `type` aliases for page-only structures:

- `EditableCopy` in `ProductDetailPage.tsx`.
- `DraftValue` in `SettingsPage.tsx`.

Use `interface` for component props and DTO object shapes:

- `TopNavProps` in `TopNav.tsx`.
- `ConfigFieldProps` in `SettingsPage.tsx`.
- API DTOs in `web/src/lib/types.ts`.

Static option arrays can use `as const`, as in `ImageChatPage.tsx::DEFAULT_SIZE_OPTIONS`.

---

## Runtime Validation Reality

The frontend currently relies on backend validation for API payloads and on TypeScript for compile-time checks. There is no
Zod/Yup/io-ts runtime validation layer in `web/src/`.

Existing frontend-side validation is lightweight and UI-oriented:

- Required form fields and file accept attributes in `ProductCreatePage.tsx`.
- Config input types/min/max from backend-provided `ConfigItem` metadata in `SettingsPage.tsx`.
- Allowed image size options derived from `/api/settings` in `ImageChatPage.tsx`.

Do not add a validation library unless a feature truly needs client-side runtime parsing beyond backend errors.

---

## Handling Unknown Data

Use `unknown`, not `any`, for flexible payloads. `CreativeBriefSummary.payload` in `web/src/lib/types.ts` allows known
optional fields and `[key: string]: unknown` for provider-specific additions.

When narrowing errors, follow current patterns:

```ts
if (mutationError instanceof ApiError) {
  setError(mutationError.detail);
  return;
}
setError(mutationError instanceof Error ? mutationError.message : "创建商品失败");
```

---

## Avoid

- `any` in API types, component props, or mutation payloads.
- Duplicating DTO interfaces inside pages instead of importing from `web/src/lib/types.ts`.
- Renaming API fields to camelCase only on the frontend.
- Type assertions that hide missing null checks; prefer `enabled: Boolean(id)` for queries and explicit null rendering.
- Adding new backend response fields without updating `web/src/lib/types.ts` and the relevant UI.
