//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcConstructorIfPressureModule(typ wasm.ValType) []byte {
	vt := byte(0x7c)
	if typ == wasm.F32 {
		vt = 0x7d
	}
	params := make([]wasm.ValType, 33)
	for i := range params {
		params[i] = wasm.I64
		if i < 20 {
			params[i] = typ
		}
	}
	params[32] = wasm.I32
	results := []wasm.ValType{wasm.EqRef, wasm.EqRef}
	results = append(results, params...)
	results = append(results, typ)
	// More than 64 locals also exercises the conservative local-pin policy.
	body := []byte{1, 33, 0x63, 0, 0x23, 0, 0x23, 1}
	for i := range params {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x20, 0, 0x20, 19, 0xfb, 0, 0, 0x21, 33, 0x20, 32,
		0x04, vt, 0x20, 33, 0xfb, 2, 0, 0, 0x05, 0x20, 33, 0xfb, 2, 0, 1, 0x0b, 0x0b)
	init := wasmtest.Code([]byte{0x20, 0, 0x20, 0, 0xfb, 0, 0, 0x24, 0,
		0x20, 0, 0x20, 0, 0xfb, 0, 0, 0x24, 1, 0x0b})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 2, vt, 1, vt, 1},
			wasmtest.FuncType([]wasm.ValType{typ}, nil), wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b}, []byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(init, append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCConstructorIfPressure(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.F32, wasm.F64} {
		t.Run(fmt.Sprint(typ), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcConstructorIfPressureModule(typ))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c = publicArtifactRoundTrip(t, c)
			defer c.Close()
			for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
				for _, cond := range []uint64{0, 1} {
					cfg := GCConfig{Profile: profile, StressNurseryBytes: 64, VerifyAfterCollect: true}
					if profile == GCProfileThroughput {
						cfg.SurvivorBytes = 64
					}
					in, err := Instantiate(c, InstantiateOptions{GC: cfg})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					value := uint64(0x7ff0123456789abc)
					if typ == wasm.F32 {
						value = 0x7fa12345
					}
					if _, err := in.Invoke("init", value); err != nil {
						t.Fatal(err)
					}
					args := make([]uint64, 33)
					for i := range args {
						args[i] = uint64(i)*0x102030405 + 17
						if i < 20 {
							args[i] = value + uint64(i)
						}
					}
					args[32] = cond
					want := []uint64{readGlobalObject(in.globalCells[0], in.c.Globals[0].Type), readGlobalObject(in.globalCells[1], in.c.Globals[1].Type)}
					want = append(want, args...)
					selected := args[19]
					if cond != 0 {
						selected = args[0]
					}
					want = append(want, selected)
					before := in.gc.Stats()
					got, err := in.Invoke("run", args...)
					if err != nil || len(got) != len(want) {
						t.Fatalf("%x, %v", got, err)
					}
					for i := range want {
						actual := got[i]
						if i < 2 {
							entry, ok := in.refStore.gcByToken[actual]
							if !ok || entry.owner != in {
								t.Fatal("lost reference root")
							}
							actual = uint64(entry.ref)
						}
						if actual != want[i] {
							t.Fatalf("profile%d cond%d result%d: %x want%x", profile, cond, i, actual, want[i])
						}
					}
					if profile == GCProfileThroughput && in.gc.Stats().YoungBytesCopied <= before.YoungBytesCopied {
						t.Fatal("fixture did not move live roots")
					}
					for _, token := range got[:2] {
						if err := in.ReleaseGCRef(GCRef{token: token}); err != nil {
							t.Fatal(err)
						}
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
