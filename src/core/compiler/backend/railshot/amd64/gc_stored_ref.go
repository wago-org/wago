//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// recordStoredRefOrigin only follows a generated ref.null, ref.i31, or successful
// struct constructor to the immediately following instruction. Validation proves
// assignability at that use. No host bits, local facts, loaded fields, merges,
// or raw object addresses enter this proof. The compact operand stays a GC root.
func (f *fn) recordStoredRefOrigin(r *wasm.Reader) {
	if !gcStoredRefElisionEnabled || !f.sc.validatedGCStores {
		return
	}
	f.gcStoredRefOrigin = f.s.back()
	f.gcStoredRefEnd = f.tracePCBase + uint32(r.Offset())
	f.gcStoredRefFunc = f.traceFuncIdx
}
