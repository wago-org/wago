//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCEmptyStructExplicitConstructor(t *testing.T) {
	body := []byte{1, 1, 0x63, 0, 0xfb, 0, 0, 0x21, 0, 0xfb, 0, 0, 0x1a, 0x20, 0, 0xd1, 0x45, 0x0b}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{1, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	for _, artifact := range []bool{false} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()

			rt := NewRuntime()
			defer rt.Close()
			mod, err := rt.Module(c)
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			in, err := rt.Instantiate(context.Background(), mod, WithGC(GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 32, ForceMajorEveryMinor: true, VerifyAfterCollect: true}))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for range 8 {
				got, err := in.Invoke("run")
				if err != nil || len(got) != 1 || got[0] != 1 {
					t.Fatalf("run=%v,%v want [1]", got, err)
				}
			}
		})
	}
}
