#!/usr/bin/env bash
set -euo pipefail

digests=()
while IFS= read -r digest; do
  digests+=("$digest")
done < <(sed -nE 's/^[^[:space:]]+: digest: (sha256:[0-9a-f]{64}) size: [0-9]+$/\1/p')

if [[ ${#digests[@]} -ne 1 ]]; then
  printf 'DOCKER_PUSH_DIGEST_RECEIPT_INVALID count=%s\n' "${#digests[@]}" >&2
  exit 64
fi
printf '%s\n' "${digests[0]}"
