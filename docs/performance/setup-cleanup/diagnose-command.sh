#!/usr/bin/env bash
set -euo pipefail
export GOMAXPROCS=16 GOGC=100 GOMEMLIMIT=off GODEBUG= WAGO_BOUNDS=signals
cd /home/jtenner/Projects/wago/bench/suite
out=../../docs/performance/setup-cleanup
bin=../../.tmp/setup-cleanup/baseline-suite.test
taskset -c 0-15 "$bin" -test.run '^TestMinimalWASICommand$' -test.bench '^BenchmarkCommandLifecycleDiagnostic$' -test.benchtime 1000x -test.count 10 -test.benchmem -wago.corpus cjson,tinyxml2 -wago.bench.lifecycle > "$out/diagnostic-command-baseline.txt"
taskset -c 0-15 ../../.tmp/setup-cleanup/baseline-wago.test -test.run '^$' -test.bench '^BenchmarkImportSnapshot$' -test.benchtime 200ms -test.count 10 -test.benchmem > "$out/diagnostic-snapshot-baseline.txt"
taskset -c 0-15 "$bin" -test.run '^$' -test.bench '^BenchmarkCommandLifecycleDiagnostic$/minimal-wasi/Imports$' -test.benchtime 5s -test.cpuprofile ../../.tmp/setup-cleanup/imports.cpu -wago.corpus cjson,tinyxml2 -wago.bench.lifecycle > "$out/profile-imports-cpu.txt"
taskset -c 0-15 "$bin" -test.run '^$' -test.bench '^BenchmarkCommandExec$/cjson$' -test.benchtime 5s -test.cpuprofile ../../.tmp/setup-cleanup/command.cpu -wago.corpus cjson > "$out/profile-command-cpu.txt"
taskset -c 0-15 "$bin" -test.run '^$' -test.bench '^BenchmarkCommandExec$/cjson$' -test.benchtime 10000x -test.memprofile ../../.tmp/setup-cleanup/command.alloc -test.memprofilerate 1 -wago.corpus cjson > "$out/profile-command-alloc.txt"
