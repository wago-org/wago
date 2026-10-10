//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64SingletonRecModule(vector, wrapped bool) []byte {
	typ := byte(0x7f)
	if vector {
		typ = 0x7b
	}
	first := []byte{0x60, 1, typ, 1, typ}
	if wrapped {
		first = append([]byte{0x4e, 1}, first...)
	}
	types := append([]byte{2}, first...)
	types = append(types, 0x60, 2, typ, 0x7f, 1, typ)
	return wasmtest.Module(
		wasmtest.Section(1, types),
		wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 0, 0, 0x0b}),
		)),
	)
}

func TestRV64PublicExplicitSingletonRec(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				key := fmt.Sprintf("singleton-rec-vector%t", vector)
				plain := rv64TableStageCompile(t, key+"-compact", rv64SingletonRecModule(vector, false), artifact)
				wrapped := rv64TableStageCompile(t, key+"-wrapped", rv64SingletonRecModule(vector, true), artifact)
				if !bytes.Equal(plain.code, wrapped.code) || !slices.Equal(plain.Entry, wrapped.Entry) || !slices.Equal(plain.InternalEntry, wrapped.InternalEntry) || !slices.Equal(plain.FuncTypeID, wrapped.FuncTypeID) {
					t.Fatal("singleton rec changed code, entries or type identity")
				}
				rt := NewRuntime()
				defer rt.Close()
				instances := []*Instance{
					rv64NativeTokenInstantiate(t, rt, plain, NewImports()),
					rv64NativeTokenInstantiate(t, rt, wrapped, NewImports()),
				}
				plain.Close()
				wrapped.Close()
				args, want := []uint64{0xdeadbeef00000007, 0}, []uint64{7}
				if vector {
					args = []uint64{0x0123456789abcdef, 0xfedcba9876543210, 0}
					want = args[:2]
				}
				for _, in := range instances {
					importedGlobalInvoke(t, in, "call", want, args...)
					in.Close()
				}
			})
		}
	}
}

func TestRV64PublicExplicitSingletonRecStateOnly(t *testing.T) {
	source := wasmtest.Module(wasmtest.Section(1, []byte{1, 0x4e, 1, 0x60, 1, 0x7e, 1, 0x7e}))
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "singleton-rec-state", source, artifact)
			if !c.hasNoFunctionCode() || c.codeCache.base != 0 || c.codeCache.mem != nil || c.codeCache.sealed {
				t.Fatal("state-only singleton rec allocated executable code")
			}
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			in.Close()
			c.Close()
		})
	}
}

func TestRV64PublicExplicitSingletonRecWrappedSIMDFeatureGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler unavailable in compiler-free qualification")
	}
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureSIMD, false)
	if compiled, err := Compile(cfg, rv64SingletonRecModule(true, true)); compiled != nil || err == nil {
		if compiled != nil {
			compiled.Close()
		}
		t.Fatal("disabled SIMD admitted wrapped v128 declaration", err)
	}
}
