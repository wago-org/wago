package amd64

import (
	"strconv"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestExternStoreColdDecodePreservesCallBoundaries(t *testing.T) {
	for _, ref := range []byte{0x6f, 0x70} {
		table := append(append(wasmtest.Name("h"), wasmtest.Name("t")...), 1, ref, 1, 1, 4)
		global := append(append(wasmtest.Name("h"), wasmtest.Name("g")...), 3, ref, 1)
		m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(table, global))))
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			name string
			body []byte
		}{
			{"global", []byte{0x24, 0, 0x0b}},
			{"table", []byte{0x26, 0, 0x0b}},
			{"fill", []byte{0xfc, 17, 0, 0x0b}},
			{"copy", []byte{0xfc, 14, 0, 0, 0x0b}},
			{"grow", []byte{0xfc, 15, 0, 0x0b}},
		} {
			t.Run(tc.name+"/"+strconv.Itoa(int(ref)), func(t *testing.T) {
				h := newFuncHints(0, 1)
				elig := newGlobalEligibilityTracker(1)
				var globals shared.GlobalHintAccumulator
				globals.Reset(1)
				got, err := scanBodyBytesIntoMemory64WithModuleCalls(tc.body, 0, 1, 0, h, &elig, false, m, nil, nil, false, nil, nil, 0, &globals)
				if err != nil {
					t.Fatal(err)
				}
				if got.flags.has(hintHasCall) != (ref == 0x6f) {
					t.Fatalf("helper call classification = %v for ref %x", got.flags.has(hintHasCall), ref)
				}
				budget := 0
				free, _, _, _ := scanLoopCallFreeDetails(wasm.NewReader(tc.body), wasm.NewModuleInstructionClassifier(m, true), false, nil, false, &budget, m)
				if free != (ref != 0x6f) {
					t.Fatalf("call-free=%v for ref %x", free, ref)
				}
			})
		}
	}
}
