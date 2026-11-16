# Fresh reconstructed shadow diagnostic

Source8bbb5885 plus diagnostic accessor/probe only. New tag wago_gc_compact_shadow and separate ShadowSnapshot schema; this is NOT recovery of historical raw shadow observations. Re-run exact six artifacts,128 warmups+5 calls,2 MiB Eden/two8 MiB old spaces, range_loop+refstruct+cohort+shadow. No SSE/stride/shared/age2 runtime selection. No timing output or speed claims.

New machine Intel Xeon Platinum8573C, not historical AMD EPYC9V74. Node24.22.1/V813.6.233.17-node.58 and Go1.27.1 available. All9 manifest artifacts verified SHA-identical. Existing benchmark drivers support original100 warmups+9x32/3rounds; fresh comparisons must rebuild BOTH variants on this machine. Historical binaries/raw shadow logs are absent and cannot be claimed restored.

Require checksum/allocation/realGC checks and shadow/cohort/native successful-collection reconciliation. Report covered/invalid/uncovered cycles, promoted/reachable bytes and objects, scratchbytes. Interpret only covered valid consecutive-minor observations; full interruptions and no-followup cases remain unobserved. No build before explicit qualification release.
