# Compiler arena reuse — 2026-09-01

Issue: #316. Base: `925eea99c9930f44b9f1b284cb3c4e4dd7113a3a`.

Host: Linux/amd64, AMD Ryzen 7 8845HS, Go 1.24.4. Focused runs used
`GOMAXPROCS=1`.

## Current-main finding

Railshot already reuses one operand-stack arena across serial functions and one
isolated arena per parallel worker. The remaining large-expression allocation
came from the serial arena's first chunk being capped at the legacy 256 nodes
even when the existing body scan had a larger bounded estimate.

A 1,539-byte ALU-heavy body reports 1,027 node-producing operations. Current
main starts with 256 nodes and grows through stable geometric chunks. Allocation
profiles attribute most bytes for this benchmark to that growth.

The estimate retains its existing 50% bounded slack. A Codex review found that a
multi-value call is one opcode in the scan but lowering allocates one node per
result. Removing the slack without first adding result-arity accounting could
make such functions grow a second retained chunk.

## Retained change

Serial compilation uses the existing bounded estimate when it is at most 2,048
nodes. A larger estimate falls back to the legacy 256-node first chunk and stable
geometric growth. This avoids both unbounded speculative retention and the
measured json-as over-allocation caused by clamping a larger hint to 2,048.

Parallel workers retain the legacy 256-node ceiling. This avoids multiplying one
large function's initial arena by every worker. A worker allocates larger stable
chunks only if it receives a function that needs them.

Modules with active inline targets also retain the legacy first chunk. A caller's
hint counts the call opcode but not each node-producing instruction spliced from
the callee at every call site. Falling back avoids a new scan or unbounded inline
expansion estimate. The measured json-as and utf-as modules have no active inline
candidates, so their results below are unchanged.

The body-size floor, multi-value slack, and no-hint fallback remain in place. An
underestimated hint can affect allocation only; it cannot invalidate node
pointers or emitted code.

## Focused benchmark

Base and candidate test binaries were run in alternating order, pinned to CPU 2,
with `GOMAXPROCS=1`, ten 500 ms samples, and `benchstat`.

| benchmark | metric | base | candidate | change |
|---|---|---:|---:|---:|
| small scalar | time | 13.23 us/op | 12.65 us/op | flat, p=0.529 |
| small scalar | Go bytes | 9,812 B/op | 9,824 B/op | flat |
| medium control | time | 15.28 us/op | 15.02 us/op | flat, p=1.000 |
| medium control | Go bytes | 12,081 B/op | 12,111 B/op | flat |
| ALU-heavy | time | 138.8 us/op | 126.3 us/op | flat, p=0.218 |
| ALU-heavy | Go bytes | 212,421 B/op | 187,769 B/op | **-11.61%** |
| ALU-heavy | allocations | 20/op | 16/op | **-20.00%** |

The stable allocation metrics show the intended effect. The timing distributions
are not statistically different.

## json-as and utf-as SWAR comparison

The full comparison used prebuilt base and candidate benchmark binaries in
alternating order on CPU 2. Pipeline stages used ten 300 ms samples. Execution
used ten 1 s samples. All timing rows below were statistically flat.

| stage | workload | base | candidate | observed change |
|---|---|---:|---:|---:|
| decode | json-as | 69.06 us | 68.99 us | -0.10% |
| decode | utf-as | 21.38 us | 21.18 us | -0.96% |
| validate | json-as | 208.1 us | 206.6 us | -0.72% |
| validate | utf-as | 41.48 us | 41.15 us | -0.80% |
| backend compile | json-as | 1.214 ms | 1.220 ms | +0.48% |
| backend compile | utf-as | 159.0 us | 156.6 us | -1.47% |
| full compile | json-as | 2.108 ms | 2.144 ms | +1.74% |
| full compile | utf-as | 347.9 us | 348.8 us | +0.26% |
| instantiate | json-as | 12.95 us | 12.85 us | -0.80% |
| instantiate | utf-as | 4.876 us | 4.901 us | +0.50% |

Compile allocation changes were exact across samples:

| workload | path | base bytes | candidate bytes | bytes change | base allocs | candidate allocs |
|---|---|---:|---:|---:|---:|---:|
| json-as | backend | 280,569 | 280,571 | flat | 1,353 | 1,353 |
| json-as | full | 338,850 | 338,850 | flat | 1,640 | 1,640 |
| utf-as | backend | 224,304 | 183,194 | **-18.33%** | 147 | 143 |
| utf-as | full | 280,641 | 239,537 | **-14.65%** | 228 | 224 |

Generated-code execution is unchanged, as expected for a compiler scratch-only
change:

| workload | base | candidate | result |
|---|---:|---:|---|
| json-as `serializeN` | 23.77 us | 23.57 us | flat, 0 B/op |
| json-as `deserializeN` | 42.08 us | 41.76 us | flat, 0 B/op |
| utf-as `convertN` | 160.0 us | 159.9 us | flat, 0 B/op |

The change therefore gives a clear compile-memory win for utf-as SWAR, leaves
json-as compile memory neutral, and does not alter execution speed.

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
