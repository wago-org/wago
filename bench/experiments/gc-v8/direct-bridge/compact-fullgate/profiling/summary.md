# Separate checked compact CPU attribution

Exact full-gate binary, measured source 32c0ae8f and runtime c3ede430, no rebuild. Each probe uses 2 MiB Eden, two 8 MiB old spaces, 128 warmups then 512 calls. Checksum/allocation/collection checks pass. Profiles include setup and warmup; elapsed samples are not benchmark results.

- Struct: 4.50 s sampled CPU, 74.44% runtime._ExternalCode; compact collection 22.00% cumulative (minor 21.33%, full 0.67%), layoutAt 3.11% cumulative, memmove 4.00% flat.
- Scalar-copy: 1.33 s sampled CPU, 98.50% external code; no meaningful sampled collector cost. This points toward generated native execution, not observed Go GC overhead.

Cumulative percentages overlap. Go profiling does not identify individual generated instructions or reliably unwind JIT frames. External samples cannot be assigned to specific guards/loads without further attribution. Zero collector samples do not prove exactly zero cost. No V8 profile or isolated collector-efficiency claim.

## Descriptive native code footprint

| Artifact | Prior direct best SSE2 bytes | Checked compact bytes |
|---|---:|---:|
| Struct |1775|3183|
| Array allocation |1967|3439|
| Fill |2543|3951|
| Bulk copy |4655|7343|
| Scalar-copy |4655|7343|
| Tree |1455|4015|

Prior values come from direct SSE2/ABI4 loop-copy evidence on identical artifacts; compact values come from this gate. Different source stages/checking contracts make this descriptive, not matched speed attribution. Code size alone does not prove the cause of runtime gaps.
