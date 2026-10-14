//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"fmt"
	"github.com/wago-org/wago/internal/runtimebridge"
	goruntime "runtime"
	"unsafe"
)

// CallFixedContext retains the prepared fixed-slot and bounded-Go-segment
// admission while passing a rooted context to a static full host dispatcher.
// It keeps the ordinary FP/root/trap contract and does not detach resources.
// The fallback retains ordinary dispatch when the live bridge is unavailable.
func (p *PreparedHostScalarCall) CallFixedContext(access runtimebridge.HostScalarCallAccess, context unsafe.Pointer, fixed FixedScalarHostContextCall, fallback FixedScalarHostCall) error {
	if p == nil || p.engine == nil || !p.boundedSegments || !p.fixed || !access.Granted() || context == nil || fixed == nil || fallback == nil {
		return fmt.Errorf("jit: invalid prepared bounded host context")
	}
	clearTrapUnlessInterrupted(p.trap)
	used, err := p.engine.tryInlinePreparedHostContext(p, context, fixed)
	if !used {
		err = p.engine.callWithBoundedHostLoop(p.code, p.serArgs, p.linMemBase, p.trap, p.results, p.ctrl, p.fixedSlots, fallback)
	}
	goruntime.KeepAlive(p)
	goruntime.KeepAlive(context)
	return err
}
