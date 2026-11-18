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

## Completed outcome: no qualified benefit

The three frozen variants were measured with standard Go 1.27.1, explicit
PGO off, all twenty experimental flags off and the full API preserved.
Baseline is b5ab03f9 with legacy lowering, NOT pristine main or qualification
against the original 40% target. The correctness recovery after executor
interruption was separately journaled; measurements were not retried.
All 540 long-execution, 2,900 cold-compile, 435 independent RSS and 1,305
page-observation records verified. Active source and these patches are unchanged.

Cold compilation uses the arithmetic mean of per-workload median times over
145 fixed inputs, with 5,000 paired within-workload trial bootstrap samples.
Hot/baseline is 0.998157 (95% CI [0.955212, 1.049582]); neutral/baseline is
0.999607 [0.946322, 1.159248]; hot/neutral is 0.998549
[0.850836, 1.052761]. Baseline A/A is 1.018854 [0.959371, 1.137100],
failing the control gate. There is no established compilation benefit.

Both layouts meet the <=5% execution guard on the five control-qualified
cases: Monocypher, pcre2, nanosvg, drwav and fastfloat. The other ten of the
fixed fifteen cases remain inconclusive because baseline controls fail.
For example, AES hot/baseline is 0.962782 [0.925475, 0.997319], but failed
controls prevent a qualified win. No evidence distinguishes beneficial
profile-informed ordering from placement/alignment noise. Inherited subgroup
names describe cohort provenance, not current streaming feature selection.

The separate one-trial, 145-process-per-arm RSS measurement gives absolute
compiled-barrier arithmetic-mean VmHWM of 12.099758 MiB baseline,
12.099946 MiB neutral and 12.100027 MiB hot. No material reduction or
absolute <5 MiB success is demonstrated.

Three balanced repetitions of the 145-input page census independently show
the selected eight functions' occupied page union shrinking from 17 to 11
in both layouts. Nevertheless, all 341 backend pages were already resident
before Compile in every observation. Whole text residency at the compiled
barrier is 1,293 pages in every arm. Before-Compile follows protocol/prepare,
not pristine startup, and fixed-slot executable writing/hashing prewarms
the file cache consistently. Presence of pages neither proves every symbol
executed nor identifies fault-around as the cause. Overlapping page sets
must not be added, and residency intersections do not reconstruct transient
high-water composition. The page census is not latency evidence and is
separate from the 435-process RSS measurement.

Reject this bounded eight-function layout experiment as an improvement.
Geometric clustering worked; measured compile and RSS targets did not.
This does not rule out every possible layout strategy. No promotion or PR
is proposed, and no variant has become the active source.
