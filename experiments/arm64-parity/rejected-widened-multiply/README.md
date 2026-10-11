# Rejected widening multiply cover

Apple M4 Max ARM64; isolated compiler base c74d21c1, production untouched. Default-off general signed/unsigned widening multiply, add and subtract covers; all-definition signed32 local proof handles zero-initialized declared locals, direct narrow loads, extensions and fitting constants. Unknown definitions/parameters decline. No workload names, artifacts or manifest arguments enter admission.

Focused diagnostic encoder/backend/catalog checks and independent native execution tests passed: signed/unsigned overflow extremes, both bounds modes, borrowed destination aliases, retained old local values, control joins, narrow loads, fitting/nonfitting constants. Encoder words independently matched Clang. These are focused checks, not full corpus qualification.

Six alternating 100ms pairs: DCT execution -0.11%, compilation +2.90%; optical flow execution -0.36%, compilation +2.83%. Four image controls and stats regression are byte-identical; their timing fluctuations are not gains. DCT changes 4052 to3940 bytes: 28 SMULL and36 SMADDL replace scalar products, total1013 to985 instructions. Optical flow changes2512 to2436 bytes. Ordinary MADD and SMADDL generally have the same instruction count.

Mitigation removed whole-body signed32 proof to retain only direct/concrete widening sources. Six150ms pairs: DCT execution -0.25%, compile +0.49%; optical execution +0.87%, compile +1.16%. Regression remains unchanged. The first execution pair may overlap a root worktree checkout; no meaningful gain is claimed. Extra source proof is therefore not worth its compilation cost; direct-only lowering also showed no useful application payoff. Removed all hooks and encoder additions from active candidate after measurement.

Raw JSONL, tests, independent encoder proof, native counts and DCT disassembly are retained. prototype.patch restores the complete initial default-off source and tests; direct mitigation skips proveSigned32Locals during compilation. No full timed or oracle corpus sweep was run for this rejected candidate.
