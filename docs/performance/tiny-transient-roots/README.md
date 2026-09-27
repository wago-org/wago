# Tiny transient roots: PR #685

Tiny stages transient root handles before it changes a collection cycle. Each
source is read once per phase. An incomplete or panicking read leaves the active
epoch, marks, gray queue, scan cursor, and sweep cursor intact. The same rule
applies during incremental remark.

The staging buffer reuses the collector's mark stack storage. It keeps capacity
until `Close`, but clears its length after each attempt. Capacity grows with the
largest enumeration, including duplicate live roots. Null, immediate, freed,
and out-of-range references are excluded.

## Tests

The root staging tests cover one-shot direct, classified, and grouped sources;
reachable children; release on the next rootless cycle; large buffer reuse;
incomplete reads before and after a supplied root; nested rejection; remark
retry; and panic cleanup. Epoch recovery tests remain with the other epoch
tests. The telemetry-only panic test has its own build-tagged file.

For focused validation, run the native GC package with each build combination:

- `go test -count=1 ./src/core/runtime/gc/native`
- `go test -count=1 -tags=wago_tiny_nonincremental ./src/core/runtime/gc/native`
- `go test -count=1 -tags=wago_gcstats ./src/core/runtime/gc/native`
- `go test -count=1 -tags=wago_gcstats,wago_tiny_nonincremental ./src/core/runtime/gc/native`

`python3 docs/performance/tiny-transient-roots/verify_mutations.py` checks
four regressions with temporary Go overlays: the old double read, discarded
marks on rejected input, missing restart color clears, and unconditional epoch
advance. All four were caught by assertions in both Tiny build modes. The
script needs local Git objects for main `30919ef8c5e9` and the earlier PR
revision `1180bab1d56a`. It does not edit the worktree.

## Performance

Focused Linux/amd64 measurements used an AMD Ryzen 7 8845HS, Go 1.27.1,
`GOMAXPROCS=1`, and five serial 200 ms samples per benchmark. The table gives
median ns/op. Each historical variant used identical final benchmark sources
and overlaid only the earlier production sources.

| Build | Workload | Main | Before staging | Staged |
| --- | --- | ---: | ---: | ---: |
| Default | Original direct root | 105.8 | 94.62 | 104.5 |
| Default | Warm buffer, 1 root | 83.20 | 74.45 | 83.89 |
| Default | Warm buffer, 256 roots | 2,415 | 1,716 | 2,341 |
| Default | Warm buffer, 4,096 roots | 37,287 | 26,545 | 36,210 |
| Default | Retained graph, 5 rejected restarts | 80,794 | 87,140 | 80,741 |
| Nonincremental | Original direct root | 71.87 | 62.51 | 67.57 |
| Nonincremental | Warm buffer, 1 root | 50.54 | 43.72 | 49.80 |
| Nonincremental | Warm buffer, 256 roots | 1,315 | 851.6 | 1,126 |
| Nonincremental | Warm buffer, 4,096 roots | 20,259 | 12,991 | 17,302 |
| Nonincremental | Retained graph, 5 rejected restarts | 37,075 | 39,901 | 37,023 |

Warm-buffer cases reported zero allocations per operation. The original direct
root benchmark reported one allocation and 24 bytes per operation in all three
variants because it boxes the root slice inside the loop. Cold buffer growth
retained 8 bytes for one root, 1,024 for 256, and 16,384 for 4,096 on this Go
version. These focused measurements do not establish application speedup.

After the final size and panic-telemetry changes, the direct-root median was
97.12 ns/op versus 101.7 before those changes in the default build, and 63.26
versus 63.66 in the nonincremental build. This separate check used
`GOMAXPROCS=16`; do not compare its values with the first table.

## Size and validation

The final `runtime-minimal-tiny` binary was 2,351,328 bytes under the
2,352,000-byte limit with Go 1.22.12 and TinyGo 0.41.1. The earlier revision
was 2,352,560 bytes. The full `scripts/size-card.sh` gate passed with that
toolchain. Local validation also covered the GC race suite, runtime and public
API tests, targeted TinyGo tests, eight mutation checks, and lint. The full
unit gate passed with the pinned Go/TinyGo pair. A newer local TinyGo linker
failed two unchanged baseline tests with a duplicate `tinygo_task_exit`
symbol; those failures reproduced on main.
