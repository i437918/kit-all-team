#!/usr/bin/env bash
set -euo pipefail

VERSION=${1:-v0.1.5}
OUTPUT_DIR=${2:-dist}
REPOSITORY_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
DESTINATION="$REPOSITORY_ROOT/$OUTPUT_DIR"
cd "$REPOSITORY_ROOT"
if [[ -n $(git status --porcelain --untracked-files=all) ]]; then
  printf 'SOURCE_TREE_DIRTY\n' >&2
  exit 1
fi
COMMIT=$(git -C "$REPOSITORY_ROOT" rev-parse HEAD)
BUILD_DATE=$(git -C "$REPOSITORY_ROOT" show -s --format=%cI "$COMMIT")

mkdir -p "$DESTINATION"
export CGO_ENABLED=0
LDFLAGS="-s -w -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.version=$VERSION -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.commit=$COMMIT -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.buildDate=$BUILD_DATE"

targets=(
  "windows amd64 teamkit-${VERSION}-windows-amd64.exe"
  "linux amd64 teamkit-${VERSION}-linux-amd64"
  "darwin amd64 teamkit-${VERSION}-darwin-amd64"
  "darwin arm64 teamkit-${VERSION}-darwin-arm64"
)

manifest_artifacts=(
  "teamkit-${VERSION}-windows-amd64.exe"
  "teamkit-${VERSION}-linux-amd64"
  "teamkit-${VERSION}-darwin-amd64"
  "teamkit-${VERSION}-darwin-arm64"
)

for target in "${targets[@]}"; do
  read -r target_os target_arch artifact <<<"$target"
  GOOS=$target_os GOARCH=$target_arch go build \
    -buildvcs=false -trimpath -ldflags "$LDFLAGS" \
    -o "$DESTINATION/$artifact" ./cmd/teamkit
done

(
  cd "$DESTINATION"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${manifest_artifacts[@]}" | LC_ALL=C sort > SHA256SUMS
  else
    shasum -a 256 "${manifest_artifacts[@]}" | LC_ALL=C sort > SHA256SUMS
  fi
)

printf 'Built %s artifacts in %s\n' "${#targets[@]}" "$DESTINATION"
