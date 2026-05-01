# ProductFlow PRD

[中文](PRD.md) | English

## 1. Product Positioning

ProductFlow is an open-source, self-hosted product creative workspace for solo merchants, small operations teams, and developers who want to manage AI creative workflows on their own infrastructure.

It is not a hosted SaaS product, not a multi-tenant open platform, and does not promise to replace human operational judgment. The current core goal is:

> Move one product from input assets to editable copy, downloadable posters, reusable image assets, and traceable workflow state.

## 2. Target Users

- Merchants who need to quickly create product titles, selling points, main images, and promotional posters.
- Teams that want to self-host model keys, databases, and asset files.
- Developers who want to extend AI ecommerce creative workflows.

Non-target users: teams that need multi-tenant isolation, complex RBAC, payment settlement, asset placement platforms, or hosted account systems.

## 3. Current Core Scenarios

### 3.1 Product Creative Chain

1. Log in with an admin key.
2. Create a product, upload the product source image, and fill in the product name.
3. Enter the product workbench and add category, price, product notes, and generation direction.
4. Use copy nodes to generate and edit the product title, selling points, poster headline, and CTA.
5. Use image-generation nodes to generate images and fill downstream reference-image slots.
6. Download images/posters, or review product asset history in the right-side Images panel.

### 3.2 Iterative Image Sessions

1. Create a standalone image session, optionally attached to a product.
2. Upload multiple reference images.
3. Enter a prompt to generate images.
4. Continue from any generated candidate, or explicitly select reference images for the next generation round.
5. View queued/running/failed state; after task completion, the page refreshes new candidates automatically.
6. Attach satisfactory generated images back to a product as reference assets, save them as product main-image references, or collect them in the gallery.
7. On mobile, use the single-column layout: top actions, status, result preview, generation settings, reference images, and parameter selection.

### 3.3 Product DAG Workflow

1. Open the workflow workbench in the product detail page.
2. Create or adjust nodes: product context, reference image, copy generation, and image generation.
3. Connect nodes to form a DAG.
4. Start a background workflow run.
5. Persist run state, node state, and failure reasons in the database.
6. While running, the frontend polls lightweight workflow status; after completion, it refreshes full workflow, product detail, and historical artifacts.

### 3.4 Gallery

1. Save generated image-session results to the gallery.
2. Browse collected generated images at `/gallery` by generation time.
3. Gallery entries keep source session, linked product, prompt, size, model, and download entrypoint.

## 4. Core Objects

- `Product`: product entity, including name, category, price, and input assets.
- `SourceAsset`: product asset, including original main images, reference images, processed product images, and other types.
- `CreativeBrief`: system-generated product understanding result that provides shared semantics for copy and posters.
- `CopySet`: one copy-generation result, editable and confirmable.
- `PosterVariant`: main image / promotional poster output based on copy and assets.
- `ImageSession` / `ImageSessionAsset`: standalone iterative image-generation session and its reference/generated images.
- `ImageSessionRound` / `ImageSessionGenerationTask`: iterative image candidates and durable async generation-task state.
- `ImageGalleryEntry`: saved generated-image collection record.
- `ProductWorkflow` / `WorkflowNode` / `WorkflowEdge` / `WorkflowRun`: product DAG workflow structure and run records.
- `AppSetting`: runtime business configuration override.

## 5. Current Pages

Implemented frontend pages:

- `/login`: admin-key login.
- `/products`: product list.
- `/products/new`: create product.
- `/products/:productId`: product detail, copy/poster main chain, history, and DAG workflow.
- `/gallery`: generated image gallery.
- `/settings`: provider, model, upload limit, job retry, and other business configuration.
- `/image-chat` and `/products/:productId/image-chat`: iterative image generation and attaching assets back to products.

## 6. V1 Implemented Acceptance Surface

For a single self-hosted deployment, the current version should be able to:

1. Run the product chain with local `mock` providers without external model keys.
2. Store products, assets, copy, posters, tasks, image sessions, and workflow state in PostgreSQL.
3. Use Redis + Dramatiq to execute async copy/poster jobs and product workflows.
4. Use durable `ImageSessionGenerationTask` records for iterative image generation, including queue position, failure reason, and completion refresh.
5. Display task state, workflow node state, generation queue overview, and history in the frontend.
6. Refresh running tasks/workflows through lightweight status APIs instead of high-frequency full-object polling.
7. Save iterative generated images to the gallery and retrieve originals through controlled download APIs.
8. Save business configuration overrides through `/settings` while avoiding secret values in API responses.
9. Store uploaded/generated files in local storage and read them through controlled download APIs.

## 7. Explicit Boundaries

Currently not included:

- Multi-user, multi-tenant, team permissions, or audit admin.
- Hosted model keys, cloud account systems, or billing systems.
- Automatic placement, automatic listing, or store authorization.
- Video generation workflows.
- Kubernetes / Helm / released container images or other production orchestration packages. The repository already includes a Docker Compose self-hosting path.

## 8. Success Criteria

- External developers can start the complete development stack locally by following the README.
- The default mock configuration does not require real API keys.
- Documentation does not exaggerate current capabilities for copy, posters, image sessions, or workflows, and does not hide key dependencies.
- Private environment files, runtime data, Trellis task history, and build outputs are not publicly committed.
