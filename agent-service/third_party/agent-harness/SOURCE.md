# agent-harness source snapshot

- Module: `github.com/yuqie6/agent-harness`
- Commit: `491c9d7e56d9dfc5004afbcda1a0f578df1cf95e`
- Commit time: `2026-08-11T18:24:02+08:00`
- License: Apache-2.0 (`LICENSE` in this directory)

This directory is an unmodified source snapshot of the packages required by
`agenttask` at the commit above. ProductFlow keeps the snapshot in-tree because
that commit is not currently available from a remote Go module source. Release
builds must not replace it with a developer-machine sibling checkout.

Included paths:

- `agenttask/`
- `durable/`
- `turn/`
- the `internal/` packages in the `agenttask` dependency graph
- upstream `go.mod`, `go.sum`, and `LICENSE`

Refresh the snapshot from a clean agent-harness checkout with `git archive`,
then update the commit metadata and run the full Go compatibility tests. Do not
edit vendored source in place.
