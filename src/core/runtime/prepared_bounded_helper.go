//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"encoding/binary"
	"fmt"
	goruntime "runtime"
	"unsafe"

	"github.com/wago-org/wago/internal/runtimebridge"
)

// CallBoundedHelper requires a compiler proof that entry and every continuation
// perform bounded native work and can yield only to the admitted Go helper.
// Helpers run on the Go stack with ordinary preemption and GC safe points. The
// mask permits callsite/safepoint bits while checking the complete dispatch tag
// and helper identity. Foreign frames and changed slot shapes fail closed.
// The helper borrows its arguments from the parked frame and must not retain
// them or re-enter guest code. No engine scratch reservation is required.
func (p *PreparedHostScalarCall) CallBoundedHelper(access runtimebridge.HostScalarCallAccess, importMask, importValue uint32, host HostCall) error {
	if !access.Granted() || p == nil || p.engine == nil || host == nil || importMask == 0 || importValue&^importMask != 0 {
		return fmt.Errorf("jit: invalid bounded helper admission")
	}
	// This path must always yield back to Go; a native import trampoline is
	// outside its proof, even if a caller previously installed one.
	if !p.boundedHelperFrame {
		if err := initBoundedGoHostCtrlFrame(p.ctrl); err != nil {
			return err
		}
		p.boundedHelperFrame = true
	}
	clearTrapUnlessInterrupted(p.trap)
	root := slicePtr(p.ctrl)
	enterNativeBounded(p.code, slicePtr(p.serArgs), p.linMemBase, slicePtr(p.trap), slicePtr(p.results), p.engine.stackTop)
	for {
		switch tc := loadTrap(p.trap); {
		case tc == hostCallPending:
			imp := binary.LittleEndian.Uint32(p.ctrl[hcImportIdx:])
			if uintptr(binary.LittleEndian.Uint64(p.trap[8:])) != root || imp&importMask != importValue || binary.LittleEndian.Uint32(p.ctrl[hcNArgs:]) != 2 {
				return fmt.Errorf("jit: bounded helper escaped its admitted frame or signature")
			}
			args := unsafe.Slice((*uint64)(unsafe.Pointer(&p.ctrl[hcArgs])), 2)
			host(root, imp, args, nil)
		case tc != 0:
			return trapErrorFromBuffer(TrapCode(tc), p.trap)
		default:
			goruntime.KeepAlive(p)
			return nil
		}
		clearTrapUnlessInterrupted(p.trap)
		if TrapCode(loadTrap(p.trap)) == TrapInterrupted {
			return trapErrorFromBuffer(TrapInterrupted, p.trap)
		}
		stackTop := p.engine.StackTop()
		prepareHostResume(p.ctrl, p.trap, stackTop, p.engine.StackLimit())
		resumeNativeBounded(root, stackTop)
	}
}
