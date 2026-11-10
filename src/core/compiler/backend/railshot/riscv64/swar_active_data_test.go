//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func swarActiveSegment(offset uint32, payload []byte) wasm.Data {
	expr := append(sleb([]byte{0x41}, int64(int32(offset))), 0x0b)
	return wasm.Data{Mode: wasm.DataMode{Kind: wasm.DataActive, Offset: wasm.Expr{BodyBytes: expr}}, Init: payload}
}

func swarMixedDataSource(fixtures []fixtureFunc, segments []wasm.Data) []byte {
	data := modifySection(binaryModule(fixtures...), 7, 5, []byte{1, 1, 1, 1}, false)
	data = modifySection(data, 10, 12, uleb(nil, uint32(len(segments))), false)
	payload := uleb(nil, uint32(len(segments)))
	for _, segment := range segments {
		if segment.Mode.Kind == wasm.DataActive {
			payload = append(payload, 0)
			payload = append(payload, segment.Mode.Offset.BodyBytes...)
		} else {
			payload = append(payload, 1)
		}
		payload = append(uleb(payload, uint32(len(segment.Init))), segment.Init...)
	}
	return section(data, 11, payload)
}

func TestSWARActiveDataSnapshotIndicesAndPrepublicationFailures(t *testing.T) {
	f := fixtureFunc{body: []byte{0x0b}}
	segments := []wasm.Data{
		swarActiveSegment(3, []byte{1, 2, 3, 4, 5}),
		{Mode: wasm.DataMode{Kind: wasm.DataPassive}, Init: []byte{0x80, 0xff, 0x7f}},
		swarActiveSegment(5, []byte{0xab, 0xcd}),
		swarActiveSegment(65536, nil),
	}
	source := swarMixedDataSource([]fixtureFunc{f}, segments)
	image, err := compileSWARModule(source, emissionMode{production: true})
	if err != nil || len(image.data) != 4 || len(image.activeData) != 3 || image.activeData[0].index != 0 || image.activeData[1].index != 2 || image.activeData[2].index != 3 {
		t.Fatal("active/passive original indices", err)
	}
	clear(source)
	job, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := job.Close(); err != nil {
			t.Error(err)
		}
	})
	mem := job.CurrentBytes()
	for i := range mem {
		mem[i] = 0xa5
	}
	before := bytes.Clone(mem)
	failure := errors.New("injected active-data arena allocation failure")
	allocations := 0
	if owner, err := image.instantiateDataWithArena(job, func(int) (*coreruntime.Arena, error) { allocations++; return nil, failure }); owner != nil || !errors.Is(err, failure) || allocations != 1 || job.CaptureInstanceContext().PassiveDataPtr != 0 || !bytes.Equal(mem, before) {
		t.Fatal("allocation failure wrote or published initialization", err)
	}
	owner, err := image.instantiateData(job)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	want := bytes.Clone(before)
	for _, segment := range segments {
		if segment.Mode.Kind == wasm.DataActive {
			offset, err := plainScalarConst(segment.Mode.Offset, wasm.I32, false)
			if err != nil {
				t.Fatal(err)
			}
			copy(want[int(offset):], segment.Init)
		}
	}
	if !bytes.Equal(mem, want) {
		t.Fatal("active overlap order or source ownership")
	}
	dir := swarOwnedDataDirectory(owner, 4)
	for _, index := range []int{0, 2, 3} {
		if !bytes.Equal(dir[index*16:(index+1)*16], make([]byte, 16)) {
			t.Fatal("active descriptor not initially dropped", index)
		}
	}
	if !bytes.Equal(swarOwnedDataPayload(dir[16:]), segments[1].Init) {
		t.Fatal("passive index shifted by active initialization")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	// A later invalid segment must prevent an earlier valid write and any
	// arena allocation, not merely avoid publishing a partially built owner.
	for _, bad := range []wasm.Data{swarActiveSegment(65535, []byte{1, 2}), swarActiveSegment(65537, nil), swarActiveSegment(0xffffffff, nil)} {
		broken, err := compileSWARModule(swarMixedDataSource([]fixtureFunc{f}, []wasm.Data{swarActiveSegment(0, []byte{0xff}), bad}), emissionMode{production: true})
		if err != nil {
			t.Fatal("valid source with runtime OOB initializer rejected at compile", err)
		}
		copy(before, mem)
		calls := 0
		if failed, err := broken.instantiateDataWithArena(job, func(int) (*coreruntime.Arena, error) { calls++; return nil, failure }); failed != nil || err == nil || errors.Is(err, failure) || calls != 0 || job.CaptureInstanceContext().PassiveDataPtr != 0 || !bytes.Equal(mem, before) {
			t.Fatal("all initializer ranges not checked before allocation/writes", bad, err, calls)
		}
	}
}

func TestSWARActiveDataExactLiteralShapeAndCombinedBounds(t *testing.T) {
	memory := &wasm.MemType{Limits: wasm.Limits{Min: 1}}
	for _, segments := range [][]wasm.Data{
		{{Mode: wasm.DataMode{Kind: wasm.DataActive, Mem: 1, Offset: wasm.Expr{BodyBytes: []byte{0x41, 0, 0x0b}}}}},
		{{Mode: wasm.DataMode{Kind: wasm.DataActive, Offset: wasm.Expr{BodyBytes: []byte{0x41, 1, 0x41, 2, 0x6a, 0x0b}}}}},
		{{Mode: wasm.DataMode{Kind: wasm.DataActive, Offset: wasm.Expr{BodyBytes: []byte{0x23, 0, 0x0b}}}}},
		{{Mode: wasm.DataMode{Kind: wasm.DataActive, Offset: wasm.Expr{BodyBytes: []byte{0x42, 0, 0x0b}}}}},
		{{Mode: wasm.DataMode{Kind: wasm.DataModeKind(2)}}},
		{swarActiveSegment(0, make([]byte, MaxModuleBytes/2)), {Mode: wasm.DataMode{Kind: wasm.DataPassive}, Init: make([]byte, MaxModuleBytes/2+1)}},
		make([]wasm.Data, 257),
	} {
		if passive, active, err := swarModuleData(segments, memory); passive != nil || active != nil || err == nil {
			t.Fatal("unsupported active data shape/resource", err)
		}
	}
	for _, memory := range []*wasm.MemType{nil, {Shared: true}, {Limits: wasm.Limits{Addr64: true}}} {
		if passive, active, err := swarModuleData([]wasm.Data{swarActiveSegment(0, nil)}, memory); passive != nil || active != nil || err == nil {
			t.Fatal("active unsupported memory", err)
		}
	}
	segments := []wasm.Data{swarActiveSegment(0, make([]byte, MaxModuleBytes/2)), {Mode: wasm.DataMode{Kind: wasm.DataPassive}, Init: make([]byte, MaxModuleBytes/2)}}
	if passive, active, err := swarModuleData(segments, memory); err != nil || len(passive) != 2 || len(active) != 1 {
		t.Fatal("exact combined active/passive byte quota", err)
	}
	job, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := job.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, active := range [][]swarActiveData{
		{{index: 1, bytes: []byte{1}}},
		{{index: 0}, {index: 0}},
		{{index: 0, bytes: make([]byte, MaxModuleBytes+1)}},
	} {
		image := &swarModuleImage{memory: memory, data: make([][]byte, 1), activeData: active}
		if owner, err := image.instantiateData(job); owner != nil || err == nil || job.CaptureInstanceContext().PassiveDataPtr != 0 {
			t.Fatal("malformed active owner metadata", err)
		}
	}
	image := &swarModuleImage{memory: memory, data: [][]byte{{1}}, activeData: []swarActiveData{{index: 0}}}
	if owner, err := image.instantiateData(job); owner != nil || err == nil {
		t.Fatal("active/passive metadata index collision", err)
	}
}

func TestSWARActiveDataZeroMemoryAndOwnerCombinedQuota(t *testing.T) {
	data := swarMixedDataSource([]fixtureFunc{{body: []byte{0x0b}}}, []wasm.Data{swarActiveSegment(0, nil)})
	data = modifySection(data, 5, 5, []byte{1, 1, 0, 0}, true)
	image, err := compileSWARModule(data, emissionMode{production: true})
	if err != nil {
		t.Fatal("zero-memory empty active source", err)
	}
	job, err := coreruntime.NewJobMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := image.instantiateData(job)
	if err != nil || owner.directory == 0 || !bytes.Equal(swarOwnedDataDirectory(owner, 1), make([]byte, 16)) {
		t.Fatal("zero-memory empty active owner", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := job.Close(); err != nil {
		t.Fatal(err)
	}
	large, err := coreruntime.NewJobMemory(MaxModuleBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := large.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, total := range []int{MaxModuleBytes, MaxModuleBytes + 1} {
		image := &swarModuleImage{
			memory:     &wasm.MemType{Limits: wasm.Limits{Min: 16}},
			data:       [][]byte{nil, make([]byte, MaxModuleBytes/2)},
			activeData: []swarActiveData{{index: 0, bytes: make([]byte, total-MaxModuleBytes/2)}},
		}
		owner, err := image.instantiateData(large)
		if total == MaxModuleBytes {
			if err != nil {
				t.Fatal("exact combined owner quota", err)
			}
			if err := owner.Close(); err != nil {
				t.Fatal(err)
			}
		} else if owner != nil || err == nil || large.CaptureInstanceContext().PassiveDataPtr != 0 {
			t.Fatal("combined owner one-past quota", err)
		}
	}
}
