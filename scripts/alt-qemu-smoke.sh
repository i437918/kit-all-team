#!/usr/bin/env bash
set -euo pipefail

ALT_QEMU_URL=${ALT_QEMU_URL:-https://download.basealt.ru/pub/distributions/ALTLinux/images/p11/cloud/x86_64/alt-p11-cloud-x86_64.qcow2}
ALT_QEMU_SHA256=${ALT_QEMU_SHA256:-d5db0d26addcd2fceed5f045d8eb7f3227afb29e6ec4986bd2edf2addfada1ee}
ALT_QEMU_MANIFEST_URL=${ALT_QEMU_MANIFEST_URL:-https://download.basealt.ru/pub/distributions/ALTLinux/images/p11/cloud/x86_64/SHA256SUM}
ALT_QEMU_SIGNATURE_URL=${ALT_QEMU_SIGNATURE_URL:-https://download.basealt.ru/pub/distributions/ALTLinux/images/p11/cloud/x86_64/SHA256SUM.asc}
ALT_QEMU_SIGNING_FINGERPRINT=17F112840DE94827C9C109FD3E2B30EA57EF33CE
ALT_QEMU_SIGNING_KEY=${ALT_QEMU_SIGNING_KEY:-assets/alt-cloud-signing-key.asc}
CANDIDATE=${1:?usage: alt-qemu-smoke.sh PATH_TO_LINUX_AMD64_BINARY [EVIDENCE_DIR]}
EVIDENCE_DIR=${2:-dist/evidence/alt-qemu}
CACHE_DIR=${ALT_QEMU_CACHE_DIR:-.cache/alt-qemu}
CANDIDATE_ARTIFACT_DIGEST=${CANDIDATE_ARTIFACT_DIGEST:?candidate artifact digest is required}
LIFECYCLE_SCRIPT=${LIFECYCLE_SCRIPT:-scripts/artifact-lifecycle-smoke.sh}

[[ "$CANDIDATE_ARTIFACT_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || { printf 'candidate artifact digest is invalid\n' >&2; exit 2; }

for command_name in curl gpg sha256sum qemu-system-x86_64 qemu-img cloud-localds ssh scp ssh-keygen; do
  command -v "$command_name" >/dev/null 2>&1 || { printf 'ALT_QEMU_UNAVAILABLE missing=%s\n' "$command_name" >&2; exit 78; }
done
[[ -f "$CANDIDATE" ]] || { printf 'candidate not found: %s\n' "$CANDIDATE" >&2; exit 2; }
[[ -f "$LIFECYCLE_SCRIPT" ]] || { printf 'lifecycle script not found: %s\n' "$LIFECYCLE_SCRIPT" >&2; exit 2; }

mkdir -p "$CACHE_DIR" "$EVIDENCE_DIR"
[[ -f "$ALT_QEMU_SIGNING_KEY" ]] || { printf 'ALT_QEMU_SIGNING_KEY_MISSING\n' >&2; exit 78; }
WORK=$(mktemp -d)
QEMU_PID=""
cleanup() {
  if [[ -n "$QEMU_PID" ]] && kill -0 "$QEMU_PID" 2>/dev/null; then
    kill "$QEMU_PID" || true
    wait "$QEMU_PID" 2>/dev/null || true
  fi
  rm -rf -- "$WORK"
}
trap cleanup EXIT

curl --fail --location --retry 3 --output "$WORK/SHA256SUM" "$ALT_QEMU_MANIFEST_URL"
curl --fail --location --retry 3 --output "$WORK/SHA256SUM.asc" "$ALT_QEMU_SIGNATURE_URL"
mkdir -m 700 "$WORK/gnupg"
gpg --batch --homedir "$WORK/gnupg" --import "$ALT_QEMU_SIGNING_KEY" >/dev/null 2>&1
ACTUAL_FINGERPRINT=$(gpg --batch --homedir "$WORK/gnupg" --with-colons --fingerprint "$ALT_QEMU_SIGNING_FINGERPRINT" | awk -F: '$1 == "fpr" { print $10; exit }')
[[ "$ACTUAL_FINGERPRINT" == "$ALT_QEMU_SIGNING_FINGERPRINT" ]] || { printf 'ALT_QEMU_SIGNING_FINGERPRINT_MISMATCH\n' >&2; exit 1; }
gpg --batch --homedir "$WORK/gnupg" --status-fd 1 --verify "$WORK/SHA256SUM.asc" "$WORK/SHA256SUM" 2>/dev/null | tee "$EVIDENCE_DIR/checksum-signature.log"
grep -F "[GNUPG:] VALIDSIG $ALT_QEMU_SIGNING_FINGERPRINT " "$EVIDENCE_DIR/checksum-signature.log" >/dev/null
MANIFEST_SHA256=$(awk '$2 == "alt-p11-cloud-x86_64.qcow2" || $2 == "*alt-p11-cloud-x86_64.qcow2" { print $1 }' "$WORK/SHA256SUM")
[[ "$MANIFEST_SHA256" == "$ALT_QEMU_SHA256" ]] || { printf 'ALT_QEMU_MANIFEST_HASH_MISMATCH\n' >&2; exit 1; }
mkdir -p "$EVIDENCE_DIR/image-verification"
chmod 700 "$EVIDENCE_DIR/image-verification"
cp -- "$WORK/SHA256SUM" "$EVIDENCE_DIR/image-verification/SHA256SUM"
cp -- "$WORK/SHA256SUM.asc" "$EVIDENCE_DIR/image-verification/SHA256SUM.asc"
chmod 600 "$EVIDENCE_DIR/image-verification/SHA256SUM" "$EVIDENCE_DIR/image-verification/SHA256SUM.asc"

IMAGE="$CACHE_DIR/alt-p11-cloud-x86_64.qcow2"
if [[ ! -f "$IMAGE" ]]; then
  curl --fail --location --retry 3 --output "$IMAGE.part" "$ALT_QEMU_URL"
  mv "$IMAGE.part" "$IMAGE"
fi
IMAGE=$(realpath "$IMAGE")
printf '%s  %s\n' "$ALT_QEMU_SHA256" "$IMAGE" | sha256sum --check --strict

ssh-keygen -q -t ed25519 -N '' -f "$WORK/id_ed25519"
PUBLIC_KEY=$(<"$WORK/id_ed25519.pub")
cat >"$WORK/user-data" <<EOF
#cloud-config
users:
  - default
  - name: teamkit-ci
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    ssh_authorized_keys:
      - $PUBLIC_KEY
runcmd:
  - [ sh, -c, "touch /tmp/teamkit-ci-ready" ]
EOF
cat >"$WORK/meta-data" <<EOF
instance-id: teamkit-alt-p11
local-hostname: teamkit-alt-p11
EOF

qemu-img create -q -f qcow2 -F qcow2 -b "$IMAGE" "$WORK/overlay.qcow2" 8G
cloud-localds "$WORK/seed.img" "$WORK/user-data" "$WORK/meta-data"
qemu-system-x86_64 \
  -machine pc,accel=tcg,usb=off \
  -cpu max -smp 4 -m 4096 \
  -drive "file=$WORK/overlay.qcow2,format=qcow2,if=ide" \
  -drive "file=$WORK/seed.img,format=raw,if=ide,media=cdrom,readonly=on" \
  -netdev user,id=net0,hostfwd=tcp:127.0.0.1:2222-:22 \
  -device virtio-net-pci,netdev=net0 \
  -display none -serial "file:$EVIDENCE_DIR/console.log" \
  -daemonize -pidfile "$WORK/qemu.pid"
QEMU_PID=$(<"$WORK/qemu.pid")

SSH=(ssh -i "$WORK/id_ed25519" -p 2222 -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5 teamkit-ci@127.0.0.1)
ready=0
for _ in $(seq 1 120); do
  if "${SSH[@]}" 'test -f /tmp/teamkit-ci-ready' >/dev/null 2>&1; then ready=1; break; fi
  sleep 5
done
if [[ "$ready" != 1 ]]; then
  printf 'ALT_QEMU_BOOT_TIMEOUT\n' >&2
  exit 1
fi

LOCAL_CANDIDATE_SHA256=$(sha256sum "$CANDIDATE" | awk '{print $1}')
scp -q -i "$WORK/id_ed25519" -P 2222 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
  "$CANDIDATE" teamkit-ci@127.0.0.1:/tmp/teamkit
scp -q -i "$WORK/id_ed25519" -P 2222 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
  "$LIFECYCLE_SCRIPT" teamkit-ci@127.0.0.1:/tmp/artifact-lifecycle-smoke.sh
REMOTE_CANDIDATE_SHA256=$("${SSH[@]}" 'sha256sum /tmp/teamkit | cut -d " " -f1')
[[ "$REMOTE_CANDIDATE_SHA256" == "$LOCAL_CANDIDATE_SHA256" ]] || { printf 'ALT_QEMU_CANDIDATE_HASH_MISMATCH\n' >&2; exit 1; }
printf '%s  teamkit\n' "$LOCAL_CANDIDATE_SHA256" >"$EVIDENCE_DIR/candidate-sha256.txt"
"${SSH[@]}" 'set -e; chmod 700 /tmp/teamkit /tmp/artifact-lifecycle-smoke.sh; mkdir -p /tmp/teamkit-evidence; /tmp/artifact-lifecycle-smoke.sh /tmp/teamkit /tmp/teamkit-evidence'
scp -q -r -i "$WORK/id_ed25519" -P 2222 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
  teamkit-ci@127.0.0.1:/tmp/teamkit-evidence/. "$EVIDENCE_DIR/"
"${SSH[@]}" 'cat /etc/os-release' >"$EVIDENCE_DIR/os-release.txt"
"${SSH[@]}" 'uname -a' >"$EVIDENCE_DIR/kernel.txt"
"${SSH[@]}" 'findmnt -n -o SOURCE,FSTYPE,OPTIONS /; df -T / /tmp' >"$EVIDENCE_DIR/filesystem.txt"
grep -F 'ID=altlinux' "$EVIDENCE_DIR/os-release.txt" >/dev/null
grep -F 'ARTIFACT_LIFECYCLE_VERIFIED' "$EVIDENCE_DIR/result.txt" >/dev/null
printf 'ALT_VM_VERIFIED image=%s sha256=%s candidate_digest=%s\n' "$ALT_QEMU_URL" "$ALT_QEMU_SHA256" "$CANDIDATE_ARTIFACT_DIGEST" | tee -a "$EVIDENCE_DIR/result.txt"
