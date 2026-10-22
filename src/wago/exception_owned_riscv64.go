//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const gcExceptionAlloc uint32 = 45
const gcExceptionPayload uint32 = 46
const ownedExceptionPayloadBytes = 136

func (c *Compiled) needsOwnedExceptionReferences() bool {
	if c == nil {
		return false
	}
	i := c.loadCompileIndexes()
	return i != nil && i.railshotTail.ownedExceptions
}
func (c *Compiled) setOwnedExceptionReferences(enabled bool) {
	if enabled {
		if c.validateMemo == nil {
			c.validateMemo = &validateMemo{}
		}
		c.ensureCodeCache()
		c.ensureCompileIndexes().railshotTail.ownedExceptions = true
	} else if i := c.loadCompileIndexes(); i != nil {
		i.railshotTail.ownedExceptions = false
	}
}

type exceptionTagMetadata struct {
	identity        uint64
	gcPayloadMask   uint8
	funcPayloadMask uint8
	params          []ValueTypeDescriptor
	refs            uint64
}

var exceptionTagSequence atomic.Uint64
var exceptionTags = struct {
	sync.Mutex
	byID map[uint64]*exceptionTagMetadata
}{byID: make(map[uint64]*exceptionTagMetadata)}

func newNativeExceptionTag(c *Compiled, index int, fallback uint64) uint64 {
	def := c.memoryDir.ehTags[index]
	if def.ImportKey != "" {
		return fallback
	}
	id := exceptionTagSequence.Add(1)
	if id == 0 {
		panic("wago: exception tag identity exhausted")
	}
	metadata := &exceptionTagMetadata{identity: id, params: append([]ValueTypeDescriptor(nil), c.Types[def.TypeIndex].Params...), refs: 1}
	for i, param := range metadata.params {
		kind, err := gcConstReferenceKind(c, param)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		if kind == gcConstFuncRef {
			metadata.funcPayloadMask |= 1 << i
		}
		if kind == gcConstCollectorRef {
			metadata.gcPayloadMask |= 1 << i
		}
	}
	exceptionTags.Lock()
	exceptionTags.byID[id] = metadata
	exceptionTags.Unlock()
	return id
}
func retainExceptionTag(id uint64) *exceptionTagMetadata {
	exceptionTags.Lock()
	defer exceptionTags.Unlock()
	tag := exceptionTags.byID[id]
	if tag != nil {
		tag.refs++
	}
	return tag
}
func releaseExceptionTag(tag *exceptionTagMetadata) {
	if tag == nil {
		return
	}
	exceptionTags.Lock()
	defer exceptionTags.Unlock()
	if tag.refs == 0 {
		panic("wago: duplicate exception tag release")
	}
	tag.refs--
	if tag.refs == 0 {
		delete(exceptionTags.byID, tag.identity)
	}
}
func releaseNativeExceptionTags(c *Compiled, directory []byte) {
	if c == nil || c.memoryDir == nil {
		return
	}
	for i, def := range c.memoryDir.ehTags {
		if def.ImportKey != "" || (i+1)*8 > len(directory) {
			continue
		}
		id := binary.LittleEndian.Uint64(directory[i*8:])
		exceptionTags.Lock()
		tag := exceptionTags.byID[id]
		exceptionTags.Unlock()
		releaseExceptionTag(tag)
	}
}
func releaseInstanceExceptionTags(in *Instance) {
	if in == nil || in.c == nil || in.c.memoryDir == nil {
		return
	}
	state := in.pluginState.Load()
	if state == nil || state.tagIdentityBase == 0 {
		return
	}
	directory := unsafe.Slice((*byte)(unsafe.Pointer(state.tagIdentityBase)), len(in.c.memoryDir.ehTags)*8)
	releaseNativeExceptionTags(in.c, directory)
	state.tagIdentityBase = 0
}

type ownedExceptionExternalRefs struct {
	store  *referenceStore
	tokens [8]uint64
	count  uint8
}
type ownedExceptionRecord struct {
	tag      *exceptionTagMetadata
	external *ownedExceptionExternalRefs
}

func (r ownedExceptionRecord) release() {
	if r.external != nil {
		for _, token := range r.external.tokens[:r.external.count] {
			r.external.store.unpinExternref(token)
		}
	}
	releaseExceptionTag(r.tag)
}

type ownedExceptionCollector struct {
	mu        sync.Mutex // protects observer-side weak records, never a collector root
	typeID    gc.TypeID
	words     [26]uint64 // collector-domain serialized constructor scratch
	nextID    uint64
	records   map[gc.Ref]ownedExceptionRecord
	reclaimed []ownedExceptionRecord
}

var ownedExceptionCollectors sync.Map // *gc.Collector -> *ownedExceptionCollector
var ownedExceptionCollectorCount atomic.Uint32

func (in *Instance) ownedExceptionCollector(domain *gcStoreDomain) *ownedExceptionCollector {
	if value, ok := ownedExceptionCollectors.Load(in.gc); ok {
		return value.(*ownedExceptionCollector)
	}
	id := gc.TypeID(in.gc.TypeCount())
	fields := ownedExceptionFieldKinds(0)
	desc, err := gc.NewStructDesc(id, fields)
	if err != nil {
		panic(gcHelperFailure(err))
	}
	if err = in.gc.AddTypes([]gc.TypeDesc{desc}); err != nil {
		panic(gcHelperFailure(err))
	}
	if domain != nil {
		in.refStore.mu.Lock()
		domain.types = append(domain.types, desc)
		// An empty representative cannot match a guest-defined structural type.
		domain.typeReps = append(domain.typeReps, gcDomainTypeRepresentative{})
		in.refStore.mu.Unlock()
	}
	state := &ownedExceptionCollector{typeID: id, records: make(map[gc.Ref]ownedExceptionRecord)}
	ownedExceptionCollectors.Store(in.gc, state)
	ownedExceptionCollectorCount.Add(1)
	collector := in.gc
	collector.SetReclaimObserver(func(ref gc.Ref) {
		state.mu.Lock()
		if ref.IsNull() {
			for ref, record := range state.records {
				state.reclaimed = append(state.reclaimed, record)
				delete(state.records, ref)
			}
		} else if record := state.records[ref]; record.tag != nil {
			delete(state.records, ref)
			state.reclaimed = append(state.reclaimed, record)
		}
		pending := len(state.reclaimed) != 0
		state.mu.Unlock()
		if pending {
			if managed, ok := managedFunctionCollectors.Load(collector); ok {
				managed.(*managedFunctionCollector).graph.pendingMetadata.Store(true)
			}
		}
		if ref.IsNull() {
			drainExceptionReclaims(collector)
			if _, existed := ownedExceptionCollectors.LoadAndDelete(collector); existed {
				ownedExceptionCollectorCount.Add(^uint32(0))
			}
		}
	})
	return state
}
func drainExceptionReclaims(collector *gc.Collector) {
	drainCompactExternReclaims(collector)
	if ownedExceptionCollectorCount.Load() == 0 {
		return
	}
	value, ok := ownedExceptionCollectors.Load(collector)
	if !ok {
		return
	}
	state := value.(*ownedExceptionCollector)
	state.mu.Lock()
	pending := state.reclaimed
	state.reclaimed = nil
	state.mu.Unlock()
	for _, record := range pending {
		record.release()
	}
	clear(pending)
	state.mu.Lock()
	if len(state.reclaimed) == 0 && cap(pending) > cap(state.reclaimed) {
		state.reclaimed = pending[:0]
	}
	state.mu.Unlock()
}

func (in *Instance) dispatchGCExceptionHelper(ctrl uintptr, helper, safepoint uint32, args, results []uint64, stack *coreruntime.Engine) {
	if in == nil || in.gc == nil || !in.c.needsOwnedExceptionReferences() || len(args) != 1 || len(results) < 1 {
		panic(gcHelperFailuref("invalid owned exception helper boundary"))
	}
	domain := in.lockGCCollector()
	defer func() {
		unlockGCCollector(domain)
		if domain == nil {
			drainExceptionReclaims(in.gc)
		}
	}()
	if helper == gcExceptionAlloc {
		in.gc.CancelNativeAllocationBatch()
	}
	owned := in.ownedExceptionCollector(domain)
	if helper == gcExceptionPayload {
		ref := gc.Ref(uint32(args[0]))
		owned.mu.Lock()
		record := owned.records[ref]
		owned.mu.Unlock()
		if args[0] != uint64(ref) || record.tag == nil {
			panic(gcHelperFailuref("invalid owned exception reference"))
		}
		actual, err := in.gc.ObjectType(ref)
		if err != nil || actual != owned.typeID && !managedExceptionPayloadType(in.gc, actual) {
			panic(gcHelperFailuref("invalid owned exception payload type"))
		}
		payload, err := in.gc.StructPayloadReadOnly(ref, actual)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		results[0] = uint64(uintptr(unsafe.Pointer(&payload[0])))
		return
	}
	if helper != gcExceptionAlloc || args[0] == 0 {
		panic(gcHelperFailuref("unknown owned exception helper"))
	}
	public := in.publicGCState()
	public.mu.Lock()
	defer public.mu.Unlock()
	defer func() { public.frameRoots.physicalStack = nil }()
	roots := in.gcHelperRoots(ctrl, public, safepoint, stack)
	packet := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(args[0]))), ownedExceptionPayloadBytes)
	tag := retainExceptionTag(binary.LittleEndian.Uint64(packet))
	if tag == nil {
		panic(gcHelperFailuref("owned exception tag identity %#x is unavailable", binary.LittleEndian.Uint64(packet)))
	}
	record := ownedExceptionRecord{tag: tag}
	published := false
	defer func() {
		if !published {
			record.release()
		}
	}()
	clear(owned.words[:])
	for i := 0; i < 9; i++ {
		owned.words[i] = binary.LittleEndian.Uint64(packet[i*8:])
	}
	for i := 0; i < 8; i++ {
		word := binary.LittleEndian.Uint64(packet[72+i*8:])
		if word != 0 && tag.gcPayloadMask&(1<<i) == 0 {
			panic(gcHelperFailuref("owned exception unused collector lane is not zero"))
		}
		if word != uint64(uint32(word)) {
			panic(gcHelperFailuref("owned exception collector lane is not compact"))
		}
		owned.words[9+i*2] = word
	}
	lane := 0
	var functions *managedFunctionCollector
	var constructor *managedFunctionExceptionScratch
	var externs *compactExternCollector
	var rootedExterns uint8
	// Raw extern packet lanes are opaque to the ordinary GC lane map. Root
	// every existing shell before a function Env or foreign anchor can allocate.
	if in.c.compactExternFrameRoots() {
		for i, param := range tag.params {
			if param.Kind == ValueTypeReference && !param.Ref.Heap.Defined && param.Ref.Heap.Abstract == AbstractHeapExtern && owned.words[1+lane] != 0 {
				if constructor == nil {
					var err error
					externs, err = in.compactExternCollector(domain)
					if err != nil {
						panic(gcHelperFailure(err))
					}
					functions = in.managedFunctionCollector(domain)
					constructor = functions.exceptionScratch(roots)
					defer constructor.clear()
				}
				word, ref, pin, err := externs.persistentWord(owned.words[1+lane])
				if err != nil {
					panic(gcHelperFailure(err))
				}
				if pin != 0 && !in.refStore.unpinExternref(pin) {
					panic("wago: exception validation pin changed")
				}
				owned.words[1+lane] = word
				if !ref.IsNull() {
					constructor.envs[constructor.count] = gc.Root(ref)
					constructor.count++
					rootedExterns |= 1 << i
				}
			}
			lane++
			if param.Kind == ValueTypeV128 {
				lane++
			}
		}
	}
	lane = 0
	for i, param := range tag.params {
		if lane >= 8 || param.Kind == ValueTypeV128 && lane >= 7 {
			panic(gcHelperFailuref("owned exception payload exceeds native lane capacity"))
		}
		if tag.funcPayloadMask&(1<<i) != 0 {
			if descriptor := binary.LittleEndian.Uint64(packet[8+lane*8:]); descriptor != 0 {
				if constructor == nil {
					functions = in.managedFunctionCollector(domain)
					constructor = functions.exceptionScratch(roots)
					defer constructor.clear()
				}
				ref, err := functions.environmentForDescriptor(in, descriptor, constructor)
				if err != nil {
					panic(gcHelperFailure(err))
				}
				constructor.envs[constructor.count] = gc.Root(ref)
				constructor.count++
			}
		}
		if param.Kind == ValueTypeReference && !param.Ref.Heap.Defined && param.Ref.Heap.Abstract == AbstractHeapExtern {
			token := owned.words[1+lane]
			if token != 0 && externs != nil {
				if rootedExterns&(1<<i) == 0 {
					ref, err := externs.anyFromForeign(token, constructor)
					if err != nil {
						panic(gcHelperFailure(err))
					}
					constructor.envs[constructor.count] = gc.Root(ref)
					constructor.count++
				}
			} else if token != 0 {
				if in.refStore == nil || !in.refStore.pinExternref(token) {
					panic(gcHelperFailuref("owned exception externref is unavailable"))
				}
				if record.external == nil {
					record.external = &ownedExceptionExternalRefs{store: in.refStore}
				}
				record.external.tokens[record.external.count] = token
				record.external.count++
			}
		}
		lane++
		if param.Kind == ValueTypeV128 {
			lane++
		}
	}
	owned.nextID++
	if owned.nextID == 0 {
		panic(gcHelperFailuref("owned exception identity exhausted"))
	}
	owned.words[25] = owned.nextID
	typeID, words := owned.typeID, owned.words[:]
	if constructor != nil {
		typeID = in.managedExceptionType(functions, domain, constructor.count)
		copy(constructor.words[:26], owned.words[:])
		for i := uint8(0); i < constructor.count; i++ {
			constructor.words[26+i] = uint64(constructor.envs[i].GetRef())
		}
		words, roots = constructor.words[:26+constructor.count], constructor
	}
	ref, err := in.gc.NewStructWordsPrevalidatedWithRootScratch(typeID, words, roots, &public.initializerRoots)
	clear(owned.words[:])
	if err != nil {
		panic(gcHelperFailure(err))
	}
	owned.mu.Lock()
	owned.records[ref] = record
	owned.mu.Unlock()
	published = true
	results[0] = uint64(ref)
}

func dispatchOwnedExceptionHelper(in *Instance, ctrl uintptr, helper, safepoint uint32, args, results []uint64, stack *coreruntime.Engine) bool {
	if helper != gcExceptionAlloc && helper != gcExceptionPayload {
		return false
	}
	in.dispatchGCExceptionHelper(ctrl, helper, safepoint, args, results, stack)
	return true
}

func ownedExceptionReferencesRuntime() bool { return true }
func ownedExceptionExactValueType(collector *gc.Collector, ref gc.Ref, typeID gc.TypeID) (ValueTypeDescriptor, bool) {
	if ownedExceptionCollectorCount.Load() == 0 {
		return ValueTypeDescriptor{}, false
	}
	value, ok := ownedExceptionCollectors.Load(collector)
	if !ok {
		return ValueTypeDescriptor{}, false
	}
	state := value.(*ownedExceptionCollector)
	if typeID != state.typeID && !managedExceptionPayloadType(collector, typeID) {
		return ValueTypeDescriptor{}, false
	}
	state.mu.Lock()
	live := state.records[ref].tag != nil
	state.mu.Unlock()
	return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Abstract: AbstractHeapExn}}}, live
}
func ownedExceptionReferenceMatches(collector *gc.Collector, ref gc.Ref, required ValueTypeDescriptor) (bool, bool) {
	typeID, err := collector.ObjectType(ref)
	if err != nil {
		return false, true
	}
	_, owned := ownedExceptionExactValueType(collector, ref, typeID)
	exn := !required.Ref.Heap.Defined && (required.Ref.Heap.Abstract == AbstractHeapExn || required.Ref.Heap.Abstract == AbstractHeapNoExn)
	if owned {
		return exn && required.Ref.Heap.Abstract == AbstractHeapExn, true
	}
	return false, exn
}
