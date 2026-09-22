# Corpus Provenance

The exact source revision, toolchain, artifact digest, inputs, and oracle for
semantic workloads are recorded directly in `catalog.json`. The remaining local
workloads are rebuilt from `sources/` with the scripts in `build/`.

| class | retained workloads | source or revision |
| --- | --- | --- |
| synthetic WAT | tiny, recursion, memory, indirect dispatch, function scale | reviewed files in `sources/wat` |
| Rust compute | linked list, nbody, fannkuch, matmul, SHA-256, ray tracing | reviewed files in `sources/rust` |
| AssemblyScript | json-as, blake-as, utf-as; scalar and SIMD | local adapters plus the corresponding upstream package checkout |
| semantic | CoreMark, BLAKE3, QOI, LZ4, zlib, zstd | revisions and WASI SDK versions pinned per catalog check |
| parsers/text | yyjson, cJSON, TinyXML-2, utf8proc, PCRE2, fast_float | revisions pinned in `catalog.json`, WASI SDK 34 |
| numeric/crypto | xxHash, LibTomMath, KissFFT, Monocypher | revisions pinned in `catalog.json`, WASI SDK 34 |
| compression/media | miniz, LodePNG, dr_wav | deterministic generated inputs, revisions pinned in `catalog.json`, WASI SDK 34 |
| graphics | NanoSVG parse plus shape/path traversal | `239e102ec2c691f2902e20ace2ed36ee4a35cfe6`, WASI SDK 34 |
| interpreters | Lua 5.4.8 and Wren running embedded deterministic programs | revisions pinned in `catalog.json`, WASI SDK 34 |
| PolyBench/C | all 30 kernels, small dataset | `5474c59fe88f4e36ba968e8f8c4ac913ee83f0d0`, WASI SDK 34 |
| Embench | crc32, huffbench, matmult-int, nettle-aes, nettle-sha256, qrduino | `09c2ed8c3b7008c95d08b038de4a3f6dc103ed70`, WASI SDK 34 |
| Sightglass | shootout base64, libsodium hash | `9ce88522d75b2d155e358f576e7d88ed26d14de8`, Binaryen 130 timing-hook removal |
| TACLeBench | self-checking bubble sort | `c6a0d73e47bbd2bc86e34637156fb26dd4d5cf08`, WASI SDK 34 |
| esbuild CLI | pinned Go/WASI minifier reading JavaScript on stdin | `f6058f8364fe7ab91ca57a83e02577ed74c9cae4`, Go 1.26.5; exact output independently captured with Wasmtime |
| QuickJS CLI | pinned WASI SDK 34 build of the standalone `qjs` interpreter; exact JavaScript output captured with Wasmtime | `saghul/wasi-lab` `05d2c175afeed626187f792c9dd1a8142e11f95a`; deterministic stripped rebuild |
| Duktape CLI | pinned WASI SDK 34 build of the standalone `duk` interpreter; exact JavaScript output captured with Wasmtime | same `saghul/wasi-lab` revision; embedded Duktape source reports `44ca54f726bfa651a7ab59286dd5c371dba2ddfc` |
| swift-format CLI | pinned upstream WASI release formatting a real Swift source file; exact output captured with Wasmtime | `kkebo/swift-format` `92097d54ac3be47738fe77e38c918e9aabce0302`, release `603.0.0-wasm32-wasi` |
| SQLite CLI | upstream public-domain 3.53.4 amalgamation, built with WASI SDK 34 and an unsupported-subprocess stub; exact SQL result captured with Wasmtime | `sqlite-amalgamation-3530400.zip`, archive SHA-256 `1e71ddf93849c6a6ecf58b827c0692073d2dd7ee40196158068f7b29f422e87d` |
| xzdec CLI | upstream XZ Utils 5.8.4 decoder built with WASI SDK 34; exact decompressed bytes captured with Wasmtime | `d3e650e63c110e830fd5391e7f8b45df0b91d3da`, release archive SHA-256 `4ce24038fd4221e0d13bc1a2de7a4db56e90b92b3bf75321f6c14be73f65de4b` |
| lzmadec CLI | same upstream XZ Utils 5.8.4 source, configured for the legacy LZMA decoder; exact decompressed bytes captured with Wasmtime | `d3e650e63c110e830fd5391e7f8b45df0b91d3da`, WASI SDK 34 |
| lzmainfo CLI | same upstream XZ Utils 5.8.4 source, configured for legacy LZMA header inspection; exact output captured with Wasmtime | `d3e650e63c110e830fd5391e7f8b45df0b91d3da`, WASI SDK 34 |
| age and age-keygen | Go/WASI commands decrypting a fixed test ciphertext and deriving its public recipient; exact outputs captured with Wasmtime | `b74dce4cdbe35b5e5f66c06d9612b72f89028758`, Go 1.27.0, `-trimpath`; the identity is deliberately public test data |
| jq CLI | standalone JSON processor built with bundled Oniguruma and WASI SDK 34; exact output captured with Wasmtime | `34f7186b86743a083a589741b6cea95293524108`, source archive SHA-256 `71b8d6e8f5fe81f6c6d0d110e3892251f6ce76ed095abd315e26e6e1193af3af` |
| Brotli CLI | upstream 1.2.0 command built with WASI SDK 34; fixed stream compression and decompression with exact outputs captured with Wasmtime | `028fb5a23661f123017c060daa546b55cf4bde29`, rebuild instructions and WASI compatibility header in `workloads/applications/brotli/` |
| tree CLI | upstream 2.2.1 command built with WASI SDK 34; exact recursive listing captured with Wasmtime | `d501b58ff9cbfd64272c8cbcad0bda36a3fada06`, numeric UID/GID fallback headers and rebuild instructions in `workloads/applications/tree/` |
| a-Shell tree CLI | prebuilt tree 1.8.0 from a-Shell release 0.1; four-file listing checked in Wago and wazero with exact output | release asset `20591324`, updated 2020-05-10; exact artifact SHA-256 and host contract in `catalog.json` and `workloads/applications/tree/README.md` |

The PolyBench adapter includes each upstream kernel unchanged, replaces its
dump stream with a deterministic checksum at the suite's two-decimal output
precision, and exports `polybench_run`. That keeps the complete live-out scan
and dead-code-elimination barrier without putting text formatting or I/O in the
timed workload. The checked result was independently captured with Wasmtime.

The suite intentionally excludes generated ISA sweeps, opaque Wasm-R3 replays,
platform-gated WABench ports, redundant microbenchmarks, and third-party
programs that only proved they compiled or did not trap. Those artifacts add
maintenance and CI cost without providing a stable correctness or performance
signal.

The yyjson, utf8proc, xxHash, LibTomMath, NanoSVG, KissFFT, TinyXML-2, Lua,
cJSON, miniz, Monocypher, dr_wav, LodePNG, fast_float, PCRE2, and Wren expected
return values were captured independently with Wasmtime 46.0.1. Node 26/V8
was also used to inspect every module's import surface. TinyXML-2, Lua, cJSON,
LodePNG, and Wren retain WASI libc imports and therefore run through the
command harness; the other eleven are import-free core modules. Wren's unused
clock primitive is bound to a deterministic guest stub. The Lua adapter
replaces error recovery with a fail-fast trap because its embedded valid
program does not test error recovery; any unexpected interpreter error
therefore fails the corpus rather than being swallowed.

Rebuild scripts never redefine admission. Review rebuilt bytes, update the
catalog digest and provenance deliberately, and rerun the individual
correctness and benchmark-wiring targets before committing.

## Reproducible builds and excluded-path recheck

For the sixteen added workloads, use the WASI SDK 34.0 **x86_64-linux** archive:
`wasi-sdk-34.0-x86_64-linux.tar.gz`, SHA-256
`b761e3a0721dbae9c09a0059e5fdb2bf917d1b4a8a7b430fb3b5aafb0984b2c4`.
Clang identifies LLVM revision `895aa2c896ada719451be2e3673c83da8ddf1141`.
All build scripts retain their pinned source revisions and optimization flags;
they now pass `--strip-debug` to exclude SDK library debug paths. The previous
artifacts contained macOS SDK build paths. A Linux SDK rebuild did not reproduce
nine of those artifacts: seven differed only in debug data, while NanoSVG and
TinyXML-2 also differed in executable sections. Do not assume that different SDK
host distributions produce identical bytes. The catalog now pins reviewed Linux
SDK output. A second build reproduces all sixteen artifacts byte for byte.
No upstream revision or license changed. Each original expected result was
rechecked with Wasmtime 48.0.2; none was changed to match Wago.

Wren retains its classes/closures/collections workload and adds `wren-modulo`.
The recovered pre-`c7eaea13c` prime sieve passes on the combined code, returning
210661955 in both engines (historical artifact SHA-256
`4c5349da94ef84075e644a5b753716b3db28b8eb381a1d071ed8118b3e775bc3`).
The permanent adapter also computes `5.5 % 2` and returns the raw f64 bits with
`memcpy`, without an integer conversion of the floating result. Its result is
210661956.5, bits `0x41a91ce489000000` (4731344651406016512), independently checked
with Wasmtime 48.0.2. The shared Wren artifact is
`2b1647ba27936995e85892492c7389d114811496e5c276a2c4b2d44e8a455b41`.
This establishes coverage of those operations with #666 present; it does not
establish that #666 caused the earlier Wren discrepancy.

NanoSVG rasterization still fails. The original and reduced adapters, artifacts,
build commands, and independent expected results are retained in the
[NanoSVG handoff](repro/nanosvg/README.md). The passing catalog continues to check
its parsing and shape/path traversal only.
