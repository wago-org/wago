#!/bin/sh
set -eu
go build -tags wago_guardpage -o /tmp/paired-compile-seed-preflight-stop ./experiments/arm64-parity/paired
python3 experiments/arm64-parity/compile-seed-preflight-stop-cross.py
