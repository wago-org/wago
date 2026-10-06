#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p results
cargo build --release --locked --manifest-path wasmtime/Cargo.toml
go build -o wago-host-latency .
if [[ "${1:-}" == "--yield-unbounded" || "${1:-}" == "--yield-no-leaf" ]]; then
  ./wasmtime/target/release/host-latency-wasmtime --yield "${@:2}" > results/wasmtime.csv
else
  ./wasmtime/target/release/host-latency-wasmtime "$@" > results/wasmtime.csv
fi
./wago-host-latency "$@" > results/wago.csv
python3 summarize.py results/wasmtime.csv results/wago.csv
