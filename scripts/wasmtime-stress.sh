#!/usr/bin/env sh
# Scheduled/local Wasmtime stress matrix. Override WAGO_STRESS_COUNT and
# WAGO_STRESS_FUZZTIME for a shorter developer smoke run.
set -eu

count=${WAGO_STRESS_COUNT:-20}
fuzztime=${WAGO_STRESS_FUZZTIME:-30s}

for procs in 1 2 4; do
	GOMAXPROCS="$procs" WAGO_BOUNDS=explicit \
		go test -count="$count" -shuffle=on ./src/wago \
		-run '^TestWasmtimePort(Concurrent|MemoryReuse|Reused|FailedInstantiation|Traps|ResourceFootprint)'
done

WAGO_WASMTIME_PRESERVE_KNOBS=1 \
	WAGO_INLINE=0 \
	WAGO_REG_MERGE=0 \
	WAGO_LOOP_PRECHECK=0 \
	WAGO_PREPARED_CALL=0 \
	WAGO_DIRECT_PREPARED=0 \
	WAGO_BOUNDS=explicit \
	go test -count=3 -shuffle=on ./src/wago -run '^TestWasmtime'

WAGO_BOUNDS=signals \
	go test -count=3 -shuffle=on -tags wago_guardpage ./src/wago -run '^TestWasmtime'

go test ./internal/wasmtimecorpus -run '^$' -fuzz '^FuzzValidateRelativePathAndRustScannerDoNotPanic$' -fuzztime="$fuzztime"
go test ./scripts/wasmtime-corpus -run '^$' -fuzz '^FuzzNormalizeWABTJSONDoesNotPanic$' -fuzztime="$fuzztime"
go test ./src/wago -run '^$' -fuzz '^FuzzSpecTrapMatchingDoesNotPanic$' -fuzztime="$fuzztime"
go test ./src/wago -run '^$' -fuzz '^FuzzWasmtimeEmbenchenSliceBounds$' -fuzztime="$fuzztime"
