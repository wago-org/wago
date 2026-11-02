# Independent recheck: ARM64 two-result entry repair

Verdict: no correctness finding in test-only repair `21d7be58952b64435110246f65d44e11046c5083`. The repair is qualified by source review, immutable parent evidence verification, bounded established-route QEMU execution in all three required modes, and Darwin ARM64 cross-builds. Overall native closure remains open because the repaired test has not yet been rerun on native Darwin and the exact cause of the historical native fault was not captured.

## Exact pins

- reviewed commit: `21d7be58952b64435110246f65d44e11046c5083`
- parent: `fba4a1d6b8550f7bfa9355fd4e1a0e7dd6b9c7cf`
- reviewed tree: `e18d5f9fbc17d974571c72bdef24c978482ae905`
- `callexec_arm64_test.go`: `7778a4f176dd648627f2c2f4f59a2db5432f8c813056f080f2d5124d71543381`
- `arm64spike/spike.go`: `c75023f6a91c7044bd8951bf58d022f03d298449334dca3e94f2dd97502681d2`
- parent repair evidence `SHA256SUMS`: `e971e2fab7c0b0aa0675e6966f217475c96de9fcfe3d8d8c1101cfdf24632672`
- retained native diagnostic: `2dc2bc0598536545b7ee320b5acabc0ba7442ce455f85dad7a67ac63d4924cef`
- Go 1.27.1 binary: `30969f97169d7f43fe6a085873d75613adc21e30818a8c61d95bd27275df4624`
- QEMU AArch64 binary: `73cc2584f119f9e74c85c0a8549dbb6fd104ae3897a06a7500797f9e5b4d0943`

`sha256sum -c docs/rfwgc-evidence/darwin-two-result-entry-repair/SHA256SUMS` passed for all 29 listed files. The worktree was at the reviewed commit before and after execution. Unrelated dirty files, index state, gitlinks, and the historical `darwin-two-result-bridge` artifacts were not edited or staged.

## Review

The original test still disables inlining, compiles the same two-function module, and requires exactly one internal `regabi` call. The callee still produces two i32 results and the caller still sums them, so the internal X0/X1 result-register behavior remains covered. The original inputs `0, 1, 7, 100` and expected values `1, 3, 15, 201` remain exact.

The changed entry is appropriate for the generated code. The original direct `arm64spike.Call2` entry supplied no X26 module context, while the generated caller reads its stack fence through basedata. The repaired test enters the same compiled module through its emitted wrapper using `runtime.Engine`, `JobMemory`, `MapCode`, and the real nonzero `Engine.StackLimit`. No compiler option, generated check, or assertion is disabled. The `Call2` comment accurately narrows its existing contract; production executable code is unchanged.

The raised-fence control sets the basedata fence to `^uintptr(0)` and requires `TrapStackFenceBreached`, proving the same generated caller consumes the supplied fence and reaches the checked runtime trap path. Restoring `Engine.StackLimit` and requiring result `201` proves successful reuse after that trap. `Engine.Call` clears/rebinds the trap cell per entry. Engine, JobMemory, Arena allocations, and executable mapping remain owned for every call; deferred cleanup is dependency-safe LIFO: code unmaps before arena, JobMemory, and Engine close.

The historical native record is retained as a `runtime: split stack overflow` while executing `arm64spike.enterNativeSpike`. It establishes the failure but does not include the original native fault address/register state needed to prove X26 was the exact trigger. Accordingly this review accepts the entry-contract repair without promoting the causal hypothesis to established fact.

## Bounded verification

Environment for builds: `GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1`, `GOTOOLCHAIN=local`, `WAGO_SHARED_SCALAR=0`, `CGO_ENABLED=0`, `GOARCH=arm64`. Each mode used `go test -c -p=2 -mod=readonly`, package `./src/core/compiler/backend/railshot/arm64`, and `wago_codegenstats`; `wagodebug` or `wago_regalloccheck` was added for its named mode. Each Linux binary ran under the pinned QEMU with `-cpu max -t '32 36 28,64 64 1' -test.v -test.count=3 -test.timeout=1m -test.run='^TestTwoIntegerResultRegisterCall$'`. Each corresponding Darwin binary was cross-built with `GOOS=darwin`. No broad or duplicate suite ran.

| Mode | QEMU target passes | Linux binary SHA-256 | Darwin binary SHA-256 | QEMU log SHA-256 |
| --- | ---: | --- | --- | --- |
| ordinary | 3 | `ea128e0a463ba863701e2bf3027f6ddf7481772b2251063772d0d49a8821e25d` | `f0c58f5d5a7b1b5b3a8a4cfc3c4a066cc6a8f42e4fe06e308001894a11a08fbe` | `c311201211e9c778cefc544b018f2d38b6fd5e4671d4c43307fff672b0d683b5` |
| wagodebug | 3 | `abc36e11816f7f84227c7f39e4b702d1b68b97099d5efe71dab828c73a412d91` | `caa904947bd5a995b8937747d2556b76e6e1426c0feb7dfc455496fc3402e4ed` | `a460441920635f2349fce0592b8d7fb04a8be3e297ea9049cce060a2f4a8e6e1` |
| wago_regalloccheck | 3 | `6e46f9424e4425217018c11206e21fc6ae15104546200fb08329355f541e59f3` | `7b4fbfa214a423428b04d87e95c5391e52344bd0cf901375f659b5396fec55f3` | `a460441920635f2349fce0592b8d7fb04a8be3e297ea9049cce060a2f4a8e6e1` |

## Readiness and objections

Native-rerun readiness: **READY**. The source is pinned, all three Darwin binaries cross-build, the exact isolated test and required tags are known, the test now uses the production entry contract, and its trap/reuse controls pass on the established QEMU route.

Overall qualification: **OPEN pending native Darwin rerun**. This review does not claim that the repaired entry contract explains the exact historical native failure. Native Darwin should rerun the isolated test in ordinary, wagodebug, and wago_regalloccheck modes, retaining complete diagnostics on any failure. Native race and broader backend replay remain separate parent-scope gaps and were not duplicated here.

No benchmark, timing, measurement lease, push, PR, dispatch, subagent, source edit, or native execution was used.
