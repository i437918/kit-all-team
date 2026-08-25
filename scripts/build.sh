#!/usr/bin/env bash
set -euo pipefail

VERSION=${1:-v0.1.6}
OUTPUT_DIR=${2:-dist}
REPOSITORY_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
DESTINATION="$REPOSITORY_ROOT/$OUTPUT_DIR"
cd "$REPOSITORY_ROOT"
if [[ -n $(git status --porcelain --untracked-files=all) ]]; then
  printf 'SOURCE_TREE_DIRTY\n' >&2
  exit 1
fi
validate_rfc3339() {
  local timestamp=$1 year month day hour minute second zone
  local year_number month_number day_number hour_number minute_number second_number zone_hour zone_minute maximum_day

  if ! [[ $timestamp =~ ^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$ ]]; then
    return 1
  fi
  year=${BASH_REMATCH[1]}
  month=${BASH_REMATCH[2]}
  day=${BASH_REMATCH[3]}
  hour=${BASH_REMATCH[4]}
  minute=${BASH_REMATCH[5]}
  second=${BASH_REMATCH[6]}
  zone=${BASH_REMATCH[8]}

  year_number=$((10#$year))
  month_number=$((10#$month))
  day_number=$((10#$day))
  hour_number=$((10#$hour))
  minute_number=$((10#$minute))
  second_number=$((10#$second))
  (( month_number >= 1 && month_number <= 12 && hour_number <= 23 && minute_number <= 59 && second_number <= 59 )) || return 1

  case $month_number in
    1|3|5|7|8|10|12) maximum_day=31 ;;
    4|6|9|11) maximum_day=30 ;;
    2)
      maximum_day=28
      if (( year_number % 400 == 0 || (year_number % 4 == 0 && year_number % 100 != 0) )); then
        maximum_day=29
      fi
      ;;
  esac
  (( day_number >= 1 && day_number <= maximum_day )) || return 1

  if [[ $zone != Z ]]; then
    zone_hour=$((10#${zone:1:2}))
    zone_minute=$((10#${zone:4:2}))
    (( zone_hour <= 23 && zone_minute <= 59 )) || return 1
  fi
}

SOURCE_REVISION=${TEAMKIT_SOURCE_REVISION:-}
SOURCE_COMMIT_TIME=${TEAMKIT_SOURCE_COMMIT_TIME:-}
if [[ -n $SOURCE_REVISION || -n $SOURCE_COMMIT_TIME ]]; then
  if [[ -z $SOURCE_REVISION || -z $SOURCE_COMMIT_TIME ]] ||
    ! [[ $SOURCE_REVISION =~ ^[0-9a-f]{40}$ ]] ||
    ! validate_rfc3339 "$SOURCE_COMMIT_TIME"; then
    printf 'SOURCE_IDENTITY_INVALID\n' >&2
    exit 1
  fi
  COMMIT=$SOURCE_REVISION
  BUILD_DATE=$SOURCE_COMMIT_TIME
else
  COMMIT=$(git -C "$REPOSITORY_ROOT" rev-parse HEAD)
  BUILD_DATE=$(git -C "$REPOSITORY_ROOT" show -s --format=%cI "$COMMIT")
fi

mkdir -p "$DESTINATION"
export CGO_ENABLED=0
IDENTITY="teamkit-build-identity-v1:${VERSION}:${COMMIT}"
LDFLAGS="-s -w -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.version=$VERSION -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.commit=$COMMIT -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.buildDate=$BUILD_DATE -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.identity=$IDENTITY"

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
