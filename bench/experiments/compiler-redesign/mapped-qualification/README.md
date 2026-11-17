# Mapped operand qualification

Production candidate `ba805158`; test portability fixes `7acc4d5f`, `2acfd3e9`.
Go 1.27.1 linux/amd64, GOMAXPROCS=2, `go test -p=1 -count=1`:

- Focused AMD64 `^TestMappedOperandArena`: default, explicit mapped tag, and
  mapped tag plus `wago_regalloccheck` passed.
- Full AMD64 and core/runtime packages: all three configurations passed.
- After test-only portability fixes, `TestFunctionResultTypesUseBoundedScratch`
  and `Test.*Worker` passed again under standard Go.

Actual TinyGo v0.42.0 toolchain, tasks/conservative, opt2/no-debug, stack256KB:

```
tinygo test -tags=wago_tinygo_mapped_operands -scheduler=tasks \
  -gc=conservative -no-debug -opt=2 -stack-size=256KB \
  -run '^TestMappedOperandArena' -v ./src/core/compiler/backend/railshot/amd64
```

Passed all named arena tests, including actual forced GC, mapped/heap overflow
links, cold managed roots, closed-owner fallback and normal/error/panicking
teardown. The whole AMD64 test package was compiled; only these focused tests
were executed. No critical arena/cleanup assertion was excluded. The full TinyGo
suite was NOT run. Existing core/runtime fault-injection tests passed under Go;
actual TinyGo unmap-failure injection is not claimed.

Two pre-existing TinyGo test-build blockers were corrected separately: direct
indexing of its intentionally zero-length result cache, and x/sys CPU assembly
in worker-reset tests. Standard-Go CPU gating remains unchanged; TinyGo uses a
conservative all-processor flags intersection. Neither fix changes production
code. Coordinator's separate provenance audit verified all19 collector/finalizer
source files against v0.42.0; the overall installed toolchain remains disclosed
as non-stock due to an existing exit-symbol patch.

No corpus, cold-compilation, RSS or performance result is claimed here.
