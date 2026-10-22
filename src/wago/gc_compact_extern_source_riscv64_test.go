//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"context"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"

	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64CompactExternSourceModule(vector ...bool) []byte {
	body := []byte{1, 1, 0x6f, 0x41, 42, 0xfb, 0, 0, 0xfb, 27, 0x21, 0, 0xfb, 1, 0, 0x1a, 0x20, 0, 0xfb, 26, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b}
	if len(vector) != 0 && vector[0] {
		prefix := append([]byte{0xfd, 12}, make([]byte, 16)...)
		prefix = append(prefix, 0x1a)
		body = append(append(append([]byte(nil), body[:3]...), prefix...), body[3:]...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.AnyRef}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.ExternRef}), wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{3}, []byte{4}, []byte{5}, []byte{1}, []byte{6})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6e, 1, 2, 4}, []byte{0x70, 1, 2, 4}, []byte{0x6f, 1, 2, 4})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("local", 0, 0), wasmtest.ExportEntry("internalize", 0, 1), wasmtest.ExportEntry("externalize", 0, 2), wasmtest.ExportEntry("get", 0, 3), wasmtest.ExportEntry("save", 0, 4), wasmtest.ExportEntry("run", 0, 5), wasmtest.ExportEntry("clear", 0, 6))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...), wasmtest.Code([]byte{0x20, 0, 0xfb, 26, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0xfb, 0, 0, 0xfb, 27, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0xfb, 26, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xfb, 0, 0, 0xfb, 27, 0x24, 0, 0x41, 0, 0x23, 0, 0x26, 2, 0xd0, 0x6f, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x41, 0, 0x25, 2, 0xfb, 26, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0x41, 0, 0xd0, 0x6f, 0x26, 2, 0x0b}))),
	)
}

func TestRV64CompactExternSourceAndArtifactPrivate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c, err := NewRuntimeConfig().Compile(rv64CompactExternSourceModule(vector))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		loaded := publicArtifactRoundTrip(t, c)
		defer loaded.Close()
		for artifact, candidate := range []*Compiled{c, loaded} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Logf("vector=%t artifact=%t profile=%v", vector, artifact != 0, profile)
				rt := NewRuntime()
				defer rt.Close()
				module, err := rt.Module(candidate)
				if err != nil {
					t.Fatal(err)
				}
				defer module.Close()
				in, err := rt.Instantiate(context.Background(), module, WithImports(NewImports()), WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if _, err := in.Invoke("save", 42); err != nil {
					t.Fatal("persistent save", err)
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
				if got, err := in.Invoke("run"); err != nil || len(got) != 1 || got[0] != 42 {
					t.Fatalf("persistent get after collection=%v,%v", got, err)
				}
				if _, err := in.Invoke("clear"); err != nil {
					t.Fatal(err)
				}

				if got, err := in.Invoke("local"); err != nil || len(got) != 1 || got[0] != 42 {
					t.Fatalf("converted local across collection: %v,%v", got, err)
				}
				got, err := in.Invoke("externalize", 42)
				if err != nil || len(got) != 1 {
					t.Fatalf("externalize: %v,%v", got, err)
				}
				token := got[0]
				if got, err := in.Invoke("get", token); err != nil || len(got) != 1 || got[0] != 42 {
					t.Fatalf("public converted extern: %v,%v", got, err)
				}
				if !rt.ReleaseExternRef(ExternRef{token}) {
					t.Fatal("public converted extern release")
				}
				foreign, err := rt.NewExternRef("foreign")
				if err != nil {
					t.Fatal(err)
				}
				got, err = in.Invoke("internalize", foreign.token)
				if err != nil || len(got) != 1 || got[0] != foreign.token {
					t.Fatalf("public foreign anyref: %v,%v", got, err)
				}
			}
		}
	}
}
