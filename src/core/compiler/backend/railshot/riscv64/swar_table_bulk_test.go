//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func swarBulkTableFixture(sub byte) fixtureFunc {
	f := fixtureFunc{params: []byte{0x7b}, results: []byte{0x7b, 0x7f}, body: []byte{0x20, 0}}
	switch sub {
	case 14:
		f.params = append(f.params, 0x7f, 0x7f, 0x7f)
		f.body = append(f.body, 0x20, 1, 0x20, 2, 0x20, 3, 0xfc, 14, 0, 0)
	case 15:
		f.params = append(f.params, 0x7f, 0x7f)
		f.results = append(f.results, 0x7f)
		f.body = append(f.body, 0xd2, 1, 0xd0, 0x70, 0x20, 2, 0x1c, 1, 0x70, 0x20, 1, 0xfc, 15, 0)
	case 17:
		f.params = append(f.params, 0x7f, 0x7f, 0x7f)
		f.body = append(f.body, 0x20, 1, 0xd2, 1, 0xd0, 0x70, 0x20, 3, 0x1c, 1, 0x70, 0x20, 2, 0xfc, 17, 0)
	}
	f.locals = []byte{1, byte(128 - len(f.params)), 0x7b}
	f.body = append(f.body, 0xfc, 16, 0, 0x0b)
	return f
}

func swarBulkTableSource(f fixtureFunc) []byte {
	return modifySection(swarTableSource(f), 4, 4, uleb(uleb([]byte{1, 0x70, 1}, 3), 1024), true)
}

func swarValidatedBulkTableFunction(t *testing.T, f fixtureFunc) (*wasm.Func, *wasm.CompType, []*wasm.CompType, *wasm.TableType) {
	t.Helper()
	m, err := wasm.DecodeModule(swarBulkTableSource(f))
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

func TestSWAROwnedBulkTableAdmissionAndLayouts(t *testing.T) {
	for _, sub := range []byte{14, 15, 16, 17} {
		f := swarBulkTableFixture(sub)
		fn, ft, functions, table := swarValidatedBulkTableFunction(t, f)
		mode := emissionMode{production: true, interruptible: true}
		code, info, calls, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, table, mode)
		if err != nil || len(code) == 0 || info.FrameBytes <= 2048 || info.FrameHeaderBytes != callFrameHeader || calls != nil {
			t.Fatal("bulk table escaped mixed frame", sub, info, err)
		}
		if code, _, _, err := compileSWARPairsWithCalls(fn, ft, nil, nil, functions, mode); code != nil || err == nil {
			t.Fatal("bulk table without provider", sub, err)
		}
		if sub != 16 {
			if code, _, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, table, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("table mutation without interrupt bridge", sub, err)
			}
		}
		if image, err := compileSWARModule(swarBulkTableSource(f), mode); image != nil || err == nil {
			t.Fatal("bulk module table gate widened", sub, err)
		}
	}
}

func TestSWAROwnedBulkTableDeadClassesIndicesAndReserve(t *testing.T) {
	for _, sub := range []byte{14, 15, 16, 17} {
		f := swarBulkTableFixture(sub)
		// A live return followed by ghost instructions exercises exact dead floors.
		f.results = []byte{0x7b}
		f.body = []byte{0x20, 0, 0x0f, 0x0b}
		fn, ft, functions, table := swarValidatedBulkTableFunction(t, f)
		mode := emissionMode{production: true, interruptible: true}
		baseline, info, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, table, mode)
		if err != nil {
			t.Fatal(err)
		}
		ghost := []byte{0xfc, sub, 0}
		if sub == 14 {
			ghost = append(ghost, 0)
		}
		if sub == 15 || sub == 16 {
			ghost = append(ghost, 0x1a)
		}
		fn.BodyBytes = append(append([]byte{0x20, 0, 0x0f}, ghost...), 0x0b)
		code, actual, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, table, mode)
		if err != nil || !bytes.Equal(code, baseline) || actual.FrameBytes != info.FrameBytes || actual.SpillSlots != info.SpillSlots {
			t.Fatal("dead bulk table emitted", sub, err)
		}
		if sub != 16 {
			if code, _, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, table, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("dead bulk loop bridge", sub, err)
			}
		}
		badIndex := bytes.Clone(ghost)
		badIndex[2] = 1
		tails := [][]byte{badIndex, {0xfc, sub, 0x80}}
		if sub == 14 {
			badSecondIndex := bytes.Clone(ghost)
			badSecondIndex[3] = 1
			tails = append(tails, badSecondIndex)
		}
		if sub != 16 {
			tails = append(tails, append([]byte{0x20, 0}, ghost...), append([]byte{2, 0x40}, append(ghost, 0x0b)...))
		}
		for _, tail := range tails {
			fn.BodyBytes = append(append([]byte{0x20, 0, 0x0f}, tail...), 0x0b)
			if code, _, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, table, mode); code != nil || err == nil {
				t.Fatal("invalid ghost table index/class/floor", sub, tail, err)
			}
		}
	}
	f := swarBulkTableFixture(15)
	fn, ft, functions, table := swarValidatedBulkTableFunction(t, f)
	for _, min := range []uint64{512, 513} {
		copyType := *table
		copyType.Limits = wasm.Limits{Min: min}
		for _, dead := range []bool{false, true} {
			if dead {
				fn.BodyBytes = []byte{0x20, 0, 0x41, 0, 0x41, 0, 0x0f, 0xfc, 15, 0, 0x1a, 0x0b}
			} else {
				fn.BodyBytes = f.body
			}
			code, _, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, &copyType, emissionMode{production: true, interruptible: true})
			if min == 512 {
				if code == nil || err != nil {
					t.Fatal("bounded table reserve", dead, err)
				}
			} else if code != nil || err == nil {
				t.Fatal("table reserve past bound", dead, err)
			}
		}
	}
}
