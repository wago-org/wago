set working-directory := '..'

# Run every lint check used by CI.
[default]
all: licenses fmt generated vet staticcheck scripts

# Check Go formatting.
fmt:
    #!/usr/bin/env bash
    set -euo pipefail
    unformatted="$(git ls-files -z -- '*.go' | xargs -0 gofmt -l)"
    if [[ -n "$unformatted" ]]; then
      echo '::error::These files are not gofmt-ed:'
      echo "$unformatted"
      exit 1
    fi

# Regenerate checked-in files and fail if they changed.
generated:
    go generate ./...
    git diff --exit-code -- wago.go schema.json || { echo "::error::Generated files are stale. Run 'go generate ./...' and commit the result."; exit 1; }

# Run go vet for the manager and runtime builds.
vet:
    go vet ./...
    go vet -tags wago_runtime ./cli/...

# Run staticcheck when it is installed.
staticcheck:
    #!/usr/bin/env bash
    set -euo pipefail
    if command -v staticcheck >/dev/null 2>&1; then
      go run ./tests/tools/staticcheck-baseline verify
    else
      echo 'just: staticcheck not found, skipping (go install honnef.co/go/tools/cmd/staticcheck@2026.2.1)'
    fi

# Test website data generators and the engine-state oracle.
scripts:
    node --test scripts/update-website-bench.test.mjs
    node --test scripts/update-website-startup.test.mjs
    node --test scripts/engine-state-oracle.test.mjs
    node --test scripts/fuzz-engine-state.test.mjs

# Check project source notices and preserve upstream fixture licensing.
licenses:
    node scripts/license-headers.mjs --check
    node --test scripts/license-headers.test.mjs
