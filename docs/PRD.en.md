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
2. Create a product, upload the product source image, and fill in product name, category, price, and other information.
3. Upload reference images for poster generation or later image sessions.
4. Trigger a copy-generation job.
5. Manually edit and confirm the copy.
6. Generate posters using the confirmed copy.
7. Download posters and review product history.

### 3.2 Iterative Image Sessions

1. Create a standalone image session, optionally attached to a product.
2. Upload multiple reference images.
3. Enter a prompt to generate images.
4. Keep iterative context so later generations can build on the previous response.
5. Attach satisfactory generated images back to a product as reference assets.

### 3.3 Product DAG Workflow

1. Open the workflow workbench in the product detail page.
2. Create or adjust nodes: product context, reference image, copy generation, and image generation.
3. Connect nodes to form a DAG.
4. Start a background workflow run.
5. Persist run state, node state, and failure reasons in the database, while the page polls and displays the latest state.

## 4. Core Objects

- `Product`: product entity, including name, category, price, and input assets.
- `SourceAsset`: product asset, including original main images, reference images, processed product images, and other types.
- `CreativeBrief`: system-generated product understanding result that provides shared semantics for copy and posters.
- `CopySet`: one copy-generation result, editable and confirmable.
- `PosterVariant`: main image / promotional poster output based on copy and assets.
- `JobRun`: traditional async job record for copy/poster generation.
- `ImageSession` / `ImageSessionAsset`: standalone iterative image-generation session and its reference/generated images.
- `ProductWorkflow` / `WorkflowNode` / `WorkflowEdge` / `WorkflowRun`: product DAG workflow structure and run records.
- `AppSetting`: runtime business configuration override.

## 5. Current Pages

Implemented frontend pages:

- `/login`: admin-key login.
- `/products`: product list.
- `/products/new`: create product.
- `/products/:productId`: product detail, copy/poster main chain, history, and DAG workflow.
- `/settings`: provider, model, upload limit, job retry, and other business configuration.
- `/image-chat` and `/products/:productId/image-chat`: iterative image generation and attaching assets back to products.

## 6. V1 Implemented Acceptance Surface

For a single self-hosted deployment, the current version should be able to:

1. Run the product chain with local `mock` providers without external model keys.
2. Store products, assets, copy, posters, jobs, image sessions, and workflow state in PostgreSQL.
3. Use Redis + Dramatiq to execute async copy/poster jobs and product workflows.
4. Display job state, workflow node state, and history in the frontend.
5. Save business configuration overrides through `/settings` while avoiding secret values in API responses.
6. Store uploaded/generated files in local storage and read them through controlled download APIs.

## 7. Explicit Boundaries

Currently not included:

- Multi-user, multi-tenant, team permissions, or audit admin.
- Hosted model keys, cloud account systems, or billing systems.
- Automatic placement, automatic listing, or store authorization.
- Video generation workflows.
- Production Kubernetes / full Docker Compose orchestration.

## 8. Success Criteria

- External developers can start the complete development stack locally by following the README.
- The default mock configuration does not require real API keys.
- Documentation does not exaggerate current capabilities for copy, posters, image sessions, or workflows, and does not hide key dependencies.
- Private environment files, runtime data, Trellis task history, and build outputs are not publicly committed.
