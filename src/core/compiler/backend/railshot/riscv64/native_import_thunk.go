//go:build linux && !tinygo

package riscv64

import (
	"fmt"
	"unsafe"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// The activation owner must retain the framed target, both contexts and homes, serialize native
// execution, and publish the returned bytes through the normal code mapper.
// Absolute addresses belong to this activation owner and must never be persisted.
// Entry inherits a valid aligned foreign SP and installed nonzero stack fence.
// The target transport is retained off-heap, disjoint, and holds the full bounded
// 256-slot ABI: a narrow export may make much wider internal calls. Stack-owned
// public activations use their own full-width transport instead of these slices. Only the
// imported signature's cells cross the instance boundary. The owner must rebind
// both contexts after a cold unwind, which skips the normal epilogue.
type nativeImportTarget struct {
	entry, home, context       uintptr
	arguments, results         []byte
	argumentSlots, resultSlots int
	stackOwnedActivation       bool
	invocationContext          bool
	hostTrampoline             uintptr
}

const nativeImportFrameBytes = 64
const nativeImportTransportBytes = 256 * 8 // legacy private activation

// A v128 value occupies two physical slots within the 256-value public ABI.
const MaxNativeCallSlots = 2 * MaxVectorFunctionValues
const nativePublicImportTransportBytes = MaxNativeCallSlots * 8

// NativeImportTarget is a trusted physically retained framed RV64 target. The
// transport slices must be retained off-heap for the complete activation; they
// may belong to the importing edge owner, independently of the target's root
// invocation buffers. This descriptor is never serialized.
type NativeImportTarget struct {
	Entry, Home, Context       uintptr
	Arguments, Results         []byte
	ArgumentSlots, ResultSlots int
	// StackOwnedActivation gives every activation full256-cell transport and
	// a fixed scalar host frame. Public Go/HostFuncRef calls must fit64 slots.
	StackOwnedActivation bool
	// InvocationContext requires both retained contexts to have the RV64 public
	// extension. Legacy private120-byte contexts leave this false.
	InvocationContext bool
	HostTrampoline    uintptr
}

// NativeImportThunk emits an instance-owned bridge after target attachment and
// exact signature/ABI validation. Publication and lifetime remain the owner's
// responsibility; callers must use the normal W^X/process-wide icache mapper.
func NativeImportThunk(callee NativeImportTarget, callerHome, callerContext uintptr, importIndex uint32) ([]byte, error) {
	return nativeImportThunk(nativeImportTarget{entry: callee.Entry, home: callee.Home, context: callee.Context, arguments: callee.Arguments, results: callee.Results, argumentSlots: callee.ArgumentSlots, resultSlots: callee.ResultSlots, stackOwnedActivation: callee.StackOwnedActivation, hostTrampoline: callee.HostTrampoline, invocationContext: callee.InvocationContext}, callerHome, callerContext, importIndex)
}

var nativeImportContextFields = [...]struct{ source, destination int32 }{
	{0, 40}, {8, 80}, {16, abi.FuncRefDescPtrOffset},
	{24, abi.PassiveElemPtrOffset}, {32, abi.GlobalsPtrOffset},
	{40, abi.PassiveDataPtrOffset}, {48, abi.TableDirPtrOffset},
	{56, abi.MemoryDirPtrOffset}, {64, abi.ImportDispatchPtrOffset},
	{coreruntime.InstanceContextGCNativeViewOffset, abi.GCNativeViewPtrOffset},
	{coreruntime.InstanceContextEHTagDirOffset, abi.EHTagDirPtrOffset},
}

var nativeImportExecutionFields = [...]int32{
	abi.RV64TrapContinuationOffset, abi.RV64TrapReentrySPOffset,
	abi.StackFenceOffset, abi.TrapCellPtrOffset,
}

func nativeImportThunk(callee nativeImportTarget, callerHome, callerContext uintptr, importIndex uint32) ([]byte, error) {
	if callee.entry == 0 || callee.entry&15 != 0 || callee.home == 0 || callee.home&7 != 0 || callee.context == 0 || callee.context&7 != 0 || callerHome == 0 || callerHome&7 != 0 || callerContext == 0 || callerContext&7 != 0 || importIndex >= MaxHostFunctionImports {
		return nil, fmt.Errorf("rv64: native import requires aligned retained framed target and caller identities")
	}
	transportBytes := nativeImportTransportBytes
	if callee.invocationContext {
		transportBytes = nativePublicImportTransportBytes
	}
	if callee.argumentSlots < 0 || callee.argumentSlots > transportBytes/8 || callee.resultSlots < 0 || callee.resultSlots > transportBytes/8 {
		return nil, fmt.Errorf("rv64: native import requires bounded target transport")
	}
	var argumentBase, resultBase uintptr
	if !callee.stackOwnedActivation || len(callee.arguments) != 0 || len(callee.results) != 0 {
		providedBytes := transportBytes
		// Stack-owned activations never use these optional legacy buffers.
		// Retain their original range checks when an older owner supplies them.
		if callee.stackOwnedActivation && len(callee.arguments) == nativeImportTransportBytes && len(callee.results) == nativeImportTransportBytes {
			providedBytes = nativeImportTransportBytes
		}
		if len(callee.arguments) != providedBytes || len(callee.results) != providedBytes {
			return nil, fmt.Errorf("rv64: native import requires bounded full-width target transport")
		}
		argumentBase, resultBase = uintptr(unsafe.Pointer(&callee.arguments[0])), uintptr(unsafe.Pointer(&callee.results[0]))
		if argumentBase&7 != 0 || resultBase&7 != 0 || argumentBase < resultBase+uintptr(providedBytes) && resultBase < argumentBase+uintptr(providedBytes) {
			return nil, fmt.Errorf("rv64: native import requires aligned disjoint target transport")
		}
	}
	if callee.stackOwnedActivation && (callee.hostTrampoline == 0 || callee.hostTrampoline&3 != 0) {
		return nil, fmt.Errorf("rv64: stack activation requires its stable host trampoline")
	}
	if callee.invocationContext && !callee.stackOwnedActivation {
		return nil, fmt.Errorf("rv64: invocation views require stack-owned import activation")
	}
	frameBytes := nativeImportFrameBytes
	if callee.stackOwnedActivation {
		frameBytes += 16 + HostFixedFrameBytes + 2*transportBytes
	}
	viewOffset := frameBytes
	if callee.invocationContext {
		frameBytes += abi.RV64InvocationViewBytes
	}
	f := target{mode: emissionMode{production: true, functionIndex: importIndex}}
	a := &f.a
	// A same-home sibling's valid thunk is still a foreign caller identity. Check
	// the live descriptor anchor before any stack or basedata write.
	a.Constant64(enc.Scratch1, int64(callerHome))
	wrongHome := a.BranchLong(enc.NE, enc.X25, enc.Scratch1)
	wrongTransport := a.BranchLong(enc.NE, enc.X11, enc.X25)
	a.Ld(enc.X28, enc.X25, -abi.FuncRefDescPtrOffset)
	missingAnchor := a.BranchLong(enc.EQ, enc.X28, enc.Zero)
	a.Ld(enc.X29, enc.X28, coreruntime.FuncRefContextOffset)
	a.Constant64(enc.Scratch1, int64(callerContext))
	wrongContext := a.BranchLong(enc.NE, enc.X29, enc.Scratch1)
	var invocationGuards []int
	if callee.invocationContext {
		a.Ld(enc.X9, enc.X25, -abi.RV64InvocationViewOffset)
		invocationGuards = nativeInvocationViewGuards(a, enc.X9, 0)
		a.Constant64(enc.Scratch1, int64(callerContext))
	}
	a.Ld(enc.X29, enc.X25, -abi.StackFenceOffset)
	below := a.BranchLong(enc.LTU, enc.SP, enc.X29)
	a.Sub(enc.X28, enc.SP, enc.X29)
	a.Constant64(enc.Scratch0, int64(frameBytes))
	short := a.BranchLong(enc.LTU, enc.X28, enc.Scratch0)
	a.Sub(enc.SP, enc.SP, enc.Scratch0)
	a.Sd(enc.RA, enc.SP, 0)
	a.Sd(enc.X25, enc.SP, 8)
	a.Sd(enc.Scratch1, enc.SP, 16) // caller context, still live from identity check
	a.Sd(enc.X10, enc.SP, 24)
	a.Sd(enc.X12, enc.SP, 32)
	a.Sd(enc.X13, enc.SP, 40)
	a.Sd(enc.X24, enc.SP, 48)
	a.Ld(enc.Scratch0, enc.X25, -40)
	a.Sd(enc.Scratch0, enc.SP, 56) // active caller ctrl may differ from canonical ctx
	if callee.invocationContext {
		a.Sd(enc.X9, enc.SP, 72)
	}
	if callee.stackOwnedActivation {
		a.Constant64(enc.X28, nativeImportFrameBytes+16+HostFixedFrameBytes)
		a.Add(enc.X28, enc.SP, enc.X28)
	} else {
		a.Constant64(enc.X28, int64(argumentBase))
	}
	for i := 0; i < callee.argumentSlots; i++ {
		source, offset := f.addressAt(enc.X10, int32(i*8), enc.Scratch1)
		a.Ld(enc.Scratch0, source, offset)
		destination, offset := f.addressAt(enc.X28, int32(i*8), enc.Scratch1)
		a.Sd(enc.Scratch0, destination, offset)
	}
	a.Addi(enc.X10, enc.X28, 0)
	if callee.stackOwnedActivation {
		a.Constant64(enc.X13, int64(nativeImportFrameBytes+16+HostFixedFrameBytes+transportBytes))
		a.Add(enc.X13, enc.SP, enc.X13)
	} else {
		a.Constant64(enc.X13, int64(resultBase))
	}
	a.Constant64(enc.X11, int64(callee.home))
	a.Constant64(enc.Scratch1, int64(callee.context))
	copyContext := func(destination enc.Reg) {
		for _, field := range nativeImportContextFields {
			if callee.stackOwnedActivation && field.source == 0 {
				continue
			}
			a.Ld(enc.Scratch0, enc.Scratch1, field.source)
			a.Sd(enc.Scratch0, destination, -field.destination)
		}
	}
	if callee.invocationContext {
		a.Ld(enc.X28, enc.SP, 72)
		nativeInvocationActivation(a, enc.X28, viewOffset, nativeImportFrameBytes+16+HostFixedFrameBytes, nativeImportFrameBytes+16+HostFixedFrameBytes+transportBytes, transportBytes)
		a.Sd(enc.X9, enc.X11, -abi.RV64InvocationViewOffset)
	}
	copyContext(enc.X11)
	if callee.stackOwnedActivation {
		// No mutable producer control pointer crosses this activation boundary.
		a.Sd(enc.Scratch1, enc.SP, nativeImportFrameBytes)
		a.Constant64(enc.X30, int64(callee.hostTrampoline))
		a.Addi(enc.X28, enc.SP, nativeImportFrameBytes+16)
		a.Sd(enc.X30, enc.X28, HostTrampolineOffset)
		a.Sd(enc.X28, enc.X11, -40)
		a.Ld(enc.Scratch1, enc.SP, nativeImportFrameBytes)
	}
	a.Sd(enc.Scratch1, enc.X11, -abi.RV64NativeContextOffset)
	for _, offset := range nativeImportExecutionFields {
		a.Ld(enc.Scratch0, enc.X25, -offset)
		a.Sd(enc.Scratch0, enc.X11, -offset)
	}
	a.Addi(enc.X25, enc.X11, 0)
	a.Constant64(enc.X28, int64(callee.entry))
	a.Jalr(enc.RA, enc.X28, 0)
	a.Ld(enc.X25, enc.SP, 8)
	// A nested Go callback may have reentered the caller on another engine. The
	// active callee's refreshed continuation/fence/trap now belongs to this call.
	a.Constant64(enc.Scratch1, int64(callee.home))
	for _, offset := range nativeImportExecutionFields {
		a.Ld(enc.Scratch0, enc.Scratch1, -offset)
		a.Sd(enc.Scratch0, enc.X25, -offset)
	}
	a.Ld(enc.Scratch1, enc.SP, 16)
	copyContext(enc.X25)
	if callee.invocationContext {
		a.Ld(enc.Scratch0, enc.SP, 72)
		a.Sd(enc.Scratch0, enc.X25, -abi.RV64InvocationViewOffset)
	}
	a.Sd(enc.Scratch1, enc.X25, -abi.RV64NativeContextOffset)
	a.Ld(enc.Scratch0, enc.SP, 56)
	a.Sd(enc.Scratch0, enc.X25, -40)
	a.Ld(enc.X10, enc.SP, 24)
	a.Ld(enc.X12, enc.SP, 32)
	a.Ld(enc.X13, enc.SP, 40)
	if callee.stackOwnedActivation {
		a.Constant64(enc.Scratch1, int64(nativeImportFrameBytes+16+HostFixedFrameBytes+transportBytes))
		a.Add(enc.Scratch1, enc.SP, enc.Scratch1)
	} else {
		a.Constant64(enc.Scratch1, int64(resultBase))
	}
	for i := 0; i < callee.resultSlots; i++ {
		source, offset := f.addressAt(enc.Scratch1, int32(i*8), enc.X29)
		a.Ld(enc.Scratch0, source, offset)
		destination, offset := f.addressAt(enc.X13, int32(i*8), enc.X29)
		a.Sd(enc.Scratch0, destination, offset)
	}
	a.Ld(enc.X24, enc.SP, 48)
	a.Addi(enc.X11, enc.X25, 0)
	a.Ld(enc.RA, enc.SP, 0)
	a.Constant64(enc.Scratch0, int64(frameBytes))
	a.Add(enc.SP, enc.SP, enc.Scratch0)
	a.Jalr(enc.Zero, enc.RA, 0)
	for _, site := range append([]int{wrongHome, wrongTransport, missingAnchor, wrongContext}, invocationGuards...) {
		if err := a.PatchLong(site, int64(a.Len())); err != nil {
			return nil, err
		}
	}
	// A wrapper has an import ordinal but no Wasm instruction PC. Public source
	// attribution and cold-unwind rebinding are enforced by public admission.
	f.emitProductionTrap(abi.TrapCalledFnNotLinked, ^uint32(0))
	for _, site := range []int{below, short} {
		if err := a.PatchLong(site, int64(a.Len())); err != nil {
			return nil, err
		}
	}
	f.emitProductionTrap(abi.TrapStackFenceBreached, ^uint32(0))
	if err := a.Err(); err != nil {
		return nil, err
	}
	return a.B, nil
}
