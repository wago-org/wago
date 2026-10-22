//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func swarHostCombinedStateSource(count int) ([]byte, []fixtureFunc, [2]uint64, []byte, []byte) {
	initial := [2]uint64{0x7ff0000000000001, 0x80000000deadbeef}
	memory := []byte{1, 2, 3, 4, 5, 6, 0xf0, 0x7f, 0x80, 0, 0, 0, 0, 0, 0, 0x80}
	payload := []byte{0xef, 0xbe, 0xad, 0xde, 0, 0, 0, 0x80, 1, 0, 0, 0, 0, 0, 0xf0, 0xff}
	hostTypes := []byte{0x7f, 0x7e, 0x7d, 0x7c}
	root := fixtureFunc{params: []byte{0x7b, 0x7f, 0x7f}, results: []byte{0x7b, 0x7b, 0x7b, 0x7c, 0x7f, 0x7e, 0x7d, 0x7c, 0x7b, 0x7b}, locals: []byte{1, 125, 0x7b}, body: []byte{
		0x23, 0, 0x20, 0, 0x20, 1, 0xfd, 0, 0, 0, 0x23, 1,
		0x20, 0, 0x24, 0, // Ordered mutation before parking in Go.
		0x20, 1, 0x42,
	}}
	root.body = sleb(root.body, -0x7edcba9876543211)
	root.body = append(root.body, 0x43)
	root.body = binary.LittleEndian.AppendUint32(root.body, 0x7f800123)
	root.body = append(root.body, 0x44)
	root.body = binary.LittleEndian.AppendUint64(root.body, 0xfff0000000000001)
	root.body = uleb(append(root.body, 0x10), uint32(count+1))
	root.body = append(root.body,
		0x20, 1, 0x41, 0, 0x41, 16, 0xfc, 8, 2, 0,
		0x20, 2, 4, 0x40, 0xfc, 9, 2, 0x0b,
		0x23, 0, 0x20, 1, 0xfd, 0, 0, 0, 0x0b,
	)
	leaf := fixtureFunc{params: hostTypes, results: hostTypes, locals: []byte{1, 124, 0x7b}, unexported: true, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3}}
	leaf.body = append(uleb(append(leaf.body, 0x10), uint32(count-1)), 0x0b)
	fixtures := []fixtureFunc{root, leaf}
	segments := []wasm.Data{swarActiveSegment(7, []byte{0xa5, 0x5a}), swarActiveSegment(101, memory), {Mode: wasm.DataMode{Kind: wasm.DataPassive}, Init: payload}}
	source := swarMixedDataSource(fixtures, segments)
	types := []byte{3}
	for _, f := range []fixtureFunc{{params: hostTypes, results: hostTypes}, root, leaf} {
		types = append(types, 0x60)
		types = append(uleb(types, uint32(len(f.params))), f.params...)
		types = append(uleb(types, uint32(len(f.results))), f.results...)
	}
	source = modifySection(source, 1, 1, types, true)
	source = modifySection(source, 3, 3, []byte{2, 1, 2}, true)
	imports := uleb(nil, uint32(count))
	for range count {
		imports = append(imports, 1, 'm', 1, 'h', 0, 0)
	}
	source = modifySection(source, 3, 2, imports, false)
	source = modifySection(source, 5, 5, []byte{1, 1, 1, 2}, true)
	globals := append([]byte{2}, swarGlobalDeclaration(0x7b, true, initial)...)
	globals = append(globals, swarGlobalDeclaration(0x7c, false, [2]uint64{0x7ff0000000000001, 0})...)
	source = modifySection(source, 7, 6, globals, false)
	source = modifySection(source, 7, 7, uleb([]byte{1, 1, 'a', 0}, uint32(count)), true)
	return source, fixtures, initial, memory, payload
}

func TestSWARHostCombinedStateWholeModuleSnapshotAndPrefix(t *testing.T) {
	for _, count := range []int{1, 256} {
		source, _, initial, memory, payload := swarHostCombinedStateSource(count)
		swarValidatedHostSource(t, source)
		image, err := compileSWARHostModule(source, emissionMode{production: true, interruptible: true})
		if err != nil || len(image.hosts) != count || len(image.functions) != 2 || image.functions[0].DirectCalls != 1 || len(image.globals) != 2 || len(image.data) != 3 || len(image.activeData) != 2 || image.globals[0].value != initial || image.memory == nil || !image.memory.Limits.HasMax || image.memory.Limits.Min != 1 || image.memory.Limits.Max != 2 {
			t.Fatal("combined host/global/data module", count, err)
		}
		for i, info := range image.functions {
			if info.mode.functionIndex != uint32(count+i) || info.FrameHeaderBytes != callFrameHeader || info.FrameBytes <= 2048 {
				t.Fatal("combined defined source prefix/frame", info)
			}
		}
		clear(source)
		if !bytes.Equal(image.data[2], payload) || !bytes.Equal(image.activeData[1].bytes, memory) || image.globals[1].value[0] != 0x7ff0000000000001 || image.hosts[count-1].params[2] != wasm.F32 {
			t.Fatal("combined source snapshot aliases")
		}
		source, _, _, _, _ = swarHostCombinedStateSource(count)
		for _, compile := range []func([]byte, emissionMode) (*swarModuleImage, error){compileSWARModule, compileSWARTableModule} {
			if image, err := compile(source, emissionMode{production: true, interruptible: true}); image != nil || err == nil {
				t.Fatal("combined source escaped explicit host policy")
			}
		}
		if image, err := compileSWARHostModule(source, emissionMode{production: true}); image != nil || err == nil {
			t.Fatal("combined host/data source omitted interruption bridge")
		}
	}
}

func TestSWARHostCombinedOwnersIndependentBindingsAndActiveFailure(t *testing.T) {
	source, _, _, memory, _ := swarHostCombinedStateSource(256)
	image, err := compileSWARHostModule(source, emissionMode{production: true, interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	guest, thunks := swarHostCodeBuffers(t, image)
	job, err := coreruntime.NewJobMemoryGrowable(65536, 131072)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := job.Close(); err != nil {
			t.Error(err)
		}
	})
	globals, err := image.instantiateGlobals(job)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := globals.Close(); err != nil {
			t.Error(err)
		}
	})
	hosts, err := image.instantiateHosts(job, guest, thunks)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := hosts.Close(); err != nil {
			t.Error(err)
		}
	})
	// Active-range validation happens before the first initializer write even
	// with unrelated host/global bindings already live. No owner arena is taken.
	bad := *image
	bad.activeData = append([]swarActiveData(nil), image.activeData...)
	bad.activeData[1].offset = 65521 // 16-byte range exceeds the first page.
	before := bytes.Clone(job.CurrentBytes())
	ctx := job.CaptureInstanceContext()
	allocations := 0
	if owner, err := bad.instantiateDataWithArena(job, func(n int) (*coreruntime.Arena, error) {
		allocations++
		return coreruntime.NewArena(n)
	}); owner != nil || err == nil || allocations != 0 || job.CaptureInstanceContext() != ctx || !bytes.Equal(job.CurrentBytes(), before) {
		t.Fatal("combined failed active initialization changed host/global/memory", allocations, err)
	}
	data, err := image.instantiateData(job)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := data.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx = job.CaptureInstanceContext()
	if ctx.ImportDispatch != hosts.dispatch || ctx.FuncRefDescPtr != hosts.directory || ctx.GlobalsPtr != globals.directory || ctx.PassiveDataPtr != data.directory || !bytes.Equal(job.CurrentBytes()[101:117], memory) {
		t.Fatal("combined host/global/data bindings")
	}
	if err := globals.Close(); err != nil {
		t.Fatal(err)
	}
	after := job.CaptureInstanceContext()
	if after.GlobalsPtr != 0 || after.PassiveDataPtr != ctx.PassiveDataPtr || after.ImportDispatch != ctx.ImportDispatch || after.FuncRefDescPtr != ctx.FuncRefDescPtr {
		t.Fatal("global retirement damaged data/host bindings")
	}
	globals, err = image.instantiateGlobals(job)
	if err != nil {
		t.Fatal(err)
	}
	ctx = job.CaptureInstanceContext()
	if err := hosts.Close(); err != nil {
		t.Fatal(err)
	}
	after = job.CaptureInstanceContext()
	if after.ImportDispatch != 0 || after.FuncRefDescPtr != 0 || after.GlobalsPtr != ctx.GlobalsPtr || after.PassiveDataPtr != ctx.PassiveDataPtr {
		t.Fatal("host retirement damaged global/data bindings")
	}
	hosts, err = image.instantiateHosts(job, guest, thunks)
	if err != nil {
		t.Fatal(err)
	}
	ctx = job.CaptureInstanceContext()
	if err := data.Close(); err != nil {
		t.Fatal(err)
	}
	after = job.CaptureInstanceContext()
	if after.PassiveDataPtr != 0 || after.ImportDispatch != ctx.ImportDispatch || after.FuncRefDescPtr != ctx.FuncRefDescPtr || after.GlobalsPtr != ctx.GlobalsPtr {
		t.Fatal("data retirement damaged host/global bindings")
	}
	data, err = image.instantiateData(job)
	if err != nil {
		t.Fatal(err)
	}
}
