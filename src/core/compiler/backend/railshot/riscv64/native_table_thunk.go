//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

const nativeTableFrameBytes = nativeImportFrameBytes + 16 + HostFixedFrameBytes + 2*nativeImportTransportBytes

// Keep the private call helper image-relative and bounded. The return address
// reaches the skip envelope; continuation then reconstructs caller values from
// their canonical homes. All source/descriptor lifetimes belong to the owner.
func (f *integerEmitter) nativeTableCall(key uint64, argumentSlots, resultSlots int) {
	if !f.mode.production {
		f.err = unsupported("foreign table call requires production framed ABI")
		return
	}
	code, err := nativeTableThunkAt(key, argumentSlots, resultSlots, f.mode.functionIndex, f.sourcePC)
	if err != nil {
		f.err = err
		return
	}
	if len(code) > MaxCodeBytes-f.a.Len()-16 {
		f.err = unsupported("foreign table bridge image bound")
		return
	}
	call := f.a.JumpLong(true)
	skip := f.a.JumpLong(false)
	start := f.a.Len()
	f.a.B = append(f.a.B, code...)
	if err := f.a.PatchLong(call, int64(start)); err != nil {
		f.err = err
		return
	}
	if err := f.a.PatchLong(skip, int64(f.a.Len())); err != nil {
		f.err = err
	}
}

// Private groundwork only: public imported-table admission remains closed.
// X14 carries an immutable, physically retained canonical40-byte descriptor;
// its target must be a qualified framed RV64 local wrapper. The activation owner
// serializes both homes and retains all descriptor/context/code storage through
// normal return or cold unwind. Root call buffers must fit the exact signature.
// No absolute runtime address is embedded in this image. The owner publishes it
// through the normal W^X/process-wide icache mapper, and rebinds after cold exit.
// Include the call/skip envelope and descriptor row guards in admission. The
// transport copies cost eight bytes per cell; leave ample fixed-width slack for
// context copies, identity checks, arbitrary constants and two trap islands.
func nativeTableCallCodeBound(argumentSlots, resultSlots int) int {
	return 4096 + 16*(argumentSlots+resultSlots)
}

func nativeTableThunk(key uint64, argumentSlots, resultSlots int, functionIndex uint32) ([]byte, error) {
	return nativeTableThunkAt(key, argumentSlots, resultSlots, functionIndex, ^uint32(0))
}

func nativeTableThunkAt(key uint64, argumentSlots, resultSlots int, functionIndex, sourcePC uint32) ([]byte, error) {
	if argumentSlots < 0 || argumentSlots > 256 || resultSlots < 0 || resultSlots > 256 || functionIndex >= MaxFunctions {
		return nil, fmt.Errorf("rv64: native table bridge requires bounded signature and source index")
	}
	f := target{mode: emissionMode{production: true, functionIndex: functionIndex}}
	a := &f.a
	var invalid []int
	guard := func(condition enc.Cond, left, right enc.Reg) {
		invalid = append(invalid, a.BranchLong(condition, left, right))
	}
	aligned := func(reg enc.Reg, mask int64) {
		a.Constant64(enc.X5, mask)
		a.And(enc.X5, reg, enc.X5)
		guard(enc.NE, enc.X5, enc.Zero)
	}
	// All identity, descriptor, transport and stack guards precede any write.
	guard(enc.NE, enc.X11, enc.X25)
	a.Ld(enc.X6, enc.X25, -abi.FuncRefDescPtrOffset)
	guard(enc.EQ, enc.X6, enc.Zero)
	aligned(enc.X6, 7)
	a.Ld(enc.X29, enc.X6, coreruntime.FuncRefContextOffset)
	guard(enc.EQ, enc.X29, enc.Zero)
	aligned(enc.X29, 7)
	a.Ld(enc.X5, enc.X25, -abi.RV64NativeContextOffset)
	guard(enc.NE, enc.X29, enc.X5)
	guard(enc.EQ, enc.X14, enc.Zero)
	aligned(enc.X14, 7)
	a.Ld(enc.X28, enc.X14, coreruntime.TableEntryCodePtrOffset)
	guard(enc.EQ, enc.X28, enc.Zero)
	aligned(enc.X28, 15)
	a.Ld(enc.X6, enc.X14, coreruntime.TableEntrySigKeyOffset)
	a.Constant64(enc.X5, int64(key))
	guard(enc.NE, enc.X6, enc.X5)
	a.Ld(enc.X6, enc.X14, coreruntime.TableEntryHomeLinMemOffset)
	homeTagMask := uint64(abi.FuncRefHomeTagMask)
	a.Constant64(enc.X5, int64(homeTagMask))
	a.And(enc.X5, enc.X6, enc.X5)
	a.Constant64(enc.X7, int64(abi.FuncRefLocalWrapperHomeTag))
	guard(enc.NE, enc.X5, enc.X7)
	a.Constant64(enc.X5, int64(^abi.FuncRefHomeTagMask))
	a.And(enc.X6, enc.X6, enc.X5)
	guard(enc.EQ, enc.X6, enc.Zero)
	aligned(enc.X6, 7)
	a.Ld(enc.X7, enc.X14, coreruntime.TableEntryRefSlotOffset)
	guard(enc.NE, enc.X7, enc.X14)
	a.Ld(enc.X7, enc.X14, coreruntime.FuncRefContextOffset)
	guard(enc.EQ, enc.X7, enc.Zero)
	aligned(enc.X7, 7)
	a.Ld(enc.X5, enc.X7, 16) // target's immutable canonical descriptor anchor
	guard(enc.EQ, enc.X5, enc.Zero)
	// Preserve X5 while checking its alignment.
	a.Constant64(enc.Scratch0, 7)
	a.And(enc.Scratch0, enc.X5, enc.Scratch0)
	guard(enc.NE, enc.Scratch0, enc.Zero)
	a.Ld(enc.X5, enc.X5, coreruntime.FuncRefContextOffset)
	guard(enc.NE, enc.X5, enc.X7)
	if argumentSlots != 0 {
		guard(enc.EQ, enc.X10, enc.Zero)
		aligned(enc.X10, 7)
	}
	if resultSlots != 0 {
		guard(enc.EQ, enc.X13, enc.Zero)
		aligned(enc.X13, 7)
	}
	a.Ld(enc.X15, enc.X25, -40) // active control, never mutable producer word0
	guard(enc.EQ, enc.X15, enc.Zero)
	aligned(enc.X15, 7)
	a.Ld(enc.X15, enc.X15, HostTrampolineOffset)
	guard(enc.EQ, enc.X15, enc.Zero)
	aligned(enc.X15, 3)
	aligned(enc.SP, 15)
	a.Ld(enc.X5, enc.X25, -abi.StackFenceOffset)
	guard(enc.EQ, enc.X5, enc.Zero)
	below := a.BranchLong(enc.LTU, enc.SP, enc.X5)
	a.Sub(enc.X5, enc.SP, enc.X5)
	a.Constant64(enc.Scratch0, nativeTableFrameBytes)
	short := a.BranchLong(enc.LTU, enc.X5, enc.Scratch0)
	a.Sub(enc.SP, enc.SP, enc.Scratch0)
	a.Sd(enc.RA, enc.SP, 0)
	a.Sd(enc.X25, enc.SP, 8)
	a.Sd(enc.X29, enc.SP, 16)
	a.Sd(enc.X10, enc.SP, 24)
	a.Sd(enc.X12, enc.SP, 32)
	a.Sd(enc.X13, enc.SP, 40)
	a.Sd(enc.X24, enc.SP, 48)
	a.Ld(enc.Scratch0, enc.X25, -40)
	a.Sd(enc.Scratch0, enc.SP, 56)
	a.Sd(enc.X7, enc.SP, nativeImportFrameBytes)
	a.Sd(enc.X14, enc.SP, nativeImportFrameBytes+8)
	a.Constant64(enc.Scratch1, nativeImportFrameBytes+16+HostFixedFrameBytes)
	a.Add(enc.Scratch1, enc.SP, enc.Scratch1)
	for i := 0; i < argumentSlots; i++ {
		a.Ld(enc.Scratch0, enc.X10, int32(i*8))
		a.Sd(enc.Scratch0, enc.Scratch1, int32(i*8))
	}
	a.Addi(enc.X10, enc.Scratch1, 0)
	a.Constant64(enc.X13, nativeImportFrameBytes+16+HostFixedFrameBytes+nativeImportTransportBytes)
	a.Add(enc.X13, enc.SP, enc.X13)
	a.Addi(enc.X11, enc.X6, 0)
	a.Addi(enc.Scratch1, enc.X7, 0)
	copyContext := func(destination enc.Reg) {
		for _, field := range nativeImportContextFields {
			if field.source == 0 {
				continue
			}
			a.Ld(enc.Scratch0, enc.Scratch1, field.source)
			a.Sd(enc.Scratch0, destination, -field.destination)
		}
	}
	copyContext(enc.X11)
	a.Addi(enc.Scratch0, enc.SP, nativeImportFrameBytes+16)
	a.Sd(enc.X15, enc.Scratch0, HostTrampolineOffset)
	a.Sd(enc.Scratch0, enc.X11, -40)
	a.Sd(enc.Scratch1, enc.X11, -abi.RV64NativeContextOffset)
	for _, offset := range nativeImportExecutionFields {
		a.Ld(enc.Scratch0, enc.X25, -offset)
		a.Sd(enc.Scratch0, enc.X11, -offset)
	}
	a.Addi(enc.X25, enc.X11, 0)
	a.Jalr(enc.RA, enc.X28, 0)
	a.Ld(enc.X25, enc.SP, 8)
	// The canonical descriptor survives nested calls, host parks and root reentry.
	a.Ld(enc.Scratch1, enc.SP, nativeImportFrameBytes+8)
	a.Ld(enc.Scratch1, enc.Scratch1, coreruntime.TableEntryHomeLinMemOffset)
	a.Constant64(enc.Scratch0, int64(^abi.FuncRefHomeTagMask))
	a.And(enc.Scratch1, enc.Scratch1, enc.Scratch0)
	for _, offset := range nativeImportExecutionFields {
		a.Ld(enc.Scratch0, enc.Scratch1, -offset)
		a.Sd(enc.Scratch0, enc.X25, -offset)
	}
	a.Ld(enc.Scratch1, enc.SP, 16)
	copyContext(enc.X25)
	a.Sd(enc.Scratch1, enc.X25, -abi.RV64NativeContextOffset)
	a.Ld(enc.Scratch0, enc.SP, 56)
	a.Sd(enc.Scratch0, enc.X25, -40)
	a.Ld(enc.X10, enc.SP, 24)
	a.Ld(enc.X12, enc.SP, 32)
	a.Ld(enc.X13, enc.SP, 40)
	a.Constant64(enc.Scratch1, nativeImportFrameBytes+16+HostFixedFrameBytes+nativeImportTransportBytes)
	a.Add(enc.Scratch1, enc.SP, enc.Scratch1)
	for i := 0; i < resultSlots; i++ {
		a.Ld(enc.Scratch0, enc.Scratch1, int32(i*8))
		a.Sd(enc.Scratch0, enc.X13, int32(i*8))
	}
	a.Ld(enc.X24, enc.SP, 48)
	a.Addi(enc.X11, enc.X25, 0)
	a.Ld(enc.RA, enc.SP, 0)
	a.Constant64(enc.Scratch0, nativeTableFrameBytes)
	a.Add(enc.SP, enc.SP, enc.Scratch0)
	a.Jalr(enc.Zero, enc.RA, 0)
	for _, site := range invalid {
		if err := a.PatchLong(site, int64(a.Len())); err != nil {
			return nil, err
		}
	}
	f.emitProductionTrap(abi.TrapCalledFnNotLinked, sourcePC)
	for _, site := range []int{below, short} {
		if err := a.PatchLong(site, int64(a.Len())); err != nil {
			return nil, err
		}
	}
	f.emitProductionTrap(abi.TrapStackFenceBreached, sourcePC)
	if err := a.Err(); err != nil {
		return nil, err
	}
	return a.B, nil
}
