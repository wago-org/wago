//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64StorageFreeCallableAfterDependencyTransfer(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			a := rv64NativeTokenInstantiate(t, rt, rv64NativeTailCompile(t, "storage-free-transfer-provider", prefinalizerTableMemorySource(false), artifact), nil)
			defer a.Close()
			memory, err := a.ExportedMemory("memory")
			if err != nil {
				t.Fatal(err)
			}
			table, err := a.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			b := rv64NativeTokenInstantiate(t, rt, rv64NativeTailCompile(t, "storage-free-transfer-callee", frozenPrefinalizerSource(t), artifact), NewImports().Memory("env", "memory", memory).Table("env", "table", table))
			defer b.Close()
			target, err := b.ExportedFunc("safe")
			if err != nil {
				t.Fatal(err)
			}
			imp := append(append(wasmtest.Name("env"), wasmtest.Name("safe")...), 0, 0)
			source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))))
			caller := rv64NativeTokenInstantiate(t, rt, rv64NativeTailCompile(t, "storage-free-transfer-caller", source, artifact), NewImports().Function("env", "safe", target))
			defer caller.Close()
			a.Close()
			b.Close()
			if !a.resourcesClosed || b.resourcesClosed {
				t.Fatal("storage-free transfer did not release only unused storage")
			}
			got, err := caller.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != 17 {
				t.Fatalf("retained storage-free callable=%v,%v", got, err)
			}
		})
	}
}

func TestRV64StorageFreeBodyProof(t *testing.T) {
	source := frozenPrefinalizerSource(t)
	for _, tc := range []struct {
		name string
		body []byte
		want bool
	}{
		{"constant", []byte{0x41, 17, 0x0b}, true},
		{"numeric", []byte{0x41, 17, 0x41, 1, 0x6a, 0x0b}, true},
		{"memory", []byte{0x41, 0, 0x28, 2, 0, 0x0b}, false},
		{"table", []byte{0x41, 0, 0x25, 0, 0x1a, 0x41, 17, 0x0b}, false},
		{"global", []byte{0x23, 0, 0x1a, 0x41, 17, 0x0b}, false},
		{"local call", []byte{0x10, 0, 0x0b}, false},
		{"indirect call", []byte{0x41, 0, 0x11, 0, 0, 0x0b}, false},
		{"reference", []byte{0xd2, 0, 0x1a, 0x41, 17, 0x0b}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := rv64ReplaceTableStageSection(t, source, 10, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tc.body))))
			m, err := wasm.DecodeModule(input)
			if err != nil {
				t.Fatal(err)
			}
			if err = wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{GCConstExpr: true, ExtendedConstGlobals: true, MultiMemory: true}, 2, wasm.ValidationLimits{}); err != nil {
				t.Fatal(err)
			}
			if got := rv64StorageFreeModule(m); got != tc.want {
				t.Fatalf("proof=%t; want %t", got, tc.want)
			}
		})
	}
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("callback")...), 0, 0)
	callbackSource := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))))
	m, err := wasm.DecodeModule(callbackSource)
	if err != nil {
		t.Fatal(err)
	}
	if rv64StorageFreeModule(m) {
		t.Fatal("host import received storage-free proof")
	}
	for _, tc := range []struct {
		name     string
		sections [][]byte
	}{
		{"GC initializer", [][]byte{
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 0, 1, 0x7f})),
			wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 0, 1))),
			wasmtest.Section(3, []byte{1, 1}),
			wasmtest.Section(6, wasmtest.Vec([]byte{0x64, 0, 0, 0x41, 1, 0xfb, 0, 0, 0x0b})),
		}},
		{"imported GC global", [][]byte{
			wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})),
			wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("root")...), 3, 0x6e, 0))),
			wasmtest.Section(3, []byte{1, 0}),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sections := append(tc.sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 17, 0x0b}))))
			m, err := wasm.DecodeModule(wasmtest.Module(sections...))
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{GCConstExpr: true, ExtendedConstGlobals: true}, 2, wasm.ValidationLimits{}); err != nil {
				t.Fatal(err)
			}
			if rv64StorageFreeModule(m) {
				t.Fatal("collector state received storage-free proof")
			}
		})
	}
	typ := reflect.TypeOf(railshotTailCompiledState{})
	var fields []reflect.StructField
	for i := 0; i < typ.NumField(); i++ {
		if field := typ.Field(i); field.Name != "storageFreeBodies" {
			fields = append(fields, field)
		}
	}
	if got, want := unsafe.Sizeof(railshotTailCompiledState{}), reflect.StructOf(fields).Size(); got != want {
		t.Fatalf("RV cold-state size=%d; before proof=%d", got, want)
	}
	t.Logf("RV cold-state size=%dB; proof adds 0B", typ.Size())
}

func TestRV64StorageFreeArtifactProof(t *testing.T) {
	c, err := NewRuntimeConfig().Compile(frozenPrefinalizerSource(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !rv64StorageFreeCallable(c) {
		t.Fatal("source proof unavailable")
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.LastIndex(blob, rv64ExecutionContractMarker[:])
	version := byte(3)
	if len(c.rv64ExecutionTableCapacities()) != 0 {
		version = 4
	}
	if at < 0 || blob[at+8] != version || blob[at+9] != 1 {
		t.Fatal("expected explicit version3/4 proof")
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if !rv64StorageFreeCallable(loaded) {
		t.Fatal("loaded proof unavailable")
	}
	for _, tc := range []struct {
		name   string
		offset int
		value  byte
	}{{"flags zero", 9, 0}, {"flags unknown", 9, 2}, {"version unknown", 8, 5}, {"version capacity shape", 8, 7 - version}, {"digest", len(blob) - at - 1, blob[len(blob)-1] ^ 1}} {
		t.Run(tc.name, func(t *testing.T) {
			bad := bytes.Clone(blob)
			bad[at+tc.offset] = tc.value
			if broken, e := LoadTrustedArtifact(bad); e == nil {
				broken.Close()
				t.Fatal("invalid proof admitted")
			}
		})
	}
	// Versions1/2 remain supported and carry no storage-free authority.
	metadata, _, err := encodeCompiledMetadataBody(c, false, false)
	if err != nil {
		t.Fatal(err)
	}
	capacities := c.rv64ExecutionTableCapacities()
	digest := rv64ExecutionDigest(c.code, metadata, c.syncHostSlots, capacities...)
	metadata = append(metadata, rv64ExecutionContractMarker[:]...)
	oldVersion := byte(1)
	if len(capacities) != 0 {
		oldVersion = 2
	}
	metadata = append(metadata, oldVersion, byte(c.syncHostSlots), byte(c.syncHostSlots>>8))
	if len(capacities) != 0 {
		metadata = binary.LittleEndian.AppendUint32(metadata, uint32(len(capacities)))
		for _, capacity := range capacities {
			metadata = binary.LittleEndian.AppendUint32(metadata, capacity)
		}
	}
	metadata = append(metadata, digest[:]...)
	w := compiledWriter{}
	w.appendString(wagoMagic)
	w.u8(wagoVersion)
	w.u8(compiledHeader)
	w.section(compiledSectionCode, c.code)
	w.section(compiledSectionMetadata, metadata)
	legacy, err := LoadTrustedArtifact(w.buf)
	if err != nil {
		t.Fatal("old contract", err)
	}
	defer legacy.Close()
	if rv64StorageFreeProof(legacy) || rv64StorageFreeCallable(legacy) {
		t.Fatal("old contract gained storage-free authority")
	}
	forged := bytes.Clone(metadata)
	pos := bytes.LastIndex(forged, rv64ExecutionContractMarker[:])
	forged[pos+8] = oldVersion + 2
	forged = append(forged[:pos+9], append([]byte{1}, forged[pos+9:]...)...)
	fw := compiledWriter{}
	fw.appendString(wagoMagic)
	fw.u8(wagoVersion)
	fw.u8(compiledHeader)
	fw.section(compiledSectionCode, c.code)
	fw.section(compiledSectionMetadata, forged)
	if broken, e := LoadTrustedArtifact(fw.buf); e == nil {
		broken.Close()
		t.Fatal("new proof with old digest admitted")
	}
	t.Log("storage-free contract adds one byte; old contracts retain dependencies")
}
