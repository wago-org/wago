//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Poison directory caches deliberately: each row is an alias of the same
// real reservation. Generated size/growth must use authoritative basedata even
// before the runtime can refresh aliases at the next invocation boundary.
func TestProductionMultiMemoryAliasAuthoritativeCachesAndQuota(t *testing.T) {
	for _, vector := range []bool{false, true} {
		e, _, args, results, trap, _ := thunkFixtureRV64(t, 256)
		j, err := coreruntime.NewJobMemoryGrowable(65536, 2*65536)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := j.Close(); err != nil {
				t.Error(err)
			}
		})
		j.SetStackFence(e.StackLimit())
		if err := j.RebindTrapCell(trap); err != nil {
			t.Fatal(err)
		}
		arena, err := coreruntime.NewArena(100 * abi.MemoryDirEntryBytes)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			j.SetMemoryDirPtr(0)
			if err := arena.Close(); err != nil {
				t.Error(err)
			}
		})
		dir := arena.Alloc(100 * abi.MemoryDirEntryBytes)
		for i := 0; i < 100; i++ {
			row := dir[i*abi.MemoryDirEntryBytes:]
			binary.LittleEndian.PutUint64(row, uint64(j.LinMemBase()))
			binary.LittleEndian.PutUint64(row[8:], 0xfedcba9876543210)
			binary.LittleEndian.PutUint32(row[16:], 0xffffffff)
		}
		j.SetMemoryDirPtr(thunkPointerRV64(dir))
		body := []byte{0x20, 0, 0x40, 99, 0x3f, 0, 0x3f, 99, 0x0b}
		m := multiMemoryBackendDecode(t, multiMemoryBackendSource(100, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, body))
		if vector {
			m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}}}}})
		}
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, image.Code) + uintptr(image.Entry[0])
		row := dir[99*abi.MemoryDirEntryBytes:]
		binary.LittleEndian.PutUint32(row[20:], 1)
		before := bytes.Clone(dir)
		invoke := func(delta uint32, want [3]uint64) {
			t.Helper()
			binary.LittleEndian.PutUint64(args, 0xfedcba9800000000|uint64(delta))
			for i := range results {
				results[i] = 0xa5
			}
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				t.Fatal(err)
			}
			for i, value := range want {
				if got := binary.LittleEndian.Uint64(results[i*8:]); got != value {
					t.Fatal("alias cache/normalization", vector, i, got, value)
				}
			}
			if !bytes.Equal(results[24:], bytes.Repeat([]byte{0xa5}, len(results)-24)) {
				t.Fatal("result tail")
			}
		}
		invoke(1, [3]uint64{0xffffffff, 1, 1})
		if !bytes.Equal(dir, before) || j.CurrentPages() != 1 {
			t.Fatal("failed growth changed any directory row or reservation")
		}
		binary.LittleEndian.PutUint32(row[20:], 2)
		invoke(1, [3]uint64{1, 2, 2})
		if j.CurrentPages() != 2 || binary.LittleEndian.Uint64(row[8:]) != 2*65536 || binary.LittleEndian.Uint32(row[16:]) != 2 {
			t.Fatal("selected successful growth cache publication")
		}
		// Unselected directory0 remains poisoned, yet the same-activation size0
		// above observes the successful growth through memory99.
		if binary.LittleEndian.Uint32(dir[16:]) != 0xffffffff || !bytes.Equal(j.CurrentBytes()[65536:], make([]byte, 65536)) {
			t.Fatal("alias cache poison or zero-filled reservation witness")
		}
		binary.LittleEndian.PutUint32(row[20:], 1)
		invoke(0, [3]uint64{2, 2, 2}) // zero delta bypasses the lower quota
		invoke(^uint32(0), [3]uint64{0xffffffff, 2, 2})
		if allocations := testing.AllocsPerRun(50, func() {
			binary.LittleEndian.PutUint64(args, 0xfedcba9800000000)
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}); allocations != 0 {
			t.Fatal("warm indexed growth allocation", allocations)
		}
	}
}
