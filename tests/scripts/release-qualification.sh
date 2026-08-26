#!/usr/bin/env bash
set -euo pipefail

repository_root=$(git rev-parse --show-toplevel)
test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT

source_sha=0123456789abcdef0123456789abcdef01234567
run_id=123456
version=v1.2.3
repository=wago-org/wago
success_needs='{"changes":{"result":"success"},"docs":{"result":"success"},"lint":{"result":"success"},"regression-corpus":{"result":"success"},"runtime-concurrency":{"result":"success"},"race":{"result":"success"},"platform-test":{"result":"success"},"core-v2":{"result":"success"},"core-v3":{"result":"success"},"tinygo":{"result":"success"},"coverage":{"result":"success"},"size":{"result":"success"}}'

CI_NEEDS="$success_needs" \
CI_REPOSITORY="$repository" \
CI_SOURCE_SHA="$source_sha" \
CI_RUN_ID="$run_id" \
CI_RUN_ATTEMPT=1 \
CI_WORKFLOW_REF="$repository/.github/workflows/ci.yml@refs/heads/main" \
  "$repository_root/scripts/release-qualification.sh" record-ci "$test_root/ci.json"

"$repository_root/scripts/release-qualification.sh" verify-ci \
  "$test_root/ci.json" "$repository" "$source_sha" "$run_id" 1
if "$repository_root/scripts/release-qualification.sh" verify-ci \
  "$test_root/ci.json" "$repository" "$source_sha" "$run_id" 2 >/dev/null 2>&1; then
  echo "qualification record unexpectedly matched a different run attempt" >&2
  exit 1
fi

skipped_needs=${success_needs/\"core-v3\":{\"result\":\"success\"}/\"core-v3\":{\"result\":\"skipped\"}}
CI_NEEDS="$skipped_needs" \
CI_REPOSITORY="$repository" \
CI_SOURCE_SHA="$source_sha" \
CI_RUN_ID="$run_id" \
CI_RUN_ATTEMPT=1 \
CI_WORKFLOW_REF="$repository/.github/workflows/ci.yml@refs/heads/main" \
  go run "$repository_root/tests/tools/release-qualification" record-ci "$test_root/ci-skipped.json" 2>/dev/null && {
    echo "skipped required CI job unexpectedly produced a qualification record" >&2
    exit 1
  }

mkdir -p "$test_root/release" "$test_root/manifest"
for name in \
  wago-linux-amd64 \
  wago-linux-amd64.sha256 \
  wago-installer-linux-amd64 \
  wago-installer-linux-amd64.sha256 \
  wago-runtime-standard-normal-linux-amd64 \
  wago-runtime-standard-normal-linux-amd64.sha256 \
  wago-runtime-minimal-normal-linux-amd64 \
  wago-runtime-minimal-normal-linux-amd64.sha256; do
  printf '%s\n' "$name" >"$test_root/release/$name"
done

GITHUB_REPOSITORY="$repository" \
  "$repository_root/scripts/release-qualification.sh" create-release \
  "$test_root/release" "$version" "$source_sha" "$run_id" 1 \
  "$test_root/ci.json" "$test_root/manifest/release-manifest.json"
"$repository_root/scripts/release-qualification.sh" verify-release \
  "$test_root/manifest/release-manifest.json" "$test_root/release" \
  "$repository" "$version" "$source_sha" "$run_id" 1

printf 'tampered\n' >>"$test_root/release/wago-linux-amd64"
if "$repository_root/scripts/release-qualification.sh" verify-release \
  "$test_root/manifest/release-manifest.json" "$test_root/release" \
  "$repository" "$version" "$source_sha" "$run_id" 1 >/dev/null 2>&1; then
  echo "tampered release asset unexpectedly verified" >&2
  exit 1
fi

smoke="$test_root/smoke"
mkdir -p "$smoke"
cat >"$smoke/wago-linux-amd64" <<EOF
#!/usr/bin/env bash
printf '{"managerVersion":"$version"}\n'
EOF
cat >"$smoke/wago-installer-linux-amd64" <<EOF
#!/usr/bin/env bash
printf '%s\n' '$version'
EOF
for profile in standard minimal; do
  runtime="$smoke/wago-runtime-${profile}-normal-linux-amd64"
  cat >"$runtime" <<EOF
#!/usr/bin/env bash
set -euo pipefail
if [[ "\${1:-}" == "--json" ]]; then
  printf '{"release":"$version"}\n'
  exit 0
fi
if [[ "\${1:-}" == "build" ]]; then
  while ((\$# > 0)); do
    if [[ "\$1" == "--output" ]]; then
      printf 'artifact\n' >"\$2"
      exit 0
    fi
    shift
  done
  exit 1
fi
if [[ "\${1:-}" == "run" ]]; then
  printf 'fib(20) = 6765\n'
  exit 0
fi
exit 1
EOF
done
chmod +x "$smoke"/*
"$repository_root/scripts/smoke-release-assets.sh" \
  "$smoke" linux-amd64 "$version"

echo "release qualification tests passed"
