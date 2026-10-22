#!/usr/bin/env bash
set -euo pipefail

repo=$(cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo"
command -v mise >/dev/null
mise install go just gh wasm-tools

missing=false
for tool in qemu-riscv64 qemu-system-riscv64 riscv64-linux-gnu-objdump clang \
  llvm-objdump ocamlc dune menhir make cmake ninja jq python3 perf /usr/bin/time; do
  command -v "$tool" >/dev/null || missing=true
done
if [[ $missing == true ]]; then
  if ! command -v apt-get >/dev/null; then
    echo 'Install QEMU, RISC-V binutils, Clang/LLVM, OCaml, Dune, and Menhir for this host.' >&2
    exit 1
  fi
  apt_command=(apt-get)
  if [[ $(id -u) != 0 ]]; then
    apt_command=(sudo -n apt-get)
  fi
  "${apt_command[@]}" update
  "${apt_command[@]}" install -y --no-install-recommends \
    qemu-user qemu-system-misc binutils-riscv64-linux-gnu clang llvm \
    build-essential cmake ninja-build ocaml ocaml-dune menhir \
    libmenhir-ocaml-dev libgmp-dev curl ca-certificates jq python3 linux-perf time
fi

# Normal checkout preserves local changes and refuses unsafe replacements.
git submodule update --init --checkout \
  tests/conformance/spec-v1 tests/conformance/spec-v2 tests/conformance/spec-v3
wabt=$(scripts/bootstrap-wabt.sh --print-path)
interpreter=$(scripts/bootstrap-spec-interpreter.sh --print-path)
mkdir -p .tools/bin .tools/rv64-fixtures
for tool in wat2wasm wast2json wasm2wat wasm-validate wasm-interp wasm-objdump; do
  ln -sfn "$(dirname -- "$wabt")/$tool" ".tools/bin/$tool"
done
ln -sfn "$interpreter" .tools/bin/wasm-spec-interpreter
benchstat_version=v0.0.0-20261009192801-be2c69fb417e
if [[ ! -x .tools/bin/benchstat ]] || \
    ! mise exec -- go version -m .tools/bin/benchstat | \
      awk -v version="$benchstat_version" '$1 == "mod" && $2 == "golang.org/x/perf" && $3 == version { found = 1 } END { exit !found }'; then
  mise exec -- env GOBIN="$repo/.tools/bin" \
    go install "golang.org/x/perf/cmd/benchstat@$benchstat_version"
fi

if [[ ! -d .tools/rv64-fixtures/simd ]]; then
  python3 scripts/prepare-riscv64-simd-spec.py \
    tests/conformance/spec-v2 .tools/rv64-fixtures/simd
fi
if [[ ! -d .tools/rv64-fixtures/relaxed-simd ]]; then
  python3 scripts/prepare-riscv64-simd-spec.py --relaxed \
    tests/conformance/spec-v3 .tools/rv64-fixtures/relaxed-simd
fi
for profile in numeric float memory control multimemory memory64 fac call; do
  suite=spec-v2
  if [[ $profile == multimemory || $profile == memory64 ]]; then
    suite=spec-v3
  fi
  if [[ ! -d .tools/rv64-fixtures/$profile ]]; then
    python3 scripts/prepare-riscv64-numeric-spec.py --profile "$profile" \
      "tests/conformance/$suite" ".tools/rv64-fixtures/$profile" "$wabt"
  fi
done
scripts/bootstrap-wabt.sh --verify
scripts/bootstrap-spec-interpreter.sh --verify
mise exec -- go version
qemu-riscv64 --version | head -1
qemu-system-riscv64 --version | head -1
mise exec -- just --version
mise exec -- wasm-tools --version
