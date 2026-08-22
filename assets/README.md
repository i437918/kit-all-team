# Runtime Payload Policy

`payloads.json` is the non-secret allowlist for externally supplied installers,
certificate archives, toolchains, OfficeCLI assets, and ALT images. Team Kit must
verify the listed identity before use. Large or organization-private payloads are
not committed to Git; a release bundle may place them beside the executable after
verification.

The `officeCLI` object mirrors the qualified OfficeCLI source-only pin: version
`1.0.144`, commit `1ced45e900782c5083ed550ddf328ee974e425e7`, and exactly four
release assets. Each asset records its native OS, architecture, file name, exact
HTTPS release URL, byte size, and SHA-256. The selection must use this exact pin;
`latest` and unrecorded upstream releases are not permitted. ALT Linux uses the
Linux amd64 asset.
