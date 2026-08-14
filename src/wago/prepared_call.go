package wago

import "os"

// preparedCallEnabled keeps instance-owned memory on the bind-once trap-cell
// path. WAGO_PREPARED_CALL=0 restores per-entry trap clearing/rebinding for A/B.
var preparedCallEnabled = os.Getenv("WAGO_PREPARED_CALL") != "0"

// directPreparedCallEnabled lets Invoke bypass the bounds-mode/ownership router
// once instantiation has already proved the common instance-owned prepared path.
// WAGO_DIRECT_PREPARED=0 restores routing through callNative for clean A/B.
var directPreparedCallEnabled = os.Getenv("WAGO_DIRECT_PREPARED") != "0"

// preparedScalarFastEnabled selects the bounded scalar PreparedFunction path.
// WAGO_PREPARED_SCALAR_FAST=0 restores generic slot marshaling for same-binary
// benchmark comparisons.
var preparedScalarFastEnabled = os.Getenv("WAGO_PREPARED_SCALAR_FAST") != "0"

// preparedPrivateEntryEnabled lets a PreparedFunction with a private,
// already-bound native context bypass the process-wide rebinding lease.
// WAGO_PREPARED_PRIVATE_ENTRY=0 restores the ordinary entry path for A/B.
var preparedPrivateEntryEnabled = os.Getenv("WAGO_PREPARED_PRIVATE_ENTRY") != "0"

// preparedIsolatedEntryEnabled lets a prepared scalar call whose instance has
// no host-visible native state enter its instance-owned Engine without taking
// the process-wide native execution lease. WAGO_PREPARED_ISOLATED_ENTRY=0
// restores the serialized private entry for A/B.
var preparedIsolatedEntryEnabled = os.Getenv("WAGO_PREPARED_ISOLATED_ENTRY") != "0"

// invokePrivateEntryEnabled lets the bounded scalar Instance.Invoke path reuse
// the same already-bound private entry as PreparedFunction. Export resolution
// remains in Invoke; WAGO_INVOKE_PRIVATE_ENTRY=0 restores the general entry.
var invokePrivateEntryEnabled = os.Getenv("WAGO_INVOKE_PRIVATE_ENTRY") != "0"

// preparedDirectIntEnabled selects register-ABI entry for adapter-free integer
// scalar leaves. WAGO_PREPARED_DIRECT_INT=0 restores the wrapper adapter.
var preparedDirectIntEnabled = os.Getenv("WAGO_PREPARED_DIRECT_INT") != "0"

// preparedDirectFPEnabled selects the fixed FP and mixed-bank register-entry
// trampolines. It is independent from the integer family so either path can be
// rolled back without changing ordinary prepared invocation.
var preparedDirectFPEnabled = os.Getenv("WAGO_PREPARED_DIRECT_FP") != "0"
