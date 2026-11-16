# Recovered checkpoint verification

The cloud workspace was replaced during publication. The repository was restored from verified remote `cc33d1037757988bf5e4a79eab4c59d38a26c1f4`. Nine pending cohort commits were reconstructed solely from already uploaded Git blobs; every intermediate tree matched the saved original manifest. The recovered remote checkpoint is `3548a64d1e2afd3aa3bcc9851a047bffa1b5c0e4`, tree `db0dda5cf1ad8c53061e55b1e3a2c2b8d5c3f89b`.

Fresh verification used official Go 1.27.1, TinyGo 0.42.0, and pinned spec-v3 corpus `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Unfiltered default Wago, native/checked GC, AMD64 backend, direct and compact cores passed; native debug passed. TinyGo initially failed its known duplicate task-exit linker symbol. Applying the repository's existing `scripts/patch-tinygo-042.sh` compatibility patch restored passing native and wrapper tests. Logs are retained here, including the failed setup attempt.

Later SSE, shadow and weighted work is not implied recovered or qualified by this checkpoint. Reconstructed code needs explicit source-identity evidence and fresh tests. Historical timings remain historical; this verification does not establish the V8 target.
