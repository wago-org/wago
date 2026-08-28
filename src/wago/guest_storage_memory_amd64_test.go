//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"testing"
)

func callbackScopedGuestStorageModule(in *Instance) instanceHostModule {
	return in.beginHostCallScopeReservedWithID(newInvocationID(), nil)
}

func TestHostGuestStorageIndexedMemory(t *testing.T) {
	compiled := stagedMultiMemoryCompile(t, localMultiMemoryExecModule())
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	host := callbackScopedGuestStorageModule(in)
	defer host.scope.end(host.generation, host.parentGeneration)
	if err := host.WithGuestStorage(func(storage GuestStorage) error {
		m0, err := storage.MemoryInfo(0)
		if err != nil {
			return err
		}
		m1, err := storage.MemoryInfo(1)
		if err != nil {
			return err
		}
		if m0.AddressType != GuestMemory32 || m0.ByteLength != 65536 {
			return &guestStorageTestError{"memory 0 metadata"}
		}
		if m1.AddressType != GuestMemory32 || m1.ByteLength != 2*65536 {
			return &guestStorageTestError{"memory 1 metadata"}
		}
		buf, err := storage.MemoryRange(1, 32, 4, GuestStorageWrite)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint32(buf, 0x12345678)
		if _, err := storage.MemoryInfo(2); err == nil {
			return &guestStorageTestError{"invalid memory index accepted"}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	got, err := in.Invoke("load1", I32(32))
	if err != nil || len(got) != 1 || uint32(got[0]) != 0x12345678 {
		t.Fatalf("memory 1 load after host write = %v, %v", got, err)
	}
	got, err = in.Invoke("load0", I32(32))
	if err != nil || len(got) != 1 || uint32(got[0]) != 0 {
		t.Fatalf("memory 0 changed by memory 1 host write = %v, %v", got, err)
	}
}

func TestHostGuestStorageMemory64MetadataAndRange(t *testing.T) {
	compiled, err := compileStagedMemory64(boundedMemory64Module(2))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	host := callbackScopedGuestStorageModule(in)
	defer host.scope.end(host.generation, host.parentGeneration)
	if err := host.WithGuestStorage(func(storage GuestStorage) error {
		info, err := storage.MemoryInfo(0)
		if err != nil {
			return err
		}
		if info.AddressType != GuestMemory64 || info.ByteLength != 65536 {
			return &guestStorageTestError{"memory64 metadata"}
		}
		buf, err := storage.MemoryRange(0, 64, 8, GuestStorageWrite)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint64(buf, 0x0102030405060708)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	memory, err := in.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint64(memory.UnsafeBytes()[64:72]); got != 0x0102030405060708 {
		t.Fatalf("memory64 host write = %#x", got)
	}
}
