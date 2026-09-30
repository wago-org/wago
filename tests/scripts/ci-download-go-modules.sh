#!/usr/bin/env bash
set -euo pipefail

repository_root=$(git rev-parse --show-toplevel)
test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT
mkdir -p "$test_root/bin"

cat >"$test_root/bin/go" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$CAPTURE_GO"
if [ "$*" != 'list -deps -test ./suite' ]; then
	echo "Only corpus dependency discovery may be retried" >&2
	exit 99
fi
attempt=$(wc -l <"$CAPTURE_GO")
if [ "$attempt" -le "$FAILURES" ]; then
	exit "$FAILURE_STATUS"
fi
EOF
cat >"$test_root/bin/sleep" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$CAPTURE_SLEEP"
EOF
chmod +x "$test_root/bin/go" "$test_root/bin/sleep"

run_case() {
  local failures=$1 expected_attempts=$2 expected_status=$3 expected_sleep=$4
  local status=0
  : >"$test_root/go"
  : >"$test_root/sleep"
  PATH="$test_root/bin:$PATH" CAPTURE_GO="$test_root/go" \
    CAPTURE_SLEEP="$test_root/sleep" FAILURES="$failures" FAILURE_STATUS=42 \
    sh "$repository_root/scripts/ci-download-go-modules.sh" >"$test_root/output" 2>&1 || status=$?
  [[ "$status" -eq "$expected_status" ]]
  [[ $(wc -l <"$test_root/go") -eq "$expected_attempts" ]]
  [[ $(grep -Fxc 'list -deps -test ./suite' "$test_root/go") -eq "$expected_attempts" ]]
  [[ $(cat "$test_root/sleep") == "$expected_sleep" ]]
  if [[ "$expected_status" -ne 0 ]]; then
    grep -F 'Go dependency preparation failed after 3 attempts (exit 42)' "$test_root/output" >/dev/null
  fi
}

run_case 0 1 0 ''
run_case 1 2 0 '5'
run_case 2 3 0 $'5\n10'
run_case 3 3 42 $'5\n10'

# Execute the actual workflow test commands with a failing Go stub. Test failures
# must propagate immediately and must never enter the download retry loop.
cat >"$test_root/bin/go" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$CAPTURE_GO"
exit 43
EOF
for step in 'Run selected correctness workloads' "Run this target's application shard"; do
  awk -v step="$step" '
    $0 == "      - name: " step { selected = 1; next }
    selected && /^      - / { exit }
    selected && /^        run: \|$/ { body = 1; next }
    body { sub(/^          /, ""); print }
  ' "$repository_root/.github/workflows/ci.yml" >"$test_root/run"
  [[ -s "$test_root/run" ]]
  : >"$test_root/go"
  status=0
  (cd "$repository_root" && PATH="$test_root/bin:$PATH" \
    CAPTURE_GO="$test_root/go" CORPUS_SELECTOR=quick \
    bash --noprofile --norc -eo pipefail "$test_root/run") || status=$?
  [[ "$status" -eq 43 ]]
  [[ $(wc -l <"$test_root/go") -eq 1 ]]
  grep -E '^test -count=1 -timeout 20m ' "$test_root/go" >/dev/null
done

echo "ci-download-go-modules tests passed"
