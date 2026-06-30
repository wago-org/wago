package amd64

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

type callReloc struct {
	at     int // offset of the rel32 within this function's code
	target int // target local function index
}

func align16(n int) int { return (n + 15) &^ 15 }

type Unsupported struct{ Op byte }

func (e *Unsupported) Error() string { return fmt.Sprintf("amd64: unsupported opcode 0x%02x", e.Op) }

// R12 is excluded because using it as a memory base forces a SIB byte.
var scratch = []Reg{RAX, RCX, RDX, RBX, R8, R9, R10, R11, R13, R14, R15}

type vkind uint8

const (
	vConst  vkind = iota // immediate constant (not yet materialized)
	vLocal               // reference to a local's frame slot (lazy)
	vReg                 // value resident in a scratch register
	vSpill               // value materialized in its canonical frame slot
	vPinned              // reference to a local pinned in a reserved register (lazy)
)

// regNone marks a local that is not pinned to a register (lives in its frame slot).
const regNone Reg = 0xFF

// pinnedLocal records an integer local kept resident in a dedicated register
// for the whole function (spilled to its frame slot only around calls).
type pinnedLocal struct {
	local int
	reg   Reg
}

// pinnedGlobal records a numeric global kept resident in a dedicated register.
// The register is authoritative within the function; it is written back to the
// shared cell at every call boundary and at function return, and reloaded after
// a call (the callee may have written the same global). wide selects i64 vs i32.
type pinnedGlobal struct {
	glob int
	reg  Reg
	wide bool
}

type ventry struct {
	kind  vkind
	fp    bool  // value is a float (lives in an XMM register / slot holds float bits)
	wide  bool  // vConst: i64 or f64 (vs i32/f32)
	cval  int64 // vConst value/bits
	local int   // vPinned local-pin: local index (glob == -1)
	glob  int   // vPinned global-pin: global index (local == -1)
	reg   Reg
	slot  int
}

type cg struct {
	a           *Asm
	m           *wasm.Module
	st          []ventry // symbolic operand stack
	busy        [16]bool // GPR occupancy
	fbusy       [16]bool // XMM occupancy
	nLocals     int
	maxDepth    int
	ctrl        []cframe // control frames (function frame is ctrl[0])
	unreachable bool     // current point is unreachable (emit nothing)
	retSites    []int    // `return`/br-to-function jump sites to patch to epilogue
	nResults    int
	relocs      []callReloc // internal call sites to patch at module layout
	localParams []wasm.ValType
	localRuns   []wasm.LocalRun
	localReg    []Reg // per-local: pinned register, or regNone if frame-resident
	pinned      []pinnedLocal
	globalReg   []Reg          // per-global: pinned register, or regNone if cell-resident
	pinnedGlob  []pinnedGlobal // globals pinned in registers, in assignment order
	poolUsed    int            // number of pinnedPool registers taken by pinned locals
	reserved    [16]bool       // GPRs dedicated to pinned locals/globals (not allocatable as scratch)
}

// Frame layout: saved ABI pointers, locals, then operand-stack spill slots.
func (g *cg) localOff(i int) int32 { return -int32(40 + 8*i) }
func (g *cg) slotOff(d int) int32  { return -int32(40 + 8*g.nLocals + 8*d) }

func (g *cg) push(e ventry) {
	g.st = append(g.st, e)
	if len(g.st) > g.maxDepth {
		g.maxDepth = len(g.st)
	}
}
func (g *cg) pop() ventry   { e := g.st[len(g.st)-1]; g.st = g.st[:len(g.st)-1]; return e }
func (g *cg) pushReg(r Reg) { g.busy[r] = true; g.push(ventry{kind: vReg, reg: r}) }
func (g *cg) freeReg(r Reg) { g.busy[r] = false }

func (g *cg) allocRegExcept(except Reg) Reg {
	for _, r := range scratch {
		if r != except && !g.busy[r] && !g.reserved[r] {
			g.busy[r] = true
			return r
		}
	}
	for i := range g.st {
		if g.st[i].kind == vReg && !g.st[i].fp && g.st[i].reg != except {
			r := g.st[i].reg
			// Spill to the entry's canonical slot so branch joins stay deterministic.
			g.a.Store64(RBP, g.slotOff(i), r)
			g.st[i] = ventry{kind: vSpill, slot: i}
			g.busy[r] = true
			return r
		}
	}
	panic("amd64: no register available to spill")
}

func (g *cg) allocReg() Reg { return g.allocRegExcept(0xFF) }

func (g *cg) ensureFree(r Reg) {
	if !g.busy[r] {
		return
	}
	for i := range g.st {
		if g.st[i].kind == vReg && !g.st[i].fp && g.st[i].reg == r {
			g.a.Store64(RBP, g.slotOff(i), r)
			g.st[i] = ventry{kind: vSpill, slot: i}
			break
		}
	}
	g.busy[r] = false
}

// Slots and locals are 64-bit; i32 values are already zero-extended.
func (g *cg) loadInto(dst Reg, e ventry) {
	switch e.kind {
	case vConst:
		if e.wide {
			g.a.MovImm64(dst, uint64(e.cval))
		} else {
			g.a.MovImm32(dst, int32(e.cval))
		}
	case vLocal:
		g.a.Load64(dst, RBP, g.localOff(e.local))
	case vReg:
		if e.reg != dst {
			g.a.MovReg64(dst, e.reg)
			g.freeReg(e.reg)
		}
	case vPinned:
		if e.reg != dst { // pinned reg is shared storage; copy out, never free
			g.a.MovReg64(dst, e.reg)
		}
	case vSpill:
		g.a.Load64(dst, RBP, g.slotOff(e.slot))
	}
}

// materializeLocalRefs prevents lazy local.get entries from seeing later writes.
// It captures pending reads of local x — both frame-resident (vLocal) and
// register-pinned (vPinned) — to their canonical slots before x is overwritten.
func (g *cg) materializeLocalRefs(x int) {
	for i := range g.st {
		switch {
		case g.st[i].kind == vLocal && g.st[i].local == x:
			g.a.Load64(RSI, RBP, g.localOff(x))
			g.a.Store64(RBP, g.slotOff(i), RSI)
			g.st[i] = ventry{kind: vSpill, slot: i}
		case g.st[i].kind == vPinned && g.st[i].local == x:
			g.a.Store64(RBP, g.slotOff(i), g.st[i].reg)
			g.st[i] = ventry{kind: vSpill, slot: i}
		}
	}
}

func (g *cg) materialize(e ventry) Reg {
	if e.kind == vReg {
		return e.reg
	}
	dst := g.allocReg()
	g.loadInto(dst, e)
	return dst
}

func (g *cg) intoDest(a, b ventry, commutative bool) (Reg, ventry) {
	if a.kind == vReg {
		return a.reg, b
	}
	if commutative && b.kind == vReg {
		return b.reg, a
	}
	dst := g.allocReg()
	g.loadInto(dst, a)
	return dst, b
}

func (g *cg) loadGlobalsBase() Reg {
	base := g.allocReg()
	g.a.Load64(base, RBP, -16)                           // saved linMem pointer
	g.a.Load64(base, base, -int32(abi.GlobalsPtrOffset)) // globals pointer table
	return base
}

func (g *cg) globalGet(r *wasm.Reader) error {
	x, err := r.U32()
	if err != nil {
		return err
	}
	if gr := g.globalRegOf(x); gr != regNone {
		g.push(ventry{kind: vPinned, reg: gr, glob: int(x), local: -1})
		return nil
	}
	gt, ok := g.m.GlobalTypeByIndex(x)
	if !ok {
		return fmt.Errorf("amd64: unknown global %d", x)
	}
	gtv := wasm.GlobalValueType(gt)
	cell := g.loadGlobalsBase()
	disp := int32(x * 8)
	g.a.Load64(cell, cell, disp)
	if wasm.EqualValType(gtv, wasm.F32) || wasm.EqualValType(gtv, wasm.F64) {
		xmm := g.allocFReg()
		// f32 uses the low half of the 8-byte cell; f64 uses the full cell.
		g.a.FLoadDisp(xmm, cell, 0, wasm.EqualValType(gtv, wasm.F64))
		g.freeReg(cell)
		g.pushFReg(xmm)
	} else if wasm.EqualValType(gtv, wasm.I64) {
		dst := cell
		g.a.Load64(dst, cell, 0)
		g.pushReg(dst)
	} else if wasm.EqualValType(gtv, wasm.I32) {
		dst := cell
		g.a.Load32(dst, cell, 0) // i32 occupies the low half of the 8-byte cell
		g.pushReg(dst)
	} else {
		g.freeReg(cell)
		return fmt.Errorf("amd64: unsupported global.get type %s for global %d", gtv, x)
	}
	return nil
}

func (g *cg) globalSet(r *wasm.Reader) error {
	x, err := r.U32()
	if err != nil {
		return err
	}
	if gr := g.globalRegOf(x); gr != regNone {
		v := g.pop()
		g.materializeGlobalRefs(int(x)) // capture pending reads before overwriting
		g.loadInto(gr, v)               // write straight into the pinned register
		return nil
	}
	gt, ok := g.m.GlobalTypeByIndex(x)
	if !ok {
		return fmt.Errorf("amd64: unknown global %d", x)
	}
	v := g.pop()
	gtv := wasm.GlobalValueType(gt)
	cell := g.loadGlobalsBase()
	disp := int32(x * 8)
	g.a.Load64(cell, cell, disp)
	if wasm.EqualValType(gtv, wasm.F32) || wasm.EqualValType(gtv, wasm.F64) {
		xmm := g.materializeF(v)
		// f32 updates only the low half of the 8-byte cell; f64 stores the full cell.
		g.a.FStoreDisp(cell, 0, xmm, wasm.EqualValType(gtv, wasm.F64))
		g.freeFReg(xmm)
	} else if wasm.EqualValType(gtv, wasm.I64) {
		rg := g.materialize(v)
		g.a.Store64(cell, 0, rg)
		g.freeReg(rg)
	} else if wasm.EqualValType(gtv, wasm.I32) {
		rg := g.materialize(v)
		g.a.Store32(cell, 0, rg) // i32 updates only the low half; runtime/API reads canonicalize
		g.freeReg(rg)
	} else {
		g.freeReg(cell)
		return fmt.Errorf("amd64: unsupported global.set type %s for global %d", gtv, x)
	}
	g.freeReg(cell)
	return nil
}

type aluDesc struct {
	rr, rm, digit byte
	comm          bool
	op            opKind // for constant folding
}

var (
	opAdd = aluDesc{0x01, 0x03, 0, true, opAddK}
	opSub = aluDesc{0x29, 0x2B, 5, false, opSubK}
	opAnd = aluDesc{0x21, 0x23, 4, true, opAndK}
	opOr  = aluDesc{0x09, 0x0B, 1, true, opOrK}
	opXor = aluDesc{0x31, 0x33, 6, true, opXorK}
)

func fitsImm32(v int64) bool { return v >= -2147483648 && v <= 2147483647 }

func (g *cg) applyALU(d aluDesc, dst Reg, src ventry, w bool) {
	switch src.kind {
	case vConst:
		if fitsImm32(src.cval) {
			g.a.AluRI(d.digit, dst, int32(src.cval), w)
		} else {
			t := g.allocReg()
			g.a.MovImm64(t, uint64(src.cval))
			g.a.AluRR(d.rr, dst, t, w)
			g.freeReg(t)
		}
	case vReg:
		g.a.AluRR(d.rr, dst, src.reg, w)
		g.freeReg(src.reg)
	case vPinned:
		g.a.AluRR(d.rr, dst, src.reg, w) // pinned reg as RHS; shared storage, never freed
	case vLocal:
		g.a.AluRM(d.rm, dst, RBP, g.localOff(src.local), w)
	case vSpill:
		g.a.AluRM(d.rm, dst, RBP, g.slotOff(src.slot), w)
	}
}

func (g *cg) binALU(d aluDesc, w bool) {
	b := g.pop()
	a := g.pop()
	if bothConst(a, b) {
		g.push(ventry{kind: vConst, wide: w, cval: foldALU(d.op, a.cval, b.cval, w)})
		return
	}
	dst, src := g.intoDest(a, b, d.comm)
	g.applyALU(d, dst, src, w)
	g.pushReg(dst)
}

func (g *cg) mul(w bool) {
	b := g.pop()
	a := g.pop()
	if bothConst(a, b) {
		g.push(ventry{kind: vConst, wide: w, cval: foldMul(a.cval, b.cval, w)})
		return
	}
	dst, src := g.intoDest(a, b, true)
	switch src.kind {
	case vConst:
		if fitsImm32(src.cval) {
			g.a.ImulRI(dst, int32(src.cval), w)
		} else {
			t := g.allocReg()
			g.a.MovImm64(t, uint64(src.cval))
			g.a.IMul(dst, t, w)
			g.freeReg(t)
		}
	case vReg:
		g.a.IMul(dst, src.reg, w)
		g.freeReg(src.reg)
	case vPinned:
		g.a.IMul(dst, src.reg, w) // pinned reg as RHS; shared storage, never freed
	case vLocal:
		g.a.ImulRM(dst, RBP, g.localOff(src.local), w)
	case vSpill:
		g.a.ImulRM(dst, RBP, g.slotOff(src.slot), w)
	}
	g.pushReg(dst)
}

// x86 division uses the fixed DX:AX pair.
func (g *cg) divRem(signed, wantRem, w bool) {
	b := g.pop() // divisor
	a := g.pop() // dividend
	if bothConst(a, b) {
		if v, ok := foldDivRem(signed, wantRem, w, a.cval, b.cval); ok {
			g.push(ventry{kind: vConst, wide: w, cval: v})
			return
		}
		// would trap (÷0 or signed overflow): fall through to codegen that
		// reproduces the trap at runtime.
	}
	g.ensureFree(RAX)
	g.ensureFree(RDX)
	g.busy[RAX] = true
	g.busy[RDX] = true
	var dreg Reg
	if b.kind == vReg && b.reg != RAX && b.reg != RDX {
		dreg = b.reg
	} else {
		dreg = g.allocReg()
		g.loadInto(dreg, b)
	}
	g.loadInto(RAX, a)

	g.a.TestSelf(dreg, w)
	nz := g.a.JccPlaceholder(CondNE)
	g.emitTrap(trapDivZero)
	g.a.PatchRel32(nz, g.a.Len())

	if signed && !wantRem { // INT_MIN / -1 overflows
		g.a.AluRI(7, dreg, -1, w)
		noOvf := g.a.JccPlaceholder(CondNE)
		g.cmpIntMin(w)
		noOvf2 := g.a.JccPlaceholder(CondNE)
		g.emitTrap(trapDivOverflow)
		g.a.PatchRel32(noOvf, g.a.Len())
		g.a.PatchRel32(noOvf2, g.a.Len())
		g.a.Cdq(w)
		g.a.Idiv(dreg, w)
	} else if signed { // rem_s: x % -1 == 0 (avoid #DE on INT_MIN/-1)
		g.a.AluRI(7, dreg, -1, w)
		notM1 := g.a.JccPlaceholder(CondNE)
		g.a.XorSelf32(RDX)
		done := g.a.JmpPlaceholder()
		g.a.PatchRel32(notM1, g.a.Len())
		g.a.Cdq(w)
		g.a.Idiv(dreg, w)
		g.a.PatchRel32(done, g.a.Len())
	} else {
		g.a.XorSelf32(RDX)
		g.a.Div(dreg, w)
	}

	g.freeReg(dreg)
	res := RAX
	if wantRem {
		res = RDX
		g.busy[RAX] = false
	} else {
		g.busy[RDX] = false
	}
	g.pushReg(res)
}

func (g *cg) cmpIntMin(w bool) {
	if w {
		t := g.allocReg()
		g.a.MovImm64(t, 0x8000000000000000)
		g.a.AluRR(0x39, RAX, t, true) // cmp rax, t
		g.freeReg(t)
	} else {
		g.a.AluRI(7, RAX, int32(-2147483648), false)
	}
}

func (g *cg) callOp(r *wasm.Reader) error {
	idx, err := r.U32()
	if err != nil {
		return err
	}
	ft, ok := g.m.FuncSignature(idx)
	if !ok {
		return fmt.Errorf("call: unknown function %d", idx)
	}
	imported := g.m.ImportedFuncCount()
	if int(idx) < imported {
		return g.callHost(int(idx), ft)
	}
	return g.callInternal(int(idx)-imported, ft)
}

func (g *cg) callInternal(localIdx int, ft *wasm.CompType) error {
	g.emitWrapperCall(len(ft.Params), len(ft.Results), func() {
		site := g.a.CallRel32()
		g.relocs = append(g.relocs, callReloc{at: site, target: localIdx})
	})
	return nil
}

// storeArgToRsp writes one call argument from its operand entry straight into
// the rsp arg buffer at byte offset disp, avoiding the canonical-slot round-trip
// the old path took (flush-to-slot then slot->RAX->buffer).
// Uses RSI as scratch, never RAX: a later argument may be a live vReg in RAX,
// and RSI is never an argument register (excluded from the scratch pool).
func (g *cg) storeArgToRsp(disp int32, e ventry) {
	switch e.kind {
	case vReg:
		if e.fp {
			g.a.MovXmmToGpr(RSI, e.reg, true)
			g.a.StoreRsp64(disp, RSI)
			g.fbusy[e.reg] = false
		} else {
			g.a.StoreRsp64(disp, e.reg)
			g.busy[e.reg] = false
		}
	case vPinned:
		g.a.StoreRsp64(disp, e.reg)
	case vConst:
		if e.wide {
			g.a.MovImm64(RSI, uint64(e.cval))
		} else {
			g.a.MovImm32(RSI, int32(e.cval))
		}
		g.a.StoreRsp64(disp, RSI)
	case vLocal:
		g.a.Load64(RSI, RBP, g.localOff(e.local))
		g.a.StoreRsp64(disp, RSI)
	case vSpill:
		g.a.Load64(RSI, RBP, g.slotOff(e.slot))
		g.a.StoreRsp64(disp, RSI)
	}
}

// emitWrapperCall uses the WasmWrapper ABI over native-stack arg/result slots.
// Operands below the arguments are flushed to their slots; the arguments
// themselves are written straight into the buffer from their live locations.
func (g *cg) emitWrapperCall(p, rN int, emitCall func()) {
	depth := len(g.st)
	g.flushBelow(depth - p)
	buf := align16((p + rN) * 8)
	if buf > 0 {
		g.a.SubRsp(int32(buf))
	}
	for i := 0; i < p; i++ {
		g.storeArgToRsp(int32(i*8), g.st[depth-p+i])
	}
	for i := range g.busy { // callee clobbers all scratch; results are re-allocated below
		g.busy[i] = false
		g.fbusy[i] = false
	}
	g.a.MovFromRsp(RDI)         // args
	g.a.LeaRsp(RCX, int32(p*8)) // results
	g.a.Load64(RSI, RBP, -16)   // linMem
	g.a.Load64(RDX, RBP, -24)   // trap
	// The callee clobbers our pinned-local registers (they are plain scratch in
	// its frame), so spill them to their slots across the call and reload after.
	// Pinned globals are flushed to their shared cells (so the callee sees the
	// current value) and reloaded after (the callee may have written them).
	for _, pl := range g.pinned {
		g.a.Store64(RBP, g.localOff(pl.local), pl.reg)
	}
	g.writeBackGlobals(RAX)
	emitCall()
	g.reloadGlobals(RAX)
	for _, pl := range g.pinned {
		g.a.Load64(pl.reg, RBP, g.localOff(pl.local))
	}

	g.a.Load64(RAX, RBP, -24)
	g.a.Load32(RAX, RAX, 0)
	g.a.TestSelf(RAX, false)
	ok := g.a.JccPlaceholder(CondE)
	if buf > 0 {
		g.a.AddRsp(int32(buf))
	}
	g.a.Leave()
	g.a.Ret()
	g.a.PatchRel32(ok, g.a.Len())

	g.st = g.st[:depth-p]
	res := make([]Reg, rN)
	for i := 0; i < rN; i++ {
		res[i] = g.allocReg()
		g.a.LoadRsp64(res[i], int32(p*8+i*8))
	}
	if buf > 0 {
		g.a.AddRsp(int32(buf))
	}
	for i := 0; i < rN; i++ {
		g.pushReg(res[i])
	}
}

// callIndirect uses table entries {codePtr u64, sigID u32, pad u32}.
func (g *cg) callIndirect(r *wasm.Reader) error {
	typeIdx, err := r.U32()
	if err != nil {
		return err
	}
	tableIdx, err := r.U32()
	if err != nil {
		return err
	}
	if tableIdx != 0 {
		return fmt.Errorf("call_indirect: multi-table unsupported: table %d", tableIdx)
	}
	ft, ok := g.m.TypeFunc(typeIdx)
	if !ok {
		return fmt.Errorf("call_indirect: bad type %d", typeIdx)
	}
	canon := int32(g.m.CanonicalTypeID(typeIdx))

	idxReg := g.materialize(g.pop()) // table index (i32)
	lm := g.allocReg()
	g.a.Load64(lm, RBP, -16) // linMem base
	tbl := g.allocReg()
	g.a.Load64(tbl, lm, -int32(offTablePtr)) // table descriptor

	ln := g.allocReg()
	g.a.Load32(ln, tbl, 0)
	g.a.AluRR(0x39, idxReg, ln, false) // cmp idx, len
	g.freeReg(ln)
	inb := g.a.JccPlaceholder(CondB)
	g.emitTrap(trapIndirectOOB)
	g.a.PatchRel32(inb, g.a.Len())

	// Keep pointer arithmetic 64-bit; a 32-bit add would truncate tbl.
	g.a.ShiftImm(4, idxReg, 4, true)   // idx *= 16
	g.a.AluRR(0x01, idxReg, tbl, true) // idx += tbl (64-bit address)
	g.freeReg(tbl)

	// idxReg points at the descriptor slot base; payload fields start at +8.
	tid := g.allocReg()
	g.a.Load32(tid, idxReg, 16)
	g.a.AluRI(7, tid, canon, false)
	g.freeReg(tid)
	okSig := g.a.JccPlaceholder(CondE)
	g.emitTrap(trapIndirectSig)
	g.a.PatchRel32(okSig, g.a.Len())

	code := g.allocReg()
	g.a.Load64(code, idxReg, 8)
	g.freeReg(idxReg)
	g.a.TestSelf(code, true)
	okNull := g.a.JccPlaceholder(CondNE)
	g.emitTrap(trapIndirectOOB)
	g.a.PatchRel32(okNull, g.a.Len())

	g.a.Store64(lm, -int32(offSpillRegion), code)
	g.freeReg(code)
	g.freeReg(lm)

	g.emitWrapperCall(len(ft.Params), len(ft.Results), func() {
		g.a.Load64(RAX, RSI, -int32(offSpillRegion)) // RSI = linMem; reload codePtr
		g.a.CallReg(RAX)
	})
	return nil
}

const (
	offCustomCtx   = 40 // host-call log pointer (WARP's V2 import ctx slot)
	offSpillRegion = 48 // 8B scratch (used to carry call_indirect's code ptr across flush)
	offTablePtr    = 80 // indirect-call table descriptor pointer
)

// callHost records imports for dispatch after returning to Go.
func (g *cg) callHost(importIdx int, ft *wasm.CompType) error {
	if len(ft.Results) != 0 {
		return fmt.Errorf("amd64: host import with results not yet supported")
	}
	p := len(ft.Params)
	g.flush()
	depth := len(g.st)
	if p > 0 {
		g.a.Load32(RAX, RBP, g.slotOff(depth-p)) // first param
	} else {
		g.a.XorSelf32(RAX)
	}
	// Host calls are logged and replayed on the Go stack after native return.
	g.a.Load64(RDI, RBP, -16)           // linMem
	g.a.Load64(RDI, RDI, -offCustomCtx) // RDI = host-call log
	g.a.Load32(RCX, RDI, 0)             // count
	g.a.LeaScaled(RDX, RDI, RCX, 3, 8)  // entry = log + count*8 + 8
	g.a.StoreImm32Mem(RDX, 0, int32(importIdx))
	g.a.Store32(RDX, 4, RAX)
	g.a.AluRI(0, RCX, 1, false) // count++
	g.a.Store32(RDI, 0, RCX)
	g.st = g.st[:depth-p]
	return nil
}

// memEffectiveAddr returns ea and leaves RDI as linMem base.
func (g *cg) memEffectiveAddr(off uint32, size int) Reg {
	addr := g.pop()
	ea := g.allocReg()
	g.loadInto(ea, addr) // ea = addr (u32, zero-extended to 64)
	if off != 0 {
		g.a.MovImm32(RSI, int32(off)) // RSI = off (zero-extended)
		g.a.Add64(ea, RSI)
	}
	t := g.allocReg()
	g.a.LeaDisp(t, ea, int32(size)) // t = ea + size
	g.a.Load64(RDI, RBP, -16)       // linMem base
	g.a.Load32(RSI, RDI, -8)        // memBytes (zero-extended)
	g.a.Cmp64(t, RSI)
	ok := g.a.JccPlaceholder(CondBE) // jbe ok (ea+size <= memBytes)
	g.emitTrap(trapMemOOB)
	g.a.PatchRel32(ok, g.a.Len())
	g.freeReg(t)
	return ea
}

// memLoad lowers a load of `size` bytes. signed selects sign-extension; wide
// selects an i64 result (so signed sub-width loads extend to all 64 bits).
func (g *cg) memLoad(r *wasm.Reader, size int, signed, wide bool) error {
	if _, err := r.U32(); err != nil { // align
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	ea := g.memEffectiveAddr(off, size)
	g.a.LoadIdx(ea, RDI, ea, size, signed, wide) // ea = mem[linMem + ea]
	g.pushReg(ea)
	return nil
}

func (g *cg) memStore(r *wasm.Reader, size int) error {
	if _, err := r.U32(); err != nil { // align
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	val := g.pop()
	vreg := g.materialize(val)
	ea := g.memEffectiveAddr(off, size)
	g.a.StoreIdx(RDI, ea, vreg, size)
	g.freeReg(ea)
	g.freeReg(vreg)
	return nil
}

// invertCond returns the condition that holds exactly when c does not. x86
// condition codes are paired by their low bit, so flipping it negates.
func invertCond(c Cond) Cond { return c ^ 1 }

// emitCompare pops two integer operands and emits `cmp a, b`, leaving the
// comparison result only in EFLAGS. It returns the (now dead) register that
// held a; callers that don't want a 0/1 value should free it. The `cond`
// passed by the consumer selects how the flags are later interpreted.
func (g *cg) emitCompare(w bool) Reg {
	b := g.pop()
	a := g.pop()
	var dst Reg
	if a.kind == vReg {
		dst = a.reg
	} else {
		dst = g.allocReg()
		g.loadInto(dst, a)
	}
	switch {
	case b.kind == vConst && fitsImm32(b.cval):
		g.a.AluRI(7, dst, int32(b.cval), w)
	case b.kind == vConst:
		t := g.allocReg()
		g.a.MovImm64(t, uint64(b.cval))
		g.a.AluRR(0x39, dst, t, w)
		g.freeReg(t)
	case b.kind == vReg:
		g.a.AluRR(0x39, dst, b.reg, w)
		g.freeReg(b.reg)
	case b.kind == vPinned:
		g.a.AluRR(0x39, dst, b.reg, w) // pinned reg as RHS; shared storage, never freed
	case b.kind == vLocal:
		g.a.AluRM(0x3B, dst, RBP, g.localOff(b.local), w)
	case b.kind == vSpill:
		g.a.AluRM(0x3B, dst, RBP, g.slotOff(b.slot), w)
	}
	return dst
}

func (g *cg) cmp(cond Cond, w bool) {
	dst := g.emitCompare(w)
	g.a.SetccReg(cond, dst) // result is i32 (0/1)
	g.pushReg(dst)
}

func (g *cg) isFloatOperand(e ventry) bool {
	switch e.kind {
	case vReg, vConst:
		return e.fp
	case vLocal:
		return g.isFloatLocal(e.local)
	}
	return false // vSpill: type not tracked; assume integer
}

func (g *cg) selectOp(typed, isFloat bool) {
	cond := g.pop()
	b := g.pop()
	a := g.pop()
	if !typed {
		isFloat = g.isFloatOperand(a) || g.isFloatOperand(b)
	}
	condReg := g.materialize(cond)
	g.a.TestSelf(condReg, false)
	if isFloat {
		dst := g.materializeF(a)
		keep := g.a.JccPlaceholder(CondNE) // cond != 0 -> keep a
		src := g.materializeF(b)
		g.a.FMov(dst, src, true)
		g.freeFReg(src)
		g.a.PatchRel32(keep, g.a.Len())
		g.freeReg(condReg)
		g.pushFReg(dst)
		return
	}
	dst := g.materialize(a)
	src := g.materialize(b)
	g.a.Cmovcc(CondE, dst, src, true) // cond == 0 -> dst = b (64-bit, covers i64)
	g.freeReg(src)
	g.freeReg(condReg)
	g.pushReg(dst)
}

func (g *cg) intUnary(w bool, emit func(dst, src Reg, w bool), kind unaryOp) {
	a := g.pop()
	if a.kind == vConst && !a.fp {
		g.push(ventry{kind: vConst, wide: w, cval: foldUnary(kind, a.cval, w)})
		return
	}
	var dst Reg
	if a.kind == vReg {
		dst = a.reg
	} else {
		dst = g.allocReg()
		g.loadInto(dst, a)
	}
	emit(dst, dst, w)
	g.pushReg(dst)
}

// emitEqzTest pops one operand and emits `test a, a`, leaving the result in
// EFLAGS (CondE means a == 0). Returns the dead register that held a.
func (g *cg) emitEqzTest(w bool) Reg {
	a := g.pop()
	var dst Reg
	if a.kind == vReg {
		dst = a.reg
	} else {
		dst = g.allocReg()
		g.loadInto(dst, a)
	}
	g.a.TestSelf(dst, w)
	return dst
}

func (g *cg) eqz(w bool) {
	dst := g.emitEqzTest(w)
	g.a.SetccReg(CondE, dst)
	g.pushReg(dst)
}

func (g *cg) shift(digit byte, w bool) {
	b := g.pop()
	a := g.pop()
	if bothConst(a, b) {
		g.push(ventry{kind: vConst, wide: w, cval: foldShift(digit, a.cval, b.cval, w)})
		return
	}
	mask := uint64(31)
	if w {
		mask = 63
	}
	if b.kind == vConst {
		var dst Reg
		if a.kind == vReg {
			dst = a.reg
		} else {
			dst = g.allocReg()
			g.loadInto(dst, a)
		}
		g.a.ShiftImm(digit, dst, byte(uint64(b.cval)&mask), w)
		g.pushReg(dst)
		return
	}
	var dst Reg
	if a.kind == vReg && a.reg != RCX {
		dst = a.reg
	} else {
		dst = g.allocRegExcept(RCX)
		g.loadInto(dst, a)
	}
	g.ensureFree(RCX)
	g.loadInto(RCX, b)
	g.a.ShiftCL(digit, dst, w)
	g.pushReg(dst)
}

type CompiledModule struct {
	Code  []byte // all local functions concatenated, 16-byte aligned
	Entry []int  // Entry[localFuncIdx] = byte offset of that function in Code
}

// CompileModule compiles local functions into one executable blob.
func CompileModule(m *wasm.Module) (*CompiledModule, error) {
	n := len(m.FuncTypes)
	codes := make([][]byte, n)
	relocs := make([][]callReloc, n)
	for i := 0; i < n; i++ {
		c, rl, err := compileFunc(m, i)
		if err != nil {
			return nil, fmt.Errorf("function %d: %w", i, err)
		}
		codes[i], relocs[i] = c, rl
	}
	entry := make([]int, n)
	var all []byte
	for i := 0; i < n; i++ {
		for len(all)%16 != 0 {
			all = append(all, 0xCC) // int3 padding between functions
		}
		entry[i] = len(all)
		all = append(all, codes[i]...)
	}
	for i := 0; i < n; i++ {
		for _, rl := range relocs[i] {
			site := entry[i] + rl.at
			binary.LittleEndian.PutUint32(all[site:], uint32(int32(entry[rl.target]-(site+4))))
		}
	}
	return &CompiledModule{Code: all, Entry: entry}, nil
}

// CompileFunction compiles one local function with no internal calls.
func CompileFunction(m *wasm.Module, funcIdx int) ([]byte, error) {
	code, relocs, err := compileFunc(m, funcIdx)
	if err != nil {
		return nil, err
	}
	if len(relocs) > 0 {
		return nil, fmt.Errorf("amd64: function %d makes calls; use CompileModule", funcIdx)
	}
	return code, nil
}

// maxCompiledLocals bounds native stack-frame size and keeps local/frame-slot
// displacements comfortably encodable until locals move out of the machine stack.
const maxCompiledLocals = 1 << 16

func countCompiledLocals(params []wasm.ValType, locals wasm.Locals) (int, error) {
	// The validator handles arbitrary local run counts without expansion, but this
	// backend stores locals in the native stack frame. Keep that frame bounded until
	// a heap/linear-memory local area exists.
	n, overflow := wasm.LocalCount(params, locals.Runs)
	if overflow || n > maxCompiledLocals {
		return 0, fmt.Errorf("amd64: local count %d exceeds limit %d", n, maxCompiledLocals)
	}
	return int(n), nil
}

// compileFunc lowers one local wasm function to WasmWrapper-ABI machine code.
func compileFunc(m *wasm.Module, funcIdx int) (code []byte, relocs []callReloc, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("amd64: %v", r)
		}
	}()

	ft, ok := m.LocalFuncType(funcIdx)
	if !ok {
		return nil, nil, fmt.Errorf("amd64: function %d has unknown type", funcIdx)
	}
	c := &m.Code[funcIdx]

	nParams := len(ft.Params)
	nLocals, err := countCompiledLocals(ft.Params, c.Locals)
	if err != nil {
		return nil, nil, err
	}

	a := &Asm{}
	g := &cg{a: a, m: m, nLocals: nLocals, nResults: len(ft.Results), localParams: ft.Params, localRuns: c.Locals.Runs}
	g.assignPinnedLocals()
	g.assignPinnedGlobals()

	a.Prologue()
	subRspAt := a.Len() + 3
	a.SubRsp(0)
	a.Store64(RBP, -8, RDI)
	a.Store64(RBP, -16, RSI)
	a.Store64(RBP, -24, RDX)
	a.Store64(RBP, -32, RCX)
	for i := 0; i < nParams; i++ { // copy params (8-byte slots; i32 args zero-extended)
		a.Load64(RAX, RDI, int32(8*i))
		a.Store64(RBP, g.localOff(i), RAX)
	}
	if nLocals > nParams {
		declared := nLocals - nParams
		a.XorSelf32(RAX)
		if declared <= 3 {
			// Avoid the fixed setup cost of rep stosb for the common tiny-local case.
			for i := nParams; i < nLocals; i++ {
				a.Store64(RBP, g.localOff(i), RAX)
			}
		} else {
			// Zero declared locals with one short memset-style sequence instead of one
			// store per local. Large local runs are valid wasm but should not bloat code
			// size or compile time linearly.
			a.LeaDisp(RDI, RBP, g.localOff(nLocals-1))
			a.MovImm32(RCX, int32(declared*8))
			a.Cld()
			a.RepStosb()
		}
	}
	// Prime each pinned local's register from its now-initialized frame slot.
	for _, pl := range g.pinned {
		a.Load64(pl.reg, RBP, g.localOff(pl.local))
	}
	// Prime each pinned global's register from its shared cell.
	g.reloadGlobals(RSI)

	g.ctrl = append(g.ctrl, cframe{kind: ckFunc, height: 0, resultN: len(ft.Results), branchN: len(ft.Results)})

	body := c.BodyBytes
	if len(body) == 0 {
		var err error
		body, err = wasm.EncodeExpr(c.Body)
		if err != nil {
			return nil, nil, err
		}
	}
	if err := g.body(wasm.NewReader(body)); err != nil {
		return nil, nil, err
	}

	for _, site := range g.retSites {
		a.PatchRel32(site, a.Len())
	}
	g.writeBackGlobals(RSI) // flush pinned globals to their cells before returning
	a.Load64(RDI, RBP, -32) // results ptr
	for i := 0; i < len(ft.Results); i++ {
		a.Load64(RAX, RBP, g.slotOff(i)) // 8-byte slots; i32 results zero-extended
		a.Store64(RDI, int32(8*i), RAX)
	}
	a.Load64(RSI, RBP, -24) // trap ptr
	a.StoreImm32Mem(RSI, 0, 0)
	a.Leave()
	a.Ret()

	frame := 40 + 8*nLocals + 8*g.maxDepth
	frame = (frame + 15) &^ 15
	a.PatchU32(subRspAt, uint32(frame))
	return a.B, g.relocs, nil
}

var i32cmp = map[byte]Cond{
	0x46: CondE, 0x47: CondNE,
	0x48: CondL, 0x49: CondB, 0x4A: CondG, 0x4B: CondA,
	0x4C: CondLE, 0x4D: CondBE, 0x4E: CondGE, 0x4F: CondAE,
}

// body walks the function bytecode until the implicit function frame closes.
func (g *cg) body(r *wasm.Reader) error {
	for len(g.ctrl) > 0 {
		op, err := r.Byte()
		if err != nil {
			return err
		}
		switch op {
		case 0x02, 0x03, 0x04: // block / loop / if
			err = g.opBlock(r, op)
		case 0x05: // else
			err = g.opElse()
		case 0x0B: // end
			err = g.opEnd()
		case 0x0C, 0x0D: // br / br_if
			err = g.opBr(r, op == 0x0D)
		case 0x0E: // br_table
			err = g.opBrTable(r)
		case 0x0F: // return
			err = g.opReturn()
		case 0x00: // unreachable
			if !g.unreachable {
				g.emitTrap(trapUnreachable)
				g.unreachable = true
			}
		default:
			if g.unreachable {
				err = skipImmediates(r, op)
			} else {
				err = g.emitPlain(r, op)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// pinnedPool lists the registers dedicated to integer locals, in priority order.
// These registers are treated as clobbered by generated wasm callees under
// wago's wrapper ABI, so callers spill/reload pinned locals around internal
// calls. At the Go/native boundary, enterNative saves/restores the Go
// callee-saved registers, so using RBX/R13/R14/R15 inside native wasm remains
// safe. While pinned they are never handed out by the scratch allocator, so each
// holds one local for the whole function. RAX/RCX/RDX are left free for
// div/shift/call-ABI fixed uses.
var pinnedPool = []Reg{RBX, R13, R14, R15}

// assignPinnedLocals pins the first few integer locals to dedicated registers.
// Float locals stay frame-resident. Pinning the low-index locals favors params,
// which are typically the hottest.
func (g *cg) assignPinnedLocals() {
	g.localReg = make([]Reg, g.nLocals)
	for i := range g.localReg {
		g.localReg[i] = regNone
	}
	k := 0
	for x := 0; x < g.nLocals && k < len(pinnedPool); x++ {
		if g.isFloatLocal(x) {
			continue
		}
		r := pinnedPool[k]
		k++
		g.localReg[x] = r
		g.reserved[r] = true
		g.pinned = append(g.pinned, pinnedLocal{local: x, reg: r})
	}
	g.poolUsed = k
}

// assignPinnedGlobals pins mutable integer globals into any pinnedPool registers
// left over after locals. A pinned global lives in its register for the whole
// function and is written back to / reloaded from its shared cell only at call
// boundaries and function return (see emitWrapperCall and the epilogue).
func (g *cg) assignPinnedGlobals() {
	n := len(g.m.Globals)
	g.globalReg = make([]Reg, n)
	for i := range g.globalReg {
		g.globalReg[i] = regNone
	}
	k := g.poolUsed
	for gi := 0; gi < n && k < len(pinnedPool); gi++ {
		gt, ok := g.m.GlobalTypeByIndex(uint32(gi))
		if !ok || !gt.Mutable {
			continue
		}
		wide := wasm.EqualValType(gt.Type, wasm.I64)
		if !wide && !wasm.EqualValType(gt.Type, wasm.I32) {
			continue // only integer globals are pinned; floats stay cell-resident
		}
		r := pinnedPool[k]
		k++
		g.globalReg[gi] = r
		g.reserved[r] = true
		g.pinnedGlob = append(g.pinnedGlob, pinnedGlobal{glob: gi, reg: r, wide: wide})
	}
}

// loadGlobalCellPtr loads the address of global gi's value cell into dst.
func (g *cg) loadGlobalCellPtr(dst Reg, gi int) {
	g.a.Load64(dst, RBP, -16)                          // saved linMem pointer
	g.a.Load64(dst, dst, -int32(abi.GlobalsPtrOffset)) // globals pointer table
	g.a.Load64(dst, dst, int32(gi*8))                  // cell pointer
}

// globalRegOf returns the pinned register for global x, or regNone.
func (g *cg) globalRegOf(x uint32) Reg {
	if int(x) < len(g.globalReg) {
		return g.globalReg[x]
	}
	return regNone
}

// materializeGlobalRefs captures pending pinned reads of global gi to their
// canonical slots before global.set overwrites the register (lazy-aliasing).
func (g *cg) materializeGlobalRefs(gi int) {
	for i := range g.st {
		if g.st[i].kind == vPinned && g.st[i].glob == gi {
			g.a.Store64(RBP, g.slotOff(i), g.st[i].reg)
			g.st[i] = ventry{kind: vSpill, slot: i}
		}
	}
}

// writeBackGlobals stores every pinned global back to its shared cell, using
// `tmp` for the cell pointer. Called before calls and at function return so the
// authoritative register value is visible to callees, the host, and exports.
func (g *cg) writeBackGlobals(tmp Reg) {
	for _, pg := range g.pinnedGlob {
		g.loadGlobalCellPtr(tmp, pg.glob)
		if pg.wide {
			g.a.Store64(tmp, 0, pg.reg)
		} else {
			g.a.Store32(tmp, 0, pg.reg)
		}
	}
}

// reloadGlobals reloads every pinned global from its cell (after a call, which
// may have written it), using `tmp` for the cell pointer.
func (g *cg) reloadGlobals(tmp Reg) {
	for _, pg := range g.pinnedGlob {
		g.loadGlobalCellPtr(tmp, pg.glob)
		if pg.wide {
			g.a.Load64(pg.reg, tmp, 0)
		} else {
			g.a.Load32(pg.reg, tmp, 0)
		}
	}
}

// signExtend sign-extends the low `bits` (8/16/32) of the top-of-stack integer.
// wide selects an i64 result; otherwise the result is an i32 (upper bits zeroed).
func (g *cg) signExtend(bits int, wide bool) {
	a := g.pop()
	if a.kind == vConst && !a.fp {
		var v int64
		switch bits {
		case 8:
			v = int64(int8(a.cval))
		case 16:
			v = int64(int16(a.cval))
		default: // 32
			v = int64(int32(a.cval))
		}
		g.push(ventry{kind: vConst, wide: wide, cval: v})
		return
	}
	dst := g.materialize(a)
	switch bits {
	case 8:
		g.a.Movsx8(dst, dst, wide)
	case 16:
		g.a.Movsx16(dst, dst, wide)
	default: // 32
		g.a.Movsxd(dst, dst)
	}
	g.pushReg(dst)
}

// emitPlain lowers non-control opcodes while the current path is reachable.
func (g *cg) emitPlain(r *wasm.Reader, op byte) error {
	switch {
	case op == 0x01: // nop
	case op == 0x10: // call
		return g.callOp(r)
	case op == 0x11: // call_indirect
		return g.callIndirect(r)
	case op == 0x28: // i32.load
		return g.memLoad(r, 4, false, false)
	case op == 0x2C: // i32.load8_s
		return g.memLoad(r, 1, true, false)
	case op == 0x2D: // i32.load8_u
		return g.memLoad(r, 1, false, false)
	case op == 0x2E: // i32.load16_s
		return g.memLoad(r, 2, true, false)
	case op == 0x2F: // i32.load16_u
		return g.memLoad(r, 2, false, false)
	case op == 0x36: // i32.store
		return g.memStore(r, 4)
	case op == 0x3A: // i32.store8
		return g.memStore(r, 1)
	case op == 0x3B: // i32.store16
		return g.memStore(r, 2)
	case op == 0x1A: // drop
		e := g.pop()
		if e.kind == vReg {
			if e.fp {
				g.freeFReg(e.reg)
			} else {
				g.freeReg(e.reg)
			}
		}
	case op == 0x1B: // select
		g.selectOp(false, false)
	case op == 0x1C: // select t
		n, err := r.U32()
		if err != nil {
			return err
		}
		isF := false
		for i := uint32(0); i < n; i++ {
			t, err := r.Byte()
			if err != nil {
				return err
			}
			if t == 0x7D || t == 0x7C { // f32 / f64
				isF = true
			}
		}
		g.selectOp(true, isF)
	case op == 0x20: // local.get
		x, err := r.U32()
		if err != nil {
			return err
		}
		if _, ok := g.localType(int(x)); !ok {
			return fmt.Errorf("amd64: unknown local %d", x)
		}
		if pr := g.localReg[x]; pr != regNone {
			g.push(ventry{kind: vPinned, reg: pr, local: int(x), glob: -1})
		} else {
			g.push(ventry{kind: vLocal, local: int(x), fp: g.isFloatLocal(int(x))})
		}
	case op == 0x23: // global.get
		return g.globalGet(r)
	case op == 0x24: // global.set
		return g.globalSet(r)
	case op == 0x21, op == 0x22: // local.set / local.tee
		x, err := r.U32()
		if err != nil {
			return err
		}
		if _, ok := g.localType(int(x)); !ok {
			return fmt.Errorf("amd64: unknown local %d", x)
		}
		// Peephole: `local.set x; local.get x` is exactly `local.tee x`
		// (pop v, store v to x, push v). Fusing keeps v live in its register
		// instead of storing it and immediately reloading the slot.
		tee := op == 0x22
		if !tee {
			if nb, ok := r.Peek(); ok && nb == 0x20 { // local.get
				save := r.Offset()
				_, _ = r.Byte() // the local.get opcode we peeked
				y, err := r.U32()
				if err != nil {
					return err
				}
				if y == x {
					tee = true
				} else if err := r.JumpTo(save); err != nil { // different local: rewind
					return err
				}
			}
		}
		e := g.pop()
		g.materializeLocalRefs(int(x))
		if pr := g.localReg[x]; pr != regNone {
			// Pinned integer local: write the value straight into its register.
			g.loadInto(pr, e)
			if tee {
				g.push(ventry{kind: vPinned, reg: pr, local: int(x), glob: -1})
			}
		} else if g.isFloatLocal(int(x)) {
			xmm := g.materializeF(e)
			g.a.FStoreDisp(RBP, g.localOff(int(x)), xmm, true)
			if tee {
				g.pushFReg(xmm)
			} else {
				g.freeFReg(xmm)
			}
		} else {
			rg := g.materialize(e)
			g.a.Store64(RBP, g.localOff(int(x)), rg)
			if tee {
				g.pushReg(rg)
			} else {
				g.freeReg(rg)
			}
		}
	case op == 0x41: // i32.const
		v, err := r.I32()
		if err != nil {
			return err
		}
		g.push(ventry{kind: vConst, cval: int64(v)})
	case op == 0x42: // i64.const
		v, err := r.I64()
		if err != nil {
			return err
		}
		g.push(ventry{kind: vConst, cval: v, wide: true})

	case op == 0x6A:
		g.binALU(opAdd, false)
	case op == 0x6B:
		g.binALU(opSub, false)
	case op == 0x6C:
		g.mul(false)
	case op == 0x6D:
		g.divRem(true, false, false)
	case op == 0x6E:
		g.divRem(false, false, false)
	case op == 0x6F:
		g.divRem(true, true, false)
	case op == 0x70:
		g.divRem(false, true, false)
	case op == 0x71:
		g.binALU(opAnd, false)
	case op == 0x72:
		g.binALU(opOr, false)
	case op == 0x73:
		g.binALU(opXor, false)
	case op == 0x74:
		g.shift(4, false)
	case op == 0x75:
		g.shift(7, false)
	case op == 0x76:
		g.shift(5, false)
	case op == 0x45:
		return g.eqzFused(r, false)
	case op == 0x67:
		g.intUnary(false, g.a.Lzcnt, uClz) // i32.clz
	case op == 0x68:
		g.intUnary(false, g.a.Tzcnt, uCtz) // i32.ctz
	case op == 0x69:
		g.intUnary(false, g.a.Popcnt, uPopcnt) // i32.popcnt
	case op == 0x77:
		g.shift(0, false) // i32.rotl
	case op == 0x78:
		g.shift(1, false) // i32.rotr
	case i32cmp[op] != 0:
		return g.cmpFused(r, i32cmp[op], false)

	case op == 0x7C:
		g.binALU(opAdd, true)
	case op == 0x7D:
		g.binALU(opSub, true)
	case op == 0x7E:
		g.mul(true)
	case op == 0x7F:
		g.divRem(true, false, true)
	case op == 0x80:
		g.divRem(false, false, true)
	case op == 0x81:
		g.divRem(true, true, true)
	case op == 0x82:
		g.divRem(false, true, true)
	case op == 0x83:
		g.binALU(opAnd, true)
	case op == 0x84:
		g.binALU(opOr, true)
	case op == 0x85:
		g.binALU(opXor, true)
	case op == 0x86:
		g.shift(4, true)
	case op == 0x87:
		g.shift(7, true)
	case op == 0x88:
		g.shift(5, true)
	case op == 0x50:
		return g.eqzFused(r, true)
	case op == 0x79:
		g.intUnary(true, g.a.Lzcnt, uClz) // i64.clz
	case op == 0x7A:
		g.intUnary(true, g.a.Tzcnt, uCtz) // i64.ctz
	case op == 0x7B:
		g.intUnary(true, g.a.Popcnt, uPopcnt) // i64.popcnt
	case op == 0x89:
		g.shift(0, true) // i64.rotl
	case op == 0x8A:
		g.shift(1, true) // i64.rotr
	case i64cmp[op] != 0:
		return g.cmpFused(r, i64cmp[op], true)

	case op == 0x29: // i64.load
		return g.memLoad(r, 8, false, true)
	case op == 0x30: // i64.load8_s
		return g.memLoad(r, 1, true, true)
	case op == 0x31: // i64.load8_u
		return g.memLoad(r, 1, false, true)
	case op == 0x32: // i64.load16_s
		return g.memLoad(r, 2, true, true)
	case op == 0x33: // i64.load16_u
		return g.memLoad(r, 2, false, true)
	case op == 0x34: // i64.load32_s
		return g.memLoad(r, 4, true, true)
	case op == 0x35: // i64.load32_u
		return g.memLoad(r, 4, false, true)
	case op == 0x37: // i64.store
		return g.memStore(r, 8)
	case op == 0x3C: // i64.store8
		return g.memStore(r, 1)
	case op == 0x3D: // i64.store16
		return g.memStore(r, 2)
	case op == 0x3E: // i64.store32
		return g.memStore(r, 4)

	case op == 0xA7: // i32.wrap_i64: keep low 32, zero-extend
		a := g.pop()
		if a.kind == vConst && !a.fp {
			g.push(ventry{kind: vConst, cval: int64(int32(uint32(a.cval)))})
		} else {
			dst := g.materialize(a)
			g.a.MovRegReg32(dst, dst)
			g.pushReg(dst)
		}
	case op == 0xAC: // i64.extend_i32_s
		a := g.pop()
		if a.kind == vConst && !a.fp {
			g.push(ventry{kind: vConst, wide: true, cval: int64(int32(uint32(a.cval)))})
		} else {
			dst := g.materialize(a)
			g.a.Movsxd(dst, dst)
			g.pushReg(dst)
		}
	case op == 0xAD: // i64.extend_i32_u: i32 is already zero-extended
		a := g.pop()
		if a.kind == vConst && !a.fp {
			g.push(ventry{kind: vConst, wide: true, cval: int64(uint32(a.cval))})
		} else {
			g.pushReg(g.materialize(a))
		}
	case op == 0xC0: // i32.extend8_s
		g.signExtend(8, false)
	case op == 0xC1: // i32.extend16_s
		g.signExtend(16, false)
	case op == 0xC2: // i64.extend8_s
		g.signExtend(8, true)
	case op == 0xC3: // i64.extend16_s
		g.signExtend(16, true)
	case op == 0xC4: // i64.extend32_s
		g.signExtend(32, true)

	case op == 0x43: // f32.const
		b, err := r.Bytes(4)
		if err != nil {
			return err
		}
		g.push(ventry{kind: vConst, fp: true, cval: int64(binary.LittleEndian.Uint32(b))})
	case op == 0x44: // f64.const
		b, err := r.Bytes(8)
		if err != nil {
			return err
		}
		g.push(ventry{kind: vConst, fp: true, wide: true, cval: int64(binary.LittleEndian.Uint64(b))})
	case op == 0x2A: // f32.load
		return g.fload(r, false)
	case op == 0x2B: // f64.load
		return g.fload(r, true)
	case op == 0x38: // f32.store
		return g.fstore(r, false)
	case op == 0x39: // f64.store
		return g.fstore(r, true)

	case op == 0x8B:
		g.fabs(false)
	case op == 0x8C:
		g.fneg(false)
	case op == 0x8D:
		g.fround(false, roundCeil)
	case op == 0x8E:
		g.fround(false, roundFloor)
	case op == 0x8F:
		g.fround(false, roundTrunc)
	case op == 0x90:
		g.fround(false, roundNearest)
	case op == 0x91:
		g.fsqrt(false)
	case op == 0x92:
		g.fbin(g.a.FAdd, false, fAddK)
	case op == 0x93:
		g.fbin(g.a.FSub, false, fSubK)
	case op == 0x94:
		g.fbin(g.a.FMul, false, fMulK)
	case op == 0x95:
		g.fbin(g.a.FDiv, false, fDivK)
	case op == 0x96:
		g.fbin(g.a.FMin, false, fMinK)
	case op == 0x97:
		g.fbin(g.a.FMax, false, fMaxK)
	case op == 0x98:
		g.fcopysign(false)

	case op == 0x99:
		g.fabs(true)
	case op == 0x9A:
		g.fneg(true)
	case op == 0x9B:
		g.fround(true, roundCeil)
	case op == 0x9C:
		g.fround(true, roundFloor)
	case op == 0x9D:
		g.fround(true, roundTrunc)
	case op == 0x9E:
		g.fround(true, roundNearest)
	case op == 0x9F:
		g.fsqrt(true)
	case op == 0xA0:
		g.fbin(g.a.FAdd, true, fAddK)
	case op == 0xA1:
		g.fbin(g.a.FSub, true, fSubK)
	case op == 0xA2:
		g.fbin(g.a.FMul, true, fMulK)
	case op == 0xA3:
		g.fbin(g.a.FDiv, true, fDivK)
	case op == 0xA4:
		g.fbin(g.a.FMin, true, fMinK)
	case op == 0xA5:
		g.fbin(g.a.FMax, true, fMaxK)
	case op == 0xA6:
		g.fcopysign(true)

	case isF32Cmp(op):
		g.fcmp(fcmpKinds[op], false)
	case op >= 0x61 && op <= 0x66:
		g.fcmp(fcmpKinds[op], true)

	case op == 0xA8: // i32.trunc_f32_s
		g.f2iTrunc(false, false)
	case op == 0xA9: // i32.trunc_f32_u (via 64-bit result)
		g.f2iTrunc(false, true)
	case op == 0xAA: // i32.trunc_f64_s
		g.f2iTrunc(true, false)
	case op == 0xAB: // i32.trunc_f64_u
		g.f2iTrunc(true, true)
	case op == 0xAE, op == 0xAF: // i64.trunc_f32_s/u
		g.f2iTrunc(false, true)
	case op == 0xB0, op == 0xB1: // i64.trunc_f64_s/u
		g.f2iTrunc(true, true)
	case op == 0xB2: // f32.convert_i32_s
		g.i2f(false, false)
	case op == 0xB3: // f32.convert_i32_u (zero-extended i32 as i64)
		g.i2f(false, true)
	case op == 0xB4, op == 0xB5: // f32.convert_i64_s/u
		g.i2f(false, true)
	case op == 0xB6: // f32.demote_f64
		g.fdemote()
	case op == 0xB7: // f64.convert_i32_s
		g.i2f(true, false)
	case op == 0xB8: // f64.convert_i32_u
		g.i2f(true, true)
	case op == 0xB9, op == 0xBA: // f64.convert_i64_s/u
		g.i2f(true, true)
	case op == 0xBB: // f64.promote_f32
		g.fpromote()
	case op == 0xBC: // i32.reinterpret_f32
		g.reinterpretFloatToInt(false)
	case op == 0xBD: // i64.reinterpret_f64
		g.reinterpretFloatToInt(true)
	case op == 0xBE: // f32.reinterpret_i32
		g.reinterpretIntToFloat(false)
	case op == 0xBF: // f64.reinterpret_i64
		g.reinterpretIntToFloat(true)

	case op == 0xFC: // misc (bulk memory, saturating truncation)
		sub, err := r.U32()
		if err != nil {
			return err
		}
		switch sub {
		case 0: // i32.trunc_sat_f32_s
			g.truncSat(false, false, true)
		case 1: // i32.trunc_sat_f32_u
			g.truncSat(false, false, false)
		case 2: // i32.trunc_sat_f64_s
			g.truncSat(true, false, true)
		case 3: // i32.trunc_sat_f64_u
			g.truncSat(true, false, false)
		case 4: // i64.trunc_sat_f32_s
			g.truncSat(false, true, true)
		case 5: // i64.trunc_sat_f32_u
			g.truncSat(false, true, false)
		case 6: // i64.trunc_sat_f64_s
			g.truncSat(true, true, true)
		case 7: // i64.trunc_sat_f64_u
			g.truncSat(true, true, false)
		case 10:
			return g.memoryCopy(r)
		case 11:
			return g.memoryFill(r)
		default:
			return fmt.Errorf("amd64: unsupported 0xFC subopcode %d", sub)
		}

	default:
		return &Unsupported{Op: op}
	}
	return nil
}

var i64cmp = map[byte]Cond{
	0x51: CondE, 0x52: CondNE,
	0x53: CondL, 0x54: CondB, 0x55: CondG, 0x56: CondA,
	0x57: CondLE, 0x58: CondBE, 0x59: CondGE, 0x5A: CondAE,
}
