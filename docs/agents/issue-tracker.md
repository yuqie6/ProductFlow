# Issue Trackers

Product triage and PRDs use GitHub Issues. Delegated execution tasks share [`../audits/tasks/`](../audits/tasks/README.md), whether produced by a business group or requested directly by the user. "Record a task for another agent" means the local board unless GitHub is explicitly requested; independent tasks need no group, charter or GitHub issue. "Fix it directly" means investigate and fix in the current session without requiring a ticket. Read-only investigations also need no ticket unless requested.

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

Create a GitHub issue for product triage or a PRD. For a requested local execution task, grouped or independent, follow the local board protocol instead; no GitHub issue is required.

## When a skill says "fetch the relevant ticket"

For a GitHub number or URL, run `gh issue view <number> --comments`. For a local task ID or path, read its task file and the board protocol, then inspect applicable repository rules and live implementation.
