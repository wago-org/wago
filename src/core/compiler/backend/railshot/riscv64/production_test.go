//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestProductionEnvelopeOwnershipAndConservativeEntries(t *testing.T) {
	data := binaryModule(sumCallFixture(0), fixtureFunc{params: []byte{0x7f, 0x7e}, results: []byte{0x7f, 0x7e}, body: []byte{0x20, 0, 0x20, 1, 0x0b}})
	var expected []byte
	for _, heap := range []bool{false, true} {
		m, err := CompileProduction(data, ProductionOptions{DeferCodeMapping: heap})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := m.CodeImage.Close(); err != nil {
				t.Error(err)
			}
		})
		if m.CodeImage == nil || len(m.Code) == 0 || len(m.Entry) != 2 || len(m.InternalEntry) != 2 {
			t.Fatal("production image/entry metadata")
		}
		for i, entry := range m.Entry {
			if entry&15 != 0 || entry != m.InternalEntry[i] || entry < 0 || entry >= len(m.Code) {
				t.Fatal("unframed or misaligned entry", m.Entry, m.InternalEntry)
			}
		}
		if uintptr(unsafe.Pointer(&m.InternalEntry[0])) != uintptr(unsafe.Pointer(&m.Entry[0]))+uintptr(len(m.Entry))*unsafe.Sizeof(m.Entry[0]) {
			t.Fatal("entry tables do not share the exact bounded allocation")
		}
		if len(m.DirectPrepared) != 0 || len(m.DirectPreparedLight) != 0 || len(m.DirectPreparedBounded) != 0 || m.PreparedIsolatedTables || m.RequiredAMD64Features != 0 || m.RequiresBMI2 || m.RequiresAVX2 || m.RequiresAVX512 || m.RequiresBitCount != 0 {
			t.Fatal("unproven entry/ISA metadata advertised")
		}
		if expected == nil {
			expected = append([]byte(nil), m.Code...)
		} else if !bytes.Equal(expected, m.Code) {
			t.Fatal("staging ownership changed emitted code")
		}
		image := m.CodeImage.(*coreruntime.CodeBuffer)
		if heap {
			if _, _, err := image.Take(); err == nil {
				t.Fatal("heap staging advertised an off-heap address")
			}
			owned, err := image.TakeHeap()
			if err != nil || !bytes.Equal(owned, m.Code) {
				t.Fatal("heap transfer", err)
			}
		} else {
			mapping, base, err := image.Take()
			if err != nil {
				t.Fatal(err)
			}
			if base == 0 || !bytes.Equal(mapping[:len(m.Code)], m.Code) {
				t.Fatal("mapping transfer")
			}
			if err := coreruntime.Unmap(mapping); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestProductionEnvelopeRejectsUnsupportedBeforeImage(t *testing.T) {
	base := binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 42, 0x0b}})
	for _, id := range []byte{2, 4, 9, 13} {
		before := byte(7)
		if id == 2 {
			before = 3
		} else if id == 9 || id == 12 {
			before = 10
		} else if id == 11 {
			before = 255 // append after the last standard section
		}
		data := modifySection(base, before, id, []byte{0}, false)
		image, err := CompileProduction(data, ProductionOptions{})
		if image != nil || err == nil {
			t.Fatal("unsupported present section published", id, image, err)
		}
	}
	for _, f := range []fixtureFunc{
		{unexported: true, body: []byte{0xfd, 0, 0x0b}},
	} {
		image, err := CompileProduction(binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 42, 0x0b}}, f), ProductionOptions{})
		if image != nil || err == nil {
			t.Fatal("unsupported unexported function published", image, err)
		}
	}
	metadata := section(append([]byte(nil), base...), 0, []byte{4, 'n', 'a', 'm', 'e'})
	image, err := CompileProduction(metadata, ProductionOptions{DeferCodeMapping: true})
	if err != nil {
		t.Fatal("inert metadata rejected", err)
	}
	if err := image.CodeImage.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileProduction(modifySection(base, 7, 4, []byte{0}, false), ProductionOptions{}); !errors.Is(err, ErrUnsupported) {
		t.Fatal("unsupported section did not fail closed", err)
	}
}

func TestProductionStateAdmissionMemory32AndFailClosed(t *testing.T) {
	fs := []fixtureFunc{memoryFixture(0x2d, 0), growFixture()}
	data := stateModule(fs, false, 65536, 65536, true, nil, nil)
	image, err := CompileProduction(data, ProductionOptions{DeferCodeMapping: true})
	if err != nil {
		t.Fatal("production full memory32 limit rejected", err)
	}
	if err := image.CodeImage.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(data); !errors.Is(err, ErrUnsupported) {
		t.Fatal("experimental runtime limit was silently widened", err)
	}
	for _, heap := range []bool{false, true} {
		image, err := CompileProduction(stateModule(fs, true, 0, 1, true, nil, nil), ProductionOptions{DeferCodeMapping: heap})
		if err != nil {
			t.Fatal("ordinary imported memory rejected", err)
		}
		if err := image.CodeImage.Close(); err != nil {
			t.Fatal(err)
		}
	}
	base := stateModule(fs, false, 0, 1, true, nil, nil)
	for _, data := range [][]byte{
		modifySection(base, 5, 5, []byte{1, 3, 0, 1}, true),          // shared
		modifySection(base, 5, 5, []byte{1, 5, 0, 1}, true),          // memory64
		modifySection(base, 5, 5, []byte{2, 1, 0, 1, 1, 0, 1}, true), // multiple
		modifySection(binaryModule(fixtureFunc{body: []byte{0x0b}}), 10, 8, []byte{0}, false),
	} {
		image, err := CompileProduction(data, ProductionOptions{})
		if image != nil || err == nil {
			t.Fatal("unsupported state acquired an image", image, err)
		}
	}
}

func TestProductionImportedMemoryRejectsOtherDependenciesBeforeImage(t *testing.T) {
	base := binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 7, 0x0b}})
	entry := func(kind byte, typ ...byte) []byte {
		return append([]byte{1, 'm', 1, 'x', kind}, typ...)
	}
	for _, shape := range []struct {
		name    string
		imports []byte
	}{
		{"function", append([]byte{1}, entry(0, 0)...)},
		{"reference-global", append([]byte{1}, entry(3, 0x70, 0)...)},
		{"table", append([]byte{1}, entry(1, 0x70, 1, 0, 1)...)},
		{"shared-memory", append([]byte{1}, entry(2, 3, 0, 1)...)},
		{"memory64", append([]byte{1}, entry(2, 5, 0, 1)...)},
		{"two-memories", append(append([]byte{2}, entry(2, 1, 0, 1)...), entry(2, 1, 0, 1)...)},
	} {
		t.Run(shape.name, func(t *testing.T) {
			data := modifySection(base, 3, 2, shape.imports, false)
			for _, heap := range []bool{false, true} {
				image, err := CompileProduction(data, ProductionOptions{DeferCodeMapping: heap})
				if image != nil || err == nil {
					t.Fatal("unsupported dependency reached production image", image, err)
				}
			}
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal("invalid rejection fixture", err)
			}
			if shape.name == "two-memories" {
				if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{MultiMemory: true}, 1, wasm.ValidationLimits{}); err != nil {
					t.Fatal("policy fixture is not a valid module", err)
				}
				if _, err := admitStateMode(m, true); !errors.Is(err, ErrUnsupported) {
					t.Fatal("validated unsupported import set escaped the gate", err)
				}
			}
			pressureCalls := 0
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, MemoryPressure: func() { pressureCalls++ }})
			if shape.name == "two-memories" {
				if image == nil || image.CodeImage != nil || err != nil || pressureCalls != 1 {
					t.Fatal("public indexed memories rejected while private gate remains closed", err, pressureCalls)
				}
				return
			}
			if image != nil || err == nil || pressureCalls != 0 {
				t.Fatal("decoded unsupported dependency reached staging", image, err, pressureCalls)
			}
		})
	}
	// A memory import plus a local memory is valid with MultiMemory enabled,
	// but remains outside the private scalar pilot independently of decoder policy.
	data := modifySection(stateModule([]fixtureFunc{memoryFixture(0x2d, 0)}, true, 0, 1, true, nil, nil), 7, 5, []byte{1, 1, 0, 1}, false)
	m, err := wasm.DecodeModuleWithFeatures(data, wasm.ValidationFeatures{MultiMemory: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{MultiMemory: true}, 1, wasm.ValidationLimits{}); err != nil {
		t.Fatal(err)
	}
	if _, err := admitStateMode(m, true); !errors.Is(err, ErrUnsupported) {
		t.Fatal("import plus defined memory escaped the gate", err)
	}
}

func TestProductionImportedScalarGlobalAggregateBound(t *testing.T) {
	base := binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x23, 0xff, 1, 0x0b}})
	module := func(count int, defined bool) []byte {
		imports := uleb(nil, uint32(count))
		for i := 0; i < count; i++ {
			imports = append(imports, 1, 'm', 1, 'x', 3, 0x7f, 0)
		}
		data := modifySection(base, 3, 2, imports, false)
		if defined {
			data = modifySection(data, 7, 6, []byte{1, 0x7f, 0, 0x41, 7, 0x0b}, false)
		}
		return data
	}
	image, err := CompileProduction(module(256, false), ProductionOptions{DeferCodeMapping: true})
	if err != nil {
		t.Fatal("supported scalar import boundary rejected", err)
	}
	if err := image.CodeImage.Close(); err != nil {
		t.Fatal(err)
	}
	if image, err := Compile(module(256, false)); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("isolated global import admission widened", image, err)
	}
	for _, data := range [][]byte{module(257, false), module(256, true)} {
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal("invalid bound fixture", err)
		}
		if _, err := admitStateMode(m, true); !errors.Is(err, ErrUnsupported) {
			t.Fatal("aggregate global count escaped state admission", err)
		}
		for _, heap := range []bool{false, true} {
			if image, err := CompileProduction(data, ProductionOptions{DeferCodeMapping: heap}); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("aggregate global count reached image", image, err)
			}
		}
	}
}
