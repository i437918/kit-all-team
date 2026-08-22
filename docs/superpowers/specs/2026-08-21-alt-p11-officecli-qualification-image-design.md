# Pinned ALT p11 OfficeCLI Qualification Image

## Context and goal

Exact-SHA run `32498652350` proved that OfficeCLI `v1.0.144` cannot start in
the current pinned minimal ALT p11 image because the loader cannot find
`librt.so.1`. Native lanes work, so this is an ALT qualification-environment
dependency gap. The gate remains fail closed: do not ignore the provisioner
error, mount host libraries, create symlinks, or install packages during smoke.

Publish a private reproducible ALT p11 qualification image containing the
official ALT package that provides `librt.so.1`, pin it by registry digest, and
run the unchanged qualified OfficeCLI bytes under the existing restrictions.

## Registry and immutable inputs

Publish privately to
`ghcr.io/dmitry-m1man/kit-all-team/alt-p11-officecli`. Publication uses minimum
`packages: write`; qualification uses `packages: read`. Credentials never enter
the image, build arguments, repository, references, or logs. Mutable tags are
operator conveniences only; CI consumes an exact `sha256:` digest.

The base remains
`registry.altlinux.org/p11/alt@sha256:4c76520bb4935edf624dde76d5e670d54f40938323b185c4c7270881b71fd8ea`.
Package identity is discovered, not guessed. A bounded discovery build installs
the candidate official ALT package and proves ownership with `rpm -qf` on
`librt.so.1`. It records exact NEVRA, architecture, base digest, and file path.
If `glibc-pthread` is not the owner, only the actual owner is accepted.

The production Dockerfile installs that package by exact version. Unversioned
installs, third-party RPMs, copied objects, host mounts, compatibility symlinks,
and rebuilt glibc are forbidden. Package metadata is removed after installation.

## Build and publication flow

A manual GitHub workflow bound to an explicit source SHA must verify checkout,
build from the exact base, prove RPM ownership, verify OfficeCLI size/SHA-256,
reject unresolved ELF dependencies, run the full production live test, publish
only after checks pass, and report the immutable digest without rewriting source.
It creates no Team Kit tag, release, or release asset.

## Runtime boundary

`scripts/alt-container-smoke.sh` remains non-root `1000:1000`, read-only,
capability-free, `no-new-privileges`, network-disabled, with bounded tmpfs and
read-only exact binaries. Before the live test it verifies RPM ownership and no
unresolved OfficeCLI dependency. Runtime package installation is forbidden.

`OFFICECLI_ALT_USERSPACE_COMPATIBLE` applies only to the pinned userspace image;
it does not claim native ALT, VM, QEMU, or arbitrary-installation compatibility.

## Repository changes

Add a focused Dockerfile and manual publication workflow. Update CI with the
resulting digest and read-only package access, extend the smoke with fail-fast
checks, and add release contract and documentation tests. Do not change Team
Kit provisioning or OfficeCLI bytes.

## Testing and delivery

Use red-green-refactor. Tests first reject a missing Dockerfile, mutable pin,
unversioned install, runtime installation, absent dependency check, or weakened
isolation. Run targeted tests, `go test ./...`, `go vet ./...`, both builds, and
`git diff --check`. Independently review provenance, digest binding, secrets,
isolation, and evidence semantics. Bind the published digest in a separate
commit, review again, and repeat exact-SHA GitLab/GitHub CI.

## Failure handling and acceptance

Missing ownership, exact-version failure, unresolved dependencies, ambiguous
runs, missing or mismatched digest, or any failed lane blocks qualification.
Retry may not change the base digest, package version, OfficeCLI asset, or
candidate SHA.

Completion requires exact package ownership, a successful unchanged live test,
an immutable repository pin, no unresolved Critical or Important review
findings, and green exact-SHA CI across build/security, Windows, Linux, both
macOS architectures, and ALT p11.

