# Wago's developer interface. Run `just` to see the small top-level surface,
# then `just --list <group>` to explore a group.

mod lint '.just/lint.just'
mod test '.just/test.just'
mod build '.just/build.just'
mod bench '.just/bench.just'
mod install '.just/install.just'
mod site '.just/site.just'

[default]
[private]
list:
    @just --list

# Validate local paths and anchors in tracked Markdown files.
docs:
    go run ./tests/tools/docs-check

# Run all five public gates with merged cross-package coverage.
coverage output=env('COVERPROFILE', 'coverage.out'):
    COVERPROFILE='{{ output }}' scripts/coverage.sh

# Run and count every public verification gate.
verify:
    scripts/verification.sh

# Build the full local PR CI card.
card dir=env('CARD_DIR', 'ci-card') output=env('CARD_FILE', 'card.md'):
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p '{{ dir }}'
    COVER_REPORT='{{ dir }}/coverage.md' scripts/coverage.sh >/dev/null
    TESTS_REPORT='{{ dir }}/tests.md' scripts/tests-card.sh >/dev/null
    SPEC_REPORT='{{ dir }}/spec.md' scripts/spec-card.sh >/dev/null
    SIZE_REPORT='{{ dir }}/size.md' scripts/size-card.sh >/dev/null
    CARD_DIR='{{ dir }}' CARD_FILE='{{ output }}' scripts/pr-card.sh
    cat '{{ output }}'

# Replay the complete GitHub Actions workflow locally in Docker.
ci:
    scripts/ci-local.sh

# Dispatch the guarded GitHub Actions release workflow for an exact main commit.
# The workflow performs CI qualification, stress testing, builds, and publication.
release version source_sha='':
    #!/usr/bin/env bash
    set -euo pipefail

    if [[ ! '{{ version }}' =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-beta\.(0|[1-9][0-9]*)$ && ! '{{ version }}' =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
      echo 'just release: version must be vMAJOR.MINOR.PATCH-beta.N or vMAJOR.MINOR.PATCH' >&2
      exit 2
    fi
    gh auth status

    source_sha='{{ source_sha }}'
    if [[ -z "$source_sha" ]]; then
      source_sha=$(gh api repos/{owner}/{repo}/commits/main --jq .sha)
    fi
    if [[ ! "$source_sha" =~ ^[0-9a-fA-F]{40}$ ]]; then
      echo 'just release: source_sha must be a full 40-character commit SHA' >&2
      exit 2
    fi

    printf 'Release {{ version }} from main commit %s? [y/N] ' "$source_sha"
    read -r answer
    [[ "$answer" == y || "$answer" == Y || "$answer" == yes || "$answer" == YES ]] || {
      echo 'just release: cancelled' >&2
      exit 1
    }

    gh workflow run release.yml --ref main \
      -f version='{{ version }}' \
      -f source_sha="$source_sha"
    echo "just release: dispatched {{ version }} for $source_sha from main"
