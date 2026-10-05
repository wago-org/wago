# RV64 scalar global import checkpoint

The production compiler and opt-in `wago_rv64_experimental` public binding admit
I32/I64/F32/F64 global imports through the existing aliased-cell directory.
Imported globals precede defined globals in Wasm index order regardless of where
a memory import appears among the declarations. I32/F32 reads use LWU and writes
replace the complete eight-byte cell with a zero-normalized carrier; I64/F64
retain raw bits. The ordinary framed ABI and FP/FCSR boundary are unchanged.

Modules retain the bounded scalar contract: at most 256 imported plus defined
globals and one ordinary memory32. A present import section contains 1..257
resource declarations; every declaration is checked, including unused globals.
The isolated diagnostic GuestContext continues to admit memory imports only.
Function/host/table/reference imports, v128, shared memory, memory64, starts and
global.get/extended initializers remain gated before image publication.
Trusted architecture-3 artifacts additionally check scalar import types and
their agreement with the leading global definitions and mutability metadata.

Numeric object imports retain their original owner. Host-owned global Close
rejects live importers; producer logical Close rejects new attachments while
existing borrowers retain its physical cells. Duplicate keys and distinct object
aliases use the existing attachment deduplication. Numeric value imports create
private cells in the consuming arena. Their re-export now enters the normal
exact-owner publication path, which retains that arena for downstream borrowers
and revokes independent execution. The final borrower releases the cells. No new
collector or register-entry proof is advertised.

Literal-only construction fills the consumer's private global directory without
writing imported cells. Combined imported-memory construction uses the separately
reviewed execution lease and complete context snapshot. Private defined globals
remain isolated when consumers share JobMemory and imported global objects.
Failed active-data initialization leaves imported cells unchanged and detaches
their roots before returning.

Focused portable public fixtures cover four mixed scalar types, host objects and
value forms, artifact round trips, live calls after Compiled.Close, raw signaling
NaNs/signed zero, dirty incoming upper words, full narrow-cell normalization,
host writes, zero warm allocations, type/mutability/missing-binding rejection,
value re-export lifetime, producer Close and final-root release. Mixed memory
imports appear first, middle and last; consumers observe shared global writes
while retaining different defined-global values. The 256-global fixture executes
global.get255 and checks a single duplicate-key attachment. RV64 rejects both
257 imports and 256 imports plus one defined global; host-independent backend
tests assert those same aggregate bounds on valid modules.

Independent admission and runtime/ownership reviewers accepted the source and
focused regressions. Qualification logs are `evidence/imported-global-*.log`:
host encoder/compiler and full compiler QEMU tests, focused public QEMU tests in
ordinary and checked modes, AMD64 shared-compiler reference/race checks,
existing global-import compatibility tests, ARM64 compilation, vet/static checks,
repository RV64 build and diagnostics/DCE/documentation checks. These establish
bounded scalar correctness. Full production parity, official broad suites and
native cache/performance qualification remain pending in the
[parity tracker](riscv64-parity-tracker.md).
