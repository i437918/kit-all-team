#!/usr/bin/env bash
set -euo pipefail

TEAMKIT_GO_VERSION=1.26.6
TEAMKIT_GO_PLATFORM=linux-amd64
TEAMKIT_GO_SHA256=708effb774be8237570d0add163225abbdfaf4fca28b2611df167beba4feef89
TEAMKIT_GO_ARCHIVE="go${TEAMKIT_GO_VERSION}.${TEAMKIT_GO_PLATFORM}.tar.gz"
TEAMKIT_GO_CACHE_DIR=${TEAMKIT_GO_CACHE_DIR:-"${CI_PROJECT_DIR:-$PWD}/.cache/teamkit-go"}
TEAMKIT_GO_VERSION_DIR="$TEAMKIT_GO_CACHE_DIR/go${TEAMKIT_GO_VERSION}-${TEAMKIT_GO_PLATFORM}"
TEAMKIT_GO_BIN="$TEAMKIT_GO_VERSION_DIR/go/bin/go"
TEAMKIT_GO_EXPECTED="go version go${TEAMKIT_GO_VERSION} linux/amd64"

if command -v go >/dev/null 2>&1 && [[ $(go version) == "$TEAMKIT_GO_EXPECTED" ]]; then
  return 0 2>/dev/null || exit 0
fi

if [[ ! -x "$TEAMKIT_GO_BIN" ]]; then
  mkdir -p "$TEAMKIT_GO_CACHE_DIR"
  archive="$TEAMKIT_GO_CACHE_DIR/$TEAMKIT_GO_ARCHIVE"
  if [[ ! -f "$archive" ]]; then
    curl --fail --location --ipv4 --retry 3 --connect-timeout 15 --max-time 300 \
      --output "$archive" "https://go.dev/dl/$TEAMKIT_GO_ARCHIVE"
  fi
  printf '%s  %s\n' "$TEAMKIT_GO_SHA256" "$archive" | sha256sum --check --status

  if [[ -e "$TEAMKIT_GO_VERSION_DIR" ]]; then
    printf 'GO_TOOLCHAIN_CACHE_INVALID: %s\n' "$TEAMKIT_GO_VERSION_DIR" >&2
    return 1 2>/dev/null || exit 1
  fi
  staging=$(mktemp -d "$TEAMKIT_GO_CACHE_DIR/.extract.XXXXXX")
  if ! tar -xzf "$archive" -C "$staging"; then
    rm -rf -- "$staging"
    return 1 2>/dev/null || exit 1
  fi
  mv -- "$staging" "$TEAMKIT_GO_VERSION_DIR"
fi

if [[ $($TEAMKIT_GO_BIN version) != "$TEAMKIT_GO_EXPECTED" ]]; then
  printf 'GO_TOOLCHAIN_VERSION_MISMATCH\n' >&2
  return 1 2>/dev/null || exit 1
fi

export PATH="$(dirname "$TEAMKIT_GO_BIN"):$PATH"
hash -r
