set working-directory := '..'

head := `git rev-parse HEAD 2>/dev/null`

# Run guarded benchmarks: `just bench run [corpus] [group]`.
[default]
run corpus=env('CORPUS', 'quick') group=env('BENCH', 'all') count=env('COUNT', '1') time=env('BENCHTIME', '1s') output=env('BENCH_RUN', 'bench/.bench-run.txt'):
    #!/usr/bin/env bash
    set -euo pipefail
    case '{{ group }}' in
      all) pattern='.' ;;
      pipeline) pattern='^Benchmark(Decode|Validate|Compile|CompileFull|Instantiate)$' ;;
      compile) pattern='^Benchmark(Decode|Validate|Compile|CompileFull)$' ;;
      exec) pattern='^Benchmark(Exec|CommandExec)$|^BenchmarkExec(CallOverhead|HostRoundtrip)_' ;;
      *) pattern='{{ group }}' ;;
    esac
    { echo '# git {{ head }}'; (cd bench && WAGO_BOUNDS=signals go test -run '^$' -tags wago_guardpage -bench "$pattern" -benchmem -count '{{ count }}' -benchtime '{{ time }}' -timeout 0 ./suite -args -wago.corpus='{{ corpus }}'); } | tee '{{ output }}'

# Verify benchmark wiring with one iteration.
check corpus=env('CORPUS', 'quick') group=env('BENCH', 'all'):
    #!/usr/bin/env bash
    set -euo pipefail
    case '{{ group }}' in
      all) pattern='.' ;;
      pipeline) pattern='^Benchmark(Decode|Validate|Compile|CompileFull|Instantiate)$' ;;
      compile) pattern='^Benchmark(Decode|Validate|Compile|CompileFull)$' ;;
      exec) pattern='^Benchmark(Exec|CommandExec)$|^BenchmarkExec(CallOverhead|HostRoundtrip)_' ;;
      *) pattern='{{ group }}' ;;
    esac
    cd bench
    go test -run '^$' -bench "$pattern" -count 1 -benchtime 1x -timeout 0 ./suite -args -wago.corpus='{{ corpus }}'

# Run explicit-bounds benchmarks.
no-guard corpus=env('CORPUS', 'quick') group=env('BENCH', 'all') count=env('COUNT', '1') time=env('BENCHTIME', '1s') output=env('BENCH_RUN', 'bench/.bench-run.txt'):
    #!/usr/bin/env bash
    set -euo pipefail
    case '{{ group }}' in all) pattern='.' ;; pipeline) pattern='^Benchmark(Decode|Validate|Compile|CompileFull|Instantiate)$' ;; compile) pattern='^Benchmark(Decode|Validate|Compile|CompileFull)$' ;; exec) pattern='^Benchmark(Exec|CommandExec)$|^BenchmarkExec(CallOverhead|HostRoundtrip)_' ;; *) pattern='{{ group }}' ;; esac
    { echo '# git {{ head }}'; (cd bench && go test -run '^$' -bench "$pattern" -benchmem -count '{{ count }}' -benchtime '{{ time }}' -timeout 0 ./suite -args -wago.corpus='{{ corpus }}'); } | tee '{{ output }}'

# Run only Wago benchmarks.
wago corpus=env('CORPUS', 'quick') count=env('COUNT', '1') time=env('BENCHTIME', '1s'):
    cd bench && go test -run '^$' -bench '^Benchmark(Decode|Validate|Compile|CompileFull|Instantiate|Exec|CommandExec)$|_wago$' -benchmem -count '{{ count }}' -benchtime '{{ time }}' -timeout 0 ./suite -args -wago.corpus='{{ corpus }}'

# Run only wazero benchmarks.
wazero corpus=env('CORPUS', 'quick') count=env('COUNT', '1') time=env('BENCHTIME', '1s'):
    cd bench && go test -run '^$' -bench '[Ww]azero' -benchmem -count '{{ count }}' -benchtime '{{ time }}' -timeout 0 ./suite -args -wago.corpus='{{ corpus }}'

# Benchmark Railshot JIT edge cases and corpus compilation.
jit corpus=env('CORPUS', 'quick') count=env('COUNT', '1') time=env('BENCHTIME', '1s'):
    go test ./src/core/compiler/backend/railshot/amd64 -run '^$' -bench '^BenchmarkRailshotCompile' -benchmem -count '{{ count }}' -benchtime '{{ time }}'
    cd bench && go test -run '^$' -bench '^BenchmarkCompile(Full)?$' -benchmem -count '{{ count }}' -benchtime '{{ time }}' -timeout 0 ./suite -args -wago.corpus='{{ corpus }}'

# Benchmark a MoonBit Starshine wasm-gc artifact.
starshine wasm count=env('COUNT', '1') time=env('BENCHTIME', '1s'):
    WAGO_STARSHINE_SMOKE_WASM='{{ wasm }}' go test ./src/wago -run '^$' -bench '^BenchmarkMoonBitStarshineWasmGC' -benchmem -count '{{ count }}' -benchtime '{{ time }}'

# Render charts from the latest capture.
render input=env('BENCH_RUN', 'bench/.bench-run.txt'):
    #!/usr/bin/env bash
    set -euo pipefail
    [[ -f '{{ input }}' ]] || { echo "just: no capture at {{ input }}; run 'just bench'" >&2; exit 1; }
    cd bench
    go run ./cmd/benchpub -in "$(basename '{{ input }}')" -out out
    echo 'just: charts written to bench/out/charts/*.svg'

# Update the website's performance data from the latest capture.
website input=env('BENCH_RUN', 'bench/.bench-run.txt'):
    #!/usr/bin/env bash
    set -euo pipefail
    [[ -f '{{ input }}' ]] || { echo "just: no capture at {{ input }}; run 'just bench'" >&2; exit 1; }
    WAGO_BENCH_IN='{{ input }}' scripts/update-website-bench.mjs

# Run the end-to-end startup sweep.
startup:
    node bench/startup/run.mjs

# Publish the capture to wago-org/docs and refresh a sibling website when present.
publish input=env('BENCH_RUN', 'bench/.bench-run.txt'):
    #!/usr/bin/env bash
    set -euo pipefail
    [[ -f '{{ input }}' ]] || { echo "just: no capture at {{ input }}; run 'just bench'" >&2; exit 1; }
    cached="$(sed -n 's/^# git //p' '{{ input }}' | head -1)"
    if [[ "$cached" != '{{ head }}' ]]; then
      echo "just: WARNING capture is stale (captured at ${cached:-none}, HEAD is {{ head }}); publishing anyway" >&2
    fi
    WAGO_BENCH_IN='{{ input }}' scripts/publish-bench.sh
    if [[ -f ../website/package.json ]]; then
      WAGO_BENCH_IN='{{ input }}' scripts/update-website-bench.mjs
    else
      echo 'just: ../website not found; skipping website benchmark update'
    fi

# Publish regenerated charts to wago-org/docs.
publish-charts:
    scripts/publish-charts.sh
