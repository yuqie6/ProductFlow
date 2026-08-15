# Cross-Layer Thinking Guide

> **Purpose**: Think through data flow across layers before implementing.

---

## The Problem

**Most bugs happen at layer boundaries**, not within layers.

Common cross-layer bugs:
- API returns format A, frontend expects format B
- Database stores X, service transforms to Y, but loses data
- Multiple layers implement the same logic differently

---

## Before Implementing Cross-Layer Features

### Step 1: Map the Data Flow

Draw out how data moves:

```
Source → Transform → Store → Retrieve → Transform → Display
```

For each arrow, ask:
- What format is the data in?
- What could go wrong?
- Who is responsible for validation?

### Step 2: Identify Boundaries

| Boundary | Common Issues |
|----------|---------------|
| API ↔ Service | Type mismatches, missing fields |
| Service ↔ Database | Format conversions, null handling |
| Backend ↔ Frontend | Serialization, date formats |
| Component ↔ Component | Props shape changes |

### Step 3: Define Contracts

For each boundary:
- What is the exact input format?
- What is the exact output format?
- What errors can occur?

### Step 4: Audit Persisted Historical Values

When a migration preserves JSON, enum-like strings, archive payloads, or runtime settings, every stored value is part of the compatibility contract even if its online behavior is retired. Before removing a runtime feature:

- Search database fixtures, migration code, ORM/Pydantic contracts, serializers, frontend unions, label maps, and test payloads for every historical value.
- Classify each value as online, archive-only, migration-only, or invalid. Keep archive-only values readable without restoring their old executor or editor.
- Add the value at every typed boundary that must round-trip it. A backend enum alone is insufficient when the frontend has exhaustive maps.
- Verify one real upgraded database record through the API and page bootstrap. Unit validation of a newly constructed payload does not cover deployed historical data.

For a preserved workflow fact, the expected path is:

```text
PostgreSQL JSON (legacy_product)
  -> WorkflowDraftPayloadV1 / ProductFactSourceType
  -> agent-workbench bootstrap response
  -> frontend ProductFactSourceType / label map
  -> read-only historical label
```

A retired value must not be silently rewritten, dropped, or routed into an online V1 executor during this read path.

---

## Common Cross-Layer Mistakes

### Mistake 1: Implicit Format Assumptions

**Bad**: Assuming date format without checking

**Good**: Explicit format conversion at boundaries

### Mistake 2: Scattered Validation

**Bad**: Validating the same thing in multiple layers

**Good**: Validate once at the entry point

### Mistake 3: Leaky Abstractions

**Bad**: Component knows about database schema

**Good**: Each layer only knows its neighbors

---

## Checklist for Cross-Layer Features

Before implementation:
- [ ] Mapped the complete data flow
- [ ] Identified all layer boundaries
- [ ] Defined format at each boundary
- [ ] Decided where validation happens

After implementation:
- [ ] Tested with edge cases (null, empty, invalid)
- [ ] Verified error handling at each boundary
- [ ] Checked data survives round-trip

---

## When to Create Flow Documentation

Create detailed flow docs when:
- Feature spans 3+ layers
- Multiple teams are involved
- Data format is complex
- Feature has caused bugs before
