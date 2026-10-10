# Retained compiler profile follow-up

Two 3-second prepared signal-bounds Samply captures use the current default-on common-exit compare and dominated-indexed-base compiler. Contracts are taken from the current application manifest: register allocation argument2048, exact1790173339; dilation argument128, exact633963730. Both captures completed successfully through the shared device reservation.

Saved images/source maps and leaf observations are in `profile-common-exit-retained-compiler-register-allocation` and `profile-common-exit-retained-vision-dilation`; sibling `-assembly.txt` files decode the exact sampled image. These counts are observations, not CPU-time percentages.

Dilation has visible CCMP instructions at native offsets0x230,0x308,0x328 and0x2e8, confirming the retained fold is active. Guarded loads and coordinate comparisons remain frequent sampled sites. Its order-dependent XOR/multiply recurrence still requires lane-order preservation for any future independent-term batching.

Register allocation remains dominated by loads at0x110,0x140,0x190,0x1dc,0x228,0x274,0x2c0,0x30c. Repeated multiplier materializations at0x694,0x6a8 and0x6bc remain visible. The MOV+LSL pattern previously investigated is still present, but the direct borrowed shift optimization was already rejected after mitigation; this capture alone supplies no reason to retry it. Before changing constant caching, inspect whether the existing scoped-loop leases are unavailable due to register pressure or admission. Any change must use general cost/effect rules and be measured on affected workloads beyond this case.

These captures identify remaining work; they establish neither a new optimization gain nor completion of the full parity goal.

## Constant admission diagnosis

`register-allocation-retained-constant-admission.txt` confirms15 pinned locals, one page lease, zero scalar leases, one spill and no reloads. Selected hints already include0x1000193, so candidate selection is not the missing mechanism. All three loops are proved call-free and the two later loops use that literal. The scalar temporary floor blocks it at function entry. Global floor5/6 variants were previously rejected after affected-case regressions.

A distinct mitigation to investigate is low-pressure, loop-scoped literal leasing for call-free functions too. Existing scoped setup is currently limited to functions with calls elsewhere. Keep the global floor unchanged; release any extra lease at loop exit and restore the regional limit. Admission must respect loop effects, nested scopes, physical live values and register pressure. Measure effects beyond the motivating case before retaining; do not claim that it is implemented or proved here.
