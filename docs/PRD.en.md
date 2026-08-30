# ProductFlow PRD

## 1. Product Position

ProductFlow is a product-visual production workspace for a single merchant. The user supplies real product references and delivery goals. A workflow Agent clarifies product facts, visual-system rules, and per-image prompts, then creates an editable, executable, reusable image-production workflow.

The current release serves a personal project and live demo. The main repository is in rapid development and accepts breaking changes. Deployments that need a stable snapshot should fork. Following this repository may require recreating the database and storage. The online release maintains one schema-v3 graph, API, and execution model. SaaS tenancy, billing, and long-term compatibility policy are outside this release.

## 2. Target Users

- Independent merchants who repeatedly produce ecommerce images.
- Designers who need direct control over prompts, references, aspect ratio, quality, and delivery size.
- Users who want Agent-led requirement clarification with a visual workflow editor.

## 3. Core Flows

### 3.1 Create a Product

1. The user opens `/products/new` and enters a product name.
2. Agent path: the system creates the Product, a live schema-v3 graph (name-only: a product-source node), a product-owned AgentSession, and AgentConversation. It does not create an onboarding Task or auto-submit a Turn. The user uploads one to six references in the composer and names the image types; the Agent writes intake and applies ChangeSets to the live graph. Types and files on the create form remain an optional shortcut before entering chat.
3. Direct create: select image types, upload one to six references, fill the brief and output settings on the form, and write a runnable canvas immediately with no conversation. Opening the workbench Agent sidebar later attaches a canvas session.
4. The Agent checks known information and asks about missing price, style, text language, copy requirements, and visual-system decisions.
5. The Agent applies one reversible ChangeSet or proposes a multi-node ChangeSet; the user confirms multi-node proposals on the canvas.
6. Closing the conversation leaves add, connect, inspect, run, undo, and recipes available.
7. After the graph can run, the conversation sidebar can start a Goal. The Agent requests runs, waits for canvas confirmation, then edits from the result. A finished WorkflowGraphRun is not Goal complete; the user marks complete or clears it. Opening chat does not create a Goal.

### 3.2 Edit and Run a Workflow

- Users add, move, delete, and connect nodes.
- Folders organize local workflow sections.
- Prompt nodes hold the prompt strategy for one image type or related image set. Seed documents generate and adopt through a ChangeSet; authored composition is not overwritten on fill.
- Visual system and creative brief nodes distinguish seed from authored; fill only completes seed documents and does not trigger image generation.
- Reference nodes bind one image from the product library.
- Image-generation nodes hold aspect ratio, resolution, quality, reference fidelity, background, and text policy; running them renders from the live prompt document and does not rewrite authored upstream content. A reference edge is optional.
- Users run a whole DAG or one node, inspect runs, cancel, and retry. Independent processing nodes call providers concurrently, limited by the generation concurrency setting; one failed image does not stop sibling shots. None of this requires the Agent conversation to be open; closing it matches never having opened it.
- A whole workflow, folder, or selected node group can be saved as a user recipe.
- Canvas folders are one-level visual organization only; they do not support nesting, independent run, cancel, or retry behavior.

### 3.3 Manage Images

- The product library stores uploads, workflow generations, and image-session attachments.
- Its directory tree exposes system groups, image types, origins, and user folders.
- Users search, sort, preview, rename, move, multi-select, download, and create delivery renditions.
- User folders are one level deep. Deleting a folder removes organization only and does not delete images or break node, cover, or lineage references.
- Node bindings and library entries share the same ProductImageAsset identity.
- Product cover selection is automatic and serves list presentation.

### 3.4 Iterative Image Generation

- A user creates an image session and selects a branch base plus up to six context references.
- Each round has candidate count, size, and advanced image parameters.
- Jobs expose queue state, progress, cancel, failure retry, and candidate branching.
- A satisfactory result can be downloaded, saved to the global media library, or saved to a product library.

### 3.5 Global Media Library

- `/media-library` is the long-lived cross-product media entry with search, folders, tags, archive/restore, upload, and batch organization.
- Iterative-image candidates can be saved into the global media library.
- Workflow sub-libraries store usage associations to global assets. One media object can be used by multiple workflows without copying its bytes.
- The Agent may publish a confirmable library-organization Draft. Rename, move, tag, and archive apply only after user confirmation.

### 3.6 Global Agent Dock

- After login, the application shell exposes a Global Agent Dock for Session/Task lists, search, create, archive, workspace jumps, task cancellation, and global library-organization Draft confirmation.
- Product-workflow editing, run, cancel, and retry stay on the product workbench. The Dock does not own the canvas or WorkflowRun.

## 4. Core Objects

- `Product`: product identity and basic information.
- `MediaObject`: media bytes, MIME type, dimensions, verification state, and storage path.
- `ProductImageAsset`: product-scoped image identity, origin, directory, and derivation.
- `MediaLibraryAsset`: global-library image identity, provenance snapshot, organization, and archive state.
- `WorkflowMediaLibraryAsset`: a usage association from a global asset to one workflow sub-library.
- Product intake: image types, quantities, and reference asset ids on the Product. The product path no longer inserts `WorkflowDraft`.
- `WorkflowGraph`: the current schema-v3 DAG.
- `WorkflowGraphNode` / `WorkflowGraphEdge` / `WorkflowGraphGroup`: canvas structure.
- `WorkflowGraphRun` / `WorkflowGraphNodeRun` / `WorkflowGraphArtifact`: execution state and artifacts.
- `WorkflowRecipe` / `WorkflowRecipeVersion`: user-saved full recipes and fragments.
- `AgentSession`: long-lived conversation container, title, summary, and task index.
- `AgentTask`: one business goal and one task-specific run. On the product path this is an explicit Goal: a finished graph run is not complete.
- `AgentConversation` / `AgentTurnProjection`: ProductFlow-side Agent conversation and Turn projection.
- `ImageSession`: independent iterative image session.
- `DeliveryRenditionJob`: asynchronous delivery-format rendering.
- `ProviderProfile` / `ProviderBinding`: provider profile and purpose binding.

## 5. Current Pages

- `/products`: product list and automatic covers.
- `/products/new`: full-screen Agent creation flow.
- `/products/:productId`: Agent, workflow canvas, inspector, runs, recipes, and image library.
- `/image-chat`: iterative text/image generation.
- `/media-library`: global media library.
- `/settings`: provider and runtime settings.
- `/help`: in-product help; a projection of `USER_GUIDE.en.md` page operations.

## 6. Product Contracts

- Agent conversation can start after a product name and births a live graph. One to six media-verified references and image types are submitted in that conversation, or optionally on the create form first. Direct create still requires them on the form and writes no conversation. The Agent and user review whether product identity is sufficiently represented; the backend does not claim to prove image authenticity automatically.
- Image types start unselected. Every selected type has a quantity from one to six, defaults to two, and the total plan is limited to 30 images.
- Confirmed structured facts outrank unconfirmed user input, which outranks Agent image observations. Conflicting required facts cannot pass final Draft confirmation.
- The Agent may organize, rename, and move product-library assets and inspect selected images. It does not load the entire library into model context.
- Every reference node binds one explicit ProductImageAsset.
- The visual system is a workflow-level shared constraint. Per-image prompts may record explicit exceptions.
- GenerationSpec, provider-effective parameters, and measured output remain separate. DeliverySpec creates deterministic renditions without regenerating or replacing the source image.
- Every generated result enters the product library. There is no rejected-draft or delivery-manifest state.
- The Agent conversation can be closed at any time. With it closed or never opened, canvas add, connect, inspect, run, undo, and recipes stay available. A failed or unknown Turn must not lock the canvas.
- Workflow reuse comes only from user-saved recipes.
- Provider purposes are `prompt`, `agent`, and `image`.

## 7. Non-Goals

- Multi-tenancy, team roles, billing, and usage settlement.
- Automatic publishing to ecommerce or ad platforms.
- Automatically classifying and deleting images the user dislikes.
- Loading an entire product library into Agent context.
- Long-term runtime readers for retired models, dual serializers, 409 compatibility stubs, or backfill/freeze/archive gates for deployed data.

## 8. Success Criteria

- A user can send photos and image requirements in the Agent conversation, confirm a Draft, and persist the graph. Direct create still starts from the create form.
- A user can keep editing nodes, edges, folders, prompts, reference bindings, and generation specifications manually.
- Uploads, workflow results, and image-session attachments are manageable in one product image library. Cross-product long-lived media lives in `/media-library`.
- Provider configuration, Agent Turns, workflow runs, and image jobs have explicit failure and restart state.
- Current code and documentation describe one online workflow contract.
- The main repository does not promise lossless upgrades for deployed instances. Deployments that need a stable snapshot should fork.
