//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func swarCallFixtures() []fixtureFunc {
	params := []byte{0x7f, 0x7b, 0x7d, 0x7e, 0x7b, 0x7c}
	results := []byte{0x7b, 0x7f, 0x7c, 0x7b, 0x7d, 0x7e}
	caller := fixtureFunc{params: params, results: append([]byte{0x7b}, results...), body: []byte{0x20, 4}}
	for i := range params {
		caller.body = append(caller.body, 0x20, byte(i))
	}
	caller.body = append(caller.body, 0x10, 1, 0x0b)
	middle := fixtureFunc{params: params, results: results}
	for i := range params {
		middle.body = append(middle.body, 0x20, byte(i))
	}
	middle.body = append(middle.body, 0x10, 2, 0x0b)
	leaf := fixtureFunc{params: params, results: results, body: []byte{0x20, 1, 0x20, 4, 0xfd, 81, 0x20, 0, 0x20, 5, 0x20, 4, 0x20, 2, 0x20, 3, 0x0b}}
	fixtures := []fixtureFunc{caller, middle, leaf}
	for i := range fixtures {
		fixtures[i].locals = []byte{1, 122, 0x7b}
	}
	return fixtures
}

func swarRecursiveCallFixture() fixtureFunc {
	return fixtureFunc{params: []byte{0x7b, 0x7f}, result: 0x7b, body: []byte{0x20, 1, 4, 0x7b, 0x20, 0, 0x20, 1, 0x41, 1, 0x6b, 0x10, 0, 5, 0x20, 0, 0x0b, 0x0b}}
}

func TestSWARDirectCallsWholeModuleAndRelocations(t *testing.T) {
	for _, fixtures := range [][]fixtureFunc{swarCallFixtures(), {swarRecursiveCallFixture()}} {
		image, err := compileSWARModule(binaryModule(fixtures...), emissionMode{production: true, interruptible: true})
		if err != nil || image == nil || len(image.functions) != len(fixtures) {
			t.Fatal("private module calls", err)
		}
		for i, info := range image.functions {
			if info.Entry&15 != 0 || info.FrameHeaderBytes != callFrameHeader || info.FrameBytes&15 != 0 || info.CodeBytes == 0 || info.Params != swarSignatureSlotsForBytes(fixtures[i].params) {
				t.Fatal("call entry/frame", i, info)
			}
		}
		if image.functions[0].DirectCalls != 1 {
			t.Fatal("missing direct relocation", image.functions[0])
		}
	}
	// A return's nonexecuted call is typechecked and budgeted but cannot create
	// a machine relocation or enlarge its physical spill peak.
	f := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0f, 0x10, 0, 0x1a, 0x0b}}
	image, err := compileSWARModule(binaryModule(f), emissionMode{production: true})
	if err != nil || image.functions[0].DirectCalls != 0 || image.functions[0].SpillSlots != 2 {
		t.Fatal("dead call emitted", image, err)
	}
}

func swarSignatureSlotsForBytes(types []byte) int {
	n := len(types)
	for _, typ := range types {
		if typ == 0x7b {
			n++
		}
	}
	return n
}

func TestSWARDirectCallDirectoryAndResourceGates(t *testing.T) {
	f := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x10, 0, 0x0b}}
	fn, ft := swarValidatedFunction(t, f)
	for _, dir := range [][]*wasm.CompType{
		nil, {nil}, {{Kind: wasm.CompStruct}}, {{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}}},
		{{Kind: wasm.CompFunc, Params: bytesToSWARTypes(bytes.Repeat([]byte{0x7b}, 129))}},
		{{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.V128}}},
		make([]*wasm.CompType, MaxFunctions+1),
	} {
		if code, _, calls, err := compileSWARPairsWithCalls(fn, ft, nil, nil, dir, emissionMode{production: true}); code != nil || calls != nil || err == nil {
			t.Fatal("bad directory emitted", len(dir), err)
		}
	}
	if code, _, _, err := compileSWARPairsWithCalls(fn, ft, nil, nil, []*wasm.CompType{ft}, emissionMode{}); code != nil || err == nil {
		t.Fatal("isolated calls admitted", err)
	}
	// Empty-signature calls isolate the exact call-count bound from argument
	// transfer weighting. Every caller uses explicit serialized arenas.
	for _, count := range []int{maxDirectCalls, maxDirectCalls + 1} {
		body := append(bytes.Repeat([]byte{0x10, 1}, count), 0x0b)
		fixtures := []fixtureFunc{{body: body}, {body: []byte{0x0b}}}
		image, err := compileSWARModule(binaryModule(fixtures...), emissionMode{production: true})
		if count == maxDirectCalls {
			if err != nil || image.functions[0].DirectCalls != count {
				t.Fatal("max calls", err)
			}
		} else if image != nil || err == nil || !strings.Contains(err.Error(), "count/transfer budget") {
			t.Fatal("call bound", err)
		}
	}
	callee := fixtureFunc{params: bytes.Repeat([]byte{0x7b}, 128), results: bytes.Repeat([]byte{0x7b}, 128)}
	for i := 0; i < 128; i++ {
		callee.body = append(callee.body, 0x20, byte(i))
	}
	callee.body = append(callee.body, 0x0b)
	for _, count := range []int{63, 64} {
		dead := fixtureFunc{result: 0x7b, body: []byte{0}}
		for i := 0; i < count; i++ {
			// Reset the virtual results without 128 extra drop instructions,
			// isolating argument/result transfer weighting at 258 per call.
			dead.body = append(dead.body, 0x10, 1, 0)
		}
		dead.body = append(dead.body, 0x0b)
		image, err := compileSWARModule(binaryModule(dead, callee), emissionMode{production: true})
		if count == 63 {
			if err != nil || image.functions[0].DirectCalls != 0 || image.functions[0].SpillSlots != 0 {
				t.Fatal("dead transfer budget boundary", image, err)
			}
		} else if image != nil || err == nil || !strings.Contains(err.Error(), "count/transfer budget") {
			t.Fatal("dead transfer budget exceeded", err)
		}
	}
}

func TestSWARDirectCallLinkedTargetsDecodeIndependently(t *testing.T) {
	fixtures := []fixtureFunc{
		{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x10, 1, 0x0b}},
		{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x10, 0, 0x0b}},
		{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x10, 2, 0x0b}},
	}
	image, err := compileSWARModule(binaryModule(fixtures...), emissionMode{production: true})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []int{1, 0, 2} {
		info := image.functions[i]
		found := 0
		for pc := info.Entry; pc+8 <= info.Entry+info.CodeBytes; pc += 4 {
			lo := binary.LittleEndian.Uint32(image.code[pc+4:])
			if lo&0xfffff != 0x67|1<<7|31<<15 {
				continue
			}
			hi := binary.LittleEndian.Uint32(image.code[pc:])
			if hi&0xfff != 0x17|31<<7 {
				t.Fatal("direct call envelope", pc)
			}
			target := int64(pc) + int64(int32(hi&0xfffff000)) + int64(int32(lo)>>20)
			if target != int64(image.functions[want].Entry) {
				t.Fatal("direct call prologue/image/target", i, pc, target, image.functions[want])
			}
			found++
		}
		if found != 1 {
			t.Fatal("linked call count", i, found)
		}
	}
}

func bytesToSWARTypes(types []byte) []wasm.ValType {
	result := make([]wasm.ValType, len(types))
	for i, typ := range types {
		switch typ {
		case 0x7b:
			result[i] = wasm.V128
		case 0x7f:
			result[i] = wasm.I32
		}
	}
	return result
}

func TestSWARPrivateModuleRejectsUnsupportedWholeSource(t *testing.T) {
	good := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}}
	base := binaryModule(good)
	for _, id := range []byte{2, 4, 8, 9, 13, 14} {
		data := modifySection(base, 7, id, []byte{0}, false)
		if image, err := compileSWARModule(data, emissionMode{production: true}); image != nil || err == nil {
			t.Fatal("unsupported empty/state section", id, err)
		}
	}
	for _, bad := range []fixtureFunc{
		{params: []byte{0x70}, result: 0x70, body: []byte{0x20, 0, 0x0b}},
		{result: 0x7b, body: []byte{0, 0xfb, 0x1d, 0x0b}},
		{result: 0x7b, body: []byte{0, 0x11, 0, 0, 0x0b}},
		{result: 0x7b, body: []byte{0, 0xfd, 0x94, 2, 0x0b}},
	} {
		bad.unexported = true
		if image, err := compileSWARModule(binaryModule(good, bad), emissionMode{production: true}); image != nil || err == nil {
			t.Fatal("unsupported unused function admitted", err)
		}
	}
	for _, data := range [][]byte{nil, {0, 97, 115, 109, 1, 0, 0, 0, 5, 127}, bytes.Repeat([]byte{0}, MaxModuleBytes+1)} {
		if image, err := compileSWARModule(data, emissionMode{production: true}); image != nil || err == nil {
			t.Fatal("malformed/oversized source", len(data), err)
		}
	}
	// Custom metadata has an explicit allowlist, even when empty.
	for _, name := range []string{"producers", "execution-control"} {
		data := section(append([]byte(nil), base...), 0, append(uleb(nil, uint32(len(name))), name...))
		image, err := compileSWARModule(data, emissionMode{production: true})
		if name == "producers" {
			if image == nil || err != nil {
				t.Fatal("inert metadata rejected", err)
			}
		} else if image != nil || err == nil {
			t.Fatal("feature metadata admitted", err)
		}
	}
}
