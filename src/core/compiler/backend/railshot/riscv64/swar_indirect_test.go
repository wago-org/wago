//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func swarIndirectFixtures() (fixtureFunc, fixtureFunc) {
	old := swarCallFixtures()
	caller, leaf := old[0], old[2]
	caller.params = append(bytes.Clone(caller.params), 0x7f)
	caller.locals = []byte{1, 121, 0x7b}
	caller.body = []byte{0x20, 4}
	for i := range leaf.params {
		caller.body = append(caller.body, 0x20, byte(i))
	}
	caller.body = append(caller.body, 0x20, 6, 0x11, 1, 0, 0x0b)
	return caller, leaf
}

func swarIndirectSource(caller, leaf fixtureFunc) []byte {
	data := binaryModule(caller, leaf)
	table := uleb([]byte{1, 0x70, 1}, 1024)
	table = uleb(table, 1024)
	data = modifySection(data, 7, 4, table, false)
	return modifySection(data, 10, 9, []byte{1, 3, 0, 2, 0, 1}, false)
}

func swarValidatedIndirectModule(t *testing.T, caller, leaf fixtureFunc) (*wasm.Module, []*wasm.CompType, []uint64) {
	t.Helper()
	return swarValidatedIndirectSource(t, swarIndirectSource(caller, leaf))
}

func swarValidatedIndirectSource(t *testing.T, source []byte) (*wasm.Module, []*wasm.CompType, []uint64) {
	t.Helper()
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 128}); err != nil {
		t.Fatal(err)
	}
	types, keys := make([]*wasm.CompType, len(m.Code)), make([]uint64, len(m.Types))
	for i := range types {
		types[i], _ = m.LocalFuncType(i)
		var ok bool
		keys[i], ok = m.StructuralTypeKeyChecked(uint32(i))
		if !ok {
			t.Fatal("checked indirect signature identity", i)
		}
	}
	return m, types, keys
}

func TestSWARIndirectTypedCallsAndExplicitIdentityAdmission(t *testing.T) {
	caller, leaf := swarIndirectFixtures()
	m, types, keys := swarValidatedIndirectModule(t, caller, leaf)
	fn, ft, table := &m.Code[0], types[0], &m.Tables[0].Type
	code, info, calls, err := compileSWARPairsWithIndirectKeys(fn, ft, nil, types, types, nil, 0, table, keys, emissionMode{production: true})
	if err != nil || len(code) == 0 || info.FrameBytes <= 2048 || info.FrameHeaderBytes != callFrameHeader || calls != nil || info.DirectCalls != 0 {
		t.Fatal("mixed typed indirect code/frame", info, err)
	}
	if code, _, _, err := compileSWARPairsWithTable(fn, ft, nil, types, types, nil, 0, table, emissionMode{production: true}); code != nil || err == nil {
		t.Fatal("indirect identity guessed", err)
	}
	for _, bad := range [][]uint64{{}, keys[:1], append(append([]uint64(nil), keys...), 3)} {
		if code, _, _, err := compileSWARPairsWithIndirectKeys(fn, ft, nil, types, types, nil, 0, table, bad, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("indirect identity shape", bad, err)
		}
	}
	for _, mode := range []emissionMode{{}, {production: true}} {
		if code, _, _, err := compileSWARPairsWithIndirectKeys(fn, ft, nil, types, nil, nil, 0, table, keys, mode); code != nil || err == nil {
			t.Fatal("indirect without declared framed functions", err)
		}
	}
	if code, _, _, err := compileSWARPairsWithIndirectKeys(fn, ft, nil, types, types, nil, 0, nil, keys, emissionMode{production: true}); code != nil || err == nil {
		t.Fatal("indirect without table owner", err)
	}
	if image, err := compileSWARModule(swarIndirectSource(caller, leaf), emissionMode{production: true}); image != nil || err == nil {
		t.Fatal("module table/indirect gate widened", err)
	}
}

func TestSWARIndirectDeadTypingAndSourceBudget(t *testing.T) {
	caller := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0f, 0x0b}}
	leaf := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}}
	m, types, keys := swarValidatedIndirectModule(t, caller, leaf)
	fn, ft, table := &m.Code[0], types[0], &m.Tables[0].Type
	mode := emissionMode{production: true}
	baseline, info, _, err := compileSWARPairsWithIndirectKeys(fn, ft, nil, types, types, nil, 0, table, keys, mode)
	if err != nil {
		t.Fatal(err)
	}
	fn.BodyBytes = []byte{0x20, 0, 0x0f, 0x11, 1, 0, 0x1a, 0x0b}
	code, actual, calls, err := compileSWARPairsWithIndirectKeys(fn, ft, nil, types, types, nil, 0, table, keys, mode)
	if err != nil || calls != nil || !bytes.Equal(code, baseline) || actual.FrameBytes != info.FrameBytes || actual.SpillSlots != info.SpillSlots {
		t.Fatal("dead indirect ghost code/frame", err)
	}
	for _, tail := range [][]byte{{0x11, 2, 0}, {0x11, 1, 1}, {0x20, 0, 0x11, 1, 0}, {2, 0x40, 0x11, 1, 0, 0x0b}, {0x11, 1, 0x80}} {
		fn.BodyBytes = append(append([]byte{0x20, 0, 0x0f}, tail...), 0x0b)
		if code, _, _, err := compileSWARPairsWithIndirectKeys(fn, ft, nil, types, types, nil, 0, table, keys, mode); code != nil || err == nil {
			t.Fatal("invalid dead indirect index/class/floor", tail, err)
		}
	}
	for _, count := range []int{maxDirectCalls, maxDirectCalls + 1} {
		caller := fixtureFunc{body: append(bytes.Repeat([]byte{0x41, 0, 0x11, 1, 0}, count), 0x0b)}
		leaf := fixtureFunc{body: []byte{0x0b}}
		m, types, keys := swarValidatedIndirectModule(t, caller, leaf)
		code, _, calls, err := compileSWARPairsWithIndirectKeys(&m.Code[0], types[0], nil, types, types, nil, 0, &m.Tables[0].Type, keys, mode)
		if count == maxDirectCalls {
			if err != nil || len(code) == 0 || calls != nil {
				t.Fatal("maximum source indirect calls", err)
			}
		} else if code != nil || err == nil {
			t.Fatal("excess source indirect calls", err)
		}
	}
}
