# ProductFlow Roadmap

[中文](ROADMAP.md) | English

This roadmap describes the evolution direction of the open-source self-hosted version. It is not a hosted-service commitment.

## Current Stage: Open-Source Self-Hosted and Runnable

Completed baseline capabilities:

- FastAPI backend, React/Vite frontend, PostgreSQL, Redis, and Dramatiq worker.
- Single-admin login and private workspace.
- Product creation, image upload, and reference image management.
- Copy generation, editing, confirmation, and history.
- Template poster generation, AI image-provider poster generation, and poster download.
- Iterative image sessions and attaching generated images back to products.
- Product DAG workflow editing, execution, persistent state, and recovery.
- In-product guided onboarding and shared top navigation.
- ProductFlow workbench canvas interactions: mouse-wheel zoom, left-drag pan, node drag positioning, and edge drag creation/deletion.
- Single-slot semantics for reference images, image drag-and-drop upload, compact right sidebar for Details / Runs / Images, and asset fill.
- Prompt configuration: product understanding, copy, workbench image generation, and iterative image-generation templates can be overridden in the settings page.
- Initial product brand assets, README preview images, and Web favicon/metadata.
- Settings page management for providers, models, upload limits, job retry, and other business configuration.
- One-command Docker Compose self-hosting path: `docker compose up -d --build` starts PostgreSQL, Redis, backend API, Dramatiq worker, and the Web static site; `just release` now uses the Compose production update and health-check flow.
- Basic open-source files, MIT License, contribution/security guides, and issue/PR templates.

## Near-Term Priorities

### 1. Developer Experience

- Add more complete local deployment screenshots and troubleshooting.
- Add a one-command seed/demo data script.
- Continue polishing Compose self-hosting troubleshooting, port conflict notes, storage migration guidance, and upgrade/rollback examples.

### 2. Testing and Quality

- Expand end-to-end test examples for product workflow DAGs.
- Add frontend component/interaction regression testing strategy.
- Add more edge tests for provider mock, OpenAI Responses provider, and failure classification.
- Add independent tests for settings-page secret updates and non-echo behavior.

### 3. Workflow Experience

- Improve DAG node run logs and failure reason display.
- Add node-level retry, skip, and duplicate capabilities.
- Optimize state refresh and partial loading for large product detail pages.
- Continue improving asset reuse between image sessions and product workflows, such as batch attach, version comparison, and clearer source labels.
- Add automated regression tests for canvas zoom, drag, connection, and image fill.

### 4. Documentation and Productization

- Add README / user-guide screenshots so ProductFlow workbench nodes, sidebar, and onboarding flow are more intuitive.
- Capture lightweight brand usage guidance, including recommended sizes and usage boundaries for logo, favicon, and README hero.
- Add provider configuration examples and common-error troubleshooting instead of expanding dependency lists.

## Mid-Term Direction

### Richer Inputs

- Multi-source product information import.
- Product URL / spreadsheet import.
- More structured brand, audience, and selling-point inputs.

### Stronger Asset Management

- Asset favorites, tags, and archiving.
- More sizes and platform adaptations.
- Configurable templates and brand color/logo.
- Clearer generated version comparison.

### Provider Expansion

- Clearer OpenAI-compatible provider configuration examples.
- Provider capability probing and health checks.
- Per-node model or provider selection.
- Interface exploration for pluggable video providers.

## Long-Term Exploration

- Video scripts, voiceover, subtitles, and template rendering.
- Multi-member collaboration and permission model.
- Object storage adapter layer.
- Deployment packages, container images, Helm chart, or other production orchestration options.
- Controlled integration with external stores/ad platforms.

## Not Planned for Now

- Built-in hosted accounts or managed model keys.
- Built-in payment/billing.
- Public registration by default.
- Fully automated placement without human confirmation.
