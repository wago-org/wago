//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func swarElementFixture(drop bool, index uint32) fixtureFunc {
	f := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0}}
	if drop {
		f.body = uleb(append(f.body, 0xfc, 13), index)
	} else {
		f.params = append(f.params, 0x7f, 0x7f, 0x7f)
		f.result = 0
		f.results = []byte{0x7b, 0x7f}
		f.body = uleb(append(f.body, 0x20, 1, 0x20, 2, 0x20, 3, 0xfc, 12), index)
		f.body = append(f.body, 0, 0xfc, 16, 0)
	}
	f.locals = []byte{1, byte(128 - len(f.params)), 0x7b}
	f.body = append(f.body, 0x0b)
	return f
}

func swarElementSource(f fixtureFunc) []byte {
	segments := uleb(nil, 256)
	for i := 0; i < 256; i++ {
		mode := byte(1)
		if i == 17 {
			mode = 3
		} // Original declarative index starts dropped.
		segments = append(segments, mode, 0)
		if i == 254 {
			segments = append(segments, 0)
		} else {
			segments = append(segments, 4, 1, 0, 1, 1)
		}
	}
	return modifySection(swarBulkTableSource(f), 9, 9, segments, true)
}

func swarValidatedElementFunction(t *testing.T, f fixtureFunc) (*wasm.Func, *wasm.CompType, []*wasm.CompType, *wasm.TableType) {
	t.Helper()
	m, err := wasm.DecodeModule(swarElementSource(f))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 128}); err != nil {
		t.Fatal(err)
	}
	functions := make([]*wasm.CompType, len(m.Code))
	for i := range functions {
		functions[i], _ = m.LocalFuncType(i)
	}
	return &m.Code[0], functions[0], functions, &m.Tables[0].Type
}

func TestSWAROwnedElementOriginalIndicesAndAdmission(t *testing.T) {
	for _, drop := range []bool{false, true} {
		for _, index := range []uint32{0, 255} {
			f := swarElementFixture(drop, index)
			fn, ft, functions, table := swarValidatedElementFunction(t, f)
			mode := emissionMode{production: true, interruptible: true}
			code, info, calls, err := compileSWARPairsWithElements(fn, ft, nil, nil, functions, nil, 0, table, nil, 256, mode)
			if err != nil || len(code) == 0 || info.FrameBytes <= 2048 || info.FrameHeaderBytes != callFrameHeader || calls != nil {
				t.Fatal("original element mixed frame", drop, index, info, err)
			}
			if code, _, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, table, mode); code != nil || err == nil {
				t.Fatal("element shape guessed", drop, index, err)
			}
			for _, count := range []int{-1, 0, 257} {
				if code, _, _, err := compileSWARPairsWithElements(fn, ft, nil, nil, functions, nil, 0, table, nil, count, mode); code != nil || err == nil {
					t.Fatal("element directory bound", drop, count, err)
				}
			}
			if code, _, _, err := compileSWARPairsWithElements(fn, ft, nil, nil, functions, nil, 0, nil, nil, 256, mode); code != nil || err == nil {
				t.Fatal("element without owner table", drop, err)
			}
			code, _, _, err = compileSWARPairsWithElements(fn, ft, nil, nil, functions, nil, 0, table, nil, 256, emissionMode{production: true})
			if drop {
				if code == nil || err != nil {
					t.Fatal("drop needlessly requires loop bridge", err)
				}
			} else if code != nil || err == nil {
				t.Fatal("init without interruption bridge", err)
			}
			if image, err := compileSWARModule(swarElementSource(f), mode); image != nil || err == nil {
				t.Fatal("module element gate widened", err)
			}
		}
	}
}

func TestSWAROwnedElementDeadFloorsIndicesAndClasses(t *testing.T) {
	f := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0f, 0x0b}}
	fn, ft, functions, table := swarValidatedElementFunction(t, f)
	mode := emissionMode{production: true, interruptible: true}
	baseline, info, _, err := compileSWARPairsWithElements(fn, ft, nil, nil, functions, nil, 0, table, nil, 256, mode)
	if err != nil {
		t.Fatal(err)
	}
	for _, tail := range [][]byte{{0xfc, 12, 0xff, 1, 0}, {0xfc, 13, 0xff, 1}} {
		fn.BodyBytes = append(append([]byte{0x20, 0, 0x0f}, tail...), 0x0b)
		code, actual, _, err := compileSWARPairsWithElements(fn, ft, nil, nil, functions, nil, 0, table, nil, 256, mode)
		if err != nil || !bytes.Equal(code, baseline) || actual.FrameBytes != info.FrameBytes || actual.SpillSlots != info.SpillSlots {
			t.Fatal("dead element ghost code/frame", tail, err)
		}
	}
	for _, tail := range [][]byte{{0xfc, 12, 0x80, 2, 0}, {0xfc, 13, 0x80, 2}, {0xfc, 12, 0xff, 1, 1}, {0xfc, 12, 0xff, 1, 0x80}, {0xfc, 13, 0x80}, {0x20, 0, 0xfc, 12, 0, 0}, {2, 0x40, 0xfc, 12, 0, 0, 0x0b}} {
		fn.BodyBytes = append(append([]byte{0x20, 0, 0x0f}, tail...), 0x0b)
		if code, _, _, err := compileSWARPairsWithElements(fn, ft, nil, nil, functions, nil, 0, table, nil, 256, mode); code != nil || err == nil {
			t.Fatal("dead element class/floor/immediate", tail, err)
		}
	}
	// Even an unused directory is bounded before emission.
	fn.BodyBytes = f.body
	for _, count := range []int{-1, 257} {
		if code, _, _, err := compileSWARPairsWithElements(fn, ft, nil, nil, functions, nil, 0, table, nil, count, mode); code != nil || err == nil {
			t.Fatal("unused element directory", count, err)
		}
	}
}
