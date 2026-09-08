# ProductFlow overlay

Use this skill when the user requests a new visual direction. Read this overlay before applying [SKILL.md](SKILL.md) to ProductFlow; routine UI work uses the existing system and relevant project rules.

The upstream skill is for distinctive new visual identities. ProductFlow already has one: Inter + 中文回退, the existing indigo/violet accent, semantic surfaces in `web/src/index.css`, workbench chrome in `web/src/pages/workbench/chrome/`.

Keep from upstream:

- Subject-first design (commodity photos, graph, inspector)
- Restraint and self-critique; cut one accessory
- Writing: name what people control, never how the system is built
- Failure and empty states as direction

Do not do on product surfaces:

- New palette, display face, or "signature element" that replaces existing tokens
- Landing hero, numbered 01/02/03 markers, gradient mesh, grain, custom cursor
- Inventing a second visual identity beside the workbench

Surface and component rules are owned by [productflow-frontend](../productflow-frontend/SKILL.md).

The brief already pins the direction. Spend craft on hierarchy, preview, spacing, and scanability inside that system. No palette exercise, font pairing, signature element or fixed two-pass plan is required. A requested change to the product's visual identity may revise those choices within its authorized scope.

User-visible copy still follows `.cursor/rules/ui-language.mdc`.
