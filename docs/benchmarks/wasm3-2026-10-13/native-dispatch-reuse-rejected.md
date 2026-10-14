# Rejected caller dispatch load reuse

Retain the existing loads. One prototype reused the direct-Go split's raw caller word in X12. A second also reused its dispatch pointer in X16 when local spills and immediate wrapper-buffer addresses could not overwrite it. Both kept the ordinary fallback and passed focused native, shared-memory, cross-instance, FP-local and artifact tests; the second also exercised wrapper buffers beyond the immediate SP range.

Three alternating public prepared Invoke pairs did not establish a material native thread or compact gain. Tag-only means were compact +1.3%, native thread -2.5%, and opaque callback -1.1%. Tag plus pointer means were compact -1.9%, native thread +2.3%, and opaque -1.7%. The native thread pairs disagreed in both experiments. Every call passed the original oracle with zero per-call allocations.

The additional register-liveness invariants are not justified by these results. All source and experiment-only test changes were removed after archiving sources, logs and timing rows. No prototype publication.
