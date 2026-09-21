#!/usr/bin/env bash
set -uo pipefail
out=docs/performance/setup-cleanup
PATH=/home/jtenner/.local/share/mise/installs/github-web-assembly-wabt/1.0.41/bin:/home/jtenner/.local/share/mise/installs/github-web-assembly-wabt/1.0.41:$PATH GOFLAGS='-buildvcs=false -overlay=/home/jtenner/Projects/wago/.tmp/setup-cleanup/baseline-overlay.json' go test -count=1 ./cli/manager/internal/standalone -run '^TestBuildTinyGoEmbedsArtifactWithoutCompiler$' > "$out/test-standalone-baseline.txt" 2>&1
printf '%s\n' "$?" > "$out/test-standalone-baseline.exit"
set -e
export GOMAXPROCS=16 GOGC=100 GOMEMLIMIT=off GODEBUG= WAGO_BOUNDS=signals
cd bench/suite
out=../../docs/performance/setup-cleanup
bin=../../.tmp/setup-cleanup/candidate-diagnostic.test
taskset -c 0-15 "$bin" -test.run '^$' -test.bench '^BenchmarkAcquisitionLifecycleDiagnostic$' -test.benchtime 200ms -test.count 10 -test.benchmem -wago.bench.lifecycle > "$out/diagnostic-acquisition.txt"
strace -f -c -e mmap,munmap,mprotect,madvise taskset -c 0-15 "$bin" -test.run '^$' -test.bench '^BenchmarkInstantiate$/utf8proc$' -test.benchtime 1000x -wago.corpus utf8proc > "$out/profile-instance-syscalls-all-threads.txt" 2> "$out/profile-instance-strace-all-threads.txt"
taskset -c 0-15 "$bin" -test.run '^$' -test.bench '^BenchmarkInstantiate$/pcre2$' -test.benchtime 5s -test.cpuprofile ../../.tmp/setup-cleanup/pcre2.cpu -wago.corpus pcre2 > "$out/profile-pcre2-cpu.txt"
