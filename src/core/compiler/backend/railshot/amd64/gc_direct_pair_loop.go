//go:build amd64

package amd64

// Exactly two immutable static receiver sites bind remaining2/1 to their
// separate caches even when canonical spilling erased local provenance.
// Existing policy6 admission authorizes typed references; null and logical
// bounds remain at each original access, not speculative loop entry.
func (f *fn) useDirectPairLoop(object *elem, t uint32, local int) (Reg, func()) {
	l := &f.directLoop
	if !gcDirectPairLoop || l.kind != loopLeaseDirectPair || l.remaining == 0 || l.remaining > 2 || f.wasmPC >= l.endPC || t != l.typeIndex {
		panic("amd64: invalid paired direct lease")
	}
	expected, cache := l.local&65535, l.object
	if l.remaining == 1 {
		expected, cache = l.local>>16, l.length
	}
	if local >= 0 && uint32(local) != expected {
		panic("amd64: paired direct receiver mismatch")
	}
	l.remaining--
	f.a.TestSelf(cache, true)
	ready := f.a.JccPlaceholder(condNE)
	obj, done := f.emitTrustedDirectOffset(object)
	f.a.MovReg64(cache, obj)
	done()
	f.a.PatchRel32(ready, f.a.Len())
	f.stats.peep("gc-direct-pair-loop-resolve")
	return cache, func() {}
}
