# Single-import early dispatch did not reach the thread workload

Do not retain the prototype that broadens the earlier native-alias branch to stateful single-import modules and uses full spills there. Focused native/shared/runtime validation passes, but the actual thread identity fixture contains loads and stores. BoundedHostSegments excludes memory operations, so dynamicHostFast is false and this outer early branch is never selected for its call.

The prototype thread image is byte-for-byte identical to retained 0ae6f232 (864 bytes, SHA256 0e8f976bba990ed1ede25ef5548eeb0edc0e8c6abbcf753c823023b046573021). Three alternating public API pairs change native thread time +3.32%, compact +1.46% and opaque +0.76%, with zero allocations. These timings establish no useful improvement; they do not demonstrate a regression caused by the unreachable branch. All prototype compiler changes were removed. Investigate the actual cross-instance/native shortcut or the Go callback bridge instead.
