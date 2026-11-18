//go:build amd64 && !tinygo && !wago_profile && !wago_streamcheck

package amd64

// Stream ownership checking is independent of the legacy graph checker,
// which deliberately routes functions away from the experimental engine.
const streamOwnershipChecks = false
