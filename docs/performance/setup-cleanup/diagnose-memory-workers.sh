#!/usr/bin/env bash
set -euo pipefail
export GOMAXPROCS=16 GOGC=100 GOMEMLIMIT=off GODEBUG= WAGO_BOUNDS=signals
cd /home/jtenner/Projects/wago/bench/suite
out=../../docs/performance/setup-cleanup
bin=../../.tmp/setup-cleanup/candidate-diagnostic.test
taskset -c 0-15 "$bin" -test.run '^TestLifecycleFixtureMetadata$' -test.v -wago.corpus tiny,utf8proc,pcre2,xxhash -wago.bench.lifecycle > "$out/fixture-metadata.txt"
taskset -c 0-15 "$bin" -test.run '^$' -test.bench '^BenchmarkInstanceLifecycleDiagnostic$' -test.benchtime 100x -test.count 10 -test.benchmem -wago.corpus tiny,utf8proc,pcre2,xxhash -wago.bench.lifecycle > "$out/diagnostic-instance.txt"
taskset -c 0-15 "$bin" -test.run '^$' -test.bench '^BenchmarkMemory(Reuse|GrowReuse)Diagnostic$' -test.benchtime 1000x -test.count 10 -test.benchmem -wago.bench.lifecycle > "$out/diagnostic-memory.txt"
taskset -c 0-15 "$bin" -test.run '^$' -test.bench '^BenchmarkWorkerLifecycleDiagnostic$' -test.benchtime 100ms -test.count 10 -test.benchmem -wago.corpus tiny,many_funcs,json-as,lua -wago.bench.lifecycle > "$out/diagnostic-workers.txt"
taskset -c 0-15 "$bin" -test.run '^$' -test.bench '^BenchmarkInstantiate$/utf8proc$' -test.benchtime 5s -test.cpuprofile ../../.tmp/setup-cleanup/instance.cpu -wago.corpus utf8proc > "$out/profile-instance-cpu.txt"
strace -c -e mmap,munmap,mprotect,madvise taskset -c 0-15 "$bin" -test.run '^$' -test.bench '^BenchmarkInstantiate$/utf8proc$' -test.benchtime 1000x -wago.corpus utf8proc > "$out/profile-instance-syscalls.txt" 2> "$out/profile-instance-strace.txt"
taskset -c 0-15 "$bin" -test.run '^$' -test.bench '^BenchmarkWorkerLifecycleDiagnostic$/json-as/concurrent/full/requested=0$' -test.benchtime 5s -test.cpuprofile ../../.tmp/setup-cleanup/workers.cpu -wago.corpus json-as -wago.bench.lifecycle > "$out/profile-workers-cpu.txt"
taskset -c 0-15 "$bin" -test.run '^$' -test.bench '^BenchmarkWorkerLifecycleDiagnostic$/json-as/concurrent/full/requested=0$' -test.benchtime 100x -test.memprofile ../../.tmp/setup-cleanup/workers.alloc -test.memprofilerate 1 -wago.corpus json-as -wago.bench.lifecycle > "$out/profile-workers-alloc.txt"
