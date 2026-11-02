# Non-amd64 execution qualification

Source pin: `0fbc269b2656e7ffe0aa455576b4f75fb9b0d7b9` on
`experiment/rfwgc-port`. This combines Stage 7 with the checked H6 foundation,
not H6's later provisional contention-wakeup repair. H2 and H6 remain separately
owned gates. This is not the final combined-head rerun or a ready-PR verdict.

## Execution environment and reproduction

Linux x86-64 host, Go 1.27.3 cross compiler, Debian QEMU user 10.0.13.
`package-index.txt` records the Debian index checksum, matching the package in
`source.json`. No system install, binfmt registration, shared configuration,
credentials, worktree creation, push, PR, subagent, or performance run was used.
Linux ARM64 runs with QEMU `-cpu max`; RV64 follows the repository baseline
`rv64,zba=false,zbb=false,zbc=false,zbs=false,v=false`. These are emulated Linux
results, not native hardware or performance evidence. Test framework durations
are incidental correctness output, not collected timing samples.

Run from the branch worktree with a Debian qemu-user package matching the recorded
SHA256 (available from the Debian pool path in `package-index.txt`):

```sh
python3 docs/rfwgc-evidence/stage8-platform-execution/run.py --pin 0fbc269b2656e7ffe0aa455576b4f75fb9b0d7b9 --qemu-package /path/to/qemu-user.deb --out /tmp/platform-collector-run
python3 docs/rfwgc-evidence/stage8-platform-execution/run-guests.py --pin 0fbc269b2656e7ffe0aa455576b4f75fb9b0d7b9 --qemu-package /path/to/qemu-user.deb --out /tmp/platform-guest-run
python3 docs/rfwgc-evidence/stage8-platform-execution/run-rv64-guests.py --pin 0fbc269b2656e7ffe0aa455576b4f75fb9b0d7b9 --qemu-package /path/to/qemu-user.deb --out /tmp/platform-rv64-guest-run
```

Each driver archives the commit into a fresh temporary export and unpacks its
own QEMU copy. Each output directory must be new. `source.json` records the
archive, adapter, driver, package and Go identities; `results.json` records exact
commands, exits and individual pass/skip/fail lines. Binary hashes and checked
ELF machine IDs (183 ARM64, 243 RV64) accompany actual QEMU commands.
`TestPlatformExecutionArchitecture` independently asserts runtime.GOOS/GOARCH
inside each guest test binary. There is no `-exec=true` substitution.
`driver-final.json` verifies the driver was unchanged for its run.

The collector suite is the full native package, including independent graph
oracles, actual helper participation, epoch/phase and deterministic failure gates,
source-owner callbacks, mature movement, rollback and 256-MiB capacity. Builds
are retained as separate controls and never counted as execution passes.
The guest selector covers architecture-available rooting and host reentry plus
the Stage 7 graph and post-mature-movement JIT read fixtures. Those fixtures
assert 60001 live objects, actual helper work and a successful guest read after
nonzero mature movement and native-view refresh.

## Test-only portability adapter and retained failed attempts

The committed Stage 7 guest file is AMD64-gated. The export gets a second copy
with ARM64/RV64 build constraints. No existing source file is edited. The corrected
`run-guests.py` also removes the AMD64-only `dead-gc-new` option; target backends
do not register that option. All graph, participant, pressure, movement and
result assertions remain unchanged. The original fixture hash and adapter hash
are recorded. This is test adaptation, not a production fix or wholesale AMD64
backend transplant.

`initial/` stopped before execution because the optional `file` utility was
missing. Its driver and exit1 are retained. The corrected main driver validates
ELF e_machine directly. `qualified/` is authoritative for collector execution,
but its guest runs retain setup failures: wrong current directory for relative
fixtures and unknown architecture-specific optimization. `guest-replay/` is the
separate corrected guest run, using package-directory cwd, authoritative for ARM64.
Its RV64 admission failures remain retained. `rv64-guest-replay/` is the final
RV64 replay: pinned `gc_rooted_scalar.go:1068` limits constant-expression
array.new_default to 256 entries. The original 60000-element global initializer
is therefore unsupported. The RV64-only adapter initializes the global to null
and performs the same 60000-element allocation in guest `run` before the original
loop. It retains every graph-size, helper, pressure, relocation and read assertion.
This tests supported guest execution without weakening or editing admission.
The rejection is a source capability boundary, not an emulator or collector
regression. Native hardware is still not qualified by this emulation.
All driver commands reproduce their respective attempts, including historical
failures; driver completion alone is not suite success. `summarize.py` checks
the mandatory named execution gates in the authoritative directories. No running driver was
edited and no earlier failure is silently relabeled PASS.

## Boundaries

Only `docs/rfwgc-evidence/stage8-platform-execution/` is owned by this task.
All production files, shared log, index entries and other workers' dirt are
preserved. Read-only sibling pins/excerpts are retained in `sibling-*`; Slipstream
has a different raw-address/native collector representation, and Starshine's
boundary document describes model/validation rather than this runtime. Harness
setup failures do not establish sibling or Wago engine defects.

`availability.txt` records runner availability. Repository CI declares native
Linux ARM64/RV64 and Darwin ARM64 hosts, but no remote execution endpoint or
credentials were supplied or used here. Linux-user QEMU does not qualify Darwin
Mach-O execution or Darwin executable-mapping policy. Darwin ARM64 execution
remains open. Race, full corpus/conformance, full backend suites, TinyGo, final
H6 wakeup/H2 repairs and the combined final-head rerun are separate gates.
No performance claim follows from emulation. This evidence/tooling-only change
cannot affect production runtime performance.

## Scoped final result

All twelve authoritative execution commands exited 0. Counts include subtests;
all required named gates passed. No guest fixture skipped.

| Target | Mode | Collector pass / skip | Guest pass / skip |
| --- | --- | ---: | ---: |
| arm64 | ordinary | 2200 / 0 | 126 / 0 |
| arm64 | wagodebug | 2282 / 3 | 126 / 0 |
| arm64 | wago_regalloccheck | 2200 / 0 | 126 / 0 |
| riscv64 | ordinary | 2145 / 5 | 81 / 0 |
| riscv64 | wagodebug | 2223 / 9 | 81 / 0 |
| riscv64 | wago_regalloccheck | 2145 / 5 | 81 / 0 |

Exact skip names and failures from historical attempts remain in the per-run
ledgers and logs. `summary.json` selects only the authoritative collector and
corrected guest runs. Both supported Linux target matrices execute JIT code;
cross-build success alone is never credited as execution. Source/driver
verification passed for all three completed exports. Their paths are retained
in source.json for inspection; no production edit or emulator capability blocker
was established. Final combined-head rerun remains required after active workers
finish. No H2 or H6 contention-wakeup gate closes from this result.
