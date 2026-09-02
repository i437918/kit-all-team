#!/usr/bin/env bash
set -euo pipefail

IMAGE=${1:?usage: qualify-alt-officecli-image.sh IMAGE EXACT_LIBRT_NEVRA EXACT_LDD_NEVRA EXACT_ICU_NEVRA OFFICECLI_ASSET OFFICECLI_LIVE_TEST}
ALT_LIBRT_PACKAGE=${2:?exact librt provider NEVRA is required}
ALT_LDD_PACKAGE=${3:?exact ldd provider NEVRA is required}
ALT_ICU_PACKAGE=${4:?exact ICU provider NEVRA is required}
OFFICECLI_ASSET=$(realpath "${5:?OfficeCLI asset is required}")
OFFICECLI_LIVE_TEST=$(realpath "${6:?OfficeCLI live test is required}")

[[ -f "$OFFICECLI_ASSET" ]] || { printf 'OfficeCLI asset not found: %s\n' "$OFFICECLI_ASSET" >&2; exit 2; }
[[ -f "$OFFICECLI_LIVE_TEST" ]] || { printf 'OfficeCLI live test not found: %s\n' "$OFFICECLI_LIVE_TEST" >&2; exit 2; }

docker run --rm \
  --user 1000:1000 \
  --read-only \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --network=none \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,mode=1777 \
  --tmpfs /run/teamkit-officecli:rw,nosuid,nodev,exec,mode=0700,uid=1000,gid=1000 \
  --env "ALT_LIBRT_PACKAGE=$ALT_LIBRT_PACKAGE" \
  --env "ALT_LDD_PACKAGE=$ALT_LDD_PACKAGE" \
  --env "ALT_ICU_PACKAGE=$ALT_ICU_PACKAGE" \
  --env "TEAMKIT_OFFICECLI_ALT_IMAGE=$IMAGE" \
  --mount "type=bind,src=$OFFICECLI_ASSET,dst=/opt/officecli,readonly" \
  --mount "type=bind,src=$OFFICECLI_LIVE_TEST,dst=/opt/officecli-live.test,readonly" \
  "$IMAGE" \
  /bin/bash -lc 'set -euo pipefail
    . /etc/os-release
    test "$ID" = altlinux
    owner="$(rpm -qf /lib64/librt.so.1)"
    owner_nevra="$(rpm -q --qf '\''%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n'\'' "$owner")"
    test "$owner_nevra" = "$ALT_LIBRT_PACKAGE"
    test -x /usr/bin/ldd
    ldd_owner="$(rpm -qf /usr/bin/ldd)"
    ldd_owner_nevra="$(rpm -q --qf '\''%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n'\'' "$ldd_owner")"
    test "$ldd_owner_nevra" = "$ALT_LDD_PACKAGE"
    test -r /usr/lib64/libicuuc.so.74
    test -r /usr/lib64/libicudata.so.74
    icu_owner="$(rpm -qf /usr/lib64/libicuuc.so.74)"
    icu_data_owner="$(rpm -qf /usr/lib64/libicudata.so.74)"
    icu_owner_nevra="$(rpm -q --qf '\''%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n'\'' "$icu_owner")"
    icu_data_owner_nevra="$(rpm -q --qf '\''%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n'\'' "$icu_data_owner")"
    test "$icu_owner_nevra" = "$ALT_ICU_PACKAGE"
    test "$icu_data_owner_nevra" = "$ALT_ICU_PACKAGE"
    /usr/bin/ldd /opt/officecli
    ! /usr/bin/ldd /opt/officecli | grep -F '\''not found'\''
    env TMPDIR=/run/teamkit-officecli TMP=/run/teamkit-officecli TEMP=/run/teamkit-officecli TEAMKIT_OFFICECLI_EXEC_ROOT=/run/teamkit-officecli TEAMKIT_OFFICECLI_EXISTING_PATH=/opt/officecli /opt/officecli-live.test -test.run "^TestOfficeCLILive_QualifiedAssetAndMCPHandshake$" -test.count=1 -test.timeout=3m'

printf 'OFFICECLI_ALT_IMAGE_QUALIFIED image=%s package=%s ldd_package=%s icu_package=%s\n' "$IMAGE" "$ALT_LIBRT_PACKAGE" "$ALT_LDD_PACKAGE" "$ALT_ICU_PACKAGE"
