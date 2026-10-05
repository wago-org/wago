//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// This fixture installs real production directories in off-heap arenas, just
// as public instantiation does. No experimental GuestContext is involved.
func productionStateFixture(t *testing.T, data []byte) (*coreruntime.JobMemory, [][]byte, []byte) {
	t.Helper()
	d, err := wasm.DecodeModuleByteBackedWithLimits(data, wasm.ValidationFeatures{}, wasm.DecodeLimits{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := admitStateMode(d.Module, true)
	if err != nil {
		t.Fatal(err)
	}
	initial, maximum := 0, 0
	if s.memory != nil {
		initial, maximum = int(s.memory.min)*65536, 65536*65536
		if s.memory.hasMax {
			maximum = int(s.memory.max) * 65536
		}
	}
	j, err := coreruntime.NewJobMemoryGrowable(initial, maximum)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := j.Close(); err != nil {
			t.Error(err)
		}
	})
	n := 64 + len(s.globals)*24 + len(s.passiveData)*16
	for _, b := range s.passiveData {
		n += len(b) + 8
	}
	arena, err := coreruntime.NewArena(n)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := arena.Close(); err != nil {
			t.Error(err)
		}
	})
	cells := make([][]byte, len(s.globals))
	if len(cells) != 0 {
		dir := arena.Alloc(len(cells) * 8)
		for i, g := range s.globals {
			cells[i] = arena.Alloc(16)
			binary.LittleEndian.PutUint64(cells[i], g.value)
			binary.LittleEndian.PutUint64(cells[i][8:], 0xfeedcabed00dcafe)
			binary.LittleEndian.PutUint64(dir[i*8:], uint64(uintptr(unsafe.Pointer(&cells[i][0]))))
		}
		j.SetGlobalsPtr(uintptr(unsafe.Pointer(&dir[0])))
	}
	var descriptors []byte
	if len(s.passiveData) != 0 {
		descriptors = arena.Alloc(len(s.passiveData) * 16)
		for i, b := range s.passiveData {
			if len(b) != 0 {
				storage := arena.Alloc(len(b))
				copy(storage, b)
				binary.LittleEndian.PutUint64(descriptors[i*16:], uint64(uintptr(unsafe.Pointer(&storage[0]))))
			}
			binary.LittleEndian.PutUint32(descriptors[i*16+8:], uint32(len(b)))
			binary.LittleEndian.PutUint32(descriptors[i*16+12:], 0xfadebead)
		}
		j.SetPassiveDataPtr(uintptr(unsafe.Pointer(&descriptors[0])))
	}
	for _, segment := range s.data {
		end := uint64(segment.Offset) + uint64(len(segment.Bytes))
		if end > uint64(len(j.HostBytes())) {
			t.Fatal("fixture active initialization out of bounds")
		}
		copy(j.HostBytes()[int(segment.Offset):int(end)], segment.Bytes)
	}
	return j, cells, descriptors
}

func productionBoundsTrap(t *testing.T, err error) {
	t.Helper()
	var trap *coreruntime.TrapError
	if !errors.As(err, &trap) || trap.Code != coreruntime.TrapLinMemOutOfBounds {
		t.Fatal("expected production memory bounds trap", err)
	}
}

func TestProductionMemoryAccessWidthsUnalignedAndBounds(t *testing.T) {
	for _, op := range []byte{0x28, 0x29, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e} {
		t.Run(fmt.Sprintf("%x", op), func(t *testing.T) {
			size, wide, signed, store, _ := memoryOpcode(op)
			for _, heap := range []bool{false, true} {
				f := productionCallsWithOptions(t, stateModule([]fixtureFunc{memoryFixture(op, 2)}, false, 1, 1, true, nil, nil), ProductionOptions{DeferCodeMapping: heap})
				memory := f.memory.HostBytes()
				copy(memory[3:], []byte{0x81, 0xfe, 0xdc, 0xba, 0x98, 0x76, 0x54, 0xf2})
				left, right := memory[2], memory[3+size]
				args := []uint64{dirty(1)}
				want := loadExpected(memory[3:3+size], wide, signed)
				if store {
					args = append(args, 0xf23456789abcdef1)
					want = args[1]
					if !wide {
						want = uint64(uint32(want))
					}
					if memory[2] != left || memory[3+size] != right {
						t.Fatal("successful store changed adjacent byte")
					}
				}
				result := []uint64{0}
				if err := f.invoke(0, args, result); err != nil || result[0] != want {
					t.Fatal("unaligned dirty-carrier access", result, want, err)
				}
				if store {
					for i := 0; i < size; i++ {
						if memory[3+i] != byte(args[1]>>uint(i*8)) {
							t.Fatal("store width", memory[3:3+size])
						}
					}
				}
				before := append([]byte(nil), memory...)
				for _, address := range []uint32{uint32(65536 - size - 1), 0xffffffff} {
					args[0] = dirty(address)
					result[0] = 0xcafe
					productionBoundsTrap(t, f.invoke(0, args, result))
					if result[0] != 0xcafe || !bytes.Equal(before, memory) {
						t.Fatal("trapping access committed output or partial store")
					}
				}
			}
		})
	}
}

func TestProductionGlobalCellsAliasingAndCanonicalWrites(t *testing.T) {
	fs := []fixtureFunc{
		{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x24, 0, 0x23, 1, 0x0b}},
		{params: []byte{0x7e}, result: 0x7e, body: []byte{0x20, 0, 0x24, 2, 0x23, 2, 0x0b}},
	}
	data := stateModule(fs, false, 0, 0, true, []globalFixture{{mutable: true, value: -1}, {mutable: true, value: 7}, {wide: true, mutable: true, value: -9}}, nil)
	f := productionCalls(t, data, false)
	// Two declared slots intentionally share the same production cell pointer.
	arena, err := coreruntime.NewArena(len(f.globalCells) * 8)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	dir := arena.Alloc(len(f.globalCells) * 8)
	for i, cell := range f.globalCells {
		binary.LittleEndian.PutUint64(dir[i*8:], uint64(uintptr(unsafe.Pointer(&cell[0]))))
	}
	binary.LittleEndian.PutUint64(dir[8:], binary.LittleEndian.Uint64(dir))
	f.memory.SetGlobalsPtr(uintptr(unsafe.Pointer(&dir[0])))
	for _, value := range []uint64{dirty(0xffffffff), dirty(0x80000001), dirty(7)} {
		result := []uint64{0}
		if err := f.invoke(0, []uint64{value}, result); err != nil || result[0] != uint64(uint32(value)) {
			t.Fatal("aliased global get/set", result, err)
		}
		if binary.LittleEndian.Uint64(f.globalCells[0]) != uint64(uint32(value)) || binary.LittleEndian.Uint64(f.globalCells[0][8:]) != 0xfeedcabed00dcafe {
			t.Fatal("global canonical carrier or neighboring cell")
		}
	}
	result := []uint64{0}
	if err := f.invoke(1, []uint64{0xfedcba9876543210}, result); err != nil || result[0] != 0xfedcba9876543210 {
		t.Fatal("i64 cell", result, err)
	}
}

func TestProductionMemoryGrowthQuotaAndFullMemory32(t *testing.T) {
	fs := []fixtureFunc{growFixture(), {result: 0x7f, body: []byte{0x3f, 0, 0x0b}}, memoryFixture(0x3a, 0), memoryFixture(0x2d, 0), memoryFixture(0x2d, 0xffffffff)}
	f := productionCalls(t, stateModule(fs, false, 0, 65536, true, nil, nil), false)
	arena, err := coreruntime.NewArena(abi.MemoryDirEntryBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	dir := arena.Alloc(abi.MemoryDirEntryBytes)
	binary.LittleEndian.PutUint32(dir[abi.MemoryDirPolicyMaxPagesOffset:], 1)
	f.memory.SetMemoryDirPtr(uintptr(unsafe.Pointer(&dir[0])))
	result := []uint64{0}
	for _, tc := range []struct {
		delta uint32
		want  uint64
		pages int
	}{{0, 0, 0}, {1, 0, 1}, {1, 0xffffffff, 1}, {0xffffffff, 0xffffffff, 1}, {0, 1, 1}} {
		if err := f.invoke(0, []uint64{dirty(tc.delta)}, result); err != nil || result[0] != tc.want || len(f.memory.HostBytes()) != tc.pages*65536 {
			t.Fatal("quota growth", tc, result, err)
		}
	}
	binary.LittleEndian.PutUint32(dir[abi.MemoryDirPolicyMaxPagesOffset:], 0)
	if err := f.invoke(0, []uint64{dirty(65535)}, result); err != nil || result[0] != 1 || uint64(len(f.memory.HostBytes())) != 1<<32 {
		t.Fatal("full memory32 growth", result, err)
	}
	if err := f.invoke(1, nil, result); err != nil || result[0] != 65536 {
		t.Fatal("full page-size cache", result, err)
	}
	if err := f.invoke(2, []uint64{dirty(0xffffffff), dirty(0xab)}, result); err != nil || f.memory.HostBytes()[0xffffffff] != 0xab {
		t.Fatal("last byte of full memory32", err)
	}
	if err := f.invoke(3, []uint64{dirty(0xffffffff)}, result); err != nil || result[0] != 0xab {
		t.Fatal("full u64 byte bound", result, err)
	}
	if err := f.invoke(0, []uint64{dirty(1)}, result); err != nil || result[0] != 0xffffffff {
		t.Fatal("full memory overflow", result, err)
	}
	productionBoundsTrap(t, f.invoke(4, []uint64{dirty(1)}, result))
	if err := f.invoke(4, []uint64{dirty(0)}, result); err != nil || result[0] != 0xab {
		t.Fatal("full memory32 unsigned immediate", result, err)
	}
	binary.LittleEndian.PutUint32(dir[abi.MemoryDirPolicyMaxPagesOffset:], 1)
	if err := f.invoke(0, []uint64{dirty(0)}, result); err != nil || result[0] != 65536 {
		t.Fatal("zero growth above a lowered quota", result, err)
	}
}

func TestProductionBulkDescriptorsBoundsOverlapAndDrop(t *testing.T) {
	segments := make([]bulkDataFixture, 130)
	segments[0] = bulkDataFixture{offset: 20, bytes: []byte{7, 8}}
	for i := 1; i < len(segments); i++ {
		segments[i] = bulkDataFixture{passive: true, bytes: []byte{byte(i), 0xab, 0xcd}}
	}
	fs := []fixtureFunc{bulkFixture(8, 129), bulkFixture(9, 129), bulkFixture(10, 0), bulkFixture(11, 0), bulkFixture(8, 0), bulkFixture(8, 128)}
	data := withBulkData(stateModule(fs, false, 1, 1, true, []globalFixture{{value: 9}}, nil), segments, true)
	for _, heap := range []bool{false, true} {
		f := productionCallsWithOptions(t, data, ProductionOptions{DeferCodeMapping: heap})
		memory := f.memory.HostBytes()
		if !bytes.Equal(memory[20:22], []byte{7, 8}) {
			t.Fatal("active fixture initialization")
		}
		result := []uint64{0}
		for _, tc := range []struct {
			index int
			args  []uint64
		}{
			{0, []uint64{dirty(3), dirty(0), dirty(3)}},
			{5, []uint64{dirty(6), dirty(0), dirty(3)}},
			{2, []uint64{dirty(4), dirty(3), dirty(5)}}, // backward overlapping copy
			{2, []uint64{dirty(3), dirty(4), dirty(5)}}, // forward overlapping copy
			{3, []uint64{dirty(10), dirty(0x123), dirty(5)}},
		} {
			if err := f.invoke(tc.index, tc.args, result); err != nil || result[0] != 37 {
				t.Fatal("production bulk call", tc, result, err)
			}
		}
		if !bytes.Equal(memory[3:9], []byte{129, 0xab, 0xcd, 128, 0xab, 0xab}) || !bytes.Equal(memory[10:15], bytes.Repeat([]byte{0x23}, 5)) {
			t.Fatal("bulk overlapping copy/fill", memory[:22])
		}
		before := append([]byte(nil), memory...)
		for _, tc := range []struct {
			index int
			args  []uint64
		}{
			{0, []uint64{dirty(65535), dirty(0), dirty(2)}},
			{0, []uint64{dirty(0), dirty(2), dirty(2)}},
			{2, []uint64{dirty(0), dirty(65535), dirty(2)}},
			{3, []uint64{dirty(65537), dirty(0), dirty(0)}},
			{4, []uint64{dirty(0), dirty(0), dirty(1)}}, // active segment is dropped
		} {
			result[0] = 0xcafe
			productionBoundsTrap(t, f.invoke(tc.index, tc.args, result))
			if result[0] != 0xcafe || !bytes.Equal(memory, before) {
				t.Fatal("bulk trap partially committed state")
			}
		}
		for _, index := range []int{0, 2, 3, 4} {
			if err := f.invoke(index, []uint64{dirty(65536), dirty(0), dirty(0)}, result); err != nil {
				t.Fatal("exact-end zero-count bulk", index, err)
			}
		}
		descriptorsBefore := append([]byte(nil), f.passiveDescriptors...)
		if err := f.invoke(1, nil, result); err != nil {
			t.Fatal(err)
		}
		binary.LittleEndian.PutUint32(descriptorsBefore[129*16+8:], 0)
		if !bytes.Equal(descriptorsBefore, f.passiveDescriptors) {
			t.Fatal("data.drop changed pointer, pad, or neighboring descriptor")
		}
		productionBoundsTrap(t, f.invoke(0, []uint64{0, 0, 1}, result))
		productionBoundsTrap(t, f.invoke(0, []uint64{0, 1, 0}, result))
		if err := f.invoke(0, []uint64{dirty(65536), 0, 0}, result); err != nil {
			t.Fatal("dropped exact empty range", err)
		}
		if err := f.invoke(5, []uint64{30, 0, 3}, result); err != nil || memory[30] != 128 {
			t.Fatal("neighboring segment after drop", err)
		}
	}
}

func TestProductionZeroMemoryBulkAndAccess(t *testing.T) {
	fs := []fixtureFunc{bulkFixture(10, 0), bulkFixture(11, 0), bulkFixture(8, 0), memoryFixture(0x2d, 0), growFixture()}
	data := withBulkData(stateModule(fs, false, 0, 0, true, nil, nil), []bulkDataFixture{{passive: true}}, true)
	f := productionCalls(t, data, false)
	result := []uint64{0}
	for _, index := range []int{0, 1, 2} {
		if err := f.invoke(index, []uint64{dirty(0), dirty(0), dirty(0)}, result); err != nil || result[0] != 37 {
			t.Fatal("zero memory empty range", index, err)
		}
		productionBoundsTrap(t, f.invoke(index, []uint64{dirty(1), 0, 0}, result))
	}
	productionBoundsTrap(t, f.invoke(3, []uint64{dirty(0)}, result))
	if err := f.invoke(4, []uint64{dirty(0)}, result); err != nil || result[0] != 0 {
		t.Fatal("zero-memory zero growth", err)
	}
	if err := f.invoke(4, []uint64{dirty(1)}, result); err != nil || result[0] != 0xffffffff {
		t.Fatal("zero-memory failed growth", err)
	}
}

func TestProductionStateDirectCallsObserveGrowth(t *testing.T) {
	fs := []fixtureFunc{
		{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x10, 1, 0x1a, 0x3f, 0, 0x0b}},
		growFixture(),
	}
	f := productionCalls(t, stateModule(fs, false, 0, 2, true, nil, nil), false)
	result := []uint64{0}
	for _, want := range []uint64{1, 2, 2} {
		if err := f.invoke(0, []uint64{dirty(1)}, result); err != nil || result[0] != want {
			t.Fatal("caller observed stale growth or home transport", result, want, err)
		}
	}
}
