//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/internal/runtimebridge"
)

// PreparedScalarHost owns the immutable admission metadata of a bounded Go
// bridge. Its owner must serialize calls and keep the engine and buffers alive;
// replacing call buffers requires a different object or the ordinary entry API.
type PreparedScalarHost struct {
	engine                    *Engine
	args, trap, results, ctrl []byte
	slots                     uint32
	staged, bridge            uintptr
	mode                      uint8
	detached                  []byte
}

// PrepareScalarHost validates immutable buffers before publishing a live bridge.
// An unavailable live Go ABI returns nil so the owner retains its ordinary path.
func (e *Engine) PrepareScalarHost(access runtimebridge.HostScalarCallAccess, args, trap, results, ctrl []byte, slots uint32) (*PreparedScalarHost, error) {
	return e.prepareScalarHost(access, args, trap, results, ctrl, slots, false)
}

// PrepareScalarHostView prepares borrowed numeric argument and result views.
func (e *Engine) PrepareScalarHostView(access runtimebridge.HostScalarCallAccess, args, trap, results, ctrl []byte, slots uint32) (*PreparedScalarHost, error) {
	return e.prepareScalarHost(access, args, trap, results, ctrl, slots, true)
}

func (e *Engine) prepareScalarHost(access runtimebridge.HostScalarCallAccess, args, trap, results, ctrl []byte, slots uint32, view bool) (*PreparedScalarHost, error) {
	if !preparedScalarHostAvailable() {
		return nil, nil
	}
	limit := uint32(2)
	if view {
		limit = maxHostArity
	}
	if e == nil || !access.Granted() || slots>>16 > limit || slots&0xffff > limit {
		return nil, fmt.Errorf("jit: invalid prepared scalar host admission")
	}
	if err := validateTrapBuffer(trap); err != nil {
		return nil, err
	}
	if len(ctrl) < ctrlFrameSize {
		return nil, fmt.Errorf("jit: host control frame has %d bytes, need %d", len(ctrl), ctrlFrameSize)
	}
	staged, err := hostCallStubPtr()
	if err != nil {
		return nil, fmt.Errorf("jit: host-call stub: %w", err)
	}
	if _, err := initHostCtrlExtension(ctrl); err != nil {
		return nil, err
	}
	bridge := preparedScalarHostBridge()
	if view {
		bridge = preparedScalarHostViewBridge()
	}
	mode := uint8(0)
	if view {
		mode = 1
	}
	p := &PreparedScalarHost{engine: e, args: args, trap: trap, results: results, ctrl: ctrl, slots: slots, staged: staged, bridge: bridge, mode: mode}
	e.preparedScalarHost = p
	return p, nil
}

// PreparedScalarHost returns the bridge belonging to this engine and its current
// owner. Idle engine release clears the bridge before the engine can be reused.
func (e *Engine) PreparedScalarHost() *PreparedScalarHost { return e.preparedScalarHost }

// DetachNumericContext gives resource-free native segments their own stable
// basedata. Admission must exclude resource and cross-instance operations.
func (p *PreparedScalarHost) DetachNumericContext(access runtimebridge.HostScalarCallAccess, memory *JobMemory) error {
	if p == nil || p.engine == nil || p.engine.preparedScalarHost != p || p.detached != nil || !access.Granted() || memory == nil || memory.linOff < basedataSize || memory.linOff > len(memory.mem) {
		return fmt.Errorf("jit: invalid detached numeric context")
	}
	p.detached = make([]byte, basedataSize+16)
	copy(p.detached[:basedataSize], memory.mem[memory.linOff-basedataSize:memory.linOff])
	binary.LittleEndian.PutUint64(p.detached[basedataSize-offCustomCtx:], uint64(slicePtr(p.ctrl)))
	binary.LittleEndian.PutUint64(p.detached[basedataSize-offStackFence:], uint64(p.engine.StackLimit()))
	return nil
}
func (p *PreparedScalarHost) DetachedNumericContext() bool { return p.detached != nil }

// DetachedNumericContextBase identifies the private native anchor for ABI checks.
func (p *PreparedScalarHost) DetachedNumericContextBase() uintptr {
	if p == nil || p.detached == nil {
		return 0
	}
	return p.detachedBase()
}
func (p *PreparedScalarHost) detachedBase() uintptr {
	return uintptr(unsafe.Pointer(&p.detached[basedataSize]))
}

// EnableIntegerGuestContext requires an external whole-module proof that guest
// code is integer-only (and on ARM64 cannot allocate SIMD/FP registers)
// and has one immutable Go import whose validated signature
// matches the prepared slots. The private anchor excludes
// resource helpers, and callbacks keep a normal rooted Go owner.
func (p *PreparedScalarHost) EnableIntegerGuestContext(access runtimebridge.HostScalarCallAccess) error {
	if p == nil || p.engine == nil || p.engine.preparedScalarHost != p || p.detached == nil || p.mode&2 != 0 || !access.Granted() {
		return fmt.Errorf("jit: invalid integer guest context")
	}
	bridge := integerHostContextBridge(p.mode&1 != 0)
	if bridge == 0 {
		return fmt.Errorf("jit: integer guest context unavailable")
	}
	p.bridge = bridge
	p.mode |= 2
	return nil
}
func (p *PreparedScalarHost) IntegerGuestContext() bool { return p.mode&2 != 0 }
