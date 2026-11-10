//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func swarDataFixture(index uint32, drop bool) fixtureFunc {
	f := swarBulkMemoryFixture(false)
	f.body = []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3}
	f.body = append(f.body, 0xfc, 8)
	f.body = append(uleb(f.body, index), 0, 0x0b)
	if drop {
		f = fixtureFunc{params: []byte{0x7b}, result: 0x7b, locals: []byte{1, 127, 0x7b}, body: []byte{0x20, 0, 0xfc, 9}}
		f.body = append(uleb(f.body, index), 0x0b)
	}
	return f
}

func swarDataSource(fixtures []fixtureFunc, memory bool, payloads [][]byte) []byte {
	data := binaryModule(fixtures...)
	if memory {
		data = modifySection(data, 7, 5, []byte{1, 1, 1, 1}, false)
	}
	data = modifySection(data, 10, 12, uleb(nil, uint32(len(payloads))), false)
	sectionData := uleb(nil, uint32(len(payloads)))
	for _, payload := range payloads {
		sectionData = append(sectionData, 1) // passive
		sectionData = append(uleb(sectionData, uint32(len(payload))), payload...)
	}
	return section(data, 11, sectionData)
}

func swarOwnedDataDirectory(owner *swarDataOwner, count int) []byte {
	bits := owner.directory
	return unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&bits))), count*16)
}

func swarOwnedDataPayload(descriptor []byte) []byte {
	bits := binary.LittleEndian.Uint64(descriptor)
	return unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&bits))), int(binary.LittleEndian.Uint32(descriptor[8:])))
}

func TestSWARPassiveDataModuleAdmissionAndDeadTyping(t *testing.T) {
	for _, index := range []uint32{0, 255} {
		payloads := make([][]byte, int(index)+1)
		payloads[index] = []byte{0x80, 0xff, 0x7f, 1, 2}
		for _, drop := range []bool{false, true} {
			f := swarDataFixture(index, drop)
			data := swarDataSource([]fixtureFunc{f}, !drop, payloads)
			image, err := compileSWARModule(data, emissionMode{production: true, interruptible: !drop})
			if err != nil || image == nil || len(image.data) != len(payloads) || image.functions[0].FrameBytes <= 2048 {
				t.Fatal("passive module lowering", index, drop, err)
			}
			if !drop {
				if image, err := compileSWARModule(data, emissionMode{production: true}); image != nil || err == nil {
					t.Fatal("memory.init without interruption", err)
				}
			}
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			ft, _ := m.LocalFuncType(0)
			var memory *wasm.MemType
			if !drop {
				memory = &m.Memories[0]
			}
			if code, _, _, err := compileSWARPairsWithData(&m.Code[0], ft, memory, nil, nil, nil, len(payloads), emissionMode{}); code != nil || err == nil {
				t.Fatal("isolated data owner", err)
			}
			// A frontend-validated function's dead tail still checks directory
			// indices and exact known operands; virtual operands emit no code.
			m.Code[0].BodyBytes = []byte{0x20, 0, 0x0f, 0x0b}
			mode := emissionMode{production: true, interruptible: true}
			base, info, _, err := compileSWARPairsWithData(&m.Code[0], ft, memory, nil, nil, nil, len(payloads), mode)
			if err != nil {
				t.Fatal(err)
			}
			tail := []byte{0xfc, 9}
			if !drop {
				tail[1] = 8
			}
			tail = uleb(tail, index)
			if !drop {
				tail = append(tail, 0)
			}
			m.Code[0].BodyBytes = append(append([]byte{0x20, 0, 0x0f}, tail...), 0x0b)
			code, actual, _, err := compileSWARPairsWithData(&m.Code[0], ft, memory, nil, nil, nil, len(payloads), mode)
			if err != nil || !bytes.Equal(base, code) || info.FrameBytes != actual.FrameBytes || info.SpillSlots != actual.SpillSlots {
				t.Fatal("dead data ghost code/frame", index, drop, err)
			}
		}
	}
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}, Results: []wasm.ValType{wasm.V128}}
	memory := &wasm.MemType{Limits: wasm.Limits{Min: 1}}
	for _, tail := range [][]byte{
		{0xfc, 8, 1, 0}, {0xfc, 9, 1}, {0xfc, 8, 0, 1}, {0xfc, 8, 0}, {0xfc, 9, 0x80},
		{0x20, 0, 0xfc, 8, 0, 0}, {2, 0x40, 0xfc, 8, 0, 0, 0x0b},
	} {
		fn := &wasm.Func{BodyBytes: append(append([]byte{0x20, 0, 0x0f}, tail...), 0x0b)}
		if code, _, _, err := compileSWARPairsWithData(fn, ft, memory, nil, nil, nil, 1, emissionMode{production: true, interruptible: true}); code != nil || err == nil {
			t.Fatal("bad dead data lowering", tail, err)
		}
	}
	for _, count := range []int{-1, 257} {
		if code, _, _, err := compileSWARPairsWithData(&wasm.Func{BodyBytes: []byte{0x20, 0, 0x0b}}, ft, memory, nil, nil, nil, count, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("invalid data directory count", count, err)
		}
	}
	good := fixtureFunc{body: []byte{0x0b}}
	base := swarDataSource([]fixtureFunc{good}, true, [][]byte{{1}})
	for _, data := range [][]byte{
		modifySection(base, 12, 12, []byte{0}, true), // wrong data count
		swarDataSource([]fixtureFunc{good}, false, make([][]byte, 257)),
	} {
		if image, err := compileSWARModule(data, emissionMode{production: true}); image != nil || err == nil {
			t.Fatal("unsupported excess/mismatched data", err)
		}
	}
}

func TestSWARPassiveDataOwnedPayloadsAndCleanup(t *testing.T) {
	payloads := make([][]byte, 256)
	for i := range payloads {
		payloads[i] = bytes.Repeat([]byte{byte(i)}, i%17)
	}
	payloads[255] = []byte{1, 2, 3, 4, 5}
	source := swarDataSource([]fixtureFunc{swarDataFixture(255, true)}, false, payloads)
	image, err := compileSWARModule(source, emissionMode{production: true})
	if err != nil {
		t.Fatal(err)
	}
	clear(source)
	for pass := range 2 {
		job, err := coreruntime.NewJobMemory(0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := job.Close(); err != nil {
				t.Error(err)
			}
		})
		owner, err := image.instantiateData(job)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := owner.Close(); err != nil {
				t.Error(err)
			}
		})
		dir := swarOwnedDataDirectory(owner, len(payloads))
		for i, payload := range payloads {
			desc := dir[i*16 : (i+1)*16]
			if !bytes.Equal(swarOwnedDataPayload(desc), payload) || binary.LittleEndian.Uint32(desc[12:]) != 0 || len(payload) == 0 && binary.LittleEndian.Uint64(desc) != 0 {
				t.Fatal("owned descriptor/payload initialization", pass, i, desc)
			}
		}
		if other, err := image.instantiateData(job); other != nil || err == nil {
			t.Fatal("replaced existing data binding", err)
		}
		job.SetPassiveDataPtr(1)
		if err := owner.Close(); err == nil || owner.arena == nil || job.CaptureInstanceContext().PassiveDataPtr != 1 {
			t.Fatal("replaced data binding released", err)
		}
		job.SetPassiveDataPtr(owner.directory)
		failure := errors.New("injected data release failure")
		ar, ptr := owner.arena, owner.directory
		if err := owner.closeWith(func(actual *coreruntime.Arena) error {
			if actual != ar || job.CaptureInstanceContext().PassiveDataPtr != 0 {
				t.Fatal("data release before detach")
			}
			return failure
		}); !errors.Is(err, failure) || owner.arena != ar || owner.directory != ptr || owner.job != job || !bytes.Equal(swarOwnedDataPayload(dir[255*16:]), payloads[255]) {
			t.Fatal("data cleanup lost retry ownership", err)
		}
		if err := owner.Close(); err != nil || job.CaptureInstanceContext().PassiveDataPtr != 0 {
			t.Fatal("data retry", err)
		}
		if err := owner.Close(); err != nil {
			t.Fatal("data idempotent close", err)
		}
	}
	for _, data := range [][][]byte{make([][]byte, 257), {make([]byte, MaxModuleBytes+1)}} {
		job, err := coreruntime.NewJobMemory(0)
		if err != nil {
			t.Fatal(err)
		}
		if owner, err := (&swarModuleImage{data: data}).instantiateData(job); owner != nil || err == nil || job.CaptureInstanceContext().PassiveDataPtr != 0 {
			t.Fatal("bad data metadata bound", err)
		}
		if err := job.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSWARPassiveDataCumulativeByteBound(t *testing.T) {
	for _, total := range []int{MaxModuleBytes, MaxModuleBytes + 1} {
		segments := []wasm.Data{
			{Mode: wasm.DataMode{Kind: wasm.DataPassive}, Init: make([]byte, MaxModuleBytes/2)},
			{Mode: wasm.DataMode{Kind: wasm.DataPassive}, Init: make([]byte, total-MaxModuleBytes/2)},
		}
		owned, err := swarPassiveData(segments)
		if total == MaxModuleBytes {
			if err != nil || len(owned) != 2 {
				t.Fatal("exact cumulative passive bound", err)
			}
			segments[0].Init[0] = 1
			if owned[0][0] != 0 {
				t.Fatal("passive metadata retained source payload")
			}
		} else if owned != nil || err == nil {
			t.Fatal("one-past cumulative passive bound", err)
		}
		job, err := coreruntime.NewJobMemory(0)
		if err != nil {
			t.Fatal(err)
		}
		owner, err := (&swarModuleImage{data: [][]byte{segments[0].Init, segments[1].Init}}).instantiateData(job)
		if total == MaxModuleBytes {
			if err != nil {
				t.Fatal("exact cumulative owner bound", err)
			}
			if err := owner.Close(); err != nil {
				t.Fatal(err)
			}
		} else if owner != nil || err == nil || job.CaptureInstanceContext().PassiveDataPtr != 0 {
			t.Fatal("one-past cumulative owner bound", err)
		}
		if err := job.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, drop := range []bool{false, true} {
		data := binaryModule(swarDataFixture(0, drop))
		if !drop {
			data = modifySection(data, 7, 5, []byte{1, 1, 1, 1}, false)
		}
		data = section(data, 11, []byte{1, 1, 1, 1})
		if image, err := compileSWARModule(data, emissionMode{production: true, interruptible: true}); image != nil || err == nil {
			t.Fatal("missing data count for init/drop", drop, err)
		}
	}
	// Unused passive data need not carry section12. Init/drop do require it
	// through normal frontend validation; the pilot does not invent a rule.
	data := section(binaryModule(fixtureFunc{body: []byte{0x0b}}), 11, []byte{1, 1, 1, 1})
	if image, err := compileSWARModule(data, emissionMode{production: true}); image == nil || err != nil {
		t.Fatal("unused passive data without count", err)
	}
}
