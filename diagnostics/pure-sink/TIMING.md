# Reconstructed pure-sink timing

Production26013b05 vs pristine main752e12f7; same Go1.27.1, reconstructed wasm-bench55c0f7d, original145 frozen artifacts and archived full oracle contracts. New source/results, not pre-reset byte identity. Single core2/GOMAXPROCS1; shuffled fresh processes; first public Compile after preparation; oracle after timer; no memory sampling. All5800 records verify.

Arithmetic mean of per-workload medians (microseconds):

| Arm |3trials|5trials|
|---|---:|---:|
|mainA|803.871|821.475|
|mainB|811.038|826.428|
|candidateOFF|792.651|787.822|
|ON gate|785.016|795.865|
|ON ungated|785.697|800.391|

Activation relative to same-binary OFF changes from -0.96% to +1.02%. The gate itself changes -0.087%/-0.57% relative to ungated. A/A +0.89%/+0.60%. Main comparisons are superficially positive but OFF also improves, so this does not establish specialization benefit. No PR,40% claim,RSS or runtime claim.

Raw gzip SHA256:
-541438d1a30b27187f9005f53adbed3c6729c218f91c784ecfc0cc9f8eac1a83 (2175records)
-74baa5d0bfe8fb120651b13e82e79d0233dd90d8ac47e651a89bf342a467423e (3625records)

Next: instrumentation-only coverage census for full-width post-load arithmetic-to-local transitions, retaining original memory/bounds/trap lowering. No implementation claim.
