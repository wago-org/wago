//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"math/bits"
	"sync"
	"sync/atomic"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Foreign anyrefs use compact collector handles. These private empty objects
// are not guest structs: the weak record supplies their extern identity and
// must be consulted before abstract or concrete reference classification.
// This bridge remains private until all opaque extern roots are traced.
type compactExternCollector struct {
	mu        sync.Mutex
	collector *gc.Collector
	store     *referenceStore
	typeID    gc.TypeID
	foreign   map[gc.Ref]uint64
	byToken   map[uint64]gc.Ref
	reclaimed []uint64
	shellType gc.TypeID
	shells    map[gc.Ref]compactExternShell
	byValue   map[gc.Ref]uint64
	scratch   *compactExternShellScratch
	rootSlot  gc.Root // reused only under this collector's mutator lock
	owners    map[*Instance]*compactExternPersistentOwner
	bulk      []compactExternPreparedWord
	bulkRoots *compactExternPreparedRoots
	hostSlots *compactExternHostSlotPool
}

type compactExternShell struct {
	word  uint64
	value gc.Ref
}

type compactExternPublicToken struct {
	root gcRefTokenEntry
	word uint64
}

func (v *compactExternPublicToken) nativeExternWord() uint64 { return v.word }

func (v *compactExternPublicToken) externrefGCRoot() gcRefTokenEntry { return v.root }
func (v *compactExternPublicToken) externrefPublicValue() any        { return v.word }

// Called at a public result boundary, after native execution has returned.
// The public token roots the shell, including an immediate i31 shell, until
// release or Runtime shutdown. Root slots are reused by the existing pool.
func (in *Instance) issueCompactExternPublicToken(word uint64) (uint64, error) {
	if in != nil && in.refStore != nil {
		if _, valid := in.refStore.resolveExternref(word); valid {
			return word, nil
		}
	}
	if in == nil || in.c == nil || in.refStore == nil || in.gc == nil || in.c.nativeGCABIRequirement() != gc.NativeABIVersion || in.c.genericGCFrameRoots() == nil {
		return 0, fmt.Errorf("compact extern result lacks exact collector ownership")
	}
	collector, ref := compactExternWordRoot(in.refStore, word)
	if collector == nil {
		if _, ok := in.refStore.resolveExternref(word); ok {
			return word, nil
		}
		return 0, fmt.Errorf("compact extern result has no live identity")
	}
	if collector != in.gc {
		return 0, fmt.Errorf("compact extern result requires source collector ownership")
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	domain := in.lockGCCollector()
	defer unlockGCCollector(domain)
	state := in.publicGCState()
	state.mu.Lock()
	defer state.mu.Unlock()
	currentCollector, currentRef := compactExternWordRoot(in.refStore, word)
	if currentCollector != collector || currentRef != ref {
		return 0, fmt.Errorf("compact extern result identity was reclaimed")
	}
	in.refStore.mu.Lock()
	registered := in.refStore.hasInstanceResourcesLocked(in)
	in.refStore.mu.Unlock()
	if !registered {
		return 0, fmt.Errorf("compact extern result producer is unregistered")
	}
	if !in.retainResourceRootMode(true) {
		return 0, fmt.Errorf("compact extern result producer is unavailable")
	}
	index := state.nextResultSlot()
	var slot uint32
	if index == state.resultRootsMade {
		var err error
		slot, err = in.gc.NewCheckedClassifiedGlobalSlot(ref, gc.RootPublicToken)
		if err != nil {
			in.releaseResourceRoot()
			return 0, err
		}
		state.appendResultRootSlot(slot)
	} else {
		slot = state.resultRootSlot(index)
		if err := in.gc.SetGlobalSlot(slot, ref); err != nil {
			in.releaseResourceRoot()
			return 0, err
		}
	}
	value := &compactExternPublicToken{word: word, root: gcRefTokenEntry{ref: ref, slot: slot, ownerIndex: index, owner: in}}
	token, err := in.refStore.issueExternref(value)
	if err != nil {
		_ = in.gc.SetGlobalSlot(slot, gc.Null())
		in.releaseResourceRoot()
		return 0, err
	}
	value.root.token = token
	state.setResultToken(index, token)
	state.resultTokenCount++
	return token, nil
}

type compactExternShellScratch struct {
	word  [1]uint64
	roots gc.InitializerWordRootScratch
}

type compactExternWordKey struct {
	store *referenceStore
	word  uint64
}

type compactExternWord struct {
	state *compactExternCollector
	ref   gc.Ref
}

var compactExternWords = struct {
	sync.RWMutex
	rows map[compactExternWordKey]compactExternWord
}{rows: make(map[compactExternWordKey]compactExternWord)}
var compactExternWordCount atomic.Uint32

func compactExternWordReserved(store *referenceStore, word uint64) bool {
	if compactExternWordCount.Load() == 0 {
		return false
	}
	compactExternWords.RLock()
	_, ok := compactExternWords.rows[compactExternWordKey{store, word}]
	compactExternWords.RUnlock()
	return ok
}

func compactExternWordRoot(store *referenceStore, word uint64) (*gc.Collector, gc.Ref) {
	if compactExternWordCount.Load() == 0 {
		return nil, gc.Null()
	}
	compactExternWords.RLock()
	entry, ok := compactExternWords.rows[compactExternWordKey{store, word}]
	compactExternWords.RUnlock()
	if !ok {
		return nil, gc.Null()
	}
	return entry.state.collector, entry.ref
}

func compactExternFrameRoot(owner *Instance, collector *gc.Collector, word uint64) (*compactExternCollector, gc.Ref) {
	if owner == nil || owner.refStore == nil || word == 0 {
		return nil, gc.Null()
	}
	var state *compactExternCollector
	ref := gc.Null()
	if compactExternWordCount.Load() != 0 {
		compactExternWords.RLock()
		entry, ok := compactExternWords.rows[compactExternWordKey{owner.refStore, word}]
		compactExternWords.RUnlock()
		if ok && entry.state.collector == collector {
			state, ref = entry.state, entry.ref
		}
	}
	if state == nil && compactExternCollectorCount.Load() != 0 {
		if candidate, ok := compactExternCollectors.Load(collector); ok && candidate.store == owner.refStore {
			state = candidate
			ref = state.byToken[word]
			if ref.IsNull() {
				owner.refStore.mu.Lock()
				value, valid := owner.refStore.externrefValueLocked(word)
				converted, convertedOK := value.(interface{ nativeExternWord() uint64 })
				owner.refStore.mu.Unlock()
				if valid && convertedOK {
					if actual, root := compactExternWordRoot(owner.refStore, converted.nativeExternWord()); actual == collector {
						ref = root
					}
				}
			}
		}
	}
	return state, ref
}
func visitCompactExternFrameRoot(owner *Instance, collector *gc.Collector, word uint64, fn func(gc.RootSlot) bool, sink gc.RootRefSink) bool {
	state, ref := compactExternFrameRoot(owner, collector, word)
	if state == nil || ref.IsNull() {
		return true
	}
	if sink != nil {
		return sink.VisitRootRef(ref)
	}
	state.rootSlot = gc.Root(ref)
	more := fn(&state.rootSlot)
	state.rootSlot = 0
	return more
}

// This typed, cold registry avoids interface boxing on instrumented helper
// paths. It owns no collector after the final reclaim notification.
type compactExternRegistry struct {
	mu   sync.RWMutex
	rows map[*gc.Collector]*compactExternCollector
}

func (r *compactExternRegistry) Load(collector *gc.Collector) (*compactExternCollector, bool) {
	r.mu.RLock()
	state, ok := r.rows[collector]
	r.mu.RUnlock()
	return state, ok
}
func (r *compactExternRegistry) Store(collector *gc.Collector, state *compactExternCollector) {
	r.mu.Lock()
	if r.rows == nil {
		r.rows = make(map[*gc.Collector]*compactExternCollector)
	}
	r.rows[collector] = state
	r.mu.Unlock()
}
func (r *compactExternRegistry) LoadAndDelete(collector *gc.Collector) (*compactExternCollector, bool) {
	r.mu.Lock()
	state, ok := r.rows[collector]
	delete(r.rows, collector)
	if len(r.rows) == 0 {
		r.rows = nil
	}
	r.mu.Unlock()
	return state, ok
}

var compactExternCollectors compactExternRegistry
var compactExternCollectorCount atomic.Uint32

func compactForeignAnyToken(collector *gc.Collector, ref gc.Ref) (uint64, bool) {
	if compactExternCollectorCount.Load() == 0 || !ref.IsObj() {
		return 0, false
	}
	if state, ok := compactExternCollectors.Load(collector); ok {
		token, foreign := state.foreign[ref]
		return token, foreign
	}
	return 0, false
}

func compactForeignAnyMatches(collector *gc.Collector, ref gc.Ref, required ValueTypeDescriptor) (bool, bool) {
	if _, foreign := compactForeignAnyToken(collector, ref); !foreign {
		return false, false
	}
	return required.Kind == ValueTypeReference && !required.Ref.Exact && !required.Ref.Heap.Defined && required.Ref.Heap.TypeIndex == 0 && required.Ref.Heap.Abstract == AbstractHeapAny, true
}

func (in *Instance) stageCompactExternAnyArgument(token uint64, required ValueTypeDescriptor) (gc.Ref, error) {
	if !in.c.compactExternFrameRoots() || required.Kind != ValueTypeReference || required.Ref.Exact || required.Ref.Heap.Defined || required.Ref.Heap.Abstract != AbstractHeapAny {
		return gc.Null(), fmt.Errorf("compact extern argument requires abstract anyref")
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	domain := in.lockGCCollector()
	defer unlockGCCollector(domain)
	public := in.publicGCState()
	public.mu.Lock()
	defer public.mu.Unlock()
	s, err := in.compactExternCollector(domain)
	if err != nil {
		return gc.Null(), err
	}
	public.frameRoots = in.gcCollectFrameRoots(public)
	ref, err := s.anyFromExtern(token, &public.frameRoots)
	if err != nil {
		return gc.Null(), err
	}
	index := public.argumentRootCount
	if index == public.argumentRootsMade {
		slot, err := in.gc.NewCheckedClassifiedGlobalSlot(ref, gc.RootForeignInstance)
		if err != nil {
			return gc.Null(), err
		}
		public.appendArgumentRootSlot(slot)
	} else if err := in.gc.SetGlobalSlot(public.argumentRootSlot(index), ref); err != nil {
		return gc.Null(), err
	}
	public.argumentRootCount++
	return ref, nil
}

func (in *Instance) dispatchCompactExternConversion(domain *gcStoreDomain, helper uint32, word uint64, roots gc.RootSet) uint64 {
	if word == 0 {
		return 0
	}
	s, err := in.compactExternCollector(domain)
	if err != nil {
		panic(gcHelperFailure(err))
	}
	if helper == gcAnyConvertExtern {
		ref, err := s.anyFromExtern(word, roots)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		return uint64(ref)
	}
	ref := gc.Ref(uint32(word))
	if uint64(ref) != word {
		panic(gcHelperFailuref("compact extern conversion contains non-compact anyref"))
	}
	value, err := s.externFromAny(ref, roots)
	if err != nil {
		panic(gcHelperFailure(err))
	}
	return value
}

// Called with the collector domain locked. Private types have no guest type
// representative and cannot match an indexed guest-defined heap type.
func (in *Instance) compactExternCollector(domain *gcStoreDomain) (*compactExternCollector, error) {
	if in.refStore == nil {
		if _, err := in.referenceStoreForBoundary(); err != nil {
			return nil, err
		}
	}
	return compactExternCollectorFor(in.gc, in.refStore, domain)
}
func compactExternCollectorFor(collector *gc.Collector, store *referenceStore, domain *gcStoreDomain) (*compactExternCollector, error) {
	if state, ok := compactExternCollectors.Load(collector); ok {
		s := state
		if s.store != store {
			return nil, fmt.Errorf("compact extern collector has a different Runtime")
		}
		return s, nil
	}
	if collector == nil || store == nil {
		return nil, fmt.Errorf("compact extern conversion has no collector or reference store")
	}
	id := gc.TypeID(collector.TypeCount())
	desc, err := gc.NewStructDesc(id, nil)
	if err != nil {
		return nil, err
	}
	if err = collector.AddTypes([]gc.TypeDesc{desc}); err != nil {
		return nil, err
	}
	if domain != nil {
		store.mu.Lock()
		domain.types = append(domain.types, desc)
		domain.typeReps = append(domain.typeReps, gcDomainTypeRepresentative{})
		store.mu.Unlock()
	}
	s := &compactExternCollector{collector: collector, store: store, typeID: id, foreign: make(map[gc.Ref]uint64), byToken: make(map[uint64]gc.Ref)}
	compactExternCollectors.Store(collector, s)
	compactExternCollectorCount.Add(1)
	collector.AddReclaimObserver(func(ref gc.Ref) {
		s.mu.Lock()
		forgetShell := func(ref gc.Ref, shell compactExternShell) {
			delete(s.shells, ref)
			delete(s.byValue, shell.value)
			compactExternWords.Lock()
			delete(compactExternWords.rows, compactExternWordKey{s.store, shell.word})
			compactExternWordCount.Add(^uint32(0))
			compactExternWords.Unlock()
		}
		if ref.IsNull() {
			for owner, rows := range s.owners {
				for _, cell := range rows.cells {
					if cell.pin != 0 {
						s.reclaimed = append(s.reclaimed, cell.pin)
					}
				}
				delete(s.owners, owner)
			}
			for ref, token := range s.foreign {
				s.reclaimed = append(s.reclaimed, token)
				delete(s.foreign, ref)
			}
			clear(s.byToken)
			for ref, shell := range s.shells {
				forgetShell(ref, shell)
			}
		} else if token, ok := s.foreign[ref]; ok {
			delete(s.foreign, ref)
			delete(s.byToken, token)
			s.reclaimed = append(s.reclaimed, token)
		}
		if shell, ok := s.shells[ref]; ok {
			forgetShell(ref, shell)
		}
		s.mu.Unlock()
		if ref.IsNull() {
			s.drain()
			if _, ok := compactExternCollectors.LoadAndDelete(collector); ok {
				compactExternCollectorCount.Add(^uint32(0))
			}
		}
	})
	return s, nil
}

// Extern identities are weak. A typed extern root must trace the shell, whose
// single collector field retains the underlying object or immediate i31 value.
// This includes i31 conversions, so no immortal per-integer identity map forms.
func (s *compactExternCollector) externFromAny(value gc.Ref, roots gc.RootSet) (uint64, error) {
	if value.IsNull() {
		return 0, nil
	}
	if token, foreign := s.foreign[value]; foreign {
		return token, nil
	}
	if !value.IsI31() {
		if _, err := s.collector.ObjectType(value); err != nil {
			return 0, err
		}
	}
	if word, ok := s.byValue[value]; ok {
		return word, nil
	}
	if s.shells == nil {
		id := gc.TypeID(s.collector.TypeCount())
		desc, err := gc.NewStructDesc(id, []gc.StorageKind{gc.StorageRefNull})
		if err != nil {
			return 0, err
		}
		if err = s.collector.AddTypes([]gc.TypeDesc{desc}); err != nil {
			return 0, err
		}
		if domain := s.store.gcDomainForCollector(s.collector); domain != nil {
			s.store.mu.Lock()
			domain.types = append(domain.types, desc)
			domain.typeReps = append(domain.typeReps, gcDomainTypeRepresentative{})
			s.store.mu.Unlock()
		}
		s.shellType = id
		s.shells, s.byValue = make(map[gc.Ref]compactExternShell), make(map[gc.Ref]uint64)
		s.scratch = new(compactExternShellScratch)
	}
	s.scratch.word[0] = uint64(value)
	shell, err := s.collector.NewStructWordsPrevalidatedWithRootScratch(s.shellType, s.scratch.word[:], roots, &s.scratch.roots)
	s.scratch.word[0] = 0
	if err != nil {
		return 0, err
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	for {
		word, err := s.store.newTokenLocked()
		if err != nil {
			return 0, err
		}
		// Existing public extern tokens use a reversible per-store encoding.
		// Check that directory before reserving a new internal opaque identity.
		raw := bits.RotateLeft64(word, -17) ^ s.store.externKey
		index, generation := uint32(raw), uint32(raw>>32)
		if s.store.externKey != 0 && index != 0 && uint64(index) <= uint64(len(s.store.externrefs)) && s.store.externrefs[index-1].generation == generation {
			continue
		}
		key := compactExternWordKey{s.store, word}
		compactExternWords.Lock()
		if _, exists := compactExternWords.rows[key]; exists {
			compactExternWords.Unlock()
			continue
		}
		compactExternWords.rows[key] = compactExternWord{s, shell}
		compactExternWordCount.Add(1)
		compactExternWords.Unlock()
		s.shells[shell], s.byValue[value] = compactExternShell{word, value}, word
		return word, nil
	}
}

func (s *compactExternCollector) anyFromExtern(word uint64, roots gc.RootSet) (gc.Ref, error) {
	collector, shell := compactExternWordRoot(s.store, word)
	if collector == nil {
		s.store.mu.Lock()
		raw := bits.RotateLeft64(word, -17) ^ s.store.externKey
		index, generation := uint32(raw), uint32(raw>>32)
		var public interface{ nativeExternWord() uint64 }
		if word != 0 && s.store.externKey != 0 && index != 0 && uint64(index) <= uint64(len(s.store.externrefs)) && s.store.externrefs[index-1].generation == generation {
			public, _ = s.store.externrefs[index-1].value.(interface{ nativeExternWord() uint64 })
		}
		s.store.mu.Unlock()
		if public != nil {
			collector, shell = compactExternWordRoot(s.store, public.nativeExternWord())
			if collector == nil {
				return gc.Null(), fmt.Errorf("compact extern public token lost its rooted identity")
			}
		} else {
			return s.anyFromForeign(word, roots)
		}
	}
	if collector != s.collector {
		return gc.Null(), fmt.Errorf("compact extern conversion requires source collector transfer")
	}
	return s.shells[shell].value, nil
}

func (s *compactExternCollector) anyFromForeign(token uint64, roots gc.RootSet) (gc.Ref, error) {
	if token == 0 {
		return gc.Null(), nil
	}
	if ref, ok := s.byToken[token]; ok {
		return ref, nil
	}
	if !s.store.pinExternref(token) {
		return gc.Null(), fmt.Errorf("invalid or released compact extern token")
	}
	ref, err := s.collector.NewStructDefaultWithRoots(s.typeID, roots)
	if err != nil {
		s.store.unpinExternref(token)
		return gc.Null(), err
	}
	s.foreign[ref], s.byToken[token] = token, ref
	return ref, nil
}

func (s *compactExternCollector) drain() {
	s.mu.Lock()
	pending := s.reclaimed
	s.reclaimed = nil
	s.mu.Unlock()
	for _, token := range pending {
		if !s.store.unpinExternref(token) {
			panic("wago: compact extern pin ownership changed")
		}
	}
	clear(pending)
	s.mu.Lock()
	if len(s.reclaimed) == 0 && cap(pending) > cap(s.reclaimed) {
		s.reclaimed = pending[:0]
	}
	s.mu.Unlock()
}

func drainCompactExternReclaims(collector *gc.Collector) {
	if compactExternCollectorCount.Load() != 0 {
		if state, ok := compactExternCollectors.Load(collector); ok {
			state.drain()
		}
	}
}
