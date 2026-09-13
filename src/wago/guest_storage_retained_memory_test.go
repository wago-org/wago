//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func guestStorageMemoryConsumer(memoryCount int) []byte {
	imports := [][]byte{append(append(wasmtest.Name("host"), wasmtest.Name("inspect")...), 0, 0)}
	for i := 0; i < memoryCount; i++ {
		entry := append(wasmtest.Name("env"), wasmtest.Name(fmt.Sprint("memory", i))...)
		imports = append(imports, append(entry, 2, 1, 1, 1))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
}

func TestHostGuestStorageRetainsImportedMemory(t *testing.T) {
	for _, memoryCount := range []int{1, 2} {
		t.Run(fmt.Sprint(memoryCount), func(t *testing.T) {
			ownerCode := stagedMultiMemoryCompile(t, wasmtest.Module(
				wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0))),
			))
			owner, err := Instantiate(ownerCode)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			memory, err := owner.ExportedMemory("memory")
			if err != nil {
				t.Fatal(err)
			}
			if !owner.Write(32, []byte{17}) {
				t.Fatal("initialize owner memory")
			}
			calls := 0
			var retained GuestStorage
			imports := Imports{"host.inspect": HostFunc(func(module HostModule, _, _ []uint64) {
				calls++
				err := module.(GuestStorageHostModule).WithGuestStorage(func(storage GuestStorage) error {
					retained = storage
					for index := uint32(0); index < uint32(memoryCount); index++ {
						info, err := storage.MemoryInfo(index)
						if err != nil {
							return err
						}
						if info.AddressType != GuestMemory32 || info.ByteLength != 65536 {
							return fmt.Errorf("memory %d info = %+v", index, info)
						}
						buf, err := storage.MemoryRange(index, 32, 1, GuestStorageWrite)
						if err != nil {
							return err
						}
						want := byte(17 + index)
						if buf[0] != want {
							return fmt.Errorf("memory %d byte = %d, want %d", index, buf[0], want)
						}
						buf[0]++
						read, err := storage.MemoryRange(index, 32, 1, GuestStorageRead)
						if err != nil {
							return err
						}
						if read[0] != want+1 {
							return fmt.Errorf("memory %d write not visible", index)
						}
						if _, err := storage.MemoryRange(index, info.ByteLength, 1, GuestStorageRead); err == nil {
							return fmt.Errorf("memory %d accepted an out-of-bounds range", index)
						}
					}
					return nil
				})
				if err != nil {
					panic(HostTrap{Err: err})
				}
			})}
			for index := 0; index < memoryCount; index++ {
				imports[fmt.Sprint("env.memory", index)] = memory
			}
			consumerCode := stagedMultiMemoryCompile(t, guestStorageMemoryConsumer(memoryCount))
			consumer, err := Instantiate(consumerCode, imports)
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			if err := owner.Close(); err != nil {
				t.Fatal(err)
			}
			if owner.resourcesClosed {
				t.Fatal("owner released before consumer")
			}
			if memory.UnsafeBytes() != nil {
				t.Fatal("public memory view remains available after owner close")
			}
			if _, err := consumer.Invoke("run"); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("callback count = %d", calls)
			}
			if _, err := retained.MemoryInfo(0); err == nil {
				t.Fatal("expired storage view remains available")
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if !owner.resourcesClosed {
				t.Fatal("owner retained after final consumer close")
			}
		})
	}
}

func BenchmarkGuestStorageMemoryAccess(b *testing.B) {
	compiled := stagedMultiMemoryCompile(b, guestStorageMemoryConsumer(1))
	memory, err := NewMemory(1, 1)
	if err != nil {
		b.Fatal(err)
	}
	defer memory.Close()
	host := HostFunc(func(module HostModule, _, _ []uint64) {
		if err := module.(GuestStorageHostModule).WithGuestStorage(func(storage GuestStorage) error {
			if _, err := storage.MemoryInfo(0); err != nil {
				return err
			}
			buf, err := storage.MemoryRange(0, 32, 8, GuestStorageWrite)
			if err == nil {
				buf[0]++
			}
			return err
		}); err != nil {
			panic(HostTrap{Err: err})
		}
	})
	in, err := Instantiate(compiled, Imports{"env.memory0": memory, "host.inspect": host})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("run"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGuestStorageBorrowMemoryAccess excludes invocation and host dispatch.
func BenchmarkGuestStorageBorrowMemoryAccess(b *testing.B) {
	compiled := stagedMultiMemoryCompile(b, guestStorageMemoryConsumer(1))
	memory, err := NewMemory(1, 1)
	if err != nil {
		b.Fatal(err)
	}
	defer memory.Close()
	in, err := Instantiate(compiled, Imports{
		"env.memory0":  memory,
		"host.inspect": HostFunc(func(HostModule, []uint64, []uint64) {}),
	})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	host := in.beginHostCallScope()
	defer host.scope.end(host.generation, host.parentGeneration)
	access := func(storage GuestStorage) error {
		if _, err := storage.MemoryInfo(0); err != nil {
			return err
		}
		buf, err := storage.MemoryRange(0, 32, 8, GuestStorageWrite)
		if err == nil {
			buf[0]++
		}
		return err
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := host.WithGuestStorage(access); err != nil {
			b.Fatal(err)
		}
	}
}
