package wago

import (
	"math/rand"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestGCFrameCodeRangeIndexMatchesLinear(t *testing.T) {
	collector := new(gc.Collector)
	c := &Compiled{code: make([]byte, 7000), validateMemo: &validateMemo{gcFrameRoots: &compiledGCFrameRoots{}}}
	store := &referenceStore{instances: make(map[*Instance]*referenceStoreInstance)}
	instances := make([]*Instance, 64)
	for i := range instances {
		in := &Instance{c: c, gc: collector, base: uintptr(0x10000 + i*8192 + 137)}
		instances[i] = in
		store.instances[in] = &referenceStoreInstance{}
		store.registerGCFrameCodeRangeLocked(in)
	}
	rng := rand.New(rand.NewSource(715))
	for _, removed := range rng.Perm(len(instances)) {
		for k := 0; k < 128; k++ {
			pc := uintptr(0x10000 + rng.Intn(65*8192))
			var want *Instance
			for in := range store.instances {
				if pc >= in.base && pc-in.base < uintptr(len(c.code)) {
					want = in
					break
				}
			}
			if got := store.gcFrameOwner(pc, collector); got != want {
				t.Fatalf("PC %x: index=%p linear=%p", pc, got, want)
			}
		}
		store.unregisterGCFrameCodeRangeLocked(instances[removed])
		delete(store.instances, instances[removed])
	}
	if store.gcDomains.codeRanges != nil {
		t.Fatal("empty index retained")
	}
}

func FuzzDefinedTypeFingerprintEquivalence(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5})
	f.Add([]byte{0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		if len(data) > 16 {
			data = data[:16]
		}
		n := len(data)
		types := make([]DefinedTypeDescriptor, n*2)
		for copyIndex := 0; copyIndex < 2; copyIndex++ {
			for i, v := range data {
				d := DefinedTypeDescriptor{RecGroup: uint32(copyIndex), Final: v&8 != 0, Kind: CompositeTypeKind(v % 3)}
				value := ValueTypeDescriptor{Kind: ValueTypeI32}
				if v&1 != 0 {
					value = ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: uint32(copyIndex*n + (i+1)%n)}}}
				}
				field := FieldTypeDescriptor{Mutable: v&4 != 0, Storage: StorageTypeDescriptor{Value: value}}
				switch d.Kind {
				case CompositeTypeFunction:
					d.Params = []ValueTypeDescriptor{value}
					d.Results = []ValueTypeDescriptor{{Kind: ValueTypeI32}}
				case CompositeTypeStruct:
					d.Fields = []FieldTypeDescriptor{field}
				case CompositeTypeArray:
					d.Array = field
				}
				types[copyIndex*n+i] = d
			}
		}
		keys, ok := definedTypeFingerprints(types)
		if !ok {
			t.Fatal("valid graph rejected")
		}
		for i := 0; i < n; i++ {
			if !definedTypeEquivalent(uint32(i), types, uint32(n+i), types) {
				t.Fatal("duplicate group differs")
			}
			if keys[i] != keys[n+i] {
				t.Fatalf("equal members %d and %d have different fingerprints", i, n+i)
			}
		}
		// Mutating the late member invalidates all projections of its group.
		types[len(types)-1].Final = !types[len(types)-1].Final
		for i := 0; i < n; i++ {
			if definedTypeEquivalent(uint32(i), types, uint32(n+i), types) {
				t.Fatal("late difference ignored")
			}
		}
	})
}

func TestGCFrameCodeRangeSingletonPromotesAndRetires(t *testing.T) {
	collector := new(gc.Collector)
	c := &Compiled{code: make([]byte, 64), validateMemo: &validateMemo{gcFrameRoots: &compiledGCFrameRoots{}}}
	first := &Instance{c: c, gc: collector, base: 0x10000}
	second := &Instance{c: c, gc: collector, base: 0x20000}
	store := &referenceStore{instances: map[*Instance]*referenceStoreInstance{first: {}}}
	store.registerGCFrameCodeRangeLocked(first)
	if store.gcDomains.singleCodeOwner != first || store.gcDomains.codeRanges != nil {
		t.Fatal("one code owner allocated a directory")
	}
	if store.gcFrameOwner(first.base+1, collector) != first || store.gcFrameOwner(first.base+64, collector) != nil {
		t.Fatal("singleton range boundaries differ from the indexed path")
	}
	store.instances[second] = &referenceStoreInstance{}
	store.registerGCFrameCodeRangeLocked(second)
	if store.gcDomains.singleCodeOwner != nil || store.gcDomains.codeRanges == nil {
		t.Fatal("second owner failed to promote the directory")
	}
	for _, in := range []*Instance{first, second} {
		if store.gcFrameOwner(in.base+1, collector) != in {
			t.Fatal("promotion omitted a live owner")
		}
		store.unregisterGCFrameCodeRangeLocked(in)
		delete(store.instances, in)
	}
	if store.gcDomains.codeRanges != nil || store.gcDomains.singleCodeOwner != nil {
		t.Fatal("empty directory retained owners")
	}
	store.instances[first] = &referenceStoreInstance{}
	store.registerGCFrameCodeRangeLocked(first)
	store.unregisterGCFrameCodeRangeLocked(first)
	delete(store.instances, first)
	if store.gcDomains.singleCodeOwner != nil {
		t.Fatal("retired singleton retained")
	}
}
