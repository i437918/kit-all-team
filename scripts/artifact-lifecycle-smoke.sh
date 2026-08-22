#!/usr/bin/env bash
set -euo pipefail

BINARY=${1:?usage: artifact-lifecycle-smoke.sh BINARY [EVIDENCE_DIR]}
EVIDENCE_DIR=${2:-evidence/artifact-lifecycle}
BINARY=$(realpath "$BINARY")
mkdir -p "$EVIDENCE_DIR"
EVIDENCE_DIR=$(realpath "$EVIDENCE_DIR")

WORK=$(mktemp -d)
cleanup() { rm -rf -- "$WORK"; }
trap cleanup EXIT

mkdir -p "$WORK/bin" "$WORK/home" "$WORK/config" "$WORK/kit/.teamkit" "$WORK/kit/.git" "$WORK/kit/db/.git/hooks"
cat >"$WORK/bin/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
directory=
while (($#)); do
  if [[ "$1" == "-C" && $# -ge 2 ]]; then
    directory=$2
    shift 2
    break
  fi
  shift
done
operation=${1:-}
shift || true
case "$operation" in
  config)
    if [[ "${1:-}" == "--local" ]]; then
      exit 0
    fi
    if [[ "$*" == *"remote.origin.url"* ]]; then
      if [[ "$directory" == */db ]]; then
        printf 'https://gitlab.example.invalid/1c/fulfillment/wms\n'
      else
        printf 'https://gitlab.example.invalid/1c/aisuz/ai.git\n'
      fi
      exit 0
    fi
    ;;
  symbolic-ref)
    if [[ "$directory" == */db ]]; then
      printf 'develop\n'
    else
      printf 'content-wms\n'
    fi
    exit 0
    ;;
  status)
    [[ "${1:-}" == "--porcelain" ]] && exit 0
    ;;
  ls-files)
    [[ "${1:-}" == "--stage" && "${2:-}" == "--" && "${3:-}" == ".gitignore" ]] && exit 0
    ;;
esac
printf 'unexpected hermetic git invocation\n' >&2
exit 91
EOF
chmod 700 "$WORK/bin/git"

KIT="$WORK/kit"
printf 'wms\n' >"$KIT/.teamkit/owner"
printf 'content-wms\n' >"$KIT/.teamkit/content.ready"
printf 'develop\n' >"$KIT/.teamkit/database.ready"
cat >"$KIT/db/.git/hooks/pre-commit" <<'EOF'
#!/bin/sh
branch="$(git symbolic-ref --quiet --short HEAD || true)"
if [ "$branch" = "develop" ]; then
  echo "teamkit: commits on develop are blocked" >&2
  exit 1
fi
exit 0
EOF
cat >"$KIT/db/.git/hooks/pre-push" <<'EOF'
#!/bin/sh
while read -r local_ref local_oid remote_ref remote_oid
do
  if [ "$remote_ref" = "refs/heads/develop" ]; then
    echo "teamkit: pushes to develop are blocked" >&2
    exit 1
  fi
done
exit 0
EOF
chmod 755 "$KIT/db/.git/hooks/pre-commit" "$KIT/db/.git/hooks/pre-push"
printf '.env\n/db/\n/.teamkit/\n' >"$KIT/.gitignore"
printf 'KIT_ALL_TEAM_HOME=%s\nOPERATING_SYSTEM=linux\nAI_APPLICATION=codex\nAI_APP_INSTALLED=true\nPROJECT=wms\nROLE=developer\nTOOLCHAIN=ai_rules_1c\n' "$KIT" >"$KIT/.env"

export HOME="$WORK/home"
export XDG_CONFIG_HOME="$WORK/config"
export PATH="$WORK/bin:$PATH"
SELECTORS=(
  --non-interactive --json --os linux --app codex --app-installed=true
  --kit-home "$KIT" --project wms --role developer
  --toolchain ai_rules_1c --update none
)

"$BINARY" plan "${SELECTORS[@]}" >"$EVIDENCE_DIR/plan.json" 2>"$EVIDENCE_DIR/plan.stderr"
grep -F '"command":"plan"' "$EVIDENCE_DIR/plan.json" >/dev/null
grep -F '"status":"needs_apply"' "$EVIDENCE_DIR/plan.json" >/dev/null
"$BINARY" status --json --kit-home "$KIT" >"$EVIDENCE_DIR/status-before.json" 2>"$EVIDENCE_DIR/status-before.stderr"
grep -F '"status":"needs_apply"' "$EVIDENCE_DIR/status-before.json" >/dev/null
"$BINARY" apply "${SELECTORS[@]}" >"$EVIDENCE_DIR/apply.json" 2>"$EVIDENCE_DIR/apply.stderr"
grep -F '"command":"apply"' "$EVIDENCE_DIR/apply.json" >/dev/null
grep -F '"handoff":"In codex, configure exactly one toolchain' "$EVIDENCE_DIR/apply.json" >/dev/null
"$BINARY" status --json --kit-home "$KIT" >"$EVIDENCE_DIR/status-after.json" 2>"$EVIDENCE_DIR/status-after.stderr"
grep -F '"status":"ready"' "$EVIDENCE_DIR/status-after.json" >/dev/null
"$BINARY" retry --json --kit-home "$KIT" >"$EVIDENCE_DIR/retry.json" 2>"$EVIDENCE_DIR/retry.stderr"
grep -F '"command":"retry"' "$EVIDENCE_DIR/retry.json" >/dev/null
grep -F '"status":"ready"' "$EVIDENCE_DIR/retry.json" >/dev/null
"$BINARY" update --json --kit-home "$KIT" --target none >"$EVIDENCE_DIR/update.json" 2>"$EVIDENCE_DIR/update.stderr"
grep -F '"command":"update"' "$EVIDENCE_DIR/update.json" >/dev/null
grep -F '"status":"ready"' "$EVIDENCE_DIR/update.json" >/dev/null

test -s "$KIT/.teamkit/handoff.txt"
for error_output in "$EVIDENCE_DIR"/*.stderr; do
  test ! -s "$error_output"
done
printf 'ARTIFACT_LIFECYCLE_VERIFIED\n' >"$EVIDENCE_DIR/result.txt"
