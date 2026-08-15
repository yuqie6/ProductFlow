# Frontend Component Guidelines

## Structure

Use named function components with explicit props.

```tsx
interface PanelProps {
  productId: string;
  onClose: () => void;
}

export function Panel({ productId, onClose }: PanelProps) {
  // hooks
  // derived values
  // handlers
  return <section>{/* UI */}</section>;
}
```

Keep static typed maps outside render. Keep handlers close to the owning interaction.

## Component Ownership

- Route pages own navigation and high-level query composition.
- Feature components own one visual/interaction domain.
- Shared components have demonstrated reuse and small stable props.
- Pure mapping/parsing belongs in a helper beside the feature.

Avoid both monolithic pages and tiny wrapper components that add no contract.

## Workbench Reuse

The Agent workbench reuses current canvas/UI components under:

- `pages/product-detail/`
- `pages/product-workflow-v2/`
- `pages/agent-workbench/`

When adding Agent behavior, compose with `WorkflowNodeCard`, canvas chrome, inspector, side rail, image explorer, and command surfaces. Do not create a visually unrelated node system.

## Node Cards and Inspector

Node cards:

- compact title/type/status;
- stable preview dimensions;
- one clear input/output handle as appropriate;
- selection and failure affordances;
- no dense forms.

Inspector:

- complete editable controls;
- grouped by node responsibility;
- visible save state;
- field-level validation;
- stable panel width and scrolling.

Use segmented controls for modes, switches/checkboxes for booleans, selects/menus for option sets, sliders/inputs for numbers, and icon buttons for familiar commands.

## Styling

Follow existing Tailwind conventions and theme tokens. Both light and dark modes are required.

- Cards have restrained radius and hierarchy.
- Do not nest decorative cards.
- Operational pages favor dense, scannable information.
- Fixed controls use stable height/width.
- Long names truncate or wrap within their owner.
- Letter spacing remains zero.
- Font size does not scale with viewport width.
- Avoid decorative gradients/orbs and one-hue surfaces.

Use Lucide icons already installed. Add `aria-label`/`title` for icon-only buttons.

## Responsive Components

Choose the measurement boundary that matches the component:

- page layout may use viewport breakpoints;
- inspector-contained Explorer uses ResizeObserver content width;
- canvas controls use explicit responsive states;
- mobile drawers/sheets use existing Vaul patterns.

Touch actions cannot rely on hover. Verify that drawers, software keyboard, bottom bars, and canvas gestures do not overlap.

## Forms

- Inputs have labels or accessible names.
- Buttons inside forms specify `type`.
- Submit handlers prevent default and call a mutation.
- Pending state disables duplicate submit.
- Errors appear near the relevant form/action.
- Secret fields do not repopulate with stored values.
- Numeric controls enforce current min/max and do not shift layout.

## Internationalization

All application chrome uses `t(...)`.

- Add keys for zh-CN, en-US, ja-JP, and vi-VN.
- Keep backend enum/system ids out of visible text.
- Product names, prompts, filenames, Agent messages, and provider notes are user/server content and are not translated.
- Verify dictionary completeness through tests/build.
- Remove keys when their page/action is deleted.

## Image Controls

Reuse shared image-generation controls for:

- aspect ratio/size;
- candidate count;
- quality/format/background;
- provider-supported advanced fields.

Workflow GenerationSpec and ImageChat requests have different business contracts. Share visual primitives and parsing where the data shape truly matches; do not force one giant settings component.

Generation count is an explicit business control, separate from advanced tool options.

## Global Navigation

`TopNav` reflects current routes:

- products;
- image chat;
- gallery;
- help;
- settings.

It owns locale/theme/logout controls and responsive navigation. Route matching uses current canonical paths.

## Gallery Page

The global Gallery is a collection surface for explicit ImageSession favorites. It is distinct from the Product Image Explorer.

- Stable image grid/list behavior.
- Source/session/product metadata.
- Preview and download.
- Empty/loading/error states.

Do not add product-library folder controls to global Gallery.

## Accessibility

- Semantic buttons, links, nav, headings, lists, and dialogs.
- Keyboard-visible focus.
- Modal/drawer focus behavior from existing primitives.
- Images have meaningful alt text or empty alt when decorative.
- Status is not conveyed only by color.
- Screen-reader text for loading/icon-only actions.
- Minimum practical touch targets on mobile.

Canvas accessibility should preserve keyboard actions and provide labeled external controls for operations that are hard to expose through graph handles alone.

## Data Boundary

Presentational components receive typed DTO/projection/callback props. Page/controller hooks call API methods.

Exceptions are feature controllers explicitly designed to own their queries, such as ProductImageExplorer.

No raw fetch in JSX components.

## Motion

Motion communicates state:

- Agent delta arrival;
- Draft-to-canvas transition;
- materialization reveal;
- drawer/panel opening;
- selection/drag feedback.

Respect `prefers-reduced-motion`. Avoid animation that delays a command or makes canvas coordinates unstable.

## Tests

Test:

- accessible labels and commands;
- mode/selection changes;
- validation/pending/error;
- responsive branch helpers;
- node card/inspector projections;
- i18n completeness;
- current navigation.

Use real browser screenshots for layout, overlap, canvas, touch, or animation changes.

## Avoid

- Parallel visual systems for the same node.
- Feature instructions rendered as permanent in-app prose.
- Hover-only mobile action.
- Card-inside-card layout.
- Raw enum strings shown to users.
- Localized duplicate logic inside components.
- Unstable dimensions caused by dynamic content.
- Shared component abstractions with only one artificial caller.
