//go:build !wago_profile

package profiling

import "fmt"

// Record is unavailable in ordinary builds; no capture implementation is linked.
func Record(Options, Harness) error {
	return fmt.Errorf("wago: application profiling requires -tags=wago_profile")
}
