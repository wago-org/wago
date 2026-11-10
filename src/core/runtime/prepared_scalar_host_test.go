//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/internal/runtimebridge"
)

func TestPreparedScalarHostAdmission(t *testing.T) {
	if !preparedScalarHostAvailable() {
		t.Skip("live bridge unavailable")
	}
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	for _, tc := range []struct {
		name       string
		access     runtimebridge.HostScalarCallAccess
		trap, ctrl []byte
		slots      uint32
	}{
		{"access", runtimebridge.HostScalarCallAccess{}, make([]byte, TrapBufferBytes), make([]byte, ctrlFrameSize), 1 | 1<<16},
		{"trap", runtimebridge.GrantHostScalarCall(), make([]byte, TrapBufferBytes-1), make([]byte, ctrlFrameSize), 1 | 1<<16},
		{"ctrl", runtimebridge.GrantHostScalarCall(), make([]byte, TrapBufferBytes), make([]byte, ctrlFrameSize-1), 1 | 1<<16},
		{"extension", runtimebridge.GrantHostScalarCall(), make([]byte, TrapBufferBytes), make([]byte, ctrlFrameSize+1), 1 | 1<<16},
		{"params", runtimebridge.GrantHostScalarCall(), make([]byte, TrapBufferBytes), make([]byte, ctrlFrameSize), 3 | 1<<16},
		{"results", runtimebridge.GrantHostScalarCall(), make([]byte, TrapBufferBytes), make([]byte, ctrlFrameSize), 1 | 3<<16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := e.PrepareScalarHost(tc.access, nil, tc.trap, nil, tc.ctrl, tc.slots)
			if err == nil || p != nil || e.PreparedScalarHost() != nil {
				t.Fatalf("admitted malformed entry: %v, %v", p, err)
			}
		})
	}
	var empty PreparedScalarHost
	context := new(uint64)
	if err := empty.Call(1, 1, unsafe.Pointer(context), func(unsafe.Pointer, uint64, uint64) uint64 { t.Fatal("callback ran"); return 0 }); err == nil {
		t.Fatal("zero entry reached native code")
	}
}

func TestPreparedScalarHostEngineRelease(t *testing.T) {
	if !preparedScalarHostAvailable() {
		t.Skip("live bridge unavailable")
	}
	e, err := AcquireEngine()
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.PrepareScalarHost(runtimebridge.GrantHostScalarCall(), nil, make([]byte, TrapBufferBytes), nil, make([]byte, ctrlFrameSize), 1|1<<16)
	if err != nil || p == nil || e.PreparedScalarHost() != p {
		e.Close()
		t.Fatalf("prepare: %v, %v", p, err)
	}
	if err := ReleaseEngine(e); err != nil {
		t.Fatal(err)
	}
	if e.PreparedScalarHost() != nil {
		t.Fatal("released engine retained old call buffers")
	}
	reused, err := AcquireEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseEngine(reused)
	if reused.PreparedScalarHost() != nil {
		t.Fatal("reused engine retained old call buffers")
	}
}

func TestPreparedScalarHostViewAdmissionAndMode(t *testing.T) {
	if !preparedScalarHostAvailable() {
		t.Skip("live bridge unavailable")
	}
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	access := runtimebridge.GrantHostScalarCall()
	trap, ctrl := make([]byte, TrapBufferBytes), make([]byte, ctrlFrameSize)
	if p, err := e.PrepareScalarHostView(access, nil, trap, nil, ctrl, (maxHostArity+1)|1<<16); err == nil || p != nil {
		t.Fatal("view above admitted arity was accepted")
	}
	view, err := e.PrepareScalarHostView(access, nil, trap, nil, ctrl, maxHostArity|maxHostArity<<16)
	if err != nil || view == nil {
		t.Fatalf("maximal numeric view: %v, %v", view, err)
	}
	context := new(uint64)
	if err := view.Call(1, 1, unsafe.Pointer(context), func(unsafe.Pointer, uint64, uint64) uint64 { t.Fatal("wrong-mode callback ran"); return 0 }); err == nil {
		t.Fatal("view admitted scalar ABI")
	}
	scalar, err := e.PrepareScalarHost(access, nil, trap, nil, ctrl, 1|1<<16)
	if err != nil || scalar == nil {
		t.Fatalf("scalar: %v, %v", scalar, err)
	}
	if err := scalar.CallView(1, 1, unsafe.Pointer(context), func(unsafe.Pointer, []uint64, []uint64) { t.Fatal("wrong-mode callback ran") }); err == nil {
		t.Fatal("scalar admitted view ABI")
	}
}

func TestPreparedScalarHostIntegerModeRequiresDetachedProof(t *testing.T) {
	if !preparedScalarHostAvailable() {
		t.Skip("live bridge unavailable")
	}
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	access := runtimebridge.GrantHostScalarCall()
	p, err := e.PrepareScalarHost(access, make([]byte, 8), make([]byte, TrapBufferBytes), make([]byte, 8), make([]byte, ctrlFrameSize), 1|1<<16)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.EnableIntegerGuestContext(access); err == nil {
		t.Fatal("integer mode admitted ordinary resource context")
	}
	context := new(uint64)
	fixed := func(unsafe.Pointer, uint64, uint64) uint64 { t.Fatal("unadmitted integer owner ran"); return 0 }
	if err := p.CallInteger(1, 1, unsafe.Pointer(context), fixed); err == nil {
		t.Fatal("ordinary mode admitted integer owner")
	}
	memory, err := NewJobMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	if err := p.DetachNumericContext(access, memory); err != nil {
		t.Fatal(err)
	}
	if err := p.EnableIntegerGuestContext(runtimebridge.HostScalarCallAccess{}); err == nil {
		t.Fatal("integer mode ignored grant")
	}
	err = p.EnableIntegerGuestContext(access)
	if integerHostContextBridge(false) == 0 {
		if err == nil || p.IntegerGuestContext() {
			t.Fatal("unsupported target admitted integer owner")
		}
		return
	}
	if err != nil || !p.IntegerGuestContext() {
		t.Fatalf("integer admission %v", err)
	}
	if err := p.Call(1, 1, unsafe.Pointer(context), fixed); err == nil {
		t.Fatal("integer mode admitted ordinary owner")
	}
	if err := p.EnableIntegerGuestContext(access); err == nil {
		t.Fatal("integer mode rebound")
	}
}
