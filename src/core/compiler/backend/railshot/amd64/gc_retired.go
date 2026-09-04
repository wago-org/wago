//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// These compact stubs keep the canonical lowering call sites simple while the
// retired fact engine is removed file by file. They never retain semantic state
// or select alternate code.
func gcKnownI32Const(e *elem) (uint32, bool) {
	if e == nil || e.kind != ekValue || e.st.kind != stConst || e.st.typ != mtI32 {
		return 0, false
	}
	return uint32(e.st.cval), true
}

func (f *fn) gcKnownArrayIndexInBounds(_, _ *elem) (uint32, uint32, bool) {
	return 0, 0, false
}

func (f *fn) gcRefFactsEnabled() bool { return false }

func gcRefFact(*elem) shared.GCRefFact         { return shared.GCRefFact{} }
func (f *fn) gcRefFact(*elem) shared.GCRefFact { return shared.GCRefFact{} }
func putGCRefFact(*storage, shared.GCRefFact)  {}

func (f *fn) markGCRefFact(e *elem, _ shared.GCRefFact) {
	if e != nil && e.kind == ekValue {
		e.st.setGCRoot(true)
	}
}

func (f *fn) gcRefFactMatchesHeap(shared.GCRefFact, int64, bool) (bool, bool) {
	return false, false
}
func (f *fn) gcRefFactMatchesTarget(shared.GCRefFact, int64, bool, bool) (bool, bool) {
	return false, false
}

func zeroGCRefFactForValType(*wasm.Module, wasm.ValType) shared.GCRefFact {
	return shared.GCRefFact{}
}

func (f *fn) declaredGCRefFact(wasm.ValType) shared.GCRefFact          { return shared.GCRefFact{} }
func (f *fn) seedFinalGCParameterTypes([]wasm.ValType, uint32, uint32) {}
func (f *fn) markLocalGetExactGCType(*elem, int)                       {}
func (f *fn) setLocalExactGCType(int, *elem) (shared.GCRefFact, bool) {
	return shared.GCRefFact{}, false
}
func (f *fn) clearLocalExactGCTypes()                                        {}
func (f *fn) snapshotGCRefFacts() []shared.GCRefFact                         { return nil }
func (f *fn) mergeGCRefFactsInto(*[]shared.GCRefFact)                        {}
func (f *fn) installGCRefFacts([]shared.GCRefFact)                           {}
func (f *fn) freeGCRefFactBuf([]shared.GCRefFact)                            {}
func (f *fn) invalidateLoopModifiedGCRefFacts([]uint16)                      {}
func (f *fn) publishGCRef(*elem)                                             {}
func (f *fn) publishGCStoredChild(*elem, *elem)                              {}
func (f *fn) publishAllFreshGCRefs()                                         {}
func (f *fn) prepareGCLoadResultCapture(byte)                                {}
func (f *fn) captureGCLoadResultLocal(_ *elem, local int)                    { f.invalidateGCLoadFactsForLocal(local) }
func (f *fn) tryForwardGCArrayLen(uint32) bool                               { return false }
func (f *fn) observeGCArrayLen(uint32)                                       {}
func (f *fn) recordGCArrayLenResult()                                        {}
func (f *fn) tryForwardGCImmutableStructGet(uint32, uint32) bool             { return false }
func (f *fn) observeGCStructGet(uint32, uint32, bool)                        {}
func (f *fn) recordGCStructGetResult()                                       {}
func (f *fn) tryForwardGCStructSetGet(uint32, uint32) bool                   { return false }
func (f *fn) recordGCConstructorConstant(uint32, uint32, bool, *elem, *elem) {}
func (f *fn) recordGCStructSetConstant(*elem)                                {}
func (f *fn) observeGCStructSet(*elem, uint32, uint32)                       {}
func (f *fn) topExactGCLocal() (int, uint32, bool)                           { return 0, 0, false }
func (f *fn) refineGCDereferencedObject(*elem)                               {}
func (f *fn) refineTopLocalExactGCType(uint32)                               {}

func (f *fn) markTopConstructorGCRefFact(uint32, *uint32) {
	if e := f.s.back(); e != nil && e.kind == ekValue {
		e.st.setGCRoot(true)
	}
}

func (f *fn) markTopExactGCType(uint32) {
	if e := f.s.back(); e != nil && e.kind == ekValue {
		e.st.setGCRoot(true)
	}
}

func (f *fn) recordGCBarrierState(state shared.GCBarrierState) {
	switch state {
	case shared.GCBarrierNoBarrier:
		f.stats.peep("gc-barrier-none")
	case shared.GCBarrierYoungParent:
		f.stats.peep("gc-barrier-young-parent")
	case shared.GCBarrierKnownOldChild:
		f.stats.peep("gc-barrier-known-old-child")
	case shared.GCBarrierExistingCard:
		f.stats.peep("gc-barrier-existing-card")
	case shared.GCBarrierCardMark:
		f.stats.peep("gc-barrier-card-mark")
	case shared.GCBarrierSlowBarrier:
		f.stats.peep("gc-barrier-slow")
	}
}
