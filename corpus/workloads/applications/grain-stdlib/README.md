# Grain standard-library assertion subset

Two unchanged upstream tests, `array.test.gr` and `string.test.gr`, are compiled
separately with Grain 0.7.2's release profile and its documented
`--no-wasm-tail-call` compatibility option. The source revision is
[`49829d7966b38b177291f7e91f5eb81c65ec07aa`](https://github.com/grain-lang/grain/tree/49829d7966b38b177291f7e91f5eb81c65ec07aa).
This is a small standard-library subset, not the complete Grain test suite.

The two artifacts total 498,829 bytes. `source.tar.gz` (164,203 bytes) contains
the two tests, all 43 Grain runtime/stdlib source dependencies, and the upstream
root and stdlib license files. Every source is unchanged from the pinned
revision. `SOURCE_MANIFEST.json` records its SHA-256 and Git blob identity.
The dependency inventory was checked against a fresh compiler build and a
separate source-import audit. No `.gro` cache, compiler executable, filesystem
fixture, or unrelated test suite is shipped.

## Runtime and oracle

- WASI Preview 1 command, invoked once through `_start` on a fresh instance
- Exactly one import: `wasi_snapshot_preview1.fd_write`, `(i32,i32,i32,i32)->i32`
- One wasm32 linear memory, initially 64 pages (4 MiB); no imported memory
- Core 2 compatibility: bulk memory is retained; no Wasm tail calls, GC, EH,
  threads, or SIMD
- No preopened directories, network, environment values, stdin data, or random
  and clock inputs are required
- Success requires a normal/zero exit and SHA-256 of the empty byte sequence for
  both stdout and stderr; an assertion writes a diagnostic and traps

The upstream runner uses this same empty-output/exit-zero oracle. Release mode
retains assertions: a separately built `module FailingAssertControl` with `assert false` trapped with an `AssertionError` diagnostic in Wago, Wasmtime, and wazero.

`REFERENCE.json` records independent Wasmtime 49.0.2 and wazero 1.9.0 execution
of these exact artifacts. A Core 2 compile check guards the compatibility flag:
Windows uses that default profile for pipeline checks, even when a command's
execution allowlist excludes Windows. Linux and Darwin amd64/arm64 use the
existing WASI command host; Windows remains outside that host's allowlist.
The initial default-tail-call pilot is not the admitted artifact, and these
fixtures do not claim Wasm tail-call coverage.

## Source and notices

The test files reside under `compiler/test`, outside the separate stdlib MIT
license. They are conservatively retained under the root LGPLv3 terms in
`COPYING.LESSER`, with the incorporated GPLv3 text in `COPYING`.
`LICENSE.stdlib` and `THIRD_PARTY_NOTICES.md` preserve the linked runtime/stdlib's
MIT, Elm BSD-3-Clause, Sun permission, Rust MIT, Nim bigint, Metallic MIT, musl
MIT, and Jacob F. W. BSD-2-Clause notices.
The archive retains the original source headers as well. These fixture licenses
do not change Wago's license.

To inspect or modify the corresponding guest sources, extract `source.tar.gz`.
The archive's `grain/` directory is a complete build input for these guests;
use the compiler invocation in `build.sh`. The script verifies unmodified
rebuilds against the committed artifacts and never replaces them. For modified
sources, run that invocation with your own output path and review the result
before updating any digest.

## Rebuild

Use the official [Grain 0.7.2 Linux x64 compiler](https://github.com/grain-lang/grain/releases/tag/grain-v0.7.2),
release asset `352713721`, SHA-256
`82658891d33f5431e7bd260f0c00b8e86c43eb9182c5327f41db25d60b54dadd`.
It contains the packaged compiler, including Binaryen 124. The separately
obtained compiler is a build tool and is not distributed in this directory.
The build explicitly uses the bundled, matching stdlib sources.

Run the following from this fixture directory on Linux x86_64:

```sh
curl -fL -o grain-linux-x64 \
  https://github.com/grain-lang/grain/releases/download/grain-v0.7.2/grain-linux-x64
printf '%s  %s\n' \
  82658891d33f5431e7bd260f0c00b8e86c43eb9182c5327f41db25d60b54dadd \
  grain-linux-x64 | sha256sum -c -
chmod +x grain-linux-x64
GRAIN_COMPILER="$PWD/grain-linux-x64" bash build.sh
```

The build is opt-in; ordinary tests and CI only read the committed Wasm.
Two clean builds in different directories produced identical artifact hashes.
On the development host, a cold Array build took about 24 seconds and String
about 13 seconds using the already compiled stdlib cache. These are acquisition
costs, not guest execution timings.

## Focused checks

From the repository root:

```sh
just test corpus tag:grain-stdlib
(cd bench && go test -tags=wago_regalloccheck -count=1 -timeout 60s \
  -run '^(TestApplicationCorpusRuns|TestCatalogGrainStdlib.*)$' ./suite \
  -args -wago.corpus=tag:grain-stdlib)
just bench check tag:grain-stdlib exec
```

For an independent reference check, run each artifact with a pinned Wasmtime:

```sh
wasmtime run -C cache=n,parallel-compilation=n \
  -W timeout=10s,max-memory-size=268435456 \
  -S inherit-env=n,inherit-network=n,tcp=n,udp=n array.wasm
```

Run String identically. Both processes must exit zero with empty output streams.
Use a process deadline as well when automating initial admission.

The quick and website profiles are unchanged. Existing all-corpus correctness
and application shards include both fixtures; no extra CI job or compiler
installation is added. The two entries belong to one `Grain stdlib` suite.

## Deliberate exclusions

The packaged compiler hit its JavaScript stack limit while compiling Number,
Regex, and BigInt in the bounded pilot. JSON executed successfully, but its
source includes JSON_checker test data with unresolved licensing attribution;
it is not included in the source archive or the executable inventory.
The WASI clock/random/filesystem/process tests and upstream-disabled `fs.test`
are also outside this initial subset. No host adapter, clock policy, expected
exit-code handling, engine implementation, or comparison dependency is changed.
The `application` and `grain-stdlib` tags select both fixtures.
