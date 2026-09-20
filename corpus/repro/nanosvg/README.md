# NanoSVG rasterization handoff

This case is excluded from the passing benchmark catalog. The parsing workload
remains admitted. Rasterization still fails on Linux/AMD64 with PR #666 included.
No backend fix is included here.

`raster.c` recovers the adapter before commit `74af00baa` in PR #659. The default
input is the original 96×64 SVG with two rectangles and a closed path. `REDUCED`
selects one red 8×8 rectangle. Both calls initialize NanoSVG with `nsvgParse`,
create the rasterizer with `nsvgCreateRasterizer`, allocate a zeroed RGBA buffer,
and call `nsvgRasterize` at scale 1 and translation (0, 0), with stride width×4.
The buffer size is width×height×4. All objects are released. Setup failures
return distinct sentinel values. Before rasterization, the reduced case checks
the dimensions, one visible opaque red shape, its 13 path points, and bounds
(0, 0, 8, 8). These checks pass on Wago before the buffer remains empty. The reduced input
uses a named color, so it does not depend on the adapter's `sscanf` replacement.
The generated modules have no imports.

The returned value is `(sum of all RGBA bytes << 16) XOR nonzero-alpha pixels`.
This is an exact diagnostic result, not a proposed framebuffer oracle for a
passing rendering workload. A future admitted renderer must compare the full
framebuffer or its independently established digest.

| artifact | Wasmtime 48.0.2 | Wago, Linux/AMD64 |
| --- | ---: | ---: |
| recovered historical module | 142588513616 | 0 |
| `raster.wasm` | 142588513616 | 0 |
| `reduced.wasm` | 2139095104 | 0 |

The reduced expected result represents 64 opaque red pixels: 32640 total channel
values and 64 covered pixels. Zero is the first confirmed output divergence;
it means no raster output after successful parsing and allocation. The separate exact structural parsing check passes.
The faulty instruction has not been isolated. Connection to the float-cache
bug was a hypothesis; #666 does not make this case pass. Driver initialization,
buffer bounds, named-color parsing, and import differences were checked. C
undefined behavior and compiler/runtime causes have not been exhaustively ruled
out. An issue search for NanoSVG/Wren/modulo found no matching open report;
this handoff preserves the case without adding a duplicate issue.

Artifact SHA-256 values:

```text
historical (74af00baa^): c95c2f06488f35d40c24db86370314fd718f2e6c67e832644f7fd5c473988c20
raster.wasm: 04dd15716a74b1e696f4ee34d9b95b43bb50a513ed46b1bc3f27bac387cec856
reduced.wasm: 68f4ce0198c613aa42d0a97d52eb639012e619bd9a32fe38368adeb698c0be7b
```

Use WASI SDK 34.0, **x86_64-linux distribution** (Clang 23.1.0,
LLVM revision `895aa2c896ada719451be2e3673c83da8ddf1141`). The scripts pin NanoSVG
revision `239e102ec2c691f2902e20ace2ed36ee4a35cfe6`; its copied license is in
[the admitted workload](../../workloads/semantic/nanosvg/LICENSE).
From the repository root:

```sh
WASI_SDK=/path/to/wasi-sdk-34.0-x86_64-linux sh corpus/repro/nanosvg/build.sh
WASI_SDK=/path/to/wasi-sdk-34.0-x86_64-linux REDUCED=1 OUTPUT=reduced.wasm sh corpus/repro/nanosvg/build.sh
wasmtime run --invoke nanosvg_run corpus/repro/nanosvg/raster.wasm
wasmtime run --invoke nanosvg_run corpus/repro/nanosvg/reduced.wasm
go run ./cli/wago run --invoke nanosvg_run corpus/repro/nanosvg/raster.wasm
go run ./cli/wago run --invoke nanosvg_run corpus/repro/nanosvg/reduced.wasm
```

Wago's tested base is `56407ed599b4ec86beb9e93187ffbfdaca2adbaf`, merged into
PR head `61147f7c1fc01b9fa279b2069ee268330cbc3208`. Wasmtime version:
`48.0.2 (e9f1ea232 2026-09-10)`. These failure results are native AMD64 execution;
no ARM64 rasterization result is claimed.
