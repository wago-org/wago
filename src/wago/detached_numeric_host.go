package wago

import (
	"encoding/binary"
	"os"
	"unsafe"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

// Only fresh tagged imports and resource-free guest segments can use the
// engine's private basedata image. The override retains the mutable-context path
// for paired measurements and diagnosis.
var armIntegerNumericEnabled = os.Getenv("WAGO_ARM64_INTEGER_HOST") != "0"

var armDetachedNumericEnabled = os.Getenv("WAGO_ARM64_DETACHED_NUMERIC") != "0"

var detachedNumericHostEnabled = os.Getenv("WAGO_DETACHED_NUMERIC_HOST") != "0"
var privateNumericLiveRouteEnabled = os.Getenv("WAGO_PRIVATE_NUMERIC_LIVE_ROUTE") != "0"
var integerNumericHostEnabled = os.Getenv("WAGO_INTEGER_NUMERIC_HOST") != "0"

func detachedNumericDispatchI32(context unsafe.Pointer, a0, a1 uint64) uint64 {
	a := (*boundedTypedHostActivation)(context)
	return I32(a.root.syncHosts[0].typedI32(AsI32(a0)))
}
func detachedNumericDispatchI32x2(context unsafe.Pointer, a0, a1 uint64) uint64 {
	a := (*boundedTypedHostActivation)(context)
	return I32(a.root.syncHosts[0].typedI32x2(AsI32(a0), AsI32(a1)))
}
func detachedNumericDispatchHostCall(context unsafe.Pointer, args, results []uint64) {
	a := (*boundedTypedHostActivation)(context)
	b := &a.root.syncHosts[0]
	b.fn.(HostCallFunc)(HostCall{params: compactHostSlots(args), results: compactHostSlots(results), sig: b.sig, exact: b.exact})
}
func detachedNumericDispatchCaller(context unsafe.Pointer, args, results []uint64) {
	a := (*boundedViewHostActivation)(context)
	invocation := a.context()
	previous := a.state.activations.boundedID.Load()
	a.state.activations.boundedID.Store(uint64(invocation.id))
	defer a.state.activations.boundedID.Store(previous)
	binding := &a.root.syncHosts[0]
	scope := &a.state.hostScope
	generation, parent := scope.beginGeneration(invocation.parent)
	caller := instanceHostModule{in: a.root, scope: scope, generation: generation, parentGeneration: parent,
		invocationID: invocation.id, reservation: invocation.reservation, exact: binding.exact}
	defer scope.end(generation, parent)
	binding.fn.(CallerHostCallFunc)(Caller{instanceHostModule: caller}, HostCall{
		params: compactHostSlots(args), results: compactHostSlots(results), sig: binding.sig, exact: binding.exact,
	})
}

func detachedNumericGoDispatch(memory *wruntime.JobMemory) bool {
	ptr := memory.CaptureInstanceContext().ImportDispatch
	if ptr == 0 {
		return false
	}
	entry := unsafe.Slice((*byte)(offHeapPtr(ptr)), wruntime.ImportDispatchEntryBytes)
	return binary.LittleEndian.Uint64(entry[wruntime.ImportDispatchCallerContextOffset:])&wruntime.ImportDispatchCallerGoHostTag != 0
}
