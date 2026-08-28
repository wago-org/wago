//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestCallFrameHeadroomIncludesFrameRecord(t *testing.T) {
	if got, want := nativeFrameStackFenceHeadroom(true), shared.MaxNativeFrameBytes-16; got != want {
		t.Fatalf("call frame headroom = %d, want %d", got, want)
	}
	if got := nativeFrameStackFenceHeadroom(false); got != shared.MaxNativeFrameBytes {
		t.Fatalf("leaf frame headroom = %d, want %d", got, shared.MaxNativeFrameBytes)
	}
}
