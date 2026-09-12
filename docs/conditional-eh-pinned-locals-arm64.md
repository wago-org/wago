# Arm64 exception snapshot audit

The pinned-local audit found the same storage problem in arm64
`emitEHCatchRoute`. It used separate 16-entry state and local-index arrays and
failed when a call-making exception function pinned more than 16 locals.

Unlike amd64, arm64 permits local pins in exception modules. A module with 24
live f64 parameters and a call to a throwing function reproduced the failure:
`arm64: too many pinned locals in EH route`. After the fix, compiler statistics
show 23 actual pins.

The handler now uses the existing `packedLocStates` format, indexed by local
number, as ordinary control-frame merges already do. This format covers the
existing whole-function pin eligibility range. Larger functions compile with
canonical locals under the unchanged allocator policy. No new compiler limit,
spill, pool, or allocator policy was added. The handler restores states directly,
including the all-zero state; it does not use the absent-snapshot sentinel.

`TestEHCatchRouteSnapshotAbove16Pins` checks compilation and the actual pin count.
`TestExceptionSnapshotPreservesWideFPLocalsARM64` executes the throw and catch,
then checks every returned f64 bit pattern, including signed zero and a NaN
payload. Both tests pass under qemu-aarch64 on the linux/amd64 development host.

Other arm64 snapshots in `call.go` and `gc.go` already copy complete local slices.
They need no change.
