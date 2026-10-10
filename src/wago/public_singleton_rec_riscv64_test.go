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

func rv64SingletonRecControlModule(vector bool, shape string, wrapped bool) []byte {
	typ := byte(0x7e)
	if vector {
		typ = 0x7b
	}
	control := []byte{0x60, 0, 1, 0x70}
	body := []byte{0x20, 0, 0x1a, 0x02, 1, 0xd0, 0x70, 0x0b, 0xd1, 0x0b}
	if shape == "nullable" {
		control = []byte{0x60, 0, 1, 0x63, 0x70}
	} else if shape == "nonnull" {
		control = []byte{0x60, 0, 1, 0x64, 0x70}
		body = []byte{0x20, 0, 0x1a, 0x02, 1, 0xd2, 0, 0x0b, 0xd1, 0x0b}
	} else if shape == "indexed-nullable" {
		control = []byte{0x60, 0, 1, 0x63, 0}
		body = []byte{0x20, 0, 0x1a, 0x02, 1, 0xd0, 0, 0x0b, 0xd1, 0x0b}
	} else if shape == "indexed-nonnull" {
		control = []byte{0x60, 0, 1, 0x64, 0}
		body = []byte{0x20, 0, 0x1a, 0x02, 1, 0xd2, 0, 0x0b, 0xd1, 0x0b}
	}
	if wrapped {
		control = append([]byte{0x4e, 1}, control...)
	}
	types := []byte{2, 0x60, 1, typ, 1, 0x7f}
	types = append(types, control...)
	return wasmtest.Module(
		wasmtest.Section(1, types),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
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

func TestRV64PublicExplicitSingletonRecAbstractControl(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, shape := range []string{"legacy", "nullable", "nonnull", "indexed-nullable", "indexed-nonnull"} {
			for _, artifact := range []bool{false, true} {
				name := fmt.Sprintf("vector%t-%s-artifact%t", vector, shape, artifact)
				t.Run(name, func(t *testing.T) {
					key := fmt.Sprintf("singleton-rec-control-vector%t-%s", vector, shape)
					compile := rv64TableStageCompile
					typed := shape != "legacy" && shape != "nullable"
					if typed {
						if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
							t.Skip("precompiled artifact path only")
						}
						compile = rv64NativeTailCompile
					}
					plain := compile(t, key+"-compact", rv64SingletonRecControlModule(vector, shape, false), artifact)
					wrapped := compile(t, key+"-wrapped", rv64SingletonRecControlModule(vector, shape, true), artifact)
					if !bytes.Equal(plain.code, wrapped.code) || !slices.Equal(plain.Entry, wrapped.Entry) || !slices.Equal(plain.InternalEntry, wrapped.InternalEntry) || !slices.Equal(plain.FuncTypeID, wrapped.FuncTypeID) || plain.requiredFeatures != wrapped.requiredFeatures || plain.nativeReferenceControlABI != wrapped.nativeReferenceControlABI || plain.nativeReferenceCallABI != wrapped.nativeReferenceCallABI {
						t.Fatal("singleton rec changed abstract control image")
					}
					if !wrapped.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || wrapped.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) != typed || wrapped.nativeReferenceControlABI != typed || wrapped.nativeReferenceCallABI {
						t.Fatal("wrapped abstract control feature/ABI witness", wrapped.requiredFeatures, wrapped.nativeReferenceControlABI, wrapped.nativeReferenceCallABI)
					}
					rt := NewRuntime()
					defer rt.Close()
					instances := []*Instance{
						rv64NativeTokenInstantiate(t, rt, plain, NewImports()),
						rv64NativeTokenInstantiate(t, rt, wrapped, NewImports()),
					}
					plain.Close()
					wrapped.Close()
					args := []uint64{9}
					if vector {
						args = []uint64{0x0123456789abcdef, 0xfedcba9876543210}
					}
					want := []uint64{1}
					if shape == "nonnull" || shape == "indexed-nonnull" {
						want[0] = 0
					}
					for _, in := range instances {
						importedGlobalInvoke(t, in, "run", want, args...)
						in.Close()
					}
				})
			}
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

func TestRV64PublicExplicitSingletonRecAbstractControlStateOnly(t *testing.T) {
	for _, shape := range []string{"legacy", "nullable", "nonnull", "indexed-nullable", "indexed-nonnull"} {
		ref := []byte{0x70}
		if shape == "nullable" {
			ref = []byte{0x63, 0x70}
		} else if shape == "nonnull" {
			ref = []byte{0x64, 0x70}
		} else if shape == "indexed-nullable" {
			ref = []byte{0x63, 0}
		} else if shape == "indexed-nonnull" {
			ref = []byte{0x64, 0}
		}
		typePayload := []byte{1, 0x4e, 1, 0x60, 0, 1}
		if shape == "indexed-nullable" || shape == "indexed-nonnull" {
			typePayload = []byte{2, 0x60, 0, 0, 0x4e, 1, 0x60, 0, 1}
		}
		typePayload = append(typePayload, ref...)
		source := wasmtest.Module(wasmtest.Section(1, typePayload))
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-artifact%t", shape, artifact), func(t *testing.T) {
				typed := shape != "legacy" && shape != "nullable"
				compile := rv64TableStageCompile
				if typed {
					if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
						t.Skip("precompiled artifact path only")
					}
					compile = rv64NativeTailCompile
				}
				c := compile(t, fmt.Sprintf("singleton-rec-control-state-%s", shape), source, artifact)
				if !c.hasNoFunctionCode() || c.codeCache.base != 0 || c.codeCache.mem != nil || c.codeCache.sealed || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) != typed || c.nativeReferenceControlABI || c.nativeReferenceCallABI {
					t.Fatal("state-only abstract singleton rec image", c.requiredFeatures, c.nativeReferenceControlABI, c.nativeReferenceCallABI)
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

func TestRV64PublicExplicitSingletonRecWrappedReferenceFeatureGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" || os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler unavailable in compiler-free qualification")
	}
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false)
	for _, shape := range []string{"legacy", "nullable", "nonnull", "indexed-nullable", "indexed-nonnull"} {
		if compiled, err := Compile(cfg, rv64SingletonRecControlModule(false, shape, true)); compiled != nil || err == nil {
			if compiled != nil {
				compiled.Close()
			}
			t.Fatal("disabled reference types admitted wrapped abstract control", shape, err)
		}
	}
	for _, shape := range []string{"nonnull", "indexed-nullable", "indexed-nonnull"} {
		for _, cfg := range []*RuntimeConfig{compatibilityDefaultConfig(), NewRuntimeConfig().WithFeature(CoreFeatureTypedFunctionReferences, false)} {
			if compiled, err := Compile(cfg, rv64SingletonRecControlModule(false, shape, true)); compiled != nil || err == nil {
				if compiled != nil {
					compiled.Close()
				}
				t.Fatal("typed references disabled admitted wrapped typed control", shape, err)
			}
		}
	}
	referenceTarget := wasmtest.Module(wasmtest.Section(1, []byte{
		2,
		0x60, 1, 0x70, 0,
		0x4e, 1, 0x60, 0, 1, 0x63, 0,
	}))
	if compiled, err := rv64NativeTailConfig().Compile(referenceTarget); compiled != nil || err == nil {
		if compiled != nil {
			compiled.Close()
		}
		t.Fatal("wrapped indexed control admitted reference-bearing target", err)
	}
}
