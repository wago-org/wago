//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	goruntime "runtime"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestSWARRV64InlineControlDirtyConditionsTransfersAndReturn(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 8)
	for index, fixture := range swarControlFixtures() {
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true, functionIndex: 12})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large control frame", index, info, err)
		}
		entry := thunkImageRV64(t, code)
		for _, condition := range []uint32{0, 1, 2, 3, 4, 16, 17, 18, 0x80000000, 0xffffffff} {
			var input [48]byte
			binary.LittleEndian.PutUint64(input[:], 0x7ff0000000000001)
			binary.LittleEndian.PutUint64(input[8:], 0x80000000deadbeef)
			binary.LittleEndian.PutUint64(input[16:], 0xa55aff0012345678)
			binary.LittleEndian.PutUint64(input[24:], 0xfff80000cafebabe)
			binary.LittleEndian.PutUint64(input[32:], dirty(condition))
			if index == 6 {
				binary.LittleEndian.PutUint64(input[16:], dirty(condition))
			}
			copy(args, input[:])
			for i := range results {
				results[i] = 0xa5
			}
			err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
			if !bytes.Equal(args[:48], input[:]) {
				t.Fatal("control modified inputs", index, condition)
			}
			if index == 7 {
				var tr *coreruntime.TrapError
				if !errors.As(err, &tr) || tr.Code != coreruntime.TrapUnreachable || binary.LittleEndian.Uint64(trap[16:]) != uint64(fn.LocalDeclBytes)<<32|13 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
					t.Fatal("unreachable source/prewrite", err)
				}
				continue
			}
			want := append([]byte(nil), input[:16]...)
			switch index {
			case 1, 4:
				if condition == 0 {
					copy(want, input[16:32])
				}
			case 2:
				if condition != 0 {
					copy(want, input[16:32])
				}
			case 5:
				if condition < 17 && condition%2 == 0 {
					for lane := range want {
						want[lane] = ^want[lane]
					}
				}
			case 6:
				want = append(want, make([]byte, 16)...)
				binary.LittleEndian.PutUint64(want[16:], uint64(condition))
				copy(want[24:], input[24:32])
			}
			if err != nil || !bytes.Equal(results[:len(want)], want) {
				t.Fatal("control result/branch/overlap/raw cells", index, condition, results[:len(want)], want, err)
			}
			for i := len(want); i < 64; i++ {
				if results[i] != 0xa5 {
					t.Fatal("control result tail", index, condition, i)
				}
			}
		}
		if index != 7 {
			call := func() {
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
					panic(err)
				}
			}
			if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
				t.Fatal("control warm allocations", index, allocs)
			}
		}
	}
}
