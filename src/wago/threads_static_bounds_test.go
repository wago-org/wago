//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func staticAtomicRMWModule(pages, address, offset uint32) []byte {
	limits := []byte{3}
	limits = append(limits, wasmtest.ULEB(pages)...)
	limits = append(limits, wasmtest.ULEB(pages)...)
	body := append([]byte{0x41}, wasmtest.SLEB32(int32(address))...)
	body = append(body, 0x20, 0, 0xfe, 0x1f, 3)
	body = append(body, wasmtest.ULEB(offset)...)
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec(limits)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("rmw", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestThreadsStaticBoundsMatchCheckedPath(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).WithMemoryLimitPages(65536)
	for _, tc := range []struct {
		pages, address, offset uint32
		proven                 bool
	}{
		{1, 0, 0, true}, {1, 8, 8, true}, {1, 65528, 0, true},
		{1, 1, 0, false}, {1, 65528, 1, false}, {1, 65528, 8, false},
		{0, 0, 0, false}, {1, 0xfffffff8, 8, false},
		{65536, 0x80000000, 0, true}, {65536, 0xfffffff8, 0, true},
		{65536, 0xfffffff8, 8, false},
	} {
		t.Run(fmt.Sprintf("pages=%d/addr=%x/off=%x", tc.pages, tc.address, tc.offset), func(t *testing.T) {
			module := staticAtomicRMWModule(tc.pages, tc.address, tc.offset)
			fast, err := cfg.WithOptimization("atomic-static-bounds", true).Compile(module)
			if err != nil {
				t.Fatal(err)
			}
			defer fast.Close()
			slow, err := cfg.WithOptimization("atomic-static-bounds", false).Compile(module)
			if err != nil {
				t.Fatal(err)
			}
			defer slow.Close()
			if bytes.Equal(fast.code, slow.code) == tc.proven {
				t.Fatal("constant bounds proof did not select exactly the admitted cases")
			}
			loaded := publicArtifactRoundTrip(t, fast)
			var reference []uint64
			var referenceTrap TrapCode
			var referenceMemory []byte
			for i, c := range []*Compiled{slow, fast, loaded} {
				in, err := Instantiate(c, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				mem := in.mem()
				ea := uint64(tc.address) + uint64(tc.offset)
				within := ea+8 <= uint64(len(mem))
				if within {
					binary.LittleEndian.PutUint64(mem[ea:], 0x80000000ffffffff)
				}
				got, err := in.Invoke("rmw", uint64(1))
				trap := TrapCode(0)
				if err != nil {
					te, ok := err.(*TrapError)
					if !ok {
						t.Fatal(err)
					}
					trap = te.Code
				}
				var contents []byte
				if within {
					contents = mem[ea : ea+8]
				}
				if i == 0 {
					reference, referenceTrap, referenceMemory = slices.Clone(got), trap, slices.Clone(contents)
				} else if !slices.Equal(got, reference) || trap != referenceTrap || !bytes.Equal(contents, referenceMemory) {
					t.Fatalf("result=%x/%x trap=%v/%v memory=%x/%x", got, reference, trap, referenceTrap, contents, referenceMemory)
				}
			}
		})
	}
}
