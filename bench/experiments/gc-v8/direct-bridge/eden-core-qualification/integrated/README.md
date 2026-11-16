# Integrated checkpoint

At source `44c314f1`, the compact core's production and test files match qualified
`b893312c`; only its status document differs. Fresh integrated default core,
Eden+age2 core, and default Wago/backend/wrapper/native GC regressions pass.
The harness package was compiled without running benchmarks in this correctness
check (`no tests to run` is not a new performance result). Full Wago ran in
56.865s; several unchanged regression results were cached. Earlier source-identical
race, TinyGo core and randomized-model results remain separately retained.

No policy was enabled by default. Native Eden integration and the V8 target
remain unfulfilled. The measured core experiment did not show a general win.
