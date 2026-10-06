//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PublicSWARMemoryExportsModule() []byte {
	return rv64PublicSWARMemoryModule(false)
}

func rv64PublicSWARMemoryModule(imported bool) []byte {
	raw := make([]byte, 16)
	binary.LittleEndian.PutUint64(raw, 0x7ff0000000000001)
	binary.LittleEndian.PutUint64(raw[8:], 0x80000000deadbeef)
	global := append([]byte{0xfd, 12}, raw...)
	global = append(global, 0x0b)
	data := append([]byte{1, 1, 16}, raw...)
	var importSection, memorySection []byte
	if imported {
		entry := append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4)
		importSection = wasmtest.Section(2, wasmtest.Vec(entry))
	} else {
		memorySection = wasmtest.Section(5, []byte{1, 1, 1, 4})
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.V128}, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.V128}, []wasm.ValType{wasm.V128, wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}),
		)),
		importSection,
		wasmtest.Section(3, []byte{6, 0, 1, 2, 3, 4, 0}),
		memorySection,
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, true, global))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("load", 0, 0), wasmtest.ExportEntry("store", 0, 1), wasmtest.ExportEntry("size", 0, 2), wasmtest.ExportEntry("grow", 0, 3), wasmtest.ExportEntry("memory", 2, 0), wasmtest.ExportEntry("alias", 2, 0), wasmtest.ExportEntry("state", 0, 4), wasmtest.ExportEntry("init", 0, 5))),
		wasmtest.Section(12, []byte{1}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xfd, 0, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0xfd, 11, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0x3f, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 1, 0x20, 0, 0x40, 0, 0x0b}),
			wasmtest.Code([]byte{0x23, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 0, 0x41, 16, 0xfc, 8, 0, 0, 0x20, 0, 0xfd, 0, 0, 0, 0x0b}),
		)),
		wasmtest.Section(11, data),
	)
}
func TestRV64PublicSWAROwnedMemoryExportsArtifactGrowthAndRetention(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			c, err := Compile(nil, rv64PublicSWARMemoryExportsModule())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if artifact {
				blob, err := c.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := LoadTrustedArtifact(blob)
				if loaded != nil {
					defer loaded.Close()
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
				c = loaded
			}
			producer, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			memory, err := producer.ExportedMemory("memory")
			if err != nil {
				t.Fatal(err)
			}
			alias, err := producer.ExportedMemory("alias")
			if err != nil || alias != memory {
				t.Fatal("memory aliases changed owner identity", err)
			}
			if unknown, err := producer.ExportedMemory("missing"); unknown != nil || err == nil {
				t.Fatal("unknown memory export resolved")
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			payload := []uint64{0x8899aabbccddeeff, 0x7ff0000000000001}
			if _, err := producer.Invoke("store", 0xdeadbeef00000003, payload[0], payload[1]); err != nil {
				t.Fatal(err)
			}
			got, err := producer.Invoke("load", 0xaabbccdd00000003)
			if err != nil || !slices.Equal(got, payload) {
				t.Fatal("unaligned exported vector memory", got, err)
			}
			if binary.LittleEndian.Uint64(memory.UnsafeBytes()[3:]) != payload[0] || binary.LittleEndian.Uint64(memory.UnsafeBytes()[11:]) != payload[1] {
				t.Fatal("host exported view lost raw lanes")
			}
			// The complete-width guard precedes either half of the split store.
			tail := slices.Clone(memory.UnsafeBytes()[65520:65536])
			got, err = producer.Invoke("store", 0xdeadbeef0000fff1, payload[0], payload[1])
			var trap *coreruntime.TrapError
			if !errors.As(err, &trap) || trap.Code != coreruntime.TrapLinMemOutOfBounds || got != nil || !slices.Equal(tail, memory.UnsafeBytes()[65520:65536]) {
				t.Fatal("exported split store changed memory on trap", got, err)
			}
			if _, err := producer.Invoke("store", 0xdeadbeef0000fff0, payload[0], payload[1]); err != nil {
				t.Fatal("last full-width access", err)
			}
			got, err = producer.Invoke("grow", 0xdeadbeef00000001, payload[0], payload[1])
			if err != nil || !slices.Equal(got, []uint64{payload[0], payload[1], 1}) || len(memory.UnsafeBytes()) != 2*65536 {
				t.Fatal("mixed-vector grow or exported host view", got, err)
			}
			if !slices.Equal(memory.UnsafeBytes()[65536:65552], make([]byte, 16)) {
				t.Fatal("new backing was not zeroed")
			}
			got, err = producer.Invoke("load", 0xdeadbeef00010000)
			if err != nil || !slices.Equal(got, []uint64{0, 0}) {
				t.Fatal("grown vector access", got, err)
			}

			consumerCode := importedMemoryCompile(t, true, 20, false, false, 0)
			a, err := Instantiate(consumerCode, NewImports().Memory("env", "memory", memory))
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			b, err := Instantiate(consumerCode, NewImports().Memory("env", "memory", memory))
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			if _, err := producer.Invoke("store", 0, payload[0], payload[1]); err != nil {
				t.Fatal(err)
			}
			importedMemoryInvoke(t, a, "init", 0xff, 0)
			if _, err := a.Invoke("write"); err != nil {
				t.Fatal(err)
			}
			got, err = producer.Invoke("load", 0)
			if err != nil || !slices.Equal(got, []uint64{payload[0]&^0xff | 0x23, payload[1]}) {
				t.Fatal("scalar importer changed vector neighbor lanes", got, err)
			}
			// A scalar tenant has its own global directory and no data directory.
			// Restore the producer's private V128 global and passive segment
			// when re-entering through the same shared JobMemory.
			importedMemoryInvoke(t, a, "bump", 21)
			state := []uint64{0x7ff0000000000001, 0x80000000deadbeef}
			got, err = producer.Invoke("state")
			if err != nil || !slices.Equal(got, state) {
				t.Fatal("shared scalar activation displaced private vector global", got, err)
			}
			importedMemoryInvoke(t, b, "bump", 21)
			got, err = producer.Invoke("init", 0xdeadbeef00000065)
			if err != nil || !slices.Equal(got, state) {
				t.Fatal("shared scalar activation displaced private passive data", got, err)
			}
			importedMemoryInvoke(t, a, "grow", 2, 1)
			got, err = producer.Invoke("size")
			if err != nil || !slices.Equal(got, []uint64{3}) {
				t.Fatal("SIMD producer did not see scalar importer growth", got, err)
			}
			got, err = producer.Invoke("load", 0xdeadbeef00020000)
			if err != nil || !slices.Equal(got, []uint64{0, 0}) {
				t.Fatal("foreign-grown vector access", got, err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			if memory.jobMemory() == nil || memory.UnsafeBytes() != nil {
				t.Fatal("logical producer close did not preserve borrower storage and hide host view")
			}
			if late, err := Instantiate(consumerCode, NewImports().Memory("env", "memory", memory)); late != nil || err == nil {
				t.Fatal("closed SIMD owner accepted a new borrower")
			}
			importedMemoryInvoke(t, a, "init", 0x23, 0)
			importedMemoryInvoke(t, b, "init", 0x23, 0)
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			importedMemoryInvoke(t, b, "grow", 3, 1)
			importedMemoryInvoke(t, b, "size", 4)
			if err := b.Close(); err != nil {
				t.Fatal(err)
			}
			if memory.jobMemory() != nil {
				t.Fatal("last scalar borrower retained retired SIMD memory owner")
			}
		})
	}
}
