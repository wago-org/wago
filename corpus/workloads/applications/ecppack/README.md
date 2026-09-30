# ECP5 packer

`ecppack.wasm` is from the YoWASP ECP5 `0.5.0.0.post399` wheel; module SHA-256
`2a2ea3a2c706ccfe0ab26f28d5a4d9ae724fb80441ab3bf422ae5aa54d602709`.
The input is Project Trellis's empty LFE5U-25F configuration. It is packed
with the pinned database subset in `../ecp5db/`, mounted read-only at `/db`.
The generated bitstream is checked byte-for-byte by SHA-256 against Wasmtime.
The ISC license text is retained here.
