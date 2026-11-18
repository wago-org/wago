# Experimental packed scalar terminals

AMD64 experimental checkpoint based on 2dd082d4. No full-function SSA, extra persistent graph, node-width increase, or synthetic operand nodes. This is work in progress, not a demonstrated performance improvement or merge recommendation.

Two clean scalar local/constant leaves can share one packed deferred node. Existing arithmetic folds retain priority. Unsupported consumers expand the ordinary tree before original lowering. Direct scalar materialization retains native choices. A separate bounded producer parses adjacent leaf/leaf/ALU sequences transactionally and allocates one node directly. Producer activation is confined to the body-loop reader; sliced and recipe replay retains ordinary per-op behavior.

Enable representation with `WAGO_EXPERIMENTAL_PACKED_TERMINAL=1`; independently enable direct producers with `WAGO_EXPERIMENTAL_PACKED_PRODUCER=1`. Both default off. Profiling and allocation-checking builds deliberately retain the ordinary representation. Unsupported locals, inline/interval states, custom instructions, active effect caches, identities and immediate ranges fall back before publication.

Focused proofs cover typed materialization, aliasing, captured bounds, overwrite and expansion, lifetime/reset, transactional rejection and source-reader provenance. The optional frozen-corpus test compares complete native output and exported metadata in both interruptibility modes. These focused gates are not full CI, TinyGo qualification, or a measured execution-latency result.

Earlier representation and boundary screens did not establish an activation gain. A direct-producer screen before the reader-scope fix likewise showed no demonstrated gain and does not qualify this final source. Compilation latency, execution latency and absolute RSS targets remain unmet. No raw benchmark archives or generated binaries are included.
