//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func rv64CustomSectionSource(base []byte, name string, data []byte) []byte {
	payload := append(uleb(nil, uint32(len(name))), name...)
	payload = append(payload, data...)
	// Prepend so the parsed branch-hint control also obeys decoder ordering.
	return append(section(append([]byte(nil), base[:8]...), 0, payload), base[8:]...)
}

func rv64CustomSectionFixtures() map[string][]byte {
	return map[string][]byte{
		"scalar": binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 7, 0x0b}}),
		"swar":   binaryModule(fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}}),
	}
}

func TestProductionOpaqueCustomSections(t *testing.T) {
	for profile, base := range rv64CustomSectionFixtures() {
		t.Run(profile, func(t *testing.T) {
			plain, err := wasm.DecodeModule(base)
			if err != nil {
				t.Fatal(err)
			}
			opts := CompileOptions{DeferCodeMapping: true, Interruptible: true}
			want, err := CompileModuleWith(plain, opts)
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name string
				data []byte
			}{
				{"opaque", []byte{0xff, 0, 0xfd, 0xfb, 0x12}},
				{"", []byte{0, 255}},
				{"opaque-λ", []byte{0xff}},
				{"target_features", []byte{0xff, 0}},
				{"execution-control", []byte{0xff}},
				{"producers", []byte{0xff, 0}},
				{"name", []byte{0, 2, 1, 'm'}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					source := rv64CustomSectionSource(base, tc.name, tc.data)
					if err := AdmitProductionSource(source); err != nil {
						t.Fatal("wire admission", err)
					}
					m, err := wasm.DecodeModule(source)
					if err != nil {
						t.Fatal(err)
					}
					got, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					if got.CodeImage != nil || !bytes.Equal(got.Code, want.Code) || !reflect.DeepEqual(got.Entry, want.Entry) || !reflect.DeepEqual(got.InternalEntry, want.InternalEntry) {
						t.Fatal("opaque metadata changed code, entries or deferred ownership")
					}
					clear(source)
					for i := range m.Customs {
						clear(m.Customs[i].Data)
					}
					if !bytes.Equal(got.Code, want.Code) {
						t.Fatal("code retained opaque payload storage")
					}
				})
			}
		})
	}
}

func TestProductionCustomSectionBoundsAndSemanticGates(t *testing.T) {
	for profile, base := range rv64CustomSectionFixtures() {
		t.Run(profile, func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				mutate func(*wasm.Module)
			}{
				{"count", func(m *wasm.Module) { m.Customs = make([]wasm.CustomSec, MaxFunctions+1) }},
				{"payload", func(m *wasm.Module) {
					m.Customs = []wasm.CustomSec{{Name: "opaque", Data: make([]byte, MaxModuleBytes)}}
				}},
				{"name", func(m *wasm.Module) { m.Customs = []wasm.CustomSec{{Name: strings.Repeat("x", MaxModuleBytes)}} }},
				{"aggregate", func(m *wasm.Module) {
					m.Customs = []wasm.CustomSec{{Data: make([]byte, MaxModuleBytes/2)}, {Data: make([]byte, MaxModuleBytes/2)}}
				}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					m, err := wasm.DecodeModule(base)
					if err != nil {
						t.Fatal(err)
					}
					tc.mutate(m)
					staged := false
					image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, MemoryPressure: func() { staged = true }})
					if image != nil || !errors.Is(err, ErrUnsupported) || staged {
						t.Fatal("unsupported metadata reached image staging", err)
					}
				})
			}
			oversized := rv64CustomSectionSource(base, "opaque", make([]byte, MaxModuleBytes))
			if err := AdmitProductionSource(oversized); !errors.Is(err, ErrUnsupported) {
				t.Fatal("oversized wire payload admitted", err)
			}
			branch := rv64CustomSectionSource(base, "metadata.code.branch_hint", []byte{0})
			m, err := wasm.DecodeModule(branch)
			if err != nil {
				t.Fatal("branch hint fixture", err)
			}
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
			if image == nil || err != nil {
				t.Fatal("empty parsed branch hint rejected", err)
			}
			plain, err := wasm.DecodeModule(base)
			if err != nil {
				t.Fatal(err)
			}
			want, err := CompileModuleWith(plain, CompileOptions{DeferCodeMapping: true, Interruptible: true})
			if err != nil || !bytes.Equal(image.Code, want.Code) || !reflect.DeepEqual(image.Entry, want.Entry) || !reflect.DeepEqual(image.InternalEntry, want.InternalEntry) {
				t.Fatal("empty branch metadata changed code layout", err)
			}
			opaque := rv64CustomSectionSource(base, "opaque", []byte{0xff})
			if profile == "scalar" {
				if image, err := Compile(opaque); image != nil || !errors.Is(err, ErrUnsupported) {
					t.Fatal("private scalar policy widened", err)
				}
			} else if image, err := compileSWARModule(opaque, emissionMode{production: true}); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("private SWAR policy widened", err)
			}
		})
	}
}

func TestProductionCustomMetadataAccountingAllocations(t *testing.T) {
	m := &wasm.Module{Customs: []wasm.CustomSec{{Name: "opaque", Data: []byte{0xff, 0}}}}
	allocs := testing.AllocsPerRun(100, func() {
		remaining := MaxModuleBytes
		if err := admitSWARDecodedMetadataBudget(m, &remaining); err != nil || remaining != MaxModuleBytes-len("opaque")-2 {
			t.Fatal("metadata byte accounting", remaining, err)
		}
	})
	if allocs != 0 {
		t.Fatal("metadata accounting allocated", allocs)
	}
}
