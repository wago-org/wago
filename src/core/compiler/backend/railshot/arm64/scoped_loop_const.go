//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

// Immutable leases exist only inside proven call-free loops. The process
// default is opt-in; the per-compilation policy owns each selected state.
var scopedLoopConstsEnabled = os.Getenv("WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST") != "0"

// Keep only the lease metadata in compiler scratch state. A pointer
// to the full function hint view can make scan-local views escape even when the
// optimization is disabled in a particular compilation.
type scopedLoopConstHints struct {
	loopIntConst      [4]int64
	flags             funcHintFlags
	loopIntConstTypes uint8
	loopIntConstCount uint8
}

type scopedLoopConstUses struct {
	hints   *scopedLoopConstHints
	mask    uint8
	blocked bool
}

func (u *scopedLoopConstUses) note(op byte, source *wasm.Reader) {
	immediate := *source
	_, _ = u.consume(op, &immediate)
}
