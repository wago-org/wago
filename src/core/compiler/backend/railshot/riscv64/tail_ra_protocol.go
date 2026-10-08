//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Private proof only. No source opcode, production option, public ABI or codec
// uses these constants. A caller context is explicitly128 bytes, not the public
// runtime's120-byte context. Instance-owned dispatcher addresses are never part
// of a persisted guest image.
const (
	tailRAContextBytes          = 128
	tailRACapOffset             = 120
	tailRACapMagic              = uint64(0x5741474f54524331)
	tailRARecordMagic           = uint64(0x5741474f54525231)
	tailRAVersion               = 1
	tailRACapHeader             = 32
	tailRASiteRow               = 16
	tailRAMaxSites              = 4096
	tailRAMaxRanges             = 1024
	tailRARecordBytes           = 5504
	tailRAInvocationRecordBytes = tailRARecordBytes + abi.RV64InvocationViewBytes
	tailRACtrlOffset            = 160
	tailRAArgsOffset            = 1408
	tailRAResultsOffset         = 3456
	tailRADispatchBound         = 8192
)

type tailRARange struct{ base, bytes uintptr }

func (r tailRARange) valid(alignment uintptr) bool {
	return r.base != 0 && r.base&(alignment-1) == 0 && r.bytes != 0 && r.bytes <= uintptr(MaxCodeBytes) && r.base <= ^uintptr(0)-r.bytes
}

// The owner freezes these checked ranges under one invocation lease. They are
// not an arbitrary public native-pointer lookup and the stack record owns none
// of their cleanup obligations.
type tailRAProofSite struct {
	invocationContext                                bool
	frameBytes                                       int
	functionIndex, sourcePC                          uint32
	callerHome, callerContext                        uintptr
	target                                           nativeImportTarget
	params, results                                  []wasm.ValType
	sourceBase, sourceStride                         int
	argumentRanges, resultRanges, continuationRanges []tailRARange
	stackLimit, stackTop                             uintptr
}

func tailRAResultMask(types []wasm.ValType) ([4]uint64, int, error) {
	var mask [4]uint64
	sig := &wasm.CompType{Kind: wasm.CompFunc, Results: types}
	if err := tailRASignature(sig); err != nil {
		return mask, 0, err
	}
	cell := 0
	for _, typ := range types {
		if wasm.EqualValType(typ, wasm.I32) || wasm.EqualValType(typ, wasm.F32) {
			mask[cell/64] |= uint64(1) << uint(cell%64)
		}
		cell++
		if wasm.EqualValType(typ, wasm.V128) {
			cell++
		}
	}
	return mask, cell, nil
}

func tailRASignature(sig *wasm.CompType) error {
	if sig == nil || sig.Kind != wasm.CompFunc || len(sig.Params) > 256 || len(sig.Results) > 256 {
		return unsupported("private tail exact signature")
	}
	vector := false
	for _, list := range [][]wasm.ValType{sig.Params, sig.Results} {
		for _, typ := range list {
			if swarClassOf(typ) == swarInvalid {
				return unsupported("private tail numeric/vector carriers")
			}
			if wasm.EqualValType(typ, wasm.V128) {
				vector = true
			}
		}
	}
	if vector {
		return swarCallSignature(sig)
	}
	return nil
}

func (s *tailRAProofSite) validate() error {
	if s == nil || s.frameBytes < callFrameHeader || s.frameBytes > MaxFrameBytes || s.frameBytes&15 != 0 || s.callerHome == 0 || s.callerHome&7 != 0 || s.callerContext == 0 || s.callerContext&7 != 0 || (!s.invocationContext && (s.stackLimit == 0 || s.stackLimit&15 != 0 || s.stackTop&15 != 0 || s.stackTop <= s.stackLimit || s.stackTop-s.stackLimit < tailRARecordBytes+16)) {
		return unsupported("private tail site framed identities/stack")
	}
	if err := tailRASignature(&wasm.CompType{Kind: wasm.CompFunc, Params: s.params, Results: s.results}); err != nil {
		return err
	}
	if s.target.entry == 0 || s.target.entry&15 != 0 || s.target.home == 0 || s.target.home&7 != 0 || s.target.context == 0 || s.target.context&7 != 0 || s.target.hostTrampoline == 0 || s.target.hostTrampoline&3 != 0 {
		return unsupported("private tail target identities")
	}
	if s.sourceStride != 8 && s.sourceStride != 16 {
		return unsupported("private tail source stride")
	}
	end := s.sourceBase
	for _, typ := range s.params {
		width := 8
		if wasm.EqualValType(typ, wasm.V128) {
			width = 16
			if s.sourceStride != 16 {
				return unsupported("private tail scalar pair home")
			}
		}
		if end < callFrameHeader || end > s.frameBytes-width {
			return unsupported("private tail source homes")
		}
		end += s.sourceStride
	}
	if len(s.argumentRanges) > tailRAMaxRanges || len(s.resultRanges) > tailRAMaxRanges || len(s.continuationRanges) == 0 || len(s.continuationRanges) > tailRAMaxRanges {
		return unsupported("private tail range count")
	}
	for _, ranges := range [][]tailRARange{s.argumentRanges, s.resultRanges} {
		for _, r := range ranges {
			if !r.valid(8) {
				return unsupported("private tail retained range")
			}
		}
	}
	for _, r := range s.continuationRanges {
		if !r.valid(4) {
			return unsupported("private tail retained continuation")
		}
	}
	if !s.invocationContext && (swarSignatureSlots(s.params) != 0 && len(s.argumentRanges) == 0 || swarSignatureSlots(s.results) != 0 && len(s.resultRanges) == 0) {
		return unsupported("private tail missing original capacity")
	}
	return nil
}

// All veneers use X31. Persistent cursors/results use other fixed temporaries.
type tailRAEmitter struct {
	invocationContext bool
	a                 enc.Asm
	bad, stack        []int
	functionIndex, pc uint32
}

func (e *tailRAEmitter) fail(c enc.Cond, l, r enc.Reg) {
	e.bad = append(e.bad, e.a.BranchLong(c, l, r))
}
func (e *tailRAEmitter) fence(c enc.Cond, l, r enc.Reg) {
	e.stack = append(e.stack, e.a.BranchLong(c, l, r))
}
func (e *tailRAEmitter) patch(site int) {
	if err := e.a.PatchLong(site, int64(e.a.Len())); err != nil {
		panic(err)
	}
}
func (e *tailRAEmitter) finish() ([]byte, error) {
	for _, sites := range []struct {
		sites []int
		code  int
	}{{e.bad, abi.TrapCalledFnNotLinked}, {e.stack, abi.TrapStackFenceBreached}} {
		for _, site := range sites.sites {
			if err := e.a.PatchLong(site, int64(e.a.Len())); err != nil {
				return nil, err
			}
		}
		f := target{a: e.a, mode: emissionMode{production: true, functionIndex: e.functionIndex}}
		f.emitProductionTrap(sites.code, e.pc)
		e.a = f.a
	}
	if err := e.a.Err(); err != nil {
		return nil, err
	}
	return e.a.B, nil
}

func tailRAAddress(a *enc.Asm, d, b enc.Reg, offset int) {
	a.Constant64(d, int64(offset))
	a.Add(d, b, d)
}
func tailRAMask(a *enc.Asm, d, s enc.Reg, mask int32) {
	a.Addi(enc.Scratch0, enc.Zero, mask)
	a.And(d, s, enc.Scratch0)
}

// Containment skips a zero-cell direction without dereferencing even a nil
// pointer. Ranges are immutable factory data, emitted in this private proof.
func (e *tailRAEmitter) contains(pointer enc.Reg, bytes int, ranges []tailRARange) {
	if bytes == 0 {
		return
	}
	a := &e.a
	alignment := int32(7)
	if bytes == 4 {
		alignment = 3
	}
	tailRAMask(a, enc.X29, pointer, alignment)
	e.fail(enc.NE, enc.X29, enc.Zero)
	var success []int
	for _, r := range ranges {
		if uintptr(bytes) > r.bytes {
			continue
		}
		a.Constant64(enc.X29, int64(r.base))
		below := a.BranchLong(enc.LTU, pointer, enc.X29)
		a.Sub(enc.X29, pointer, enc.X29)
		a.Constant64(enc.Scratch0, int64(r.bytes-uintptr(bytes)))
		success = append(success, a.BranchLong(enc.GEU, enc.Scratch0, enc.X29))
		e.patch(below)
	}
	e.fail(enc.EQ, enc.Zero, enc.Zero)
	for _, site := range success {
		e.patch(site)
	}
}

// [pointer,pointer+bytes) must not overlap [base,base+size). Containment has
// already proved pointer arithmetic cannot wrap.
func (e *tailRAEmitter) disjoint(pointer enc.Reg, bytes int, base enc.Reg, size int) {
	if bytes == 0 {
		return
	}
	a := &e.a
	tailRAAddress(a, enc.X29, base, size)
	above := a.BranchLong(enc.GEU, pointer, enc.X29)
	tailRAAddress(a, enc.X29, pointer, bytes)
	e.fail(enc.LTU, base, enc.X29)
	e.patch(above)
}

// Check all native extents before any record dereference. The original owner
// and arena pointers later borrowed by exit were frozen/validated by preflight
// under the same lease, not inferred from this tag.
func (e *tailRAEmitter) record(record, exit enc.Reg, s *tailRAProofSite) {
	a := &e.a
	tailRAMask(a, enc.X29, record, 15)
	e.fail(enc.NE, enc.X29, enc.Zero)
	a.Ld(enc.X29, enc.X25, -abi.StackFenceOffset)
	e.fence(enc.LTU, record, enc.X29)
	a.Ld(enc.X29, enc.X25, -abi.RV64TrapReentrySPOffset)
	e.fence(enc.LTU, enc.X29, record)
	a.Sub(enc.X29, enc.X29, record)
	a.Constant64(enc.Scratch0, int64(e.recordBytes()))
	e.fence(enc.LTU, enc.X29, enc.Scratch0)
	a.Ld(enc.X29, record, 80)
	a.Constant64(enc.Scratch0, int64(tailRARecordMagic))
	e.fail(enc.NE, enc.X29, enc.Scratch0)
	a.Ld(enc.X29, record, 104)
	a.Constant64(enc.Scratch0, int64(e.recordBytes()))
	e.fail(enc.NE, enc.X29, enc.Scratch0)
	tailRAAddress(a, enc.Scratch0, record, e.recordBytes())
	a.Ld(enc.X29, record, 64)
	e.fail(enc.NE, enc.X29, enc.Scratch0)
	a.Ld(enc.X29, record, 72)
	e.fail(enc.NE, enc.X29, exit)
	a.Ld(enc.X29, record, 96)
	e.fail(enc.NE, enc.X29, enc.X25)
	a.Ld(enc.X29, enc.X25, -abi.RV64NativeContextOffset)
	a.Ld(enc.Scratch0, record, 144)
	e.fail(enc.NE, enc.X29, enc.Scratch0)
	if e.invocationContext {
		tailRAAddress(a, enc.X28, record, tailRARecordBytes)
		a.Ld(enc.X29, enc.X25, -abi.RV64InvocationViewOffset)
		e.fail(enc.NE, enc.X28, enc.X29)
		e.bad = append(e.bad, nativeInvocationViewGuards(a, enc.X28, 0)...)
		a.Ld(enc.X29, enc.X28, abi.RV64InvocationFirstRAOffset)
		e.fail(enc.NE, enc.X29, exit)
		for _, direction := range []struct {
			pointer, capacity int32
			offset            int
		}{{abi.RV64InvocationArgumentsOffset, abi.RV64InvocationArgumentBytesOffset, tailRAArgsOffset}, {abi.RV64InvocationResultsOffset, abi.RV64InvocationResultBytesOffset, tailRAResultsOffset}} {
			tailRAAddress(a, enc.X29, record, direction.offset)
			a.Ld(enc.Scratch0, enc.X28, direction.pointer)
			e.fail(enc.NE, enc.X29, enc.Scratch0)
			a.Ld(enc.X29, enc.X28, direction.capacity)
			a.Constant64(enc.Scratch0, nativeImportTransportBytes)
			e.fail(enc.NE, enc.X29, enc.Scratch0)
		}
	}
	a.Ld(enc.X5, record, 88)
	a.Constant64(enc.Scratch0, 256)
	e.fail(enc.LTU, enc.Scratch0, enc.X5)
	if s != nil {
		mask, count, _ := tailRAResultMask(s.results)
		a.Constant64(enc.Scratch0, int64(count))
		e.fail(enc.NE, enc.X5, enc.Scratch0)
		for i, m := range mask {
			a.Ld(enc.X29, record, int32(112+i*8))
			a.Constant64(enc.Scratch0, int64(m))
			e.fail(enc.NE, enc.X29, enc.Scratch0)
		}
	} else {
		// Unused bits in all four immutable masks must be zero.
		for i := 0; i < 4; i++ {
			a.Constant64(enc.Scratch0, int64((i+1)*64))
			full := a.BranchLong(enc.GEU, enc.X5, enc.Scratch0)
			a.Ld(enc.X29, record, int32(112+i*8))
			a.Constant64(enc.Scratch0, int64(i*64))
			empty := a.BranchLong(enc.GEU, enc.Scratch0, enc.X5)
			a.Sub(enc.Scratch0, enc.X5, enc.Scratch0)
			a.Srl(enc.X29, enc.X29, enc.Scratch0)
			e.fail(enc.NE, enc.X29, enc.Zero)
			done := a.JumpLong(false)
			e.patch(empty)
			e.fail(enc.NE, enc.X29, enc.Zero)
			e.patch(done)
			e.patch(full)
		}
	}
}

func tailRAPreflight(s tailRAProofSite, exit, finalize uintptr) ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if exit == 0 || exit&15 != 0 || finalize == 0 || finalize&15 != 0 {
		return nil, unsupported("private tail dispatcher addresses")
	}
	e := tailRAEmitter{functionIndex: s.functionIndex, pc: s.sourcePC, invocationContext: s.invocationContext}
	a := &e.a
	// Validate current frame before reading saved RA/transport. Caller and target
	// authentication happens before any exchange, stack or basedata write.
	a.Constant64(enc.X29, int64(s.callerHome))
	e.fail(enc.NE, enc.X25, enc.X29)
	e.fail(enc.NE, enc.X11, enc.X25)
	a.Ld(enc.X29, enc.X25, -abi.RV64NativeContextOffset)
	a.Constant64(enc.Scratch0, int64(s.callerContext))
	e.fail(enc.NE, enc.X29, enc.Scratch0)
	a.Ld(enc.X29, enc.X25, -abi.FuncRefDescPtrOffset)
	e.fail(enc.EQ, enc.X29, enc.Zero)
	a.Ld(enc.X29, enc.X29, coreruntime.FuncRefContextOffset)
	e.fail(enc.NE, enc.X29, enc.Scratch0)
	// Frozen target pointers are retained by the owner; check the canonical
	// target anchor too before committing exchange or activation state.
	a.Constant64(enc.Scratch0, int64(s.target.context))
	a.Ld(enc.X29, enc.Scratch0, 16)
	e.fail(enc.EQ, enc.X29, enc.Zero)
	a.Ld(enc.X29, enc.X29, coreruntime.FuncRefContextOffset)
	e.fail(enc.NE, enc.X29, enc.Scratch0)
	a.Ld(enc.X29, enc.X25, -abi.StackFenceOffset)
	if !s.invocationContext {
		a.Constant64(enc.Scratch0, int64(s.stackLimit))
		e.fail(enc.NE, enc.X29, enc.Scratch0)
	}
	e.fence(enc.LTU, enc.SP, enc.X29)
	tailRAMask(a, enc.X29, enc.SP, 15)
	e.fail(enc.NE, enc.X29, enc.Zero)
	tailRAAddress(a, enc.X14, enc.SP, s.frameBytes)
	e.fence(enc.LTU, enc.X14, enc.SP)
	a.Ld(enc.X29, enc.X25, -abi.RV64TrapReentrySPOffset)
	if !s.invocationContext {
		a.Constant64(enc.Scratch0, int64(s.stackTop))
		e.fence(enc.LTU, enc.Scratch0, enc.X29)
	}
	e.fence(enc.LTU, enc.X29, enc.X14)
	if s.invocationContext {
		a.Ld(enc.X28, enc.X25, -abi.RV64InvocationViewOffset)
		e.bad = append(e.bad, nativeInvocationViewGuards(a, enc.X28, s.frameBytes)...)
	}
	a.Ld(enc.X9, enc.SP, 0)
	a.Constant64(enc.X15, int64(exit))
	reuse := a.BranchLong(enc.EQ, enc.X9, enc.X15)
	// First hop: validate original return and actual arena capacities. These
	// ranges include the leased root/bridge continuation, not just target code.
	if s.invocationContext {
		a.Ld(enc.X29, enc.X28, abi.RV64InvocationFirstRAOffset)
		e.fail(enc.EQ, enc.X29, enc.Zero)
		first := a.BranchLong(enc.EQ, enc.X9, enc.X29)
		e.contains(enc.X9, 4, s.continuationRanges)
		e.patch(first)
	} else {
		e.contains(enc.X9, 4, s.continuationRanges)
	}
	a.Ld(enc.X6, enc.SP, 16)
	a.Ld(enc.X7, enc.SP, 8)
	argBytes, resBytes := 8*swarSignatureSlots(s.params), 8*swarSignatureSlots(s.results)
	if s.invocationContext {
		e.invocationTransport(enc.X6, argBytes, enc.X28, abi.RV64InvocationArgumentsOffset, abi.RV64InvocationArgumentBytesOffset)
		e.invocationTransport(enc.X7, resBytes, enc.X28, abi.RV64InvocationResultsOffset, abi.RV64InvocationResultBytesOffset)
	} else {
		e.contains(enc.X6, argBytes, s.argumentRanges)
		e.contains(enc.X7, resBytes, s.resultRanges)
	}
	a.Ld(enc.X29, enc.X25, -abi.StackFenceOffset)
	a.Sub(enc.X29, enc.X14, enc.X29)
	a.Constant64(enc.Scratch0, int64(e.recordBytes()))
	e.fence(enc.LTU, enc.X29, enc.Scratch0)
	a.Sub(enc.X15, enc.X14, enc.Scratch0)
	e.disjoint(enc.X6, argBytes, enc.X15, e.recordBytes())
	e.disjoint(enc.X7, resBytes, enc.X15, e.recordBytes())
	e.disjoint(enc.X6, argBytes, enc.SP, s.frameBytes)
	e.disjoint(enc.X7, resBytes, enc.SP, s.frameBytes)
	if argBytes != 0 && resBytes != 0 {
		e.disjoint(enc.X6, argBytes, enc.X7, resBytes)
	}
	done := a.JumpLong(false)
	e.patch(reuse)
	e.record(enc.X14, enc.X15, &s)
	// The full record arenas lie above the caller homes. Confirm header transport
	// is exactly those areas, not a partially overlapping caller scratch span.
	tailRAAddress(a, enc.X29, enc.X14, tailRAArgsOffset)
	a.Ld(enc.Scratch0, enc.SP, 16)
	e.fail(enc.NE, enc.X29, enc.Scratch0)
	tailRAAddress(a, enc.X29, enc.X14, tailRAResultsOffset)
	a.Ld(enc.Scratch0, enc.SP, 8)
	e.fail(enc.NE, enc.X29, enc.Scratch0)
	e.patch(done)
	a.Constant64(enc.X28, int64(finalize))
	a.Ret()
	return e.finish()
}

func tailRAFinalize(s tailRAProofSite, exit uintptr) ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if exit == 0 || exit&15 != 0 {
		return nil, unsupported("private tail exit address")
	}
	e := tailRAEmitter{functionIndex: s.functionIndex, pc: s.sourcePC, invocationContext: s.invocationContext}
	a := &e.a
	// No park/callback is permitted between preflight and this frozen finalizer.
	// Reload original header0, never the linked preflight's live RA.
	a.Ld(enc.X9, enc.SP, 0)
	a.Ld(enc.X10, enc.SP, 16)
	a.Ld(enc.X12, enc.SP, 24)
	a.Ld(enc.X13, enc.SP, 8)
	tailRAAddress(a, enc.X14, enc.SP, s.frameBytes)
	a.Constant64(enc.Scratch0, int64(exit))
	reuse := a.BranchLong(enc.EQ, enc.X9, enc.Scratch0)
	a.Ld(enc.X28, enc.X25, -40)
	if s.invocationContext {
		a.Ld(enc.X15, enc.X25, -abi.RV64InvocationViewOffset)
	}
	a.Constant64(enc.Scratch1, int64(e.recordBytes()))
	a.Sub(enc.SP, enc.X14, enc.Scratch1)
	a.Sd(enc.X9, enc.SP, 0)
	a.Sd(enc.X25, enc.SP, 8)
	a.Constant64(enc.Scratch1, int64(s.callerContext))
	a.Sd(enc.Scratch1, enc.SP, 16)
	a.Sd(enc.X10, enc.SP, 24)
	a.Sd(enc.X12, enc.SP, 32)
	a.Sd(enc.X13, enc.SP, 40)
	a.Sd(enc.X24, enc.SP, 48)
	a.Sd(enc.X28, enc.SP, 56)
	a.Sd(enc.X14, enc.SP, 64)
	a.Sd(enc.Scratch0, enc.SP, 72)
	a.Constant64(enc.Scratch0, int64(tailRARecordMagic))
	a.Sd(enc.Scratch0, enc.SP, 80)
	mask, count, _ := tailRAResultMask(s.results)
	a.Constant64(enc.Scratch0, int64(count))
	a.Sd(enc.Scratch0, enc.SP, 88)
	a.Constant64(enc.Scratch0, int64(e.recordBytes()))
	a.Sd(enc.Scratch0, enc.SP, 104)
	if s.invocationContext {
		a.Sd(enc.X15, enc.SP, 152)
	}
	for i, m := range mask {
		a.Constant64(enc.Scratch0, int64(m))
		a.Sd(enc.Scratch0, enc.SP, int32(112+i*8))
	}
	// Bounded raw first-hop copy; staged source is outside the discarded frame
	// and the new record. Subsequent hops already stage into record.args.
	args := swarSignatureSlots(s.params)
	if args != 0 {
		a.Addi(enc.X6, enc.X10, 0)
		tailRAAddress(a, enc.X7, enc.SP, tailRAArgsOffset)
		a.Constant64(enc.X5, int64(args))
		loop := a.Len()
		a.Ld(enc.X29, enc.X6, 0)
		a.Sd(enc.X29, enc.X7, 0)
		a.Addi(enc.X6, enc.X6, 8)
		a.Addi(enc.X7, enc.X7, 8)
		a.Addi(enc.X5, enc.X5, -1)
		site := a.BranchLong(enc.NE, enc.X5, enc.Zero)
		if err := a.PatchLong(site, int64(loop)); err != nil {
			return nil, err
		}
	}
	bind := a.JumpLong(false)
	e.patch(reuse)
	a.Addi(enc.SP, enc.X14, 0)
	e.patch(bind)
	// Every active native hop initializes this nonparked record control. Its
	// prefix/control never aliases instance-global or producer-prototype state.
	tailRAAddress(a, enc.X6, enc.SP, tailRACtrlOffset)
	a.Constant64(enc.X5, HostFixedFrameBytes/8)
	loop := a.Len()
	a.Sd(enc.Zero, enc.X6, 0)
	a.Addi(enc.X6, enc.X6, 8)
	a.Addi(enc.X5, enc.X5, -1)
	site := a.BranchLong(enc.NE, enc.X5, enc.Zero)
	if err := a.PatchLong(site, int64(loop)); err != nil {
		return nil, err
	}
	a.Constant64(enc.Scratch0, int64(s.target.hostTrampoline))
	a.Sd(enc.Scratch0, enc.SP, tailRACtrlOffset+HostTrampolineOffset)
	a.Constant64(enc.X11, int64(s.target.home))
	a.Sd(enc.X11, enc.SP, 96)
	a.Constant64(enc.Scratch1, int64(s.target.context))
	a.Sd(enc.Scratch1, enc.SP, 144)
	if !s.invocationContext {
		a.Sd(enc.Zero, enc.SP, 152)
	}
	if s.invocationContext {
		// First hop retains its exact original view; reuse inherits this
		// record's own view so nested ordinary calls keep full transport.
		a.Ld(enc.X15, enc.X25, -abi.RV64InvocationViewOffset)
		nativeInvocationActivation(a, enc.X15, tailRARecordBytes, tailRAArgsOffset, tailRAResultsOffset)
		a.Sd(enc.X9, enc.X11, -abi.RV64InvocationViewOffset)
	}
	for _, field := range nativeImportContextFields {
		if field.source == 0 {
			continue
		}
		a.Ld(enc.Scratch0, enc.Scratch1, field.source)
		a.Sd(enc.Scratch0, enc.X11, -field.destination)
	}
	for _, off := range nativeImportExecutionFields {
		a.Ld(enc.Scratch0, enc.X25, -off)
		a.Sd(enc.Scratch0, enc.X11, -off)
	}
	a.Sd(enc.Scratch1, enc.X11, -abi.RV64NativeContextOffset)
	tailRAAddress(a, enc.Scratch0, enc.SP, tailRACtrlOffset)
	a.Sd(enc.Scratch0, enc.X11, -40)
	a.Addi(enc.X25, enc.X11, 0)
	tailRAAddress(a, enc.X10, enc.SP, tailRAArgsOffset)
	tailRAAddress(a, enc.X13, enc.SP, tailRAResultsOffset)
	a.Constant64(enc.RA, int64(exit))
	a.Constant64(enc.X28, int64(s.target.entry))
	a.Jalr(enc.Zero, enc.X28, 0)
	if err := a.Err(); err != nil {
		return nil, err
	}
	return a.B, nil
}

// The exit itself contains no absolute process or instance pointers. Its own
// first AUIPC supplies exact entry identity; publication owner retains its map.
func tailRAExitCode() ([]byte, error) { return tailRAExitCodeInvocation(false) }

func tailRAExitCodeInvocation(invocation bool) ([]byte, error) {
	e := tailRAEmitter{pc: ^uint32(0), invocationContext: invocation}
	a := &e.a
	a.Auipc(enc.X9, 0)
	a.Addi(enc.X14, enc.SP, 0)
	e.record(enc.X14, enc.X9, nil)
	a.Ld(enc.X5, enc.SP, 88)
	tailRAAddress(a, enc.X6, enc.SP, tailRAResultsOffset)
	a.Ld(enc.X7, enc.SP, 40)
	a.Addi(enc.X9, enc.SP, 112)
	a.Ld(enc.X14, enc.X9, 0)
	a.Addi(enc.X15, enc.Zero, 64)
	finished := a.BranchLong(enc.EQ, enc.X5, enc.Zero)
	loop := a.Len()
	tailRAMask(a, enc.X28, enc.X14, 1)
	wide := a.BranchLong(enc.EQ, enc.X28, enc.Zero)
	a.Lwu(enc.X29, enc.X6, 0)
	loaded := a.JumpLong(false)
	e.patch(wide)
	a.Ld(enc.X29, enc.X6, 0)
	e.patch(loaded)
	a.Sd(enc.X29, enc.X7, 0)
	a.Addi(enc.X6, enc.X6, 8)
	a.Addi(enc.X7, enc.X7, 8)
	a.Addi(enc.X5, enc.X5, -1)
	end := a.BranchLong(enc.EQ, enc.X5, enc.Zero)
	a.Srli(enc.X14, enc.X14, 1)
	a.Addi(enc.X15, enc.X15, -1)
	more := a.BranchLong(enc.NE, enc.X15, enc.Zero)
	a.Addi(enc.X9, enc.X9, 8)
	a.Ld(enc.X14, enc.X9, 0)
	a.Addi(enc.X15, enc.Zero, 64)
	e.patch(more)
	back := a.JumpLong(false)
	if err := a.PatchLong(back, int64(loop)); err != nil {
		return nil, err
	}
	e.patch(end)
	e.patch(finished)
	// Read current execution fields before rebinding a possibly shared home.
	a.Ld(enc.X11, enc.SP, 8)
	for _, off := range nativeImportExecutionFields {
		a.Ld(enc.Scratch0, enc.X25, -off)
		a.Sd(enc.Scratch0, enc.X11, -off)
	}
	a.Ld(enc.Scratch1, enc.SP, 16)
	for _, field := range nativeImportContextFields {
		if field.source == 0 {
			continue
		}
		a.Ld(enc.Scratch0, enc.Scratch1, field.source)
		a.Sd(enc.Scratch0, enc.X11, -field.destination)
	}
	a.Sd(enc.Scratch1, enc.X11, -abi.RV64NativeContextOffset)
	a.Ld(enc.Scratch0, enc.SP, 56)
	a.Sd(enc.Scratch0, enc.X11, -40)
	if invocation {
		a.Ld(enc.Scratch0, enc.SP, 152)
		a.Sd(enc.Scratch0, enc.X11, -abi.RV64InvocationViewOffset)
	}
	a.Addi(enc.X25, enc.X11, 0)
	a.Ld(enc.X10, enc.SP, 24)
	a.Ld(enc.X12, enc.SP, 32)
	a.Ld(enc.X13, enc.SP, 40)
	a.Ld(enc.X24, enc.SP, 48)
	a.Ld(enc.RA, enc.SP, 0)
	a.Constant64(enc.Scratch0, int64(e.recordBytes()))
	a.Add(enc.SP, enc.SP, enc.Scratch0)
	a.Ret()
	return e.finish()
}

// This guest sequence is process-address-free. X28 survives the qualified
// marshaller; saved header0 is authoritative after linked preflight. It is only
// usable with the explicit private128-byte context/factory contract.
func (f *integerEmitter) tailRAProofCall(site uint32, sig *wasm.CompType, base int, spill func(int) int32) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if site >= tailRAMaxSites || f.mode.production == false || f.mode.interruptible == false || f.frameHeader() != callFrameHeader || sig == nil {
		f.err = unsupported("private tail proof guest contract")
		return
	}
	if err := tailRASignature(sig); err != nil {
		f.err = err
		return
	}
	a := &f.a
	// Prior scalar/vector operations may use every transport register. The
	// framed header is authoritative before the linked authentication call.
	f.restoreCallTransport()
	a.Ld(enc.X29, enc.X25, -abi.RV64NativeContextOffset)
	f.trap(trapImportContext, a.BranchLong(enc.EQ, enc.X29, enc.Zero))
	a.Ld(enc.X29, enc.X29, tailRACapOffset)
	f.trap(trapImportMissing, a.BranchLong(enc.EQ, enc.X29, enc.Zero))
	a.Ld(enc.Scratch0, enc.X29, 0)
	a.Constant64(enc.Scratch1, int64(tailRACapMagic))
	f.trap(trapImportContext, a.BranchLong(enc.NE, enc.Scratch0, enc.Scratch1))
	a.Lwu(enc.Scratch0, enc.X29, 8)
	a.Addi(enc.Scratch1, enc.Zero, tailRAVersion)
	f.trap(trapImportContext, a.BranchLong(enc.NE, enc.Scratch0, enc.Scratch1))
	a.Lwu(enc.Scratch0, enc.X29, 12)
	a.Constant64(enc.Scratch1, int64(site))
	f.trap(trapImportContext, a.BranchLong(enc.GEU, enc.Scratch1, enc.Scratch0))
	tailRAAddress(a, enc.Scratch1, enc.X29, tailRACapHeader+int(site)*tailRASiteRow)
	a.Ld(enc.X28, enc.Scratch1, 0)
	f.trap(trapImportMissing, a.BranchLong(enc.EQ, enc.X28, enc.Zero))
	a.Jalr(enc.RA, enc.X28, 0)
	f.swarCallArguments(sig, base, spill)
	a.Jalr(enc.Zero, enc.X28, 0)
}
