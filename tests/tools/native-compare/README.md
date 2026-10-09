# Offline operand comparison experiment (#815)

This local prototype compares instruction records from existing disassembly and
source maps. It adds no compiler/runtime instrumentation. It is a diagnostic,
not a semantic equivalence checker or automatic performance diagnosis.

From the repository root:

```sh
go run ./tests/tools/native-compare evidence/real-before.json evidence/real-after-control.json
```

The output is JSON. Exit 0 means the admitted comparison is complete (it may
contain changes), 2 means invalid input, and 3 means incomplete coverage or a
resource limit. `fib-snapshot.json` compared with itself deliberately exits 3:
unknown instructions cannot establish equivalence.

Each snapshot supplies architecture, compiler revision and binary SHA-256,
loaded input SHA-256, configured CPU mask, bounds mode, build/profile mode,
compiler path, and named source regions. Each region has a function index,
Wasm offset and an ordered instruction list with native offset and raw hex.
See the retained evidence JSON for a complete example. Wasm offsets include
local-declaration bytes, matching the existing source-map contract.

The producer supplies instruction boundaries and source/target identities.
These are assertions, not independently authenticated artifact identities.
The real fixture tests obtain boundaries from existing GNU objdump and ranges
from current compiler metadata, and independently check selected Wasm/native
operation attribution. They do not prove every source map or branch target.
The command does not disassemble or load native code and does not execute,
map or upload supplied artifacts.

## Admitted instruction subset

- AMD64: 32/64-bit MOV immediate and register forms; register ADD/XOR;
  base-plus-displacement GP MOV loads/stores without indexing; direct JMP rel32.
  GP32 destination writes record zero extension. XOR of the same full GP
  register has no input data dependency; arithmetic flag writes are recorded.
- ARM64: MOVZ, non-SP ADD immediate, unsigned-immediate GP LDR/STR, direct B.
  W-register writes record zero extension; SP addressing differs from XZR.
- Calls, returns, SIMD, partial GP writes, conditional branches, indexed memory,
  unsupported prefixes/encodings and general implicit effects remain unknown.
  Ordinary retained raw bytes still expose changes to unknown instructions.

The records retain raw bytes, opcode, operand widths, register reads/writes,
immediate bits, addressing displacement and base, and zero-extension/flag facts.
`comparison_encoding` is separate from raw bytes. Only an admitted direct
JMP/B with an explicit stable logical target identity has its displacement
normalized. Different targets, missing identities and changed opcodes do not
compare equal. No register renaming or broad constant normalization is applied.

Matching function/source/region identities are aligned linearly by instruction
position. Changed counts, missing anchors, unmapped ranges, unknown semantics
and incompatible configuration make the comparison incomplete. Insertions are
not guessed. No quadratic sequence alignment is used. `complete` applies only
to the supplied regions; omitted/unmapped image bytes are not qualified.

Input JSON is capped at 1 MiB, 64 regions, 4096 total instructions, 15 bytes per
AMD64 instruction or four aligned bytes per ARM64 instruction, and 512 reported
changes. Regions must be ordered and nonoverlapping; records in each region
must be contiguous. Input parsing, explicit branch normalization, changed
reports, uppercase hex and multiple-region maps may allocate. The zero-allocation
benchmark covers only equal lowercase register records in one region.

## Reproduce focused checks

```sh
go test ./tests/tools/native-compare -count=1
go test -tags=wago_profile,wago_regalloccheck ./tests/tools/native-compare -count=1
go vet -tags=wago_profile,wago_regalloccheck ./tests/tools/native-compare
go test ./tests/tools/native-compare -bench=BenchmarkCompare -benchmem -benchtime=100x -count=3
```

Use `GOMAXPROCS=2`, `-p=2`, and a writable private `GOCACHE` when sharing this
machine with other agents. The profiled native tests need existing GNU objdump
and Linux AMD64. ARM64 control decoding and cross-building do not establish
native ARM64 execution coverage. The ordinary runtime build remains identical.

Full #815 integration with #719's settled artifact/provenance interfaces,
broader instruction semantics, other native targets and richer bounded
alignment remain future work. No production optimization or issue closure is
claimed. See [local results](../../../evidence/results.md).
