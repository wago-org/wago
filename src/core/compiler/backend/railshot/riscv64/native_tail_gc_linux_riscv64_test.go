//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"encoding/binary"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestNativeTailHostContinuation(t *testing.T) {
	arena, err := coreruntime.NewArena(tailRAInvocationRecordBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	data := arena.Alloc(tailRAInvocationRecordBytes)
	record := tailRAPointer(data)
	exit, err := nativeTailExitPointer()
	if err != nil {
		t.Fatal(err)
	}
	const home, context, originalPC = uintptr(0x10000), uintptr(0x20000), uintptr(0x30000)
	put := func(offset int, value uintptr) { binary.LittleEndian.PutUint64(data[offset:], uint64(value)) }
	put(0, originalPC)
	put(64, record+tailRAInvocationRecordBytes)
	put(72, exit)
	put(80, uintptr(tailRARecordMagic))
	put(96, home)
	put(104, tailRAInvocationRecordBytes)
	put(144, context)
	put(tailRAPublicRecordBytes, uintptr(abi.RV64InvocationMagic))
	put(tailRAPublicRecordBytes+int(abi.RV64InvocationFirstRAOffset), exit)
	read := func() (uintptr, uintptr, bool) {
		return NativeTailHostContinuation(record+tailRACtrlOffset, home, context, record+tailRAPublicRecordBytes, record, record+tailRAInvocationRecordBytes)
	}
	if pc, top, ok := read(); !ok || pc != originalPC || top != record+tailRAInvocationRecordBytes {
		t.Fatalf("continuation = %#x, %#x, %t", pc, top, ok)
	}
	for _, offset := range []int{64, 72, 80, 96, 104, 144, tailRAPublicRecordBytes, tailRAPublicRecordBytes + int(abi.RV64InvocationFirstRAOffset)} {
		original := binary.LittleEndian.Uint64(data[offset:])
		put(offset, uintptr(original)^8)
		if _, _, ok := read(); ok {
			t.Fatalf("damaged record word %d accepted", offset)
		}
		put(offset, uintptr(original))
	}
	for _, bounds := range [][2]uintptr{{record + 16, record + tailRAInvocationRecordBytes}, {record, record + tailRAInvocationRecordBytes - 16}, {record + 16, record}} {
		if _, _, ok := NativeTailHostContinuation(record+tailRACtrlOffset, home, context, record+tailRAPublicRecordBytes, bounds[0], bounds[1]); ok {
			t.Fatal("invalid stack bounds accepted")
		}
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if _, _, ok := read(); !ok {
			panic("valid continuation rejected")
		}
	}); allocations != 0 {
		t.Fatalf("continuation allocations = %g", allocations)
	}
}
