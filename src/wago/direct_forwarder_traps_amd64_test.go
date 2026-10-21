//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func directForwarderTrapsModule() []byte {
	direct := []byte{0x20, 0, 0x20, 1, 0x10, 2, 0x0b}
	globals := make([][]byte, 8)
	dirty := []byte{0x03, 0x40}
	for g := byte(0); g < 8; g++ {
		globals[g] = wasmtest.GlobalEntry(wasm.I64, true, []byte{0x42, 0, 0x0b})
		dirty = append(dirty, 0x23, g, 0x42, g+1, 0x7c, 0x24, g)
	}
	dirty = append(dirty, 0x0b)
	argTrap := append(append([]byte(nil), dirty...), 0x20, 0, 0x28, 2, 0, 0x20, 1, 0x10, 2, 0x0b)
	dirtyCall := append(append([]byte(nil), dirty...), direct...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1},
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1}, []byte{1}, []byte{1}, []byte{1}, []byte{1}, []byte{2}, []byte{2}, []byte{1}, []byte{1}, []byte{3})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 2})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(6, wasmtest.Vec(globals...)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("wrapper", 0, 2), wasmtest.ExportEntry("direct", 0, 3), wasmtest.ExportEntry("indirect", 0, 4), wasmtest.ExportEntry("viaRef", 0, 5),
			wasmtest.ExportEntry("identity", 0, 6), wasmtest.ExportEntry("targetIdentity", 0, 7), wasmtest.ExportEntry("argTrap", 0, 8), wasmtest.ExportEntry("dirty", 0, 9))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 2, 2, 0})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x6d, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x10, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x10, 1, 0x0b}),
			wasmtest.Code(direct),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x41, 0, 0x11, 1, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0xd2, 2, 0x14, 1, 0x0b}),
			wasmtest.Code([]byte{0xd2, 2, 0x0b}), wasmtest.Code([]byte{0xd2, 0, 0x0b}),
			wasmtest.Code(argTrap), wasmtest.Code(dirtyCall), wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x0b}))),
	)
}

func TestDirectForwarderTrapsAndIdentity(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("direct-forwarder", enabled).WithOptimization("inline", false).WithOptimization("immutable-table", false), directForwarderTrapsModule())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		loaded := publicArtifactRoundTrip(t, c)
		defer loaded.Close()
		for ai, artifact := range []*Compiled{c, loaded} {
			if status := artifact.GCNativeRootAdmission(); !status.Required || !status.Exact {
				t.Fatalf("trap fixture needs exact roots: %+v", status)
			}
			in, err := Instantiate(artifact)
			if err != nil {
				t.Fatal(err)
			}
			func() {
				defer in.Close()
				for _, export := range []string{"wrapper", "direct", "indirect", "viaRef"} {
					got, err := in.Invoke(export, 42, 2)
					if err != nil || len(got) != 1 || got[0] != 21 {
						t.Fatalf("%s=%v,%v", export, got, err)
					}
					_, err = in.Invoke(export, 42, 0)
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapDivZero {
						t.Fatalf("%s divide trap=%v", export, err)
					}
					if len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != 0 {
						t.Fatalf("trap lost actual target source: %+v", trap.Frames)
					}
				}
				identity, err := in.InvokeValues(context.Background(), "identity")
				if err != nil || len(identity) != 1 {
					t.Fatalf("identity=%v,%v", identity, err)
				}
				if !in.FuncRefMatchesFunction(identity[0].FuncRef(), 2) || in.FuncRefMatchesFunction(identity[0].FuncRef(), 0) {
					t.Fatal("forwarder function identity was replaced")
				}
				target, err := in.InvokeValues(context.Background(), "targetIdentity")
				if err != nil || len(target) != 1 || identity[0].Bits() == target[0].Bits() {
					t.Fatalf("wrapper and target tokens alias: %v,%v", target, err)
				}
			}()
			for _, tc := range []struct {
				name string
				x, y uint64
				trap TrapCode
			}{
				{"argTrap", 65536, 0, TrapLinMemOutOfBounds},
				{"dirty", 42, 0, TrapDivZero}, {"dirty", 1 << 31, 0xffffffff, TrapDivOverflow},
			} {
				t.Run(fmt.Sprintf("enabled%t/artifact%d/%s/%d", enabled, ai, tc.name, tc.trap), func(t *testing.T) {
					in, err := Instantiate(artifact)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					_, err = in.Invoke(tc.name, tc.x, tc.y)
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != tc.trap {
						t.Fatalf("trap=%v,want%d", err, tc.trap)
					}
					for g := 0; g < 8; g++ {
						if got := readGlobalObject(in.globalCells[g], in.c.Globals[g].Type); got != uint64(g+1) {
							t.Fatalf("global%d after trap=%d,want%d", g, got, g+1)
						}
					}
				})
			}
		}
	}
}
