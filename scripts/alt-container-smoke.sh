#!/usr/bin/env bash
set -euo pipefail

ALT_BASE_IMAGE=registry.altlinux.org/p11/alt@sha256:4c76520bb4935edf624dde76d5e670d54f40938323b185c4c7270881b71fd8ea
ALT_OFFICECLI_IMAGE=ghcr.io/i437918/kit-all-team/alt-p11-officecli@sha256:5ee493c6c7edbdb8d68fb0ab9af2847bae855c9042bc5f13f5fd6b3d0965a825
ALT_IMAGE=${ALT_IMAGE:-$ALT_BASE_IMAGE}
CANDIDATE=${1:?usage: alt-container-smoke.sh PATH_TO_LINUX_AMD64_BINARY [OFFICECLI_ASSET OFFICECLI_LIVE_TEST]}
OFFICECLI_ASSET=${2:-}
OFFICECLI_LIVE_TEST=${3:-}
if [[ -n "$OFFICECLI_ASSET" && -z "$OFFICECLI_LIVE_TEST" ]] || [[ -z "$OFFICECLI_ASSET" && -n "$OFFICECLI_LIVE_TEST" ]]; then
  printf 'OFFICECLI_LIVE_EVIDENCE_PAIR_REQUIRED\n' >&2
  exit 2
fi
CANDIDATE=$(realpath "$CANDIDATE")
[[ -f "$CANDIDATE" ]] || { printf 'candidate not found: %s\n' "$CANDIDATE" >&2; exit 2; }

if [[ -n "$OFFICECLI_ASSET" ]]; then
  OFFICECLI_ASSET=$(realpath "$OFFICECLI_ASSET")
  OFFICECLI_LIVE_TEST=$(realpath "$OFFICECLI_LIVE_TEST")
  [[ -f "$OFFICECLI_ASSET" ]] || { printf 'OfficeCLI asset not found: %s\n' "$OFFICECLI_ASSET" >&2; exit 2; }
  [[ -f "$OFFICECLI_LIVE_TEST" ]] || { printf 'OfficeCLI live test not found: %s\n' "$OFFICECLI_LIVE_TEST" >&2; exit 2; }
fi

if [[ -n "$OFFICECLI_ASSET" && "$ALT_IMAGE" != "$ALT_OFFICECLI_IMAGE" ]]; then
  printf 'ALT_IMAGE_PIN_MISMATCH expected=%s actual=%s\n' "$ALT_OFFICECLI_IMAGE" "$ALT_IMAGE" >&2
  exit 64
fi

docker pull "$ALT_IMAGE"
docker image inspect "$ALT_IMAGE" --format '{{json .RepoDigests}}'
docker run --rm \
  --user 1000:1000 \
  --read-only \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --network=none \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,mode=1777 \
  --mount "type=bind,src=$CANDIDATE,dst=/opt/teamkit,readonly" \
  "$ALT_IMAGE" \
  /bin/bash -lc 'set -euo pipefail; . /etc/os-release; test "$ID" = altlinux; /opt/teamkit --version'

printf 'ALT_USERSPACE_COMPATIBLE image=%s\n' "$ALT_IMAGE"

if [[ -n "$OFFICECLI_ASSET" ]]; then
  TEAMKIT_OFFICECLI_ALT_LIBRT_PACKAGE=${TEAMKIT_OFFICECLI_ALT_LIBRT_PACKAGE:?TEAMKIT_OFFICECLI_ALT_LIBRT_PACKAGE is required for OfficeCLI ALT qualification}
  TEAMKIT_OFFICECLI_ALT_LDD_PACKAGE=${TEAMKIT_OFFICECLI_ALT_LDD_PACKAGE:?TEAMKIT_OFFICECLI_ALT_LDD_PACKAGE is required for OfficeCLI ALT qualification}
  TEAMKIT_OFFICECLI_ALT_ICU_PACKAGE=${TEAMKIT_OFFICECLI_ALT_ICU_PACKAGE:?TEAMKIT_OFFICECLI_ALT_ICU_PACKAGE is required for OfficeCLI ALT qualification}
  docker run --rm \
    --user 1000:1000 \
    --read-only \
    --cap-drop=ALL \
    --security-opt=no-new-privileges \
    --network=none \
    --tmpfs /tmp:rw,nosuid,nodev,noexec,mode=1777 \
    --tmpfs /run/teamkit-officecli:rw,nosuid,nodev,exec,mode=0700,uid=1000,gid=1000 \
    --env "TEAMKIT_OFFICECLI_ALT_IMAGE=$ALT_IMAGE" \
    --env "TEAMKIT_OFFICECLI_ALT_LIBRT_PACKAGE=$TEAMKIT_OFFICECLI_ALT_LIBRT_PACKAGE" \
    --mount "type=bind,src=$OFFICECLI_ASSET,dst=/opt/officecli,readonly" \
    --env "TEAMKIT_OFFICECLI_ALT_LDD_PACKAGE=$TEAMKIT_OFFICECLI_ALT_LDD_PACKAGE" \
    --env "TEAMKIT_OFFICECLI_ALT_ICU_PACKAGE=$TEAMKIT_OFFICECLI_ALT_ICU_PACKAGE" \
    --mount "type=bind,src=$OFFICECLI_LIVE_TEST,dst=/opt/officecli-live.test,readonly" \
    "$ALT_IMAGE" \
    /bin/bash -lc 'set -euo pipefail; . /etc/os-release; test "$ID" = altlinux; test -x /opt/officecli; test -x /opt/officecli-live.test; run_officecli_live() { env TMPDIR=/run/teamkit-officecli TMP=/run/teamkit-officecli TEMP=/run/teamkit-officecli TEAMKIT_OFFICECLI_EXEC_ROOT=/run/teamkit-officecli TEAMKIT_OFFICECLI_EXISTING_PATH=/opt/officecli "$@" /opt/officecli-live.test -test.run "^TestOfficeCLILive_QualifiedAssetAndMCPHandshake$" -test.count=1 -test.timeout=3m; }; test -r /lib64/librt.so.1; owner="$(rpm -qf /lib64/librt.so.1)"; owner_nevra="$(rpm -q --qf '\''%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n'\'' "$owner")"; test "$owner_nevra" = "$TEAMKIT_OFFICECLI_ALT_LIBRT_PACKAGE"; test -x /usr/bin/ldd; ldd_owner="$(rpm -qf /usr/bin/ldd)"; ldd_owner_nevra="$(rpm -q --qf '\''%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n'\'' "$ldd_owner")"; test "$ldd_owner_nevra" = "$TEAMKIT_OFFICECLI_ALT_LDD_PACKAGE"; test -r /usr/lib64/libicuuc.so.74; test -r /usr/lib64/libicudata.so.74; icu_owner="$(rpm -qf /usr/lib64/libicuuc.so.74)"; icu_data_owner="$(rpm -qf /usr/lib64/libicudata.so.74)"; icu_owner_nevra="$(rpm -q --qf '\''%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n'\'' "$icu_owner")"; icu_data_owner_nevra="$(rpm -q --qf '\''%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n'\'' "$icu_data_owner")"; test "$icu_owner_nevra" = "$TEAMKIT_OFFICECLI_ALT_ICU_PACKAGE"; test "$icu_data_owner_nevra" = "$TEAMKIT_OFFICECLI_ALT_ICU_PACKAGE"; /usr/bin/ldd /opt/officecli; ! /usr/bin/ldd /opt/officecli | grep -F "not found"; set +e; run_officecli_live; officecli_status=$?; set -e; if [[ "$officecli_status" -ne 0 ]]; then set +e; run_officecli_live TEAMKIT_OFFICECLI_ALT_DIAGNOSTICS=stderr-stage-v1; diagnostic_status=$?; set -e; printf "OFFICECLI_ALT_DIAGNOSTIC_COMPLETE primary_status=%s diagnostic_status=%s\\n" "$officecli_status" "$diagnostic_status" >&2; exit "$officecli_status"; fi'

  printf 'OFFICECLI_ALT_USERSPACE_COMPATIBLE image=%s\n' "$ALT_IMAGE"
fi
