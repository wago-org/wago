# Rejected reduced native wrapper instructions

Keep the current wrapper. Removing its result-pointer copy, omitting already-canonical i32 truncation, and combining both changes produced fewer instructions but no compact-import improvement.

Four independent binaries ran three balanced orders under the shared device lock. Compact-import baseline remained 663.5–664.3 µs; combined changes averaged 690.9 µs, pointer-only 706.7 µs, and truncation-only 697.8 µs. All three variants were slower in every order. Thread baseline varied from 330.7 to 493.3 µs, so these timings do not establish a thread gain. Original workload checks passed with zero allocations per call.

Focused native scalar, aliases, stateful shared memory, cross-instance and thread tests passed for the combined prototype. All runtime changes were restored after archiving diagnostic sources, binary hashes and timings. No prototype publication.
