# ProductFlow Beginner Tutorial and Reference

[中文](USER_GUIDE.md) | English

This document has two parts:

1. **Beginner tutorial**: minimal jargon, follow the clicks and fields, and generate one usable product image first.
2. **Reference**: after completing one run, read more about workbench cards, prompt configuration, model settings, and common questions.

ProductFlow no longer provides a top-navigation guide button, guide progress card, or separate beginner tutorial page. This document remains as text reference; follow the steps below manually to complete one full flow.

The current workbench is the **ProductFlow workbench**: the middle area is a zoomable and draggable node canvas; the right side is a compact sidebar that switches between **Details / Runs / Images** with a small rail. Normal use does not require understanding the internal DAG. Just remember: product, reference image, copy, and image generation are cards; edges mean "downstream generation refers to upstream data".

---

## Beginner Tutorial: Start from One Product Image

Goal: upload one product image, add a little information, generate copy, then generate a satisfying image.

### 1. Create a Product

1. Click **Products / Workbench** in the top navigation.
2. Click **New product**.
3. Upload a clear product main image.
4. Fill in a product name, for example: `cream white commuter tote bag`.
5. Click **Create and continue**.

Expected result: the page enters this product's workbench, with several clickable cards in the middle.

### 2. Add Product Details

1. Click the **Product** card on the canvas.
2. The right side switches to **Details**. Add category, price, product description, or the direction you want to emphasize this time.
3. Example description: `Suitable for commuting and weekend outings, lightweight, large capacity, cream white color.`
4. Click **Save**, or wait until the right-side status shows **Saved**.

Expected result: the form saves successfully. Later copy and image generation use these saved product details.

### 3. Generate the First Copy Version

1. Click the **Copy** card.
2. In generation requirements, write one sentence, for example:

   ```text
   Emphasize commuting, lightweight design, and large capacity. Use a premium tone without exaggeration.
   ```

3. Click **Run current node**. If you want to run from product details all the way to image generation, click **Run workflow**.

Expected result: the copy card generates a title, selling points, poster headline, and button copy.

If you are not satisfied, change only one direction and try again, such as "make it younger", "make it more concise", or "use fewer exaggerated words".

### 4. Add or Connect Reference Images

If you have a style image you want to reference:

1. Select or add a **Reference image** card.
2. Upload a reference image, such as lighting, background, composition, or style that you like. Reference image upload also supports click-to-select and drag-and-drop.
3. Drag from the connection point on the reference image card to the **Copy** or **Image generation** card.

You only need to remember: **connecting to it = reference it during generation**.

Expected result: an edge appears on the canvas. When the connected card runs later, it references this image's tags and image information. If you connect the wrong edge, select and delete the edge, then drag a new one.

### 5. Generate the First Image

1. Click the **Image generation** card.
2. Confirm that the **Image generation** card is connected to at least one downstream **Reference image** card. The image-generation card only triggers generation; it does not display/download images itself. Generated results are written into the connected reference image cards.
3. Write image requirements, for example:

   ```text
   Place a white tote bag on a commuter desk with a laptop and coffee nearby, clean natural light, suitable for an ecommerce main image.
   ```

4. Click **Run current node** or **Run workflow**.

Expected result: the downstream reference image card is filled with the new image and provides preview/download on the card. The right-side **Images** panel also aggregates the image. Click the thumbnail to preview it in the app; click **Download** to download the original image.

If there is no downstream reference image card connected, the system tells you to connect at least one image/reference image node first. It will not silently place the image on the image-generation card.

### 6. Keep Adjusting Until Satisfied

Change only one or two things per round; it is easier to tell which sentence worked.

Common adjustments:

- Subject is unclear: add `product centered in frame, complete subject, clear texture`.
- Background is too busy: add `clean background, fewer props, keep only 1-2 supporting objects`.
- Style is wrong: add `natural light`, `magazine-like composition`, `minimal ecommerce`, or `warm lifestyle`.
- Selling point is missing: put the most important selling point in the first sentence, such as `large capacity`, `lightweight`, or `commuter-friendly`.

Copyable rewrite example:

```text
Make the background cleaner, keep only the laptop and coffee; the bag texture should be clear and the shadow soft.
```

Download the image when you are satisfied. If you want to continue fine-tuning iteratively, click **Image chat** in the top navigation. If this image came from Image chat, you can also save it to **Gallery** for centralized browsing later.

### Canvas Basics

- **Zoom**: move the mouse into the workbench canvas and scroll the wheel; the canvas zooms around the mouse position. Zoom buttons and percentage are also available in the lower-right corner.
- **Pan**: hold the left mouse button on blank canvas and drag to move the view. Dragging cards, clicking buttons, uploading, or dragging edges does not trigger canvas panning.
- **Move cards**: hold the card body or title area and drag; the position is saved after release. It stays where you placed it after refresh.
- **Connect cards**: drag from a card connection point to a target card. An edge is created after release. Edges are part of the workflow, not temporary visuals.
- **Adjust sidebar**: the right sidebar handles Details, Runs, and Images. It stays compact and no longer uses a large bottom panel that occupies canvas space.

### 7. Use Iterative Image Generation for Detail Tuning

1. Click **Image chat** in the top navigation.
2. Select a product, or generate freely first.
3. The first image can be generated directly from a text description. For later edits, first click a completed image in history as the base image.
4. Request changes conversationally, for example:

   ```text
   Keep the bag angle unchanged, change the background to a brighter office, and reduce desk clutter.
   ```

5. When satisfied, write the image back to the product so the workbench can reference it later.

On small screens, Image chat switches to a single-column layout: top actions, generation status, result preview, history, and generation settings appear in order. When generating multiple candidates, the history first shows matching in-progress placeholders.

### 8. Save to Gallery

Image chat results can be saved to **Gallery**. The gallery keeps image source, linked product, prompt, size, and model information, and provides a download entrypoint.

Good gallery candidates:

- Backgrounds or compositions that may be reused later but should not be attached to a product yet.
- Satisfying candidates that need to be reviewed together.
- Useful tuning results that are not the current product's final image.

---

## Reference: What Cards Are in the Workbench

These notes are for users who have completed one run and want more precise control.

### Product

Stores product name, category, price, and description. Downstream generation prioritizes the latest saved product details.

### Reference Image

A reference image card holds only the current image. You can upload manually, or let an image-generation card fill it with a new image. The new image replaces the current image in the card; old assets remain in product history.

When a reference image card is selected, assets in the right-side **Images** panel show fill actions. When filling from an existing asset, the system reuses the existing asset record and does not create a duplicate upload for the same image.

### Copy

Generates title, selling points, poster headline, and button copy. After generation, you can keep editing inside the card. Edited copy is used by later image generation.

### Image Generation

Triggers image generation based on product details, copy, reference images, and your image requirements. It is not an image slot: generated images are written into connected downstream reference image cards. If no downstream reference image card is connected, running fails and tells you to connect at least one image/reference image node first.

The image-generation card now distinguishes between "generate directly from product details" and "generate with copy/reference context": when upstream copy or reference images are connected, generation reads that context. Without connected copy, it can still try to generate from product details and the node's image requirements.

---

## Reference: Connections and Runs

- Connect A to B: B references A during generation.
- To try one card only: select the card and run the current node.
- To generate from product details all the way to image: run the whole workflow.
- Before running, confirm that the right-side form is saved. If the selected card has unsaved draft content, the current run button first attempts to save it, then starts running.
- You can keep organizing canvas positions while the workflow is running, but do not repeatedly click run or change the structure.
- Image-generation results are not downloaded from the image-generation card. Use the downstream reference image card or the right-side **Images** panel.

---

## Reference: Prompt Configuration

Open **Settings** in the top navigation and find the **Prompts** group. You can adjust four long-term default prompt templates:

- `prompt_brief_system`: default prompt for product understanding.
- `prompt_copy_system`: default prompt for copy generation.
- `prompt_poster_image_template`: workbench image-generation template.
- `prompt_poster_image_edit_template`: workbench edit template when upstream copy or reference-image context is present.
- `prompt_poster_image_reference_policy`: visual-reference rule used by the `reference_policy` placeholder in workbench image templates.
- `prompt_image_chat_template`: iterative image-generation template.

Recommended usage:

- For one-off effects: write requirements in the copy card or image-generation card.
- For long-term tone or format: change prompt templates in the settings page.
- If unsure: copy the default value first, make a small adjustment, save, and test.

Restoring defaults deletes the custom value from the database and returns to the system default prompt.

Common placeholders:

- Workbench image template: `product_name`, `category`, `price`, `source_note`, `instruction`, `title`, `selling_points`, `poster_headline`, `cta`, `context_block`, `reference_policy`, `size`, `kind`, `kind_label`, `kind_requirements`.
- Workbench edit template: `product_name`, `category`, `price`, `source_note`, `instruction`, `title`, `selling_points`, `poster_headline`, `cta`, `context_block`, `reference_policy`, `size`, `kind`, `kind_label`, `kind_requirements`.
- Iterative image template: `prompt`, `size`, `history_block`.

If a placeholder is misspelled, the system does not crash just because of the unknown placeholder. That part may not be replaced as expected. Prefer small edits followed by testing.

---

## Reference: Model and Runtime Settings

The top-navigation **Settings** page can also manage:

- Copy provider and copy model.
- Image provider and image model.
- Default image size. Iterative image generation and workbench image generation can directly select common 1K / 2K / 4K frames or enter custom width/height.
- Iterative image-generation idle recovery threshold, defaulting to 90 minutes; the system judges stale running tasks by the latest generation-progress heartbeat.
- Upload file size limits.
- API keys for copy/image providers.

Secret fields are not echoed back. Saving with an empty value does not overwrite the old secret; only entering a new value writes it to the database.

## Reference: Running State

Copy, poster, workflow, and Image chat generation are background tasks. Pages refresh status while running, but they do not repeatedly download complete historical data:

- Image chat updates queue position, completed candidate count, latest progress time, provider status, success/failure state, and failure reason.
- Product workflows update node state, run state, and failure reasons.
- After a task ends, the page refreshes full details and shows new images, copy, or product history.

If a page does not change for a long time, check the running state and error message first, then refresh the page to confirm backend results.

---

## Common Questions

### Running a downstream card directly did not use new details?

First confirm that the right-side form has been saved. Runs use saved content, not unsaved input draft.

### Does the image-generation card have to connect to a reference image card?

It must connect to at least one downstream reference image card. The image-generation card only triggers and configures generation; image preview/download happens on the filled reference image card. If one image-generation card connects to multiple reference image cards, generation runs concurrently and fills those cards separately.

### Image quality is poor. How should I change the prompt?

Do not change many sentences at once. Change only one item per round: background, composition, lighting, or subject detail. This makes it easier to know which sentence improved the result.

### Settings failed to save?

Check the field name in the page error message. A common cause is invalid image size format, such as needing `1024x1024`. Custom width/height does not need to be added to an allow-list beforehand, but width and height must be positive and each side must not exceed the system safety limit of `3840`.

### Are complete prompts recorded in logs?

They should not be. The backend only saves necessary node summaries and artifact references. It should not log full prompts, secrets, uploaded bytes, or provider payloads.
