# Source layout experiment (not active by default)

These two alternative patches apply independently to the active b5ab03f9
compiler source in this branch. No compiler source is changed by storing
this experiment. Do not apply both patches together.

```
git apply --check diagnostics/stream-layout/hot.patch
git apply diagnostics/stream-layout/hot.patch
# Reverse before trying neutral.patch:
git apply -R diagnostics/stream-layout/hot.patch
```

The hot variant places eight declarations in an early file in fixed
profile-informed order. The neutral variant moves the identical set to a
late file in original file/declaration order; it is a relocation/alignment
control, not a dispersed control. Both retain function bodies, declarations,
initialization order, dependencies and build constraints. The generator can
be run on a separate clean copy using `go run generate.go ROOT hot MANIFEST`
(or `neutral`); the verifier takes ROOT and that generated manifest. Neither
script builds the compiler. Start from the specified base, not an already
patched tree. Generated manifests contain original declaration bytes for
verification; the stored compact manifests record their hashes.

Source checks verified 1,939 function inventory keys, non-import declarations
and init ordering, import dependencies, 686 original source headers and all
eight raw declaration bodies. Three standard-Go static builds retain all
9,463 named text symbols and every per-function size. The selected functions
sum to 39,129 bytes; their address span changes from 662,922 to 39,249 bytes
(hot) or 39,242 bytes (neutral). This is address clustering, not code-size or
resident-memory savings.

Conservative disassembly review resolved 117 neutral and 123 hot differing
RIP-relative targets by actual string/header payloads or exact consumed
constant widths. Two other functions redistribute NOP padding and relative
branch offsets; nonpadding instruction sequences and instruction-identity
branch destinations match. Thus machine bytes are NOT globally identical,
and alignment remains an experimental mechanism/confounder.

The recovered untimed public-API parity gate passed for all three arms:
145 pristine native outputs and mutual serialized metadata match per arm,
with 22 HostCall callbacks and trap/recovery checks per arm. Serialization
excludes some private/debug fields. An interrupted attempt was retained;
the recovered parity run is not a timing result. Production adapter runtime
oracle qualification remains separate. No timing/RSS outcome, target gain,
promotion or PR is claimed. No binary or profiling data is included.
