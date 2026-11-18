# Diagnostic optimizer-family ablation

Current main752e12f7; same Go1.27.1 binary for every arm. Original145 frozen artifacts and original oracle contracts. Three shuffled fresh-process trials; core2/GOMAXPROCS1; first public Compile after preparation; oracle outside timer.4350/4350 outputs verified. Not a runtime-speed or RSS qualification.

The diagnostic adapter uses existing public WithOptimizations before the timer. Both explicit controls apply all default values; each family sets its named options false in the same full map. Ordinary default arm measures this configuration-route overhead separately. Whole-loop reduction is a separate existing environment option. Experimental pure sink disabled. Actual memory bounds checking remains enabled.

Most disabled families compile slower: tree+1.35%,inline+1.02%,scalar-loop/if+7.21%,bounds+1.66%,control+1.31%,whole-loop reduction+1.46%, relative to explicit controls. Interval residency -1.15% is small and not a target-scale finding; execution cost unmeasured. Public explicit selection itself adds about6% versus ordinary defaults, so these are diagnostic comparisons, not published default-mode compiler gains.

Public options may preserve candidate analysis: assoc-tree=false avoids affine analysis but retains ordinary associative walks; tree-order=false retains candidate checks. Therefore no gain from disabling a knob does not prove its analysis is free. Conversely removing optimizations can increase emitted work and compilation latency.

No production removal or PR justified by this screen. Next use actual deferred-root topology and context frequency to select a broader complete-transition experiment, rather than extending a family with negligible admission.
