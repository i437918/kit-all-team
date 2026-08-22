# GitLab release artifact handoff

## Goal

Make GitLab the sole byte source and release gate after the first successful
exact-SHA GitHub CI. A release publisher must not wait for, dispatch, or download
another GitHub Actions artifact after that handoff.

## Inputs and immutable handoff

The initial successful GitHub CI remains the build and platform-qualification
authority. It exposes immutable provenance only: GitHub run ID, artifact ID,
artifact digest, and exact commit SHA.

A single GitLab handoff job persists the candidate set and required evidence for
that SHA. Its artifact manifest contains exactly the six release filenames, their
SHA256 values, and embedded version/commit identity. The GitLab artifact is kept
before any release mutation.

## Release flow

1. Confirm exactly one successful GitLab handoff/verify job for the release SHA.
2. Download and validate only that GitLab artifact: six filenames, SHA256,
   embedded version and commit, and retained-artifact status.
3. Treat GitHub metadata as provenance only. Do not fetch GitHub artifact bytes,
   use `archive_download_url`, dispatch another CI, or wait for a new GitHub run.
4. Perform final validation by comparing the GitLab handoff manifest with the
   GitLab verify artifact. A mismatch, duplicate, missing item, or ambiguous
   provenance fails closed.
5. Only then publish, in order: generic package, protected annotated tag, GitLab
   Release, and API post-verification.

## Failure behavior

All artifact, hash, identity, retention, package, tag, and Release ambiguities
fail closed before the next mutation. Retrying a GitHub workflow, sleeping for a
GitHub runner, or using an expiring redirect is not a recovery mechanism.

## Tests

Tests must first demonstrate that, after a verified GitLab handoff, the publisher
rejects GitHub byte downloads and new GitHub CI dispatches. They must also cover
missing, duplicate, expired, or hash-mismatched GitLab handoff artifacts.
