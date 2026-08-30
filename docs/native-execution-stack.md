# Native execution stack

Wago runs generated code on a bounded off-heap native stack. The default capacity
is 4 MiB. A fixed 256 KiB fence remains above the low mapping boundary so a
function prologue traps before a native access can leave the mapping.

Applications with validated modules that need deeper recursion or large live
frames can select a larger capacity on `RuntimeConfig`:

```go
cfg := wago.NewRuntimeConfig().WithNativeStackBytes(8 << 20)
rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
```

The accepted range is 512 KiB through 1 GiB, inclusive. The byte count must be a
multiple of 16. Invalid values fail configuration validation or instantiation
before guest code executes.

The CLI accepts raw bytes and binary suffixes:

```sh
wago run --native-stack 8MiB module.wasm
```

`KiB`, `MiB`, and `GiB` suffixes are supported. The option changes runtime
execution only. It does not change compiled artifact identity.

Each instance engine and each synchronous host re-entry engine receives the
selected capacity. The bounded one-slot engine cache reuses a mapping only when
its capacity exactly matches the next request. A later default-size request does
not retain a mismatched large mapping.
