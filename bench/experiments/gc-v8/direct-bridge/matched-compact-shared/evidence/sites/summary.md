# Exact compact loop selection diagnostic

Compile-only, no guest execution or timing. Source21563b36 (runtime50c3065d), Go1.27.1, tags `wago_gc_compact_loop wago_gc_compact_refstruct`, Runtime.Compile with CoreFeaturesV3 and dead-gc-new=false. Uninstrumented and diagnostic-instrumented outputs have identical native bytes/SHA256 for both exact WASM artifacts (see baseline.txt/instrumented.txt).

Both modules select exactly three leases:
- Function0 checksum: loopPC3, firstaccessPC16, local0,11 availableGP.
- Function1 bulk kernel initialization: loopPC24, firstaccessPC38, local1,8 availableGP.
- Function2 scalar kernel initialization: loopPC24, firstaccessPC38, local1,8 availableGP.

Rejected: function1 outerloopPC63; function2 outerloopPC63; function2 actual scalar-copy innerloopPC87 (8 availableGP); function3 repeatloopPC3. The exact source and bounded scanner show innercopy is rejected because get(source local1) then set(target local2) introduces a second receiver. It is not rejected for register pressure. Outer loops contain calls/nested control/bulk operations and fall outside the scanner.

Native output7535 bytes for each; both hashes unchanged. Temporary compiler logging was restored after capture; diagnostic source is retained only in this evidence directory, not production.
