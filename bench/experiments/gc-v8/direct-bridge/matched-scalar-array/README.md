# Matched checked scalar-array experiment

Build `checked_refstore.test` with `-tags wago_gc_direct_refstore` and `scalar_arrays.test` with `-tags wago_gc_direct_scalar_arrays` from this same source, using `go test -p 1 -c`.

Run `python3 run.py --binary-dir <directory> --output-dir <directory> --audit`, then omit `--audit` for timing. Both variants use explicit 512 KiB Eden and 8 MiB per old semispace. Three fresh-process rounds each run three warmup and five checked timed invocations, rotating workload and variant order.

This isolates checked scalar-array native helper removal without trusted type-check reduction. It does not include the subsequent final-array-child reference-store extension. Struct and tree remain controls; report every workload and all raw samples. Exact artifacts, checksums, allocation counts, collection and process memory are verified/retained. This short comparison is not V8 parity or a default heap-policy decision.
