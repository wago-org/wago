//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcPureIfPressureModule(typ wasm.ValType) []byte {
	storage := byte(0x7c)
	if typ == wasm.F32 {
		storage = 0x7d
	}
	params := make([]wasm.ValType, 32)
	for i := range params {
		params[i] = wasm.I64
		if i < 20 {
			params[i] = typ
		}
	}
	results := []wasm.ValType{wasm.EqRef, wasm.EqRef}
	results = append(results, params...)
	// More than 64 locals uses the compiler's conservative local-pin policy,
	// leaving a register for the address certificate under operand pressure.
	body := []byte{1, 33, 0x63, 0, 0x23, 0, 0x21, 32, 0x23, 1, 0x21, 33,
		0x20, 32, 0xfb, 2, 0, 0, 0x1a, 0x20, 32, 0x20, 33}
	for i := range params {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x20, 20, 0x50, 0x04, 0x40, 0x20, 21, 0x42, 3, 0x85, 0x1a, 0x05, 0x20, 22, 0x42, 2, 0x7c, 0x1a, 0x0b)
	for i := 0; i < 24; i++ {
		body = append(body, 0x20, 32, 0xfb, 2, 0, 0)
		results = append(results, typ)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, storage, 1}, wasmtest.FuncType([]wasm.ValType{typ}, nil), wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b}, []byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, 0, 0, 0x24, 0, 0x20, 0, 0xfb, 0, 0, 0x24, 1, 0x0b}), append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCPureIfFloatRootPressure(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.F32, wasm.F64} {
		t.Run(fmt.Sprint(typ), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcPureIfPressureModule(typ))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
				in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, VerifyAfterCollect: true}})
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
				args := make([]uint64, 32)
				for i := range args {
					args[i] = uint64(i)*0x102030405 + 17
					if i < 20 {
						args[i] = value + uint64(i)
					}
				}
				for round := 0; round < 2; round++ {
					args[20] = uint64(round)
					want := []uint64{readGlobalObject(in.globalCells[0], in.c.Globals[0].Type), readGlobalObject(in.globalCells[1], in.c.Globals[1].Type)}
					want = append(want, args...)
					for i := 0; i < 24; i++ {
						want = append(want, value)
					}
					got, err := in.Invoke("run", args...)
					if err != nil || len(got) != len(want) {
						t.Fatalf("profile%d: %x, %v", profile, got, err)
					}
					for i := range want {
						actual := got[i]
						if i < 2 {
							entry, ok := in.refStore.gcByToken[actual]
							if !ok || entry.owner != in {
								t.Fatalf("invalid reference token %x", actual)
							}
							actual = uint64(entry.ref)
						}
						if actual != want[i] {
							t.Fatalf("profile%d result%d: %x want%x", profile, i, got[i], want[i])
						}
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
