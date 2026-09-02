#!/usr/bin/env bash
set -euo pipefail

REPOSITORY_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$REPOSITORY_ROOT"

go_files=()
while IFS= read -r go_file; do
  go_files[${#go_files[@]}]="$go_file"
done < <(git ls-files --cached --others --exclude-standard -- '*.go')
if ((${#go_files[@]})); then
  unformatted=$(gofmt -l "${go_files[@]}")
  if [[ -n "$unformatted" ]]; then
    printf 'gofmt required:\n%s\n' "$unformatted" >&2
    exit 1
  fi
fi

go vet ./...
go test ./...
if [[ "${TEAMKIT_SKIP_RACE:-0}" != "1" ]]; then
  go test -race ./...
fi
