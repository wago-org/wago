// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"math/bits"
	"os"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

// Nonzero-immediate maps retain a same-binary scalar control for qualification.
var floatMapScatterEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_SCATTER") != "1"

var floatMapOffsetsEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_OFFSETS") != "1"

var floatMapLaneFPLoadsEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_LANE_FP_LOADS") != "1"

var floatMapLaneOffsetsEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_LANE_OFFSETS") != "1"

var floatMapInductionEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_INDUCTION") != "1"

var floatMapPacketsEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_PACKETS") != "1"
var floatMapInvariantsEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_INVARIANTS") != "1"
var floatMapLatchEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_LATCH") != "1"
var floatMapFourPacketsEnabled = os.Getenv("WAGO_ARM64_FLOAT_MAP_PACKET_COUNT") != "2"

// A bounded symbolic proof admits independent scalar floating-point and integer maps.
// The vector prefix preserves the expression tree and leaves an original scalar
// packet to restore every observable local. All range/alias failures fall back.
type floatMapNode struct {
	bits uint64
	a, b int16
	op   byte
}

const integerMapImmediateShift = 1 << 32

func (n floatMapNode) unary() bool {
	return n.op == 0x45 || n.immediateShift()
}

func (n floatMapNode) immediateShift() bool {
	return n.op >= 0x74 && n.op <= 0x76 && n.bits&integerMapImmediateShift != 0
}

func (n floatMapNode) left() int      { return int(n.a) }
func (n floatMapNode) right() int     { return int(n.b) }
func (n floatMapNode) condition() int { return int(n.bits) }

type floatMapAddress struct {
	offset    uint64 // widened memory immediate, added after dynamic i32 wrap
	base      int
	index     int    // source byte-index local plus one; zero keeps floating-map counter addressing
	stride    uint32 // source byte stride divided by contiguous output stride; zero means one
	bias      uint32
	invariant bool
}
type floatMapPlan struct {
	nodes                        [128]floatMapNode
	addresses                    [128]floatMapAddress
	invariant                    [128]bool
	locals                       [32]shared.LoopLocal[machineType, int]
	localN                       int
	counter, result, dest, count int
	step, limit, packet          uint32
	counterScale                 uint32 // physical source bytes per logical packet byte
	scatterBudget                int
	scatterCandidate             bool
	destOffset                   uint64
	wide                         bool
	integer                      bool
	orderedStoreLoads            bool
	interleaved                  bool
	guardOnlyLoads               [4]uint8
	guardOnlyLoadN               int
	extraStoreN                  int
	extraStores                  [2]shared.LoopStore[int]
	normalizedInduction          bool
	work, workLimit              int
	counterStep, spanScale       uint32
	limitTerms                   [2]integerMapLimitTerm
	limitTermN                   int
	byteIndices                  uint32
	byteSteps                    [32]uint32
}

// Local identities stay in source coordinates; only the bounded cell table is
// dense. Expressions capture immutable node indices when a source local changes.
func (p *floatMapPlan) localSlot(index int) int {
	return shared.FindLoopLocal(p.locals[:p.localN], uint32(index))
}

func (p *floatMapPlan) localWritten(index int) bool {
	slot := p.localSlot(index)
	return slot >= 0 && p.locals[slot].Written
}

const (
	floatMapMaxBytes         = 512
	floatMapScatterWorkLimit = 4096
)

func (f *fn) inspectFloatMapLoop(source *wasm.Reader, p *floatMapPlan) bool {
	*p = floatMapPlan{}
	p.integer = f.integerLoadMapProbe
	p.normalizedInduction = p.integer
	p.spanScale = 1
	p.counterScale = 1
	p.scatterBudget = floatMapScatterWorkLimit - int(f.floatMapScatterWork)
	p.workLimit = integerLoadMapWorkLimit - int(f.integerLoadMapWork)
	scan := *source
	start := scan.Offset()
	var stack [24]int
	var initialRead [32]bool
	var loadStage [128]uint8
	var stores [8]shared.LoopStore[int]
	depth, ns, n := 0, 0, 0
	push := func(x int) bool {
		if depth == len(stack) || x < 0 {
			return false
		}
		stack[depth] = x
		depth++
		return true
	}
	node := func(op byte, a, b int, bits uint64) int {
		if n == len(p.nodes) {
			return -1
		}
		p.nodes[n] = floatMapNode{bits: bits, a: int16(a), b: int16(b), op: op}
		n++
		return n - 1
	}
	local := func(index uint32) int {
		if slot := shared.FindLoopLocal(p.locals[:p.localN], index); slot >= 0 {
			return slot
		}
		if p.localN == len(p.locals) {
			return -1
		}
		id := node(0x20, int(index), 0, 0)
		if id < 0 {
			return -1
		}
		slot := p.localN
		p.locals[slot] = shared.LoopLocal[machineType, int]{Index: index, Type: f.localType[index], Initial: id, Value: id}
		p.localN++
		return slot
	}
	for scan.Offset()-start < floatMapMaxBytes {
		if !p.spend() {
			return false
		}
		pos := scan.Offset()
		op, err := scan.Byte()
		if err != nil {
			return false
		}
		switch op {
		case 0x01, 0x1a: // nop and drop
			if !p.integer || op == 0x1a && depth == 0 {
				return false
			}
			if op == 0x1a {
				depth--
			}
		case 0x20, 0x21, 0x22:
			x, e := scan.U32()
			if e != nil || x >= uint32(f.nLocals) {
				return false
			}
			slot := local(x)
			if slot < 0 {
				return false
			}
			cell := &p.locals[slot]
			if op == 0x20 {
				if cell.Value == cell.Initial {
					initialRead[slot] = true
				}
				if !push(cell.Value) {
					return false
				}
			} else {
				if depth == 0 {
					return false
				}
				cell.Assign(stack[depth-1], 0)
				if op == 0x21 {
					depth--
				}
			}
		case 0x41, 0x43, 0x44:
			var bits uint64
			if op == 0x41 {
				v, e := scan.I32()
				err = e
				bits = uint64(uint32(v))
			} else if op == 0x43 {
				v, e := scan.LEU32()
				err = e
				bits = uint64(v)
			} else {
				bits, err = scan.LEU64()
			}
			if err != nil || !push(node(op, 0, 0, bits)) {
				return false
			}
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x47, 0x92, 0x93, 0x94, 0x95, 0xa0, 0xa1, 0xa2, 0xa3:
			if depth < 2 {
				return false
			}
			depth -= 2
			if !push(node(op, stack[depth], stack[depth+1], 0)) {
				return false
			}
		case 0x74, 0x75, 0x76:
			if !p.integer || depth < 2 {
				return false
			}
			count := p.nodes[stack[depth-1]]
			depth -= 2
			a, b, bits := stack[depth], stack[depth+1], uint64(0)
			if count.op == 0x41 {
				b, bits = 0, integerMapImmediateShift|count.bits&31
			}
			if !push(node(op, a, b, bits)) {
				return false
			}
		case 0x45:
			if !p.integer || depth == 0 {
				return false
			}
			depth--
			if !push(node(op, stack[depth], 0, 0)) {
				return false
			}
		case 0x46, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f:
			if !p.integer || depth < 2 {
				return false
			}
			depth -= 2
			if !push(node(op, stack[depth], stack[depth+1], 0)) {
				return false
			}
		case 0x1b:
			if !p.integer || depth < 3 {
				return false
			}
			depth -= 3
			a, b, cond := stack[depth], stack[depth+1], stack[depth+2]
			kind, bits := byte(0x1b), uint64(cond)
			if op, ok := integerMapMinMax(a, b, p.nodes[cond]); ok {
				kind, bits = op, 0
			}
			if !push(node(kind, a, b, bits)) {
				return false
			}
		case 0x28, 0x2a, 0x2b:
			mem, off, e := f.readMemArg(&scan)
			if e != nil || mem != 0 || (off > 1<<20 || off != 0 && !floatMapOffsetsEnabled) || depth == 0 {
				return false
			}
			x := node(op, stack[depth-1], 0, off)
			if x < 0 {
				return false
			}
			loadStage[x] = uint8(ns)
			stack[depth-1] = x
		case 0x36, 0x38, 0x39:
			mem, off, e := f.readMemArg(&scan)
			if e != nil || mem != 0 || (off > 1<<20 || off != 0 && !floatMapOffsetsEnabled) || depth < 2 || (ns == len(stores) || ns == 2 && !floatMapPacketsEnabled) {
				return false
			}
			depth -= 2
			stores[ns] = shared.LoopStore[int]{Address: stack[depth], Value: stack[depth+1], Offset: off, PC: uint32(pos)}
			ns++
			if p.integer != (op == 0x36) {
				return false
			}
			if ns == 1 {
				p.wide = op == 0x39
			} else if p.wide != (op == 0x39) {
				return false
			}
		case 0x0d:
			label, e := scan.U32()
			if e != nil || label != 0 || depth != 1 || ns == 0 {
				return false
			}
			end, e := scan.Byte()
			if e != nil || end != 0x0b {
				return false
			}
			if p.integer {
				packetStores := ns
				if ns > 1 && ns <= len(p.extraStores)+1 {
					var bases [3]int
					distinct := true
					for i, store := range stores[:ns] {
						if !distinct {
							break
						}
						base := p.mapStoreBase(store.Address, 0)
						distinct = distinct && base >= 0
						for _, previous := range bases[:i] {
							distinct = distinct && previous != base
						}
						bases[i] = base
					}
					if distinct {
						p.extraStoreN = ns - 1
						packetStores = 1
					}
				}
				if !p.finishIntegerInduction(stack[0], packetStores, initialRead[:]) {
					return false
				}
			} else if !p.finishFloatInduction(stack[0], ns, initialRead[:]) {
				if !floatMapInductionEnabled {
					return false
				}
				p.normalizedInduction = true
				if !p.finishIntegerInduction(stack[0], ns, initialRead[:]) {
					return false
				}
			}
			size := uint32(4)
			if p.wide {
				size = 8
			}
			p.count, p.dest, p.result = n, stores[0].Address, stores[0].Value
			p.destOffset = stores[0].Offset
			if !p.proveAddresses(f.localType) {
				return false
			}
			if stride := p.addresses[p.dest].stride; stride > 1 && p.counterScale <= 1 && (!p.integer || stride != 2) {
				return false
			}
			for i := 1; i < ns; i++ {
				visits := 0
				a, valid := p.affine(stores[i].Address, f.localType, &visits)
				if !valid {
					return false
				}
				a.offset = stores[i].Offset
				p.addresses[stores[i].Address] = a
				if p.extraStoreN != 0 {
					if a.invariant || a.stride > 2 {
						return false
					}
					p.extraStores[i-1] = stores[i]
				}
			}
			if p.normalizedInduction && !p.integer {
				strided := false
				for i := 0; i < n; i++ {
					strided = strided || p.load(p.nodes[i].op) && p.addresses[i].stride > 1
				}
				if !strided {
					return false
				}
			}
			var used [128]uint8
			var storeMask uint8
			var mark func(int)
			mark = func(x int) {
				if !p.spend() {
					return
				}
				if used[x]&storeMask != 0 {
					return
				}
				used[x] |= storeMask
				v := p.nodes[x]
				if v.unary() {
					mark(v.left())
				} else if v.op == 0x1b || p.binary(v.op) {
					mark(v.left())
					mark(v.right())
					if v.op == 0x1b {
						mark(v.condition())
					}
				}
			}
			for i := 0; i < ns; i++ {
				storeMask = 1 << uint(i)
				mark(stores[i].Value)
			}
			for i := 0; i < n; i++ {
				if (p.nodes[i].op == 0x28 || p.nodes[i].op == 0x2a || p.nodes[i].op == 0x2b) && used[i] == 0 {
					if !p.integer || p.nodes[i].op != 0x28 || p.guardOnlyLoadN == len(p.guardOnlyLoads) {
						return false
					}
					p.guardOnlyLoads[p.guardOnlyLoadN] = uint8(i)
					p.guardOnlyLoadN++
				}
			}
			for i := 1; i < ns; i++ {
				distance := uint32(i) * size
				if p.extraStoreN != 0 {
					continue
				}
				if !p.nextAddress(stores[0].Address, stores[i].Address, distance) {
					return false
				}
				if p.sameMap(stores[0].Value, stores[i].Value, distance, 0) {
					continue
				}
				if !p.integer || ns != 2 || p.addresses[p.dest].stride > 1 {
					return false
				}
				p.extraStoreN, p.interleaved = 1, true
				p.extraStores[i-1] = stores[i]
				p.packet = 2 * floatMapVectorBytes
			}
			if !p.validExpression(p.result, 0) {
				return false
			}
			for _, store := range p.extraStores[:p.extraStoreN] {
				if !p.validExpression(store.Value, 0) {
					return false
				}
			}
			if floatMapInvariantsEnabled {
				p.labelInvariants()
			}
			// A non-invariant load may belong only to the store whose source
			// stage created it. Earlier captured values retain whole-range
			// disambiguation; invariant loads already have separate guards.
			p.orderedStoreLoads = true
			for i := 0; i < n; i++ {
				if p.load(p.nodes[i].op) && !p.invariant[i] && used[i] != 0 && used[i] != uint8(1<<loadStage[i]) {
					p.orderedStoreLoads = false
				}
			}
			return true
		default:
			return false
		}
	}
	return false
}

func (p *floatMapPlan) affine(x int, types []machineType, visits *int) (floatMapAddress, bool) {
	if p.normalizedInduction {
		return p.integerAffine(x)
	}
	if !p.spend() {
		return floatMapAddress{}, false
	}
	(*visits)++
	if *visits > 128 {
		return floatMapAddress{}, false
	}
	v := p.nodes[x]
	if v.op == 0x20 && types[v.left()] == mtI32 && v.left() != p.counter && !p.localWritten(v.left()) {
		return floatMapAddress{base: v.left(), invariant: true}, true
	}
	if v.op != 0x6a {
		return floatMapAddress{}, false
	}
	// Flatten addition modulo 2^32. Exactly one invariant base and one counter
	// are required; the runtime guard excludes wrap across the entire prefix.
	var base, counter, n int
	var bias uint32
	var walk func(int) bool
	walk = func(x int) bool {
		if !p.spend() {
			return false
		}
		n++
		if n > 128 {
			return false
		}
		v := p.nodes[x]
		switch v.op {
		case 0x41:
			bias += uint32(v.bits)
		case 0x20:
			if types[v.left()] != mtI32 {
				return false
			}
			if v.left() == p.counter {
				counter++
			} else {
				if base != 0 || p.localWritten(v.left()) {
					return false
				}
				base = v.left() + 1
			}
		case 0x6a:
			return walk(v.left()) && walk(v.right())
		default:
			return false
		}
		return true
	}
	if !walk(x) || base == 0 || counter > 1 || bias > 1<<20 {
		return floatMapAddress{}, false
	}
	stride := uint32(0)
	if counter != 0 && p.counterScale > 1 {
		stride = p.counterScale
	}
	return floatMapAddress{base: base - 1, bias: bias, stride: stride, invariant: counter == 0}, true
}
func (p *floatMapPlan) proveAddresses(types []machineType) bool {
	visits := 0
	addr, ok := p.affine(p.dest, types, &visits)
	if !ok || addr.invariant {
		return false
	}
	addr.offset = p.destOffset
	p.addresses[p.dest] = addr
	for i := 0; i < p.count; i++ {
		if p.load(p.nodes[i].op) {
			visits = 0
			a, ok := p.affine(p.nodes[i].left(), types, &visits)
			if !ok {
				return false
			}
			a.offset = p.nodes[i].bits
			p.addresses[i] = a
		}
	}
	return true
}
func (p *floatMapPlan) validExpression(x, depth int) bool {
	if depth > 8 || !p.spend() {
		return false
	}
	v := p.nodes[x]
	if v.op == 0x20 {
		if p.mapCounterValue(v) {
			return true
		}
		return !p.localWritten(v.left()) && p.locals[p.localSlot(v.left())].Type == p.valueType()
	}
	if p.integer {
		if v.op == 0x28 || v.op == 0x41 {
			return true
		}
		if v.unary() {
			return p.validExpression(v.left(), depth+1)
		}
		if v.op == 0x1b && !p.validExpression(v.condition(), depth+1) {
			return false
		}
		if v.op == 0x1b || p.binary(v.op) {
			return p.validExpression(v.left(), depth+1) && p.validExpression(v.right(), depth+1)
		}
		return false
	}
	if v.op == 0x2b || v.op == 0x44 {
		return p.wide
	}
	if v.op == 0x2a || v.op == 0x43 {
		return !p.wide
	}
	if p.wide && v.op >= 0xa0 && v.op <= 0xa3 || !p.wide && v.op >= 0x92 && v.op <= 0x95 {
		return p.validExpression(v.left(), depth+1) && p.validExpression(v.right(), depth+1)
	}
	return false
}

// Nodes are owned by one candidate and constructed after their operands.
// Memory invariance is usable only after the same guarded disjointness proof
// that protects the existing invariant-load coefficients.
func (p *floatMapPlan) labelInvariants() {
	for i := 0; i < p.count; i++ {
		v := p.nodes[i]
		switch v.op {
		case 0x41, 0x43, 0x44:
			p.invariant[i] = true
		case 0x20:
			p.invariant[i] = !p.localWritten(v.left()) && p.locals[p.localSlot(v.left())].Type == p.valueType()
		case 0x28, 0x2a, 0x2b:
			p.invariant[i] = p.addresses[i].invariant
		case 0x1b:
			p.invariant[i] = p.invariant[v.left()] && p.invariant[v.right()] && p.invariant[v.condition()]
		default:
			if v.unary() {
				p.invariant[i] = p.invariant[v.left()]
			} else if p.binary(v.op) {
				p.invariant[i] = p.invariant[v.left()] && p.invariant[v.right()]
			}
		}
	}
}

func (p *floatMapPlan) nextAddress(x, y int, step uint32) bool {
	a, b := p.addresses[x], p.addresses[y]
	if a.invariant || b.invariant {
		return a.invariant && b.invariant && a.base == b.base && a.index == b.index && a.bias == b.bias && a.offset == b.offset
	}
	return a.base == b.base && a.index == b.index && a.stride == b.stride && a.offset == b.offset && int64(int32(a.bias))+int64(step)*int64(max(a.stride, 1)) == int64(int32(b.bias))
}
func (p *floatMapPlan) sameMap(x, y int, step uint32, depth int) bool {
	if depth > 8 || !p.spend() {
		return false
	}
	a, b := p.nodes[x], p.nodes[y]
	if a.op != b.op {
		return false
	}
	if p.load(a.op) {
		if p.integer && integerMapAdjacentSLPEnabled {
			left, right := p.addresses[x], p.addresses[y]
			if left.invariant || right.invariant {
				return left.invariant && left == right
			}
			if left.base == right.base && left.index == right.index && left.stride == right.stride &&
				left.bias == right.bias && left.offset+uint64(step)*uint64(max(left.stride, 1)) == right.offset {
				return true
			}
		}
		return p.nextAddress(x, y, step)
	}
	if a.op == 0x20 {
		return a.a == b.a
	}
	if p.constant(a.op) {
		return a.bits == b.bits
	}
	if a.unary() {
		return a.bits == b.bits && p.sameMap(a.left(), b.left(), step, depth+1)
	}
	if a.op == 0x1b && !p.sameMap(a.condition(), b.condition(), step, depth+1) {
		return false
	}
	if p.sameMap(a.left(), b.left(), step, depth+1) && p.sameMap(a.right(), b.right(), step, depth+1) {
		return true
	}
	if p.integer && integerMapAdjacentSLPEnabled {
		switch a.op {
		case 0x46, 0x47, 0x6a, 0x6c, 0x71, 0x72, 0x73, integerMapMinS, integerMapMinU, integerMapMaxS, integerMapMaxU:
			return p.sameMap(a.left(), b.right(), step, depth+1) && p.sameMap(a.right(), b.left(), step, depth+1)
		}
	}
	return false
}

func (f *fn) emitFloatMapLoop(source *wasm.Reader) {
	if !f.opt(optDotLoopVector) || f.interruptible || f.moduleEH || f.localBase != 0 || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.memoryAddr64(0) || f.threadedMemory0 {
		return
	}
	if !f.hasFloatMapPrefix(source) {
		return
	}
	f.emitFloatMapBody(source)
}

// Maps need a typed producer and a matching output store. Screen that prefix
// and unsupported carried locals before reserving the symbolic candidate frame.
func (f *fn) hasFloatMapPrefix(source *wasm.Reader) bool {
	scan := *source
	start := scan.Offset()
	floating := false
	// These masks only reject; high local indices retain the full planner path.
	var initialReads, written uint64
	for scan.Offset()-start < floatMapMaxBytes {
		op, e := scan.Byte()
		if e != nil {
			return false
		}
		switch op {
		case 0x20, 0x21, 0x22:
			local, e := scan.U32()
			if e != nil || local >= uint32(f.nLocals) {
				return false
			}
			typ := f.localType[local]
			if op == 0x20 {
				floating = floating || typ.isFloat()
				if local < 64 && typ != mtI32 && written&(uint64(1)<<local) == 0 {
					initialReads |= uint64(1) << local
				}
			} else if local < 64 && typ != mtI32 {
				if initialReads&(uint64(1)<<local) != 0 {
					return false
				}
				written |= uint64(1) << local
			}
		case 0x41:
			if _, e := scan.I32(); e != nil {
				return false
			}
		case 0x43, 0x44:
			width := 4
			if op == 0x44 {
				width = 8
			}
			if _, e := scan.Bytes(width); e != nil {
				return false
			}
			floating = true
		case 0x2a, 0x2b:
			floating = true
			if e := f.classifier.SkipScalarMemoryImmediate(&scan); e != nil {
				return false
			}
		case 0x28:
			if !floating {
				return false
			}
			if e := f.classifier.SkipScalarMemoryImmediate(&scan); e != nil {
				return false
			}
		case 0x36, 0x38, 0x39:
			return floating && f.integerLoadMapProbe == (op == 0x36)
		case 0x6a, 0x47:
			// These are also valid before the first floating producer.
		case 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x92, 0x93, 0x94, 0x95, 0xa0, 0xa1, 0xa2, 0xa3:
			if !floating {
				return false
			}
		default:
			return false
		}
	}
	return false
}

func (f *fn) emitFloatMapBody(source *wasm.Reader) {
	var p floatMapPlan
	ok := f.inspectFloatMapLoop(source, &p)
	defer func() {
		if p.scatterCandidate {
			// Share the bounded integer-analysis budget without adding another
			// field to every compiler function or changing ordinary map admission.
			f.floatMapScatterWork = uint16(min(floatMapScatterWorkLimit, int(f.floatMapScatterWork)+p.work))
		}
		if p.integer {
			f.integerLoadMapWork = uint16(min(integerLoadMapWorkLimit, int(f.integerLoadMapWork)+p.work))
		}
	}()
	if !ok || p.counterScale > 1 && f.a.Len() > 64<<10 {
		return
	}
	// Deeply nested copy-only loops often move a handful of words, and even
	// the cheap runtime admission check can dominate them. Keep their original
	// scalar path until a lower-overhead entry recipe is available. Arithmetic
	// maps and shallow copies retain the normal guarded trip policy.
	if p.integer && p.limitTermN != 0 && p.nodes[p.result].op == 0x28 {
		parents := 0
		for i := len(f.ctrl) - 1; i >= 0; i-- {
			if !p.spend() {
				return
			}
			if f.ctrl[i].kind == cfLoop {
				parents++
				if parents == 2 {
					f.stats.peep("integer-load-map-nested-copy-reject")
					return
				}
			}
		}
	}
	minimumSpan := p.packet + p.step
	if p.normalizedInduction {
		minimumSpan = max(minimumSpan, 128)
		// The guarded positive-limit path requires counter < limit, so its
		// span cannot exceed limit, even when the entry value is unknown.
		if p.limitTermN == 0 && p.limit != 0 && uint64(p.limit)*uint64(p.spanScale) < uint64(minimumSpan) {
			return
		}
	}
	type group struct {
		base   int
		index  int
		stride uint32
		min    int32
		max    int64
		reg    Reg
	}
	var groups [4]group
	groupLimit := 3
	if p.extraStoreN != 0 {
		groupLimit = len(groups)
	}
	ng := 0
	add := func(a floatMapAddress) bool {
		// Compare dynamic biases as signed offsets, then normalize the
		// shared low address modulo i32 before checking the widened range.
		bias := int32(a.bias)
		end := int64(bias) + int64(a.offset)
		for i := 0; i < ng; i++ {
			g := &groups[i]
			if g.base == a.base && g.index == a.index && g.stride == a.stride {
				g.min = min(g.min, bias)
				g.max = max(g.max, end)
				return true
			}
		}
		if ng == groupLimit {
			return false
		}
		groups[ng] = group{base: a.base, index: a.index, stride: a.stride, min: bias, max: end}
		ng++
		return true
	}
	var loads [16]int
	nl := 0
	reusedLoad := -1
	var coefficients uint32
	usesCounterValue := false
	var invariant [2]int
	nInvariant := 0
	var collect func(int) bool
	collect = func(x int) bool {
		if !p.spend() {
			return false
		}
		v := p.nodes[x]
		if p.load(v.op) {
			if p.addresses[x].invariant {
				for i := 0; i < nInvariant; i++ {
					if invariant[i] == x {
						return true
					}
				}
				if nInvariant == len(invariant) {
					return false
				}
				invariant[nInvariant] = x
				nInvariant++
				return true
			}
			if p.extraStoreN != 0 && !p.interleaved {
				for _, seen := range loads[:nl] {
					if seen == x {
						if reusedLoad < 0 {
							reusedLoad = x
						}
						return true
					}
				}
			}
			if nl == len(loads) || !add(p.addresses[x]) {
				return false
			}
			loads[nl] = x
			nl++
			return true
		}
		if v.op == 0x20 {
			if p.mapCounterValue(v) {
				usesCounterValue = true
			} else {
				coefficients |= 1 << p.localSlot(v.left())
			}
			return true
		}
		if p.constant(v.op) {
			return true
		}
		if v.unary() {
			return collect(v.left())
		}
		return collect(v.left()) && collect(v.right()) && (v.op != 0x1b || collect(v.condition()))
	}
	if !add(p.addresses[p.dest]) || !collect(p.result) {
		return
	}
	// Each input has a disjointness check against both outputs.
	for _, store := range p.extraStores[:p.extraStoreN] {
		if !add(p.addresses[store.Address]) || !collect(store.Value) {
			return
		}
	}
	// A discarded value needs a range proof, but cannot carry a dependence
	// through memory. If the proof fails, the original loop preserves the
	// location of the trap and any earlier stores.
	for _, load := range p.guardOnlyLoads[:p.guardOnlyLoadN] {
		address := p.addresses[load]
		if !address.invariant && !add(address) {
			return
		}
	}
	if p.extraStoreN == 1 && nl > 8 || p.extraStoreN == 2 && nl > 4 {
		return
	}
	// Each carried byte index must have a guarded stream from which its next
	// source value can be recovered. Other carried state stays on scalar code.
	if p.normalizedInduction {
		var covered uint32
		for _, g := range groups[:ng] {
			if g.index > 0 {
				covered |= 1 << p.localSlot(g.index-1)
			}
		}
		if p.byteIndices&^covered != 0 {
			return
		}
	}
	if floatMapPacketDependenceEnabled && p.hasShortForwardDependence(loads[:nl]) {
		return
	}
	var countConst func(int) int
	countConst = func(x int) int {
		if !p.spend() {
			return 1000
		}
		v := p.nodes[x]
		if p.mapCounterValue(v) {
			return 0
		}
		if p.invariant[x] || v.op == 0x20 || p.constant(v.op) {
			return 1
		}
		if p.load(v.op) {
			if p.addresses[x].invariant {
				return 1
			}
			return 0
		}
		if v.unary() {
			return countConst(v.left())
		}
		count := countConst(v.left()) + countConst(v.right())
		if v.op == 0x1b {
			count += countConst(v.condition())
		}
		return count
	}
	constantCount := countConst(p.result)
	for _, store := range p.extraStores[:p.extraStoreN] {
		constantCount += countConst(store.Value)
	}
	if constantCount > 2 {
		return
	}
	// Reserve enough FP scratch for the deepest expression before emitting code.
	// Whole-function pins and outer immutable caches remain unavailable.
	available := 0
	blocked := f.blockedFRegs(0)
	for _, reg := range fpAllocRegs {
		if !blocked.has(reg) && f.fregUser[reg] == nil {
			available++
		}
	}
	var pressure func(int) int
	pressure = func(x int) int {
		if !p.spend() {
			return 1000
		}
		v := p.nodes[x]
		if p.load(v.op) || v.op == 0x20 || p.constant(v.op) {
			return 1
		}
		if v.unary() {
			return pressure(v.left())
		}
		peak := max(pressure(v.left()), pressure(v.right())+1)
		if v.op == 0x1b {
			peak = max(peak, pressure(v.condition())+2)
		}
		return peak
	}
	peakPressure := pressure(p.result)
	for _, store := range p.extraStores[:p.extraStoreN] {
		storePressure := pressure(store.Value)
		if p.interleaved {
			storePressure++ // the first result stays live until the ZIPs
		}
		peakPressure = max(peakPressure, storePressure)
	}
	if usesCounterValue {
		constantCount += 2 // fixed lane offsets and the current block's counter
	}
	if reusedLoad >= 0 {
		constantCount++ // shared across the independent outputs of one block
	}
	if available < constantCount+peakPressure+1 || p.normalizedInduction && p.work > p.workLimit {
		return
	}
	read := func(local int, dst Reg) {
		if local < 0 {
			f.a.MovImm64(dst, 0)
			return
		}
		if reg, _, pin := f.pinReg(local); pin {
			f.recoverLocal(local)
			f.a.MovReg32(dst, reg)
		} else if f.locals[local].state == lsConstZero {
			f.a.MovImm64(dst, 0)
		} else {
			f.ld32(dst, SP, f.localOff(local))
		}
	}

	readLimit := func(dst, scratch Reg) {
		if p.limitTermN == 0 {
			f.a.MovImm64(dst, uint64(p.limit))
			return
		}
		first := p.limitTerms[0]
		read(int(first.local), dst)
		if first.shift != 0 {
			f.a.LslImm(dst, dst, first.shift, true)
		}
		if first.sub {
			f.a.Sub32(dst, ZR, dst)
		}
		if p.limitTermN == 2 {
			second := p.limitTerms[1]
			read(int(second.local), scratch)
			if second.sub {
				f.a.SubShiftedReg(dst, dst, scratch, a64.RegShiftLSL, second.shift, true)
			} else {
				f.a.AddShifted(dst, dst, scratch, second.shift, true)
			}
		}
		if p.limit != 0 {
			f.floatMapOffset(dst, dst, uint64(p.limit), false, scratch)
		}
	}
	exit := f.planIntegerMapExit(&p)
	if p.normalizedInduction {
		// Admission must not evict an original stack value merely to execute a
		// scalar fallback. These are leases from the existing allocator's free
		// pool; the unchanged scalar loop retains its original owned locations.
		free := 0
		blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
		for _, reg := range gpAlloc {
			if f.regUser[reg] == nil && !blocked.has(reg) {
				free++
			}
		}
		if free < ng+3 {
			f.stats.peep("integer-load-map-gp-pressure-reject")
			return
		}
		// Recover all borrowed pins on both paths before a check can skip setup.
		f.recoverLocal(p.counter)
		for i := 0; i < p.localN; i++ {
			bit := uint32(1) << i
			if exit.extra&bit != 0 {
				f.recoverLocal(int(p.locals[i].Index))
			}
			if exit.invariant&bit != 0 {
				f.recoverLocal(int(exit.source[i].local))
			}
		}
		for _, term := range p.limitTerms[:p.limitTermN] {
			f.recoverLocal(int(term.local))
		}
		for slot, cell := range p.locals[:p.localN] {
			if coefficients&(1<<slot) != 0 {
				f.recoverLocal(int(cell.Index))
			}
		}
		for _, x := range invariant[:nInvariant] {
			if p.addresses[x].base >= 0 {
				f.recoverLocal(p.addresses[x].base)
			}
		}
		for _, load := range p.guardOnlyLoads[:p.guardOnlyLoadN] {
			if address := p.addresses[load]; address.invariant && address.base >= 0 {
				f.recoverLocal(address.base)
			}
		}
		for _, g := range groups[:ng] {
			if g.base >= 0 {
				f.recoverLocal(g.base)
			}
			if g.index > 0 {
				f.recoverLocal(g.index - 1)
			}
		}
		// Compare in source-counter units before leasing/initializing streams,
		// computing widened byte spans or checking ranges. A wrapped negative
		// difference may pass here but is rejected by the full eligibility proof.
		// A compound bound may need both scratch registers. Keep the
		// established schedule for constant and single-local bounds.
		if p.limitTermN == 2 || p.limitTermN == 1 && p.limit != 0 {
			readLimit(X16, X17)
			read(p.counter, X17)
		} else {
			read(p.counter, X17)
			readLimit(X16, X17)
		}
		f.a.Sub32(X17, X16, X17)
		f.cmpImm(X17, (minimumSpan+p.spanScale-1)/p.spanScale, false)
		scalar := f.a.Bcond(condB)
		defer func() { f.patchBranch19(scalar, f.a.Len()) }()
	}

	oldPinned, oldFPinned := f.pinned, f.fpinned
	var regs [7]Reg
	nr := 0
	alloc := func() Reg {
		x := f.allocRegOrNone(0)
		if x != regNone {
			f.pinned = f.pinned.add(x)
			regs[nr] = x
			nr++
		}
		return x
	}
	for i := 0; i < ng; i++ {
		groups[i].reg = alloc()
	}
	counter, span, size := alloc(), alloc(), alloc()
	defer func() {
		f.pinned = oldPinned
		f.fpinned = oldFPinned
		for i := 0; i < nr; i++ {
			f.release(regs[i])
		}
	}()
	if counter == regNone || span == regNone || size == regNone {
		return
	}
	for i := 0; i < ng; i++ {
		if groups[i].reg == regNone {
			return
		}
	}

	read(p.counter, counter)
	for slot, cell := range p.locals[:p.localN] {
		if coefficients&(1<<slot) != 0 {
			f.recoverLocal(int(cell.Index))
		}
	}
	for i := 0; i < nInvariant; i++ {
		if base := p.addresses[invariant[i]].base; base >= 0 {
			f.recoverLocal(base)
		}
	}
	// Recover every borrowed local before any guard can bypass its reload.
	for i := 0; i < ng; i++ {
		if p.normalizedInduction {
			if groups[i].base >= 0 {
				f.recoverLocal(groups[i].base)
			}
			if groups[i].index > 0 {
				f.recoverLocal(groups[i].index - 1)
			}
		} else {
			read(groups[i].base, groups[i].reg)
		}
	}
	var guards [64]int
	nGuards := 0
	guard := func(cc Cond) { guards[nGuards] = f.a.Bcond(cc); nGuards++ }
	if p.counterScale > 1 {
		// A source counter in another residue class must retain the original
		// modular loop. Normalize only an exact multiple of the physical scale.
		f.a.MovImm64(X17, uint64(p.counterScale))
		f.a.Udiv32(span, counter, X17)
		f.a.Msub32(X16, span, X17, counter)
		f.cmpImm(X16, 0, false)
		guard(condNE)
		f.a.MovReg32(counter, span)
	}
	if p.normalizedInduction && p.limitTermN != 0 {
		readLimit(X16, X17)
		f.cmpRR(counter, X16, false)
		guard(condAE)
		f.a.Sub32(span, X16, counter)
	} else {
		f.a.MovImm64(span, uint64(p.limit))
		f.a.Sub32(span, span, counter)
	}
	if p.spanScale > 1 {
		shift := uint8(0)
		for scale := p.spanScale; scale > 1; scale >>= 1 {
			shift++
		}
		f.a.LslImm(span, span, shift, false)
		f.a.MovImm64(X16, 1<<32-1)
		f.cmpRR(span, X16, true)
		guard(condA)
	}
	f.cmpImm(span, minimumSpan, false)
	guard(condB)
	if p.normalizedInduction {
		for i := 0; i < ng; i++ {
			read(groups[i].base, groups[i].reg)
		}
	}
	if p.step&(p.step-1) == 0 {
		f.a.TstImm32(span, p.step-1)
	} else {
		// A source packet can contain an odd number of scalar lanes. A
		// nontrapping remainder guard retains the original modular loop when
		// its fixed limit is not reachable before wrapping.
		f.a.MovImm64(X17, uint64(p.step))
		f.a.Udiv32(X16, span, X17)
		f.a.Msub32(X16, X16, X17, span)
		f.cmpImm(X16, 0, false)
	}
	guard(condNE)
	// Reject nonterminating modular trips: zero-limit countdowns must start
	// negative; positive fixed limits must be ahead of the unsigned counter.
	if p.normalizedInduction && p.limitTermN != 0 {
		// Dynamic limits were checked before computing their widened byte span.
	} else if p.limit == 0 {
		f.cmpImm(counter, 0, false)
		guard(condGE)
	} else {
		f.a.MovImm64(X16, uint64(p.limit))
		f.cmpRR(counter, X16, false)
		guard(condAE)
	}
	f.ld64(size, linMemReg, -int32(bdCurBytes))
	for _, load := range p.guardOnlyLoads[:p.guardOnlyLoadN] {
		address := p.addresses[load]
		if address.invariant {
			read(address.base, X16)
			f.floatMapOffset(X16, X16, uint64(address.bias), false, X17)
			f.floatMapOffset(X16, X16, address.offset+4, true, X17)
			f.cmpRR(X16, size, true)
			guard(condA)
		}
	}

	for i := 0; i < ng; i++ {
		g := &groups[i]
		if !p.normalizedInduction || g.index == p.counter+1 {
			scale := p.counterScale
			if p.normalizedInduction {
				scale = p.spanScale * max(g.stride, 1)
			}
			if p.normalizedInduction && scale > 1 && scale&(scale-1) == 0 {
				f.a.AddShifted(g.reg, g.reg, counter, uint8(bits.TrailingZeros32(scale)), true)
			} else if scale > 1 {
				f.a.MovImm32(X17, int32(scale))
				f.a.Mul32(X16, counter, X17)
				f.a.Add32(g.reg, g.reg, X16)
			} else {
				f.a.Add32(g.reg, g.reg, counter)
			}
		} else if g.index > 0 {
			read(g.index-1, X16)
			f.a.Add32(g.reg, g.reg, X16)
		}
		f.floatMapOffset(g.reg, g.reg, uint64(uint32(g.min)), false, X16)
		if g.stride > 1 {
			f.a.MovImm64(X17, uint64(g.stride))
			f.a.Mul64(X16, span, X17)
			f.a.Add64(X16, g.reg, X16)
		} else {
			f.a.Add64(X16, g.reg, span)
		}
		f.floatMapOffset(X16, X16, uint64(g.max-int64(g.min)), true, X17)
		f.cmpRR(X16, size, true)
		guard(condA)
		f.a.MovImm64(X17, 1<<32)
		f.cmpRR(X16, X17, true)
		guard(condA)
	}
	locate := func(a floatMapAddress) (Reg, uint32) {
		for i := 0; i < ng; i++ {
			if groups[i].base == a.base && groups[i].index == a.index && groups[i].stride == a.stride {
				return groups[i].reg, uint32(int64(int32(a.bias)) - int64(groups[i].min) + int64(a.offset))
			}
		}
		panic("arm64: missing float map address")
	}
	d, dOff := locate(p.addresses[p.dest])
	outputStride := max(p.counterScale, p.addresses[p.dest].stride, 1)
	type outputStream struct {
		reg    Reg
		offset uint32
		stride uint32
		value  int
	}
	outputs := [3]outputStream{{d, dOff, outputStride, p.result}}
	outputCount := 1 + p.extraStoreN
	for i, store := range p.extraStores[:p.extraStoreN] {
		address := p.addresses[store.Address]
		base, off := locate(address)
		outputs[i+1] = outputStream{base, off, max(address.stride, 1), store.Value}
	}
	compareSpan := func(stride uint32) {
		extent := span
		if stride > 1 {
			f.a.MovImm64(X17, uint64(stride))
			f.a.Mul64(X17, span, X17)
			extent = X17
		}
		f.cmpRR(X16, extent, true)
	}
	guardDisjoint := func(a, b outputStream) {
		f.floatMapOffset(X16, a.reg, uint64(a.offset), true, X16)
		f.floatMapOffset(X17, b.reg, uint64(b.offset), true, X17)
		f.cmpRR(X16, X17, true)
		above := f.a.Bcond(condA)
		f.a.Sub64(X16, X17, X16)
		compareSpan(a.stride)
		guard(condB)
		skip := f.a.Branch()
		f.patchBranch19(above, f.a.Len())
		f.a.Sub64(X16, X16, X17)
		compareSpan(b.stride)
		guard(condB)
		f.patchBranch26(skip, f.a.Len())
	}
	if !p.interleaved {
		for i, output := range outputs[:outputCount] {
			for _, previous := range outputs[:i] {
				guardDisjoint(previous, output)
			}
		}
	}
	guardedOutputs := outputs[:outputCount]
	if p.interleaved {
		// The primary span covers both alternating words.
		guardedOutputs = guardedOutputs[:1]
	}
	// The scalar suffix reloads the invariant before any original store
	// can change it. Limit clipping to contiguous single-store packets.
	clipInvariantPrefix := integerMapInvariantPrefixEnabled && p.integer && p.step == 4 &&
		nInvariant != 0 && p.extraStoreN == 0 && outputStride == 1
	// An invariant memory coefficient must stay unchanged for the whole map.
	// Invalid words and even byte overlaps with the output retain scalar loads.
	for _, output := range guardedOutputs {
		for i := 0; i < nInvariant; i++ {
			iv := p.addresses[invariant[i]]
			read(iv.base, X16)
			f.floatMapOffset(X16, X16, uint64(iv.bias), false, X17)
			if iv.offset != 0 {
				f.floatMapOffset(X16, X16, iv.offset, true, X17)
			}
			width := uint32(4)
			if p.wide {
				width = 8
			}
			f.a.AddImm64(X17, X16, width)
			f.cmpRR(X17, size, true)
			guard(condA)
			f.floatMapOffset(X17, output.reg, uint64(output.offset), true, X17)
			if clipInvariantPrefix {
				f.cmpRR(X16, X17, true)
				beforeOutput := f.a.Bcond(condB)
				f.a.Sub64(X16, X16, X17)
				f.cmpRR(X16, span, true)
				f.a.Csel64(span, X16, span, condB)
				f.cmpImm(span, minimumSpan, false)
				guard(condB)
				done := f.a.Branch()
				f.patchBranch19(beforeOutput, f.a.Len())
				f.a.Sub64(X16, X17, X16)
				f.cmpImm(X16, width, true)
				guard(condB)
				f.patchBranch26(done, f.a.Len())
				continue
			}
			f.a.Sub64(X16, X16, X17)
			f.a.AddImm64(X16, X16, width-1)
			if output.stride > 1 {
				f.a.MovImm64(X17, uint64(output.stride))
				f.a.Mul64(X17, span, X17)
				f.a.AddImm64(X17, X17, width-1)
			} else {
				f.a.AddImm64(X17, span, width-1)
			}
			f.cmpRR(X16, X17, true)
			guard(condB)
		}
	}
	// Packet-local dependence checks require source loads to follow the previous
	// store. Captured multi-store values retain whole-range disambiguation.
	scalarWidth := uint32(4)
	if p.wide {
		scalarWidth = 8
	}
	packetDependence := floatMapPacketDependenceEnabled && (p.step == scalarWidth || p.orderedStoreLoads)
	var checked [16]struct {
		base   Reg
		offset uint32
	}
	nChecked := 0
	for i := 0; i < nl; i++ {
		s, off := locate(p.addresses[loads[i]])
		if p.counterScale > 1 {
			width := uint32(4)
			if p.wide {
				width = 8
			}
			f.floatMapScatterAlias(d, s, dOff, off, span, size, p.counterScale, width, guard)
			continue
		}
		stride := max(p.addresses[loads[i]].stride, 1)
		if p.extraStoreN != 0 || stride > 1 || outputStride > 1 {
			// Independent stores and non-unit spacing require disjoint whole
			// ranges. The unit-stride packet dependence proof does not apply.
			input := outputStream{reg: s, offset: off, stride: stride}
			for _, output := range guardedOutputs {
				guardDisjoint(output, input)
			}
			continue
		}
		if floatMapGuardSelectionEnabled {
			duplicate := false
			for _, a := range checked[:nChecked] {
				if a.base == s && a.offset == off {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			checked[nChecked].base, checked[nChecked].offset = s, off
			nChecked++
			// The range checks above exclude address wrap. Equal symbolic
			// bases then differ by this exact widened byte distance.
			if s == d {
				if packetDependence {
					continue
				}
				if off != dOff {
					distance := off - dOff
					if off < dOff {
						distance = dOff - off
					}
					f.compareFloatMapSpan(span, distance)
					guard(condA)
				}
				continue
			}
		}
		f.floatMapOffset(X16, d, uint64(dOff), true, X16)
		f.floatMapOffset(X17, s, uint64(off), true, X17)
		if packetDependence {
			if floatMapDirectionGuardEnabled {
				// Equal or backward output addresses introduce no forward
				// dependence. Keep their cheap branch around the subtraction.
				f.cmpRR(X16, X17, true)
				safe := f.a.Bcond(condBE)
				f.a.Sub64(X16, X16, X17)
				f.cmpImm(X16, floatMapVectorBytes, true)
				guard(condB)
				f.patchBranch19(safe, f.a.Len())
			} else {
				// Same proof in a branchless direction test, retained as a
				// same-binary cost control for qualification.
				f.a.Sub64(X16, X16, X17)
				f.a.SubImm64(X16, X16, 1)
				f.cmpImm(X16, floatMapVectorBytes-1, true)
				guard(condB)
			}
			continue
		}
		f.cmpRR(X16, X17, true)
		equal := f.a.Bcond(condE)
		above := f.a.Bcond(condA)
		f.a.Sub64(X16, X17, X16)
		f.cmpRR(X16, span, true)
		guard(condB)
		skip := f.a.Branch()
		f.patchBranch19(above, f.a.Len())
		f.a.Sub64(X16, X16, X17)
		f.cmpRR(X16, span, true)
		guard(condB)
		f.patchBranch19(equal, f.a.Len())
		f.patchBranch26(skip, f.a.Len())
	}
	for i := 0; i < ng; i++ {
		f.a.Add64(groups[i].reg, groups[i].reg, linMemReg)
	}
	var constants [8]struct {
		node int
		reg  Reg
	}
	nc := 0
	var vectors [24]Reg
	nv := 0
	laneCounters := regNone
	if usesCounterValue {
		laneCounters = f.allocFReg(0)
		f.fpinned = f.fpinned.add(laneCounters)
		vectors[nv] = laneCounters
		nv++
		f.a.NeonEor16b(laneCounters, laneCounters, laneCounters)
		for lane := uint8(1); lane < 4; lane++ {
			f.a.MovImm32(X16, int32(uint32(lane)*p.counterStep))
			f.a.NeonInsS(laneCounters, X16, lane)
		}
	}

	emitOp := func(op byte, out, a, b Reg) {
		switch op {
		case 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f:
			f.emitIntegerMapComparison(op, out, a, b)
		case 0x6a:
			f.a.NeonAddS(out, a, b)
		case 0x6b:
			f.a.NeonSubS(out, a, b)
		case 0x6c:
			f.a.NeonMulS(out, a, b)
		case 0x71:
			f.a.NeonAnd16b(out, a, b)
		case 0x72:
			f.a.NeonOrr16b(out, a, b)
		case 0x73:
			f.a.NeonEor16b(out, a, b)
		case 0x74, 0x75, 0x76:
			// Wasm masks lane counts to five bits. NEON uses negative
			// counts for right shifts.
			count := f.allocFReg(0)
			f.a.NeonShlS(count, b, 27)
			f.a.NeonUshrS(count, count, 27)
			if op != 0x74 {
				f.a.NeonNegS(count, count)
			}
			if op == 0x75 {
				f.a.NeonSshrvS(out, a, count)
			} else {
				f.a.NeonUshlS(out, a, count)
			}
			f.releaseF(count)
		case integerMapMinS:
			f.a.NeonSminS(out, a, b)
		case integerMapMinU:
			f.a.NeonUminS(out, a, b)
		case integerMapMaxS:
			f.a.NeonSmaxS(out, a, b)
		case integerMapMaxU:
			f.a.NeonUmaxS(out, a, b)
		case 0x92, 0xa0:
			f.a.NeonFadd(out, a, b, p.wide)
		case 0x93, 0xa1:
			f.a.NeonFsub(out, a, b, p.wide)
		case 0x94, 0xa2:
			f.a.NeonFmul(out, a, b, p.wide)
		case 0x95, 0xa3:
			f.a.NeonFdiv(out, a, b, p.wide)
		}
	}
	releaseTemp := func(r Reg) { f.fpinned = f.fpinned.remove(r); f.releaseF(r) }
	var invariantValue func(int) Reg
	invariantValue = func(x int) Reg {
		v := p.nodes[x]
		if v.unary() {
			r := invariantValue(v.left())
			f.emitIntegerMapUnary(v, r, r)
			return r
		}
		if v.op == 0x1b {
			a := invariantValue(v.left())
			b := invariantValue(v.right())
			mask := invariantValue(v.condition())
			f.a.NeonCmeqZeroS(mask, mask)
			f.a.NeonBsl16b(mask, b, a)
			releaseTemp(a)
			releaseTemp(b)
			return mask
		}
		if p.binary(v.op) {
			a := invariantValue(v.left())
			b := invariantValue(v.right())
			emitOp(v.op, a, a, b)
			releaseTemp(b)
			return a
		}
		r := f.allocFReg(0)
		f.fpinned = f.fpinned.add(r)

		if v.op == 0x20 && p.integer {
			read(v.left(), X16)
			f.a.FmovFromGpr(r, X16, false)
		} else if v.op == 0x20 {
			if reg, _, pin := f.pinReg(v.left()); pin {
				f.a.FmovReg(r, reg, p.wide)
			} else if f.locals[v.left()].state == lsConstZero {
				f.a.FmovFromGpr(r, ZR, p.wide)
			} else {
				f.a.LdrF(r, SP, f.localOff(v.left()), p.wide)
			}
		} else if p.load(v.op) {
			iv := p.addresses[x]
			read(iv.base, X16)
			f.floatMapOffset(X16, X16, uint64(iv.bias), false, X17)
			if iv.offset != 0 {
				f.floatMapOffset(X16, X16, iv.offset, true, X17)
			}
			f.a.Add64(X16, X16, linMemReg)
			f.a.LdrF(r, X16, 0, p.wide)
		} else if p.integer {
			f.a.MovImm64(X16, uint64(uint32(v.bits)))
			f.a.FmovFromGpr(r, X16, false)
		} else {
			f.loadFConst(r, storage{kind: stConst, typ: mapFloatType(p.wide), cval: int64(v.bits)})
		}
		if p.wide {
			f.a.NeonDupD(r, r)
		} else {
			f.a.NeonDupS(r, r)
		}
		return r
	}
	var preload func(int)
	preload = func(x int) {
		v := p.nodes[x]
		if p.mapCounterValue(v) {
			return
		}
		if p.invariant[x] || v.op == 0x20 || p.constant(v.op) || (p.load(v.op)) && p.addresses[x].invariant {
			for i := 0; i < nc; i++ {
				if constants[i].node == x {
					return
				}
			}
			r := invariantValue(x)
			vectors[nv] = r
			nv++
			constants[nc].node = x
			constants[nc].reg = r
			nc++
			return
		}
		if !p.load(v.op) {
			preload(v.left())
			if !v.unary() {
				preload(v.right())
			}
			if v.op == 0x1b {
				preload(v.condition())
			}
		}
	}
	// The bounded expression has at most eight levels. Count constants before
	// emitting anything that would need an unbounded register reservation.
	for _, output := range outputs[:outputCount] {
		preload(output.value)
	}
	var block int32
	counterValue, cachedLoad := regNone, regNone
	var emit func(int) (Reg, bool)
	emit = func(x int) (Reg, bool) {
		v := p.nodes[x]
		if p.mapCounterValue(v) {
			return counterValue, false
		}
		if p.invariant[x] || v.op == 0x20 || p.constant(v.op) || (p.load(v.op)) && p.addresses[x].invariant {
			for i := 0; i < nc; i++ {
				if constants[i].node == x {
					return constants[i].reg, false
				}
			}
			panic("arm64: missing float map constant")
		}
		if p.load(v.op) {
			if x == reusedLoad && cachedLoad != regNone {
				return cachedLoad, false
			}
			base, off := locate(p.addresses[x])
			r := f.allocFReg(0)
			f.fpinned = f.fpinned.add(r)
			stride := max(p.addresses[x].stride, 1)
			if p.interleaved && stride == 1 {
				// The whole-span guard also covers the skipped word after
				// each selected lane, including the last one.
				tmp := f.allocFReg(0)
				f.a.LdrQ(r, base, int32(off)+block)
				f.a.LdrQ(tmp, base, int32(off)+block+floatMapVectorBytes)
				f.a.NeonUzp1S(r, r, tmp)
				f.releaseF(tmp)
			} else if stride > 1 || p.interleaved {
				size := uint32(4)
				if p.wide {
					size = 8
				}
				laneStep := size
				if p.interleaved {
					laneStep = p.step
				}
				for lane := uint32(0); lane < 16/size; lane++ {
					address := uint64(off) + (uint64(block)+uint64(lane*laneStep))*uint64(stride)
					if lane == 0 && floatMapLaneFPLoadsEnabled {
						// The first scalar lane initializes the vector directly;
						// later lane inserts never need its temporary GP copy.
						if floatMapLaneOffsetsEnabled && address%uint64(size) == 0 && address/uint64(size) <= 0xfff {
							f.a.LdrF(r, base, int32(address), p.wide)
						} else {
							f.floatMapOffset(X17, base, address, true, X16)
							f.a.LdrF(r, X17, 0, p.wide)
						}
						continue
					}
					if floatMapLaneOffsetsEnabled && address%uint64(size) == 0 && address/uint64(size) <= 0xfff {
						// The guarded exact-width load can use its native scaled
						// displacement without materializing a second address.
						if p.wide {
							f.a.Load64(X16, base, uint32(address))
						} else {
							f.a.Load32(X16, base, uint32(address))
						}
					} else {
						f.floatMapOffset(X17, base, address, true, X16)
						if lane != 0 && floatMapLaneFPLoadsEnabled {
							f.a.Ld1Lane(r, X17, uint8(lane), p.wide)
							continue
						}
						if p.wide {
							f.ld64(X16, X17, 0)
						} else {
							f.ld32(X16, X17, 0)
						}
					}
					if lane == 0 {
						f.a.FmovFromGpr(r, X16, p.wide)
					} else if p.wide {
						f.a.NeonInsD(r, X16, byte(lane))
					} else {
						f.a.NeonInsS(r, X16, byte(lane))
					}
				}
			} else {
				f.a.LdrQ(r, base, int32(off)+block)
			}
			if x == reusedLoad {
				cachedLoad = r
				return r, false
			}
			return r, true
		}
		a, ownA := emit(v.left())
		var b Reg
		var ownB bool
		if !v.unary() {
			b, ownB = emit(v.right())
		}
		if v.op == 0x1b {
			cond, ownCond := emit(v.condition())
			out := cond
			if !ownCond {
				out = f.allocFReg(0)
				f.fpinned = f.fpinned.add(out)
			}
			f.a.NeonCmeqZeroS(out, cond)
			f.a.NeonBsl16b(out, b, a)
			if ownA {
				releaseTemp(a)
			}
			if ownB {
				releaseTemp(b)
			}
			return out, true
		}
		out := a
		if !ownA {
			out = f.allocFReg(0)
			f.fpinned = f.fpinned.add(out)
		}
		if v.unary() {
			f.emitIntegerMapUnary(v, out, a)
		} else {
			emitOp(v.op, out, a, b)
		}
		if ownB {
			f.fpinned = f.fpinned.remove(b)
			f.releaseF(b)
		}
		return out, true
	}
	blockBytes := int32(floatMapVectorBytes)
	if p.interleaved {
		blockBytes *= 2
	}
	packet := func(width uint32, final bool) {
		for block = 0; block < int32(width); block += blockBytes {
			if p.interleaved {
				a, ownA := emit(outputs[0].value)
				b, ownB := emit(outputs[1].value)
				zipped := f.allocFReg(0)
				f.a.NeonZip1S(zipped, a, b)
				f.a.StrQ(d, int32(dOff)+block, zipped)
				f.a.NeonZip2S(zipped, a, b)
				f.a.StrQ(d, int32(dOff)+block+floatMapVectorBytes, zipped)
				f.releaseF(zipped)
				if ownA {
					releaseTemp(a)
				}
				if ownB {
					releaseTemp(b)
				}
				continue
			}
			if usesCounterValue {
				counterValue = f.allocFReg(0)
				f.fpinned = f.fpinned.add(counterValue)
				source := counter
				if block != 0 {
					f.a.AddImm32(X16, counter, uint32(block)/p.spanScale)
					source = X16
				}
				f.a.FmovFromGpr(counterValue, source, false)
				f.a.NeonDupS(counterValue, counterValue)
				f.a.NeonAddS(counterValue, counterValue, laneCounters)
			}
			for _, output := range outputs[:outputCount] {
				value, owned := emit(output.value)
				if output.stride > 1 {
					width := uint32(4)
					if p.wide {
						width = 8
					}
					for lane := uint32(0); lane < 16/width; lane++ {
						address := uint64(output.offset) + (uint64(block)+uint64(lane*width))*uint64(output.stride)
						base, disp := output.reg, int32(0)
						if address%uint64(width) == 0 && address/uint64(width) <= 0xfff {
							disp = int32(address)
						} else {
							f.floatMapOffset(X17, output.reg, address, true, X16)
							if lane != 0 {
								f.a.St1Lane(value, X17, uint8(lane), p.wide)
								continue
							}
							base = X17
						}
						if lane == 0 {
							f.a.StrF(base, disp, value, p.wide)
						} else if p.wide {
							f.a.NeonUmovD(X16, value, byte(lane))
							f.a.Store64(X16, base, uint32(disp))
						} else {
							f.a.NeonUmovS(X16, value, byte(lane))
							f.a.Store32(X16, base, uint32(disp))
						}
					}
				} else {
					f.a.StrQ(output.reg, int32(output.offset)+block, value)
				}
				if final && block+floatMapVectorBytes == int32(width) {
					f.a.NeonUmovS(X16, value, 3)
					for i, cell := range p.locals[:p.localN] {
						if exit.fromStore&(uint32(1)<<i) == 0 {
							continue
						}
						x := int(cell.Index)
						if reg, _, pinned := f.pinReg(x); pinned {
							f.a.MovReg32(reg, X16)
							f.markLocalDirty(x)
						} else {
							f.st64(SP, f.localOff(x), X16)
							f.locals[x].state = lsMem
						}
						f.setFactsForLocal(x, 0)
					}
				}
				if owned {
					f.fpinned = f.fpinned.remove(value)
					f.releaseF(value)
				}
			}
			if cachedLoad != regNone {
				releaseTemp(cachedLoad)
				cachedLoad = regNone
			}
			if usesCounterValue {
				releaseTemp(counterValue)
			}
		}
		for i := 0; i < ng; i++ {
			if groups[i].stride > 1 {
				f.floatMapOffset(groups[i].reg, groups[i].reg, uint64(width)*uint64(groups[i].stride), true, X16)
			} else {
				f.a.AddImm64(groups[i].reg, groups[i].reg, width)
			}
		}
		f.a.AddImm32(counter, counter, width/p.spanScale)
		f.a.SubImm32(span, span, width)
	}
	scalarReserve := p.step
	if exit.fromStore != 0 {
		scalarReserve = p.packet
	} else if exit.written != 0 {
		scalarReserve = 0
	}
	// Choose a small native packet menu independently of the source unroll.
	// Larger source packets already have a bounded replicated body.
	wideWidth := 2 * p.packet
	if floatMapFourPacketsEnabled && p.packet == floatMapVectorBytes {
		wideWidth = 4 * p.packet
	}
	f.cmpImm(span, 4*wideWidth+scalarReserve, false)
	short := f.a.Bcond(condB)
	if floatMapLatchEnabled {
		wideLoop := f.a.Len()
		packet(wideWidth, false)
		f.cmpImm(span, wideWidth+scalarReserve, false)
		f.patchBranch19(f.a.Bcond(condAE), wideLoop)
		f.patchBranch19(short, f.a.Len())
		f.cmpImm(span, p.packet+scalarReserve, false)
		done := f.a.Bcond(condB)
		compactLoop := f.a.Len()
		packet(p.packet, false)
		f.cmpImm(span, p.packet+scalarReserve, false)
		f.patchBranch19(f.a.Bcond(condAE), compactLoop)
		f.patchBranch19(done, f.a.Len())
	} else {
		wideLoop := f.a.Len()
		f.cmpImm(span, wideWidth+scalarReserve, false)
		tail := f.a.Bcond(condB)
		packet(wideWidth, false)
		f.patchBranch26(f.a.Branch(), wideLoop)
		f.patchBranch19(short, f.a.Len())
		f.patchBranch19(tail, f.a.Len())
		loop := f.a.Len()
		f.cmpImm(span, p.packet+scalarReserve, false)
		done := f.a.Bcond(condB)
		packet(p.packet, false)
		f.patchBranch26(f.a.Branch(), loop)
		f.patchBranch19(done, f.a.Len())
	}
	if exit.fromStore != 0 {
		// Capture load-derived live-outs only in the final vector packet.
		// The normal packet body and the scalar remainder are unchanged.
		f.cmpImm(span, p.packet, false)
		remainder := f.a.Bcond(condNE)
		packet(p.packet, true)
		f.patchBranch19(remainder, f.a.Len())
		f.stats.peep("integer-load-map-vector-live-out")
	}
	f.stats.peep("float-map-loop-unroll")
	if p.normalizedInduction {
		var published uint32
		for _, g := range groups[:ng] {
			if g.index == 0 {
				continue
			}
			local := g.index - 1
			slot := p.localSlot(local)
			bit := uint32(1) << slot
			if p.byteIndices&bit == 0 || published&bit != 0 {
				continue
			}
			published |= bit
			// g.reg is hostMemory + wrap(base + oldIndex + min) + bytesDone.
			// Range guards excluded wrap during the prefix. Recover the exact
			// modular source index before the original scalar packet resumes.
			f.a.Sub64(X16, g.reg, linMemReg)
			f.a.MovImm64(X17, uint64(uint32(g.min)))
			f.a.Sub32(X16, X16, X17)
			read(g.base, X17)
			f.a.Sub32(X16, X16, X17)
			if reg, _, pin := f.pinReg(local); pin {
				f.a.MovReg32(reg, X16)
				f.markLocalDirty(local)
			} else {
				f.st64(SP, f.localOff(local), X16)
				f.locals[local].state = lsMem
			}
			f.setFactsForLocal(local, 0)
		}
	}
	if p.counterScale > 1 {
		f.a.MovImm32(X16, int32(p.counterScale))
		f.a.Mul32(counter, counter, X16)
		f.stats.peep("float-map-scatter-vector")
	}
	if reg, _, pin := f.pinReg(p.counter); pin {
		f.a.MovReg32(reg, counter)
		f.markLocalDirty(p.counter)
	} else {
		f.st64(SP, f.localOff(p.counter), counter)
		f.locals[p.counter].state = lsMem
	}
	if exit.written != 0 {
		for i := 0; i < p.localN; i++ {
			cell := p.locals[i]
			if (exit.extra&^exit.fromStore)&(uint32(1)<<i) == 0 {
				continue
			}
			x := int(cell.Index)
			coefficient := exit.coeff[i]
			bias := exit.bias[i] - p.counterStep*coefficient
			switch coefficient {
			case 0:
				f.a.MovImm32(X16, int32(bias))
			case 1:
				f.floatMapOffset(X16, counter, uint64(bias), false, X17)
			default:
				f.a.MovImm32(X17, int32(coefficient))
				f.a.MovImm32(X16, int32(bias))
				f.a.Madd32(X16, counter, X17, X16)
			}
			if exit.invariant&(uint32(1)<<i) != 0 {
				term := exit.source[i]
				read(int(term.local), X17)
				if term.sub {
					f.a.SubShiftedReg(X16, X16, X17, a64.RegShiftLSL, term.shift, true)
				} else {
					f.a.AddShifted(X16, X16, X17, term.shift, true)
				}
			}
			if reg, _, pinned := f.pinReg(x); pinned {
				f.a.MovReg32(reg, X16)
				f.markLocalDirty(x)
			} else {
				f.st64(SP, f.localOff(x), X16)
				f.locals[x].state = lsMem
			}
			f.setFactsForLocal(x, 0)
		}
		var done int
		if clipInvariantPrefix {
			// A zero vector span can be the clipped limit. Only skip the
			// original loop after reaching its actual source bound.
			readLimit(X16, X17)
			f.cmpRR(counter, X16, false)
			done = f.a.Bcond(condE)
		} else {
			done = f.a.Cbz32(span)
		}
		f.vectorLoopExit = &vectorLoopJoin{depth: len(f.ctrl) + 1, site: done, written: exit.written}
		f.stats.peep("integer-load-map-affine-exit")
	}
	for i := 0; i < nGuards; i++ {
		f.patchBranch19(guards[i], f.a.Len())
	}
	for i := 0; i < nv; i++ {
		f.releaseF(vectors[i])
	}
	f.setFactsForLocal(p.counter, 0)
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	if p.integer {
		if clipInvariantPrefix {
			f.stats.peep("integer-load-map-invariant-prefix")
		}
		if p.step > 4 && p.extraStoreN == 0 {
			f.stats.peep("integer-load-map-packed-adjacent")
		}
		f.stats.peep("integer-load-map-vector")
	} else {
		f.stats.peep("float-map-loop-vector")
	}
}

// compareFloatMapSpan compares a guarded byte span with an exact stream
// separation. X16 is available as scratch during map disambiguation.
func (f *fn) compareFloatMapSpan(span Reg, distance uint32) {
	if f.fitsAddSubImmediate(int64(distance)) {
		f.cmpImmS(span, int64(distance), false)
		return
	}
	f.a.MovImm64(X16, uint64(distance))
	f.cmpRR(span, X16, false)
}

func mapFloatType(wide bool) machineType {
	if wide {
		return mtF64
	}
	return mtF32
}

// Each native packet must end on both a vector boundary and an original
// source-iteration boundary. This leaves every final changed local to the
// original scalar packet, including loops unrolled by an odd number of lanes.
func floatMapPacketWidth(step uint32) uint32 {
	if step == 0 || step > 64 {
		return 0
	}
	a, b := uint32(floatMapVectorBytes), step
	for b != 0 {
		a, b = b, a%b
	}
	width := floatMapVectorBytes / a * step
	// Bound duplicated native recipes independently of the source scan budget.
	if width > 80 {
		return 0
	}
	return width
}

// Preserve the original fixed byte-counter recipe before trying broader induction.
func (p *floatMapPlan) finishFloatInduction(condition, ns int, initialRead []bool) bool {
	increment := condition
	if condition := p.nodes[increment]; condition.op == 0x47 {
		increment = condition.left()
		limit := p.nodes[condition.right()]
		if limit.op != 0x41 {
			increment = condition.right()
			limit = p.nodes[condition.left()]
		}
		if limit.op != 0x41 {
			return false
		}
		p.limit = uint32(limit.bits)
	}
	inc := p.nodes[increment]
	if inc.op != 0x6a {
		return false
	}
	local, stride := p.nodes[inc.left()], p.nodes[inc.right()]
	if local.op != 0x20 {
		local, stride = stride, local
	}
	size := uint32(4)
	if p.wide {
		size = 8
	}
	if local.op != 0x20 || p.locals[p.localSlot(local.left())].Type != mtI32 || stride.op != 0x41 {
		return false
	}
	step := size * uint32(ns)
	scale := uint32(1)
	if stride.bits != uint64(step) {
		if !floatMapScatterEnabled || step == 0 || stride.bits > 1<<24 || stride.bits%uint64(step) != 0 {
			return false
		}
		scale = uint32(stride.bits / uint64(step))
		if scale <= 1 || scale > 1<<16 || p.limit == 0 || p.limit%scale != 0 || p.scatterBudget <= p.work {
			return false
		}
		p.scatterCandidate = true
		p.workLimit = min(p.workLimit, p.scatterBudget)
	}
	p.counter, p.step = local.left(), step
	p.packet = floatMapPacketWidth(p.step)
	if p.packet == 0 {
		return false
	}
	if p.locals[p.localSlot(p.counter)].Value != increment {
		return false
	}
	for i, cell := range p.locals[:p.localN] {
		if scale > 1 && !p.spend() {
			return false
		}
		if cell.Written && initialRead[i] && int(cell.Index) != p.counter {
			return false
		}
	}
	p.counterScale = scale
	p.limit /= scale
	return true
}
