#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
python3 docs/performance/setup-cleanup/prepare-phase-probe.py
gofmt -w .tmp/setup-cleanup/instantiate-probe.go .tmp/setup-cleanup/instance-phase-probe_test.go
go test -c -overlay .tmp/setup-cleanup/baseline-phase-overlay.json -tags wago_guardpage -o .tmp/setup-cleanup/baseline-phase.test ./src/wago
go test -c -overlay .tmp/setup-cleanup/candidate-phase-overlay.json -tags wago_guardpage -o .tmp/setup-cleanup/candidate-phase.test ./src/wago
python3 docs/performance/setup-cleanup/run-phase-probe.py
benchstat docs/performance/setup-cleanup/phase-probe-baseline.txt docs/performance/setup-cleanup/phase-probe-candidate.txt > docs/performance/setup-cleanup/phase-probe-benchstat.txt
