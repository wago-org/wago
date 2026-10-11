package wago

import (
	"os"
	goruntime "runtime"
	"unsafe"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

// A signature-specific owner calls the rooted closure without an adapter frame.
// The override retains the fixed dispatcher for paired diagnosis.
var directIntegerI32Enabled = os.Getenv("WAGO_DIRECT_INTEGER_I32") != "0"

var armPrivateNumericSessionEnabled = os.Getenv("WAGO_ARM64_PRIVATE_SESSION") != "0"

var privateNumericSessionEnabled = os.Getenv("WAGO_PRIVATE_NUMERIC_SESSION") != "0"

// This context belongs to one reserved session and one immutable private bridge.
// Reentry uses another engine; the active guard prevents overlapping invocations.
type privateNumericSession struct {
	prepared   *wruntime.PreparedScalarHost
	activation boundedViewHostActivation
	scalar     wruntime.FixedScalarHostContextCall
	view       wruntime.FixedHostContextCallView
	directI32  func(int32) int32
	owner      *wruntime.IntegerI32Owner
	viewOwner  *wruntime.IntegerViewOwner
}

func newPrivateNumericSession(fn *WasmFunc, p *wruntime.PreparedScalarHost, state *instancePluginState) *privateNumericSession {
	h := &privateNumericSession{prepared: p, activation: boundedViewHostActivation{boundedTypedHostActivation: boundedTypedHostActivation{root: fn.in, ctrl: offHeapSlicePtr(fn.in.ctrl), state: state}}}
	binding := &fn.in.syncHosts[0]
	switch binding.scalarKind {
	case syncHostTypedI32:
		h.scalar = detachedNumericDispatchI32
		if (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") && directIntegerI32Enabled && p.IntegerGuestContext() {
			h.directI32 = (func(int32) int32)(binding.typedI32)
			h.owner = p.IntegerI32Owner()
		}
	case syncHostTypedI32x2:
		h.scalar = detachedNumericDispatchI32x2
	case syncHostTypedI64:
		h.scalar = detachedNumericDispatchI64
	default:
		h.view = detachedNumericDispatchHostCall
		if _, ok := binding.fn.(CallerHostCallFunc); ok {
			h.view = detachedNumericDispatchCaller
		}
	}
	if (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") && h.view != nil {
		h.viewOwner = p.IntegerViewOwner()
	}
	return h
}

func (h *privateNumericSession) invokeOwned(fn *WasmFunc, args []uint64) (out []uint64, err error) {
	in := fn.in
	defer func() {
		h.activation.invocation = hostInvocationContext{}
		if r := recover(); r != nil {
			setNativeSyncPanicError(r, &err)
		}
		if err != nil {
			err = in.decorateTrap(err)
		}
	}()
	if activeHostInvocationBindings.Load() != 0 {
		restore := bindHostInvocationParent(in, nil)
		defer restore()
	}
	copyPublicScalarSlotsByClass(nativeUint64Slots(in.serArgs), args, fn.paramWide, fn.paramWidthClass)
	if h.directI32 != nil {
		err = h.prepared.CallIntegerI32(fn.entry, 1, h.directI32)
	} else if h.scalar != nil {
		if h.prepared.IntegerGuestContext() {
			err = h.prepared.CallInteger(fn.entry, 1, unsafe.Pointer(&h.activation.boundedTypedHostActivation), h.scalar)
		} else {
			err = h.prepared.Call(fn.entry, 1, unsafe.Pointer(&h.activation.boundedTypedHostActivation), h.scalar)
		}
	} else {
		if h.prepared.IntegerGuestContext() {
			err = h.prepared.CallIntegerView(fn.entry, 1, unsafe.Pointer(&h.activation), h.view)
		} else {
			err = h.prepared.CallView(fn.entry, 1, unsafe.Pointer(&h.activation), h.view)
		}
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	if err != nil {
		return nil, err
	}
	out = in.resultVals[:fn.resultSlots]
	copyPublicScalarSlotsByClass(out, nativeUint64Slots(in.results), fn.resultWide, fn.resultWidthClass)
	return out, nil
}
