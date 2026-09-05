# Issue Trackers

Product triage and PRDs use GitHub Issues. Delegated execution tasks share [`../audits/tasks/`](../audits/tasks/README.md), whether produced by a business group or requested directly by the user. "Record a task for another agent" means the local board unless GitHub is explicitly requested; independent tasks need no group, charter or GitHub issue. "Fix it directly" means investigate and fix in the current session without requiring a ticket. Read-only investigations also need no ticket unless requested.

An execution issue can link its originating GitHub issue. Keep detailed execution evidence locally and post only the outcome/link to GitHub when requested. Local closure does not close a product issue or pass a business-group gate. Do not maintain two copies of one execution status.

Use the `gh` CLI for GitHub operations.

## Conventions

- **Create an issue**: `gh issue create --title "..." --body-file <path>`. For multiline content, use a structured tool argument or an exact-text body file with safe shell quoting.
- **Read an issue**: `gh issue view <number> --comments`, filtering comments by `jq` and also fetching labels.
- **List issues**: `gh issue list --state open --json number,title,body,labels,comments --jq '[.[] | {number, title, body, labels: [.labels[].name], comments: [.comments[].body]}]'` with appropriate `--label` and `--state` filters.
- **Comment on an issue**: `gh issue comment <number> --body "..."`
- **Apply / remove labels**: `gh issue edit <number> --add-label "..."` / `--remove-label "..."`
- **Close**: `gh issue close <number> --comment "..."`

Infer the repo from `git remote -v` - `gh` does this automatically when run inside a clone.

## When a skill says "publish to the issue tracker"

A skill does not authorize publication. When the user has authorized publishing product triage or a PRD, create the GitHub issue without requesting the same permission again. For a requested local execution task, grouped or independent, follow the local board protocol; no GitHub issue is required. Otherwise return the draft in the conversation or the authorized local artifact.

## When a skill says "fetch the relevant ticket"

For a GitHub number or URL, run `gh issue view <number> --comments`. For a local task ID or path, read the task packet and relevant board protocol. Reading a packet for selection or reporting does not grant execution ownership; confirm a claim before task-specific implementation investigation. Reuse unchanged procedures already loaded.
