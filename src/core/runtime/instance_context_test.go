//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestInstanceContextBytesReserveNativeTailMetadata(t *testing.T) {
	jm, err := NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	buf := make([]byte, InstanceContextBytes)
	for i := range buf {
		buf[i] = 0xff
	}
	jm.CaptureInstanceContextBytes(buf)
	for i, value := range buf[InstanceContextGCDomainOffset:] {
		if value != 0 {
			t.Fatalf("native context metadata byte %d = %#x, want zero", i, value)
		}
	}
	binary.LittleEndian.PutUint64(buf[InstanceContextGCDomainOffset:], 7)
	binary.LittleEndian.PutUint64(buf[InstanceContextTailCodeOffset:], 11)
	binary.LittleEndian.PutUint64(buf[InstanceContextTailHomeOffset:], 13)
	binary.LittleEndian.PutUint64(buf[InstanceContextTailTargetCtxOffset:], 17)
	binary.LittleEndian.PutUint64(buf[InstanceContextGCNativeViewOffset:], 19)
	binary.LittleEndian.PutUint64(buf[InstanceContextProfileCountersOffset:], 23)
	binary.LittleEndian.PutUint64(buf[InstanceContextTierEntriesOffset:], 29)
	jm.BindInstanceContextBytes(buf)
	if got := binary.LittleEndian.Uint64(buf[InstanceContextGCDomainOffset:]); got != 7 {
		t.Fatalf("binding pointer context rewrote GC domain metadata: %d", got)
	}
	if got := jm.GCNativeViewPtr(); got != 19 {
		t.Fatalf("binding native context GC view = %d, want 19", got)
	}
	if got := jm.ProfileCountersPtr(); got != 23 {
		t.Fatalf("binding native context profile counters = %d, want 23", got)
	}
	if got := jm.TierEntriesPtr(); got != 29 {
		t.Fatalf("binding native context tier entries = %d, want 29", got)
	}
}

func TestBindInstanceContextBytesAcceptsUnalignedSource(t *testing.T) {
	jm, err := NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()

	backing := make([]byte, InstanceContextBytes+1)
	src := backing[1:]
	for i := 0; i < InstanceContextGCDomainOffset/8; i++ {
		binary.LittleEndian.PutUint64(src[i*8:], uint64(i+1))
	}
	binary.LittleEndian.PutUint64(src[InstanceContextGCNativeViewOffset:], 10)
	binary.LittleEndian.PutUint64(src[InstanceContextProfileCountersOffset:], 11)
	binary.LittleEndian.PutUint64(src[InstanceContextTierEntriesOffset:], 12)
	jm.BindInstanceContextBytes(src)
	want := InstanceContext{
		CustomCtx: 1, TablePtr: 2, FuncRefDescPtr: 3, PassiveElemPtr: 4,
		GlobalsPtr: 5, PassiveDataPtr: 6, TableDirPtr: 7, MemoryDirPtr: 8,
		ImportDispatch: 9, ProfileCounters: 11, TierEntries: 12,
	}
	if got := jm.CaptureInstanceContext(); got != want {
		t.Fatalf("InstanceContext = %+v, want %+v", got, want)
	}
	if got := jm.GCNativeViewPtr(); got != 10 {
		t.Fatalf("GC native view = %d, want 10", got)
	}
}

func TestInstanceContextRoundTripLeavesMemoryAndInvocationState(t *testing.T) {
	jm, err := NewJobMemoryGrowable(65536, 4*65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()

	jm.SetStackFence(0x1111)
	trap := make([]byte, 8)
	if err := jm.BindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	beforeBytes := jm.curBytes()
	beforeFence := jm.getU64(offStackFence)
	beforeTrap := jm.getU64(abi.TrapCellPtrOffset)

	want := InstanceContext{
		CustomCtx:      1,
		TablePtr:       2,
		FuncRefDescPtr: 3,
		PassiveElemPtr: 4,
		GlobalsPtr:     5,
		PassiveDataPtr: 6,
		TableDirPtr:    7,
		MemoryDirPtr:   8,
		ImportDispatch: 9,
	}
	jm.BindInstanceContext(want)
	if got := jm.CaptureInstanceContext(); got != want {
		t.Fatalf("InstanceContext = %+v, want %+v", got, want)
	}
	if jm.curBytes() != beforeBytes || jm.getU64(offStackFence) != beforeFence || jm.getU64(abi.TrapCellPtrOffset) != beforeTrap {
		t.Fatal("binding instance context changed memory or invocation state")
	}
}
