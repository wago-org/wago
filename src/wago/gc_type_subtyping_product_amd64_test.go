//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

func compileStagedGCTypeSubtypingProductForTest(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCTypeSubtypingProducts = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func BenchmarkStagedGCTypeSubtypingRuntimeCallCast(b *testing.B) {
	pin := stagedGCTypeSubtypingProductPins[23]
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, pin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("run"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingRuntimeTypedTable(b *testing.B) {
	pin := stagedGCTypeSubtypingTypedTablePin
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, pin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("run"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingLinkProviderNullResult(b *testing.B) {
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, stagedGCTypeSubtypingLinkProviderPin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("f2")
		if err != nil || len(got) != 1 || got[0] != 0 {
			b.Fatalf("f2 = %v, %v; want one null result", got, err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingFinalityLinkProviderEmpty(b *testing.B) {
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, stagedGCTypeSubtypingFinalityLinkProviderPin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("f2")
		if err != nil || len(got) != 0 {
			b.Fatalf("f2 = %v, %v; want empty success", got, err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingStructLinkProviderEmpty(b *testing.B) {
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, stagedGCTypeSubtypingStructLinkProviderPin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("g")
		if err != nil || len(got) != 0 {
			b.Fatalf("g = %v, %v; want empty success", got, err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingStructProjectionLinkProviderEmpty(b *testing.B) {
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, stagedGCTypeSubtypingStructProjectionLinkProviderPin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("g")
		if err != nil || len(got) != 0 {
			b.Fatalf("g = %v, %v; want empty success", got, err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingStructMismatchLinkProviderEmpty(b *testing.B) {
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, stagedGCTypeSubtypingStructMismatchLinkProviderPin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("g")
		if err != nil || len(got) != 0 {
			b.Fatalf("g = %v, %v; want empty success", got, err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingIndependentStructLinkProviderEmpty(b *testing.B) {
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, stagedGCTypeSubtypingIndependentStructLinkProviderPin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("g")
		if err != nil || len(got) != 0 {
			b.Fatalf("g = %v, %v; want empty success", got, err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingExtendedProjectionLinkProviderEmpty(b *testing.B) {
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, stagedGCTypeSubtypingExtendedProjectionLinkProviderPin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("h")
		if err != nil || len(got) != 0 {
			b.Fatalf("h = %v, %v; want empty success", got, err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingRuntimeFinalityRecovery(b *testing.B) {
	pin := stagedGCTypeSubtypingProductPins[24]
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, pin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.invokeLocal(0, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func TestStagedGCTypeSubtypingProductsCompile(t *testing.T) {
	if got := unsafe.Sizeof(compiledCodeCache{}); got != 64 {
		t.Fatalf("compiledCodeCache size = %d, want 64 bytes", got)
	}
	wantCodeBytes := []int{0, 0, 0, 0, 0, 0, 632, 592, 77, 77, 77, 77, 253, 253, 178, 178, 178, 178, 215, 448, 560, 178, 178, 7834, 1257, 1431, 77, 0, 77, 0}
	wantCodecBytes := []int{349, 385, 347, 219, 238, 386, 1019, 1128, 499, 657, 420, 755, 598, 852, 648, 806, 648, 569, 923, 786, 1096, 470, 550, 8330, 1556, 1791, 314, 237, 404, 237}
	for i, pin := range stagedGCTypeSubtypingProductPins {
		t.Run(pin.Filename, func(t *testing.T) {
			data := stagedGCTypeSubtypingProductData(t, pin)
			if _, err := Compile(NewRuntimeConfig(), data); err == nil || !strings.Contains(err.Error(), "gc type") {
				t.Fatalf("public compile = %v, want closed GC type gate", err)
			}
			c, err := compileStagedGCTypeSubtypingProductForTest(data)
			if err != nil {
				t.Fatalf("staged compile: %v", err)
			}
			defer c.Close()
			if c.stagedGCTypeSubtypingProduct() != pin.Class || c.collectorFreeStructuralMetadata() || c.stagedFeatures()&CoreFeatureGC == 0 {
				t.Fatalf("product/legacy-marker/features = %v/%v/%v", c.stagedGCTypeSubtypingProduct(), c.collectorFreeStructuralMetadata(), c.stagedFeatures())
			}
			if got := compiledStructuralRequiredFeatures(c); got&CoreFeatureGC == 0 {
				t.Fatalf("required features = %v, want GC", got)
			}
			if pin.Class == stagedGCTypeSubtypingDeclarations && !gc.HasHeapObjectTypes(c.GCTypeDescs) {
				t.Fatalf("declaration product lost heap-object descriptors: %#v", c.GCTypeDescs)
			}
			if pin.Class.usesLinkFunctionIdentity() {
				if !c.NeedsFuncRefDescs {
					t.Fatal("link product did not request canonical function descriptors")
				}
				return
			}
			in, err := instantiateCore(c, InstantiateOptions{})
			if err != nil {
				t.Fatalf("instantiate: %v", err)
			}
			if in.gc != nil {
				t.Fatal("no-object type-subtyping product allocated a collector")
			}
			if pin.Class.usesRefTest() {
				wantDescBytes := (len(c.FuncTypeID) + 1) * coreruntime.FuncRefDescBytes
				if len(in.funcRefDescs) != wantDescBytes {
					t.Fatalf("ref.test descriptor arena = %d bytes, want exact %d", len(in.funcRefDescs), wantDescBytes)
				}
				got, err := in.Invoke("run")
				if err != nil || !reflect.DeepEqual(got, pin.Results) {
					t.Fatalf("run = %v, %v; want %v", got, err, pin.Results)
				}
				var invokeErr error
				allocs := testing.AllocsPerRun(1000, func() {
					got, invokeErr = in.Invoke("run")
				})
				if invokeErr != nil || !reflect.DeepEqual(got, pin.Results) || allocs != 0 {
					t.Fatalf("steady run = %v, %v, allocs=%v; want %v, nil, 0", got, invokeErr, allocs, pin.Results)
				}
			}
			if pin.Class == stagedGCTypeSubtypingRuntimeCallCast {
				if got, want := len(in.funcRefDescs), 11*coreruntime.FuncRefDescBytes; got != want {
					t.Fatalf("runtime descriptor arena = %d bytes, want %d", got, want)
				}
				if got, want := in.tableDescLen, 8+3*coreruntime.TableEntryBytes; got != want {
					t.Fatalf("runtime table descriptor = %d bytes, want %d", got, want)
				}
				if got, err := in.Invoke("run"); err != nil || len(got) != 0 {
					t.Fatalf("run = %v, %v; want empty success", got, err)
				}
				for i, name := range []string{"fail1", "fail2", "fail3", "fail4", "fail5", "fail6"} {
					_, err := in.Invoke(name)
					want := "wrong signature"
					if i >= 3 {
						want = "cast failure"
					}
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Fatalf("%s = %v, want %s trap", name, err, want)
					}
				}
				var invokeErr error
				allocs := testing.AllocsPerRun(1000, func() {
					_, invokeErr = in.Invoke("run")
				})
				if invokeErr != nil || allocs != 0 {
					t.Fatalf("steady run = %v, allocs=%v; want nil, 0", invokeErr, allocs)
				}
			}
			if pin.Class == stagedGCTypeSubtypingRuntimeFinalityCallCast {
				if got, want := len(in.funcRefDescs), 7*coreruntime.FuncRefDescBytes; got != want {
					t.Fatalf("runtime finality descriptor arena = %d bytes, want %d", got, want)
				}
				if got, want := in.tableDescLen, 8+2*coreruntime.TableEntryBytes; got != want {
					t.Fatalf("runtime finality table descriptor = %d bytes, want %d", got, want)
				}
				for i, name := range []string{"fail1", "fail2", "fail3", "fail4"} {
					_, err := in.Invoke(name)
					want := "wrong signature"
					if i >= 2 {
						want = "cast failure"
					}
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Fatalf("%s = %v, want %s trap", name, err, want)
					}
				}
				if got, err := in.invokeLocal(0, nil); err != nil || len(got) != 0 {
					t.Fatalf("post-trap local recovery = %v, %v; want empty success", got, err)
				}
				var invokeErr error
				allocs := testing.AllocsPerRun(1000, func() {
					_, invokeErr = in.invokeLocal(0, nil)
				})
				if invokeErr != nil || allocs != 0 {
					t.Fatalf("steady finality recovery = %v, allocs=%v; want nil, 0", invokeErr, allocs)
				}
			}
			if pin.Class == stagedGCTypeSubtypingRuntimeTypedTableCall {
				if got, want := len(in.funcRefDescs), 6*coreruntime.FuncRefDescBytes; got != want {
					t.Fatalf("runtime typed-table descriptor arena = %d bytes, want %d", got, want)
				}
				if got, want := in.tableDescLen, 8+2*coreruntime.TableEntryBytes; got != want {
					t.Fatalf("runtime typed-table image = %d bytes, want %d", got, want)
				}
				tableImage := unsafe.Slice((*byte)(offHeapPtr(in.tableDescPtr)), in.tableDescLen)
				for i := 0; i < 2; i++ {
					tableOff := 8 + i*coreruntime.TableEntryBytes
					canonicalOff := (i + 1) * coreruntime.FuncRefDescBytes
					wantIdentity := uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[canonicalOff])))
					if got := binary.LittleEndian.Uint64(tableImage[tableOff+coreruntime.TableEntryCodePtrOffset:]); got == 0 {
						t.Fatalf("runtime typed-table entry %d has null code pointer", i)
					}
					if got := binary.LittleEndian.Uint64(tableImage[tableOff+coreruntime.TableEntryRefSlotOffset:]); got != wantIdentity {
						t.Fatalf("runtime typed-table entry %d identity = %#x, want canonical local %#x", i, got, wantIdentity)
					}
				}
				if got, err := in.Invoke("run"); err != nil || len(got) != 0 {
					t.Fatalf("typed-table run = %v, %v; want empty success", got, err)
				}
				for _, name := range []string{"fail1", "fail2"} {
					if _, err := in.Invoke(name); err == nil || !strings.Contains(err.Error(), "wrong signature") {
						t.Fatalf("%s = %v, want wrong-signature trap", name, err)
					}
				}
				if got, err := in.Invoke("run"); err != nil || len(got) != 0 {
					t.Fatalf("post-trap typed-table recovery = %v, %v; want empty success", got, err)
				}
				var invokeErr error
				allocs := testing.AllocsPerRun(1000, func() {
					_, invokeErr = in.Invoke("run")
				})
				if invokeErr != nil || allocs != 0 {
					t.Fatalf("steady typed-table run = %v, allocs=%v; want nil, 0", invokeErr, allocs)
				}
			}
			if pin.Class == stagedGCTypeSubtypingRefFuncGlobals {
				wantDescBytes := (len(c.FuncTypeID) + 1) * coreruntime.FuncRefDescBytes
				if len(in.funcRefDescs) != wantDescBytes {
					t.Fatalf("descriptor arena = %d bytes, want exact %d", len(in.funcRefDescs), wantDescBytes)
				}
				for globalIndex, global := range c.Globals {
					if global.Mutable || !global.HasInitFunc {
						t.Fatalf("global %d = %+v, want immutable ref.func initializer", globalIndex, global)
					}
					off := (int(global.InitFunc) + 1) * coreruntime.FuncRefDescBytes
					want := uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[off])))
					if got := readGlobalObject(in.globalCells[globalIndex], ValFuncRef); got != want {
						t.Fatalf("global %d descriptor = %#x, want local canonical %#x", globalIndex, got, want)
					}
					if got := binary.LittleEndian.Uint64(in.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:]); got != want {
						t.Fatalf("global %d descriptor identity = %#x, want self-owned %#x", globalIndex, got, want)
					}
				}
			}
			meta := (&Module{c: c}).Metadata()
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			blob, err := marshalCompiled(c)
			if err != nil {
				t.Fatalf("marshal codec-v27: %v", err)
			}
			if len(c.Code) != wantCodeBytes[i] || len(blob) != wantCodecBytes[i] {
				t.Fatalf("product sizes code/codec = %d/%d, want %d/%d", len(c.Code), len(blob), wantCodeBytes[i], wantCodecBytes[i])
			}
			t.Logf("product size: wasm=%d code=%d codec=%d types=%d gcdescs=%d", len(data), len(c.Code), len(blob), len(c.Types), len(c.GCTypeDescs))
			var loaded Compiled
			if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
				t.Fatalf("private codec-v27 reload: %v", err)
			}
			defer loaded.Close()
			if loaded.stagedGCTypeSubtypingProduct() != 0 || loaded.stagedFeatures().IsEnabled(CoreFeatureGC) {
				t.Fatalf("codec inherited type-subtyping admission: product=%v features=%v", loaded.stagedGCTypeSubtypingProduct(), loaded.stagedFeatures())
			}
			loadedMeta := (&Module{c: &loaded}).Metadata()
			if !reflect.DeepEqual(loadedMeta.Types, meta.Types) || !reflect.DeepEqual(loadedMeta.Functions, meta.Functions) || !reflect.DeepEqual(loadedMeta.Globals, meta.Globals) || !reflect.DeepEqual(loadedMeta.Tables, meta.Tables) || !reflect.DeepEqual(loadedMeta.ExportedTables, meta.ExportedTables) {
				t.Fatalf("codec metadata changed\n got: %#v\nwant: %#v", loadedMeta, meta)
			}
			if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
				t.Fatalf("codec-loaded instantiate = %v, want required-feature rejection", err)
			}
			var public Compiled
			if err := public.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
				t.Fatalf("public codec load = %v, want unsupported GC gate", err)
			}
			if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC reference products") {
				t.Fatalf("snapshot capture = %v, want explicit GC state gate", err)
			}
		})
	}
}

func TestStagedGCTypeSubtypingFirstLinkingClusterLifecycle(t *testing.T) {
	providerData := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingLinkProviderPin)
	consumerData := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingLinkConsumerPin)
	providerCompiled, err := compileStagedGCTypeSubtypingProductForTest(providerData)
	if err != nil {
		t.Fatalf("compile provider: %v", err)
	}
	defer providerCompiled.Close()
	consumerCompiled, err := compileStagedGCTypeSubtypingProductForTest(consumerData)
	if err != nil {
		t.Fatalf("compile consumer: %v", err)
	}
	defer consumerCompiled.Close()
	if providerCompiled.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingLinkProvider || consumerCompiled.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingLinkConsumer {
		t.Fatalf("provider/consumer products = %v/%v", providerCompiled.stagedGCTypeSubtypingProduct(), consumerCompiled.stagedGCTypeSubtypingProduct())
	}
	if !providerCompiled.NeedsFuncRefDescs || !consumerCompiled.NeedsFuncRefDescs {
		t.Fatal("link provider/consumer must own canonical descriptor arenas")
	}
	providerBlob, err := marshalCompiled(providerCompiled)
	if err != nil {
		t.Fatalf("marshal provider: %v", err)
	}
	consumerBlob, err := marshalCompiled(consumerCompiled)
	if err != nil {
		t.Fatalf("marshal unlinked consumer: %v", err)
	}
	if got := [6]int{len(providerData), len(providerCompiled.Code), len(providerBlob), len(consumerData), len(consumerCompiled.Code), len(consumerBlob)}; got != [6]int{103, 369, 624, 86, 0, 301} {
		t.Fatalf("link product wasm/code/codec sizes = %v, want [103 369 624 86 0 301]", got)
	}

	instantiateProvider := func() (*Instance, map[string]*InstanceExport) {
		in, err := instantiateCore(providerCompiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate provider: %v", err)
		}
		if in.gc != nil {
			t.Fatal("link provider allocated a collector")
		}
		exports := make(map[string]*InstanceExport, 3)
		for i := 0; i < 3; i++ {
			name := fmt.Sprintf("f%d", i)
			exports[name], err = in.ExportedFunc(name)
			if err != nil {
				t.Fatalf("export %s: %v", name, err)
			}
		}
		return in, exports
	}
	positiveImports := func(exports map[string]*InstanceExport) Imports {
		return Imports{"M.f0": exports["f0"], "M.f1": exports["f1"], "M.f2": exports["f2"]}
	}
	resourceState := func(in *Instance) (refs int, closed bool) {
		in.lifeMu.Lock()
		refs, closed = in.resourceRefs, in.resourcesClosed
		in.lifeMu.Unlock()
		return
	}

	provider, exports := instantiateProvider()
	if got, err := provider.Invoke("f2"); err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("provider f2 = %v, %v; want one null result", got, err)
	}
	var invokeErr error
	allocs := testing.AllocsPerRun(1000, func() {
		_, invokeErr = provider.Invoke("f2")
	})
	if invokeErr != nil || allocs != 0 {
		t.Fatalf("provider f2 steady state = %v, allocs=%v; want nil, 0", invokeErr, allocs)
	}
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: positiveImports(exports)})
	if err != nil {
		t.Fatalf("instantiate subtype-compatible duplicate imports: %v", err)
	}
	if consumer.gc != nil {
		t.Fatal("link consumer allocated a collector")
	}
	if got, want := len(provider.funcRefDescs), 4*coreruntime.FuncRefDescBytes; got != want {
		t.Fatalf("provider descriptor arena = %d bytes, want %d", got, want)
	}
	if got, want := len(consumer.funcRefDescs), 7*coreruntime.FuncRefDescBytes; got != want {
		t.Fatalf("consumer descriptor arena = %d bytes, want %d", got, want)
	}
	wantProviderFunctions := []int{0, 1, 1, 2, 2, 2}
	for importIndex, providerFunction := range wantProviderFunctions {
		consumerOff := (importIndex + 1) * coreruntime.FuncRefDescBytes
		providerOff := (providerFunction + 1) * coreruntime.FuncRefDescBytes
		if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryCodePtrOffset:]); got == 0 {
			t.Fatalf("consumer import %d has a null code pointer", importIndex)
		}
		got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryRefSlotOffset:])
		want := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryRefSlotOffset:])
		if got == 0 || got != want {
			t.Fatalf("consumer import %d identity = %#x, want provider function %d canonical %#x", importIndex, got, providerFunction, want)
		}
	}
	if refs, closed := resourceState(provider); refs != 1 || closed {
		t.Fatalf("duplicate imports retained provider refs/closed = %d/%v, want 1/false", refs, closed)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("logical provider close: %v", err)
	}
	if refs, closed := resourceState(provider); refs != 1 || closed {
		t.Fatalf("logically closed provider refs/resourcesClosed = %d/%v, want 1/false", refs, closed)
	}
	for importIndex, providerFunction := range wantProviderFunctions {
		consumerOff := (importIndex + 1) * coreruntime.FuncRefDescBytes
		providerOff := (providerFunction + 1) * coreruntime.FuncRefDescBytes
		if got, want := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryRefSlotOffset:]), binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryRefSlotOffset:]); got != want {
			t.Fatalf("post-close consumer import %d identity = %#x, want retained %#x", importIndex, got, want)
		}
	}
	if _, err := marshalCompiled(consumer.c); err != nil {
		t.Fatalf("binding-independent consumer codec: %v", err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer close: %v", err)
	}
	if refs, closed := resourceState(provider); refs != 0 || !closed {
		t.Fatalf("provider after consumer close refs/resourcesClosed = %d/%v, want 0/true", refs, closed)
	}

	provider2, exports2 := instantiateProvider()
	rollbackImports := positiveImports(exports2)
	rollbackImports["M.f2"] = exports2["f1"]
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: rollbackImports}); err == nil || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("later subtype mismatch = %v, want signature mismatch", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || closed {
		t.Fatalf("failed later import retained refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}
	for i, pin := range stagedGCTypeSubtypingLinkUnlinkablePins {
		c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(t, pin))
		if err != nil {
			t.Fatalf("compile %s: %v", pin.Filename, err)
		}
		name := "f0"
		if i == 2 {
			name = "f1"
		}
		_, linkErr := instantiateCore(c, InstantiateOptions{Imports: Imports{"M." + name: exports2[name]}})
		_ = c.Close()
		if linkErr == nil || !strings.Contains(linkErr.Error(), "signature mismatch") {
			t.Fatalf("%s link = %v, want incompatible signature", pin.Filename, linkErr)
		}
		if refs, closed := resourceState(provider2); refs != 0 || closed {
			t.Fatalf("%s retained refs/resourcesClosed = %d/%v, want 0/false", pin.Filename, refs, closed)
		}
	}
	consumer2, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: positiveImports(exports2)})
	if err != nil {
		t.Fatalf("instantiate close-order consumer: %v", err)
	}
	if err := consumer2.Close(); err != nil {
		t.Fatalf("consumer-first close: %v", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || closed {
		t.Fatalf("consumer-first close provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}
	if err := provider2.Close(); err != nil {
		t.Fatalf("provider-final close: %v", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || !closed {
		t.Fatalf("provider-final refs/resourcesClosed = %d/%v, want 0/true", refs, closed)
	}

	host := HostFunc(func(HostModule, []uint64, []uint64) {})
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M.f0": host, "M.f1": host, "M.f2": host}}); err == nil || !strings.Contains(err.Error(), "exact gc/type-subtyping link provider") {
		t.Fatalf("host link = %v, want exact provider rejection", err)
	}
	for name, blob := range map[string][]byte{"provider": providerBlob, "consumer": consumerBlob} {
		var loaded Compiled
		if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
			t.Fatalf("private %s reload: %v", name, err)
		}
		if loaded.stagedGCTypeSubtypingProduct() != 0 || loaded.stagedFeatures().IsEnabled(CoreFeatureGC) {
			t.Fatalf("private %s reload inherited admission: product=%v features=%v", name, loaded.stagedGCTypeSubtypingProduct(), loaded.stagedFeatures())
		}
		if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
			t.Fatalf("private %s reload instantiate = %v, want required-feature rejection", name, err)
		}
		_ = loaded.Close()
		var public Compiled
		if err := public.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
			t.Fatalf("public %s reload = %v, want unsupported GC rejection", name, err)
		}
	}
	for name, c := range map[string]*Compiled{"provider": providerCompiled, "consumer": consumerCompiled} {
		if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC reference products") {
			t.Fatalf("%s snapshot = %v, want GC reference-product rejection", name, err)
		}
	}
}

func TestStagedGCTypeSubtypingStructLinkingClusterLifecycle(t *testing.T) {
	providerData := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructLinkProviderPin)
	consumerData := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructLinkConsumerPin)
	providerCompiled, err := compileStagedGCTypeSubtypingProductForTest(providerData)
	if err != nil {
		t.Fatalf("compile provider: %v", err)
	}
	defer providerCompiled.Close()
	consumerCompiled, err := compileStagedGCTypeSubtypingProductForTest(consumerData)
	if err != nil {
		t.Fatalf("compile consumer: %v", err)
	}
	defer consumerCompiled.Close()
	if providerCompiled.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingStructLinkProvider || consumerCompiled.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingStructLinkConsumer {
		t.Fatalf("provider/consumer products = %v/%v", providerCompiled.stagedGCTypeSubtypingProduct(), consumerCompiled.stagedGCTypeSubtypingProduct())
	}
	if !providerCompiled.NeedsFuncRefDescs || !consumerCompiled.NeedsFuncRefDescs {
		t.Fatal("struct link provider/consumer must own canonical descriptor arenas")
	}
	providerBlob, err := marshalCompiled(providerCompiled)
	if err != nil {
		t.Fatalf("marshal provider: %v", err)
	}
	consumerBlob, err := marshalCompiled(consumerCompiled)
	if err != nil {
		t.Fatalf("marshal unlinked consumer: %v", err)
	}
	if got := [6]int{len(providerData), len(providerCompiled.Code), len(providerBlob), len(consumerData), len(consumerCompiled.Code), len(consumerBlob)}; got != [6]int{70, 77, 314, 51, 0, 237} {
		t.Fatalf("struct link wasm/code/codec sizes = %v, want [70 77 314 51 0 237]", got)
	}

	resourceState := func(in *Instance) (refs int, closed bool) {
		in.lifeMu.Lock()
		refs, closed = in.resourceRefs, in.resourcesClosed
		in.lifeMu.Unlock()
		return
	}
	instantiateProvider := func() (*Instance, *InstanceExport) {
		in, err := instantiateCore(providerCompiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate provider: %v", err)
		}
		if in.gc != nil {
			t.Fatal("struct link provider allocated a collector")
		}
		ex, err := in.ExportedFunc("g")
		if err != nil {
			t.Fatalf("export g: %v", err)
		}
		return in, ex
	}

	provider, exported := instantiateProvider()
	if got, want := len(provider.funcRefDescs), 2*coreruntime.FuncRefDescBytes; got != want {
		t.Fatalf("provider descriptor arena = %d bytes, want %d", got, want)
	}
	providerOff := coreruntime.FuncRefDescBytes
	providerIdentity := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryRefSlotOffset:])
	if providerIdentity != uint64(uintptr(unsafe.Pointer(&provider.funcRefDescs[providerOff]))) {
		t.Fatalf("provider descriptor identity = %#x, want self-owned canonical address", providerIdentity)
	}
	if got := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryCodePtrOffset:]); got == 0 {
		t.Fatal("provider descriptor has a null code pointer")
	}
	if got, err := provider.Invoke("g"); err != nil || len(got) != 0 {
		t.Fatalf("provider g = %v, %v; want empty success", got, err)
	}
	var invokeErr error
	allocs := testing.AllocsPerRun(1000, func() {
		_, invokeErr = provider.Invoke("g")
	})
	if invokeErr != nil || allocs != 0 {
		t.Fatalf("provider g steady state = %v, allocs=%v; want nil, 0", invokeErr, allocs)
	}
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M3.g": exported}})
	if err != nil {
		t.Fatalf("instantiate compatible consumer: %v", err)
	}
	if consumer.gc != nil {
		t.Fatal("struct link consumer allocated a collector")
	}
	if got, want := len(consumer.funcRefDescs), 2*coreruntime.FuncRefDescBytes; got != want {
		t.Fatalf("consumer descriptor arena = %d bytes, want %d", got, want)
	}
	consumerOff := coreruntime.FuncRefDescBytes
	if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryCodePtrOffset:]); got == 0 {
		t.Fatal("consumer import descriptor has a null code pointer")
	}
	if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryRefSlotOffset:]); got != providerIdentity {
		t.Fatalf("consumer import identity = %#x, want provider canonical %#x", got, providerIdentity)
	}
	if refs, closed := resourceState(provider); refs != 1 || closed {
		t.Fatalf("consumer retained provider refs/resourcesClosed = %d/%v, want 1/false", refs, closed)
	}
	if _, err := marshalCompiled(consumer.c); err != nil {
		t.Fatalf("binding-independent struct consumer codec: %v", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("logical provider close: %v", err)
	}
	if refs, closed := resourceState(provider); refs != 1 || closed {
		t.Fatalf("logically closed provider refs/resourcesClosed = %d/%v, want 1/false", refs, closed)
	}
	if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryRefSlotOffset:]); got != providerIdentity {
		t.Fatalf("post-close consumer identity = %#x, want retained %#x", got, providerIdentity)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer close: %v", err)
	}
	if refs, closed := resourceState(provider); refs != 0 || !closed {
		t.Fatalf("provider after consumer close refs/resourcesClosed = %d/%v, want 0/true", refs, closed)
	}

	provider2, exported2 := instantiateProvider()
	consumer2, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M3.g": exported2}})
	if err != nil {
		t.Fatalf("instantiate consumer-first close pair: %v", err)
	}
	if err := consumer2.Close(); err != nil {
		t.Fatalf("consumer-first close: %v", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || closed {
		t.Fatalf("consumer-first provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}
	invalidExport := &InstanceExport{inst: provider2, localIdx: len(provider2.c.Entry)}
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M3.g": invalidExport}}); err == nil || !strings.Contains(err.Error(), "unavailable function") {
		t.Fatalf("invalid provider export link = %v, want unavailable-function rejection", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || closed {
		t.Fatalf("failed link retained provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}
	if err := provider2.Close(); err != nil {
		t.Fatalf("provider-final close: %v", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || !closed {
		t.Fatalf("provider-final refs/resourcesClosed = %d/%v, want 0/true", refs, closed)
	}

	host := HostFunc(func(HostModule, []uint64, []uint64) {})
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M3.g": host}}); err == nil || !strings.Contains(err.Error(), "exact gc/type-subtyping link provider") {
		t.Fatalf("host link = %v, want exact provider rejection", err)
	}
	oldProviderCompiled, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingLinkProviderPin))
	if err != nil {
		t.Fatalf("compile old provider: %v", err)
	}
	defer oldProviderCompiled.Close()
	oldProvider, err := instantiateCore(oldProviderCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate old provider: %v", err)
	}
	defer oldProvider.Close()
	oldExport, err := oldProvider.ExportedFunc("f0")
	if err != nil {
		t.Fatalf("old provider export: %v", err)
	}
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M3.g": oldExport}}); err == nil || !strings.Contains(err.Error(), "outside the exact gc/type-subtyping link product") {
		t.Fatalf("cross-product provider link = %v, want exact pair rejection", err)
	}
	if refs, closed := resourceState(oldProvider); refs != 0 || closed {
		t.Fatalf("cross-product rejection retained old provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}

	for name, item := range map[string]struct {
		compiled *Compiled
		blob     []byte
	}{"provider": {providerCompiled, providerBlob}, "consumer": {consumerCompiled, consumerBlob}} {
		meta := (&Module{c: item.compiled}).Metadata()
		var loaded Compiled
		if err := unmarshalCompiled(&loaded, item.blob[5:]); err != nil {
			t.Fatalf("private %s reload: %v", name, err)
		}
		if loaded.stagedGCTypeSubtypingProduct() != 0 || loaded.stagedFeatures().IsEnabled(CoreFeatureGC) {
			t.Fatalf("private %s reload inherited admission: product=%v features=%v", name, loaded.stagedGCTypeSubtypingProduct(), loaded.stagedFeatures())
		}
		loadedMeta := (&Module{c: &loaded}).Metadata()
		if !reflect.DeepEqual(loadedMeta.Types, meta.Types) || !reflect.DeepEqual(loadedMeta.Functions, meta.Functions) {
			t.Fatalf("private %s reload changed type/function metadata", name)
		}
		if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
			t.Fatalf("private %s reload instantiate = %v, want required-feature rejection", name, err)
		}
		_ = loaded.Close()
		var public Compiled
		if err := public.UnmarshalBinary(item.blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
			t.Fatalf("public %s reload = %v, want unsupported GC rejection", name, err)
		}
		if _, err := Capture(item.compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC reference products") {
			t.Fatalf("%s snapshot = %v, want GC reference-product rejection", name, err)
		}
	}
}

func TestStagedGCTypeSubtypingStructProjectionLinkingClusterLifecycle(t *testing.T) {
	providerData := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructProjectionLinkProviderPin)
	consumerData := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructProjectionLinkConsumerPin)
	providerCompiled, err := compileStagedGCTypeSubtypingProductForTest(providerData)
	if err != nil {
		t.Fatalf("compile provider: %v", err)
	}
	defer providerCompiled.Close()
	consumerCompiled, err := compileStagedGCTypeSubtypingProductForTest(consumerData)
	if err != nil {
		t.Fatalf("compile consumer: %v", err)
	}
	defer consumerCompiled.Close()
	if providerCompiled.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingStructProjectionLinkProvider || consumerCompiled.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingStructProjectionLinkConsumer {
		t.Fatalf("provider/consumer products = %v/%v", providerCompiled.stagedGCTypeSubtypingProduct(), consumerCompiled.stagedGCTypeSubtypingProduct())
	}
	if !providerCompiled.NeedsFuncRefDescs || !consumerCompiled.NeedsFuncRefDescs {
		t.Fatal("struct projection link provider/consumer must own canonical descriptor arenas")
	}
	providerBlob, err := marshalCompiled(providerCompiled)
	if err != nil {
		t.Fatalf("marshal provider: %v", err)
	}
	consumerBlob, err := marshalCompiled(consumerCompiled)
	if err != nil {
		t.Fatalf("marshal unlinked consumer: %v", err)
	}
	if got := [6]int{len(providerData), len(providerCompiled.Code), len(providerBlob), len(consumerData), len(consumerCompiled.Code), len(consumerBlob)}; got != [6]int{104, 77, 483, 85, 0, 406} {
		t.Fatalf("struct projection link wasm/code/codec sizes = %v, want [104 77 483 85 0 406]", got)
	}

	resourceState := func(in *Instance) (refs int, closed bool) {
		in.lifeMu.Lock()
		refs, closed = in.resourceRefs, in.resourcesClosed
		in.lifeMu.Unlock()
		return
	}
	instantiateProvider := func() (*Instance, *InstanceExport) {
		in, err := instantiateCore(providerCompiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate provider: %v", err)
		}
		if in.gc != nil {
			t.Fatal("struct projection link provider allocated a collector")
		}
		ex, err := in.ExportedFunc("g")
		if err != nil {
			t.Fatalf("export g: %v", err)
		}
		return in, ex
	}

	provider, exported := instantiateProvider()
	if got, want := len(provider.funcRefDescs), 2*coreruntime.FuncRefDescBytes; got != want {
		t.Fatalf("provider descriptor arena = %d bytes, want %d", got, want)
	}
	providerOff := coreruntime.FuncRefDescBytes
	providerIdentity := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryRefSlotOffset:])
	if providerIdentity != uint64(uintptr(unsafe.Pointer(&provider.funcRefDescs[providerOff]))) {
		t.Fatalf("provider descriptor identity = %#x, want self-owned canonical address", providerIdentity)
	}
	if got := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryCodePtrOffset:]); got == 0 {
		t.Fatal("provider descriptor has a null code pointer")
	}
	if got, err := provider.Invoke("g"); err != nil || len(got) != 0 {
		t.Fatalf("provider g = %v, %v; want empty success", got, err)
	}
	var invokeErr error
	allocs := testing.AllocsPerRun(1000, func() {
		_, invokeErr = provider.Invoke("g")
	})
	if invokeErr != nil || allocs != 0 {
		t.Fatalf("provider g steady state = %v, allocs=%v; want nil, 0", invokeErr, allocs)
	}
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M4.g": exported}})
	if err != nil {
		t.Fatalf("instantiate compatible consumer: %v", err)
	}
	if consumer.gc != nil {
		t.Fatal("struct projection link consumer allocated a collector")
	}
	if got, want := len(consumer.funcRefDescs), 2*coreruntime.FuncRefDescBytes; got != want {
		t.Fatalf("consumer descriptor arena = %d bytes, want %d", got, want)
	}
	consumerOff := coreruntime.FuncRefDescBytes
	if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryCodePtrOffset:]); got == 0 {
		t.Fatal("consumer import descriptor has a null code pointer")
	}
	if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryRefSlotOffset:]); got != providerIdentity {
		t.Fatalf("consumer import identity = %#x, want provider canonical %#x", got, providerIdentity)
	}
	if refs, closed := resourceState(provider); refs != 1 || closed {
		t.Fatalf("consumer retained provider refs/resourcesClosed = %d/%v, want 1/false", refs, closed)
	}
	if _, err := marshalCompiled(consumer.c); err != nil {
		t.Fatalf("binding-independent projected-struct consumer codec: %v", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("logical provider close: %v", err)
	}
	if refs, closed := resourceState(provider); refs != 1 || closed {
		t.Fatalf("logically closed provider refs/resourcesClosed = %d/%v, want 1/false", refs, closed)
	}
	if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryRefSlotOffset:]); got != providerIdentity {
		t.Fatalf("post-close consumer identity = %#x, want retained %#x", got, providerIdentity)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer close: %v", err)
	}
	if refs, closed := resourceState(provider); refs != 0 || !closed {
		t.Fatalf("provider after consumer close refs/resourcesClosed = %d/%v, want 0/true", refs, closed)
	}

	provider2, exported2 := instantiateProvider()
	consumer2, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M4.g": exported2}})
	if err != nil {
		t.Fatalf("instantiate consumer-first close pair: %v", err)
	}
	if err := consumer2.Close(); err != nil {
		t.Fatalf("consumer-first close: %v", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || closed {
		t.Fatalf("consumer-first provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}
	invalidExport := &InstanceExport{inst: provider2, localIdx: len(provider2.c.Entry)}
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M4.g": invalidExport}}); err == nil || !strings.Contains(err.Error(), "unavailable function") {
		t.Fatalf("invalid provider export link = %v, want unavailable-function rejection", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || closed {
		t.Fatalf("failed link retained provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}
	if err := provider2.Close(); err != nil {
		t.Fatalf("provider-final close: %v", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || !closed {
		t.Fatalf("provider-final refs/resourcesClosed = %d/%v, want 0/true", refs, closed)
	}

	host := HostFunc(func(HostModule, []uint64, []uint64) {})
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M4.g": host}}); err == nil || !strings.Contains(err.Error(), "exact gc/type-subtyping link provider") {
		t.Fatalf("host link = %v, want exact provider rejection", err)
	}
	oldProviderCompiled, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructLinkProviderPin))
	if err != nil {
		t.Fatalf("compile old provider: %v", err)
	}
	defer oldProviderCompiled.Close()
	oldProvider, err := instantiateCore(oldProviderCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate old provider: %v", err)
	}
	defer oldProvider.Close()
	oldExport, err := oldProvider.ExportedFunc("g")
	if err != nil {
		t.Fatalf("old provider export: %v", err)
	}
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M4.g": oldExport}}); err == nil || !strings.Contains(err.Error(), "outside the exact gc/type-subtyping link product") {
		t.Fatalf("cross-product provider link = %v, want exact pair rejection", err)
	}
	if refs, closed := resourceState(oldProvider); refs != 0 || closed {
		t.Fatalf("cross-product rejection retained old provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}

	for name, item := range map[string]struct {
		compiled *Compiled
		blob     []byte
	}{"provider": {providerCompiled, providerBlob}, "consumer": {consumerCompiled, consumerBlob}} {
		meta := (&Module{c: item.compiled}).Metadata()
		var loaded Compiled
		if err := unmarshalCompiled(&loaded, item.blob[5:]); err != nil {
			t.Fatalf("private %s reload: %v", name, err)
		}
		if loaded.stagedGCTypeSubtypingProduct() != 0 || loaded.stagedFeatures().IsEnabled(CoreFeatureGC) {
			t.Fatalf("private %s reload inherited admission: product=%v features=%v", name, loaded.stagedGCTypeSubtypingProduct(), loaded.stagedFeatures())
		}
		loadedMeta := (&Module{c: &loaded}).Metadata()
		if !reflect.DeepEqual(loadedMeta.Types, meta.Types) || !reflect.DeepEqual(loadedMeta.Functions, meta.Functions) {
			t.Fatalf("private %s reload changed type/function metadata", name)
		}
		if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
			t.Fatalf("private %s reload instantiate = %v, want required-feature rejection", name, err)
		}
		_ = loaded.Close()
		var public Compiled
		if err := public.UnmarshalBinary(item.blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
			t.Fatalf("public %s reload = %v, want unsupported GC rejection", name, err)
		}
		if _, err := Capture(item.compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC reference products") {
			t.Fatalf("%s snapshot = %v, want GC reference-product rejection", name, err)
		}
	}
}

func TestStagedGCTypeSubtypingDuplicateRecursiveLinkingClusterLifecycle(t *testing.T) {
	providerData := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingDuplicateRecursiveLinkProviderPin)
	consumerData := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingDuplicateRecursiveLinkConsumerPin)
	providerCompiled, err := compileStagedGCTypeSubtypingProductForTest(providerData)
	if err != nil {
		t.Fatal(err)
	}
	defer providerCompiled.Close()
	consumerCompiled, err := compileStagedGCTypeSubtypingProductForTest(consumerData)
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCompiled.Close()
	providerBlob, err := marshalCompiled(providerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	consumerBlob, err := marshalCompiled(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	if got := [6]int{len(providerData), len(providerCompiled.Code), len(providerBlob), len(consumerData), len(consumerCompiled.Code), len(consumerBlob)}; got != [6]int{100, 253, 531, 92, 0, 315} {
		t.Fatalf("wasm/code/codec sizes = %v", got)
	}
	state := func(in *Instance) (int, bool) {
		in.lifeMu.Lock()
		defer in.lifeMu.Unlock()
		return in.resourceRefs, in.resourcesClosed
	}
	newProvider := func() (*Instance, map[string]*InstanceExport) {
		in, e := instantiateCore(providerCompiled, InstantiateOptions{})
		if e != nil {
			t.Fatal(e)
		}
		if in.gc != nil {
			t.Fatal("duplicate recursive provider allocated collector")
		}
		ex := map[string]*InstanceExport{}
		for _, name := range []string{"f11", "f12"} {
			ex[name], e = in.ExportedFunc(name)
			if e != nil {
				t.Fatal(e)
			}
		}
		return in, ex
	}
	provider, exports := newProvider()
	if len(provider.funcRefDescs) != 3*coreruntime.TableEntryBytes {
		t.Fatalf("provider arena = %d", len(provider.funcRefDescs))
	}
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M8.f11": exports["f11"], "M8.f12": exports["f12"]}})
	if err != nil {
		t.Fatal(err)
	}
	if consumer.gc != nil || len(consumer.funcRefDescs) != 5*coreruntime.TableEntryBytes {
		t.Fatalf("consumer gc/arena = %v/%d", consumer.gc, len(consumer.funcRefDescs))
	}
	for i, providerFunc := range []int{0, 0, 1, 1} {
		consumerOff := (i + 1) * coreruntime.TableEntryBytes
		providerOff := (providerFunc + 1) * coreruntime.TableEntryBytes
		got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryRefSlotOffset:])
		want := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryRefSlotOffset:])
		if got == 0 || got != want {
			t.Fatalf("import %d identity = %#x, want %#x", i, got, want)
		}
	}
	if refs, closed := state(provider); refs != 1 || closed {
		t.Fatalf("deduplicated owner state = %d/%v", refs, closed)
	}
	if _, err := marshalCompiled(consumer.c); err == nil || !strings.Contains(err.Error(), "retained gc/type-subtyping function bindings") {
		t.Fatalf("linked codec = %v", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	if refs, closed := state(provider); refs != 1 || closed {
		t.Fatalf("provider-first retained state = %d/%v", refs, closed)
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if refs, closed := state(provider); refs != 0 || !closed {
		t.Fatalf("provider-first final state = %d/%v", refs, closed)
	}
	provider2, exports2 := newProvider()
	consumer2, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M8.f11": exports2["f11"], "M8.f12": exports2["f12"]}})
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer2.Close(); err != nil {
		t.Fatal(err)
	}
	if refs, closed := state(provider2); refs != 0 || closed {
		t.Fatalf("consumer-first state = %d/%v", refs, closed)
	}
	if err := provider2.Close(); err != nil {
		t.Fatal(err)
	}
	host := HostFunc(func(HostModule, []uint64, []uint64) {})
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M8.f11": host, "M8.f12": host}}); err == nil || !strings.Contains(err.Error(), "exact gc/type-subtyping link provider") {
		t.Fatalf("host link = %v", err)
	}
	for name, item := range map[string]struct {
		compiled *Compiled
		blob     []byte
	}{"provider": {providerCompiled, providerBlob}, "consumer": {consumerCompiled, consumerBlob}} {
		var loaded Compiled
		if err := unmarshalCompiled(&loaded, item.blob[5:]); err != nil {
			t.Fatal(err)
		}
		if loaded.stagedGCTypeSubtypingProduct() != 0 || loaded.stagedFeatures().IsEnabled(CoreFeatureGC) {
			t.Fatalf("private %s reload inherited admission", name)
		}
		if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
			t.Fatalf("private %s instantiate = %v", name, err)
		}
		_ = loaded.Close()
		var public Compiled
		if err := public.UnmarshalBinary(item.blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
			t.Fatalf("public %s reload = %v", name, err)
		}
		if _, err := Capture(item.compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC reference products") {
			t.Fatalf("%s snapshot = %v", name, err)
		}
	}
}

func TestStagedGCTypeSubtypingExtendedProjectionLinkingClusterLifecycle(t *testing.T) {
	providerData := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingExtendedProjectionLinkProviderPin)
	consumerData := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingExtendedProjectionLinkConsumerPin)
	providerCompiled, err := compileStagedGCTypeSubtypingProductForTest(providerData)
	if err != nil {
		t.Fatalf("compile provider: %v", err)
	}
	defer providerCompiled.Close()
	consumerCompiled, err := compileStagedGCTypeSubtypingProductForTest(consumerData)
	if err != nil {
		t.Fatalf("compile consumer: %v", err)
	}
	defer consumerCompiled.Close()
	if providerCompiled.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingExtendedProjectionLinkProvider || consumerCompiled.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingExtendedProjectionLinkConsumer {
		t.Fatalf("provider/consumer products = %v/%v", providerCompiled.stagedGCTypeSubtypingProduct(), consumerCompiled.stagedGCTypeSubtypingProduct())
	}
	if !providerCompiled.NeedsFuncRefDescs || !consumerCompiled.NeedsFuncRefDescs {
		t.Fatal("extended projection link provider/consumer must own canonical descriptor arenas")
	}
	providerBlob, err := marshalCompiled(providerCompiled)
	if err != nil {
		t.Fatalf("marshal provider: %v", err)
	}
	consumerBlob, err := marshalCompiled(consumerCompiled)
	if err != nil {
		t.Fatalf("marshal unlinked consumer: %v", err)
	}
	if got := [6]int{len(providerData), len(providerCompiled.Code), len(providerBlob), len(consumerData), len(consumerCompiled.Code), len(consumerBlob)}; got != [6]int{114, 77, 561, 102, 0, 502} {
		t.Fatalf("extended projection link wasm/code/codec sizes = %v, want [114 77 561 102 0 502]", got)
	}

	resourceState := func(in *Instance) (refs int, closed bool) {
		in.lifeMu.Lock()
		refs, closed = in.resourceRefs, in.resourcesClosed
		in.lifeMu.Unlock()
		return
	}
	instantiateProvider := func() (*Instance, *InstanceExport) {
		in, err := instantiateCore(providerCompiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate provider: %v", err)
		}
		if in.gc != nil {
			t.Fatal("extended projection link provider allocated a collector")
		}
		ex, err := in.ExportedFunc("h")
		if err != nil {
			t.Fatalf("export h: %v", err)
		}
		return in, ex
	}

	provider, exported := instantiateProvider()
	if got, want := len(provider.funcRefDescs), 2*coreruntime.TableEntryBytes; got != want {
		t.Fatalf("provider descriptor arena = %d bytes, want %d", got, want)
	}
	providerOff := coreruntime.TableEntryBytes
	providerIdentity := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryRefSlotOffset:])
	if providerIdentity != uint64(uintptr(unsafe.Pointer(&provider.funcRefDescs[providerOff]))) {
		t.Fatalf("provider descriptor identity = %#x, want self-owned canonical address", providerIdentity)
	}
	if got := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryCodePtrOffset:]); got == 0 {
		t.Fatal("provider descriptor has a null code pointer")
	}
	if got, err := provider.Invoke("h"); err != nil || len(got) != 0 {
		t.Fatalf("provider h = %v, %v; want empty success", got, err)
	}
	var invokeErr error
	allocs := testing.AllocsPerRun(1000, func() {
		_, invokeErr = provider.Invoke("h")
	})
	if invokeErr != nil || allocs != 0 {
		t.Fatalf("provider h steady state = %v, allocs=%v; want nil, 0", invokeErr, allocs)
	}
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M7.h": exported}})
	if err != nil {
		t.Fatalf("instantiate compatible two-import consumer: %v", err)
	}
	if consumer.gc != nil {
		t.Fatal("extended projection link consumer allocated a collector")
	}
	if got, want := len(consumer.funcRefDescs), 3*coreruntime.TableEntryBytes; got != want {
		t.Fatalf("consumer descriptor arena = %d bytes, want %d", got, want)
	}
	for importIndex := 0; importIndex < 2; importIndex++ {
		consumerOff := (importIndex + 1) * coreruntime.TableEntryBytes
		if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryCodePtrOffset:]); got == 0 {
			t.Fatalf("consumer import %d descriptor has a null code pointer", importIndex)
		}
		if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryRefSlotOffset:]); got != providerIdentity {
			t.Fatalf("consumer import %d identity = %#x, want provider canonical %#x", importIndex, got, providerIdentity)
		}
	}
	if refs, closed := resourceState(provider); refs != 1 || closed {
		t.Fatalf("duplicate imports retained provider refs/resourcesClosed = %d/%v, want 1/false", refs, closed)
	}
	if _, err := marshalCompiled(consumer.c); err == nil || !strings.Contains(err.Error(), "retained gc/type-subtyping function bindings") {
		t.Fatalf("linked consumer codec = %v, want retained-binding rejection", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("logical provider close: %v", err)
	}
	if refs, closed := resourceState(provider); refs != 1 || closed {
		t.Fatalf("logically closed provider refs/resourcesClosed = %d/%v, want 1/false", refs, closed)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer close: %v", err)
	}
	if refs, closed := resourceState(provider); refs != 0 || !closed {
		t.Fatalf("provider after consumer close refs/resourcesClosed = %d/%v, want 0/true", refs, closed)
	}

	provider2, exported2 := instantiateProvider()
	consumer2, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M7.h": exported2}})
	if err != nil {
		t.Fatalf("instantiate consumer-first close pair: %v", err)
	}
	if err := consumer2.Close(); err != nil {
		t.Fatalf("consumer-first close: %v", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || closed {
		t.Fatalf("consumer-first provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}
	invalidExport := &InstanceExport{inst: provider2, localIdx: len(provider2.c.Entry)}
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M7.h": invalidExport}}); err == nil || !strings.Contains(err.Error(), "unavailable function") {
		t.Fatalf("invalid provider export link = %v, want unavailable-function rejection", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || closed {
		t.Fatalf("failed link retained provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}
	if err := provider2.Close(); err != nil {
		t.Fatalf("provider-final close: %v", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || !closed {
		t.Fatalf("provider-final refs/resourcesClosed = %d/%v, want 0/true", refs, closed)
	}

	host := HostFunc(func(HostModule, []uint64, []uint64) {})
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M7.h": host}}); err == nil || !strings.Contains(err.Error(), "exact gc/type-subtyping link provider") {
		t.Fatalf("host link = %v, want exact provider rejection", err)
	}
	oldProviderCompiled, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructProjectionLinkProviderPin))
	if err != nil {
		t.Fatalf("compile old provider: %v", err)
	}
	defer oldProviderCompiled.Close()
	oldProvider, err := instantiateCore(oldProviderCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate old provider: %v", err)
	}
	defer oldProvider.Close()
	oldExport, err := oldProvider.ExportedFunc("g")
	if err != nil {
		t.Fatalf("old provider export: %v", err)
	}
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M7.h": oldExport}}); err == nil || !strings.Contains(err.Error(), "outside the exact gc/type-subtyping link product") {
		t.Fatalf("cross-product provider link = %v, want exact pair rejection", err)
	}
	if refs, closed := resourceState(oldProvider); refs != 0 || closed {
		t.Fatalf("cross-product rejection retained old provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}

	for name, item := range map[string]struct {
		compiled *Compiled
		blob     []byte
	}{"provider": {providerCompiled, providerBlob}, "consumer": {consumerCompiled, consumerBlob}} {
		meta := (&Module{c: item.compiled}).Metadata()
		var loaded Compiled
		if err := unmarshalCompiled(&loaded, item.blob[5:]); err != nil {
			t.Fatalf("private %s reload: %v", name, err)
		}
		if loaded.stagedGCTypeSubtypingProduct() != 0 || loaded.stagedFeatures().IsEnabled(CoreFeatureGC) {
			t.Fatalf("private %s reload inherited admission: product=%v features=%v", name, loaded.stagedGCTypeSubtypingProduct(), loaded.stagedFeatures())
		}
		loadedMeta := (&Module{c: &loaded}).Metadata()
		if !reflect.DeepEqual(loadedMeta.Types, meta.Types) || !reflect.DeepEqual(loadedMeta.Functions, meta.Functions) {
			t.Fatalf("private %s reload changed type/function metadata", name)
		}
		if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
			t.Fatalf("private %s reload instantiate = %v, want required-feature rejection", name, err)
		}
		_ = loaded.Close()
		var public Compiled
		if err := public.UnmarshalBinary(item.blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
			t.Fatalf("public %s reload = %v, want unsupported GC rejection", name, err)
		}
		if _, err := Capture(item.compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC reference products") {
			t.Fatalf("%s snapshot = %v, want GC reference-product rejection", name, err)
		}
	}
}

func TestStagedGCTypeSubtypingIndependentStructLinkingClusterLifecycle(t *testing.T) {
	providerData := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingIndependentStructLinkProviderPin)
	consumerData := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingIndependentStructLinkConsumerPin)
	providerCompiled, err := compileStagedGCTypeSubtypingProductForTest(providerData)
	if err != nil {
		t.Fatalf("compile provider: %v", err)
	}
	defer providerCompiled.Close()
	consumerCompiled, err := compileStagedGCTypeSubtypingProductForTest(consumerData)
	if err != nil {
		t.Fatalf("compile consumer: %v", err)
	}
	defer consumerCompiled.Close()
	if providerCompiled.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingIndependentStructLinkProvider || consumerCompiled.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingIndependentStructLinkConsumer {
		t.Fatalf("provider/consumer products = %v/%v", providerCompiled.stagedGCTypeSubtypingProduct(), consumerCompiled.stagedGCTypeSubtypingProduct())
	}
	if !providerCompiled.NeedsFuncRefDescs || !consumerCompiled.NeedsFuncRefDescs {
		t.Fatal("independent struct link provider/consumer must own canonical descriptor arenas")
	}
	providerBlob, err := marshalCompiled(providerCompiled)
	if err != nil {
		t.Fatalf("marshal provider: %v", err)
	}
	consumerBlob, err := marshalCompiled(consumerCompiled)
	if err != nil {
		t.Fatalf("marshal unlinked consumer: %v", err)
	}
	if got := [6]int{len(providerData), len(providerCompiled.Code), len(providerBlob), len(consumerData), len(consumerCompiled.Code), len(consumerBlob)}; got != [6]int{82, 77, 403, 63, 0, 326} {
		t.Fatalf("independent struct link wasm/code/codec sizes = %v, want [82 77 403 63 0 326]", got)
	}

	resourceState := func(in *Instance) (refs int, closed bool) {
		in.lifeMu.Lock()
		refs, closed = in.resourceRefs, in.resourcesClosed
		in.lifeMu.Unlock()
		return
	}
	instantiateProvider := func() (*Instance, *InstanceExport) {
		in, err := instantiateCore(providerCompiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate provider: %v", err)
		}
		if in.gc != nil {
			t.Fatal("independent struct link provider allocated a collector")
		}
		ex, err := in.ExportedFunc("g")
		if err != nil {
			t.Fatalf("export g: %v", err)
		}
		return in, ex
	}

	provider, exported := instantiateProvider()
	if got, want := len(provider.funcRefDescs), 2*coreruntime.TableEntryBytes; got != want {
		t.Fatalf("provider descriptor arena = %d bytes, want %d", got, want)
	}
	providerOff := coreruntime.TableEntryBytes
	providerIdentity := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryRefSlotOffset:])
	if providerIdentity != uint64(uintptr(unsafe.Pointer(&provider.funcRefDescs[providerOff]))) {
		t.Fatalf("provider descriptor identity = %#x, want self-owned canonical address", providerIdentity)
	}
	if got := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryCodePtrOffset:]); got == 0 {
		t.Fatal("provider descriptor has a null code pointer")
	}
	if got, err := provider.Invoke("g"); err != nil || len(got) != 0 {
		t.Fatalf("provider g = %v, %v; want empty success", got, err)
	}
	var invokeErr error
	allocs := testing.AllocsPerRun(1000, func() {
		_, invokeErr = provider.Invoke("g")
	})
	if invokeErr != nil || allocs != 0 {
		t.Fatalf("provider g steady state = %v, allocs=%v; want nil, 0", invokeErr, allocs)
	}
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M6.g": exported}})
	if err != nil {
		t.Fatalf("instantiate compatible consumer: %v", err)
	}
	if consumer.gc != nil {
		t.Fatal("independent struct link consumer allocated a collector")
	}
	if got, want := len(consumer.funcRefDescs), 2*coreruntime.TableEntryBytes; got != want {
		t.Fatalf("consumer descriptor arena = %d bytes, want %d", got, want)
	}
	consumerOff := coreruntime.TableEntryBytes
	if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryCodePtrOffset:]); got == 0 {
		t.Fatal("consumer import descriptor has a null code pointer")
	}
	if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryRefSlotOffset:]); got != providerIdentity {
		t.Fatalf("consumer import identity = %#x, want provider canonical %#x", got, providerIdentity)
	}
	if refs, closed := resourceState(provider); refs != 1 || closed {
		t.Fatalf("consumer retained provider refs/resourcesClosed = %d/%v, want 1/false", refs, closed)
	}
	if _, err := marshalCompiled(consumer.c); err == nil || !strings.Contains(err.Error(), "retained gc/type-subtyping function bindings") {
		t.Fatalf("linked consumer codec = %v, want retained-binding rejection", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("logical provider close: %v", err)
	}
	if refs, closed := resourceState(provider); refs != 1 || closed {
		t.Fatalf("logically closed provider refs/resourcesClosed = %d/%v, want 1/false", refs, closed)
	}
	if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryRefSlotOffset:]); got != providerIdentity {
		t.Fatalf("post-close consumer identity = %#x, want retained %#x", got, providerIdentity)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer close: %v", err)
	}
	if refs, closed := resourceState(provider); refs != 0 || !closed {
		t.Fatalf("provider after consumer close refs/resourcesClosed = %d/%v, want 0/true", refs, closed)
	}

	provider2, exported2 := instantiateProvider()
	consumer2, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M6.g": exported2}})
	if err != nil {
		t.Fatalf("instantiate consumer-first close pair: %v", err)
	}
	if err := consumer2.Close(); err != nil {
		t.Fatalf("consumer-first close: %v", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || closed {
		t.Fatalf("consumer-first provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}
	invalidExport := &InstanceExport{inst: provider2, localIdx: len(provider2.c.Entry)}
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M6.g": invalidExport}}); err == nil || !strings.Contains(err.Error(), "unavailable function") {
		t.Fatalf("invalid provider export link = %v, want unavailable-function rejection", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || closed {
		t.Fatalf("failed link retained provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}
	if err := provider2.Close(); err != nil {
		t.Fatalf("provider-final close: %v", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || !closed {
		t.Fatalf("provider-final refs/resourcesClosed = %d/%v, want 0/true", refs, closed)
	}

	host := HostFunc(func(HostModule, []uint64, []uint64) {})
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M6.g": host}}); err == nil || !strings.Contains(err.Error(), "exact gc/type-subtyping link provider") {
		t.Fatalf("host link = %v, want exact provider rejection", err)
	}
	oldProviderCompiled, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructMismatchLinkProviderPin))
	if err != nil {
		t.Fatalf("compile old provider: %v", err)
	}
	defer oldProviderCompiled.Close()
	oldProvider, err := instantiateCore(oldProviderCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate old provider: %v", err)
	}
	defer oldProvider.Close()
	oldExport, err := oldProvider.ExportedFunc("g")
	if err != nil {
		t.Fatalf("old provider export: %v", err)
	}
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M6.g": oldExport}}); err == nil || !strings.Contains(err.Error(), "outside the exact gc/type-subtyping link product") {
		t.Fatalf("cross-product provider link = %v, want exact pair rejection", err)
	}
	if refs, closed := resourceState(oldProvider); refs != 0 || closed {
		t.Fatalf("cross-product rejection retained old provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}

	for name, item := range map[string]struct {
		compiled *Compiled
		blob     []byte
	}{"provider": {providerCompiled, providerBlob}, "consumer": {consumerCompiled, consumerBlob}} {
		meta := (&Module{c: item.compiled}).Metadata()
		var loaded Compiled
		if err := unmarshalCompiled(&loaded, item.blob[5:]); err != nil {
			t.Fatalf("private %s reload: %v", name, err)
		}
		if loaded.stagedGCTypeSubtypingProduct() != 0 || loaded.stagedFeatures().IsEnabled(CoreFeatureGC) {
			t.Fatalf("private %s reload inherited admission: product=%v features=%v", name, loaded.stagedGCTypeSubtypingProduct(), loaded.stagedFeatures())
		}
		loadedMeta := (&Module{c: &loaded}).Metadata()
		if !reflect.DeepEqual(loadedMeta.Types, meta.Types) || !reflect.DeepEqual(loadedMeta.Functions, meta.Functions) {
			t.Fatalf("private %s reload changed type/function metadata", name)
		}
		if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
			t.Fatalf("private %s reload instantiate = %v, want required-feature rejection", name, err)
		}
		_ = loaded.Close()
		var public Compiled
		if err := public.UnmarshalBinary(item.blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
			t.Fatalf("public %s reload = %v, want unsupported GC rejection", name, err)
		}
		if _, err := Capture(item.compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC reference products") {
			t.Fatalf("%s snapshot = %v, want GC reference-product rejection", name, err)
		}
	}
}

func TestStagedGCTypeSubtypingStructMismatchLinkingClusterLifecycle(t *testing.T) {
	providerData := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructMismatchLinkProviderPin)
	consumerData := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructMismatchLinkConsumerPin)
	providerCompiled, err := compileStagedGCTypeSubtypingProductForTest(providerData)
	if err != nil {
		t.Fatalf("compile provider: %v", err)
	}
	defer providerCompiled.Close()
	consumerCompiled, err := compileStagedGCTypeSubtypingProductForTest(consumerData)
	if err != nil {
		t.Fatalf("compile consumer: %v", err)
	}
	defer consumerCompiled.Close()
	if providerCompiled.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingStructMismatchLinkProvider || consumerCompiled.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingStructMismatchLinkConsumer {
		t.Fatalf("provider/consumer products = %v/%v", providerCompiled.stagedGCTypeSubtypingProduct(), consumerCompiled.stagedGCTypeSubtypingProduct())
	}
	if !providerCompiled.NeedsFuncRefDescs || !consumerCompiled.NeedsFuncRefDescs {
		t.Fatal("struct mismatch provider/consumer must have bounded canonical descriptor requirements")
	}
	providerBlob, err := marshalCompiled(providerCompiled)
	if err != nil {
		t.Fatalf("marshal provider: %v", err)
	}
	consumerBlob, err := marshalCompiled(consumerCompiled)
	if err != nil {
		t.Fatalf("marshal unlinked consumer: %v", err)
	}
	if got := [6]int{len(providerData), len(providerCompiled.Code), len(providerBlob), len(consumerData), len(consumerCompiled.Code), len(consumerBlob)}; got != [6]int{82, 77, 404, 51, 0, 237} {
		t.Fatalf("struct mismatch wasm/code/codec sizes = %v, want [82 77 404 51 0 237]", got)
	}
	if got, want := (len(providerCompiled.FuncTypeID)+1)*coreruntime.FuncRefDescBytes, 2*coreruntime.FuncRefDescBytes; got != want {
		t.Fatalf("provider descriptor requirement = %d bytes, want %d", got, want)
	}
	if got, want := (len(consumerCompiled.FuncTypeID)+1)*coreruntime.FuncRefDescBytes, 2*coreruntime.FuncRefDescBytes; got != want {
		t.Fatalf("attempted consumer descriptor requirement = %d bytes, want %d", got, want)
	}

	resourceState := func(in *Instance) (refs int, closed bool) {
		in.lifeMu.Lock()
		refs, closed = in.resourceRefs, in.resourcesClosed
		in.lifeMu.Unlock()
		return
	}
	instantiateProvider := func() (*Instance, *InstanceExport) {
		in, err := instantiateCore(providerCompiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate provider: %v", err)
		}
		if in.gc != nil {
			t.Fatal("struct mismatch provider allocated a collector")
		}
		ex, err := in.ExportedFunc("g")
		if err != nil {
			t.Fatalf("export g: %v", err)
		}
		return in, ex
	}

	provider, exported := instantiateProvider()
	if got, want := len(provider.funcRefDescs), 2*coreruntime.FuncRefDescBytes; got != want {
		t.Fatalf("provider descriptor arena = %d bytes, want %d", got, want)
	}
	providerOff := coreruntime.FuncRefDescBytes
	providerIdentity := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryRefSlotOffset:])
	if providerIdentity != uint64(uintptr(unsafe.Pointer(&provider.funcRefDescs[providerOff]))) {
		t.Fatalf("provider descriptor identity = %#x, want self-owned canonical address", providerIdentity)
	}
	if got := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryCodePtrOffset:]); got == 0 {
		t.Fatal("provider descriptor has a null code pointer")
	}
	if got, err := provider.Invoke("g"); err != nil || len(got) != 0 {
		t.Fatalf("provider g = %v, %v; want empty success", got, err)
	}
	var invokeErr error
	allocs := testing.AllocsPerRun(1000, func() {
		_, invokeErr = provider.Invoke("g")
	})
	if invokeErr != nil || allocs != 0 {
		t.Fatalf("provider g steady state = %v, allocs=%v; want nil, 0", invokeErr, allocs)
	}
	if in, linkErr := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M5.g": exported}}); in != nil || linkErr == nil || !strings.Contains(linkErr.Error(), "signature mismatch") {
		t.Fatalf("incompatible consumer link = %v, %v; want nil/signature mismatch", in, linkErr)
	}
	if refs, closed := resourceState(provider); refs != 0 || closed {
		t.Fatalf("mismatched consumer retained provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}
	if consumerCompiled.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingStructMismatchLinkConsumer || len(consumerCompiled.Code) != 0 {
		t.Fatal("failed consumer link published or mutated the unlinked compiled product")
	}
	if blob, err := marshalCompiled(consumerCompiled); err != nil || len(blob) != len(consumerBlob) {
		t.Fatalf("post-mismatch consumer codec = %d bytes, %v; want unchanged %d-byte unlinked artifact", len(blob), err, len(consumerBlob))
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("provider close after mismatch: %v", err)
	}
	if refs, closed := resourceState(provider); refs != 0 || !closed {
		t.Fatalf("provider after mismatch close refs/resourcesClosed = %d/%v, want 0/true", refs, closed)
	}

	provider2, _ := instantiateProvider()
	defer provider2.Close()
	invalidExport := &InstanceExport{inst: provider2, localIdx: len(provider2.c.Entry)}
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M5.g": invalidExport}}); err == nil || !strings.Contains(err.Error(), "unavailable function") {
		t.Fatalf("invalid provider export link = %v, want unavailable-function rejection", err)
	}
	if refs, closed := resourceState(provider2); refs != 0 || closed {
		t.Fatalf("invalid export retained provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}
	host := HostFunc(func(HostModule, []uint64, []uint64) {})
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M5.g": host}}); err == nil || !strings.Contains(err.Error(), "exact gc/type-subtyping link provider") {
		t.Fatalf("host link = %v, want exact provider rejection", err)
	}
	oldProviderCompiled, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructProjectionLinkProviderPin))
	if err != nil {
		t.Fatalf("compile old provider: %v", err)
	}
	defer oldProviderCompiled.Close()
	oldProvider, err := instantiateCore(oldProviderCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate old provider: %v", err)
	}
	defer oldProvider.Close()
	oldExport, err := oldProvider.ExportedFunc("g")
	if err != nil {
		t.Fatalf("old provider export: %v", err)
	}
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M5.g": oldExport}}); err == nil || !strings.Contains(err.Error(), "outside the exact gc/type-subtyping link product") {
		t.Fatalf("cross-product provider link = %v, want exact pair rejection", err)
	}
	if refs, closed := resourceState(oldProvider); refs != 0 || closed {
		t.Fatalf("cross-product rejection retained old provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}
	if refs, closed := resourceState(provider2); refs != 0 || closed {
		t.Fatalf("rejections retained exact provider refs/resourcesClosed = %d/%v, want 0/false", refs, closed)
	}

	for name, item := range map[string]struct {
		compiled *Compiled
		blob     []byte
	}{"provider": {providerCompiled, providerBlob}, "consumer": {consumerCompiled, consumerBlob}} {
		meta := (&Module{c: item.compiled}).Metadata()
		var loaded Compiled
		if err := unmarshalCompiled(&loaded, item.blob[5:]); err != nil {
			t.Fatalf("private %s reload: %v", name, err)
		}
		if loaded.stagedGCTypeSubtypingProduct() != 0 || loaded.stagedFeatures().IsEnabled(CoreFeatureGC) {
			t.Fatalf("private %s reload inherited admission: product=%v features=%v", name, loaded.stagedGCTypeSubtypingProduct(), loaded.stagedFeatures())
		}
		loadedMeta := (&Module{c: &loaded}).Metadata()
		if !reflect.DeepEqual(loadedMeta.Types, meta.Types) || !reflect.DeepEqual(loadedMeta.Functions, meta.Functions) {
			t.Fatalf("private %s reload changed type/function metadata", name)
		}
		if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
			t.Fatalf("private %s reload instantiate = %v, want required-feature rejection", name, err)
		}
		_ = loaded.Close()
		var public Compiled
		if err := public.UnmarshalBinary(item.blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
			t.Fatalf("public %s reload = %v, want unsupported GC rejection", name, err)
		}
		if _, err := Capture(item.compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC reference products") {
			t.Fatalf("%s snapshot = %v, want GC reference-product rejection", name, err)
		}
	}
}

func TestStagedGCTypeSubtypingFinalityLinkingClusterLifecycle(t *testing.T) {
	providerData := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingFinalityLinkProviderPin)
	providerCompiled, err := compileStagedGCTypeSubtypingProductForTest(providerData)
	if err != nil {
		t.Fatalf("compile provider: %v", err)
	}
	defer providerCompiled.Close()
	consumerCompiled := make([]*Compiled, len(stagedGCTypeSubtypingFinalityLinkUnlinkablePins))
	consumerBlobs := make([][]byte, len(consumerCompiled))
	for i, pin := range stagedGCTypeSubtypingFinalityLinkUnlinkablePins {
		consumerCompiled[i], err = compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(t, pin))
		if err != nil {
			t.Fatalf("compile %s: %v", pin.Filename, err)
		}
		defer consumerCompiled[i].Close()
		if consumerCompiled[i].stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingFinalityLinkConsumer || !consumerCompiled[i].NeedsFuncRefDescs {
			t.Fatalf("%s product/descriptors = %v/%v, want exact finality consumer/true", pin.Filename, consumerCompiled[i].stagedGCTypeSubtypingProduct(), consumerCompiled[i].NeedsFuncRefDescs)
		}
		consumerBlobs[i], err = marshalCompiled(consumerCompiled[i])
		if err != nil {
			t.Fatalf("marshal unlinked %s: %v", pin.Filename, err)
		}
		if got := [3]int{pin.Size, len(consumerCompiled[i].Code), len(consumerBlobs[i])}; got != [3]int{38, 0, 145} {
			t.Fatalf("%s wasm/code/codec sizes = %v, want [38 0 145]", pin.Filename, got)
		}
	}
	if providerCompiled.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingFinalityLinkProvider || !providerCompiled.NeedsFuncRefDescs {
		t.Fatalf("provider product/descriptors = %v/%v, want exact finality provider/true", providerCompiled.stagedGCTypeSubtypingProduct(), providerCompiled.NeedsFuncRefDescs)
	}
	providerBlob, err := marshalCompiled(providerCompiled)
	if err != nil {
		t.Fatalf("marshal provider: %v", err)
	}
	if got := [3]int{len(providerData), len(providerCompiled.Code), len(providerBlob)}; got != [3]int{70, 157, 324} {
		t.Fatalf("provider wasm/code/codec sizes = %v, want [70 157 324]", got)
	}

	provider, err := instantiateCore(providerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate provider: %v", err)
	}
	defer provider.Close()
	if provider.gc != nil {
		t.Fatal("finality link provider allocated a collector")
	}
	if got, want := len(provider.funcRefDescs), 3*coreruntime.FuncRefDescBytes; got != want {
		t.Fatalf("provider descriptor arena = %d bytes, want %d", got, want)
	}
	for i := 0; i < 2; i++ {
		off := (i + 1) * coreruntime.FuncRefDescBytes
		wantIdentity := uint64(uintptr(unsafe.Pointer(&provider.funcRefDescs[off])))
		if got := binary.LittleEndian.Uint64(provider.funcRefDescs[off+coreruntime.TableEntryCodePtrOffset:]); got == 0 {
			t.Fatalf("provider descriptor %d has a null code pointer", i)
		}
		if got := binary.LittleEndian.Uint64(provider.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:]); got != wantIdentity {
			t.Fatalf("provider descriptor %d identity = %#x, want self-owned %#x", i, got, wantIdentity)
		}
	}
	if got, err := provider.Invoke("f2"); err != nil || len(got) != 0 {
		t.Fatalf("provider f2 = %v, %v; want empty success", got, err)
	}
	var invokeErr error
	allocs := testing.AllocsPerRun(1000, func() {
		_, invokeErr = provider.Invoke("f2")
	})
	if invokeErr != nil || allocs != 0 {
		t.Fatalf("provider f2 steady state = %v, allocs=%v; want nil, 0", invokeErr, allocs)
	}
	exports := make(map[string]*InstanceExport, 2)
	for _, name := range []string{"f1", "f2"} {
		exports[name], err = provider.ExportedFunc(name)
		if err != nil {
			t.Fatalf("export %s: %v", name, err)
		}
	}
	resourceState := func(in *Instance) (refs int, closed bool) {
		in.lifeMu.Lock()
		refs, closed = in.resourceRefs, in.resourcesClosed
		in.lifeMu.Unlock()
		return
	}
	for i, pin := range stagedGCTypeSubtypingFinalityLinkUnlinkablePins {
		name := "f1"
		if i == 1 {
			name = "f2"
		}
		if _, linkErr := instantiateCore(consumerCompiled[i], InstantiateOptions{Imports: Imports{"M2." + name: exports[name]}}); linkErr == nil || !strings.Contains(linkErr.Error(), "signature mismatch") {
			t.Fatalf("%s link = %v, want incompatible finality signature", pin.Filename, linkErr)
		}
		if refs, closed := resourceState(provider); refs != 0 || closed {
			t.Fatalf("%s retained refs/resourcesClosed = %d/%v, want 0/false", pin.Filename, refs, closed)
		}
		host := HostFunc(func(HostModule, []uint64, []uint64) {})
		if _, hostErr := instantiateCore(consumerCompiled[i], InstantiateOptions{Imports: Imports{"M2." + name: host}}); hostErr == nil || !strings.Contains(hostErr.Error(), "exact gc/type-subtyping link provider") {
			t.Fatalf("%s host link = %v, want exact provider rejection", pin.Filename, hostErr)
		}
	}

	oldProviderCompiled, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingLinkProviderPin))
	if err != nil {
		t.Fatalf("compile old provider: %v", err)
	}
	defer oldProviderCompiled.Close()
	oldProvider, err := instantiateCore(oldProviderCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate old provider: %v", err)
	}
	defer oldProvider.Close()
	oldExport, err := oldProvider.ExportedFunc("f0")
	if err != nil {
		t.Fatalf("old provider export: %v", err)
	}
	if _, err := instantiateCore(consumerCompiled[0], InstantiateOptions{Imports: Imports{"M2.f1": oldExport}}); err == nil || !strings.Contains(err.Error(), "outside the exact gc/type-subtyping link product") {
		t.Fatalf("cross-product provider link = %v, want exact pair rejection", err)
	}

	allBlobs := append([][]byte{providerBlob}, consumerBlobs...)
	allCompiled := append([]*Compiled{providerCompiled}, consumerCompiled...)
	for i, blob := range allBlobs {
		meta := (&Module{c: allCompiled[i]}).Metadata()
		var loaded Compiled
		if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
			t.Fatalf("private reload %d: %v", i, err)
		}
		if loaded.stagedGCTypeSubtypingProduct() != 0 || loaded.stagedFeatures().IsEnabled(CoreFeatureGC) {
			t.Fatalf("private reload %d inherited admission: product=%v features=%v", i, loaded.stagedGCTypeSubtypingProduct(), loaded.stagedFeatures())
		}
		loadedMeta := (&Module{c: &loaded}).Metadata()
		if !reflect.DeepEqual(loadedMeta.Types, meta.Types) || !reflect.DeepEqual(loadedMeta.Functions, meta.Functions) {
			t.Fatalf("private reload %d changed type/function metadata", i)
		}
		if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
			t.Fatalf("private reload %d instantiate = %v, want required-feature rejection", i, err)
		}
		_ = loaded.Close()
		var public Compiled
		if err := public.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
			t.Fatalf("public reload %d = %v, want unsupported GC rejection", i, err)
		}
		if _, err := Capture(allCompiled[i], SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC reference products") {
			t.Fatalf("snapshot %d = %v, want GC reference-product rejection", i, err)
		}
	}
}
