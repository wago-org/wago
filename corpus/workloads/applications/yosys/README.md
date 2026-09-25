# Yosys CLI

`yosys.wasm` is the CLI from the YoWASP `yowasp-yosys==0.32.0.0.post560`
PyPI wheel (wheel SHA-256
`ae2980dbef16117d3fa226c16a67ad81fab14f61b568607319e09dcb0921a355`).
The module SHA-256 is
`7668d2963f8cc276ccd32980126dfddac961db1cd002cb93073506a694059493`.
The upstream license is ISC; its text is retained in `LICENSE`.

The input is a sequential Verilog counter. The workload runs `read_verilog`,
`proc`, and `stat`; both Wago and Wasmtime produce the catalog's exact output.
The fuller `opt` pass currently traps in Wago; the separate reproducer records
that gap. This fixture does not claim complete synthesis support.
