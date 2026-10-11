# Rejected destructive branch-index home reuse

Baseline: 37174169, Apple M4 Max arm64. Experimental default off. Both frozen paired binaries were built without wago_guardpage: these results are explicit-bounds mode only. No signal-mode qualification or retention claim.

A bounded structured CFG proves a borrowed index source dead on every dispatch successor, preserving earlier operand-stack snapshots before a destructive write to its permanent home. Only register VM changed in the eight-case screen, removing one MOV (1576 to1572 module bytes); all seven controls were byte-identical. The original contracts and focused structured liveness/stack-alias fixtures passed.

| Variant | Workload | Exec delta | Compile delta |
|---|---|---:|---:|
| dead-index | audio-adpcm | +0.21% | +0.80% |
| dead-index | compiler-register-allocation | -0.55% | -0.09% |
| dead-index | vision-dilation | +2.22% | +0.18% |
| dead-index | vision-components | +0.95% | -0.13% |
| dead-index | files-glob-match | +0.01% | +0.18% |
| dead-index | serialization-protobuf | -0.23% | -0.44% |
| dead-index | language-register-vm | +2.72% | +19.67% |
| dead-index | ml-knn | -0.07% | +1.48% |
| dead-index-compact | files-glob-match | -0.81% | -0.27% |
| dead-index-compact | language-register-vm | +2.26% | +8.75% |

The meaningful mitigation halved graph nodes to16 bytes and omitted arithmetic/constants/memory effects/ordinary calls from the graph while retaining their decoding. VM compiler overhead fell from19.67% to8.75%, but execution still regressed2.26%. Reject the emission: one eliminated copy does not repay analysis or improve execution. Retain the independent compact liveness helper only for a separate entry-zero experiment, whose broader static payoff must be measured.

Frozen binaries: /tmp/paired-dead-index-home and /tmp/paired-dead-index-home-compact. Source archive is the compact helper and fixtures; initial graph had32-byte nodes and included ordinary instructions.
