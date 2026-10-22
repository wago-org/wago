package wago

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestOwnedSecondaryMemory64FiniteReservation(t *testing.T) {
	if !SupportedFeatures().IsEnabled(CoreFeatureMemory64 | CoreFeatureMultiMemory) {
		t.Skip("memory64 and multi-memory backend required")
	}
	for _, explicit := range []bool{false, true} {
		name := "no-maximum"
		limits := []byte{0x04, 0x00}
		if explicit {
			name = "maximum-type-limit"
			limits = binary.AppendUvarint([]byte{0x05, 0x00}, 1<<48)
		}
		t.Run(name, func(t *testing.T) {
			c, err := Compile(wasmtest.Module(
				wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x00, 0x00}, limits)),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("secondary", 2, 1))),
			))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			// No memory quota: its exact explicit type overflows byte accounting,
			// while physical instantiation must still reserve a bounded mapping.
			in, err := instantiateCore(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			memory, err := in.ExportedMemory("secondary")
			if err != nil {
				t.Fatal(err)
			}
			if jm := memory.jobMemory(); jm == nil || jm.CurrentPages() != 0 || jm.MaxPages() != 65535 {
				t.Fatalf("secondary reservation = %v; want 0/65535 pages", jm)
			}
			def := c.memoryDef(1)
			if !def.Addr64 || def.HasMax != explicit || explicit && def.Max != 1<<48 {
				t.Fatalf("exact memory type changed: %+v", def)
			}
		})
	}
}

func TestOwnedMemoryReservationPages(t *testing.T) {
	for _, tt := range []struct {
		name string
		def  memoryDef
		want uint64
		bad  bool
	}{
		{"memory32", memoryDef{Min: 1}, 65536, false},
		{"memory64", memoryDef{Addr64: true, Min: 1}, 65535, false},
		{"exact-limit", memoryDef{Addr64: true, Min: 1, HasMax: true, Max: 2}, 2, false},
		{"zero", memoryDef{Addr64: true, HasMax: true}, 0, false},
		{"huge-declared-max", memoryDef{Addr64: true, Min: 1, HasMax: true, Max: 1 << 48}, 65535, false},
		{"maximum-min", memoryDef{Addr64: true, Min: 65535}, 65535, false},
		{"oversized-min", memoryDef{Addr64: true, Min: 65536}, 0, true},
		{"min-exceeds-max", memoryDef{Addr64: true, Min: 2, HasMax: true, Max: 1}, 0, true},
		{"host-conversion-overflow", memoryDef{Addr64: true, Min: 1 << 48}, 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ownedMemoryReservationPages(tt.def)
			if (err != nil) != tt.bad || got != tt.want {
				t.Fatalf("reservation = %d, %v; want %d, error=%t", got, err, tt.want, tt.bad)
			}
		})
	}
}

func TestDeclaredMemoryMaxBytesSeparatesTypeAndReservation(t *testing.T) {
	for _, tt := range []struct {
		name string
		defs []memoryDef
		want uint64
		bad  bool
	}{
		{"mixed-owned", []memoryDef{{}, {Addr64: true}, {Addr64: true}}, (65536 + 65535*2) << 16, false},
		{"import-conservative", []memoryDef{{ImportKey: "p.m", Addr64: true}, {Addr64: true}}, (65536 + 65535) << 16, false},
		{"exact-type", []memoryDef{{}, {Addr64: true, HasMax: true, Max: 1 << 32}}, (65536 + 1<<32) << 16, false},
		{"explicit-zero", []memoryDef{{Addr64: true, HasMax: true}}, 0, false},
		{"type-byte-overflow", []memoryDef{{Addr64: true, HasMax: true, Max: 1 << 48}}, 0, true},
		{"aggregate-overflow", []memoryDef{{Addr64: true, HasMax: true, Max: 1 << 47}, {Addr64: true, HasMax: true, Max: 1 << 47}}, 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiled{memoryDir: &compiledMemoryDirectory{defs: tt.defs}}
			got, err := c.declaredMemoryMaxBytes()
			if (err != nil) != tt.bad || got != tt.want {
				t.Fatalf("declared bytes = %d, %v; want %d, error=%t", got, err, tt.want, tt.bad)
			}
		})
	}
}
