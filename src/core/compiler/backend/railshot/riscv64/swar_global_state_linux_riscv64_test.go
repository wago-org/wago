//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestSWARRV64LiteralGlobalModuleInitializationAndInstanceIsolation(t *testing.T) {
	for _, class := range []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b} {
		// The caller holds a raw vector and the old global live across a
		// forward call. The callee mutates the final directory entry.
		caller := fixtureFunc{params: []byte{0x7b, class}, results: []byte{0x7b, class, class}, locals: []byte{1, 126, 0x7b}, body: []byte{0x20, 0, 0x23}}
		caller.body = uleb(caller.body, 255)
		caller.body = append(caller.body, 0x20, 1, 0x10, 1, 0x23)
		caller.body = append(uleb(caller.body, 255), 0x0b)
		leaf := fixtureFunc{params: []byte{class}, locals: []byte{1, 127, 0x7b}, body: []byte{0x20, 0, 0x24}}
		leaf.body = append(uleb(leaf.body, 255), 0x0b)
		declarations := make([][]byte, 256)
		classes := []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b}
		for i := range declarations {
			c := classes[i%len(classes)]
			if i == 255 {
				c = class
			}
			declarations[i] = swarGlobalDeclaration(c, true, [2]uint64{0x7ff0000000000001 + uint64(i), 0x80000000deadbeef ^ uint64(i)})
		}
		source := swarGlobalDeclarationsSource([]fixtureFunc{caller, leaf}, declarations...)
		image, err := compileSWARModule(source, emissionMode{production: true})
		if err != nil || len(image.functions) != 2 || image.functions[0].FrameBytes <= 2048 {
			t.Fatal("literal global mixed-call module", class, err)
		}
		clear(source)
		entry := thunkImageRV64(t, image.code)
		type instance struct {
			engine        *coreruntime.Engine
			job           *coreruntime.JobMemory
			args, results []byte
			trap          []byte
			owner         *swarGlobalsOwner
			old           [2]uint64
		}
		var instances [2]instance
		for i := range instances {
			e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
			owner, err := image.instantiateGlobals(j)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := owner.Close(); err != nil {
					t.Error(err)
				}
			})
			instances[i] = instance{e, j, args, results, trap, owner, image.globals[255].value}
		}
		if instances[0].owner.directory == instances[1].owner.directory {
			t.Fatal("instance global directory aliased")
		}
		for _, pair := range dirtyInputs() {
			for n := range instances {
				in := &instances[n]
				input := [4]uint64{0x7ff0000000000001, 0x80000000deadbeef, pair[0] ^ uint64(n), pair[1] ^ uint64(n)}
				for i, value := range input {
					binary.LittleEndian.PutUint64(in.args[i*8:], value)
				}
				for i := range in.results {
					in.results[i] = 0xa5
				}
				if err := in.engine.CallPrepared(entry, in.args, in.job.LinMemBase(), in.trap, in.results); err != nil {
					t.Fatal(err)
				}
				current := [2]uint64{input[2], 0}
				if class == 0x7f || class == 0x7d {
					current[0] = uint64(uint32(current[0]))
				}
				if class == 0x7b {
					current[1] = input[3]
				}
				want := []uint64{input[0], input[1], in.old[0], current[0]}
				if class == 0x7b {
					want = []uint64{input[0], input[1], in.old[0], in.old[1], current[0], current[1]}
				}
				for i, value := range want {
					if binary.LittleEndian.Uint64(in.results[i*8:]) != value {
						t.Fatal("initialized old/current/call carrier", class, n, pair, i, want, in.results[:len(want)*8])
					}
				}
				if !bytes.Equal(in.results[len(want)*8:], bytes.Repeat([]byte{0xa5}, len(in.results)-len(want)*8)) {
					t.Fatal("global module result tail")
				}
				// Direct calls reuse the serialized argument arena. The leaf's
				// one carrier replaces its compact prefix; live caller values
				// were copied into canonical homes and are verified above.
				expectedArgs := input
				expectedArgs[0] = current[0]
				if class == 0x7b {
					expectedArgs[1] = current[1]
				}
				for i, value := range expectedArgs {
					if binary.LittleEndian.Uint64(in.args[i*8:]) != value {
						t.Fatal("callee compact argument prefix or untouched suffix", class, n, i)
					}
				}
				in.old = current
				// Check every exact-width cell in both owners after each call.
				// Mixed scalar/vector layout catches adjacent-cell overwrites.
				for other := range instances {
					for index, global := range image.globals {
						width, expected := 8, global.value
						if swarClassOf(global.typ.Type) == swarV128 {
							width = 16
						}
						if index == 255 {
							expected = instances[other].old
						}
						cell := swarOwnedGlobalCell(instances[other].owner, index, width)
						if binary.LittleEndian.Uint64(cell) != expected[0] || width == 16 && binary.LittleEndian.Uint64(cell[8:]) != expected[1] {
							t.Fatal("instance/neighbor cell changed", class, n, other, index)
						}
					}
				}
			}
		}
		in := &instances[1]
		if allocs := testing.AllocsPerRun(50, func() {
			if err := in.engine.CallPrepared(entry, in.args, in.job.LinMemBase(), in.trap, in.results); err != nil {
				panic(err)
			}
		}); allocs != 0 {
			t.Fatal("literal global module warm allocations", class, allocs)
		}
		if err := instances[0].owner.Close(); err != nil || instances[0].job.CaptureInstanceContext().GlobalsPtr != 0 {
			t.Fatal("first owner close", err)
		}
		// Releasing one owner's cells leaves the shared image and other owner
		// callable. A newly instantiated owner starts at the original literals.
		if err := in.engine.CallPrepared(entry, in.args, in.job.LinMemBase(), in.trap, in.results); err != nil {
			t.Fatal("surviving owner call", err)
		}
		fresh, err := image.instantiateGlobals(instances[0].job)
		if err != nil {
			t.Fatal(err)
		}
		width := 8
		if class == 0x7b {
			width = 16
		}
		cell := swarOwnedGlobalCell(fresh, 255, width)
		if binary.LittleEndian.Uint64(cell) != image.globals[255].value[0] || width == 16 && binary.LittleEndian.Uint64(cell[8:]) != image.globals[255].value[1] {
			t.Fatal("fresh owner retained previous mutations")
		}
		if err := fresh.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
