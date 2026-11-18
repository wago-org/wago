# Optimization binding initialization

Experimental cleanup atop 2dd082d4. Internal bindings now borrow the package-private immutable catalog instead of calling public defensive-copy APIs twice. Public Lookup, ForArch and All continue returning independent architecture slices. The same catalog order, validation errors, flags and configuration semantics remain.

Focused optimization-package suite passes, including caller-mutation isolation and exact nil/duplicate/unknown/missing binding errors. A three-repeat,100-iteration construction benchmark used the same test source and Go1.27.1 for baseline and candidate, with baseline catalog.go supplied through a source overlay. Requested allocation bytes/count were stable:

|Target bindings|Baseline bytes/allocations|Candidate bytes/allocations|
|---|---:|---:|
|AMD64|28624 /181|10384 /4|
|ARM64|34352 /261|14832 /4|
|RISC-V64|14484 /13|532 /4|

This saves18,240 requested bytes in AMD64 binding initialization. It is not an absolute RSS measurement or a first-Compile speedup. Package initialization normally occurs before the public Compile timer. Nanosecond timings were noisy and are not used to claim an end-to-end benefit. No full CI, TinyGo build or full adapter memory run was performed for this cleanup; the larger compiler targets remain unmet.
