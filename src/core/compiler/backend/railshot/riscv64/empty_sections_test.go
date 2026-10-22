//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestProductionEmptySupportedSections(t *testing.T) {
	for name, fixture := range map[string]fixtureFunc{
		"scalar": {result: 0x7f, body: []byte{0x41, 7, 0x0b}},
		"swar":   {params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}},
	} {
		t.Run(name, func(t *testing.T) {
			base := binaryModule(fixture)
			plain, err := wasm.DecodeModule(base)
			if err != nil {
				t.Fatal(err)
			}
			opts := CompileOptions{DeferCodeMapping: true, Interruptible: true}
			want, err := CompileModuleWith(plain, opts)
			if err != nil {
				t.Fatal(err)
			}
			source := modifySection(base, 3, 2, []byte{0}, false)
			source = modifySection(source, 7, 4, []byte{0}, false)
			source = modifySection(source, 10, 9, []byte{0}, false)
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			got, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got.Code, want.Code) || !reflect.DeepEqual(got.Entry, want.Entry) || !reflect.DeepEqual(got.InternalEntry, want.InternalEntry) {
				t.Fatal("empty sections changed code or entries")
			}
		})
	}
	state := wasmtest.Module(wasmtest.Section(2, []byte{0}), wasmtest.Section(4, []byte{0}), wasmtest.Section(9, []byte{0}))
	if err := AdmitProductionSource(state); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(state)
	if err != nil {
		t.Fatal(err)
	}
	if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true}); err != nil || image == nil || image.CodeImage != nil || len(image.Code) != 0 {
		t.Fatal("empty sections invented executable code", err)
	}
}

func TestProductionEmptySectionsPreserveBounds(t *testing.T) {
	for _, id := range []byte{2, 4, 9} {
		limit := uint32(MaxPublicImports)
		if id == 4 {
			limit = MaxPublicCodeBytes / 16
		} else if id == 9 {
			limit = MaxPublicElementSegments
		}
		for _, payload := range [][]byte{nil, {0x80}, uleb(nil, limit+1)} {
			if err := AdmitProductionSource(wasmtest.Module(wasmtest.Section(id, payload))); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid/bounded section admitted", id, err)
			}
		}
		// The public wire policy consumes only section counts; normal decoding
		// remains responsible for malformed trailing bytes in empty sections.
		if _, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(id, []byte{0, 0xff}))); err == nil {
			t.Fatal("trailing empty-section payload accepted", id)
		}
	}
	if err := AdmitProductionSource(wasmtest.Module(wasmtest.Section(13, []byte{0}))); !errors.Is(err, ErrUnsupported) {
		t.Fatal("unsupported empty tag section admitted", err)
	}
}
