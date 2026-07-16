//go:build !tinygo

package wago

import (
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func stagedFirstNullReferenceModule(mutableAnyGlobal bool) []byte {
	anyref := wasm.RefVal(wasm.AbsRef(wasm.HeapAny))
	exnref := wasm.RefVal(wasm.AbsRef(wasm.HeapExn))
	indexed := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	results := []wasm.ValType{anyref, wasm.FuncRef, exnref, wasm.ExternRef, indexed}
	encodings := [][]byte{{byte(wasm.HeapAny)}, {byte(wasm.HeapFunc)}, {byte(wasm.HeapExn)}, {byte(wasm.HeapExtern)}, {0x63, 0x00}}
	heaps := []byte{byte(wasm.HeapAny), byte(wasm.HeapFunc), byte(wasm.HeapExn), byte(wasm.HeapExtern), 0}
	names := []string{"anyref", "funcref", "exnref", "externref", "ref"}

	types := [][]byte{wasmtest.FuncType(nil, nil)}
	funcs := make([][]byte, len(results))
	globals := make([][]byte, len(results))
	exports := make([][]byte, len(results))
	codes := make([][]byte, len(results))
	for i := range results {
		funcType := []byte{0x60, 0x00, 0x01}
		funcType = append(funcType, encodings[i]...)
		types = append(types, funcType)
		funcs[i] = wasmtest.ULEB(uint32(i + 1))
		mutable := byte(0)
		if i == 0 && mutableAnyGlobal {
			mutable = 1
		}
		global := append([]byte(nil), encodings[i]...)
		global = append(global, mutable, 0xd0, heaps[i], 0x0b)
		globals[i] = global
		exports[i] = wasmtest.ExportEntry(names[i], 0, uint32(i))
		codes[i] = wasmtest.Code([]byte{0xd0, heaps[i], 0x0b})
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(6, wasmtest.Vec(globals...)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
}

func stagedBottomNullReferenceModule(mutableBottomGlobal bool) []byte {
	encAny := []byte{byte(wasm.HeapAny)}
	encNone := []byte{byte(wasm.HeapNone)}
	encFunc := []byte{byte(wasm.HeapFunc)}
	encNoFunc := []byte{byte(wasm.HeapNoFunc)}
	encExn := []byte{byte(wasm.HeapExn)}
	encNoExn := []byte{byte(wasm.HeapNoExn)}
	encExtern := []byte{byte(wasm.HeapExtern)}
	encNoExtern := []byte{byte(wasm.HeapNoExtern)}
	encIndexed := []byte{0x63, 0x00}
	resultEncodings := [][]byte{encAny, encNone, encFunc, encNoFunc, encExn, encNoExn, encExtern, encNoExtern, encIndexed}
	names := namesForBottomNullReferenceProduct()
	globalGets := []byte{0, 0, 1, 1, 2, 2, 3, 3, 1}

	types := [][]byte{wasmtest.FuncType(nil, nil)}
	funcs := make([][]byte, len(resultEncodings))
	exports := make([][]byte, len(resultEncodings))
	codes := make([][]byte, len(resultEncodings))
	for i, enc := range resultEncodings {
		ft := []byte{0x60, 0x00, 0x01}
		ft = append(ft, enc...)
		types = append(types, ft)
		funcs[i] = wasmtest.ULEB(uint32(i + 1))
		exports[i] = wasmtest.ExportEntry(names[i], 0, uint32(i))
		codes[i] = wasmtest.Code([]byte{0x23, globalGets[i], 0x0b})
	}

	globalTypeEncodings := [][]byte{
		encNone, encNoFunc, encNoExn, encNoExtern,
		encAny, encAny, encFunc, encFunc, encExn, encExn, encExtern, encExtern,
		encNone, encNoFunc, encNoExn, encNoExtern, encIndexed, encIndexed,
	}
	globalHeaps := []byte{
		byte(wasm.HeapNone), byte(wasm.HeapNoFunc), byte(wasm.HeapNoExn), byte(wasm.HeapNoExtern),
		byte(wasm.HeapAny), byte(wasm.HeapNone), byte(wasm.HeapFunc), byte(wasm.HeapNoFunc),
		byte(wasm.HeapExn), byte(wasm.HeapNoExn), byte(wasm.HeapExtern), byte(wasm.HeapNoExtern),
		byte(wasm.HeapNone), byte(wasm.HeapNoFunc), byte(wasm.HeapNoExn), byte(wasm.HeapNoExtern), 0, byte(wasm.HeapNoFunc),
	}
	globals := make([][]byte, len(globalTypeEncodings))
	for i, enc := range globalTypeEncodings {
		g := append([]byte(nil), enc...)
		mutable := byte(0)
		if i == 0 && mutableBottomGlobal {
			mutable = 1
		}
		g = append(g, mutable, 0xd0, globalHeaps[i], 0x0b)
		globals[i] = g
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(6, wasmtest.Vec(globals...)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
}

func namesForBottomNullReferenceProduct() []string {
	return []string{"anyref", "nullref", "funcref", "nullfuncref", "exnref", "nullexnref", "externref", "nullexternref", "ref"}
}

func compileStagedNullReferenceProductForTest(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.NullReferenceProducts = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func TestStagedNullReferenceProductPlatformAndBoundsGate(t *testing.T) {
	cfg := NewRuntimeConfig()
	if guardPageBuilt {
		cfg = cfg.WithBoundsChecks(BoundsChecksSignalsBased)
	} else {
		cfg = cfg.WithBoundsChecks(BoundsChecksExplicit)
	}
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.NullReferenceProducts = true
	c, err := compileWithFrontendFeatures(cfg, stagedFirstNullReferenceModule(false), features)
	if goruntime.GOOS != "linux" || goruntime.GOARCH != "amd64" {
		if err == nil || !strings.Contains(err.Error(), "unsupported null-reference product staged execution on") {
			t.Fatalf("platform compile = %v, want explicit platform rejection", err)
		}
		return
	}
	if guardPageBuilt {
		if err == nil || !strings.Contains(err.Error(), "signals-based bounds checks") {
			t.Fatalf("guard compile = %v, want explicit bounds-mode rejection", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("linux/amd64 explicit compile: %v", err)
	}
	_ = c.Close()
}
