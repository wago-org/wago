//go:build arm64

package arm64

import "testing"

func requireNativeCompaction(t *testing.T) {
	t.Helper()
	if !nativeCompactionAvailable {
		t.Skip("native compaction is not present in this build")
	}
}
