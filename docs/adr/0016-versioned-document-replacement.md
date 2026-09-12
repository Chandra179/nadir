# 0016 — Versioned failure-safe Document replacement

Date: 2026-09-12

## Status

Accepted

## Context

The Indexing pass previously deleted all points for a Document before writing
the replacement points. A failed or interrupted upsert could therefore leave
the Document absent even though the previous version was still usable.

Retries did not solve the data-loss window because every retry repeated the
delete before attempting the upsert.

## Decision

The Store replacement seam uses the Document's content hash as its version:

1. New points use versioned deterministic IDs and are written with an inactive
   marker.
2. The complete staged version is activated only after the write succeeds.
3. Older versions for the same source identity are marked inactive.
4. Older inactive points are deleted after the new version is active.

The search filters exclude explicitly inactive points. Points written by an
older release without the marker remain readable until their next successful
replacement. A failed staging write leaves the previous active version
available; a failed cleanup leaves inactive historical points that can be
cleaned by a retry. If the transition fails after activation but before old
versions are deactivated, both versions can be visible briefly; retrying
converges to one active version without losing the Document. Repeating the
replacement is safe because versioned IDs and cleanup are deterministic.

## Consequences

The Indexing pass no longer has a delete-before-write data-loss window. A
short-lived duplicate or cleanup backlog is possible after a cleanup failure,
but it does not remove the active Document. Source hashes become part of the
point identity, so the Store seam owns versioning and callers only request a
Document replacement.
