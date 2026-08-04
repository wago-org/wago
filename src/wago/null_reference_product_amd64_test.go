//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

func TestStagedFirstNullReferenceProductExecution(t *testing.T) {
	data := stagedFirstNullReferenceModule(false)
	if len(data) != 149 {
		t.Fatalf("first synthetic null-reference fixture size = %d, want 149 bytes", len(data))
	}
	if _, err := Compile(NewRuntimeConfig(), data); err == nil || !strings.Contains(err.Error(), "ref null any") {
		t.Fatalf("public compile = %v, want closed any-reference gate", err)
	}
	c, err := compileStagedNullReferenceProductForTest(data)
	if err != nil {
		t.Fatalf("compile staged null-reference product: %v", err)
	}
	defer c.Close()

	wantFeatures := CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences | CoreFeatureGC | CoreFeatureExceptionHandling
	if got := compiledStructuralRequiredFeatures(c); got&wantFeatures != wantFeatures {
		t.Fatalf("required features = %v, want at least %v", got, wantFeatures)
	}
	if gc.HasHeapObjectTypes(c.GCTypeDescs) {
		t.Fatalf("null-only function type descriptors unexpectedly require a collector: %#v", c.GCTypeDescs)
	}
	meta := (&Module{c: c}).Metadata()
	wantResults := []ValType{ValAnyRef, ValFuncRef, ValExnRef, ValExternRef, ValFuncRef}
	if len(meta.Functions) != len(wantResults) || len(meta.Globals) != len(wantResults) {
		t.Fatalf("metadata functions/globals = %d/%d, want %d/%d", len(meta.Functions), len(meta.Globals), len(wantResults), len(wantResults))
	}
	for i, want := range wantResults {
		if !reflect.DeepEqual(meta.Functions[i].Results, []ValType{want}) || meta.Globals[i].Type != want || !meta.Globals[i].HasValueType {
			t.Fatalf("metadata %d = function %#v global %#v, want category %s", i, meta.Functions[i], meta.Globals[i], want)
		}
	}
	if got := meta.Functions[0].ResultTypes[0].Ref.Heap.Abstract; got != AbstractHeapAny {
		t.Fatalf("anyref exact result heap = %v, want any", got)
	}
	if got := meta.Functions[2].ResultTypes[0].Ref.Heap.Abstract; got != AbstractHeapExn {
		t.Fatalf("exnref exact result heap = %v, want exn", got)
	}
	if got := meta.Functions[4].ResultTypes[0].Ref.Heap; !got.Defined || got.TypeIndex != 0 {
		t.Fatalf("indexed exact result heap = %#v, want type 0", got)
	}

	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatalf("marshal staged null-reference product: %v", err)
	}
	t.Logf("first null-reference product: wasm=%d codec-v27=%d", len(data), len(blob))
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("private reload staged null-reference product: %v", err)
	}
	defer loaded.Close()
	if got := (&Module{c: &loaded}).Metadata(); !reflect.DeepEqual(got.Functions, meta.Functions) || !reflect.DeepEqual(got.Globals, meta.Globals) {
		t.Fatalf("codec metadata changed: functions=%#v globals=%#v", got.Functions, got.Globals)
	}
	var public Compiled
	if err := public.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
		t.Fatalf("public codec load = %v, want unsupported GC/EH/typed feature gate", err)
	}
	if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC reference products") {
		t.Fatalf("snapshot capture = %v, want explicit null-reference/GC gate", err)
	}

	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate staged null-reference product: %v", err)
	}
	defer in.Close()
	if in.gc != nil {
		t.Fatal("null-only product allocated a WasmGC collector")
	}
	for i, name := range []string{"anyref", "funcref", "exnref", "externref", "ref"} {
		got, err := in.Invoke(name)
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("Invoke(%q) = %v, %v, want one zero slot", name, got, err)
		}
		typed, err := in.Call(context.Background(), name)
		if err != nil || len(typed) != 1 || typed[0].Type() != wantResults[i] || typed[0].Bits() != 0 {
			t.Fatalf("Call(%q) = %v, %v, want null %s", name, typed, err, wantResults[i])
		}
	}
	allocs := testing.AllocsPerRun(1000, func() {
		got, err := in.Invoke("anyref")
		if err != nil || len(got) != 1 || got[0] != 0 {
			panic("null-only anyref invocation failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("null-only anyref invocation allocations = %v, want 0", allocs)
	}
}

func TestStagedFirstNullReferenceProductRejectsWidening(t *testing.T) {
	mutable, err := compileStagedNullReferenceProductForTest(stagedFirstNullReferenceModule(true))
	if err != nil {
		t.Fatalf("mutable anyref global compile = %v", err)
	}
	_ = mutable.Close()

	m, err := wasm.DecodeModule(stagedFirstNullReferenceModule(false))
	if err != nil {
		t.Fatal(err)
	}
	features := frontend.AllFeatures()
	features.TypedFunctionReferences = true
	features.NullReferenceProducts = false
	if err := frontend.RejectUnsupportedWithFeatures(m, features); err == nil || !strings.Contains(err.Error(), "ref null any") {
		t.Fatalf("frontend without null-product gate = %v, want explicit any-reference rejection", err)
	}
}

func TestStagedBottomNullReferenceGlobalsExecution(t *testing.T) {
	data := stagedBottomNullReferenceModule(false)
	if len(data) != 308 {
		t.Fatalf("bottom-global synthetic null fixture size = %d, want 308 bytes", len(data))
	}
	if _, err := Compile(NewRuntimeConfig(), data); err == nil || !strings.Contains(err.Error(), "ref null any") {
		t.Fatalf("public compile = %v, want closed abstract-reference gate", err)
	}
	c, err := compileStagedNullReferenceProductForTest(data)
	if err != nil {
		t.Fatalf("compile staged bottom-global null product: %v", err)
	}
	defer c.Close()
	if gc.HasHeapObjectTypes(c.GCTypeDescs) {
		t.Fatalf("bottom null-only descriptors unexpectedly require a collector: %#v", c.GCTypeDescs)
	}
	meta := (&Module{c: c}).Metadata()
	wantResults := []ValType{ValAnyRef, ValAnyRef, ValFuncRef, ValFuncRef, ValExnRef, ValExnRef, ValExternRef, ValExternRef, ValFuncRef}
	wantHeaps := []AbstractHeapType{AbstractHeapAny, AbstractHeapNone, AbstractHeapFunc, AbstractHeapNoFunc, AbstractHeapExn, AbstractHeapNoExn, AbstractHeapExtern, AbstractHeapNoExtern}
	if len(meta.Functions) != 9 || len(meta.Globals) != 18 {
		t.Fatalf("metadata functions/globals = %d/%d, want 9/18", len(meta.Functions), len(meta.Globals))
	}
	for i, want := range wantResults {
		if !reflect.DeepEqual(meta.Functions[i].Results, []ValType{want}) {
			t.Fatalf("function %d result category = %v, want %s", i, meta.Functions[i].Results, want)
		}
		if i < len(wantHeaps) && meta.Functions[i].ResultTypes[0].Ref.Heap.Abstract != wantHeaps[i] {
			t.Fatalf("function %d exact heap = %v, want %v", i, meta.Functions[i].ResultTypes[0].Ref.Heap.Abstract, wantHeaps[i])
		}
	}
	if heap := meta.Functions[8].ResultTypes[0].Ref.Heap; !heap.Defined || heap.TypeIndex != 0 {
		t.Fatalf("indexed widened result heap = %#v, want type 0", heap)
	}
	for i := range meta.Globals {
		if meta.Globals[i].Mutable || !meta.Globals[i].HasValueType {
			t.Fatalf("global %d metadata = %#v, want immutable exact type", i, meta.Globals[i])
		}
	}

	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatalf("marshal bottom-global null product: %v", err)
	}
	t.Logf("bottom-global null-reference product: wasm=%d codec-v27=%d", len(data), len(blob))
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("private reload bottom-global null product: %v", err)
	}
	defer loaded.Close()
	loadedMeta := (&Module{c: &loaded}).Metadata()
	if !reflect.DeepEqual(loadedMeta.Functions, meta.Functions) || !reflect.DeepEqual(loadedMeta.Globals, meta.Globals) {
		t.Fatalf("bottom-global codec metadata changed: functions=%#v globals=%#v", loadedMeta.Functions, loadedMeta.Globals)
	}
	var public Compiled
	if err := public.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
		t.Fatalf("public bottom-global codec load = %v, want unsupported feature gate", err)
	}

	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate bottom-global null product: %v", err)
	}
	defer in.Close()
	if in.gc != nil {
		t.Fatal("bottom-global null product allocated a WasmGC collector")
	}
	for i, name := range namesForBottomNullReferenceProduct() {
		got, err := in.Invoke(name)
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("Invoke(%q) = %v, %v, want one zero slot", name, got, err)
		}
		typed, err := in.Call(context.Background(), name)
		if err != nil || len(typed) != 1 || typed[0].Type() != wantResults[i] || typed[0].Bits() != 0 {
			t.Fatalf("Call(%q) = %v, %v, want null %s", name, typed, err, wantResults[i])
		}
	}
	allocs := testing.AllocsPerRun(1000, func() {
		got, err := in.Invoke("nullexnref")
		if err != nil || len(got) != 1 || got[0] != 0 {
			panic("bottom nullexnref invocation failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("bottom nullexnref invocation allocations = %v, want 0", allocs)
	}
}

func TestStagedBottomNullReferenceGlobalsRejectWidening(t *testing.T) {
	mutable, err := compileStagedNullReferenceProductForTest(stagedBottomNullReferenceModule(true))
	if err != nil {
		t.Fatalf("mutable bottom global compile = %v", err)
	}
	_ = mutable.Close()
}

func BenchmarkStagedBottomNullReferenceGlobalGet(b *testing.B) {
	c, err := compileStagedNullReferenceProductForTest(stagedBottomNullReferenceModule(false))
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
		got, err := in.Invoke("nullexnref")
		if err != nil || len(got) != 1 || got[0] != 0 {
			b.Fatalf("Invoke = %v, %v", got, err)
		}
	}
}

func replayStagedNullReferenceScript(t *testing.T, base, tmp string, script stagedSpecScript) (counts stagedSpecCounts, gates map[string]int) {
	t.Helper()
	gates = map[string]int{}
	definitions := map[string][]byte{}
	var latestDefinition []byte
	var current stagedSpecModule
	named := map[string]stagedSpecModule{}
	var live []stagedSpecModule
	defer func() {
		for i := len(live) - 1; i >= 0; i-- {
			_ = live[i].in.Close()
			_ = live[i].c.Close()
		}
	}()
	instantiate := func(data []byte, cmd stagedSpecCommand) (stagedSpecModule, error) {
		decoded, err := wasm.DecodeModule(data)
		if err != nil {
			return stagedSpecModule{}, fmt.Errorf("decode: %w", err)
		}
		kind, err := stagedNullReferenceProductShape(decoded)
		if err != nil {
			return stagedSpecModule{}, fmt.Errorf("shape: %w", err)
		}
		wantBytes := 189
		if kind == stagedNullReferenceProductBottomGlobals {
			wantBytes = 364
		}
		if len(data) != wantBytes {
			return stagedSpecModule{}, fmt.Errorf("pinned module size %d, want %d", len(data), wantBytes)
		}
		c, err := compileStagedNullReferenceProductForTest(data)
		if err != nil {
			return stagedSpecModule{}, fmt.Errorf("compile: %w", err)
		}
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			_ = c.Close()
			return stagedSpecModule{}, fmt.Errorf("instantiate: %w", err)
		}
		if in.gc != nil {
			_ = in.Close()
			_ = c.Close()
			return stagedSpecModule{}, fmt.Errorf("null-only product allocated a collector")
		}
		m := stagedSpecModule{in: in, c: c}
		live = append(live, m)
		if cmd.Name != "" {
			named[cmd.Name] = m
		}
		return m, nil
	}
	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module_definition":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read module definition: %v", base, cmd.Line, err)
				continue
			}
			latestDefinition = data
			if cmd.Name != "" {
				definitions[cmd.Name] = data
			}
		case "module_instance", "module":
			var data []byte
			var err error
			if cmd.Type == "module" {
				data, err = os.ReadFile(filepath.Join(tmp, cmd.Filename))
			} else if cmd.Module != "" {
				data = definitions[cmd.Module]
			} else {
				data = latestDefinition
			}
			if err != nil || data == nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read module: %v", base, cmd.Line, err)
				continue
			}
			current, err = instantiate(data, cmd)
			if err != nil {
				if strings.Contains(err.Error(), "compile:") {
					counts.UnexpectedCompileRejects++
				} else {
					counts.UnexpectedLinkRejects++
				}
				counts.Failures++
				t.Errorf("%s.wast:%d module rejected: %v", base, cmd.Line, err)
				continue
			}
			counts.ModulesPassed++
		case "assert_return":
			m := current
			if cmd.Action.Module != "" {
				m = named[cmd.Action.Module]
			}
			if m.in == nil || cmd.Action.Type != "invoke" || len(cmd.Action.Args) != 0 {
				counts.Failures++
				t.Errorf("%s.wast:%d unavailable or unsupported null action", base, cmd.Line)
				continue
			}
			got, err := m.in.Invoke(cmd.Action.Field)
			if err != nil || len(got) != len(cmd.Expected) {
				counts.Failures++
				t.Errorf("%s.wast:%d result=%v err=%v want=%v", base, cmd.Line, got, err, cmd.Expected)
				continue
			}
			matched := true
			for i := range got {
				if !stagedTypedReferenceMatch(m, got[i], cmd.Expected[i]) {
					matched = false
					break
				}
			}
			if !matched {
				counts.Failures++
				t.Errorf("%s.wast:%d result=%v want=%v", base, cmd.Line, got, cmd.Expected)
				continue
			}
			counts.AssertionsPassed++
		default:
			counts.Failures++
			t.Errorf("%s.wast:%d unhandled command %q", base, cmd.Line, cmd.Type)
		}
	}
	if current.in != nil {
		allocs := testing.AllocsPerRun(1000, func() {
			got, err := current.in.Invoke("nullexnref")
			if err != nil || len(got) != 1 || got[0] != 0 {
				panic("official bottom null-reference replay failed")
			}
		})
		if allocs != 0 {
			counts.Failures++
			t.Errorf("%s official null-reference allocations = %v, want 0", base, allocs)
		}
	}
	return counts, gates
}
