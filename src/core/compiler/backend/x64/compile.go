package x64

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/wago-org/wago/src/core/compiler/backend/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// fn holds the per-function code-generation state — the port's equivalent of
// WARP's Compiler/backend working set. One is created per compiled function.
type fn struct {
	a  *amd64.Asm // the (reused) x86-64 encoder
	s  *stack     // the valent-block operand stack
	m  *wasm.Module
	ft *wasm.CompType // this function's signature

	nParams   int
	nLocals   int           // params + declared locals
	localType []machineType // per-local machine type

	// Register-pinned locals (the WARP recoverLocalToReg win): localReg[i] is the
	// dedicated register caching local i, or regNone if i is frame-resident. Pinned
	// registers are excluded from the general allocation pool (pinnedLocalMask).
	localReg        []Reg
	pinnedLocalMask regMask

	// Register occupancy: regUser[r] is the value elem currently resident in
	// physical register r, or nil if r is free. Only allocatable GPRs are tracked.
	regUser [16]*elem
	// pinned[r] marks a register temporarily protected from spilling/allocation
	// (e.g. an operand being consumed by the current op).
	pinned regMask

	// Parallel XMM-register occupancy for float values (Phase 5).
	fregUser [16]*elem
	fpinned  regMask

	maxSpill  int  // high-water number of operand spill slots used
	subRspAt  int  // byte offset of the prologue's SubRsp imm32 (patched with frameSize)
	guardMode bool // elide inline bounds checks; rely on guard-page + SIGSEGV trap

	// Control-flow state (Phase 3).
	ctrl        []ctrlFrame // open block/loop/if frames; ctrl[0] is the function frame
	unreachable bool        // in dead code after an unconditional branch/trap
	retSites    []int       // forward jmp sites that target the epilogue

	// Call state (Phase 4).
	relocs []callReloc // CallRel32 sites to patch at module layout
}

func align16(n int) int { return (n + 15) &^ 15 }

// Frame layout (RBP-relative), matching wago's runtime ABI so the trampoline and
// param/result marshaling are unchanged:
//
//	[-8] spare · [-16] saved linMem · [-24] trap ptr · [-32] results ptr
//	locals at localOff(i) = -(40 + 8*i) · spill slots after locals.
func (f *fn) localOff(i int) int32 { return -int32(40 + 8*i) }
func (f *fn) spillOff(k int) int32 { return -int32(40 + 8*f.nLocals + 8*k) }
func (f *fn) frameSize() int {
	sz := 40 + 8*f.nLocals + 8*f.maxSpill
	return (sz + 15) &^ 15
}

// CompileModule compiles every local function into one executable blob with
// per-function entry offsets — the same shape backend/amd64 produces, so
// src/wago consumes it unchanged. Phase 0: straight-line integer functions.
func CompileModule(m *wasm.Module) (*amd64.CompiledModule, error) {
	n := len(m.Code)
	codes := make([][]byte, n)
	relocs := make([][]callReloc, n)
	entry := make([]int, n)
	var code []byte
	for i := range m.Code {
		fnCode, rl, err := compileFunc(m, i)
		if err != nil {
			return nil, fmt.Errorf("x64: function %d: %w", i, err)
		}
		// 16-byte align each function.
		if pad := (16 - len(code)%16) % 16; pad != 0 {
			code = append(code, make([]byte, pad)...)
		}
		entry[i] = len(code)
		codes[i], relocs[i] = fnCode, rl
		code = append(code, fnCode...)
	}
	// Patch internal-call sites now that every function's entry offset is known.
	for i := 0; i < n; i++ {
		for _, rl := range relocs[i] {
			site := entry[i] + rl.at
			target := entry[rl.target]
			binary.LittleEndian.PutUint32(code[site:], uint32(int32(target-(site+4))))
		}
	}
	return &amd64.CompiledModule{Code: code, Entry: entry}, nil
}

func compileFunc(m *wasm.Module, funcIdx int) (code []byte, relocs []callReloc, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("x64: %v", r)
		}
	}()

	ft, ok := m.LocalFuncType(funcIdx)
	if !ok {
		return nil, nil, fmt.Errorf("unknown function type")
	}
	c := &m.Code[funcIdx]
	nLocals, err := countLocals(ft.Params, c.Locals)
	if err != nil {
		return nil, nil, err
	}

	f := &fn{a: &amd64.Asm{}, s: newStack(), m: m, ft: ft, nParams: len(ft.Params), nLocals: nLocals}
	f.localType = make([]machineType, nLocals)
	i := 0
	for _, p := range ft.Params {
		f.localType[i] = mtOf(p)
		i++
	}
	for _, run := range c.Locals.Runs {
		for k := 0; k < int(run.Count); k++ {
			f.localType[i] = mtOf(run.Type)
			i++
		}
	}
	f.assignPinnedLocals(localHotness(c.Body, nLocals))

	f.prologue()
	f.ctrl = []ctrlFrame{{kind: cfFunc, resultN: len(ft.Results), branchN: len(ft.Results)}}
	if err := f.body(c.BodyBytes); err != nil {
		return nil, nil, err
	}
	for _, s := range f.retSites { // return/br-to-function targets land at the epilogue
		f.a.PatchRel32(s, f.a.Len())
	}
	f.epilogue()
	f.a.PatchU32(f.subRspAt, uint32(f.frameSize()))
	return f.a.B, f.relocs, nil
}

// assignPinnedLocals dedicates registers to the hottest integer locals (by the
// hotness scores). Locals with a zero score (no AST / unused) are ordered by
// index, so a body carrying only BodyBytes falls back to first-N pinning.
func (f *fn) assignPinnedLocals(scores []int64) {
	f.localReg = make([]Reg, f.nLocals)
	for i := range f.localReg {
		f.localReg[i] = regNone
	}
	// Candidate integer locals, ranked by score (desc), then index (asc).
	cand := make([]int, 0, f.nLocals)
	for i := 0; i < f.nLocals; i++ {
		if !f.localType[i].isFloat() {
			cand = append(cand, i)
		}
	}
	sort.SliceStable(cand, func(a, b int) bool {
		if scores[cand[a]] != scores[cand[b]] {
			return scores[cand[a]] > scores[cand[b]]
		}
		return cand[a] < cand[b]
	})
	for k := 0; k < len(cand) && k < len(pinnedLocalRegs); k++ {
		r := pinnedLocalRegs[k]
		f.localReg[cand[k]] = r
		f.pinnedLocalMask = f.pinnedLocalMask.add(r)
	}
}

// prologue: standard frame, pin linMem in RBX (moved from RSI per WARP's
// convention), stash trap/results, load params into their register or slot, zero
// declared locals.
func (f *fn) prologue() {
	a := f.a
	a.Prologue()              // push rbp; mov rbp,rsp
	f.subRspAt = len(a.B) + 3 // SubRsp opcode is 3 bytes (48 81 EC), then imm32
	a.SubRsp(0)               // frame; imm32 patched after body
	a.MovReg64(RBX, RSI)      // linMem → RBX (pinned for the whole function)
	a.Store64(RBP, -16, RSI)
	a.Store64(RBP, -24, RDX) // trap ptr
	a.Store64(RBP, -32, RCX) // results ptr
	for i := 0; i < f.nParams; i++ {
		if pr := f.localReg[i]; pr != regNone {
			a.Load64(pr, RDI, int32(8*i)) // pinned param → its register
		} else {
			a.Load64(RAX, RDI, int32(8*i))
			a.Store64(RBP, f.localOff(i), RAX)
		}
	}
	if f.nLocals > f.nParams {
		a.XorSelf32(RAX)
		for i := f.nParams; i < f.nLocals; i++ {
			if pr := f.localReg[i]; pr != regNone {
				a.XorSelf32(pr) // pinned declared local → 0
			} else {
				a.Store64(RBP, f.localOff(i), RAX)
			}
		}
	}
}

// epilogue: copy results from their canonical slots to the results buffer, clear
// the trap slot, and return. Every reaching path (fallthrough end, return, br to
// the function label) has already placed the results in slots [0, resultN).
func (f *fn) epilogue() {
	a := f.a
	a.Load64(RDI, RBP, -32) // results ptr
	for i := range f.ft.Results {
		a.Load64(RAX, RBP, f.spillOff(i))
		a.Store64(RDI, int32(8*i), RAX)
	}
	a.Load64(RSI, RBP, -24) // trap ptr
	a.StoreImm32Mem(RSI, 0, 0)
	a.Leave()
	a.Ret()
}

func mtOf(t wasm.ValType) machineType {
	switch {
	case wasm.EqualValType(t, wasm.I32):
		return mtI32
	case wasm.EqualValType(t, wasm.I64):
		return mtI64
	case wasm.EqualValType(t, wasm.F32):
		return mtF32
	case wasm.EqualValType(t, wasm.F64):
		return mtF64
	}
	return mtNone
}

func countLocals(params []wasm.ValType, locals wasm.Locals) (int, error) {
	n := len(params)
	for _, run := range locals.Runs {
		n += int(run.Count)
	}
	return n, nil
}
