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
- Generated image gallery: iterative image results can be collected at `/gallery`, keeping source session, prompt, size, model, and download entrypoint.
- Product DAG workflow editing, execution, persistent state, and recovery.
- Shared top navigation.
- ProductFlow workbench canvas interactions: desktop mouse-wheel zoom, left-drag pan, node drag positioning, box selection / multi-select, and edge drag creation/deletion; mobile Browse, Edit, and Select modes, touch drag/edge creation, and two-finger pinch zoom.
- Full scenario templates for product creation: blank canvas, marketplace hero images, detail persuasion, scene galleries, content covers, and campaign assets.
- Workbench templates: the same built-in scenario templates can be inserted into existing canvases and automatically reuse the product node; users can save selected nodes as their own node-group templates with rename and archive-delete support.
- Single-slot semantics for reference images, image drag-and-drop upload, compact right sidebar for Details / Runs / Library / Templates, and asset fill.
- In-product help page: `/help` covers quick start, canvas operations, templates, run failure handling, supported operations, and common questions.
- Prompt configuration: product understanding, copy, workbench image generation, and iterative image-generation templates can be overridden in the settings page.
- Initial product brand assets, README preview images, and Web favicon/metadata.
- Settings page management for providers, models, upload limits, job retry, and other business configuration.
- Lightweight status polling while running: iterative image generation and product workflows poll status responses only, then refresh full details after completion.
- Mobile product list and product workbench adaptation: product list cards with floating pagination, plus workbench bottom toolbar, bottom sheet, and canvas touch modes.
- Mobile iterative image page adaptation: main view, session drawer, narrow history drawer, generation-settings bottom sheet, and bottom quick actions are organized for small screens.
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
- Add more edge tests for provider mock, OpenAI Responses provider, failure classification, and manual retry/cancel behavior.
- Add independent tests for settings-page secret updates and non-echo behavior.

### 3. Workflow Experience

- Continue improving DAG node run logs and failure reason display; categorized failure messages and workflow retry/cancel actions already exist.
- Add node-level skip and duplicate capabilities; workflow-level retry/cancel already exists.
- Continue optimizing partial loading and component boundaries on large product detail pages; active full-workflow polling has already been replaced with lightweight status polling.
- Continue improving batch selection, version comparison, and source labels after iterative image results are attached back to products.
- Add more frontend regression coverage for the template panel, user-template saving, and key workbench component interactions; core canvas selection/drag helpers already have unit coverage.

### 4. Documentation and Productization

- Add README / user-guide screenshots so ProductFlow workbench nodes, template panel, and sidebar are more intuitive.
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
- Released container images, Helm chart, or other production orchestration packages. The repository already includes the Docker Compose self-hosting path, but no hosted image or Helm chart is published.
- Controlled integration with external stores/ad platforms.

## Not Planned for Now

- Built-in hosted accounts or managed model keys.
- Built-in payment/billing.
- Public registration by default.
- Fully automated placement without human confirmation.
