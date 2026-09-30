# ECP5 unpacker

`ecpunpack.wasm` is from the YoWASP ECP5 `0.5.0.0.post399` wheel; module SHA-256
`d50205335eadfbe406599306251f3c9ea9016e1885dbc6578309b861410f83f8`.
The pinned input bitstream is the exact `ecppack` output from
`ecppack-bitstream`. The command decodes it using the read-only database
subset in `../ecp5db/`; the generated text configuration is checked against
an independent Wasmtime hash. The ISC license text is retained here.
