//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func swarGlobalFixture(class byte, index uint32) fixtureFunc {
	f := fixtureFunc{params: []byte{0x7b, class}, results: []byte{0x7b, class, class}, locals: []byte{1, 126, 0x7b}, body: []byte{0x20, 0, 0x23}}
	f.body = uleb(f.body, index)
	f.body = append(f.body, 0x20, 1, 0x24, 0, 0x23)
	f.body = uleb(f.body, index)
	f.body = append(f.body, 0x0b)
	return f
}

func swarGlobalSource(f fixtureFunc, class byte, count int) []byte {
	payload := uleb(nil, uint32(count))
	for i := 0; i < count; i++ {
		payload = append(payload, class, 1)
		switch class {
		case 0x7f:
			payload = append(payload, 0x41, 0)
		case 0x7e:
			payload = append(payload, 0x42, 0)
		case 0x7d:
			payload = append(payload, 0x43, 0, 0, 0, 0)
		case 0x7c:
			payload = append(payload, 0x44)
			payload = append(payload, make([]byte, 8)...)
		case 0x7b:
			payload = append(payload, swarLiteral(0, 0)...)
		}
		payload = append(payload, 0x0b)
	}
	return modifySection(binaryModule(f), 7, 6, payload, false)
}

func swarValidatedGlobalsFunction(t *testing.T, f fixtureFunc, class byte, count int) (*wasm.Func, *wasm.CompType, []wasm.GlobalType) {
	t.Helper()
	m, err := wasm.DecodeModule(swarGlobalSource(f, class, count))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
		t.Fatal(err)
	}
	ft, ok := m.LocalFuncType(0)
	if !ok {
		t.Fatal("global fixture function type")
	}
	globals := make([]wasm.GlobalType, len(m.Globals))
	for i := range globals {
		globals[i] = m.Globals[i].Type
	}
	return &m.Code[0], ft, globals
}

func TestSWARTypedGlobalsAllCarriersAndDirectoryBounds(t *testing.T) {
	for _, class := range []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b} {
		for _, index := range []uint32{1, 255} {
			fixture := swarGlobalFixture(class, index)
			fn, ft, globals := swarValidatedGlobalsFunction(t, fixture, class, int(index)+1)
			code, info, calls, err := compileSWARPairsWithGlobals(fn, ft, nil, nil, nil, globals, emissionMode{production: true})
			if err != nil || len(code) == 0 || calls != nil || info.FrameBytes <= 2048 {
				t.Fatal("typed global lowering", class, index, info, err)
			}
			if code, _, _, err := compileSWARPairsWithGlobals(fn, ft, nil, nil, nil, globals, emissionMode{}); code != nil || err == nil {
				t.Fatal("isolated global owner admitted", class, index, err)
			}
			if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("global without directory", class, index, err)
			}
			// Literal globals are now independently admitted by the private module.
			if image, err := compileSWARModule(swarGlobalSource(fixture, class, int(index)+1), emissionMode{production: true}); image == nil || err != nil || len(image.globals) != len(globals) {
				t.Fatal("private module literal global admission", err)
			}
		}
	}
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}, Results: []wasm.ValType{wasm.V128}}
	fn := &wasm.Func{BodyBytes: []byte{0x20, 0, 0x0b}}
	for _, globals := range [][]wasm.GlobalType{
		{{Type: wasm.FuncRef}}, {{Type: wasm.Bot}}, make([]wasm.GlobalType, 257),
	} {
		if code, _, _, err := compileSWARPairsWithGlobals(fn, ft, nil, nil, nil, globals, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("unused invalid global directory", globals, err)
		}
	}
	for _, body := range [][]byte{
		{0x23, 1, 0x0b}, {0x24, 0, 0x20, 0, 0x0b},
		{0x41, 0, 0x24, 0, 0x20, 0, 0x0b},
		{0x20, 0, 0x0f, 0x23, 1, 0x1a, 0x0b},
		{0x20, 0, 0x0f, 0x24, 0, 0x0b},
	} {
		for _, mutable := range []bool{false, true} {
			// Known wrong I32, index1, and live underflow all remain invalid;
			// only the final polymorphic write is legal with a mutable V cell.
			globals := []wasm.GlobalType{{Type: wasm.V128, Mutable: mutable}}
			code, _, _, err := compileSWARPairsWithGlobals(&wasm.Func{BodyBytes: body}, ft, nil, nil, nil, globals, emissionMode{production: true})
			valid := mutable && bytes.Equal(body, []byte{0x20, 0, 0x0f, 0x24, 0, 0x0b})
			if valid {
				if err != nil || len(code) == 0 {
					t.Fatal("polymorphic mutable write", err)
				}
			} else if code != nil || err == nil {
				t.Fatal("invalid global access emitted", mutable, body, err)
			}
		}
	}
}

func TestSWARTypedDeadGlobalOperationsHaveNoCodeOrSpills(t *testing.T) {
	for _, class := range []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b} {
		fixture := swarGlobalFixture(class, 1)
		fn, ft, globals := swarValidatedGlobalsFunction(t, fixture, class, 2)
		baseBody := append(append([]byte(nil), fn.BodyBytes[:len(fn.BodyBytes)-1]...), 0x0f, 0x0b)
		fn.BodyBytes = baseBody
		baseline, info, _, err := compileSWARPairsWithGlobals(fn, ft, nil, nil, nil, globals, emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, tail := range [][]byte{{0x23, 1, 0x1a}, {0x24, 1}} {
			fn.BodyBytes = append(append(append([]byte(nil), baseBody[:len(baseBody)-1]...), tail...), 0x0b)
			code, actual, _, err := compileSWARPairsWithGlobals(fn, ft, nil, nil, nil, globals, emissionMode{production: true})
			if err != nil || !bytes.Equal(code, baseline) || actual.FrameBytes != info.FrameBytes || actual.SpillSlots != info.SpillSlots {
				t.Fatal("dead global ghost code/frame", class, tail, actual, info, err)
			}
		}
	}
}
