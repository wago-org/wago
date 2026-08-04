// Package nativeabi defines target-neutral metadata shared by native code
// generators and runtime subsystems. It must remain free of compiler/runtime
// implementation dependencies so both sides can validate the same contract.
package nativeabi

import "fmt"

// RootKind identifies the owner responsible for a native frame root.
type RootKind uint8

const (
	// RootGCRef is a compact gc.Ref value scanned and, if required by a future
	// moving representation, rewritten by the WasmGC collector.
	RootGCRef RootKind = iota + 1
	// RootFuncRef is a canonical funcref descriptor identity. It is not a gc.Ref;
	// the instance/reference lifecycle must retain its producer while the slot is
	// live, and the collector must not reinterpret its bits as a heap handle.
	RootFuncRef
)

// RootSlot describes one 8-byte native frame slot. Offset is relative to the
// function's post-prologue RSP and remains valid until that frame is discarded.
type RootSlot struct {
	Offset uint32
	Kind   RootKind
}

// FunctionRootMap describes the conservative fixed root area of one local
// function. FrameBytes is the minimum frame prefix containing every listed slot;
// the final aligned native frame may be larger because of ordinary spills.
type FunctionRootMap struct {
	LocalFunction uint32
	FrameBytes    uint32
	Slots         []RootSlot
}

// ValidateRootMaps checks the representation before a collector or lifecycle
// scanner trusts native offsets. Maps and slots must be strictly ordered so
// malformed or duplicate metadata cannot create ambiguous mutable roots.
func ValidateRootMaps(maps []FunctionRootMap, localFunctions int) error {
	var previousFunction uint32
	for i, rootMap := range maps {
		if int(rootMap.LocalFunction) >= localFunctions {
			return fmt.Errorf("native root map %d function %d is out of range", i, rootMap.LocalFunction)
		}
		if i != 0 && rootMap.LocalFunction <= previousFunction {
			return fmt.Errorf("native root maps are not strictly ordered at function %d", rootMap.LocalFunction)
		}
		previousFunction = rootMap.LocalFunction
		var previousOffset uint32
		for j, slot := range rootMap.Slots {
			if slot.Kind != RootGCRef && slot.Kind != RootFuncRef {
				return fmt.Errorf("native root map %d slot %d has invalid kind %d", i, j, slot.Kind)
			}
			if slot.Offset&7 != 0 {
				return fmt.Errorf("native root map %d slot %d offset %d is not 8-byte aligned", i, j, slot.Offset)
			}
			if slot.Offset > rootMap.FrameBytes || rootMap.FrameBytes-slot.Offset < 8 {
				return fmt.Errorf("native root map %d slot %d offset %d exceeds frame prefix %d", i, j, slot.Offset, rootMap.FrameBytes)
			}
			if j != 0 && slot.Offset <= previousOffset {
				return fmt.Errorf("native root map %d slots are not strictly ordered at offset %d", i, slot.Offset)
			}
			previousOffset = slot.Offset
		}
	}
	return nil
}
