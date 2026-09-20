# Correctness follow-up: bounds, dispatch, runtime, and CLI

The work continues `fix/correctness-review` from `4a6939ff8`. Its files matched
`main` at `d6951bec8`, which contains the earlier correctness work from PR #670.
All eleven reported issues were reproduced and fixed.

## Fixes and exact regression tests

| Issue | Fix | Regression tests |
| --- | --- | --- |
| 1, 2: Bounds certificates | Large-offset adjustment clears the certificate source and range extension for that access. AMD64 also clears loop-proof reuse. Existing 64-bit extent checks retain the small-offset optimized path. | `amd64.TestLargeOffsetBoundsCertificate`, `arm64.TestLargeOffsetBoundsCertificate`, `wago.TestLargeOffsetBoundsTrap` |
| 3: Recursive watchers | Remove watch options throughout command arguments, retain other flag values, and stop at the explicit `--` separator. | Expanded `run.TestWithoutWatchFlagsPreservesGuestArguments` |
| 4: Reference nullability | Inspect the popped type, preserve nullability, and retain the existing subtype check. Unknown unreachable operands admit the non-null input type. | `wasm.TestReferenceConversionNullability`, with AST and byte-backed validation |
| 5: GC dispatch overlap | Derive the safepoint mask from the first dispatch tag. Maximum ID: 2,097,151. Reject ID 2,097,152. | `shared.TestGCDispatchReservedBits` |
| 6: Code buffer self-append | Preserve and rebase aliased byte ranges across growth, including subslices and mapping bytes beyond the logical image. | `runtime.TestCodeBufferSelfAppend`, `runtime.TestCodeBufferAppendNoGrowthAllocations` |
| 7: Explicit Core profile | An explicit Core selection replaces stored feature defaults, including stored disables. Without an explicit choice, retain stored behavior. | `settings.TestExplicitCoreOverridesStoredFeatures` |
| 8: `_start` results | Resolve every signature and require `_start : () -> ()`. Report mismatched formatter value/type counts without a panic. | `run.TestRunStartSignature`, `wasmcall.TestFormatResultsMismatchedLengths` |
| 9: Full Linux uninstall | Include active data, config, and cache targets along with the selected legacy root. Retain path checks and containment removal. | `self.TestFullUninstallIncludesActiveXDGPaths` |
| 10: Passive element multiplication | Check the descriptor count by division before multiplication. | `runtime.TestInstantiateArenaNeedPassiveElementOverflow` |
| 11: Fish XDG config | Share the Fish path helper between installation and cleanup, using the existing XDG resolver. Retain explicit install-path precedence. | `config.TestFishCompletionXDGConfigHome`, `self.TestFishCompletionInstallCleanupPath` |

The bounds tests retain both load results and cover offsets 2,147,483,647,
2,147,483,648, and 4,294,967,295. Execution uses explicit bounds checks with
bounds facts enabled. Both original backends emitted one check and elided the
second; both fixed backends emit two. The ARM64 failure was also confirmed in
an isolated checkout of `4a6939ff8`. Native self-append caused a SIGSEGV before
the fix. The other new regressions also failed on their original code.

## Related code checks

- Indexed-memory paths emit independent checks; memory64 paths retain carry
  checks. Small-offset proof calculations check the extent in 64-bit arithmetic
  before narrowing it.
- Backend emission, module planning, runtime root validation, and serialized
  safepoint counts already use `GCSafepointIDMax`. No metadata format change is
  needed.
- Other CodeBuffer mutators do not retain an append source across growth.
  AppendTail already specifies that another mutation invalidates its slice.
- Other multiplications in InstantiateArenaNeed are checked before they occur,
  either locally or in the initial footprint checks.
- Related installer config-file discovery already honors XDG_CONFIG_HOME.
- CompilationRequest has no explicit per-command feature override field.

## Verification

Targeted regressions and their nearest package suites ran after each fix.
This final combined package check passed:

```sh
PATH="$PWD/.tools/wabt-1.0.41-linux-x64/bin:$PATH" go test \
  ./src/core/compiler/backend/railshot/amd64 \
  ./src/core/compiler/backend/railshot/shared \
  ./src/core/compiler/wasm ./src/core/runtime ./src/wago \
  ./cli/runtime/commands/run ./cli/internal/settings \
  ./cli/manager/internal/self ./cli/manager/internal/config \
  ./cli/internal/wasmcall ./internal/wagopaths
```

These ARM64 tests passed with the locally installed QEMU:

```sh
GOOS=linux GOARCH=arm64 go test -exec /tmp/wago-qemu/usr/bin/qemu-aarch64 \
  ./src/core/compiler/backend/railshot/arm64
GOOS=linux GOARCH=arm64 go test -exec /tmp/wago-qemu/usr/bin/qemu-aarch64 \
  ./src/wago -run 'TestLargeOffsetBoundsTrap|TestGCModuleFrameRootPlan' -count=1
GOOS=linux GOARCH=arm64 go test -exec /tmp/wago-qemu/usr/bin/qemu-aarch64 \
  ./src/core/runtime -run 'TestCodeBuffer|TestInstantiateArenaNeed' -count=1
```

The broader commands were also run:

```sh
PATH="$PWD/.tools/wabt-1.0.41-linux-x64/bin:$PATH" go test ./...
GOFLAGS=-buildvcs=false go test ./cli/manager/internal/standalone
PATH="$PWD/.tools/wabt-1.0.41-linux-x64/bin:$PATH" GOFLAGS=-buildvcs=false \
  go test -tags wago_runtime ./cli/...
```

Their only failing package was `cli/manager/internal/standalone`. Three TinyGo
tests initially failed while reading VCS status. With VCS stamping disabled,
two build tests failed at link time with duplicate symbol `tinygo_task_exit`.
`TestBuildTinyGoEmbedsArtifactWithoutCompiler` failed with the same duplicate
symbol on `4a6939ff8`. Tests and expectations were not weakened to bypass it.

`just lint` passed before every commit. Standard staticcheck emits the same
diagnostics as the original checkout; the repository gate already permits
those findings. Runtime-tagged staticcheck passes. There are no new findings.
Formatting, generation, vet, script tests, and `git diff --check` passed.

## Allocation and performance

No fields were added to per-function compiler state. The bounds fix changes
only the large-offset branch. Dispatch uses constants and scalar arithmetic.
Validation uses the existing value and reference representations.

CodeBuffer checks for aliases only when growth is required. It uses integer
offsets and the replacement buffer, with no temporary allocation. Ordinary and
aliased appends without growth both measured zero allocations for native and
heap backing.

These benchmarks ran on both `4a6939ff8` and the fixed code:

```sh
go test ./src/core/compiler/backend/railshot/amd64 -run '^$' \
  -bench 'BenchmarkRailshotCompile(SmallScalar|SIMDHeavy)$' \
  -benchmem -benchtime=200ms -count=3
go test ./src/core/compiler/wasm -run '^$' \
  -bench '^BenchmarkGCTypeValidate$' -benchmem -benchtime=200ms -count=3
```

Compile allocation counts remained 22/op for SmallScalar and 24/op for
SIMDHeavy. GC type validation retained the same allocated bytes and allocation
counts in every case. Timing samples overlapped other verification work, so
they do not support a precise throughput comparison. ARM64 execution was
verified with QEMU; native ARM64 performance was not measured.
