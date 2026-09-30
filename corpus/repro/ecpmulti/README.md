# ECP5 multiboot trap

`ecpmulti.wasm` comes from the YoWASP ECP5 `0.5.0.0.post399` wheel,
SHA-256 `6e830266f57425276048930c4db48d4deafadad5f0d2966b56476a9b04fe7340`.
The ISC license text is retained here.

Mount `corpus/workloads/applications/ecpunpack/inputs` at guest `/` and
`corpus/workloads/applications/ecp5db/db` read-only at guest `/db`. Then invoke:

```sh
--input /input.bit --input /input.bit --address 0x100000 --flashsize 16 --db /db /multi.bit
```

Wasmtime and wazero produce a multiboot image with SHA-256
`1243347df7c07cd662194e5f72e752a851db358303ac3474bcac0a40a5be1f2b`.
Wago instead traps at an `unreachable` in function 2252 after parsing the first
input bitstream, before emitting the image. It is deliberately not admitted to
the benchmark inventory.
