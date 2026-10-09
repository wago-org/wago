# Operand-aware native comparison (#815)

The offline diagnostic captures bounded trusted Wasm input using the existing
profile/source-map backend, then compares operand records. It adds no production
instrumentation and does not execute, map or upload diagnostic native artifacts.
It is not a semantic-equivalence checker or automatic performance diagnosis.

From the repository root:

```sh
just bench native-capture tests/fixtures/wasm/fib.wasm /tmp/fib-before.json
# Run the same recipe in another compiler revision for an actual before/after.
just bench native-capture tests/fixtures/wasm/fib.wasm /tmp/fib-after.json
just bench native-compare /tmp/fib-before.json /tmp/fib-after.json
just test native-compare
```

Capture requires native AMD64, `wago_profile`, and existing GNU objdump. No software
is installed. The recipe compiles with a revision stamp; the capture records
actual executable SHA-256, input SHA-256, native image SHA-256, optimization
selection fingerprint, explicit bounds, baseline feature mask and compiler path.
A recipe stamp identifies its build checkout; it does not attest a clean checkout.
VCS build info, when available, identifies the built revision and dirty status.
Unstamped builds lacking VCS info produce unqualified provenance.

Comparison also accepts legacy `native-compare before.json after.json`. JSON is
written to stdout and a coverage summary to stderr. Standalone exit 0 means the
comparison is complete for supplied regions (changes are allowed), 2 means invalid
input, and 3 means incomplete. `go run` wraps nonzero tool exits as Go runner exit 1;
use a built binary when consuming the exact exit codes in scripts.

Each region has a stable function / Wasm PC / occurrence identity and contiguous
instructions. Wasm offsets include local-declaration bytes. Capture admits only
ranges with verified instruction boundaries, rejects overlapping or unordered
source maps, and anchors direct branch destinations only at captured instruction
starts. Relocation gaps and mid-instruction destinations remain literal. These
source/target identities are producer assertions, not independent proofs of every
compiler source map. Tests independently check selected Wasm/native attribution.

`complete` applies only to supplied regions. Capture/report metadata exposes native,
mapped and unmapped bytes and validates count consistency; omitted wrapper/call
bytes remain unqualified. Current Fibonacci has 12 supported mapped instructions
in eight regions: 51 of 105 native bytes, leaving 54 unmapped bytes. Unknown sites
retain source/function anchors, offsets and raw bytes and force incomplete output.
Any alignment mismatch is inconclusive: matching regions align linearly by
instruction position; no LCS, guessed insertion alignment or quadratic search.

Admitted operand facts:

- AMD64: full GP32/64 MOV, register ADD/XOR/CMP, register ADD/SUB/CMP immediate,
  base-plus-displacement GP MOV without indexing, JMP rel8/32, Jcc rel8/32, and
  three exact canonical NOP forms. Immediate sign extension, register reads/writes,
  address base/displacement, GP32 zero extension and flag reads/writes are retained.
- ARM64 comparison controls: MOVZ, non-SP ADD immediate, unsigned-immediate GP
  LDR/STR, direct B. W-register zero extension and SP/XZR distinctions are retained.
- Calls, returns, SIMD, partial GP writes, indexed memory and unsupported encodings
  remain unknown. Unknown raw changes are still reported. Native ARM64 capture and
  execution are not qualified; portable ARM64 controls and cross-builds are separate.

Raw bytes remain separate from normalized comparison bytes. Only JMP/Jcc/B with a
matching explicit stable target has displacement normalized; Jcc condition and
flag dependencies remain visible. Changes get a primary category: branch, width,
addressing, constant, register dependency, instruction or unknown. Categories are
an aid to inspection, not a complete list of simultaneous effects.

Comparison input is capped at 1 MiB, 64 regions, 4096 instructions and 512 changes
or unknown sites. Capture accepts at most 32 KiB Wasm, bounded module/local counts,
64 KiB emitted code and a 1 MiB / five-second objdump listing. Emitted size is
checked after compilation, so the source limits are not a compiler heap quota.
Capture hashes its executable by streaming, never reads the entire binary into
heap, and uses one compilation worker. Parsing, capture, normalization and change
reports allocate; equal lowercase register records in one region alone have a
zero-allocation comparison fast path.

Portable controls run automatically in the existing ordinary/checked `go test
./...` workflow. `just test native-compare` adds the opt-in native profile capture
checks and vet without changing CI configuration. Use a private writable GOCACHE
when sharing the machine. Focused reproduction:

```sh
GOMAXPROCS=1 go test -p=1 ./tests/tools/native-compare -count=1
GOMAXPROCS=1 go test -p=1 -tags=wago_profile,wago_regalloccheck ./tests/tools/native-compare -count=1
GOMAXPROCS=1 go test -p=1 ./tests/tools/native-compare -run '^$' -bench=BenchmarkCompare -benchmem -benchtime=100x -count=3
```

Full #815 requires more instruction/implicit-effect coverage, settled #719 artifact
interfaces and native ARM64 evidence. See [integration results](../../../evidence/integration/results.md)
and [initial experiment](../../../evidence/results.md).
