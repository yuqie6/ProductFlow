# ProductFlow User Guide

This document is the canonical user-operations source. The in-product `/help` pages (`web/src/pages/HelpPage.tsx`) are a projection of the same operations and must change in the same commit. This document is optimized for repository search and troubleshooting.

## 1. First Use

1. Log in with `ADMIN_ACCESS_KEY`.
2. Open `/settings` and unlock it with `SETTINGS_ACCESS_TOKEN`.
3. Create provider profiles with Base URL, API key, capabilities, and default models.
4. Bind profiles, interfaces, and models to `prompt`, `agent`, and `image`.
5. Save and return to `/products`.

All three purposes are required by the core flow:

- Prompt: visual system, creative brief, and prompt-generation nodes.
- Agent: requirement clarification, library organization, and workflow creation.
- Image: workflow and iterative image generation.

## 2. Create a Product

Open `/products/new`.

### 2.1 Choose an Image Plan

- Enter the product name.
- Select the required image types.
- Each type defaults to two images and has an independent quantity control.
- Multiple hero images are usually candidates. For multi-angle output, describe the intended angle or content of each image during clarification.

### 2.2 Upload Real References

- Upload one to six images of the actual product.
- PNG, JPEG, and WebP are supported.
- Images should expose product shape, structure, color, and important details.
- Logos, certifications, packaging, factory material, and manuals can only be used when the user supplies them.

References are product evidence and candidates for reference-node bindings. Product cover selection is automatic; no separate cover prop is required.

### 2.3 Clarify with the Agent

The Agent checks the selected image types and uploaded references for gaps. Common questions cover:

- Price, category, specifications, and selling points.
- Visual style, colors, typography, photography, and realism.
- Image-text language, copy density, and forbidden content.
- Aspect ratio, quality, reference fidelity, and background.
- Whether multiple images of one type are candidates or distinct angles/content.

Questions should affect the workflow or image result. The user can add text or upload more verifiable product material.

### 2.4 Confirm the Draft

The confirmation view summarizes:

- Product facts and conflicts.
- Image types and quantities.
- Shared visual system.
- Per-type or per-image goals and prompts.
- Reference bindings.
- Aspect ratio, resolution, quality, text policy, and delivery specifications.
- Planned folders, nodes, and edges.

Confirmation writes the draft into the workflow graph, then opens the product workbench with the same Agent conversation in the sidebar.

### 2.5 Create the Canvas Directly

Product creation can skip the Agent and write a runnable workflow immediately. After you upload references and choose image types, the graph includes product facts, references, visual system, creative brief, prompt nodes, and image nodes, with photos wired to the visual, brief, prompt, and image nodes. Open the workbench and run the visual system, creative brief, and prompt nodes first so the model writes into the inspector; then run image nodes or the whole graph.

## 3. Product Workbench

### 3.1 Node Types

- Product facts: confirmed product facts.
- Image asset: one explicit image from the product library.
- Creative brief: running the node writes goals, copy, and constraints from product facts and photos; the result stays editable.
- Visual system: running the node writes style, background, and constraints from product facts and photos; the result stays editable.
- Prompt generation: running the node writes a prompt from product facts, photos, visual system, and brief; the result stays editable. Running this node does not render images.
- Image generation: aspect ratio, resolution, quality, background, text policy, reference fidelity, and execution state.

### 3.2 Canvas Operations

- Adding a node from the add panel places it near the current viewport center and selects it.
- Drag from an output handle to a target input handle to create an edge. Legal targets turn green; illegal targets turn red.
- Drag nodes to position them; zoom with wheel/touch and pan from blank canvas.
- Ctrl/Cmd/Shift-click or marquee-select multiple nodes.
- Copy then paste selects the new nodes and keeps edges inside the selection.
- Deleting nodes asks for confirmation; deleting an edge can be undone immediately.
- Ctrl/Cmd+Z undoes the last graph edit; Ctrl/Cmd+Shift+Z redoes it unless a newer edit landed.
- Node cards keep type color; running uses a glow; failures appear on the card and in the open inspector.
- The node toolbar can run, run up to here, duplicate, focus, save as recipe, and delete; image assets also have bind.
- Maximize hides the top navigation so the canvas fills the main area.
- Use automatic layout to improve routing.
- Put a local flow in a canvas group. Double-click the group or use the enter control to see only its members; the breadcrumb returns to the full graph. In-group and full-graph viewports are remembered separately. A group changes visual organization only, not DAG execution order. Cross-group edges stay visible on the full graph.

Node cards show compact scanning summaries. Edit complete content in the inspector so cards remain readable.

### 3.3 Inspector

With nothing selected, Details offers add-node, open-library, and run-graph. With a node selected:

Reference nodes:

- Select one product-library asset.
- Preview the current binding.
- Upload or save an image-session result to the product before binding a new reference.

Creative brief nodes:

- Run the node to generate the goal, design goals, and prohibitions from photos and product facts.
- The result is written into the inspector and can be edited, then rerun.

Visual system nodes:

- Run the node to generate style keywords and background from photos and product facts.
- The result is written into the inspector; without a version those values are used as-is.

Prompt nodes:

- Run the node to write a prompt into the inspector. This does not render images.
- Edit image goal, composition, content, text, and atmosphere.
- New executions retain artifact versions.

Image nodes:

- Choose aspect ratio and resolution tier.
- Set quality intent, reference fidelity, background, and text policy.
- Text policy constrains prompt generation and rendering. The default forbids on-image copy; switch to allow or required and set a language when the image needs letters.
- Connect a reference image before running. Direct create wires uploads to visual, brief, prompt, and image nodes.
- Running this node only renders an image. Empty visual, brief, and prompt nodes need their own runs, or use run-to-node on the image node.
- Inspect output both on the node and in the library.

### 3.4 Runs

- Run this node executes only the selected processing node: visual, brief, and prompt write content; image generation renders.
- Run to this node runs upstream processing nodes, then the selected node.
- Run the whole graph follows DAG order: visual system, creative brief, prompt generation, then image generation.
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

Every generation remains in the library. The system has no rejected-draft state and does not automatically delete unselected candidates.

### 5.3 Agent Organization

The Agent can read bounded names, directories, and metadata; create or rename folders; and rename or move assets. When visual inspection is required, it opens selected images instead of loading the entire library.

## 6. Save Workflow Recipes

The workbench can save:

- A complete workflow preset.
- The local flow of the current group.
- A fragment from the current node selection.

A recipe stores reusable structure, edges, and configuration. It excludes product identity, product images, and generated results. The recipe library contains only content explicitly saved by the user. Applying a recipe on another product previews the nodes and edges that will be created, then writes a draft to review. Partial presets cannot merge into an existing workflow yet.

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

`/gallery` redirects to `/media-library`. The old collected-image gallery is no longer a product entry.

The Agent may propose a library-organization Draft. Names, folders, tags, and archive state do not change before confirmation.

## 11. History Archives

`/history` provides read-only browse, filter, download, and export for retired V1 workflows, user templates, and Canvas Agent records. Historical images can be previewed or downloaded only after canonical media verification; missing and pending media remain visible as metadata.

“Rebuild with Agent” creates a WorkflowDraft and AgentConversation seeded from history for the original or selected target product. The user still reviews Agent questions and the structured Draft and confirms it explicitly. The action does not restore the old canvas, template application, or executor.

## 12. Troubleshooting

### Agent Creation Cannot Start

Select at least one image type and upload one to six valid product images. Then check the Agent binding and Agent service health.

### The Agent Replied but No Workflow Appeared

Check whether the Turn requires input or awaits confirmation. Answer the question and confirm the latest WorkflowDraft revision so the workbench can persist the graph.

### A Reference Node Is Empty

Open the product library, confirm the target image exists, and bind the explicit asset from the inspector. The product cover is not automatically used by every reference node.

### Image Generation Failed

Check the Image binding, model, references, generation specification, and safe error. Change unsupported size or advanced fields and retry.

### Workflow Routing Is Hard to Read

Use automatic layout, place one image type or local stage in a group, then enter the group to lay out only its members. Groups do not require dependency changes. Cross-group edges stay visible on the full graph.

### A Newly Saved Image Is Missing From the Media Library

Confirm the save finished, then check whether the current filters hide the source, folder, or archived items. `/gallery` is only a redirect and no longer shows a separate collection list.

### Settings Cannot Save

Confirm the independent SETTINGS_ACCESS_TOKEN unlock and inspect provider capability, model, and request-field validation errors.

### Are Secrets Written to Logs?

Application logs must not contain API keys, session tokens, complete upload bytes, or data URLs. Treat any such log as a security issue.
