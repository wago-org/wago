#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT

cat >"$test_root/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >"$CAPTURE_ARGS"
cat >"$CAPTURE_BODY"
EOF
chmod +x "$test_root/gh"

capture_args="$test_root/args"
capture_body="$test_root/body"
PATH="$test_root:$PATH" \
  CAPTURE_ARGS="$capture_args" \
  CAPTURE_BODY="$capture_body" \
  DOCS_SYNC_TOKEN=test-token \
  "$repository_root/scripts/dispatch-docs-release.sh" \
  nightly nightly-20260731-0123456 0123456789abcdef0123456789abcdef01234567

grep -qx -- 'api --method POST repos/wago-org/docs/dispatches --input -' "$capture_args"
jq -e '
  .event_type == "code-release" and
  .client_payload.channel == "nightly" and
  .client_payload.tag == "nightly-20260731-0123456" and
  .client_payload.sha == "0123456789abcdef0123456789abcdef01234567"
' "$capture_body" >/dev/null

if DOCS_SYNC_TOKEN=test-token "$repository_root/scripts/dispatch-docs-release.sh" invalid tag 0123456789abcdef0123456789abcdef01234567; then
  echo "invalid channel unexpectedly succeeded" >&2
  exit 1
fi

if DOCS_SYNC_TOKEN=test-token "$repository_root/scripts/dispatch-docs-release.sh" canary tag short; then
  echo "invalid SHA unexpectedly succeeded" >&2
  exit 1
fi

echo "dispatch-docs-release tests passed"
