# Rejected: branchless dynamic signed remainder

Based on pushed checkpoint c74d21c1 in isolated worktree /tmp/wago-branchless-rem-20261010. Main compiler sources were never changed. The raw W/X SDIV+MSUB native proof passes, and both covers pass strict backend diagnostics plus all 148 exact contracts in both bounds modes. Exactly one corpus image changes: kissfft complex-roundtrip.

Initial eight-round 200ms same-thread result: exec +0.14% paired median; compile +0.32% with substantial variation. No measurable execution benefit. Before rejection, mitigated temporary-register pressure by using the already-distinct result register for the quotient, eliminating an allocation/release pair. That version again passes diagnostics and all 148 contracts in both bounds modes, changes only kissfft, and measures exec -0.23% (IQR -1.23% to +0.30%) and compile -2.45% (IQR -8.08% to +3.76%). These effects are small/noisy; there is no demonstrated corpus execution gain.

Rejected after the register-pressure mitigation. Do not infer full parity or a reliable compiler improvement from these runs. Native semantics and fixture remain useful evidence for future work. The disabled helper, test, integration patches, exact-oracle proofs, and raw paired observations are archived here. No production rollback is needed because the experiment was isolated and remained default off.
