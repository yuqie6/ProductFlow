# ProductFlow PRD

## 1. Product Position

ProductFlow is a product-visual production workspace for a single merchant. The user supplies real product references and delivery goals. A workflow Agent clarifies product facts, visual-system rules, and per-image prompts, then creates an editable, executable, reusable image-production workflow.

The current release serves a personal project and live demo, but upgrades for deployed instances cannot rely on resetting data. The migration window preserves bounded immutable history snapshots, canonical asset mappings, and Agent rebuild entry points; the online release still maintains one V2 schema, API, and execution model. SaaS tenancy, billing, and long-term compatibility policy are outside this release.

## 2. Target Users

- Independent merchants who repeatedly produce ecommerce images.
- Designers who need direct control over prompts, references, aspect ratio, quality, and delivery size.
- Users who want Agent-led requirement clarification with a visual workflow editor.

## 3. Core Flows

### 3.1 Create a Product

1. The user opens `/products/new` and enters a product name.
2. The user selects image types. Each type defaults to quantity two and can be adjusted independently.
3. The user uploads one to six real product reference images.
4. The system creates a Product, WorkflowDraft, and AgentConversation.
5. The Agent checks known information and asks about missing price, style, text language, copy requirements, and visual-system decisions.
6. The Agent produces product facts, a visual system, image plans, per-image prompts, reference bindings, and generation specifications.
7. The user reviews and confirms the Draft.
8. The system materializes a schema-v2 workflow and streams folder, node, and edge reveal events.
9. The creation screen transitions into the product workbench while the same Agent conversation continues in the sidebar.

### 3.2 Edit and Run a Workflow

- Users add, move, delete, and connect nodes.
- Folders organize local workflow sections.
- Prompt nodes hold the prompt strategy for one image type or related image set.
- Reference nodes bind one image from the product library.
- Image-generation nodes hold aspect ratio, resolution, quality, reference fidelity, background, and text policy.
- Users run a whole DAG or one node, inspect runs, cancel, and retry.
- A whole workflow, folder, or selected node group can be saved as a user recipe.

### 3.3 Manage Images

- The product library stores uploads, workflow generations, and image-session attachments.
- Its directory tree exposes system groups, image types, origins, and user folders.
- Users search, sort, preview, rename, move, multi-select, download, and create delivery renditions.
- Node bindings and library entries share the same ProductImageAsset identity.
- Product cover selection is automatic and serves list presentation.

### 3.4 Iterative Image Generation

- A user creates an image session and selects a branch base plus up to six context references.
- Each round has candidate count, size, and advanced image parameters.
- Jobs expose queue state, progress, cancel, failure retry, and candidate branching.
- A satisfactory result can be downloaded, collected in Gallery, or saved to a product library.

## 4. Core Objects

- `Product`: product identity and basic information.
- `MediaObject`: media bytes, MIME type, dimensions, verification state, and storage path.
- `ProductImageAsset`: product-scoped image identity, origin, directory, and derivation.
- `WorkflowDraft` / `WorkflowDraftRevision`: confirmable Agent workflow proposal.
- `ProductWorkflow`: the current schema-v2 DAG.
- `WorkflowNode` / `WorkflowEdge` / `WorkflowFolder`: canvas structure.
- `WorkflowRun` / `WorkflowNodeRun`: execution state and results.
- `WorkflowRecipe` / `WorkflowRecipeVersion`: user-saved full recipes and fragments.
- `AgentConversation` / `AgentTurnProjection`: ProductFlow-side Agent conversation and Turn projection.
- `ImageSession`: independent iterative image session.
- `DeliveryRenditionJob`: asynchronous delivery-format rendering.
- `ProviderProfile` / `ProviderBinding`: provider profile and purpose binding.

## 5. Current Pages

- `/products`: product list and automatic covers.
- `/products/new`: full-screen Agent creation flow.
- `/products/:productId`: Agent, V2 canvas, inspector, runs, recipes, and image library.
- `/image-chat`: iterative text/image generation.
- `/gallery`: collected images.
- `/settings`: provider and runtime settings.
- `/help`: current in-product help.

## 6. Product Contracts

- Real product references are required before the Agent creation flow begins.
- Every selected image type has an explicit quantity with a default of two.
- The Agent may organize, rename, and move product-library assets and inspect selected images. It does not load the entire library into model context.
- Every reference node binds one explicit ProductImageAsset.
- The visual system is a workflow-level shared constraint. Per-image prompts may record explicit exceptions.
- Every generated result enters the product library. There is no rejected-draft or delivery-manifest state.
- Workflow reuse comes only from user-saved recipes.
- Provider purposes are `prompt`, `agent`, and `image`.

## 7. Non-Goals

- Multi-tenancy, team roles, billing, and usage settlement.
- Automatic publishing to ecommerce or ad platforms.
- Automatically classifying and deleting images the user dislikes.
- Loading an entire product library into Agent context.
- Long-term runtime readers for retired V1 database models; migration-window archive snapshots remain a bounded upgrade capability.

## 8. Success Criteria

- A user can move from references and image-type selection through Agent clarification, confirmation, and workflow materialization.
- A user can keep editing nodes, edges, folders, prompts, reference bindings, and generation specifications manually.
- Uploads, workflow results, and image-session attachments are manageable in one product image library.
- Provider configuration, Agent Turns, workflow runs, and image jobs have explicit failure and restart state.
- Current code and documentation describe one online workflow contract.
