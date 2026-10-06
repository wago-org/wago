# Dragline handoff — 2026-10-06

Branch: `jairus/dragline-mvp`. This is a checkpoint for continued work, not a
claim that the performance objective is finished.

## Status

The target is **50% higher execution throughput than updated Railshot PR #780**
(`efa9aa22dfb3781284a55c465f7f57544eafade3`), measured across all 65 modules and
72 exports. The module-balanced paired time ratio must be at most **2/3**.
**The target is unmet.**

There are three distinct source states:

| State | Location | Evidence |
| --- | --- | --- |
| Physical v77 checkpoint | Normal `src/` files on this branch | Preserved working source; not the latest experimental overlay |
| Retained experimental reference | `divisibility-02/` in the evidence archive | Historical matched full run: time/PR780 0.9159781825, or 9.17% higher throughput |
| Latest candidate | `wide-setup-01/` in the evidence archive | Native AMD64 correctness qualified; performance and compile cost unmeasured |

The latest candidate folds proven I32 entry constants only during vector-loop
setup and replaces passing constant trip-count checks with their exact count.
It changes 25 modules and removes 5,712 native bytes. It includes the two
qualified structured-emitter correctness fixes from `direct-wrapper-02`, while
the nested-loop, call-tree, and direct-wrapper optimization policies are off.
Word-copy and quotient prototypes are not included.

Native candidate and retained policies passed:

- All 65 modules / 72 exports and 21 semantic cases.
- 55,836 independent byte-memory/trap/exit-state calls per policy.
- Twenty FP fixtures, each with 2,160 cases in two bounds modes, including
  full memory, traps, tails, NaNs and returned state.
- Compiler/runtime suites, 90,000 constant-expression checks, 1,024 trip-guard
  cases, and dedicated emission/proof checks.

These are preserved qualification results, not tests rerun during this handoff.
Rosetta and ARM results are distinct from native AMD64 evidence.

## Restore the evidence

The full 103,101-file archive is preserved losslessly as deduplicated zstd
parts under `docs/performance/dragline-20261003/evidence/`. The parts are each
at most 40 MiB; the bundle index pins their sizes and SHA-256 hashes. Expanded
evidence occupies about 2.4 GB and is intentionally ignored by Git.

Requirements: Python 3.9+ and `zstd` on PATH.

```sh
python3 scripts/restore-dragline-evidence.py
```

The restorer verifies transport, manifest, and every unique object. It preserves
existing matching files and refuses to overwrite changed files. Use
`--verify-only` to check the archive without restoring it, or `--destination`
to restore elsewhere. The existing expanded copies on the original workstation
were kept; the full packing backup is also at
`/Users/work/.codex/artifacts/dragline-handoff-20261006/`.

Start with these restored paths under `docs/performance/dragline-20261003/`:

- `wide-setup-01/README.md`, `metadata.json`, `native-verification.json`:
  latest source and qualification.
- `wide-setup-01/measure.py`: prepared screen, compile, and full comparison.
- `wide-setup-screen-01/` and `wide-setup-full-preflight-01/`: failed host
  activity gates; **zero timing samples** were taken.
- `divisibility-full-02/`: last retained matched full performance result.
- `integer-alias-01/` and `direct-wrapper-02/correctness-fixes.diff`:
  structured integer alias and FP call-preservation fixes.

The [research log](dragline-optimization-research-2026-10-03.md) records rejected
experiments and known issues. Search it before repeating an optimization. The
archive preserves unfavorable samples and failed harness runs as well as passes.

## Prepare the candidate in a fresh checkout

Historical overlay JSON files contain old absolute paths. Reconstruct one:

```sh
python3 scripts/prepare-dragline-checkpoint.py --out /tmp/dragline-wide-setup
. /tmp/dragline-wide-setup/env.sh
```

This checks the physical and effective Go source hashes and writes portable
absolute mappings for this checkout, plus a dedicated unit-test overlay. It
does not modify production Go files, build, or run tests. `--policy retained`
selects the control in the same candidate source. Use a fresh output directory.

On native Linux AMD64, with Go **1.22.2** installed at the recorded path, the
benchmark executable can be rebuilt with:

```sh
(cd bench/suite && /usr/lib/go-1.22/bin/go test -vet=off \
  -overlay /tmp/dragline-wide-setup/overlay.json -tags wago_guardpage \
  -c -o /tmp/dragline-wide-setup/suite.test .)
/usr/lib/go-1.22/bin/go version -m /tmp/dragline-wide-setup/suite.test
```

The archived `native-runner.py` and other old runners document exact commands
but assume the existing hub directory layout. Adapt paths and rerun native
qualification if rebuilding elsewhere or changing sources. The general
`scripts/dragline-iterate.py` uses physical sources and is **not** a replacement
for the latest overlay's pinned PR780 comparison.

## Resume measurements on the existing native host

Last recorded native layout (2026-10-05):

- Host: `hub@hub`, Ryzen 7 7800X3D, CPU 7 used for this work.
- Checkout: `/home/hub/wago-dragline-a82a-20261003`.
- Candidate: `wide-setup-01/suite.test`, Go 1.22.2.
- PR780 binary: `pr780-baseline-02/suite.test`, also Go 1.22.2.
- PR checkout: `/home/hub/wago-railshot-pr780-efa9aa22`.

Both measurement attempts stopped before execution because unrelated work used
about 5.4 cores and CPU 7 averaged about 26% background activity. The performance
goal was marked blocked pending a quiet host. All our recorded native jobs are
terminal; current host activity has not been inferred from those old samples.

Once the host is quiet:

```sh
cd /home/hub/wago-dragline-a82a-20261003
python3 wide-setup-01/measure.py "$PWD" screen wide-setup-screen-02
```

Review all 25 changed modules and two unchanged controls before choosing the
next action. The same runner supports `compile` (all 65 modules, four alternating
rounds, three compilations per sample) and `full` (all 65 modules / 72 exports,
six balanced candidate/retained/PR780 orders). Give each run a fresh directory.
Keep one native benchmark job active at a time. Do not disturb other users'
processes to obtain a quiet host.

The runner validates source/binary/corpus pins and Go versions before timing.
It checks host activity before and during execution and preserves partial
samples if interrupted. Review spread, unchanged controls, compile costs and
all regressions before retention. Whole-process RSS is not isolated compiler
memory. Earlier mixed Go 1.22.2/1.27.1 comparisons are not valid matched evidence.

## Exact source and binary identities

| Item | SHA-256 |
| --- | --- |
| Physical Go source | `41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f` |
| Effective wide-setup01 Go source | `9210c2ec6df27f4aa2c55f7253a39c466505e8a314ad5eefd3d1ad145a571ad9` |
| Qualified native candidate binary | `f267bc4d9138445fa360c02d2d81ae629b5cf9ad33a19645f502d14833913c9d` |
| Pinned PR780 Go 1.22.2 binary | `6b37ada3508f8714c140af3150ae73e9a793c4c03ed123312af622de233adc6a` |

Known independent issues remain recorded in the research log: physical v77 ARM
NanoSVG failure, intermittent Darwin interrupt cancellation, and Railshot
constant-I64 cross-page partial-store behavior. Do not weaken correctness
oracles or claim full ARM corpus qualification to work around these issues.
