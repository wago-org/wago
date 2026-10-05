//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"testing"
	"unsafe"

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
	base := stateModule(fs, false, 0, 1, true, nil, nil)
	for _, data := range [][]byte{
		stateModule(fs, true, 0, 1, true, nil, nil),
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
