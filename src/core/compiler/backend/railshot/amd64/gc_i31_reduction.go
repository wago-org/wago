//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// gcI31Reduction proves a whole unsigned induction sum. The i31 identity is
// immediately consumed; no allocation, trap, call, or externally visible store
// can be removed. Local indexes are inferred, not tied to a particular module.
func (f *fn) gcI31Reduction(code []byte) bool {
	if f.interruptible || f.localBase != 0 || len(f.customInstructions) != 0 || f.nParams != 1 || len(f.ft.Results) != 1 || f.ft.Params[0] != wasm.I32 || f.ft.Results[0] != wasm.I32 {
		return false
	}
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		return false
	}
	for _, typ := range f.localType {
		if typ != mtI32 {
			return false
		}
	}
	r := wasm.ReaderFrom(code)
	op := func(want byte) bool { b, err := r.Byte(); return err == nil && b == want }
	imm := func(want uint32) bool { x, err := r.U32(); return err == nil && x == want }
	local := func() (uint32, bool) {
		if !op(0x20) {
			return 0, false
		}
		x, err := r.U32()
		return x, err == nil && int(x) < len(f.localType)
	}
	if !op(0x03) || !op(0x40) {
		return false
	}
	acc, ok := local()
	if !ok || acc == 0 {
		return false
	}
	counter, ok := local()
	if !ok || counter == 0 || counter == acc {
		return false
	}
	if !op(0xfb) || !imm(28) || !op(0xfb) || !imm(30) || !op(0x6a) || !op(0x21) || !imm(acc) {
		return false
	}
	x, ok := local()
	if !ok || x != counter || !op(0x41) {
		return false
	}
	step, err := r.I32()
	if err != nil || step != 1 {
		return false
	}
	if !op(0x6a) || !op(0x21) || !imm(counter) {
		return false
	}
	x, ok = local()
	if !ok || x != counter {
		return false
	}
	x, ok = local()
	if !ok || x != 0 {
		return false
	}
	if !op(0x49) || !op(0x0d) || !imm(0) || !op(0x0b) {
		return false
	}
	x, ok = local()
	if !ok || x != acc || !op(0x0b) {
		return false
	}
	_, more := r.Peek()
	return !more
}
