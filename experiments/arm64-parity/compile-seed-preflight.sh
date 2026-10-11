#!/bin/sh
set -eu
go build -tags wago_guardpage -o /tmp/paired-compile-seed-preflight ./experiments/arm64-parity/paired
python3 experiments/arm64-parity/compile-seed-preflight-cross.py
