# ProductFlow PRD

## 1. Product Position

ProductFlow currently implements a product-visual production workspace plus User/Merchant identity and merchant ownership foundations. The user supplies real product references and delivery goals. A workflow Agent clarifies product facts, visual-system rules, and per-image prompts, then creates an editable, executable, reusable image-production workflow.

The project has no formal commercial release or commercial merchant users yet. The development mainline accepts breaking changes and may require recreating the database and storage. It maintains one schema-v3 graph, API, and execution model. The current development bootstrap still creates the only development merchant; public registration is implemented and has passed real-browser and real SMTP/IMAP mail-receipt verification. Account management, administrator product management and real payments remain incomplete; quota and stable-release recovery/upgrade mechanisms have scoped evidence.

The intended product is a self-hostable, multi-merchant SaaS, also operated by the project owner as a service using the same product. Each ordinary account owns one merchant. Workspace switching and team UX are excluded; administrators have explicit merchant-targeted product read, facts-edit and delete APIs, while complete administration pages and operation auditing remain planned; the [release roadmap](ROADMAP.en.md) owns the planned isolation, experience, competitive and deployment requirements. This PRD describes connected capabilities only.

### 1.1 Account entry

After the deployer completes `ADMIN_ACCESS_KEY` bootstrap, the site Operator configures registration SMTP in the existing `/settings`. The Register mode asks a public user for an email, verification code, password, and merchant name; successful verification creates an ordinary `User`, that user's own `Merchant`, and trial quota. The API's `display_name` field is optional and defaults to the email prefix. SMTP uses the closed field set `smtp_host`, `smtp_port`, `smtp_security` (`starttls` or `tls`), `smtp_username`, `smtp_password` (secret), `smtp_from_address`, and `smtp_from_name`.

The development stack reads startup defaults from `.env.dev` as `SMTP_HOST`, `SMTP_PORT`, `SMTP_SECURITY`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM_ADDRESS`, and `SMTP_FROM_NAME`. Values saved through Settings are database overrides; restoring a default deletes that database override and returns to the current environment default. `smtp_password` is never echoed, exported in ordinary settings, or logged.

The user entry is the Register mode on the existing `/login` page; no separate `/register` route is added.

The code is a random six-digit value valid for 10 minutes, with a 60-second resend interval and at most five failed attempts per challenge; resending invalidates the previous challenge. Existing email/password login remains. A real-browser flow has verified successful code delivery, SMTP send and IMAP receipt, registration into `/products`, an ordinary User session with its own Merchant, a 410 response for replaying the old code, and successful password login. This evidence covers the registration slice and does not establish whole-site publication or password recovery. Referrals and rebates are out of scope for this phase and remain a future decision. Accounts directly own their merchant through users.merchant_id; team roles and workspace switching are outside current scope.

## 2. Target Users

- Independent merchants who repeatedly produce ecommerce images.
- Designers who need direct control over prompts, references, aspect ratio, quality, and delivery size.
- Users who want Agent-led requirement clarification with a visual workflow editor.

## 3. Core Flows

### 3.1 Create a Product

1. The user opens `/products/new` and enters a product name.
2. Agent path: the system creates the Product, a live schema-v3 graph (name-only: a product-source node), a product-owned AgentSession, and AgentConversation. It does not create an onboarding Task or auto-submit a Turn. The user uploads one to six references in the composer and names the image types; the Agent writes intake and applies ChangeSets to the live graph. Types and files on the create form remain an optional shortcut before entering chat.
3. Direct create: upload one to six references, fill the brief (Help fill can draft it from photos; spec rows come from the returned JSON) and output settings, select image types, and write a runnable canvas immediately with no conversation. Opening the workbench Agent sidebar later attaches a canvas session.
4. The Agent checks known information and asks about missing price, style, text language, copy requirements, and visual-system decisions.
5. The Agent applies one reversible ChangeSet or proposes a multi-node ChangeSet; the user confirms multi-node proposals on the canvas.
6. Closing the conversation leaves add, connect, inspect, run, undo, and recipes available.
7. After the graph can run, the conversation sidebar can start a Goal. The Agent requests runs, waits for canvas confirmation, then edits from the result. A finished WorkflowGraphRun is not Goal complete; the user marks complete or clears it. Opening chat does not create a Goal.

### 3.2 Edit and Run a Workflow

- Users add, move, delete, and connect nodes.
- Folders organize local workflow sections.
- Prompt nodes hold the prompt strategy for one image type or related image set. A seed may publish its first generation automatically. Complete, rewrite, and replace actions on published content create a candidate for section-based review.
- Visual system and creative brief nodes use the same candidate contract. Generated output never directly overwrites authored or collaborative content.
- Reference nodes bind one image from the product library.
- Picture plans own the scene, composition, content, on-image text mode and language. Image-generation nodes hold sparse per-picture changes, optional text replacement, aspect ratio, resolution, quality, reference fidelity and background. Unchanged fields follow the plan; restoring inheritance removes the local change. Rendering does not rewrite upstream content. Delivery settings are separate and do not call the model. A reference edge is optional.
- Users run a whole DAG, run-to-node, a single node, or one selection for a shot or failed subset; they inspect runs, cancel, and retry. A busy graph queues new submits. Independent processing nodes call providers concurrently, limited by the generation concurrency setting; one failed image does not stop sibling shots. None of this requires the Agent conversation to be open; closing it matches never having opened it.
- Processing nodes expose one input port per role; missing required inputs mark the port and card red and disable that node or shot Play. Whole-graph Play stays available while at least one processing node can enqueue.
- A whole workflow, folder, or selected node group can be saved as a user recipe.
- The creation page's "From a recipe" mode accepts a saved complete recipe, a new product name, and 1 to 6 reference images. Cancelling its preview creates nothing; confirmation saves the product and first graph together, without an Agent session. Retrying the same confirmation returns the same product. The graph uses target product details; asset placeholders need new selections and never inherit source bindings or generated outputs. Owners/tests: `go/internal/product/recipe_create.go`, `recipe_create_test.go`, `web/e2e/canvas-asset-recipe.spec.ts`.
- Canvas folders are one-level visual organization only; they do not support nesting, independent run, cancel, or retry behavior.

### 3.3 Manage Images

- The product library stores uploads, workflow generations, image-session attachments, and local-edit results.
- Its directory tree exposes system groups, image types, origins, and user folders.
- Users search, sort, preview, rename, move, multi-select, download, and create delivery renditions.
- Users can remove, replace text, or inpaint an existing product image. The new asset keeps lineage; failure does not replace the node's current result. Opening from the inspector can adopt the result as that generation node's current image, or keep it in the library only.
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

- `/home`: authenticated feature navigation and real product showcase.
- `/products`: product list and automatic covers.
- `/products/new`: full-screen Agent creation flow.
- `/products/:productId`: Agent, workflow canvas, inspector, runs, recipes, and image library.
- `/image-chat`: iterative text/image generation.
- `/media-library`: global media library.
- `/settings`: provider and runtime settings.
- `/help`: in-product help; a projection of `USER_GUIDE.en.md` page operations.

The public registration page and its SMTP settings projection are part of the current-pages contract: the entry is Register mode on `/login`, shown after initialization, with code delivery and submission disabled while SMTP is unavailable.

## 6. Product Contracts

- Agent conversation can start after a product name and births a live graph. One to six media-verified references and image types are submitted in that conversation, or optionally on the create form first. Direct create still requires them on the form and writes no conversation. The Agent and user review whether product identity is sufficiently represented; the backend does not claim to prove image authenticity automatically.
- Image types start unselected. Every selected type has a quantity from one to six, defaults to two, and the total plan is limited to 30 images.
- Confirmed structured facts outrank unconfirmed user input, which outranks Agent image observations. Conflicting required facts cannot pass final Draft confirmation.
- The Agent may organize, rename, and move product-library assets and inspect selected images. It does not load the entire library into model context.
- Every reference node binds one explicit ProductImageAsset.
- The visual system is a workflow-level shared constraint. Per-image prompts may record explicit exceptions.
- GenerationSpec, provider-effective parameters, and measured output remain separate. DeliverySpec creates deterministic renditions without regenerating or replacing the source image.
- Every generated result enters the product library. Explicit delivery adoption snapshots persist selected assets; reruns do not change historical selections or automatically delete other results. Limited image checks that fail or cannot judge require explicit user confirmation before adoption. Confirmed images remain downloadable with their actual quality findings; confirmation does not mark them qualified. Invalid specifications and missing, corrupt or unauthorized assets remain blocked.
- The Agent conversation can be closed at any time. With it closed or never opened, canvas add, connect, inspect, run, undo, and recipes stay available. A failed or unknown Turn must not lock the canvas.
- Workflow reuse comes only from user-saved recipes.
- Provider purposes are `prompt`, `agent`, and `image`.

## 7. Non-Goals

- Multi-organization workspace switching, expanded team roles and real payments; existing account isolation and internal quota remain maintained.
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
- The development baseline can break compatibility. Operating-data upgrades from the first stable release follow release/README.md; retired experimental paradigms do not gain compatibility readers.
