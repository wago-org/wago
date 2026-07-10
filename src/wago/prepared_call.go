package wago

import "os"

// preparedCallEnabled keeps instance-owned memory on the bind-once trap-cell
// path. WAGO_PREPARED_CALL=0 restores per-entry trap clearing/rebinding for A/B.
var preparedCallEnabled = os.Getenv("WAGO_PREPARED_CALL") != "0"

// directPreparedCallEnabled lets Invoke bypass the bounds-mode/ownership router
// once instantiation has already proved the common instance-owned prepared path.
// WAGO_DIRECT_PREPARED=0 restores routing through callNative for clean A/B.
var directPreparedCallEnabled = os.Getenv("WAGO_DIRECT_PREPARED") != "0"
