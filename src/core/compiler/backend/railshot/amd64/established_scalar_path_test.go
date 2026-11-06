//go:build amd64

package amd64

import "testing"

// These serial tests assert named Valent/region emitter transformations. The
// whole-function shared scalar compiler does not run those emitters (runBody
// returns scalarBody for eligible functions). Select that implementation only
// for its own contract tests; shared admission and semantic pairs stay enabled.
func useEstablishedScalarPath(t *testing.T) {
	t.Helper()
	saved := sharedScalarEnabled
	sharedScalarEnabled = false
	t.Cleanup(func() { sharedScalarEnabled = saved })
}
