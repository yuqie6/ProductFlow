# Backend Audit Checklist

[中文](BACKEND_AUDIT_CHECKLIST.md) | English

This checklist comes from a read-only review of `backend/` on 2026-04-23, ordered by stability first, then security boundaries, then maintainability.

## P0 Stability

- [x] Job creation and enqueue must have idempotent semantics: when an active job already exists, do not enqueue the same job id again.
- [x] Workers must verify job state before execution; duplicate messages must not rerun the same job.
- [x] Job failure and retry semantics must be consistent; the code must not declare Dramatiq retry while swallowing all exceptions in business logic.
- [x] Queue send failure must not silently leave an unexecutable `QUEUED` job behind.

## P0 Input and Resource Boundaries

- [x] Limit single-file size, file count, and MIME type when uploading product images, reference images, or session reference images.
- [x] Uploaded images must be decoded and validated as real images, rejecting forged content types or undecodable files.
- [x] Image size parameters must not rely only on `^\d+x\d+$`; preset buttons are frontend shortcuts, while actual generation entrypoints must uniformly require positive numbers and a single-side safety limit of `3840`.
- [x] Bad inputs for product price, product name, and similar form fields must become 400 responses instead of 500 errors.

## P1 Data and Storage Security

- [x] `LocalStorage.resolve()` must guarantee that the result stays under the storage root, preventing arbitrary file reads caused by dirty DB data.
- [x] Product-list pagination and `page`/`page_size` parameters must be pushed down and limited to avoid full loading.
- [x] Session cookie should support secure configuration in production.
- [x] DB engine should enable connection health checks, such as `pool_pre_ping=True`.

## P1 Migration and Tests

- [x] Tests must not rely only on `Base.metadata.create_all()`; they must cover Alembic `upgrade head`.
- [x] SQLite migration path must either be explicitly unsupported or avoid failing on ALTER constraint operations.
- [x] Clean up or explain duplicate enum migrations so migration history does not keep drifting.

## P2 API / Architecture Cleanup

- [x] Old `/api/image-chat/generate` has been removed; iterative image generation keeps only the persistent `/api/image-sessions` interface.
- [ ] Iterative image generation should not wait synchronously for 180 seconds inside HTTP requests long-term; it should later move to jobs. The current synchronous entrypoint is already under global generation concurrency admission control.
- [x] Building iterative image-generation context should not read all historical images and then discard most of them; it should only read the recent images actually sent to the provider.
- [x] OpenAI text provider should use structured output or stronger JSON parsing error handling.
- [x] DB layer should add business unique constraints: active job, `generated_asset_id`, single main image, and similar invariants.

## Deliberately Deferred Items

- Moving iterative image generation to async jobs changes the frontend interaction protocol and needs to be done together with `/web` polling/loading states. This round keeps the synchronous API, but already limits image size, bounds historical image reads, and reduces resource risk through a global generation concurrency limit.
