//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// This is a proof of reference validity, not a dynamic-type proof. It reads
// validated original bodies, never changes calls, and rejects unsupported work.
// Its fixed state and shared instruction budget bound recursive compile work.
type gcStoredProofState struct {
	stack, locals uint32
	n             uint8
}

func (s *gcStoredProofState) push(known bool) bool {
	if s.n == 32 {
		return false
	}
	bit := uint32(1) << s.n
	s.stack &^= bit
	if known {
		s.stack |= bit
	}
	s.n++
	return true
}

func (s *gcStoredProofState) pop(n int) bool {
	if n < 0 || n > int(s.n) {
		return false
	}
	s.n -= uint8(n)
	s.stack &= (uint32(1) << s.n) - 1
	return true
}

func (f *fn) provedStoredGCCallResult(index uint32) bool {
	if !f.gcLocalTypeFactsEnabled() {
		return false
	}
	ft, ok := f.functionSignature(index)
	if !ok || len(ft.Results) != 1 || !gcFrameRefType(f.m, ft.Results[0]) {
		return false
	}
	key := (uint64(index) + 1) << 2
	for _, cached := range f.gcStoredReturnCache {
		if cached&^3 == key {
			return cached&2 != 0
		}
	}
	budget := 1024
	proved := f.proveStoredGCReturn(index, 0, &budget)
	value := key | 1
	if proved {
		value |= 2
	}
	f.gcStoredReturnCache[f.gcStoredReturnNext] = value
	f.gcStoredReturnNext = (f.gcStoredReturnNext + 1) % uint8(len(f.gcStoredReturnCache))
	return proved
}

func (f *fn) proveStoredGCReturn(index uint32, depth int, budget *int) bool {
	local := int(index) - f.m.ImportedFuncCount()
	if depth >= 3 || local < 0 || local >= len(f.m.Code) {
		return false
	}
	ft, ok := f.m.LocalFuncType(local)
	if !ok || len(ft.Results) != 1 || !gcFrameRefType(f.m, ft.Results[0]) {
		return false
	}
	body := &f.m.Code[local]
	if len(body.BodyBytes) > 256 {
		return false
	}
	nLocals := uint64(len(ft.Params))
	for _, run := range body.Locals.Runs {
		nLocals += uint64(run.Count)
	}
	if nLocals > 32 {
		return false
	}
	var s gcStoredProofState
	var control [4]struct {
		before, left gcStoredProofState
		hasElse      bool
	}
	controls := 0
	r := wasm.ReaderFrom(body.BodyBytes)
	for r.Offset() < len(body.BodyBytes) {
		*budget--
		if *budget < 0 {
			return false
		}
		op, err := r.Byte()
		if err != nil {
			return false
		}
		switch op {
		case 0x20, 0x21, 0x22:
			i, err := r.U32()
			if err != nil || uint64(i) >= nLocals {
				return false
			}
			if op == 0x20 {
				if !s.push(s.locals&(1<<i) != 0) {
					return false
				}
			} else {
				if s.n == 0 {
					return false
				}
				known := s.stack&(1<<(s.n-1)) != 0
				s.locals &^= 1 << i
				if known {
					s.locals |= 1 << i
				}
				if op == 0x21 && !s.pop(1) {
					return false
				}
			}
		case 0x41, 0x42:
			if op == 0x41 {
				_, err = r.I32()
			} else {
				_, err = r.I64()
			}
			if err != nil || !s.push(false) {
				return false
			}
		case 0xd0:
			heap, err := r.S33()
			if err != nil || !s.push(heap >= -22 && heap <= -18 || heap == -15) {
				return false
			}
		case 0x1a:
			if !s.pop(1) {
				return false
			}
		case 0x4f, 0x6a:
			if !s.pop(2) || !s.push(false) {
				return false
			}
		case 0x04:
			if !s.pop(1) || controls == len(control) {
				return false
			}
			block, err := r.BlockType()
			if err != nil || block.Kind == wasm.BlockTypeIndex {
				return false
			}
			control[controls].before = s
			control[controls].hasElse = false
			controls++
		case 0x05:
			if controls == 0 || control[controls-1].hasElse {
				return false
			}
			c := &control[controls-1]
			c.left, c.hasElse = s, true
			s = c.before
		case 0x0b:
			if controls == 0 {
				return r.Offset() == len(body.BodyBytes) && s.n == 1 && s.stack&1 != 0
			}
			controls--
			c := &control[controls]
			other := c.before
			if c.hasElse {
				other = c.left
			}
			if s.n != other.n {
				return false
			}
			s.stack &= other.stack
			s.locals &= other.locals
		case 0x10:
			callee, err := r.U32()
			if err != nil || int(callee) < f.m.ImportedFuncCount() {
				return false
			}
			typ, ok := f.m.LocalFuncType(int(callee) - f.m.ImportedFuncCount())
			if !ok || !s.pop(len(typ.Params)) || len(typ.Results) > 1 {
				return false
			}
			if len(typ.Results) == 1 {
				known := gcFrameRefType(f.m, typ.Results[0]) && f.proveStoredGCReturn(callee, depth+1, budget)
				if !s.push(known) {
					return false
				}
			}
		case 0xfb:
			sub, err := r.U32()
			if err != nil {
				return false
			}
			switch sub {
			case 22, 23:
				heap, _, err := readRefHeapTypeImmediate(&r)
				if err != nil || heap < 0 || uint64(heap) > uint64(^uint32(0)) {
					return false
				}
				typ, ok := f.stagedGCType(uint32(heap))
				if !ok || typ.Comp.Kind != wasm.CompStruct && typ.Comp.Kind != wasm.CompArray || !s.pop(1) || !s.push(true) {
					return false
				}
			case 2, 5:
				typ, err := r.U32()
				if err != nil {
					return false
				}
				field, err := r.U32()
				if err != nil {
					return false
				}
				ft, ok := f.stagedStructField(typ, field)
				if !ok {
					return false
				}
				if sub == 5 {
					if !s.pop(2) {
						return false
					}
				} else if !s.pop(1) || !s.push(nativeGCCollectorRefStorage(f.m, typ, ft.Storage())) {
					return false
				}
			case 11:
				typ, err := r.U32()
				if err != nil {
					return false
				}
				field, ok := f.stagedArrayType(typ)
				if !ok || !s.pop(2) || !s.push(nativeGCCollectorRefStorage(f.m, typ, field.Storage())) {
					return false
				}
			case 15:
				if !s.pop(1) || !s.push(false) {
					return false
				}
			case 28:
				if !s.pop(1) || !s.push(true) {
					return false
				}
			default:
				return false
			}
		default:
			return false
		}
	}
	return false
}

// Only nullable abstract fields may use this validity proof. Typed fields and
// nonnullable fields retain their checks, as do shared constructors and helpers.
func (f *fn) gcStructInitializerProofMask(typ uint32, roots []*elem, first int) uint64 {
	if !f.gcLocalTypeFactsEnabled() {
		return 0
	}
	st, ok := f.stagedStructType(typ)
	if !ok || len(st.Comp.Fields) > 64 || first < 0 || first+len(st.Comp.Fields) > len(roots) {
		return 0
	}
	var mask uint64
	for i, field := range st.Comp.Fields {
		val := field.Storage().Val()
		if val.Kind() != wasm.ValRef || !val.Ref().Nullable() || val.Ref().Heap().Kind() != wasm.HeapAbs {
			continue
		}
		heap := val.Ref().Heap().Abs()
		if heap != wasm.HeapAny && heap != wasm.HeapEq {
			continue
		}
		root := roots[first+i]
		proved := root.st.valueFacts().has(factGCStoredRef)
		if local, ok := gcLocalProvenance(root); ok {
			if fact, ok := f.gcLocalTypeFacts.get(local); ok {
				proved = proved || fact.stored()
			}
		}
		if proved {
			mask |= uint64(1) << i
		}
	}
	return mask
}
