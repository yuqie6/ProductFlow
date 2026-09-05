---
name: web-design-guidelines
description: Review UI code for Web Interface Guidelines compliance. Use when asked to review UI, check accessibility, audit design, review UX, or check the site against best practices.
metadata:
  author: vercel
  version: "1.0.0"
  argument-hint: <file-or-pattern>
---

# Web Interface Guidelines

Review files for compliance with Web Interface Guidelines.

ProductFlow overlay: user-visible copy still follows `.cursor/rules/ui-language.mdc`. Icon-only controls are required to have `aria-label`. Do not "fix" icon toolbars by stuffing visible English Title Case labels back onto them. Chinese UI uses sentence-like phrasing, not Chicago Title Case.

## How It Works

1. Establish the requested review scope from the files, diff or reported interaction. Ask only when the target cannot be reasonably inferred.
2. Read the relevant sections of `command.md` in this directory (locally adapted snapshot); a comprehensive UI audit uses all applicable sections. Reuse unchanged guidance already loaded.
3. Check observable behavior against project contracts. Native controls, existing state ownership and measured rendering costs determine which guidelines apply; do not add handlers, URL state or virtualization solely to satisfy a generic checklist.
4. Report actionable findings with file locations, impact and a concrete correction. A review remains read-only unless fixes are authorized. State any browser or accessibility behavior that was not verified.

If the user asks for the latest upstream rules, fetch them for comparison from the source below. Replace the local snapshot only when an update is authorized, preserving the ProductFlow adaptations and license:

```
https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md
```
