set working-directory := '..'

# Regenerate the whole website from committed data, then build it.
[default]
build website=env('WEBSITE_DIR', '../website') capture=env('BENCH_RUN', 'bench/.bench-run.txt'):
    #!/usr/bin/env bash
    set -euo pipefail
    [[ -f '{{ website }}/package.json' ]] || { echo 'just: {{ website }} not found (pass the website path)' >&2; exit 1; }
    WAGO_SITE_NOBUILD=1 WAGO_WEBSITE_DIR='{{ website }}' scripts/update-website-startup.mjs
    if [[ -f bench/out/bench.json || -f '{{ capture }}' ]]; then
      WAGO_SITE_NOBUILD=1 WAGO_WEBSITE_DIR='{{ website }}' scripts/update-website-bench.mjs
    else
      echo "just: no local benchmark data; leaving performance unchanged (run 'just bench && just bench render')"
    fi
    cd '{{ website }}'
    npm run sync
    npm run build
    echo 'just: website regenerated from startup, performance, and stats data'

# Update website startup data from both architecture captures.
startup website=env('WEBSITE_DIR', '../website'):
    WAGO_WEBSITE_DIR='{{ website }}' scripts/update-website-startup.mjs
