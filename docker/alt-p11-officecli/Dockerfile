FROM registry.altlinux.org/p11/alt@sha256:4c76520bb4935edf624dde76d5e670d54f40938323b185c4c7270881b71fd8ea

# Publication supplies evidence-backed provider NEVRAs and exact apt-rpm
# selectors for the userspace libraries required by the immutable runtime
# proof.
ARG ALT_LIBRT_PACKAGE
ARG ALT_LIBRT_APT_SPEC
ARG ALT_LDD_PACKAGE
ARG ALT_LDD_APT_SPEC
ARG ALT_ICU_PACKAGE
ARG ALT_ICU_APT_SPEC

RUN set -eux; \
    test -n "$ALT_LIBRT_PACKAGE"; \
    case "$ALT_LIBRT_PACKAGE" in *-*:*) ;; *) echo 'ALT_LIBRT_PACKAGE must be an exact NEVRA' >&2; exit 64 ;; esac; \
    test -n "$ALT_LDD_PACKAGE"; \
    case "$ALT_LDD_PACKAGE" in *-*:*) ;; *) echo 'ALT_LDD_PACKAGE must be an exact NEVRA' >&2; exit 64 ;; esac; \
    test -n "$ALT_ICU_PACKAGE"; \
    case "$ALT_ICU_PACKAGE" in *-*:*) ;; *) echo 'ALT_ICU_PACKAGE must be an exact NEVRA' >&2; exit 64 ;; esac; \
    apt-get update; \
    test -n "$ALT_LIBRT_APT_SPEC"; \
    [[ "$ALT_LIBRT_APT_SPEC" =~ ^[A-Za-z0-9][A-Za-z0-9+._-]*=[^[:space:]=]+$ ]]; \
    test -n "$ALT_LDD_APT_SPEC"; \
    [[ "$ALT_LDD_APT_SPEC" =~ ^[A-Za-z0-9][A-Za-z0-9+._-]*=[^[:space:]=]+$ ]]; \
    test -n "$ALT_ICU_APT_SPEC"; \
    [[ "$ALT_ICU_APT_SPEC" =~ ^[A-Za-z0-9][A-Za-z0-9+._-]*=[^[:space:]=]+$ ]]; \
    apt-get install -y "$ALT_LIBRT_APT_SPEC" "$ALT_LDD_APT_SPEC" "$ALT_ICU_APT_SPEC"; \
    exact_nevra="$(rpm -q --qf '%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n' "$ALT_LIBRT_PACKAGE")"; \
    test "$exact_nevra" = "$ALT_LIBRT_PACKAGE"; \
    owner="$(rpm -qf /lib64/librt.so.1)"; \
    exact_ldd_nevra="$(rpm -q --qf '%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n' "$ALT_LDD_PACKAGE")"; \
    test "$exact_ldd_nevra" = "$ALT_LDD_PACKAGE"; \
    owner_nevra="$(rpm -q --qf '%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n' "$owner")"; \
    test "$owner_nevra" = "$ALT_LIBRT_PACKAGE"; \
    ldd_owner="$(rpm -qf /usr/bin/ldd)"; \
    ldd_owner_nevra="$(rpm -q --qf '%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n' "$ldd_owner")"; \
    test "$ldd_owner_nevra" = "$ALT_LDD_PACKAGE"; \
    exact_icu_nevra="$(rpm -q --qf '%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n' "$ALT_ICU_PACKAGE")"; \
    test "$exact_icu_nevra" = "$ALT_ICU_PACKAGE"; \
    test -r /usr/lib64/libicuuc.so.74; \
    test -r /usr/lib64/libicudata.so.74; \
    icu_owner="$(rpm -qf /usr/lib64/libicuuc.so.74)"; \
    icu_data_owner="$(rpm -qf /usr/lib64/libicudata.so.74)"; \
    icu_owner_nevra="$(rpm -q --qf '%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n' "$icu_owner")"; \
    icu_data_owner_nevra="$(rpm -q --qf '%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n' "$icu_data_owner")"; \
    test "$icu_owner_nevra" = "$ALT_ICU_PACKAGE"; \
    test "$icu_data_owner_nevra" = "$ALT_ICU_PACKAGE"; \
    rm -rf /var/cache/apt/archives/* /var/lib/apt/lists/*
