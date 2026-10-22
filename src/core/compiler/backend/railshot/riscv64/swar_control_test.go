//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func swarControlFixtures() []fixtureFunc {
	fixtures := []fixtureFunc{
		{params: []byte{0x7b}, result: 0x7b, body: []byte{2, 0x7b, 0x20, 0, 0x0b, 0x0b}},
		{params: []byte{0x7b, 0x7b, 0x7f}, result: 0x7b, body: []byte{0x20, 2, 4, 0x7b, 0x20, 0, 5, 0x20, 1, 0x0b, 0x0b}},
		{params: []byte{0x7b, 0x7b, 0x7f}, result: 0x7b, body: []byte{0x20, 2, 4, 0x40, 0x20, 1, 0x21, 0, 0x0b, 0x20, 0, 0x0b}},
		{params: []byte{0x7b, 0x7b}, result: 0x7b, body: []byte{2, 0x7b, 2, 0x7b, 0x20, 1, 0x20, 0, 0x0c, 1, 0x0b, 0x0b, 0x0b}},
		{params: []byte{0x7b, 0x7b, 0x7f}, result: 0x7b, body: []byte{2, 0x7b, 0x20, 0, 0x20, 2, 0x0d, 0, 0x1a, 0x20, 1, 0x0b, 0x0b}},
	}
	branchTable := fixtureFunc{params: []byte{0x7b, 0x7b, 0x7f}, result: 0x7b, body: []byte{2, 0x7b, 2, 0x7b, 0x20, 1, 0x20, 0, 0x20, 2, 0x0e, 17}}
	for index := 0; index < 17; index++ {
		branchTable.body = append(branchTable.body, byte(index%2))
	}
	branchTable.body = append(branchTable.body, 1, 0x0b, 0xfd, 77, 0x0b, 0x0b)
	fixtures = append(fixtures, branchTable,
		fixtureFunc{params: []byte{0x7b, 0x7f, 0x7c, 0x7b}, results: []byte{0x7b, 0x7f, 0x7c}, body: []byte{0x20, 3, 0x20, 0, 0x20, 1, 0x20, 2, 0x0f, 0x0b}},
		fixtureFunc{result: 0x7b, body: []byte{0, 0x0b}})
	for i := range fixtures {
		fixtures[i].locals = append(uleb([]byte{1}, uint32(128-len(fixtures[i].params))), 0x7b)
	}
	return fixtures
}

func TestSWARControlLabelAndAggregateTransferBounds(t *testing.T) {
	fixture := func(results, labels int) fixtureFunc {
		body := bytes.Repeat([]byte{0x20, 0}, results)
		body = append(body, 0x41, 0, 0x0e)
		body = uleb(body, uint32(labels))
		body = append(body, make([]byte, labels+1)...)
		body = append(body, 0x0b)
		return fixtureFunc{params: []byte{0x7b}, results: bytes.Repeat([]byte{0x7b}, results), body: body}
	}
	for _, test := range []struct {
		results, labels int
		want            string
	}{
		{1, 1024, ""}, {1, 1025, "branch table bound"},
		{128, 125, ""}, {128, 126, "branch transfer budget"},
	} {
		fn, ft := swarValidatedFunction(t, fixture(test.results, test.labels))
		code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if test.want == "" {
			if len(code) == 0 || err != nil {
				t.Fatal("control budget boundary", test, err)
			}
		} else if code != nil || err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatal("control budget failed to reject before image", test, err)
		}
	}
	fn := &wasm.Func{BodyBytes: []byte{2, 0x7b, 0x20, 0, 0x41, 0, 0x0e, 1, 0, 1, 0x0b, 0x1a, 0x41, 0, 0x0b}}
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}, Results: []wasm.ValType{wasm.I32}}
	if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil || !strings.Contains(err.Error(), "branch carrier class") {
		t.Fatal("mixed same-table label classes admitted", err)
	}
}

func TestSWARInlineControlAdmissionAndClosedShapes(t *testing.T) {
	for index, fixture := range swarControlFixtures() {
		fn, ft := swarValidatedFunction(t, fixture)
		for _, production := range []bool{false, true} {
			code, info, err := compileSWARPairs(fn, ft, emissionMode{production: production})
			if index == 7 && !production {
				if code != nil || err == nil {
					t.Fatal("isolated unreachable trap admitted", err)
				}
				continue
			}
			if len(code) == 0 || err != nil || info.FrameBytes <= 2048 {
				t.Fatal("inline control lowering", index, production, info, err)
			}
		}
	}
	for _, body := range [][]byte{
		{0x20, 0, 2, 0x40, 0xfd, 77, 0x0b, 0x0b},            // cannot consume beneath control floor
		{0x20, 0, 4, 0x7b, 0x20, 0, 5, 0x20, 0, 0x0b, 0x0b}, // vector condition
		{2, 0x7b, 0x41, 0, 0x0b, 0x0b},                      // wrong branch result class
		{2, 0x7b, 0x20, 0, 0x0c, 2, 0x0b, 0x0b},             // invalid label
		{2, 0x7b, 0x20, 0, 0x41, 0, 0x0e, 1, 0, 2, 0x0b, 0x0b},
		{0x41, 0, 4, 0x7b, 0x20, 0, 0x0b, 0x0b}, // missing result else
		{5, 0x0b}, {2, 0x40, 0x0b},              // unmatched structures
		{0x20, 0, 0x0f, 0x10, 0, 0x0b}, // unsupported calls still reject in dead code
		{3, 0x7b, 0x20, 0, 0x0b, 0x0b}, // loops still closed
		{0x20, 0, 2, 0, 0x0b, 0x0b},    // indexed signatures still closed
	} {
		ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}, Results: []wasm.ValType{wasm.V128}}
		if code, _, err := compileSWARPairs(&wasm.Func{BodyBytes: body}, ft, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("malformed/pending control emitted", body, err)
		}
	}
	for _, count := range []int{maxControlNesting + 1, integerBudget + 1} {
		body := bytes.Repeat([]byte{2, 0x40}, count)
		body = append(body, bytes.Repeat([]byte{0x0b}, count)...)
		body = append(body, 0x20, 0, 0x0b)
		fn, ft := swarValidatedFunction(t, fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: body})
		code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if count == maxControlNesting+1 && (len(code) == 0 || err != nil) || count == integerBudget+1 && (code != nil || err == nil) {
			t.Fatal("control depth bound", count, err)
		}
	}
}
