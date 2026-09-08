# ProductFlow User Guide

This document is the canonical user-operations source. The in-product `/help` pages (`web/src/pages/HelpPage.tsx`) are a projection of the same operations and must change in the same commit. This document is optimized for repository search and troubleshooting.

## 1. First Use

1. In the current development baseline, use `ADMIN_ACCESS_KEY` on an empty site to initialize an account and development merchant, setting an email and password. Subsequent logins use that email and password.
2. After signing in, an Operator opens `/settings`; ordinary Users and anonymous requests are denied.
3. Create provider profiles with Base URL, API key, capabilities, and default models.
4. Bind profiles, interfaces, and models to `prompt`, `agent`, and `image`.
5. Save and return to `/products`.

Login and setup show a countdown when attempts are rate limited. Submission becomes available again after the cooldown, and form inputs are retained. A temporary service failure remains retryable and does not indicate a successful login.

### 1.1 Public registration (shown after initialization)

1. After deployer bootstrap, a signed-in Operator opens `/settings` and fills in registration SMTP: host, port, `starttls` or `tls`, username, password, sender address, and sender name. Ordinary Users and anonymous requests are denied, and the password is not echoed after save.
2. After initialization, switch to the Register mode on the login page, enter an email, and request a verification code. The form remains visible when SMTP is not ready, while sending the code and submitting registration are disabled with an email-service notice.
3. Enter the six-digit code, set a password of at least eight characters, and provide a merchant name before submitting.
4. After verification, the system creates an ordinary account, that account's own merchant, and trial quota, then signs in through the existing session. Email/password login remains available.

A code is valid for 10 minutes, resends are limited to one per 60 seconds, and each challenge permits at most five failed attempts; resending invalidates the previous challenge. Each ordinary account owns one merchant; membership management and merchant switching are not provided.

All three purposes are required by the core flow:

- Prompt: visual system, creative brief, prompt-generation nodes, and create-page source-note drafting from photos.
- Agent: requirement clarification, library organization, and workflow creation.
- Image: workflow and iterative image generation.

### 1.2 Personal account and password recovery

Open `/account` from the navigation to update your display name. Email is read-only. Ordinary accounts show their own merchant; an independent Operator can have no merchant. Active sessions are paginated by sign-in time and can be revoked individually. Revoking the current session signs you out. Failed revocation or logout remains visible and retryable.

Changing a password requires the current password. The new password needs at least eight characters and at most 72 bytes. Success invalidates all existing sessions and outstanding recovery codes, requiring a new sign-in.

Choose Reset password on the login page to open `/password-recovery`. Request a six-digit code through the account email, then enter it with a new password. An accepted request does not disclose account existence or promise mail delivery; check the inbox and spam folder and use the resend countdown. Recovery uses the existing SMTP configuration, a ten-minute validity window, a 60-second send interval and at most five failed attempts. Success consumes the code and revokes every existing session. A new send replaces the old code; a request during the send interval retains the code and refreshes its validity window. Unconfigured email service is reported as unavailable. No additional administrator unlock is required.

Language and theme controls in the account page and navigation save to the same account preferences. Changes apply after a successful save; failed saves retain the previous setting and offer retry. Login restores the account settings, while logout restores anonymous browser settings. Another account does not inherit these choices. Interface language does not change product output language.

Edit the owned merchant name under Merchant profile and save it; names contain 1–160 characters. Operators without a merchant do not see this form. A suspended merchant cannot be renamed, but personal profile, preferences, password changes and session revocation remain available.

### 1.3 Administrator product management

Open `/ops` to search merchants by name and status. Open a merchant to view its products, task states, quota ledger and administrator action records. Every product management page identifies the owning merchant. Image preview and download use the same explicit authorization; administrators can work without a merchant of their own.

Save product facts against the displayed version. A conflict retains your draft so you can reload and compare. Editing facts does not run generation or adopt new graph results. Product deletion requires confirmation and remains subject to the site deletion setting and current business restrictions. Action records retain the deleted product name. An unknown audit outcome must not be treated as a confirmed failure.

Quota values are internal units. Adjustments require a signed quantity and a reason; retrying an uncertain submission checks the same operation. Read the resulting balance and ledger record to confirm it. Task records are read-only; merchant suspension controls and manual unknown-consumption resolution are not provided on this page.

## 2. Create a Product

Open `/products/new`.

### 2.1 Product details and image plan

- Enter the product name. Upload reference photos first, then write the product brief. Help fill drafts the brief from the photos; returned specs become an editable table, with blank cells for facts the photos do not show. Name-only Start conversation does not require generating first.
- Select the required image types. Types are grouped as photography, infographic, and evidence.
- Manual checks default to two images per type. Apply recommended set fills the listing minimum: cover 2, selling-point 4 (one hook per frame), specifications 1, SKU 1, scene 1, detail 1, without overwriting quantities already chosen.
- Multiple images of one type share one prompt; each image node can carry a variation instruction (selling-point pages use it to split hooks).
- Certification and factory are evidence types: one unbound asset placeholder, not generated.
- Each generating type has its own aspect ratio, editable in output settings. Cover defaults to 3:4, detail and SKU to 1:1, scene to 4:3.

### 2.2 Upload Real References

- Upload one to six images of the actual product.
- PNG, JPEG, and WebP are supported.
- Images should expose product shape, structure, color, and important details.
- Logos, certifications, packaging, factory material, and manuals can only be used when the user supplies them.

References are product evidence and candidates for reference-node bindings. Product cover selection is automatic; no separate cover prop is required.

### 2.3 Talk with the Agent

After entering a product name, the workbench opens with a live canvas (name-only starts with a product-source node). The conversation stays empty until your first message; the system does not auto-send a read-the-product prompt. Upload one to six reference photos in the composer and say which images you need. The Agent writes that as product intake and edits the canvas.

Image types and references on the create page still feed direct create, and remain an optional shortcut before you click Start conversation. Direct create does not open a conversation; open the sidebar when you want one. After you are in chat, keep sending photos and text there.

Once the graph can run, the conversation sidebar can start a Goal. The Agent uses existing tools to request a run, wait for your canvas confirmation, then inspect and edit. A finished WorkflowGraphRun is not Goal complete; you mark complete or clear it. Opening chat does not create a Goal.

Common follow-up questions cover:

- Price, category, specifications, and selling points.
- Visual style, colors, typography, photography, and realism.
- Image-text language, copy density, and forbidden content.
- Aspect ratio, quality, reference fidelity, and background.
- Whether multiple images of one type are candidates or distinct angles/content.

Answer in the same-turn composer: pick an option, type, or skip. The timeline does not add a second user bubble.

### 2.4 Confirm canvas proposals

Multi-node edits preview on the canvas and write into the live graph on confirm. Closing the conversation does not block add, connect, run, or undo.

Graph confirmation uses canvas proposals. The product path no longer confirms a WorkflowDraft.

### 2.5 Create the Canvas Directly

On the create page, enter a product brief plus on-image copy and language, then set an aspect ratio for each image type. Skip the Agent to write a runnable workflow immediately. The brief is written into the creative-brief node. After uploading references, Help fill can draft the brief from the photos and open a spec table from the returned JSON. The create-page text mode and language are stored on each picture plan. They can later be changed per plan or overridden for one image. Aspect ratio is written per type, such as 3:4 for cover and 1:1 for detail. After you upload references and choose image types, the graph includes product facts, identity references, visual system, creative brief, and one group per photography/infographic shot (one prompt plus N images; variation instructions when quantity is greater than one). Evidence types are unbound placeholders. Identity photos connect to visual, brief, and generating shots, not to evidence placeholders. You can talk to the Agent as soon as the workbench opens; the photos are already on the product, so you do not resubmit create-page image requirements. If something is missing, the Agent asks in chat. Edit nodes and edges on the canvas, or use Add a shot. The Agent can explain the graph, check configuration, and request a run; it cannot submit a Draft that replaces the live graph. Open the workbench and run the visual system, creative brief, and prompt nodes first so the model writes into the inspector; then run image nodes, a shot, or the whole graph.

## 3. Product Workbench

In Image Results, use Preview image on a result card to open the existing image viewer and download the image; clicking the card still selects it. Delivery package export uses the adopted version. When a current image changes, its source node disappears, the adopted version belongs to a different graph, or an adopted image cannot be linked to a current slot, the results view explains the mismatch. You can still export the previously adopted version. Explicitly adopt a new image to include it in the delivery package. Moving nodes or changing the canvas layout does not change the adopted version.

### 3.1 Node Types

- Product facts: confirmed facts, with pending observations separated for explicit confirmation. Pending or conflicting facts are not generation evidence.
- Reference image: one product-library image, its role and note. Product identity, environment, style and evidence have distinct uses.
- Creative brief: the goal, key messages, required elements, prohibitions and missing facts. It does not own finished on-image copy.
- Series style: shared style and a palette with editable swatches, roles and labels. Restrictions stay in the creative brief.
- Picture plan: objective, scene, composition, content and text, with advanced detail collapsed. This node owns text mode and language. Running it writes a candidate or initial document, not an image.
- Image generation: current result, per-picture changes, optional text replacement, generation settings and separate delivery settings. Restore inheritance to follow the plan's latest value.

### 3.2 Canvas Operations

Closing the Agent conversation leaves the same canvas as never opening it: add, connect, inspect, run, undo, and recipes stay available. A failed or unknown Turn does not lock the canvas. Nodes the Agent just changed can be edited and undone immediately.

- The add panel can add a shot: one group, one prompt, and one image node, connected to existing product facts, visual system, and creative brief. Selected identity references are connected too.
- Running a shot submits one run for the group's image nodes. Independent images in the group run in parallel, using the current prompt document, and do not rewrite authored content nodes.
- Adding a single node from the add panel places it near the current viewport center and selects it.
- Drag from an output handle to a matching input port to create an edge. Processing nodes expose one input port per Catalog role. Compatible ports highlight; incompatible ports do not snap. Existing edges can be reconnected by dragging an endpoint. Missing required inputs turn the port and card red and disable that node or shot Run. Whole-graph Run stays available while at least one processing node can enqueue.
- Hovering whole-graph, node, run-to-node, or shot Run colors nodes that will generate, reuse, freeze, or block for that submit scope; a click submits immediately. A new request while a run is active joins the queue; the top-right chip shows running and queued counts.
- If the product has no workflow yet, blank-canvas create writes an empty graph, then you can add all six node types.
- With two or more nodes selected, the node toolbar can duplicate, group, save as recipe, and delete.
- Drag nodes to position them; zoom with wheel/touch and pan from blank canvas.
- Ctrl/Cmd/Shift-click or marquee-select multiple nodes.
- Copy then paste selects the new nodes and keeps edges inside the selection.
- Deleting nodes asks for confirmation; deleting an edge can be undone immediately.
- Ctrl/Cmd+Z undoes the last graph edit; Ctrl/Cmd+Shift+Z redoes it unless a newer edit landed.
- Node cards keep type color; running uses a glow; run failures appear on the card and in the open inspector. Missing edges are not shown as failure reasons.
- The node toolbar can run, run up to here, duplicate, focus, save as recipe, and delete; image assets also have bind.
- Maximize hides the top navigation so the canvas fills the main area.
- Use automatic layout to improve routing.
- Put a local flow in a canvas group. Double-click the group or use the enter control to see only its members; the breadcrumb returns to the full graph. In-group and full-graph viewports are remembered separately. A group changes visual organization only, not DAG execution order. Cross-group edges stay visible on the full graph.
- While a graph save is in progress, add, inspect, bind, and recipes lock so unsaved prompts are not eaten by a run.
- Dropping an asset onto blank canvas creates a bound unused image node; dropping onto a reference port creates or reuses a node, then connects a reference edge.

Node cards show compact scanning summaries. Edit complete content in the inspector so cards remain readable.

### 3.3 Inspector

With nothing selected, Details offers add-node, open-library, and run-graph. With a node selected, Details and Runs explain actual inputs with source titles and edge roles on the first screen; they do not put internal ids on that screen. Missing required inputs are explained in the inspector; missing edges are not written into the card failure slot. A selected edge keeps a visible delete control. On a narrow screen the inspector is a bottom drawer and a strip of canvas nodes stays visible.

With a node selected:

Reference nodes:

- Select one product-library asset.
- Preview the current binding.
- Upload or save an image-session result to the product before binding a new reference.

Creative brief nodes:

- A template seed can run to generate the goal, design goals, and prohibitions from photos and product facts.
- The first generation of a template seed may publish automatically. Complete, rewrite, and replace actions on an existing document create a reviewable suggestion that can be applied by business section.

Visual system nodes:

- An empty overlay seed can run to generate style keywords and background from photos and product facts.
- A published overlay is not overwritten by normal runs. New AI output becomes a reviewable suggestion; the published overlay remains the rendering input.

Prompt nodes:

- A seed can run to write a prompt into the inspector. This does not render images.
- Edit image goal, composition, content, text, and atmosphere. A hand-filled composition can feed downstream image nodes immediately.
- Authored documents are not overwritten by normal runs. Complete, rewrite, and replace produce Inspector suggestions that can be applied by section, applied in full, or discarded.

Image nodes:

- Choose aspect ratio and resolution tier.
- Set quality intent, reference fidelity and background separately from delivery dimensions and format.
- Text mode and language belong to the picture plan. Newly added photography plans default to no added text; infographic plans default to text required. Either can be changed. An image can replace the whole text block locally or restore inheritance. No-text mode retains authored text in the plan but omits it from rendering; existing printed product labels are preserved.
- Local scene, composition and atmosphere edits save only changed fields. Explicitly clearing a field is different from restoring inheritance. A local change does not alter the plan or sibling images, and cannot remove connected brief requirements.
- A reference edge is optional. Text-only generation is allowed; an unbound reference edge fails and points at that asset node. Direct create wires uploads to visual, brief, prompt, and image nodes.
- Running this node only renders an image from the live prompt document. A prompt artifact is not required. A seed prompt with only the template design goal can still render; the inspector warns that you may want to generate or fill composition first.
- Inspect output both on the node and in the library.
- When the node has a current image, the inspector can open local edit: mark a region and choose remove, replace text, or inpaint. The new image enters the library and keeps lineage; failure does not replace the node's current result. Adopt it as the node's current image, or keep it in the library only. The library menu can also open local edit; those results stay in the library unless opened from the inspector. If the current image provider does not support local edit, the entry explains why.

### 3.4 Runs

- Run this node executes only the selected processing node. Visual, brief, and prompt write content when they are seed; authored documents skip. Image generation renders.
- Run to this node processes ancestors that still need generation (seed content and missing or stale images), then the selected node. Published document nodes are not queued.
- Run the whole graph considers every processing node with its required inputs in DAG order. Seed content and stale images generate; authored documents and unchanged images are recorded as skipped with frozen or reused status. Nodes missing required inputs are excluded, and a graph with no enqueueable node returns a validation error.
- When a published document needs another model pass, the Inspector offers complete, rewrite, and replace. The result is reviewed against the current document before an undoable canvas edit is applied.
- Card status describes operational availability, such as data available, document available, can generate, or review required. Queued, running, and failed are temporary execution states; prior execution results remain in Runs.
- The Runs panel shows state, node results, and failure reasons. Compiler keys stay out of the first screen.
- Active runs can be cancelled. Retry is available for retryable failures.

## 4. Visual System and Prompts

The visual system is shared across the workflow and can define:

- Style position.
- Primary, secondary, accent, and background colors.
- Typography and size hierarchy.
- Decoration, icon language, and whitespace.
- Lighting, depth of field, lens, and composition.
- Resolution, commercial quality, realism, and product-form lock.

Images of one type may share a goal and base composition. Images with distinct angles, content, or text should have separate prompts. Record explicit per-image exceptions so visual consistency remains understandable.

## 5. Product Image Library

The workbench Library panel uses an Explorer-style structure.

### 5.1 Browse and Classify

- System directories: all, recent, uploads, generations, and related views.
- Image-type directories from the creation plan.
- Origin directories for upload, workflow generation, and image-session attachment.
- User folders that can be created, renamed, removed, and used as move targets.

Switch between grid and list, sort by name or time, search the current scope, and load cursor pages.

### 5.2 Image Actions

- Preview, rename, and download one image.
- Multi-select loaded images to move or download a ZIP.
- Create a delivery rendition from an image.
- Bind an explicit asset to a reference node.
- Locally edit a readable image; opening from the inspector can adopt the result as the current generation node.

Every generation remains in the library. The system has no rejected-draft state and does not automatically delete unselected candidates.

### 5.3 Agent Organization

The Agent can read bounded names, directories, and metadata; create or rename folders; and rename or move assets. When visual inspection is required, it opens selected images instead of loading the entire library.

## 6. Save Workflow Recipes

To reuse a complete recipe, open New Product and select "From a recipe". Enter the new product name, choose a complete recipe, upload 1 to 6 product references, and optionally add product details. "Preview recipe" shows its structure; cancelling creates neither a product nor a graph. "Confirm and create product" opens the new canvas without an Agent. After a network error, retrying the same confirmation does not create another product. Asset placeholders do not carry source product images: select replacements from the new product's library and review reusable documents for suitability.

The workbench can save:

- A complete workflow preset.
- The local flow of the current group.
- A fragment from the current node selection.

A recipe stores reusable structure, edges, and configuration. It excludes product identity, product images, and generated results. The recipe library contains only content explicitly saved by the user. Apply preview names create vs merge and lists the nodes and edges that will appear; a failed preview cannot be confirmed. Confirm writes the target product's live graph once. Full presets are used for new products from the creation page; an existing workflow is an explicit conflict. Fragment recipes merge into an existing workflow from the workbench or return a conflict.

## 7. Iterative Image Generation

Open `/image-chat`.

1. Create or choose a session.
2. Enter a prompt.
3. Choose image size and candidate count.
4. Optionally choose a completed result as the branch base.
5. Select up to six context references. A branch base consumes one context slot.
6. Set advanced fields supported by the active provider.
7. Submit and inspect queue, progress, candidates, and provider notes.

A completed candidate can be:

- Used as the next branch base.
- Downloaded.
- Saved to the global media library.
- Saved to a selected product library.

After save-to-library, organize, archive, or associate it from `/media-library`. After save-to-product, it uses the same ProductImageAsset management as uploads and workflow results.

## 8. Settings

### 8.1 Provider Profiles

A profile contains:

- Name and provider type.
- Base URL and API key.
- Capabilities.
- Default models and provider configuration.
- Enabled/disabled state.

API keys are not echoed after save. Leaving the secret blank during update does not send the stored value back to the browser.

### 8.2 Purpose Bindings

Each purpose chooses a profile, interface mode, model, and request settings. The profile must advertise the required capability.

### 8.3 Runtime Settings

Settings also controls:

- Allowed image-tool fields and defaults.
- Maximum generation dimension.
- Upload byte, pixel, and count limits.
- Concurrency and security switches.
- Import/export of the current configuration format.

Generation count belongs to the image plan or image-session candidate count and is not duplicated as an advanced image field.

### 8.4 Registration SMTP

The Operator manages registration SMTP in the existing settings page. The closed field set is `smtp_host`, `smtp_port`, `smtp_security` (`starttls` or `tls`), `smtp_username`, `smtp_password`, `smtp_from_address`, and `smtp_from_name`. The development stack reads startup defaults from `.env.dev` as `SMTP_HOST`, `SMTP_PORT`, `SMTP_SECURITY`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM_ADDRESS`, and `SMTP_FROM_NAME`; values saved in Settings override the same-name environment defaults, and restoring a default deletes the database override and returns to the current environment value. The password is secret, is not echoed after save, and is excluded from ordinary configuration exports and logs. The public registration form remains visible before SMTP is ready, while code delivery and registration submission stay disabled.

## 9. Global Agent Dock

After login, the right-hand Global Agent Dock can:

- List, search, create, and archive Agent Sessions.
- Inspect Task summaries, cancel a Task, and pause or resume a pausable Task.
- Open the matching product workspace.
- Display and confirm a global library-organization Draft.

The Dock does not edit the workflow, submit a WorkflowRun, or replace the workbench run controls. Edit, run, cancel, and retry remain on `/products/:productId`.

## 10. Global Media Library

Open `/media-library`.

- Search, filter, and page through long-lived cross-product assets.
- Create folders and tags; batch-move, tag, archive, or restore.
- Upload images, or save an iterative-image candidate into the library.
- Associate a global asset with a workflow sub-library without copying media bytes.

The Agent may propose a library-organization Draft. Names, folders, tags, and archive state do not change before confirmation.

## 11. Troubleshooting

### Agent Conversation Cannot Start

Check that a product name was entered. Upload references in the workbench composer; the create form does not have to be completed first. Then check the Agent binding and Agent service health.

### The Agent Replied but No Workflow Appeared

Check whether the Turn requires input. Answer in the composer; the original turn continues and does not add another user message. Graph edits use canvas ChangeSets / proposals, not WorkflowDraft confirmation.

### A Reference Node Is Empty

Open the product library, confirm the target image exists, and bind the explicit asset from the inspector. The product cover is not automatically used by every reference node.

### Local edit is unavailable

The inspector or library entry explains whether the current image provider supports remove, replace-text, and inpaint. The node's current image is not replaced when it is unsupported. Mock bindings can complete these operations; OpenAI requires the masked local-edit capability on the profile; Gemini currently does not support it.

### Image Generation Failed

Check the Image binding, model, references, generation specification, and safe error. Change unsupported size or advanced fields and retry.

### Workflow Routing Is Hard to Read

Use automatic layout, place one image type or local stage in a group, then enter the group to lay out only its members. Groups do not require dependency changes. Cross-group edges stay visible on the full graph.

### A Newly Saved Image Is Missing From the Media Library

Confirm the save finished, then check whether the current filters hide the source, folder, or archived items.

### Settings Cannot Save

Confirm that the current session belongs to a signed-in Operator, then inspect provider capability, model, and request-field validation errors.

### Are Secrets Written to Logs?

Application logs must not contain API keys, session tokens, complete upload bytes, or data URLs. Treat any such log as a security issue.

For self-hosted troubleshooting, read `storage-dev/logs/` (Compose: `logs/` under the mounted storage volume): `productflow-api.log`, `productflow-worker.log`, `productflow-dispatcher.log`. Each line is JSON with `ts`, `level`, `msg`, and `process`. API access lines include `request_id`. The terminal shows the same events as readable lines, not JSON.

When adopting a delivery image, failed or inconclusive checks show their findings. Cancel to revise, or explicitly confirm adoption and download; confirmation does not change the check into a pass. Missing, corrupt or unauthorized images remain blocked. Unresolved generation holds are returned at expiry (72 hours by default); the platform retains the unknown provider-cost record and bears unverifiable costs.
