# Yosys optimization trap

The pinned YoWASP Yosys artifact in
`corpus/workloads/applications/yosys/yosys.wasm` successfully executes
`read_verilog /top.v; proc; stat` in Wago. Extending the same script to
`read_verilog /top.v; proc; opt; stat` traps in Wago at a linear-memory
out-of-bounds access in function 14723 after entering `opt`. Wasmtime and
wazero complete that script on the same input. This is a separate runtime gap,
not covered by the admitted `yosys-counter` workload.

The input is `corpus/workloads/applications/yosys/inputs/top.v`; mount its
directory at guest `/` and invoke the artifact with:

```sh
-Q -T -p 'read_verilog /top.v; proc; opt; stat'
```
