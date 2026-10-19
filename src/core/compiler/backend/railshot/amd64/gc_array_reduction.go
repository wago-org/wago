//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// gcArrayReduction proves the complete unsigned induction sum of immediately
// consumed uniform arrays. The masked index is in bounds, identities cannot
// escape, and allocation is the only remaining effect.
func (f *fn) gcArrayReduction(code []byte) (uint32, uint32, bool) {
	if !f.gcArrayHelpers || len(f.customInstructions) != 0 || f.interruptible || f.localBase != 0 || f.nParams != 1 || len(f.ft.Results) != 1 || f.ft.Results[0] != wasm.I32 || f.ft.Params[0] != wasm.I32 {
		return 0, 0, false
	}
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		return 0, 0, false
	}
	for _, typ := range f.localType {
		if typ != mtI32 {
			return 0, 0, false
		}
	}
	r := wasm.ReaderFrom(code)
	b := func(want byte) bool { v, err := r.Byte(); return err == nil && v == want }
	u := func(want uint32) bool { v, err := r.U32(); return err == nil && v == want }
	local := func() (uint32, bool) {
		if !b(0x20) {
			return 0, false
		}
		v, err := r.U32()
		return v, err == nil && int(v) < len(f.localType)
	}
	if !b(0x03) || !b(0x40) {
		return 0, 0, false
	}
	acc, ok := local()
	if !ok || acc == 0 {
		return 0, 0, false
	}
	counter, ok := local()
	if !ok || counter == 0 || counter == acc || !b(0x41) {
		return 0, 0, false
	}
	length, err := r.I32()
	if err != nil || length <= 0 || length > 64 || length&(length-1) != 0 || !b(0xfb) || !u(6) {
		return 0, 0, false
	}
	source, err := r.U32()
	if err != nil {
		return 0, 0, false
	}
	typ, ok := f.stagedArraySubtype(source)
	if !ok || !typ.Final {
		return 0, 0, false
	}
	field := typ.Comp.Array
	if field.Storage().Packed() || field.Storage().Val() != wasm.I32 {
		return 0, 0, false
	}
	index, ok := local()
	if !ok || index != counter || !b(0x41) {
		return 0, 0, false
	}
	mask, err := r.I32()
	if err != nil || mask != length-1 || !b(0x71) || !b(0xfb) || !u(11) || !u(source) {
		return 0, 0, false
	}
	if !b(0x6a) || !b(0x21) || !u(acc) {
		return 0, 0, false
	}
	x, ok := local()
	if !ok || x != counter || !b(0x41) {
		return 0, 0, false
	}
	step, err := r.I32()
	if err != nil || step != 1 || !b(0x6a) || !b(0x21) || !u(counter) {
		return 0, 0, false
	}
	x, ok = local()
	if !ok || x != counter {
		return 0, 0, false
	}
	x, ok = local()
	if !ok || x != 0 || !b(0x49) || !b(0x0d) || !u(0) || !b(0x0b) {
		return 0, 0, false
	}
	x, ok = local()
	if !ok || x != acc || !b(0x0b) {
		return 0, 0, false
	}
	if _, more := r.Peek(); more {
		return 0, 0, false
	}
	return source, uint32(length), true
}
