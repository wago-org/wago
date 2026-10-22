//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func swarHostCodeBuffers(t *testing.T, image *swarModuleImage) (*coreruntime.CodeBuffer, *coreruntime.CodeBuffer) {
	t.Helper()
	return swarTableCodeBuffer(t, image), swarTableCodeBuffer(t, &swarModuleImage{code: image.hostCode})
}

func TestSWARHostModulePolicySnapshotSourceIndicesAndBounds(t *testing.T) {
	for _, count := range []int{1, 65, 256} {
		for _, slots := range []int{0, 4, 65, 126} {
			source, _, classes := swarHostSource(count, count-1, slots, false)
			image, err := compileSWARHostModule(source, emissionMode{production: true, interruptible: true})
			if err != nil || len(image.hosts) != count || len(image.hostEntries) != count || len(image.hostCode) == 0 || len(image.functions) != 1 || image.functions[0].mode.functionIndex != uint32(count) || image.functions[0].DirectCalls != 0 || len(image.code)+len(image.hostCode) > MaxCodeBytes {
				t.Fatal("private host module", count, slots, err)
			}
			clear(source)
			for _, sig := range image.hosts {
				if len(sig.params) != slots || len(sig.results) != slots {
					t.Fatal("host signature snapshot shape")
				}
				for i, class := range classes {
					if wasm.MustEncodeValType(sig.params[i]) != class || wasm.MustEncodeValType(sig.results[i]) != class {
						t.Fatal("host signature snapshot aliases source")
					}
				}
			}
		}
	}
	source, fixtures, _ := swarHostSource(256, 255, 65, true)
	image, err := compileSWARHostModule(source, emissionMode{production: true, interruptible: true})
	if err != nil || image.functions[0].DirectCalls != 1 || image.functions[0].mode.functionIndex != 256 || image.functions[1].mode.functionIndex != 257 {
		t.Fatal("host defined relocation/full source indices", err)
	}
	for _, compile := range []func([]byte, emissionMode) (*swarModuleImage, error){compileSWARModule, compileSWARTableModule} {
		if image, err := compile(source, emissionMode{production: true, interruptible: true}); image != nil || err == nil {
			t.Fatal("default/table import policy widened", err)
		}
	}
	if image, err := compileSWARHostModule(source, emissionMode{production: true}); image != nil || err == nil {
		t.Fatal("whole-module host omitted park/interruption policy", err)
	}
	for _, shape := range []string{"count", "table-empty", "elements-empty", "reexport", "resource", "start"} {
		bad := source
		switch shape {
		case "count":
			bad, _, _ = swarHostSource(257, 256, 4, false)
		case "table-empty":
			bad = modifySection(source, 7, 4, []byte{0}, false)
		case "elements-empty":
			bad = modifySection(source, 10, 9, []byte{0}, false)
		case "reexport":
			bad = modifySection(source, 7, 7, []byte{1, 1, 'a', 0, 0}, true)
		case "resource":
			bad = hostImportResourceModule(1, 0, []byte{0x7f}, false)
		case "start":
			bad = modifySection(hostImportModule(1, 0, nil, false), 10, 8, []byte{1}, false)
		}
		if image, err := compileSWARHostModule(bad, emissionMode{production: true, interruptible: true}); image != nil || err == nil {
			t.Fatal("private host unsupported shape", shape, err)
		}
	}
	// Normal frontend validation accepts this hidden tail call to the same
	// defined signature; whole-module RV64 admission must still reject it.
	fixtures[1].body = append(uleb([]byte{0, 0x12}, 257), 0x0b)
	code := []byte{2}
	for _, f := range fixtures {
		body := append(bytes.Clone(f.locals), f.body...)
		code = append(uleb(code, uint32(len(body))), body...)
	}
	bad := modifySection(source, 10, 10, code, true)
	swarValidatedHostSource(t, bad)
	if image, err := compileSWARHostModule(bad, emissionMode{production: true, interruptible: true}); image != nil || err == nil {
		t.Fatal("hidden normally validated unsupported function admitted", err)
	}
	// A bounded source can still have too many aggregate functions.
	fs := make([]fixtureFunc, 769)
	for i := range fs {
		fs[i] = fixtureFunc{body: []byte{0x0b}, unexported: i != 0}
	}
	bad = binaryModule(fs...)
	imports := uleb(nil, 256)
	for range 256 {
		imports = append(imports, 1, 'm', 1, 'h', 0, 0)
	}
	bad = modifySection(bad, 3, 2, imports, false)
	bad = modifySection(bad, 7, 7, uleb([]byte{1, 1, 'a', 0}, 256), true)
	swarValidatedHostSource(t, bad)
	if image, err := compileSWARHostModule(bad, emissionMode{production: true, interruptible: true}); image != nil || err == nil {
		t.Fatal("aggregate1025 functions admitted", err)
	}
}

func TestSWARHostOwnerSharedCodeCanonicalDispatchAndCleanupRetry(t *testing.T) {
	source, _, _ := swarHostSource(256, 255, 65, true)
	image, err := compileSWARHostModule(source, emissionMode{production: true, interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	guest, thunks := swarHostCodeBuffers(t, image)
	var owners [2]*swarHostOwner
	var identities [2]uint64
	for i := range owners {
		job, err := coreruntime.NewJobMemory(0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := job.Close(); err != nil {
				t.Error(err)
			}
		})
		owner, err := image.instantiateHosts(job, guest, thunks)
		if err != nil {
			t.Fatal(err)
		}
		owners[i] = owner
		t.Cleanup(func() {
			if err := owner.Close(); err != nil {
				t.Error(err)
			}
		})
		rows := swarTableOwnedBytes(owner.dispatch, 256*coreruntime.ImportDispatchEntryBytes)
		header := swarTableOwnedBytes(owner.directory, coreruntime.FuncRefDescBytes)
		identities[i] = binary.LittleEndian.Uint64(header[coreruntime.FuncRefContextOffset:])
		if identities[i] == 0 || !bytes.Equal(header[:32], make([]byte, 32)) || job.CaptureInstanceContext().ImportDispatch != owner.dispatch || job.CaptureInstanceContext().FuncRefDescPtr != owner.directory {
			t.Fatal("host context/header/binding")
		}
		for n, entry := range image.hostEntries {
			row := rows[n*32 : (n+1)*32]
			if binary.LittleEndian.Uint64(row) != uint64(thunks.Base())+uint64(entry) || binary.LittleEndian.Uint64(row[8:]) != uint64(job.LinMemBase()) || binary.LittleEndian.Uint64(row[16:]) != identities[i] || binary.LittleEndian.Uint64(row[24:]) != identities[i] {
				t.Fatal("complete canonical dispatch row", i, n)
			}
		}
		if other, err := image.instantiateHosts(job, guest, thunks); other != nil || err == nil {
			t.Fatal("host bound job replaced", err)
		}
	}
	if identities[0] == identities[1] {
		t.Fatal("host context shared across jobs")
	}
	owner := owners[0]
	for _, replace := range []func(uintptr){owner.job.SetImportDispatchPtr, owner.job.SetFuncRefDesc} {
		replace(1)
		before := owner.job.CaptureInstanceContext()
		if err := owner.Close(); err == nil || owner.arena == nil || owner.job.CaptureInstanceContext() != before {
			t.Fatal("host replaced binding partially detached", err)
		}
		owner.job.SetImportDispatchPtr(owner.dispatch)
		owner.job.SetFuncRefDesc(owner.directory)
	}
	failure := errors.New("injected host owner release failure")
	job := owner.job
	if err := owner.closeWith(func(*coreruntime.Arena) error { return failure }); !errors.Is(err, failure) || owner.arena == nil || owner.job != job || owner.guest != guest || owner.thunks != thunks || job.CaptureInstanceContext().ImportDispatch != 0 || job.CaptureInstanceContext().FuncRefDescPtr != 0 {
		t.Fatal("host detached release lost retry ownership", err)
	}
	if err := owner.Close(); err != nil || owner.arena != nil || owner.guest != nil || owner.thunks != nil || guest.Base() == 0 || thunks.Base() == 0 || owners[1].job.CaptureInstanceContext().ImportDispatch != owners[1].dispatch {
		t.Fatal("host release retry/shared code survivor", err)
	}
}

func TestSWARHostOwnerPreflightAndSecondSealFailureNoPublication(t *testing.T) {
	source, _, _ := swarHostSource(2, 1, 4, false)
	image, err := compileSWARHostModule(source, emissionMode{production: true, interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	job, err := coreruntime.NewJobMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := job.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, corrupt := range []func(*swarModuleImage){
		func(i *swarModuleImage) {
			i.hosts = append([]swarHostSignature(nil), i.hosts...)
			i.hosts[0].params = []wasm.ValType{wasm.V128}
		},
		func(i *swarModuleImage) { i.hostEntries = append([]int(nil), i.hostEntries...); i.hostEntries[0] = 4 },
		func(i *swarModuleImage) { i.hostCode = bytes.Clone(i.hostCode); i.hostCode[0] ^= 1 },
		func(i *swarModuleImage) {
			i.functions = append([]FunctionInfo(nil), i.functions...)
			i.functions[0].mode.functionIndex = 0
		},
		func(i *swarModuleImage) { i.hosts = nil },
	} {
		bad := *image
		corrupt(&bad)
		guest, thunks := swarHostCodeBuffers(t, &bad)
		allocations := 0
		if owner, err := bad.instantiateHostsWithArena(job, guest, thunks, func(int) (*coreruntime.Arena, error) { allocations++; return nil, errors.New("must not allocate") }); owner != nil || err == nil || allocations != 0 || job.CaptureInstanceContext().ImportDispatch != 0 || job.CaptureInstanceContext().FuncRefDescPtr != 0 {
			t.Fatal("host malformed metadata reached seal/arena/binding", err)
		}
		if err := guest.Append(nil); err != nil {
			t.Fatal("host metadata failure sealed first code", err)
		}
	}
	// Exact heap-backed thunk bytes reach second Seal only after first image
	// becomes RX. No owner arena/binding exists and caller retains both buffers.
	guest, _ := swarHostCodeBuffers(t, image)
	heap, err := coreruntime.NewHeapCodeBuffer(len(image.hostCode))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := heap.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := heap.Append(image.hostCode); err != nil {
		t.Fatal(err)
	}
	allocations := 0
	if owner, err := image.instantiateHostsWithArena(job, guest, heap, func(int) (*coreruntime.Arena, error) { allocations++; return nil, errors.New("must not allocate") }); owner != nil || err == nil || allocations != 0 || job.CaptureInstanceContext().ImportDispatch != 0 || job.CaptureInstanceContext().FuncRefDescPtr != 0 || guest.Base() == 0 {
		t.Fatal("second seal failure published/lost external ownership", err)
	}
	if err := guest.Append(nil); err == nil {
		t.Fatal("first image was not sealed before second failed")
	}
	_, thunks := swarHostCodeBuffers(t, image)
	failure := errors.New("injected host arena allocation failure")
	if owner, err := image.instantiateHostsWithArena(job, guest, thunks, func(int) (*coreruntime.Arena, error) { allocations++; return nil, failure }); owner != nil || !errors.Is(err, failure) || allocations != 1 || job.CaptureInstanceContext().ImportDispatch != 0 || job.CaptureInstanceContext().FuncRefDescPtr != 0 {
		t.Fatal("host allocation failure published", err)
	}
	owner, err := image.instantiateHosts(job, guest, thunks)
	if err != nil {
		t.Fatal("host successful retry after second seal/arena failure", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}
