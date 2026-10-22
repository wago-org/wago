//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"runtime"
	"slices"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func rv64PublicSWARStateImportsCompile(t *testing.T, position int, artifact bool, quota uint32) *Compiled {
	t.Helper()
	c, err := Compile(NewRuntimeConfig().WithMemoryLimitPages(quota), rv64PublicSWARGlobalStateModule(2, position))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if !artifact {
		return c
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if loaded != nil {
		t.Cleanup(func() { _ = loaded.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	return loaded
}

func rv64PublicSWARBorrowState(t *testing.T, producer *Instance, memory *Memory) (*Imports, []*Global) {
	t.Helper()
	imports := NewImports().Memory("env", "memory", memory)
	objects := make([]*Global, 6)
	for i := range objects {
		g, err := producer.ExportedGlobalObject("g" + string(rune('0'+i)))
		if err != nil {
			t.Fatal(err)
		}
		objects[i] = g
		imports.Global("env", "g"+string(rune('0'+i)), g)
	}
	return imports, objects
}

func TestRV64PublicSWARCombinedStateImportsContextQuotaAndRetirement(t *testing.T) {
	for _, position := range []int{0, 3, 7} {
		for _, memoryFirst := range []bool{true, false} {
			name := string(rune('0'+position)) + map[bool]string{true: "-memory-close-first", false: "-globals-close-first"}[memoryFirst]
			t.Run(name, func(t *testing.T) {
				memoryProducer, memory := importedMemoryProvider(t)
				globalCode, err := Compile(nil, rv64PublicSWARGlobalExportsModule())
				if err != nil {
					t.Fatal(err)
				}
				defer globalCode.Close()
				globalProducer, err := Instantiate(globalCode)
				if err != nil {
					t.Fatal(err)
				}
				defer globalProducer.Close()
				imports, objects := rv64PublicSWARBorrowState(t, globalProducer, memory)
				codes := []*Compiled{rv64PublicSWARStateImportsCompile(t, position, false, 2), rv64PublicSWARStateImportsCompile(t, position, true, 0)}
				var consumers []*Instance
				for _, c := range codes {
					in, err := Instantiate(c, imports)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = in.Close() })
					consumers = append(consumers, in)
					if forwarded, err := in.ExportedMemory("memory"); err != nil || forwarded != memory {
						t.Fatal("combined memory identity", err)
					}
					if forwarded, err := in.ExportedGlobalObject("g6"); err != nil || forwarded != objects[4] {
						t.Fatal("combined global identity", err)
					}
				}
				a, b := consumers[0], consumers[1]
				bits := []uint64{0xffffffff, 0x8000000000000001, 0x7f800001, 0x7ff0000000000001, 0x8899aabbccddeeff, 0x0807060504030201, 0x7ff0000000000001, 0x80000000deadbeef}
				importedGlobalInvoke(t, a, "get", rv64PublicSWARImportedGlobalWant(bits, V128{}))
				args := []uint64{0xdeadbeef80000000, 9, 0xdeadbeefff800001, 0xfff0000000000001, 0x80000000deadbeef, 0x7ff0000000000001}
				importedGlobalInvoke(t, a, "set", rv64PublicSWARImportedGlobalWant(bits, V128{}), args...)
				bits = append([]uint64{0x80000000, 9, 0xff800001, args[3], args[4], args[5]}, bits[6:]...)
				importedGlobalInvoke(t, globalProducer, "get", bits)
				importedGlobalInvoke(t, b, "get", rv64PublicSWARImportedGlobalWant(bits, V128{}))
				private := rv64PublicVector(0xffffffffffffffff, 0xff8000017f800001)
				importedGlobalInvoke(t, a, "private", []uint64{0, 0}, 0xffffffffffffffff, 0xff8000017f800001)
				payload := []uint64{0x7ff0000000000042, 0x8000000000000000}
				importedGlobalInvoke(t, a, "store", bits[4:6], 0xdeadbeef00000003, payload[0], payload[1])
				importedGlobalInvoke(t, b, "load", payload, 0xdeadbeef00000003)
				importedMemoryInvoke(t, memoryProducer, "bump", 11)
				importedGlobalInvoke(t, a, "get", rv64PublicSWARImportedGlobalWant(bits, private))
				// Both directories must survive the runtime memory.grow transition with a
				// live imported V128 result, including delta-zero above this importer's quota.
				for _, test := range []struct {
					in          *Instance
					delta, want uint64
				}{{a, 1, 1}, {a, 1, 0xffffffff}, {b, 1, 2}, {a, 0, 3}, {a, 1, 0xffffffff}} {
					importedGlobalInvoke(t, test.in, "grow", []uint64{bits[4], bits[5], test.want}, 0xaabbccdd00000000|test.delta)
				}
				end := len(memory.UnsafeBytes())
				tail := slices.Clone(memory.UnsafeBytes()[end-16:])
				got, err := a.Invoke("store", uint64(end-15)|0xdeadbeef00000000, payload[0], payload[1])
				var trap *coreruntime.TrapError
				if !errors.As(err, &trap) || trap.Code != coreruntime.TrapLinMemOutOfBounds || got != nil || !slices.Equal(tail, memory.UnsafeBytes()[end-16:]) {
					t.Fatal("combined state pair preflight", got, err)
				}
				probe, err := Instantiate(codes[1], imports)
				if err != nil {
					t.Fatal("unlimited code must still admit the open state owners", err)
				}
				if err := probe.Close(); err != nil {
					t.Fatal(err)
				}
				if memoryFirst {
					err = memoryProducer.Close()
				} else {
					err = globalProducer.Close()
				}
				if err != nil {
					t.Fatal(err)
				}
				// Keep code and the second producer open: this must reject the first retired
				// state owner and unwind any attachments made before that failure.
				if late, err := Instantiate(codes[1], imports); late != nil || err == nil {
					t.Fatal("combined import attached a retired first owner")
				}
				importedGlobalInvoke(t, a, "get", rv64PublicSWARImportedGlobalWant(bits, private))
				importedGlobalInvoke(t, b, "load", payload, 3)
				if memoryFirst {
					err = globalProducer.Close()
				} else {
					err = memoryProducer.Close()
				}
				if err != nil {
					t.Fatal(err)
				}
				for _, c := range codes {
					if err := c.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if err := globalCode.Close(); err != nil {
					t.Fatal(err)
				}
				runtime.GC()
				if memory.UnsafeBytes() != nil || memory.jobMemory() == nil || !globalProducer.hasPhysicalResources() || objects[4].GetV128() != (V128{}) {
					t.Fatal("combined retirement lost roots or kept host access")
				}
				importedGlobalInvoke(t, a, "get", rv64PublicSWARImportedGlobalWant(bits, private))
				importedGlobalInvoke(t, b, "grow", []uint64{bits[4], bits[5], 3}, 0xdeadbeef00000000)
				if err := a.Close(); err != nil {
					t.Fatal(err)
				}
				if memory.jobMemory() == nil || !globalProducer.hasPhysicalResources() {
					t.Fatal("first combined borrower released state owners")
				}
				importedGlobalInvoke(t, b, "store", bits[4:6], 3, payload[1], payload[0])
				importedGlobalInvoke(t, b, "load", []uint64{payload[1], payload[0]}, 3)
				if err := b.Close(); err != nil {
					t.Fatal(err)
				}
				if memory.jobMemory() != nil || globalProducer.hasPhysicalResources() {
					t.Fatal("last borrower or failed attachment retained state owners")
				}
				for _, g := range objects {
					if len(g.cell) != 0 {
						t.Fatal("released global producer cell remains")
					}
				}
			})
		}
	}
}

func TestRV64PublicSWARCombinedStateImportsRetainOneProducer(t *testing.T) {
	c := rv64PublicSWARGlobalImportCompile(t, false, true)
	bits := []uint64{0xffffffff, 9, 0x7f800001, 0x7ff0000000000001, 0x8899aabbccddeeff, 0x0807060504030201, 0x7ff0000000000001, 0x80000000deadbeef}
	values := NewImports()
	for i, typ := range []ValType{ValI32, ValI64, ValF32, ValF64, ValV128, ValV128} {
		value := GlobalImport{Type: typ, Mutable: i != 5}
		if i < 4 {
			value.Bits = bits[i]
		} else {
			offset := 4 + (i-4)*2
			value.V128 = rv64PublicVector(bits[offset], bits[offset+1])
		}
		values.Global("env", "g"+string(rune('0'+i)), value)
	}
	producer, err := Instantiate(c, values)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	memory, err := producer.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(memory.UnsafeBytes()[3:], bits[4])
	binary.LittleEndian.PutUint64(memory.UnsafeBytes()[11:], bits[5])
	imports, objects := rv64PublicSWARBorrowState(t, producer, memory)
	var borrowers []*Instance
	for i, position := range []int{0, 7} {
		code := rv64PublicSWARStateImportsCompile(t, position, i == 1, 0)
		in, err := Instantiate(code, imports)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = in.Close() })
		borrowers = append(borrowers, in)
		if err := code.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	if !producer.hasPhysicalResources() || memory.UnsafeBytes() != nil || objects[4].GetV128() != (V128{}) {
		t.Fatal("shared owner retirement")
	}
	importedGlobalInvoke(t, borrowers[0], "get", rv64PublicSWARImportedGlobalWant(bits, V128{}))
	importedGlobalInvoke(t, borrowers[1], "load", bits[4:6], 0xdeadbeef00000003)
	importedGlobalInvoke(t, borrowers[0], "grow", []uint64{bits[4], bits[5], 1}, 1)
	importedGlobalInvoke(t, borrowers[1], "grow", []uint64{bits[4], bits[5], 0xffffffff}, 1)
	if err := borrowers[0].Close(); err != nil {
		t.Fatal(err)
	}
	if !producer.hasPhysicalResources() {
		t.Fatal("deduplicated first borrower prematurely released shared owner")
	}
	importedGlobalInvoke(t, borrowers[1], "get", rv64PublicSWARImportedGlobalWant(bits, V128{}))
	if err := borrowers[1].Close(); err != nil {
		t.Fatal(err)
	}
	if producer.hasPhysicalResources() || memory.jobMemory() != nil {
		t.Fatal("combined same-owner attachments leaked arena")
	}
	for _, g := range objects {
		if len(g.cell) != 0 {
			t.Fatal("final same-owner global cell still names storage")
		}
	}
}
