//go:build !wago_regalloccheck && wago_profile

package shared

// Source provenance is retained only when profiling is compiled in.
type ScalarState struct {
	scalarSourceState
	scalarState
}
