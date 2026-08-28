//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestCallFrameHeadroomIncludesFrameRecord(t *testing.T) {
	if got, want := nativeFrameStackFenceHeadroom(true), shared.MaxNativeFrameBytes-shared.MaxNativeInboundCallBytes-16; got != want {
		t.Fatalf("call frame headroom = %d, want %d", got, want)
	}
	if got, want := nativeFrameStackFenceHeadroom(false), shared.MaxNativeFrameBytes-shared.MaxNativeInboundCallBytes; got != want {
		t.Fatalf("leaf frame headroom = %d, want %d", got, want)
	}
}
