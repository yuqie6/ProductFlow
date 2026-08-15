# Backend Directory Structure

## Overview

The Python backend lives under `backend/src/productflow_backend/` and uses four layers:

- `presentation/` for FastAPI and wire contracts;
- `application/` for business use cases and transaction orchestration;
- `domain/` for database-free rules, enums, and business errors;
- `infrastructure/` for database, storage, queues, providers, logging, and service clients.

`main.py` only exposes the app created by `presentation/api.py`.

The Go workflow Agent is an independent module under `agent-service/`. It owns durable Agent execution, while ProductFlow business data remains in the Python backend.

## Current Layout

```text
backend/
  alembic/
    env.py
    versions/
  src/productflow_backend/
    main.py
    config.py
    workers.py
    domain/
      enums.py
      errors.py
      workflow_rules.py
      durable_generation_tasks.py
    application/
      agent_conversations.py
      agent_control.py
      agent_product_intake.py
      agent_product_workspaces.py
      agent_sync.py
      agent_tools.py
      agent_workbenches.py
      delivery_renditions.py
      durable_recovery.py
      gallery_mutations.py
      gallery_queries.py
      image_generation_core.py
      image_session_dependencies.py
      image_sessions.py
      media_assets.py
      product_gallery.py
      product_workflows.py
      product_workflow/
      product_workflow_dependencies.py
      provider_profiles.py
      queue_submission.py
      runtime_settings.py
      settings.py
      storage_compensation.py
      use_cases.py
      workflow_drafts/
    infrastructure/
      agent_service.py
      db/
      image/
      logging.py
      prompts.py
      provider_config.py
      queue.py
      runtime_config_store.py
      storage.py
    presentation/
      api.py
      deps.py
      errors.py
      routes/
      schemas/
      upload_validation.py
  tests/
```

Use `rg --files` before relying on this map; the source tree is authoritative.

## Presentation

`presentation/api.py` owns:

- FastAPI construction;
- middleware;
- exception handlers;
- health endpoint;
- router registration.

`presentation/routes/` groups current resources:

- auth;
- Agent runtime, conversations, workspaces, workbench, and internal tools;
- products and canonical product images;
- WorkflowDraft/V2 workflow;
- WorkflowRecipe;
- image sessions;
- gallery;
- delivery renditions;
- generation queue;
- settings.

`presentation/schemas/` owns Pydantic wire DTOs and serializers. Routes may validate path/query/form concerns and delegate one business operation. Provider calls, graph algorithms, storage mutation, and multi-step transaction logic do not belong here.

## Application

The application layer owns use cases and receives a SQLAlchemy Session from its caller.

Key boundaries:

- `agent_product_workspaces.py`: initial Product/Draft/Conversation creation.
- `agent_conversations.py`: ProductFlow projection and idempotent Turn reservation.
- `agent_control.py` / `agent_sync.py`: remote Turn control and reconciliation.
- `agent_tools.py`: product-scoped read tools and durable image-library mutations.
- `workflow_drafts/`: versioned Draft contracts, revisions, confirmation, materialization, and reveal.
- `product_workflow/`: current graph commands, node editing, references, execution, runs, folders, recipes, and lineage.
- `product_workflows.py`: narrow stable execution facade used by workers/composition.
- `product_gallery.py`, `gallery_queries.py`, `gallery_mutations.py`: canonical product image library.
- `image_sessions.py`: iterative generation sessions and save-to-product.
- `delivery_renditions.py`: delivery-format jobs.
- `settings.py` / `provider_profiles.py`: runtime settings, profiles, bindings, and import/export.
- `durable_recovery.py` and `queue_submission.py`: durable row / broker delivery boundaries.
- `storage_compensation.py`: cleanup of bytes created by a failed database mutation.

When a module becomes large, split by real ownership or executable boundary. Do not add a facade solely to rename imports.

## Domain

`domain/enums.py` is the shared string-enum authority for current runtime states and node types. Enum changes require database, schema, frontend, and migration review.

`domain/errors.py` defines typed business failures. Presentation maps them to HTTP.

`domain/workflow_rules.py` owns DB-free DAG ordering/readiness. `domain/durable_generation_tasks.py` owns DB-free queue/task classification.

Domain code must not import FastAPI, SQLAlchemy models, provider SDKs, or storage.

## Infrastructure

- `db/models.py`: typed SQLAlchemy declarative models.
- `db/session.py`: engine and session factory.
- `storage.py`: local media storage and image variants.
- `queue.py`: Dramatiq broker/message adapters.
- `agent_service.py`: internal HTTP/SSE client.
- `image/`: provider-neutral protocols and concrete image adapters.
- `provider_config.py`: ProviderProfile/ProviderBinding resolution.
- `runtime_config_store.py`: AppSetting reads.
- `prompts.py`: operator-configurable prompt rendering.
- `logging.py`: persistent logging configuration and redaction boundaries.

Concrete provider SDKs stay under infrastructure. Application code depends on protocols/resolved configuration rather than SDK response objects.

## Workers

`workers.py` is the Dramatiq composition root:

- creates sessions;
- injects current dependencies;
- calls application execution functions;
- handles actor-level retry/delivery behavior;
- closes sessions.

Workers do not duplicate application business logic.

## Tests

Tests live in `backend/tests/` and are grouped by current behavior:

- Agent workspace/conversation/service;
- WorkflowDraft and V2 workflow;
- graph commands and node editing;
- prompt/visual/image nodes;
- gallery/media/image sessions;
- provider/settings;
- queue/recovery;
- storage/upload;
- migration/database constraints;
- opt-in live dependency/provider flows.

Shared fixtures stay in `conftest.py`, `helpers.py`, and focused helper modules. Keep regressions near the trigger.

## Naming

- modules/functions: `snake_case`;
- model/DTO classes: `PascalCase`;
- request/response wire classes end in `Request` / `Response` where useful;
- private helpers start with `_` and are not imported across application modules;
- public shared helpers have explicit owner names;
- SQLAlchemy class names are singular and table names plural.

## Avoid

- Route modules not registered in `presentation/api.py`.
- DTO duplication outside `presentation/schemas/`.
- Provider SDK, storage path, or queue internals in routes.
- FastAPI objects in application/domain.
- SQLAlchemy or config database reads inside `config.py`.
- Hidden commits in application helpers.
- Re-exporting private helpers through a large compatibility facade.
- New runtime modules for a data model that is absent from current SQLAlchemy metadata.
