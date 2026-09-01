# Compiler arena reuse — 2026-09-01

Issue: #316. Base: `925eea99c9930f44b9f1b284cb3c4e4dd7113a3a`.

Host: Linux/amd64, AMD Ryzen 7 8845HS, Go 1.24.4. Focused runs used
`GOMAXPROCS=1`.

## Current-main finding

Railshot already reuses one operand-stack arena across serial functions and one
isolated arena per parallel worker. The remaining large-expression allocation
was caused by two sizing policies:

- the serial arena's first chunk was capped at the legacy 256 nodes even when the
  existing body scan had a larger node estimate; and
- `StackArenaCapacity` added 50% slack to an estimate that already counts every
  node-producing instruction and control-edge rebuild allowance.

A 1,539-byte ALU-heavy body reports 1,027 node-producing operations. Current
main starts with 256 nodes and grows through stable geometric chunks. Allocation
profiles attribute most bytes for this benchmark to that growth.

## Retained change

Serial compilation now reserves the existing node estimate directly. The first
chunk remains capped at 2,048 nodes. Larger functions keep the stable chunked
fallback, so a pathological hint cannot cause an unbounded speculative first
allocation.

Parallel workers retain the legacy 256-node ceiling. This avoids multiplying one
large function's initial arena by every worker. A worker allocates larger stable
chunks only if it receives a function that needs them.

The body-size floor and no-hint fallback remain in place. An underestimated hint
can affect allocation only; it cannot invalidate node pointers or emitted code.

## Focused benchmark

Command, run from both the base and candidate worktrees:

```sh
GOMAXPROCS=1 go test ./src/core/compiler/backend/railshot/amd64 -run '^$' \
  -bench 'BenchmarkRailshotCompile(SmallScalar|MediumControl|ALUHeavy)$' \
  -benchmem -count=5
```

Five-sample medians:

| benchmark | metric | base | candidate | change |
|---|---|---:|---:|---:|
| small scalar | time | 10,026 ns/op | 9,722 ns/op | -3.03% |
| small scalar | Go bytes | 9,813 B/op | 9,804 B/op | -0.09% |
| small scalar | allocations | 29/op | 29/op | 0% |
| medium control | time | 11,482 ns/op | 10,708 ns/op | -6.74% |
| medium control | Go bytes | 12,009 B/op | 11,104 B/op | -7.54% |
| medium control | allocations | 30/op | 30/op | 0% |
| ALU-heavy | time | 104,523 ns/op | 84,205 ns/op | -19.44% |
| ALU-heavy | Go bytes | 212,370 B/op | 130,306 B/op | -38.64% |
| ALU-heavy | allocations | 20/op | 16/op | -20.00% |

The stable allocation metrics show the intended effect. Time results are
supporting data; broader corpus work is still required before making a general
compile-time claim.

## Verification

Passed:

```sh
go test ./src/core/compiler/backend/railshot/shared \
  ./src/core/compiler/backend/railshot/amd64
GOOS=linux GOARCH=arm64 go test -c \
  ./src/core/compiler/backend/railshot/arm64
```

The ARM64 command compiled the test binary but did not execute it on this AMD64
host.

## Next work

- Add retry cost counters and a bounded pressure hint before changing pin policy.
- Reuse function result-type conversion storage.
- Measure large real modules and forced parallel builds before changing worker
  arena growth.
- Rerun the matrix on Go 1.26 when that toolchain is available.
