//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func explicitSingletonRecModule(f fixtureFunc) []byte {
	compact := binaryModule(f)
	payload := []byte{1, 0x4e, 1, 0x60, byte(len(f.params))}
	payload = append(payload, f.params...)
	if f.results != nil {
		payload = append(payload, byte(len(f.results)))
		payload = append(payload, f.results...)
	} else if f.result == 0 {
		payload = append(payload, 0)
	} else {
		payload = append(payload, 1, f.result)
	}
	return modifySection(compact, 1, 1, payload, true)
}

func singletonRecControlModule(vector bool, shape string, wrapped bool) []byte {
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

func TestPublicExplicitSingletonRecAdmissionIdentity(t *testing.T) {
	for _, f := range []fixtureFunc{
		{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x0b}},
		{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}},
	} {
		compact := binaryModule(f)
		wrapped := explicitSingletonRecModule(f)
		if err := AdmitProductionSource(wrapped); err != nil {
			t.Fatal("public singleton rec source", f.params, err)
		}
		if err := admitWire(wrapped); !errors.Is(err, ErrUnsupported) {
			t.Fatal("private singleton rec source", f.params, err)
		}
		cm, err := wasm.DecodeModule(compact)
		if err != nil {
			t.Fatal(err)
		}
		wm, err := wasm.DecodeModule(wrapped)
		if err != nil {
			t.Fatal(err)
		}
		if len(cm.Types) != 1 || len(wm.Types) != 1 || len(wm.Types[0].SubTypes) != 1 {
			t.Fatal("decoded singleton rec shape")
		}
		opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true}
		plain, err := CompileModuleWith(cm, opts)
		if err != nil {
			t.Fatal("compact compile", err)
		}
		got, err := CompileModuleWith(wm, opts)
		if err != nil {
			t.Fatal("wrapped compile", err)
		}
		if !bytes.Equal(plain.Code, got.Code) || !slices.Equal(plain.Entry, got.Entry) || !slices.Equal(plain.InternalEntry, got.InternalEntry) {
			t.Fatal("explicit singleton changed RV64 code layout")
		}
		if plain.CodeImage != nil {
			plain.CodeImage.Close()
		}
		if got.CodeImage != nil {
			got.CodeImage.Close()
		}
	}
}

func TestPublicExplicitSingletonRecStateOnly(t *testing.T) {
	source := []byte{0, 97, 115, 109, 1, 0, 0, 0, 1, 6, 1, 0x4e, 1, 0x60, 0, 0}
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true})
	if err != nil || image == nil || len(image.Code) != 0 || image.CodeImage != nil {
		t.Fatal("state-only singleton rec", image, err)
	}
}

func TestPublicExplicitSingletonRecAbstractControlIdentity(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, shape := range []string{"legacy", "nullable", "nonnull", "indexed-nullable", "indexed-nonnull"} {
			compact := singletonRecControlModule(vector, shape, false)
			wrapped := singletonRecControlModule(vector, shape, true)
			if err := AdmitProductionSource(wrapped); err != nil {
				t.Fatal("public wrapped abstract control", vector, shape, err)
			}
			if err := admitWire(wrapped); !errors.Is(err, ErrUnsupported) {
				t.Fatal("private wrapped abstract control", vector, shape, err)
			}
			plainModule, err := wasm.DecodeModule(compact)
			if err != nil {
				t.Fatal(err)
			}
			wrappedModule, err := wasm.DecodeModule(wrapped)
			if err != nil {
				t.Fatal(err)
			}
			if shape == "indexed-nullable" || shape == "indexed-nonnull" {
				plainRef := plainModule.Types[1].SubTypes[0].Comp.Results[0]
				wrappedRef := wrappedModule.Types[1].SubTypes[0].Comp.Results[0]
				idx := wrappedRef.Ref().Heap().Type()
				if !wasm.EqualValType(plainRef, wrappedRef) || idx.Index != 0 || idx.Rec {
					t.Fatal("wrapped indexed control changed absolute type identity", plainRef, wrappedRef, idx)
				}
			}
			var plainPressure, wrappedPressure int
			typed := shape != "legacy" && shape != "nullable"
			plain, err := CompileModuleWith(plainModule, referenceControlProductionOptions(typed, &plainPressure))
			if err != nil {
				t.Fatal("compact control", err)
			}
			got, err := CompileModuleWith(wrappedModule, referenceControlProductionOptions(typed, &wrappedPressure))
			if err != nil {
				t.Fatal("wrapped control", err)
			}
			if plainPressure != 1 || wrappedPressure != 1 || !bytes.Equal(plain.Code, got.Code) || !slices.Equal(plain.Entry, got.Entry) || !slices.Equal(plain.InternalEntry, got.InternalEntry) || plain.NativeReferenceControl != got.NativeReferenceControl || plain.NativeReferenceCalls != got.NativeReferenceCalls {
				t.Fatal("singleton rec changed abstract control production", plainPressure, wrappedPressure)
			}
			if plain.CodeImage != nil {
				plain.CodeImage.Close()
			}
			if got.CodeImage != nil {
				got.CodeImage.Close()
			}
		}
	}
}

func TestPublicExplicitSingletonRecRejectsBroaderGrammar(t *testing.T) {
	header := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	for name, payload := range map[string][]byte{
		"empty":           {1, 0x4e, 0},
		"multiple":        {1, 0x4e, 2, 0x60, 0, 0, 0x60, 0, 0},
		"nested":          {1, 0x4e, 1, 0x4e, 1, 0x60, 0, 0},
		"final-subtype":   {1, 0x4e, 1, 0x4f, 0, 0x60, 0, 0},
		"open-subtype":    {1, 0x4e, 1, 0x50, 0, 0x60, 0, 0},
		"struct":          {1, 0x4e, 1, 0x5f, 0},
		"array":           {1, 0x4e, 1, 0x5e, 0x7f, 0},
		"self-indexed":    {1, 0x4e, 1, 0x60, 1, 0x63, 0, 0},
		"forward-indexed": {2, 0x4e, 1, 0x60, 1, 0x64, 1, 0, 0x60, 0, 0},
		"nullable-extern": {1, 0x4e, 1, 0x60, 1, 0x63, 0x6f, 0},
		"nonnull-extern":  {1, 0x4e, 1, 0x60, 1, 0x64, 0x6f, 0},
		"truncated":       {1, 0x4e},
		"arity":           append([]byte{1, 0x4e, 1, 0x60, 0x81, 0x02}, make([]byte, 257)...),
	} {
		t.Run(name, func(t *testing.T) {
			source := section(append([]byte(nil), header...), 1, payload)
			if err := admitWireStartProfile(source, true, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("broader singleton rec grammar admitted", err)
			}
		})
	}
}
