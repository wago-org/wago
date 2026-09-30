# YoWASP FPGA command artifacts

These are command modules extracted from pinned YoWASP PyPI wheels, not Python
entry-point wrappers. The package versions, wheel SHA-256s, individual module
SHA-256s, runtime arguments, and exact output oracles are in
`corpus/PROVENANCE.md` and `corpus/catalog.json`. Each artifact directory includes
the YoWASP ISC license text. The packages do not publish an exact upstream
source commit for each embedded tool, so the wheel and module digests are the
artifact pins.

The iCE40 `icepack` input comes from Project IceStorm's `icecompr/example_1k.bin`
at commit `1fb7443c7c6870fadc275066c19af9c5add3482a`. Wasmtime running the
pinned `icepack.wasm -u` generated `example.asc`, and packing that ASCII file
reproduces the original binary byte-for-byte. Both directions run in Wago and
wazero with exact SHA-256 checks. The seeded BRAM and PLL workloads independently
match Wasmtime. For `ecpbram`, the harness hashes the generated file in a fresh
temporary preopen and leaves the committed fixture untouched.

The latest 0.11.1 YoWASP wheels currently exceed Wago's bounded exception
handling limit for most tools. The older 0.5.0 wheels are deliberately selected
for those commands; `icepll` uses the newer wheel because it compiles and runs.
