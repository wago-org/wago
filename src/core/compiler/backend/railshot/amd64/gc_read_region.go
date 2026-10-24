//go:build amd64 && (!tinygo || !wago_lean)

package amd64

import (
	"fmt"
	"math"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const gcReadRegionsAvailable = true

// A region is a complete prevalidated, bounded function body. Every GC group
// selects its physical native emitter here, before any caller state is changed.
// Original bytes and offsets are retained for source attribution.
type gcReadRegionPlan struct {
	cacheHome bool
	params    []wasm.ValType
	result    wasm.ValType
	groups    map[int]gcReadRegionGroup
	numeric   map[int]gcReadRegionNumeric
}
type gcReadRegionGroup struct {
	end, getPC, indexPC          int
	typ, offset, required        uint32
	scalar                       directGCScalar
	sourceLocal                  int
	path                         [2]gcReadRegionArm
	pathDepth                    int
	cache                        bool
	array, nullable, ref, signed bool
}
type gcReadRegionNumeric struct {
	target uint32
	op     byte
}
type gcReadRegionControl struct{ entry, then, hasElse bool }

type gcReadRegionArm struct {
	pc        int
	otherwise bool
}
type gcReadRegionProof struct {
	gcResolvedObject
	inLocal bool
}
type gcReadRegionState struct {
	plan    *gcReadRegionPlan
	base    int
	lease   Reg
	proof   gcReadRegionProof
	entries [2]gcReadRegionProof
	depth   int
	err     error
}

func gcReadRegionInteger(op byte) bool {
	return op >= 0x45 && op <= 0x5a || op >= 0x67 && op <= 0x6c ||
		op >= 0x71 && op <= 0x7e || op >= 0x83 && op <= 0x8a ||
		op == 0xa7 || op == 0xac || op == 0xad || op >= 0xc0 && op <= 0xc4
}

func gcReadRegionNumericLeaf(m *wasm.Module, idx uint32) (byte, bool) {
	local := int(idx) - m.ImportedFuncCount()
	if local < 0 || local >= len(m.Code) {
		return 0, false
	}
	ft, ok := m.LocalFuncType(local)
	if !ok || len(ft.Params) != 2 || len(ft.Results) != 1 || len(m.Code[local].Locals.Runs) != 0 {
		return 0, false
	}
	r := wasm.ReaderFrom(m.Code[local].BodyBytes)
	for i := uint32(0); i < 2; i++ {
		op, e := r.Byte()
		if e != nil || op != 0x20 {
			return 0, false
		}
		n, e := r.U32()
		if e != nil || n != i {
			return 0, false
		}
	}
	op, e := r.Byte()
	if e != nil {
		return 0, false
	}
	var param, result wasm.ValType
	switch {
	case op >= 0x46 && op <= 0x4f:
		param, result = wasm.I32, wasm.I32
	case op >= 0x51 && op <= 0x5a:
		param, result = wasm.I64, wasm.I32
	case op >= 0x6a && op <= 0x6c || op >= 0x71 && op <= 0x78:
		param, result = wasm.I32, wasm.I32
	case op >= 0x7c && op <= 0x7e || op >= 0x83 && op <= 0x8a:
		param, result = wasm.I64, wasm.I64
	default:
		return 0, false
	}
	end, e := r.Byte()
	return op, e == nil && end == 0x0b && r.BytesLeft() == 0 && ft.Params[0] == param && ft.Params[1] == param && ft.Results[0] == result
}

func planGCReadRegion(m *wasm.Module, local int, layouts []codegen.GCTypeLayout, arrays bool) *gcReadRegionPlan {
	ft, ok := m.LocalFuncType(local)
	if !ok || len(ft.Params) == 0 || len(ft.Params) > 2 || len(ft.Results) != 1 || len(m.Code[local].Locals.Runs) != 0 || len(m.Code[local].BodyBytes) > 192 {
		return nil
	}
	allowed := func(t wasm.ValType) bool { return isIntValType(t) || gcFrameRefType(m, t) }
	for _, t := range ft.Params {
		if !allowed(t) {
			return nil
		}
	}
	if !allowed(ft.Results[0]) {
		return nil
	}
	p := &gcReadRegionPlan{params: ft.Params, result: ft.Results[0], groups: make(map[int]gcReadRegionGroup), numeric: make(map[int]gcReadRegionNumeric)}
	probe := fn{m: m, gcTypeLayouts: layouts}
	r := wasm.ReaderFrom(m.Code[local].BodyBytes)
	depth, previousLocal := 0, -1
	var path [2]gcReadRegionArm
	var control [2]gcReadRegionControl
	reachable := true
	for r.BytesLeft() > 0 {
		pc := r.Offset()
		op, e := r.Byte()
		if e != nil {
			return nil
		}
		sourceLocal := previousLocal
		previousLocal = -1
		switch {
		case op == 0x20:
			n, e := r.U32()
			if e != nil || int(n) >= len(ft.Params) {
				return nil
			}
			previousLocal = int(n)
		case op == 0x41:
			if _, e = r.I32(); e != nil {
				return nil
			}
		case op == 0x42:
			if _, e = r.I64(); e != nil {
				return nil
			}
		case gcReadRegionInteger(op), op == 0x01:
		case op == 0x00:
			reachable = false
		case op == 0x10:
			idx, e := r.U32()
			if e != nil {
				return nil
			}
			binary, ok := gcReadRegionNumericLeaf(m, idx)
			if !ok {
				return nil
			}
			p.numeric[pc] = gcReadRegionNumeric{target: idx, op: binary}
		case op == 0x04:
			if depth >= 2 || !gcReadRegionBlockType(m, &r) {
				return nil
			}
			path[depth] = gcReadRegionArm{pc: pc}
			control[depth] = gcReadRegionControl{entry: reachable}
			depth++
		case op == 0x05:
			if depth == 0 {
				return nil
			}
			path[depth-1].otherwise = true
			control[depth-1].then = reachable
			control[depth-1].hasElse = true
			reachable = control[depth-1].entry
		case op == 0x0b:
			if depth == 0 {
				// An all-trapping splice would make later caller source masks
				// unreachable to emission while the original plan still counts
				// them. Keep that callee as an ordinary call.
				if r.BytesLeft() == 0 && len(p.groups) > 0 && reachable {
					finishGCReadRegionPlan(p)
					return p
				}
				return nil
			}
			depth--
			if control[depth].hasElse {
				reachable = control[depth].then || reachable
			} else {
				reachable = control[depth].entry
			}
		case op == 0xfb:
			if len(p.groups) >= 8 {
				return nil
			}
			sub, e := r.U32()
			if e != nil || sub != 22 && sub != 23 {
				return nil
			}
			heap, exact, e := readRefHeapTypeImmediate(&r)
			if e != nil || exact || heap < 0 || heap > math.MaxUint32 {
				return nil
			}
			typ := uint32(heap)
			st, ok := probe.stagedGCType(typ)
			if !ok || !st.Final {
				return nil
			}
			g := gcReadRegionGroup{typ: typ, nullable: sub == 23, sourceLocal: sourceLocal, indexPC: -1, path: path, pathDepth: depth}
			if st.Comp.Kind == wasm.CompArray {
				if !arrays {
					return nil
				}
				g.array = true
				g.indexPC = r.Offset()
				producer, e := r.Byte()
				if e != nil {
					return nil
				}
				switch producer {
				case 0x20:
					n, e := r.U32()
					if e != nil || int(n) >= len(ft.Params) || ft.Params[n] != wasm.I32 {
						return nil
					}
				case 0x41:
					if _, e = r.I32(); e != nil {
						return nil
					}
				default:
					return nil
				}
			} else if st.Comp.Kind != wasm.CompStruct {
				return nil
			}
			g.getPC = r.Offset()
			op, e = r.Byte()
			if e != nil || op != 0xfb {
				return nil
			}
			get, e := r.U32()
			if e != nil {
				return nil
			}
			access, e := r.U32()
			if e != nil || access != typ {
				return nil
			}
			var storage wasm.StorageType
			if g.array {
				if get < 11 || get > 13 {
					return nil
				}
				storage = st.Comp.Array.Storage()
				g.signed = get == 12
			} else {
				if get < 2 || get > 4 {
					return nil
				}
				field, e := r.U32()
				if e != nil || int(field) >= len(st.Comp.Fields) {
					return nil
				}
				storage = st.Comp.Fields[field].Storage()
				layout, final, ok := probe.gcStructFieldLayout(typ, field)
				if !ok || !final {
					return nil
				}
				g.offset = layout.Offset
				required := uint64(gc.PayloadOffset) + uint64(layout.Offset) + uint64(layout.Size)
				if required > math.MaxInt32 {
					return nil
				}
				g.required = uint32(required)
				g.signed = get == 3
			}
			if (get == 2 || get == 11) == storage.Packed() {
				return nil
			}
			g.scalar, ok = directGCScalarStorage(storage)
			g.ref = nativeGCCollectorRefStorage(m, typ, storage)
			if g.ref {
				g.scalar, ok = directGCScalar{size: 4, typ: mtI64}, true
			}
			if !ok || g.scalar.typ.isFloat() || g.scalar.typ == mtV128 {
				return nil
			}
			if !g.array && g.required != uint32(gc.PayloadOffset)+g.offset+uint32(g.scalar.size) {
				return nil
			}
			g.end = r.Offset()
			p.groups[pc] = g
		default:
			return nil
		}
	}
	return nil
}

func gcReadRegionBlockType(m *wasm.Module, r *wasm.Reader) bool {
	b, ok := r.Peek()
	if !ok {
		return false
	}
	if b == 0x40 || b == 0x7f || b == 0x7e {
		_, _ = r.Byte()
		return true
	}
	// Only collector-reference singleton results; no block inputs or wide types.
	if isValByte(b) || b == 0x63 || b == 0x64 {
		if b != 0x63 && b != 0x64 {
			_, _ = r.Byte()
			return b == 0x6e || b == 0x6d || b == 0x6c || b == 0x6b || b == 0x6a || b == 0x71
		}
		_, _ = r.Byte()
		heap, exact, e := readRefHeapTypeImmediate(r)
		if e != nil || exact {
			return false
		}
		if heap >= 0 {
			st, ok := nativeGCFlatType(m, uint32(heap))
			return ok && (st.Comp.Kind == wasm.CompStruct || st.Comp.Kind == wasm.CompArray)
		}
		return heap == -18 || heap == -19 || heap == -20 || heap == -21 || heap == -22 || heap == -15
	}
	idx, e := r.I64()
	if e != nil || idx < 0 || idx > math.MaxUint32 {
		return false
	}
	ft, ok := m.TypeFunc(uint32(idx))
	if !ok || len(ft.Params) != 0 || len(ft.Results) > 1 {
		return false
	}
	return len(ft.Results) == 0 || isIntValType(ft.Results[0]) || gcFrameRefType(m, ft.Results[0])
}

func addGCReadRegionTargets(m *wasm.Module, ts *inlineTargetTable, layouts []codegen.GCTypeLayout, arrays bool, policy CodegenPolicy) {
	if !policy.EnabledOption(optInline) {
		return
	}
	for i := range m.Code {
		p := planGCReadRegion(m, i, layouts, arrays)
		if p == nil {
			continue
		}
		if ts.data == nil {
			*ts = inlineTargetTable{first: m.ImportedFuncCount(), data: &inlineTargetData{slots: make([]uint32, len(m.Code))}, classifier: wasm.NewModuleInstructionClassifier(m, true)}
		}
		data := ts.data
		start := len(data.types)
		for _, param := range p.params {
			data.types = append(data.types, mtOf(param))
		}
		if p.cacheHome {
			data.types = append(data.types, mtI64)
		}
		end := len(data.types)
		data.types = append(data.types, mtOf(p.result))
		data.targets = append(data.targets, inlineTarget{body: m.Code[i].BodyBytes, globalIdx: ts.first + i, localDeclBytes: m.Code[i].LocalDeclBytes, typeStart: uint32(start), localTypeEnd: uint32(end), resultTypeEnd: uint32(len(data.types)), params: uint32(len(p.params)), res0: mtOf(p.result), hasCtrl: true, readRegion: p})
		data.slots[i] = uint32(len(data.targets))
	}
	if ts.data != nil && policy.EnabledOption(optDirectForwarder) {
		for i := range m.Code {
			idx := uint32(m.ImportedFuncCount() + i)
			target, ok := directForwarderTarget(m, m.ImportedFuncCount(), idx)
			if !ok {
				continue
			}
			t := ts.target(int(target))
			if t == nil || t.readRegion == nil {
				continue
			}
			sourceFT, valid := m.LocalFuncType(i)
			targetFT, validTarget := m.LocalFuncType(int(target) - m.ImportedFuncCount())
			if !valid || !validTarget || !equalForwarderTypes(sourceFT.Params, targetFT.Params) || !equalForwarderTypes(sourceFT.Results, targetFT.Results) {
				continue
			}
			if ts.data.readForward == nil {
				ts.data.readForward = make(map[uint32]uint32)
			}
			ts.data.readForward[idx] = target
		}
	}
}

func (f *fn) gcReadRegionViolation(what string) error {
	return fmt.Errorf("amd64: native-only GC read region reached %s in function %d at %d", what, f.traceFuncIdx, f.wasmPC)
}

// Snapshot only entry proofs. Branch-created certificates never escape a join.
// One preserved register is reserved for the entire splice, even while its proof
// is invalid, so an else arm cannot let allocator state reuse its address home.
func (f *fn) prepareGCReadRegionControl(op byte) error {
	s := f.gcReadRegion
	if s == nil {
		return nil
	}
	switch op {
	case 0x04:
		if s.depth >= len(s.entries) {
			return f.gcReadRegionViolation("unplanned if")
		}
		s.entries[s.depth] = s.proof
		s.depth++
	case 0x05:
		if s.depth == 0 {
			return f.gcReadRegionViolation("unplanned else")
		}
		s.proof = s.entries[s.depth-1]
	case 0x0b:
		if s.depth > 0 {
			s.depth--
			s.proof = s.entries[s.depth]
		}
	}
	return nil
}

func (f *fn) emitGCReadRegionGroup(r *wasm.Reader) error {
	s := f.gcReadRegion
	g, ok := s.plan.groups[int(f.wasmPC-f.tracePCBase)]
	if !ok {
		return f.gcReadRegionViolation("unplanned GC operation")
	}
	if g.array {
		if err := r.JumpTo(g.indexPC); err != nil {
			return err
		}
		op, err := r.Byte()
		if err != nil {
			return err
		}
		if err = f.emitPlain(r, op); err != nil {
			return err
		}
		_ = r.JumpTo(g.end)
		f.stats.peep("gc-read-region-array")
		return f.emitNativeFinalCastArrayGet(g.typ, g.nullable, g.scalar, g.ref, g.signed)
	}
	parentID := f.gcValueID(f.s.back())
	_ = r.JumpTo(g.end)
	local := -1
	if g.sourceLocal >= 0 {
		local = s.base + g.sourceLocal
	}
	reuse := s.proof.valid && local >= 0 && s.proof.local == local && s.proof.typeIndex == g.typ && s.proof.requiredBytes >= g.required
	var address Reg
	if reuse {
		f.dropValue()
		address = s.proof.reg
		if s.proof.inLocal {
			address = f.allocReg(0)
			f.a.Load64(address, RSP, f.localAddr(s.base+len(s.plan.params)))
			f.stats.peep("gc-read-region-stack-reuse")
		}
		f.stats.peep("gc-read-region-reuse")
	} else {
		// Canonicalize only the older prefix, with the argument still linked.
		// Preserve the complete operand-slot extent until its load finishes.
		// The physical resolver's following flush sees a canonical prefix and
		// cannot overwrite the detached argument's original slot.
		oldFloor := f.spillFloor
		if err := f.stageGCReadRegionOperands(1); err != nil {
			return err
		}
		defer func() { f.spillFloor = oldFloor }()
		if err := f.emitNativeFinalCastStructFieldAddress(g.typ, g.offset, uint32(g.scalar.size), g.nullable); err != nil {
			return err
		}
		address = RAX
		f.stats.peep("gc-read-region-resolve")
		if g.cache && local >= 0 && !s.proof.valid {
			if s.lease == regNone {
				s.lease = f.gcReadRegionRegister()
				if s.lease != regNone {
					f.reserved = f.reserved.add(s.lease)
				}
			}
			if s.lease != regNone {
				f.a.MovReg64(s.lease, RAX)
				s.proof = gcReadRegionProof{gcResolvedObject: gcResolvedObject{valid: true, local: local, typeIndex: g.typ, requiredBytes: g.required, reg: s.lease}}
			} else {
				if !s.plan.cacheHome {
					return f.gcReadRegionViolation("missing private address home")
				}
				f.a.Store64(RSP, f.localAddr(s.base+len(s.plan.params)), RAX)
				s.proof = gcReadRegionProof{gcResolvedObject: gcResolvedObject{valid: true, local: local, typeIndex: g.typ, requiredBytes: g.required, reg: regNone}, inLocal: true}
				f.stats.peep("gc-read-region-stack-cache")
			}
		}
	}
	out := address
	// Fresh resolver results and reloaded private addresses are disposable.
	// Only a leased address register must survive the loaded result.
	if reuse && !s.proof.inLocal {
		out = f.allocReg(maskOf(address))
	}
	if g.ref {
		f.a.Load32(out, address, int32(gc.PayloadOffset+g.offset))
		result := f.pushReg(out, mtI64)
		f.setStackGCRoot(result, true)
		if f.gcValues != nil {
			f.setGCValueID(result, f.gcValues.fieldValue(parentID, g.typ, g.offset))
		}
	} else {
		f.a.LoadIdx(out, address, RSP, int32(gc.PayloadOffset+g.offset), g.scalar.size, g.signed, g.scalar.typ == mtI64)
		f.pushReg(out, g.scalar.typ)
	}
	return nil
}

// Expand exactly one fully checked binary leaf without consuming a second
// source-call mask. Deferred result nodes keep the callee instruction origin.
func (f *fn) emitGCReadRegionNumeric(r *wasm.Reader) error {
	s := f.gcReadRegion
	n, ok := s.plan.numeric[int(f.wasmPC-f.tracePCBase)]
	idx, err := r.U32()
	if err != nil {
		return err
	}
	if !ok || idx != n.target {
		return f.gcReadRegionViolation("unplanned call")
	}
	f.stats.peep("gc-read-region-numeric")
	oldFunc, oldBase, oldPC := f.traceFuncIdx, f.tracePCBase, f.wasmPC
	defer func() { f.traceFuncIdx, f.tracePCBase, f.wasmPC = oldFunc, oldBase, oldPC }()
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		parent := f.enterProfileInline()
		defer f.leaveProfileInline(parent)
		body := f.m.Code[int(n.target)-f.importedFunctionCount()]
		f.traceFuncIdx = n.target
		f.tracePCBase = body.LocalDeclBytes
		f.wasmPC = body.LocalDeclBytes + uint32(len(body.BodyBytes)-2)
		origin := f.enterProfileInstruction()
		defer f.switchProfileOrigin(origin)
	}
	return f.emitPlain(wasm.NewReader(nil), n.op)
}

// Integer control joins write mergeReg directly, outside ordinary allocation.
// A reserved raw-address home must therefore exclude it while merging is active.
func (f *fn) gcReadRegionRegister() Reg {
	block := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, reg := range [...]Reg{RBP, R12, R13, R14} {
		if f.regMerge && reg == mergeReg {
			continue
		}
		if f.regUser[reg] == nil && !block.has(reg) {
			return reg
		}
	}
	return regNone
}

// Keep all older values canonical in the first staging pass. Only consumed
// operands bypass canonical stores; arbitrary deferred prefixes still use the
// existing checked partial flush, including its overlapping-slot staging.
func (f *fn) stageGCReadRegionOperands(count int) error {
	object := f.s.back()
	for i := 1; i < count; i++ {
		if object == f.s.head {
			return f.gcReadRegionViolation("missing native read operand")
		}
		object = baseOfValentBlock(object).prev
	}
	if object == f.s.head || !object.isValue() {
		return f.gcReadRegionViolation("unplanned native read operand")
	}
	f.flushBelow(object)
	f.spillFloor = max(f.spillFloor, f.curSpillSlot())
	f.stats.peep("gc-read-region-stage-operands")
	return nil
}

// A private address home is worthwhile only when a later same-parameter read
// could use an already proved extent. Control dominance is enforced at emission;
// this scan cannot itself authorize reuse or enlarge an earlier extent check.
func finishGCReadRegionPlan(p *gcReadRegionPlan) {
	for pc, g := range p.groups {
		if g.array || g.sourceLocal < 0 {
			continue
		}
		for later, h := range p.groups {
			if later > pc && !h.array && h.sourceLocal == g.sourceLocal && h.typ == g.typ && h.required <= g.required && gcReadRegionDominates(g, h) {
				g.cache = true
				p.groups[pc] = g
				p.cacheHome = true
				break
			}
		}
	}
}

func gcReadRegionDominates(first, later gcReadRegionGroup) bool {
	if first.pathDepth > later.pathDepth {
		return false
	}
	for i := 0; i < first.pathDepth; i++ {
		if first.path[i] != later.path[i] {
			return false
		}
	}
	return true
}
