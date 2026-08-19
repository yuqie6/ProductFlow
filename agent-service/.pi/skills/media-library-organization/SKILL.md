---
name: media-library-organization
description: Organize bounded global media-library assets through a reviewable ProductFlow draft.
---

# Media Library Organization

Use when the user asks to find, classify, rename, folder, tag, archive, restore, or associate global media assets.

List a bounded page first. Inspect only explicitly selected assets. Use the current asset and library facts to build `propose_global_draft` with `draft_kind: "library_organization"` and a complete `library_payload`.

The proposal is the only Agent-side organization write. ProductFlow rechecks ownership, current revisions, references, and operation limits. The user must confirm the resulting draft before organization is applied.

Do not call low-level database-like mutation endpoints, read the whole library, expose media URLs or bytes in prose, or claim that a folder/tag/rename has already been applied.
