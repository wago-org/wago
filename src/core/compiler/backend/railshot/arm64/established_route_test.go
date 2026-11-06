//go:build arm64

package arm64

import "testing"

// establishedScalarRoute selects the target-local lowering whose exact
// peepholes/register assignments the caller tests. Shared scalar has separate
// allocation and folding; TestSharedEstablishedPathPairs checks both routes.
// Keep this scoped to individual tests, never package initialization/TestMain.
func establishedScalarRoute(t *testing.T) {
	t.Helper()
	previous := sharedScalarEnabled
	sharedScalarEnabled = false
	t.Cleanup(func() { sharedScalarEnabled = previous })
}
