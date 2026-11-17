# Cooperative poll padding diagnostic

Build with `wago_experimental_poll_padding`; select `WAGO_AMD64_POLL_PADDING=0`
(default) or `32` at process startup. The same binary supports both modes.
Other values panic only in this explicitly tagged diagnostic. Untagged builds
ignore this variable and add no padding.

Every original trap-cell load, TEST, conditional trap edge and source attribution
remains. NOPs follow the conditional edge until the emitted poll's length is a
multiple of 32 bytes **relative to its start**, not an absolute alignment boundary.
The fall-through flags/registers and taken trap path remain unchanged. Padding
costs up to 31 bytes per site and extra executed NOPs; it can worsen instruction
cache use, compilation size, or latency.

Hypothesis only: poll insertion changes hot-code alignment and may explain part
of bitmap's Go/TinyGo discrepancy. This does not remove poll overhead or establish
that alignment explains it. Final branch relaxation, function placement and other
alignment passes can still change modulo-32 layout, so inspect final images before
interpreting a comparison. No measured gain or correctness qualification yet.
