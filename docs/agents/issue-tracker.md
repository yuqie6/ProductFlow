# Issue Trackers

Product requests, PRDs and externally reported problems live in GitHub Issues. Bounded execution issues for the internal audit groups live in [`../audits/tasks/`](../audits/tasks/README.md). Use the local board when the user asks to publish or claim a business-group task; use GitHub for product triage and PRDs. Ordinary fixes and read-only investigations need neither unless requested.

An execution issue can link its originating GitHub issue. Keep detailed execution evidence locally and post only the outcome/link to GitHub when requested. Local closure does not close a product issue or pass a business-group gate. Do not maintain two copies of one execution status.

Use the `gh` CLI for GitHub operations.

## Conventions

- **Create an issue**: `gh issue create --title "..." --body "..."`. Use a heredoc for multi-line bodies.
- **Read an issue**: `gh issue view <number> --comments`, filtering comments by `jq` and also fetching labels.
- **List issues**: `gh issue list --state open --json number,title,body,labels,comments --jq '[.[] | {number, title, body, labels: [.labels[].name], comments: [.comments[].body]}]'` with appropriate `--label` and `--state` filters.
- **Comment on an issue**: `gh issue comment <number> --body "..."`
- **Apply / remove labels**: `gh issue edit <number> --add-label "..."` / `--remove-label "..."`
- **Close**: `gh issue close <number> --comment "..."`

Infer the repo from `git remote -v` - `gh` does this automatically when run inside a clone.

## When a skill says "publish to the issue tracker"

Create a GitHub issue for a product request or PRD. For an explicitly requested internal business-group execution issue, follow the local board protocol instead; no GitHub issue is required.

## When a skill says "fetch the relevant ticket"

For a GitHub number or URL, run `gh issue view <number> --comments`. For a local task ID or path, read its task file and the board protocol, then inspect applicable repository rules and live implementation.
