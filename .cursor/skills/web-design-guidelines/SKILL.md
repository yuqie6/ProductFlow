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

1. Read `command.md` in this directory (vendored snapshot).
2. Read the specified files, or ask which files to review.
3. Check against all rules in `command.md`.
4. Output findings in the terse `file:line` format from that file.

If the user asks for the latest upstream rules, fetch and replace `command.md` from:

```
https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md
```
