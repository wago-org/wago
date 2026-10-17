//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// gcStructReduction proves a complete, effect-free unsigned induction sum whose
// per-iteration scalar comes from an immediately consumed fresh struct. Only
// allocation remains observable. The narrow whole-function proof excludes
// escaping identities, changed roots, interrupts, traps and intermediate stores.
func (f *fn) gcStructReduction(code []byte) (uint32, bool) {
	if !f.gcStructHelpers || len(f.customInstructions) != 0 || f.interruptible || f.localBase != 0 || f.nParams != 1 || len(f.ft.Results) != 1 || f.ft.Results[0] != wasm.I32 || f.ft.Params[0] != wasm.I32 {
		return 0, false
	}
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		return 0, false
	}
	for _, typ := range f.localType {
		if typ != mtI32 {
			return 0, false
		}
	}
	r := wasm.ReaderFrom(code)
	byteIs := func(want byte) bool { got, err := r.Byte(); return err == nil && got == want }
	local := func() (uint32, bool) {
		if !byteIs(0x20) {
			return 0, false
		}
		v, err := r.U32()
		return v, err == nil && int(v) < len(f.localType)
	}
	immIs := func(want uint32) bool { got, err := r.U32(); return err == nil && got == want }
	if !byteIs(0x03) || !byteIs(0x40) {
		return 0, false
	}
	acc, ok := local()
	if !ok || acc == 0 {
		return 0, false
	}
	// Initializers are only local reads and integer constants, so skipping the
	// unused fields cannot erase a trap or a state change.
	var locals [8]int
	fields := 0
	for fields < len(locals) {
		op, ok := r.Peek()
		if !ok {
			return 0, false
		}
		if op == 0xfb {
			break
		}
		if op == 0x20 {
			x, ok := local()
			if !ok {
				return 0, false
			}
			locals[fields] = int(x)
		} else if op == 0x41 {
			_, _ = r.Byte()
			if _, err := r.I32(); err != nil {
				return 0, false
			}
			locals[fields] = -1
		} else {
			return 0, false
		}
		fields++
	}
	if !byteIs(0xfb) || !immIs(0) {
		return 0, false
	}
	source, err := r.U32()
	if err != nil {
		return 0, false
	}
	st, ok := f.stagedStructType(source)
	if !ok || len(st.Comp.Fields) != fields {
		return 0, false
	}
	for _, field := range st.Comp.Fields {
		if field.Storage().Packed() || field.Storage().Val() != wasm.I32 {
			return 0, false
		}
	}
	if !byteIs(0xfb) {
		return 0, false
	}
	sub, err := r.U32()
	if err != nil {
		return 0, false
	}
	if sub == 22 || sub == 23 {
		target, exact, err := readRefHeapTypeImmediate(&r)
		if err != nil || target < 0 || (exact && uint32(target) != source) || (!exact && !f.constructorSubtypeProof(source, uint32(target))) {
			return 0, false
		}
		if !byteIs(0xfb) {
			return 0, false
		}
		sub, err = r.U32()
		if err != nil {
			return 0, false
		}
	}
	if sub != 2 {
		return 0, false
	}
	access, err := r.U32()
	if err != nil || !f.constructorSubtypeProof(source, access) {
		return 0, false
	}
	field, err := r.U32()
	if err != nil || int(field) >= fields {
		return 0, false
	}
	counter := locals[field]
	if counter <= 0 || uint32(counter) == acc {
		return 0, false
	}
	if !byteIs(0x6a) || !byteIs(0x21) || !immIs(acc) {
		return 0, false
	}
	x, ok := local()
	if !ok || x != uint32(counter) {
		return 0, false
	}
	if !byteIs(0x41) {
		return 0, false
	}
	step, err := r.I32()
	if err != nil || step != 1 {
		return 0, false
	}
	if !byteIs(0x6a) || !byteIs(0x21) || !immIs(uint32(counter)) {
		return 0, false
	}
	x, ok = local()
	if !ok || x != uint32(counter) {
		return 0, false
	}
	x, ok = local()
	if !ok || x != 0 {
		return 0, false
	}
	if !byteIs(0x49) || !byteIs(0x0d) || !immIs(0) || !byteIs(0x0b) {
		return 0, false
	}
	x, ok = local()
	if !ok || x != acc || !byteIs(0x0b) {
		return 0, false
	}
	if _, more := r.Peek(); more {
		return 0, false
	}
	return source, true
}

// Reduced functions have no native loop or guest call. Publish their segment
// bound after ordinary call-graph resolution: callers of the original looping
// body must not inherit this separate reduced-entry contract.
func mergeGCReducedBounds(ordinary, reduced []uint64) []uint64 {
	if len(ordinary) == 0 {
		return reduced
	}
	for i, bits := range reduced {
		ordinary[i] |= bits
	}
	return ordinary
}

func (f *fn) constructorSubtypeProof(source, target uint32) bool {
	actual := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: source}), false)
	required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: target}), false)
	return f.m.ReferenceTypeSubtype(actual, required)
}
