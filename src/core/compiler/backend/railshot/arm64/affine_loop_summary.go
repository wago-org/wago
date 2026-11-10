//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"os"
)

var affineLoopSummaryEnabled = os.Getenv("WAGO_ARM64_NO_AFFINE_LOOP_SUMMARY") != "1"

const (
	affineLoopCells     = 8
	affineLoopDim       = affineLoopCells + 1
	affineLoopWorkLimit = 16384
)

type affineLoopForm [affineLoopDim]uint32

// These cells describe only the candidate's current source values. A get copies
// its affine form; a set replaces that form, without retaining assignment history.
type affineLoopCell struct {
	index           uint32
	global, written bool
	value           affineLoopForm
}
type affineLoopEntry struct {
	reg    Reg
	offset int32
	zero   bool
}
type affineLoopMatrix [affineLoopDim][affineLoopDim]uint32
type affineLoopPlan struct {
	cells                              [affineLoopCells]affineLoopCell
	entry                              [affineLoopCells]affineLoopEntry
	output                             [affineLoopCells][4]affineLoopForm
	state                              [affineLoopCells]int
	n, dim, counter, limit, end, depth int
	carried                            uint16
	dispatch, fast, scalar, slot       int
}

func (f *fn) chargeAffineLoop(n int) bool {
	if n > affineLoopWorkLimit-int(f.affineLoopWork) {
		f.affineLoopWork = affineLoopWorkLimit
		return false
	}
	f.affineLoopWork += uint16(n)
	return true
}

func (f *fn) affineLoopCell(p *affineLoopPlan, index uint32, global bool) int {
	for i := 0; i < p.n; i++ {
		if p.cells[i].index == index && p.cells[i].global == global {
			return i
		}
	}
	if p.n == len(p.cells) {
		return -1
	}
	if global {
		gt, ok := f.globalType(index)
		// Imported cells can alias; this first family uses only defined numeric cells.
		if !ok || int(index) < f.m.ImportedGlobalCount() || !wasm.EqualValType(wasm.GlobalValueType(gt), wasm.I32) {
			return -1
		}
	} else if uint64(index) >= uint64(f.nLocals) || f.localType[index] != mtI32 {
		return -1
	}
	i := p.n
	p.n++
	p.cells[i] = affineLoopCell{index: index, global: global}
	p.cells[i].value[i] = 1
	return i
}

func (f *fn) inspectAffineLoop(source *wasm.Reader) (p affineLoopPlan, ok bool) {
	r := *source
	start := r.Offset()
	type value struct {
		form affineLoopForm
		cell int
	}
	var stack [24]value
	var blocks [8]struct{ height, result int }
	depth, nesting := 0, 0
	p.counter, p.limit = -1, -1
	for ops := 0; ops < 128 && r.Offset()-start < 512; ops++ {
		if !f.chargeAffineLoop(1) {
			return p, false
		}
		op, err := r.Byte()
		if err != nil {
			return p, false
		}
		switch op {
		case 0x01:
		case 0x02:
			bt, e := r.Byte()
			if e != nil || nesting == len(blocks) || bt != 0x40 && bt != 0x7f {
				return p, false
			}
			result := 0
			if bt == 0x7f {
				result = 1
			}
			blocks[nesting].height, blocks[nesting].result = depth, result
			nesting++
		case 0x0b:
			if nesting == 0 {
				return p, false
			}
			nesting--
			if depth != blocks[nesting].height+blocks[nesting].result {
				return p, false
			}
		case 0x20, 0x21, 0x22, 0x23, 0x24:
			index, e := r.U32()
			if e != nil {
				return p, false
			}
			cell := f.affineLoopCell(&p, index, op == 0x23 || op == 0x24)
			if cell < 0 {
				return p, false
			}
			if op == 0x20 || op == 0x23 {
				if depth == len(stack) {
					return p, false
				}
				stack[depth] = value{p.cells[cell].value, cell}
				depth++
			} else {
				if depth == 0 {
					return p, false
				}
				if op == 0x24 {
					gt, _ := f.globalType(index)
					if !gt.Mutable {
						return p, false
					}
				}
				p.cells[cell].value = stack[depth-1].form
				p.cells[cell].written = true
				if op == 0x22 {
					stack[depth-1].cell = cell
				} else {
					depth--
				}
			}
		case 0x41:
			c, e := r.I32()
			if e != nil || depth == len(stack) {
				return p, false
			}
			stack[depth] = value{cell: -1}
			stack[depth].form[affineLoopCells] = uint32(c)
			depth++
		case 0x1a:
			if depth == 0 {
				return p, false
			}
			depth--
		case 0x6a, 0x6b, 0x6c:
			if depth < 2 || !f.chargeAffineLoop(affineLoopDim) {
				return p, false
			}
			a, b := stack[depth-2].form, stack[depth-1].form
			if op == 0x6c {
				ac, bc := true, true
				for i := 0; i < affineLoopCells; i++ {
					ac = ac && a[i] == 0
					bc = bc && b[i] == 0
				}
				if !ac && !bc {
					return p, false
				}
				if ac {
					a, b = b, a
				}
				for i := range a {
					a[i] *= b[affineLoopCells]
				}
			} else {
				for i := range a {
					if op == 0x6a {
						a[i] += b[i]
					} else {
						a[i] -= b[i]
					}
				}
			}
			depth--
			stack[depth-1] = value{a, -1}
		case 0x49:
			// Only the final unsigned comparison is a termination relation. Its left
			// value must be the final counter, and its bound an unchanged local input.
			if nesting != 0 || depth != 2 {
				return p, false
			}
			a, b := stack[0], stack[1]
			if a.cell < 0 || b.cell < 0 || a.cell == b.cell {
				return p, false
			}
			c, l := p.cells[a.cell], p.cells[b.cell]
			if c.global || l.global || !c.written || l.written || a.form != c.value {
				return p, false
			}
			var step affineLoopForm
			step[a.cell] = 1
			step[affineLoopCells] = 1
			var bound affineLoopForm
			bound[b.cell] = 1
			if c.value != step || b.form != bound {
				return p, false
			}
			branch, e := r.Byte()
			if e != nil || branch != 0x0d {
				return p, false
			}
			label, e := r.U32()
			if e != nil || label != 0 {
				return p, false
			}
			end, e := r.Byte()
			if e != nil || end != 0x0b || r.Offset()-start > 512 {
				return p, false
			}
			p.counter, p.limit, p.end = a.cell, b.cell, r.Offset()
			ok = f.finishAffineLoopPlan(&p)
			return p, ok
		default:
			return p, false
		}
	}
	return p, false
}

func (f *fn) affineLoopMultiply(a, b *affineLoopMatrix, n int) (out affineLoopMatrix, ok bool) {
	if !f.chargeAffineLoop(n * n * n) {
		return out, false
	}
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			for k := 0; k < n; k++ {
				out[i][j] += a[i][k] * b[k][j]
			}
		}
	}
	return out, true
}

func (f *fn) finishAffineLoopPlan(p *affineLoopPlan) bool {
	var needed [affineLoopCells]bool
	needed[p.counter], needed[p.limit] = true, true
	for i := 0; i < p.n; i++ {
		if p.cells[i].written {
			for j := 0; j < p.n; j++ {
				needed[j] = needed[j] || p.cells[i].value[j] != 0
			}
		}
	}
	real := 0
	for i := 0; i < p.n; i++ {
		p.state[i] = -1
		if needed[i] {
			p.state[i] = real
			real++
		}
	}
	p.dim = real + 1
	var d affineLoopMatrix
	for i := 0; i < p.n; i++ {
		row := p.state[i]
		if row < 0 {
			continue
		}
		if p.cells[i].written {
			p.carried |= 1 << uint(i)
			for j := 0; j < p.n; j++ {
				if col := p.state[j]; col >= 0 {
					d[row][col] = p.cells[i].value[j]
				}
			}
			d[row][real] = p.cells[i].value[affineLoopCells]
			d[row][row]--
		}
	}
	d2, ok := f.affineLoopMultiply(&d, &d, p.dim)
	if !ok {
		return false
	}
	d3, ok := f.affineLoopMultiply(&d2, &d, p.dim)
	if !ok {
		return false
	}
	d4, ok := f.affineLoopMultiply(&d3, &d, p.dim)
	if !ok {
		return false
	}
	for i := 0; i < p.dim; i++ {
		for j := 0; j < p.dim; j++ {
			if d4[i][j] != 0 {
				return false
			}
		}
	}
	powers := [3]*affineLoopMatrix{&d, &d2, &d3}
	for i := 0; i < p.n; i++ {
		if !p.cells[i].written {
			continue
		}
		if row := p.state[i]; row >= 0 {
			p.output[i][0][row] = 1
			for k, power := range powers {
				p.output[i][k+1] = power[row]
			}
		} else {
			// Overwritten per-iteration temporaries publish their last iteration's
			// value, rather than preserving an entry value which the source discarded.
			var form affineLoopForm
			for j := 0; j < p.n; j++ {
				if col := p.state[j]; col >= 0 {
					form[col] = p.cells[i].value[j]
				}
			}
			form[real] = p.cells[i].value[affineLoopCells]
			p.output[i][0] = form
			if !f.chargeAffineLoop(3 * p.dim * p.dim) {
				return false
			}
			for k, power := range powers {
				for j := 0; j < p.dim; j++ {
					for col := 0; col < p.dim; col++ {
						p.output[i][k+1][col] += form[j] * power[j][col]
					}
				}
			}
		}
	}
	return true
}

func (f *fn) readAffineEntry(e affineLoopEntry, out Reg) {
	if e.zero {
		f.a.MovImm32(out, 0)
	} else if e.reg != regNone {
		f.a.MovReg32(out, e.reg)
	} else {
		f.ld32(out, SP, e.offset)
	}
}

// Keep ordinary emission of the source loop as the short-trip alternative.
// The reusable workspace is allocated only after a complete bounded proof.
func (f *fn) beginAffineLoopSummary(source *wasm.Reader) {
	if !affineLoopSummaryEnabled || f.affineLoop != nil || profileEnabled || f.unreachable || f.usesCalls || f.interruptible || f.moduleEH || f.preserveCallerPins || f.localBase != 0 || f.inlineDepth != 0 || f.nLocals > 64 || f.depth() != 0 || f.threadedMemory0 || f.tracksGCFrameRoots() || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.affineLoopWork >= affineLoopWorkLimit {
		return
	}
	// Keep the common miss outside the larger candidate frame.
	r := *source
	op, e := r.Byte()
	if e != nil || op != 0x20 && op != 0x23 {
		return
	}
	index, e := r.U32()
	if e != nil {
		return
	}
	if op == 0x20 {
		if int(index) >= f.nLocals || f.localType[index] != mtI32 {
			return
		}
	} else {
		gt, ok := f.globalType(index)
		if !ok || !wasm.EqualValType(wasm.GlobalValueType(gt), wasm.I32) {
			return
		}
	}
	f.beginAffineLoopCandidate(source)
}

func (f *fn) beginAffineLoopCandidate(source *wasm.Reader) {
	p, ok := f.inspectAffineLoop(source)
	if !ok {
		return
	}
	free := 0
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, r := range gpAlloc {
		if !blocked.has(r) && f.regUser[r] == nil {
			free++
		}
	}
	if free < 6 {
		return
	}
	for i := 0; i < p.n; i++ {
		c := p.cells[i]
		entry := affineLoopEntry{reg: regNone}
		if !c.global {
			entry.zero = f.localConstZero(int(c.index))
			entry.offset = f.localOff(int(c.index))
			if reg, _, pin := f.pinReg(int(c.index)); pin && !entry.zero {
				f.recoverLocal(int(c.index))
				entry.reg = reg
			}
		}
		p.entry[i] = entry
	}
	sc := f.scratchState()
	if sc.affineLoopScratch == nil {
		sc.affineLoopScratch = new(affineLoopPlan)
	}
	*sc.affineLoopScratch = p
	active := sc.affineLoopScratch
	f.affineLoop = active
	active.depth = len(f.ctrl) + 1
	active.slot = f.allocSpillSlots(active.dim)
	active.dispatch = f.a.Branch()
	// Branch patching fills zero displacement fields once; qualify both
	// dispatch placeholders only after the original scalar loop is emitted.
	var scalar [2]int
	n := 0
	counter, limit := active.entry[active.counter], active.entry[active.limit]
	if counter.zero {
		f.readAffineEntry(limit, X16)
		f.cmpImm(X16, 16, false)
		scalar[n] = f.a.Bcond(condB)
		n++
	} else {
		f.readAffineEntry(counter, X16)
		f.readAffineEntry(limit, X17)
		f.a.AddImm32(X16, X16, 1)
		f.cmpRR(X16, X17, false)
		scalar[n] = f.a.Bcond(condAE)
		n++
		f.a.Sub64(X16, X17, X16)
		f.a.AddImm64(X16, X16, 1)
		f.cmpImm(X16, 16, true)
		scalar[n] = f.a.Bcond(condB)
		n++
	}
	f.st64(SP, f.spillOff(active.slot), X16)
	active.fast = f.a.Branch()
	active.scalar = f.a.Len()
	for _, site := range scalar[:n] {
		f.patchBranch19(site, active.scalar)
	}
}

func (f *fn) finishAffineLoopSummary(depth int) {
	p := f.affineLoop
	if p == nil || p.depth != depth {
		return
	}
	f.affineLoop = nil
	f.invalidateGlobalsCache()
	// Publish the scalar alternative into homes before joining the fast recipe.
	for i := 0; i < p.n; i++ {
		c := p.cells[i]
		if c.global || !c.written {
			continue
		}
		x := int(c.index)
		if reg, _, pin := f.pinReg(x); pin {
			f.recoverLocal(x)
			f.storeLocalReg(x, reg, false)
			f.locals[x].state = lsStackReg
		}
	}
	oldPins := f.pinned
	var gp [5]Reg
	n := 0
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, r := range gpAlloc {
		if n < len(gp) && !blocked.has(r) && f.regUser[r] == nil {
			gp[n] = r
			n++
		}
	}
	if n != len(gp) {
		f.patchBranch26(p.dispatch, p.scalar)
		f.patchBranch26(p.fast, p.scalar)
		f.stats.peep("affine-loop-summary-late-reject")
		return
	}
	done := f.a.Branch()
	f.patchBranch26(p.dispatch, p.dispatch+4)
	f.patchBranch26(p.fast, f.a.Len())
	for _, r := range gp {
		f.pinned = f.pinned.add(r)
	}
	defer func() {
		f.invalidateGlobalsCache()
		f.pinned = oldPins
		for _, r := range gp {
			f.release(r)
		}
	}()
	count, c2, c3, out, dot := gp[0], gp[1], gp[2], gp[3], gp[4]
	f.ld64(count, SP, f.spillOff(p.slot))
	// Capture every needed entry before publishing any changed source cell.
	for i := 0; i < p.n; i++ {
		col := p.state[i]
		if col < 0 {
			continue
		}
		c := p.cells[i]
		if c.global {
			if reg, yes := f.pinnedGlobalValueReg(c.index); yes {
				f.a.MovReg32(X16, reg)
			} else {
				f.ld64(X16, linMemReg, -int32(abi.GlobalsPtrOffset))
				f.ld64(X16, X16, int32(c.index*8))
				f.ld32(X16, X16, 0)
			}
		} else {
			f.readAffineEntry(p.entry[i], X16)
		}
		f.st64(SP, f.spillOff(p.slot+1+col), X16)
	}
	f.a.SubImm64(c2, count, 1)
	f.a.Mul64(c2, count, c2)
	f.shiftImm(shLSR, c2, 1, true)
	f.a.SubImm32(c3, count, 2)
	f.a.Mul32(c3, c2, c3)
	f.a.MovImm32(X16, -1431655765)
	f.a.Mul32(c3, c3, X16)
	emitDot := func(form affineLoopForm, target Reg) {
		f.a.MovImm32(target, int32(form[p.dim-1]))
		for col := 0; col < p.dim-1; col++ {
			coefficient := form[col]
			if coefficient == 0 {
				continue
			}
			f.ld32(X16, SP, f.spillOff(p.slot+1+col))
			if coefficient == 1 {
				f.a.Add32(target, target, X16)
			} else if coefficient == ^uint32(0) {
				f.a.Sub32(target, target, X16)
			} else {
				f.a.MovImm32(X17, int32(coefficient))
				f.a.Madd32(target, X16, X17, target)
			}
		}
	}
	// Carried values use M^N. Other written cells use their one-iteration recipe
	// over M^(N-1), retaining all last-iteration locals and global values.
	for phase := 0; phase < 2; phase++ {
		if phase == 1 {
			f.a.SubImm64(count, count, 1)
			f.a.Sub32(c2, c2, count)
			f.a.Sub32(c3, c3, c2)
		}
		for i := 0; i < p.n; i++ {
			c := p.cells[i]
			if !c.written || (p.carried&(1<<uint(i)) != 0) != (phase == 0) {
				continue
			}
			emitDot(p.output[i][0], out)
			for k, power := range []Reg{count, c2, c3} {
				form := p.output[i][k+1]
				zero := true
				for _, v := range form {
					zero = zero && v == 0
				}
				if !zero {
					emitDot(form, dot)
					f.a.Madd32(out, dot, power, out)
				}
			}
			f.pushReg(out, mtI32).st.setValueFacts(factUpper32Zero)
			if c.global {
				if err := f.globalSetIndex(c.index); err != nil {
					panic(err)
				}
			} else {
				if _, _, pin := f.pinReg(int(c.index)); !pin {
					f.stats.peep("affine-loop-unpinned-output")
				}
				f.setLocal(nil, int(c.index), false)
				if reg, _, pin := f.pinReg(int(c.index)); pin {
					f.storeLocalReg(int(c.index), reg, false)
					f.locals[c.index].state = lsStackReg
				}
			}
			f.release(out)
		}
	}
	f.patchBranch26(done, f.a.Len())
	f.stats.peep("affine-loop-summary")
}
