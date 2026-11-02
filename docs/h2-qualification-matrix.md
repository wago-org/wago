# H2 qualification matrix

Base `35c36bc594d2a027317cfcd321379f2d40c6b219`, production `501e9ab46`. Commands below qualify implementation commits on immutable source exports. This support branch contains documents only. Existing tests are distinguished from tests the two proposed slices must add; a zero-match or skipped required test is never a pass.

The current baseline already has module-owned publication hooks. Host-created Table/Global support needs new acceptance tests. Migration remains adapter regression coverage, not a requirement to add production relocation. Final combined qualification remains the parent's responsibility, including zero unresolved review findings.

## Scoped commands for each piece

Run from an immutable export of the finished candidate. Use Go 1.27.3 and the pinned WABT 1.0.41 from the campaign. These test names are selected directly from the current source; names beginning `TestR9H2HostContainer` or `TestR9H2HostImport` are the new slice contract in `h2-parallel-slices.md`.

```bash
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.3
export GOTOOLCHAIN=local GOWORK=off
export PATH="$GOROOT/bin:/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:$PATH"
```

| Piece | Existing baseline regex | Required new regex on completed slice | Required behavioral oracle |
| --- | --- | --- | --- |
| Global publication and public global store | `^TestR9H2StoreHooksOriginalProvider$` | `^TestR9H2HostContainerPublicGlobal` and `^TestR9H2HostImportGlobal` | Publisher physically retires; surviving physical global still resolves/collects; replacement/null clears exact claim; immutable/type/nullability failures leave storage unchanged. |
| Imported and public table stores | `^TestR9H2StoreHooks(OriginalProvider|IndependentPhysicalTables)$` | `^TestR9H2HostContainerPublicTable` and `^TestR9H2HostImportTable` | Aliases share one receipt; independent allocations do not; older collector-free provider works; cross-Runtime tokens reject; public set survives publisher retirement. |
| Bulk fill/copy/init/grow | `^TestR9H2StoreHooksBulk$` | `^TestR9H2HostImportBulk` | Full-range prepare before writes, overlap both directions, source snapshot, failed bounds/grow rollback, grow length last, no double-release on repeated values. |
| Active initialization | `^TestR9H2StoreHooksActiveInitialization$` | `^TestR9H2HostImportInitialization` | Each segment stages before publication, prior completed segments survive later trap, failed construction releases only its ownership, imported host containers remain usable. |
| Storage identity and migration | `^TestR9H2StoreHooksDescriptorMigration$` | `^TestR9H2HostContainerMigration` | Canonical receipt object/index survives key change; failed migration preserves old key/aliases; current grow uses reserved capacity without relocation. New test applies only if host authority changes migration behavior. |
| Converted cycles and final release | `^TestR9H2Receipt(ConvertedProducerCycle|PublisherRetirement|CloseOrders|FinalJournalRemainsCollectionAuthority)$` | `^TestR9H2HostContainer(Cycle|Close)` and `^TestR9H2HostImportCycle` | Real guest publication, exact physical-container root, publisher-independent domain, external root keeps cycle alive, final clear/Close releases cycle/domain exactly once without another invocation. |
| Contention and authority | `^TestR9H2Receipt(OnlyContention|IndependentContainersAndRollback|DomainKeepsPrivateTypeAdmission)$` | `^TestR9H2Host(Container|Import)Authority` | Complete ordered pre-admission, no helper waits under partial leases, retry/drain after unlocking, no lost wakeup and no private type-domain admission leak. |

For **each individual regex cell** above, execute the following exact command family. Assign `rx` to that cell's literal regex (run both when a cell says “and”). This expands to ordinary, wagodebug and wago_regalloccheck, each with normal and race execution. New regex cells are pending until implemented; the list guard deliberately fails if a worker forgot the test. Migration's new cell is conditional as stated; the existing migration cell is always required.

```bash
rx='^TestR9H2StoreHooksBulk$' # replace with the selected cell, retaining its anchors
for mode in ordinary wagodebug wago_regalloccheck; do
  tags=()
  if [ "$mode" != ordinary ]; then tags=(-tags "$mode"); fi
  selected=$(go test "${tags[@]}" ./src/wago -list "$rx") || exit 1
  printf '%s\n' "$selected" | rg '^Test' >/dev/null || exit 1
  go test "${tags[@]}" ./src/wago -run "$rx" -count=1 -v -timeout=3m || exit 1
  go test "${tags[@]}" -race ./src/wago -run "$rx" -count=1 -v -timeout=3m || exit 1
done
```

Require actual PASS assertions for every selected case in the supported AMD64/ARM64 host lane, not only process exit zero. Enumerate skipped subtests and their admission conditions. Expand the existing per-region cases rather than converting unsupported-region skips into supposed coverage. Slice A can pass its own direct public/root-lifecycle cells before B; B's new host-import cells become mandatory on A+B. Existing baseline cells remain mandatory on either slice.

For B's backend call-frame/helper contract, use this separate exact package command in all three modes. New contract tests must exist in both packages; compile-only handling of architecture-specific execution remains explicit.

```bash
for mode in ordinary wagodebug wago_regalloccheck; do
  tags=()
  if [ "$mode" != ordinary ]; then tags=(-tags "$mode"); fi
  for pkg in ./src/core/compiler/backend/railshot/amd64 ./src/core/compiler/backend/railshot/arm64; do
    selected=$(go test "${tags[@]}" "$pkg" -list '^TestExternHostStoreContract') || exit 1
    printf '%s\n' "$selected" | rg '^TestExternHostStoreContract' >/dev/null || exit 1
    go test "${tags[@]}" "$pkg" -run '^TestExternHostStoreContract' -count=1 -v -timeout=10m || exit 1
  done
done
```

## Immediate full slice gate using the existing driver

Run these commands from a repository checkout containing the candidate commit. Replace the two shell arguments with the full candidate SHA and a fresh absolute evidence directory. Use A's or B's reserved evidence directory, never the active combined worker's directory. Drivers reject pre-existing output directories. The integrated driver exports the pin and pinned corpus gitlinks; it does not test dirty worktree source.

```bash
pin=$(git rev-parse HEAD)
evidence=/tmp/wago-h2-candidate-qualification # must not already exist
python3 docs/rfwgc-evidence/r9-h2-production/qualify-integrated.py \
  --pin "$pin" --out "$evidence/integrated"
```

This unchanged driver runs the following six commands for each mode (18 total), inserting `-tags wagodebug` or `-tags wago_regalloccheck` after `go test` for the corresponding mode:

```bash
go test ./src/wago -run '^Test(R9(H2|Ownership)|GCExternConversion)' -timeout=3m -count=1 -v
go test ./src/wago -timeout=20m -count=1 -v
go test -race ./src/wago -run '^Test(R9(H2|Ownership)|GCExternConversion)' -timeout=3m -count=1 -v
go test ./src/core/compiler/backend/railshot/amd64 ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/wasm -timeout=10m -count=1 -v
GOOS=linux GOARCH=arm64 go test ./src/wago -run '^$' -exec=true
GOOS=linux GOARCH=riscv64 go test ./src/wago -run '^$' -exec=true
```

Both new H2 test prefixes match the existing focused driver automatically. Cross commands are compile-only, not target execution. Full corpus setup comes from the driver: spec interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`, WABT 1.0.41, archived spec-v1/v2/v3 gitlinks, and cleared baseline-update/filter environment variables. Do not regenerate expectations to hide failures. Require `driver-final.json` to show unchanged driver, no export changes, all_pass true and 18 commands.

## Actual ARM64 and RV64 QEMU execution

Use the same candidate pin after scoped tests exist. The campaign package is `/tmp/qemu-user.deb`, recorded SHA256 `ca6ede739327a20ae5a3498230c8a3a9a072c586e131bc6bcc1201eb1725e336`; verify the hash before use. The existing driver compiles the pin for each architecture and checks ELF machine 183/243 plus a runtime architecture assertion.

```bash
printf '%s  %s\n' ca6ede739327a20ae5a3498230c8a3a9a072c586e131bc6bcc1201eb1725e336 /tmp/qemu-user.deb | sha256sum -c -
python3 docs/rfwgc-evidence/r9-h2-production/qualify-targets.py \
  --pin "$pin" --qemu-package /tmp/qemu-user.deb --out "$evidence/targets"
```

The exact target selector is:

```text
^(TestPlatformExecutionArchitecture|TestR9H2.*|TestGCExternConversion.*|TestRV64ManagedFunctionJoint.*|TestRV64CompactExtern.*|TestRV64CompactConvertedExternShellReclamation|TestRV64CompactForeignExternPinAndReclaim)$
```

For each of ordinary/wagodebug/wago_regalloccheck and each architecture, the driver expands this command family using its private extracted emulator and binary paths:

```bash
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -p=2 -o "$binary" ./src/wago
WAGO_EXPECT_ARCH=arm64 WAGO_RV64_TEST_RUNNER=qemu "$qemu/usr/bin/qemu-aarch64" -cpu max "$binary" -test.v -test.count=1 -test.timeout=12m -test.run="$target_rx"
GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 go test -c -p=2 -o "$binary" ./src/wago
WAGO_EXPECT_ARCH=riscv64 WAGO_RV64_TEST_RUNNER=qemu "$qemu/usr/bin/qemu-riscv64" -cpu rv64,zba=false,zbb=false,zbc=false,zbs=false,v=false "$binary" -test.v -test.count=1 -test.timeout=12m -test.run="$target_rx"
```

Here `binary`, `qemu`, and `target_rx` denote values set by the driver, not additional runnable setup instructions. Nonordinary builds add `-tags=MODE`. Execution cwd is the exported `src/wago`; GOMAXPROCS=4 and GOWORK=off. There are 14 recorded commands: two emulator version checks, six builds and six executions. The driver has a test-only stage7 portability adapter whose original and adapted hashes are recorded; this is not a production source change.

ARM64 must execute the new host tests and all existing H2 selectors. RV64 exercises its separate compact-extern/joint-ownership implementation; host opaque receipt tests are build-excluded, so its successful run cannot qualify those adapters. Require every expected architecture/mode execution row, ELF check and architecture probe. This driver records failing commands without necessarily exiting nonzero: inspect the ledger, not just its shell status.

## TinyGo compatibility and ledger checks

```bash
python3 docs/rfwgc-evidence/r9-h2-production/qualify-tinygo.py \
  --pin "$pin" --out "$evidence/tinygo"
```

It uses the campaign's patched TinyGo binary `/home/dev/.local/share/wago-tinygo-investigation/tinygo/bin/tinygo`, Go 1.27.1 and explicit guest tags. Commands are:

```bash
tinygo test -scheduler=threads -gc=conservative -tags=wago_runtime -run='^TestGCExternConversion' -v ./src/wago
tinygo test -scheduler=threads -gc=conservative -tags=wago_runtime,wagodebug -run='^TestGCExternConversion' -v ./src/wago
tinygo test -scheduler=threads -gc=conservative -tags=wago_runtime,wago_regalloccheck -run='^TestGCExternConversion' -v ./src/wago
```

This is compatibility smoke; TinyGo host receipt stubs are excluded. It does not replace the 34-command foreign-stack prerequisite or final combined TinyGo gate. The ledger must contain three zero-exit modes with unchanged driver and all_pass true. For all drivers, archive failures and interruptions rather than overwriting them with a later pass.

After the three drivers finish, this read-only ledger check catches missing/failed commands. It supplements the required per-test PASS/skip inspection and new-prefix census above.

```bash
python3 - "$evidence" <<'PY'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
for lane, expected in [('integrated', 18), ('targets', 14), ('tinygo', 3)]:
    rows = json.loads((root/lane/'results.json').read_text())
    assert len(rows) == expected, (lane, len(rows))
    assert all(row['exit'] == 0 for row in rows), lane
    final = json.loads((root/lane/'driver-final.json').read_text())
    assert final['driver_unchanged'], lane
    if lane != 'targets':
        assert final['all_pass'], lane
    if lane == 'integrated':
        assert not final['export_changes'], lane
PY
```

## Evidence, cleanup and handoff

Record candidate full SHA, driver hashes, source archive hash, toolchain/QEMU identities, exact commands, per-mode PASS/FAIL/SKIP counts and the required new test census. A owns `docs/rfwgc-evidence/r9-h2-host-containers/`; B owns `docs/rfwgc-evidence/r9-h2-host-imports/`. The integration owner owns aggregate closure; current combined qualification output remains untouched.

The existing drivers retain their private temporary exports/binaries. After preserving evidence, remove only the owned temporary roots recorded in `integrated/provenance.json` (`source` parent), `targets/source.json` (`export` parent), and `tinygo/provenance.json` (`source` itself). Verify prefixes `wago-h2-integrated-`, `wago-platform-`, and `wago-h2-tinygo-` respectively before cleanup. Use an owned temporary GOCACHE/GOTMPDIR for new runs and clean them after completion; never purge shared caches, another worker's export or the pinned toolchains. These are correctness commands; run no benchmarks or timing workloads.

## Support branch validation

On this documentation branch, the existing `^TestR9H2(StoreHooks|Receipt)` scope passed with `-count=1 -timeout=3m` in ordinary, wagodebug and wago_regalloccheck using Go 1.27.3. A private temporary build cache and work directory were removed after all three commands. This is focused baseline validation only; new host-container/import tests are still proposed, and no new race, QEMU, TinyGo or full-suite result is claimed. Document checks confirmed balanced fenced blocks, existing production paths, empty intersection of the 15-path A and 24-path B allowlists, clean whitespace, and exactly the three requested documents changed from the base.
