# Frontend Hook Guidelines

## General Rules

- Call hooks unconditionally at the top level.
- Keep dependency arrays complete.
- Use TanStack Query for remote records and React state for interaction drafts.
- Extract a custom hook when it owns a coherent lifecycle, not just to shorten a component.
- Keep feature hooks beside their feature.

## Query Hooks

Every query needs:

- a stable feature-owned query key;
- a typed `api.ts` function;
- an `enabled` guard when ids are optional;
- bounded retry/polling behavior;
- a clear loading/error/empty projection.

Query functions should not capture changing objects that are absent from the key.

## Mutation Hooks

A mutation owns:

- typed variables;
- server call;
- pending feedback;
- precise cache update/invalidation;
- action-local error;
- cleanup of optimistic state.

Use explicit idempotency keys where the API requires them. Do not generate a new key during a retry of the same user action.

## Polling

Poll only lightweight status endpoints while work is active.

- Return a numeric interval only for queued/running/cancel-requested states.
- Stop after terminal state.
- Refetch full detail once when the active status becomes terminal.
- Keep background polling bounded and visible.
- Do not poll full image history, graph payload, or media bytes.

ImageChat status polling and Agent SSE are separate mechanisms. Do not apply the polling contract to an event stream.

## Agent Hooks

`useAgentConversation` owns conversation/Turn pages and control mutations.

`useAgentTurnEvents` owns:

- SSE open/close;
- Last-Event-ID/after cursor;
- sequence deduplication;
- heartbeat handling;
- delta dispatch;
- reconnect/backoff;
- terminal refetch.

`agentEventReducer` is pure and testable. It must ignore repeated/out-of-order sequence and preserve unknown state.

`useWorkflowMaterialization` starts or resumes materialization exactly once for the confirmed revision.

`useWorkflowReveal` projects ordered reveal events without creating server graph state in the browser.

## V2 Node Autosave

`useV2NodeDraftAutosave` is the node-edit owner.

Contract:

- initialize from the latest node detail;
- normalize draft before equality comparison;
- debounce valid changes;
- flush before node selection, panel close, or action that depends on saved content;
- send expected edit/concurrency value;
- show pending/saved/conflict/error state;
- refetch canonical node/workflow after success or conflict;
- never overwrite a newer server edit silently.

Incomplete form state remains local and is not submitted.

## Product Image Explorer Hook

`useProductImageExplorer` owns:

- bootstrap and infinite asset pages;
- directory/search/sort/view;
- selection cap;
- folder and asset mutations;
- exact query-chain cleanup;
- reference-target binding.

ResizeObserver composition stays in the component because it observes rendered layout.

## ImageChat Controller State

Page-local hooks/helpers may own:

- selected session/result;
- active generation task;
- branch/context selection;
- candidate count and tool options;
- save-to-product mutation;
- drawers/panels.

Derive candidate tree/history placeholders from server ids/status. Avoid duplicated arrays in state.

## Settings Hooks

Settings state is sensitive:

- fetch lock state before protected config;
- keep unlock token only in submit-local/component state;
- represent secret updates as touched/untouched;
- invalidate runtime/session/provider queries after relevant save;
- do not include secret in query keys, logs, or persistent state.

## Effects

Use effects for external synchronization:

- event source;
- ResizeObserver;
- document preference attributes;
- localStorage persistence;
- navigation after confirmed lifecycle change.

Do not use effects to calculate render-only derived state.

When an effect registers an external resource, return cleanup. StrictMode double mounting must not create duplicate active streams or observers.

## Refs

Use refs for:

- DOM measurement/focus;
- latest callback for a long-lived stream;
- stable drag/gesture state that does not need render;
- previous terminal status for one-time full refetch;
- pending debounce timer.

Do not hide user-visible state in refs.

## Local State

Good local state:

- form draft;
- selected id;
- dialog/drawer open state;
- temporary validation;
- canvas interaction.

Server records stay in Query cache. Derive current object from cached list/detail and selected id.

## Error Handling

Narrow errors through `ApiError`. Preserve server detail for action-local feedback. Connection failures may use a localized generic message.

An effect/stream error should expose reconnect/retry state; it must not throw during render.

## Tests

Test pure helpers/reducers separately and hooks where lifecycle matters:

- query key and invalidation;
- polling active-to-terminal transition;
- SSE dedup/reconnect/terminal;
- autosave debounce/flush/conflict;
- image explorer query identity and selection cap;
- branch/context limit;
- settings secret touched state.

## Avoid

- Missing effect dependencies.
- Query key that omits a filter/id used by queryFn.
- Full-detail polling.
- New idempotency key on transport retry.
- Duplicate EventSource under StrictMode.
- Autosave per inspector subsection.
- Mirroring a full server object in useState.
- Broad cache clear.
- Secret-bearing query state.
