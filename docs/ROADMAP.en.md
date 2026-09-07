# ProductFlow Roadmap

Updated 2026-09-07. This is an English summary of the [authoritative product direction and release design](ROADMAP.md), which contains the detailed competitor sources, acceptance contracts and decisions. Current capabilities remain in [PRD](PRD.en.md) and [Architecture](ARCHITECTURE.en.md). The requirements below are planned, not delivered features.

## Product and Stage

The intended product is a self-hostable, multi-merchant SaaS. The project owner will also operate an instance of the same product. Self-hosting describes deployment control; multiple merchants describe application isolation. The unreleased development baseline starts with one administrator and one bootstrap development merchant; public email registration and Operator SMTP configuration are implemented and have passed real-browser and real SMTP/IMAP mail-receipt verification. Full multi-merchant isolation and workspace UX are not implemented.

The primary job is a complete usable product-image delivery: organize supported facts, plan a set, generate, revise, select, export and reuse approved work for the next product. Agent, Graph, providers and self-evolution support this outcome.

The account entry is public email-code registration after deployer bootstrap. The Operator configures `smtp_host`, `smtp_port`, `smtp_security` (`starttls` or `tls`), `smtp_username`, `smtp_password`, `smtp_from_address`, and `smtp_from_name` in the existing settings surface. The Register mode asks for an email, verification code, password, and merchant name; the optional API `display_name` defaults to the email prefix. A verified six-digit code creates an ordinary User, that user's own Merchant, an Owner membership, and trial quota; the code is valid for 10 minutes, resends wait 60 seconds, and each challenge allows five failed attempts. Existing password login remains. A real-browser flow has verified SMTP sending, IMAP receipt, registration into `/products`, an ordinary User session with its own Merchant and Owner membership, rejection of the old code replay with 410, and password login. This evidence covers the registration slice; whole-site publication and password recovery remain separate unfinished capabilities. Referrals and rebates are out of scope for this phase and remain a future decision.

## Competitive Direction

The Chinese roadmap records dated official sources for Designkit, 51aic, Gaoding, Lovart, Photoroom, PicCopilot, Huiwa, Canva and ComfyUI. The investigation covers public interfaces, help/API documentation and public technical material. Paid same-task generation comparisons were not performed, and private competitor implementation details are unknown.

Sets, Agent assistance, local editing and workflow reuse already appear across competitors. ProductFlow must provide straightforward creation, result inspection, editing, comparison, selection, export and reuse. Potential differentiation is the combined effect of source-backed product facts, controlled revisions, deterministic text/layout where precision matters, and reusable approved visual decisions. These remain hypotheses until complete-task comparisons show less rework and better usable output.

Existing recipe preview, atomic creation, idempotency, source-identity stripping, local edits and ZIP delivery should be reused. An outcome view is a projection of the same Graph; it must cover ungrouped image nodes and multiple outputs per group. The existing canvas remains the default until task observations justify a change.

## Multi-Merchant and Commercial Requirements

Merchant is the tenant. Users access merchants through memberships; brands, products and delivery channels belong within that boundary. Planned roles are merchant Owner, Editor and Viewer, plus a separate site Operator. One user can belong to multiple merchants.

Isolation must cover HTTP/use cases, resource references, media variants, exports, queues, retries, SSE, browser caches, Agent contracts and internal callbacks. A payload merchant ID or internal bearer token cannot grant access. Switching merchants does not reassign existing tasks. Partial implementation must not expose a second mutually untrusted merchant.

The first supported topology is a single-host Compose deployment with PostgreSQL, Redis, local media and the current Go/Node/Web services. Providers are configured by the site Operator; deployment-level BYOK is supported by this design, while merchant-specific keys are a later extension.

Usage facts, estimated provider cost and commercial credits/payments are separate records. Every billable entry needs merchant attribution, idempotent reservation and settlement, and explicit handling of unknown outcomes. Public-registration trials can use the transaction-created trial quota. Public paid operation additionally requires payments, refunds, reconciliation and actual operating decisions.

From the first stable release, installation, consistent backups, restoration and supported upgrades become product responsibilities. This does not introduce migration support for retired V1/v2 or historical experimental data. Self-hosted deployments do not send business telemetry to the project operator by default; external model calls still have explicit data destinations.

## Release Sequence

1. Freeze complete tenant coverage and implementation slices; deliver the outcome-view increment and an evidence-based installation/restoration gap inventory.
2. Deliver an internal two-merchant build with complete identity, resource, media, event, asynchronous and Agent isolation, plus accountable usage and resource limits.
3. Deliver public-registration trial workflows: complete image sets, controlled edits, selected delivery snapshots, basic precise text/layout and merchant-scoped visual reuse, with applicable quality and recovery evidence.
4. Ship a stable self-hosted release with fixed artifacts, supported configuration and installation/restoration/upgrade evidence. The operated site uses the same artifacts.
5. Add public paid operation based on actual trial feedback, pricing and payment decisions. Expand batch production, channels and specialist features only with supporting demand, quality and cost evidence.

Release gates cover isolation, common operations, image quality, Agent behavior, credits/execution and installation/recovery. Existing IMG and G-06/G-07 contracts stay in force. Historical runs do not pass a new candidate; development fixtures do not represent customers or revenue.

## Organization and Existing Work

Six groups own independently verifiable outcomes: Merchant Platform, Workflow Experience, Image Quality, Platform Reliability, Agent Quality and Agent Self-Evolution. The [group index](audits/README.md) owns responsibilities and capacity; the [shared board](audits/tasks/README.md) owns claims and exact task state. Roles without an actual assignee remain unfilled. No growth group is created for nonexistent merchant operations.

Self-evolution retains automatic discovery, causal validation, instruction/code candidates, bounded iterations and one final user approval. It starts from a valid frozen development package without waiting for production feedback or every evaluation layer. Hidden evaluation material, authorization and spending boundaries remain outside candidate modification.

Existing evaluation IDs, frozen thresholds, candidates and evidence are preserved. Production mining is scheduled after an actual authorized commercial environment exists. New paid experiments still require their own applicable authorization.
