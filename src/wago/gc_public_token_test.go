//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcGenericPublicTokenModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	arrayType := []byte{0x5e, 0x7f, 0x01}
	structResult := []byte{0x60, 0x00, 0x01, 0x64, 0x00}
	arrayResult := []byte{0x60, 0x00, 0x01, 0x64, 0x01}
	structRead := []byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x7f}
	arrayRead := []byte{0x60, 0x01, 0x64, 0x01, 0x01, 0x7f}
	pairRead := []byte{0x60, 0x02, 0x64, 0x00, 0x64, 0x00, 0x01, 0x7f}
	anyRead := []byte{0x60, 0x01, 0x6e, 0x01, 0x7f}
	pairResult := []byte{0x60, 0x00, 0x02, 0x64, 0x00, 0x64, 0x01}
	newStruct := []byte{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x0b}
	newArray := []byte{0x00, 0x41, 0x07, 0x41, 0x02, 0xfb, 0x06, 0x01, 0x0b}
	readPrefix := []byte{0x01, 0x01, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b}
	readStruct := append(append([]byte(nil), readPrefix...), 0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b)
	readStructFast := []byte{0x00, 0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	readArray := append(append([]byte(nil), readPrefix...), 0x20, 0x00, 0x41, 0x01, 0xfb, 0x0b, 0x01, 0x0b)
	pairPrefix := append([]byte(nil), readPrefix...)
	pairPrefix[8], pairPrefix[20], pairPrefix[25] = 0x02, 0x02, 0x02
	readPair := append(pairPrefix,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00,
		0x6a, 0x0b)
	readAny := append(append([]byte(nil), readPrefix...), 0x20, 0x00, 0xd1, 0x0b)
	newPair := []byte{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x41, 0x07, 0x41, 0x02, 0xfb, 0x06, 0x01, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, arrayType, structResult, arrayResult, structRead, arrayRead, pairRead, anyRead, pairResult)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(4), wasmtest.ULEB(5), wasmtest.ULEB(6), wasmtest.ULEB(4), wasmtest.ULEB(7), wasmtest.ULEB(8))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("new_struct", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("new_array", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("read_struct", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("read_array", byte(wasm.ExternFunc), 3),
			wasmtest.ExportEntry("read_pair", byte(wasm.ExternFunc), 4),
			wasmtest.ExportEntry("read_struct_fast", byte(wasm.ExternFunc), 5),
			wasmtest.ExportEntry("read_any", byte(wasm.ExternFunc), 6),
			wasmtest.ExportEntry("new_pair", byte(wasm.ExternFunc), 7),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(newStruct))), newStruct...),
			append(wasmtest.ULEB(uint32(len(newArray))), newArray...),
			append(wasmtest.ULEB(uint32(len(readStruct))), readStruct...),
			append(wasmtest.ULEB(uint32(len(readArray))), readArray...),
			append(wasmtest.ULEB(uint32(len(readPair))), readPair...),
			append(wasmtest.ULEB(uint32(len(readStructFast))), readStructFast...),
			append(wasmtest.ULEB(uint32(len(readAny))), readAny...),
			append(wasmtest.ULEB(uint32(len(newPair))), newPair...),
		)),
	)
}

func TestGenericGCResultsIssueBoundedHostTokens(t *testing.T) {
	base, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGenericPublicTokenModule())
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	if !base.usesGenericGCExecution() || base.genericGCFrameRoots() == nil {
		t.Fatal("generic result module lost exact collector/root admission")
	}
	for _, codec := range []bool{false, true} {
		mode := map[bool]string{false: "compiled", true: "codec"}[codec]
		t.Run(mode, func(t *testing.T) {
			compiled := base
			if codec {
				compiled = roundTripCompiled(t, base)
				defer compiled.Close()
			}
			for _, tc := range []struct {
				name string
				gc   GCConfig
			}{
				{name: "throughput", gc: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}},
				{name: "tiny", gc: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, TinyStepBudget: 1, VerifyAfterCollect: true}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					in, err := Instantiate(compiled, InstantiateOptions{GC: tc.gc})
					if err != nil {
						t.Fatal(err)
					}
					structBits, err := in.Invoke("new_struct")
					if err != nil || len(structBits) != 1 || structBits[0]>>32 == 0 {
						in.Close()
						t.Fatalf("new_struct = %v, %v; want one opaque token", structBits, err)
					}
					structRef := ValueOf(ValAnyRef, structBits[0]).GCRef()
					structToken := ValueGCRef(structRef).Bits()
					if got, err := in.Invoke("read_struct", structToken); err != nil || len(got) != 1 || got[0] != 42 {
						in.Close()
						t.Fatalf("read_struct Invoke = %v, %v", got, err)
					}
					if got, err := in.Call(context.Background(), "read_struct", ValueGCRef(structRef)); err != nil || len(got) != 1 || got[0].I32() != 42 {
						in.Close()
						t.Fatalf("read_struct Call = %v, %v", got, err)
					}
					prepared, err := in.PrepareFunction("read_struct")
					if err != nil {
						in.Close()
						t.Fatal(err)
					}
					if got, err := prepared.Invoke(structToken); err != nil || len(got) != 1 || got[0] != 42 {
						in.Close()
						t.Fatalf("read_struct PreparedFunction = %v, %v", got, err)
					}
					if got, err := in.Invoke("read_pair", structToken, structToken); err != nil || len(got) != 1 || got[0] != 84 {
						in.Close()
						t.Fatalf("read_pair = %v, %v", got, err)
					}
					if got, err := in.Invoke("read_any", structToken); err != nil || len(got) != 1 || got[0] != 0 {
						in.Close()
						t.Fatalf("read_any structural supertype = %v, %v", got, err)
					}
					if _, err := in.Invoke("read_array", structToken); err == nil || !strings.Contains(err.Error(), "required structural argument type") {
						in.Close()
						t.Fatalf("wrong-type GC token ingress = %v", err)
					}
					state := in.existingPublicGCState()
					if state == nil || state.argumentRootCount != 0 || state.argumentRootsMade != 2 || !in.gc.GlobalSlot(state.argumentRootSlots[0]).IsNull() || !in.gc.GlobalSlot(state.argumentRootSlots[1]).IsNull() {
						in.Close()
						t.Fatalf("cleared argument roots = %+v", state)
					}
					values, err := in.Call(context.Background(), "new_array")
					if err != nil || len(values) != 1 || values[0].Type() != ValAnyRef || values[0].GCRef().IsNull() || values[0].Bits()>>32 == 0 {
						in.Close()
						t.Fatalf("second live new_array = %v, %v; want typed opaque token", values, err)
					}
					arrayRef := values[0].GCRef()
					if err := in.ReleaseGCRef(structRef); err != nil {
						in.Close()
						t.Fatal(err)
					}
					if _, err := in.Invoke("read_struct", structToken); err == nil || !strings.Contains(err.Error(), "stale") {
						in.Close()
						t.Fatalf("stale GC token ingress = %v", err)
					}
					if got, err := in.Call(context.Background(), "read_array", ValueGCRef(arrayRef)); err != nil || len(got) != 1 || got[0].I32() != 7 {
						in.Close()
						t.Fatalf("read_array Call = %v, %v", got, err)
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					if err := in.ReleaseGCRef(arrayRef); err != nil {
						t.Fatalf("release after producer close: %v", err)
					}
					if err := in.ReleaseGCRef(arrayRef); err == nil || !strings.Contains(err.Error(), "stale") {
						t.Fatalf("stale generic token release = %v", err)
					}
				})
			}
		})
	}
}

func TestGenericGCMultiResultIssuesIndependentTokens(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGenericPublicTokenModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	values, err := in.Call(context.Background(), "new_pair")
	if err != nil || len(values) != 2 || values[0].GCRef().IsNull() || values[1].GCRef().IsNull() || values[0].Bits() == values[1].Bits() {
		t.Fatalf("new_pair = %v, %v", values, err)
	}
	if got, err := in.Call(context.Background(), "read_struct", ValueGCRef(values[0].GCRef())); err != nil || len(got) != 1 || got[0].I32() != 42 {
		t.Fatalf("read pair struct = %v, %v", got, err)
	}
	if got, err := in.Call(context.Background(), "read_array", ValueGCRef(values[1].GCRef())); err != nil || len(got) != 1 || got[0].I32() != 7 {
		t.Fatalf("read pair array = %v, %v", got, err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if !in.hasPhysicalResources() {
		t.Fatal("two live result tokens did not retain producer resources")
	}
	if err := in.ReleaseGCRef(values[1].GCRef()); err != nil {
		t.Fatal(err)
	}
	if !in.hasPhysicalResources() {
		t.Fatal("first of two releases dropped producer resources")
	}
	if err := in.ReleaseGCRef(values[0].GCRef()); err != nil {
		t.Fatal(err)
	}
	if in.hasPhysicalResources() {
		t.Fatal("last result-token release retained producer resources")
	}
}

func TestGenericGCResultTokensGrowAndReuse(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGenericPublicTokenModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	const live = 160
	refs := make([]GCRef, live)
	for i := range refs {
		bits, err := in.Invoke("new_struct")
		if err != nil || len(bits) != 1 {
			t.Fatalf("issue token %d = %v, %v", i, bits, err)
		}
		refs[i] = ValueOf(ValAnyRef, bits[0]).GCRef()
	}
	state := in.existingPublicGCState()
	if state == nil || int(state.resultTokenCount) != live || int(state.resultRootsMade) != live || len(state.resultTokensExtra) != live-gcPublicSlotLimit {
		t.Fatalf("wide result-token state = %+v", state)
	}
	const released = 17
	releasedSlot := state.resultRootSlot(released)
	stale := refs[released]
	if err := in.ReleaseGCRef(stale); err != nil {
		t.Fatal(err)
	}
	refs[released] = GCRef{}
	if !in.gc.GlobalSlot(releasedSlot).IsNull() {
		t.Fatal("released result root remains non-null")
	}
	if err := in.ReleaseGCRef(stale); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("released token became valid again: %v", err)
	}
	bits, err := in.Invoke("new_array")
	if err != nil || len(bits) != 1 {
		t.Fatalf("reissue into released slot = %v, %v", bits, err)
	}
	refs[released] = ValueOf(ValAnyRef, bits[0]).GCRef()
	if state.resultTokenCount != live || state.resultRootsMade != live || state.resultRootSlot(released) != releasedSlot {
		t.Fatalf("reused result-token state = %+v", state)
	}
	pair, err := in.Invoke("new_pair")
	if err != nil || len(pair) != 2 {
		t.Fatalf("wide two-result issue = %v, %v", pair, err)
	}
	refs = append(refs, ValueOf(ValAnyRef, pair[0]).GCRef(), ValueOf(ValAnyRef, pair[1]).GCRef())
	for i, ref := range refs {
		if err := in.ReleaseGCRef(ref); err != nil {
			t.Fatalf("release token %d: %v", i, err)
		}
	}
	if state.resultTokenCount != 0 {
		t.Fatalf("live result token count = %d, want 0", state.resultTokenCount)
	}
}

func TestGenericGCResultTokensRetainExactSharedDomainOwner(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGenericPublicTokenModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	store := newReferenceStore(false)
	defer store.closeRuntime()
	config := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}
	first, err := instantiateCore(compiled, InstantiateOptions{GC: config, store: store})
	if err != nil {
		t.Fatal(err)
	}
	second, err := instantiateCore(compiled, InstantiateOptions{GC: config, store: store})
	if err != nil {
		first.Close()
		t.Fatal(err)
	}
	defer second.Close()
	if first.gc == nil || first.gc != second.gc || !store.ownsGCCollector(first.gc) {
		t.Fatal("generic token instances do not share one Runtime collector domain")
	}
	bits, err := first.Invoke("new_struct")
	if err != nil || len(bits) != 1 {
		first.Close()
		t.Fatalf("first new_struct = %v, %v", bits, err)
	}
	ref := ValueOf(ValAnyRef, bits[0]).GCRef()
	token := ValueGCRef(ref).Bits()
	if err := second.ReleaseGCRef(ref); err == nil || !strings.Contains(err.Error(), "different producer") {
		first.Close()
		t.Fatalf("cross-producer release = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := second.Invoke("read_struct", token); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("cross-instance token ingress after producer close = %v, %v", got, err)
	}
	secondBits, err := second.Invoke("new_array")
	if err != nil || len(secondBits) != 1 {
		t.Fatalf("second new_array after producer close = %v, %v", secondBits, err)
	}
	if err := second.ReleaseGCRef(ValueOf(ValAnyRef, secondBits[0]).GCRef()); err != nil {
		t.Fatal(err)
	}
	if err := first.ReleaseGCRef(ref); err != nil {
		t.Fatalf("release retained producer token: %v", err)
	}
}

func TestGenericGCArgumentRejectsForeignStore(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGenericPublicTokenModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	producer, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	foreign, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	bits, err := producer.Invoke("new_struct")
	if err != nil || len(bits) != 1 {
		t.Fatalf("issue token = %v, %v", bits, err)
	}
	ref := ValueOf(ValAnyRef, bits[0]).GCRef()
	foreignBits, err := foreign.Invoke("new_array")
	if err != nil || len(foreignBits) != 1 {
		t.Fatalf("issue foreign token = %v, %v", foreignBits, err)
	}
	foreignRef := ValueOf(ValAnyRef, foreignBits[0]).GCRef()
	if _, err := foreign.Invoke("read_struct", ValueGCRef(ref).Bits()); err == nil || !strings.Contains(err.Error(), "stale GC reference token") {
		t.Fatalf("foreign-store GC token ingress = %v", err)
	}
	if err := foreign.ReleaseGCRef(foreignRef); err != nil {
		t.Fatal(err)
	}
	if err := producer.ReleaseGCRef(ref); err != nil {
		t.Fatal(err)
	}
}

func TestGenericGCArgumentIngressWarmedAllocations(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGenericPublicTokenModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	store := newReferenceStore(false)
	defer store.closeRuntime()
	options := InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16}, store: store}
	producer, err := instantiateCore(compiled, options)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	consumer, err := instantiateCore(compiled, options)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	bits, err := producer.Invoke("new_struct")
	if err != nil || len(bits) != 1 {
		t.Fatalf("issue token = %v, %v", bits, err)
	}
	ref := ValueOf(ValAnyRef, bits[0]).GCRef()
	token := ValueGCRef(ref).Bits()
	if got, err := consumer.Invoke("read_struct_fast", token); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("warm ingress = %v, %v", got, err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if got, err := consumer.Invoke("read_struct_fast", token); err != nil || len(got) != 1 || got[0] != 42 {
			panic("generic GC token ingress failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("warmed GC token ingress allocations = %v, want 0", allocs)
	}
	if err := producer.ReleaseGCRef(ref); err != nil {
		t.Fatal(err)
	}
}

func TestGenericGCArgumentRootsGrowAndReuse(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGenericPublicTokenModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	bits, err := in.Invoke("new_struct")
	if err != nil || len(bits) != 1 {
		t.Fatalf("issue token = %v, %v", bits, err)
	}
	ref := ValueOf(ValAnyRef, bits[0]).GCRef()
	token := ValueGCRef(ref).Bits()
	params, _, err := exactFuncSignatureView(compiled.Funcs[2], compiled.Types)
	if err != nil || len(params) != 1 {
		t.Fatalf("read_struct signature = %v, %v", params, err)
	}
	const roots = 160
	for i := 0; i < roots; i++ {
		if _, err := in.refStore.stageGCRefArgument(in, token, params[0]); err != nil {
			t.Fatalf("stage argument root %d: %v", i, err)
		}
	}
	in.clearGCRefArgumentRoots()
	state := in.existingPublicGCState()
	if state == nil || state.argumentRootCount != 0 || state.argumentRootsMade != roots || len(state.argumentRootSlotsExtra) != roots-gcPublicSlotLimit {
		t.Fatalf("wide argument-root state = %+v", state)
	}
	for _, index := range []uint32{0, 63, 64, roots - 1} {
		if !in.gc.GlobalSlot(state.argumentRootSlot(index)).IsNull() {
			t.Fatalf("argument root %d remained live", index)
		}
	}
	if err := in.ReleaseGCRef(ref); err != nil {
		t.Fatal(err)
	}
}

func TestGenericGCArgumentRootRacesTokenRelease(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGenericPublicTokenModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	store := newReferenceStore(false)
	defer store.closeRuntime()
	config := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, TinyStepBudget: 1, VerifyAfterCollect: true, StressBarriers: true}
	producer, err := instantiateCore(compiled, InstantiateOptions{GC: config, store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	consumer, err := instantiateCore(compiled, InstantiateOptions{GC: config, store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()

	for i := 0; i < 50; i++ {
		bits, err := producer.Invoke("new_struct")
		if err != nil || len(bits) != 1 {
			t.Fatalf("iteration %d issue = %v, %v", i, bits, err)
		}
		ref := ValueOf(ValAnyRef, bits[0]).GCRef()
		token := ValueGCRef(ref).Bits()
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var callResult []uint64
		var callErr, releaseErr error
		go func() {
			defer wg.Done()
			<-start
			callResult, callErr = consumer.Invoke("read_struct", token)
		}()
		go func() {
			defer wg.Done()
			<-start
			releaseErr = producer.ReleaseGCRef(ref)
		}()
		close(start)
		wg.Wait()
		if releaseErr != nil {
			t.Fatalf("iteration %d release = %v", i, releaseErr)
		}
		if callErr == nil {
			if len(callResult) != 1 || callResult[0] != 42 {
				t.Fatalf("iteration %d call result = %v", i, callResult)
			}
		} else if !strings.Contains(callErr.Error(), "stale GC reference token") {
			t.Fatalf("iteration %d call error = %v", i, callErr)
		}
		state := consumer.existingPublicGCState()
		if state != nil && state.argumentRootCount != 0 {
			t.Fatalf("iteration %d left %d argument roots", i, state.argumentRootCount)
		}
	}
}
