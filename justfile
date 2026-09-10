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

# Run/count every public gate and refresh VERIFICATION.md.
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
