# MicroPython WASI command

`micropython.wasm` is the command-style artifact from the
`micropython-wasm==0.1a2` PyPI wheel, SHA-256
`a57989e2b56e9603438b98c55bb7edc6f275366a37a43dcd25952d465e01dc19`.
The module SHA-256 is
`1c054a4d21d4a6589bc568821ebf562889988ddaaf8a17a2e2985a56cf228051`.
The wrapper's Apache-2.0 and MicroPython's MIT license texts are retained.

The workload evaluates a generator expression for 1,000 squared integers and
checks the exact output `332833500` plus newline. This was independently
captured using the wheel's Wasmtime Python host. Its two custom imports are
provided by the Wago corpus harness: `host_result_cap` returns 1024 and
`host_call` denies arbitrary calls. The workload does not invoke `host_call`.

Wazero currently rejects this artifact's exception-handling section at
compile time, so only the Wago run is performed in the local test gate. The
captured Wasmtime output supplies the independent oracle.
