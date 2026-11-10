//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcFinalResolverTrapPinsModule() []byte {
	globals := make([][]byte, 0, 9)
	for range 8 {
		globals = append(globals, wasmtest.GlobalEntry(wasm.I64, true, []byte{0x42, 0, 0x0b}))
	}
	globals = append(globals, []byte{0x6d, 1, 0xd0, 0x6d, 0x0b})
	bodies := make([][]byte, 2)
	for i := range bodies {
		body := []byte{0x03, 0x40} // one call-free loop iteration makes globals hot
		for g := byte(0); g < 8; g++ {
			body = append(body, 0x23, g, 0x42, g+1, 0x7c, 0x24, g)
		}
		body = append(body, 0x0b, 0x23, 8, 0xfb, byte(22+i), 0, 0xfb, 2, 0, 0, 0x0b)
		bodies[i] = wasmtest.Code(body)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x5f, 1, 0x7e, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec(globals...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("nonnull", 0, 0), wasmtest.ExportEntry("nullable", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestGCFinalResolverTrapPreservesGlobalPins(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalResolverTrapPinsModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// Neither function has parameters or locals. Thus the resolver's saved
	// R9/R10/R11 are global value pins, not unused register saves or local pins.
	saves := []byte{0x41, 0x51, 0x41, 0x52, 0x41, 0x53}
	if n := bytes.Count(c.code, saves); n != 2 {
		t.Fatalf("resolver R9/R10/R11 save sequences = %d, want 2; fixture must exercise all three global pins", n)
	}

	for ai, artifact := range []*Compiled{c} {
		for _, export := range []string{"nonnull", "nullable"} {
			for _, kind := range []string{"wrong", "null", "valid"} {
				t.Run(fmt.Sprintf("artifact%d/%s/%s", ai, export, kind), func(t *testing.T) {
					in, err := Instantiate(artifact)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if kind != "null" {
						typeIndex := uint32(0)
						if kind == "wrong" {
							typeIndex = 1
						}
						ref, err := in.gc.NewStructDefaultWithRoots(in.requireGCDomainType(typeIndex), gc.EmptyRoots{})
						if err != nil {
							t.Fatal(err)
						}
						writeGlobalObject(in.globalCells[8], in.c.Globals[8].Type, uint64(ref))
					}
					got, err := in.Invoke(export)
					if kind == "valid" {
						if err != nil || len(got) != 1 || got[0] != 0 {
							t.Fatalf("valid read = %v, %v", got, err)
						}
					} else {
						want := TrapCastFailure
						if kind == "null" && export == "nullable" {
							want = TrapNullReference
						}
						var trap *TrapError
						if !errors.As(err, &trap) || trap.Code != want {
							t.Fatalf("read trap = %v; want %d", err, want)
						}
					}
					for g := 0; g < 8; g++ {
						if got := readGlobalObject(in.globalCells[g], in.c.Globals[g].Type); got != uint64(g+1) {
							t.Errorf("global %d after %s read = %#x; want %d", g, kind, got, g+1)
						}
					}
				})
			}
		}
	}
}
