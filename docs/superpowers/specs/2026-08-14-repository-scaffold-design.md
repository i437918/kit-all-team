# Repository Scaffold Design

> **Superseded on 2026-08-14.** This Windows-only PowerShell scaffold was replaced by
> `2026-08-14-cross-platform-go-teamkit-design.md`. It remains only as design history.

## Goal

Prepare a complete engineering scaffold for implementing 1C Team Kit v1.2.0 from the approved technical specification, without inventing runtime behavior, catalogs, hashes, manifests, or vendored payloads.

## Repository Shape

The repository will expose the product boundaries directly through top-level directories: `roles/`, `skills/`, `projects/`, `capabilities/`, `onboarding/`, `schemas/`, `scripts/`, `tests/`, `docs/`, and `vendor/`. Areas that do not yet contain implementation files will have a short local `README.md` describing their ownership and constraints.

## Governance Files

- `AGENTS.md` will be the concise contributor guide and will define Windows-only compatibility, PowerShell 5.1 requirements, naming, tests, security controls, and review expectations.
- `README.md` will describe the product, its scope boundaries, target platform, intended repository layout, and development status.
- `.gitignore` will exclude generated archives, distribution output, caches, credentials, local runtime state, databases, and IDE artifacts.
- `.editorconfig` will standardize UTF-8 text, line endings, indentation, and PowerShell/Python/JSON/YAML formatting defaults.

## Project Documentation

The initial `docs/` material will include a phased implementation roadmap, a development guide, and an ADR template. The roadmap will follow the specification's vertical slices: closed selectors, bootstrap and handoff, workspace publication, application finalization, MCP parity, then deterministic release tooling.

## Safety and Scope

The scaffold will not contain credentials, repository clones, project databases, release artifacts, or fabricated supply-chain metadata. Catalogs, schemas, scripts, tests, manifests, and vendor payloads will be implemented only in later reviewed slices. Shared areas must not contain project-specific business facts, memory, tasks, or database content.

## Verification

Verification will check that required paths and governance files exist, Markdown contains no placeholders, ignored sensitive/generated paths are covered, and `AGENTS.md` remains within the requested 200–400-word range. Git status will be reviewed to ensure only scaffold files are introduced.
