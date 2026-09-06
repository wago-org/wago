package wago

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestStagedGCStructGlobalSupportsMoreThanFourFields(t *testing.T) {
	fields := make([]gc.StorageKind, 6)
	bits := make([]uint64, len(fields))
	for i := range fields {
		fields[i] = gc.StorageI32
		bits[i] = uint64(i + 10)
	}
	desc, err := gc.NewStructDesc(0, fields)
	if err != nil {
		t.Fatal(err)
	}
	collector, err := gc.NewCollector(gc.Config{}, []gc.TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer collector.Close()
	ref, slot, err := instantiateGCStructGlobal(collector, nil, []gc.TypeDesc{desc}, gcStructGlobalInit{TypeID: 0, FieldCount: uint32(len(fields)), Bits: bits})
	if err != nil {
		t.Fatal(err)
	}
	if rooted, err := collector.CheckedGlobalSlot(slot); err != nil || rooted != ref {
		t.Fatalf("wide struct global root = %v, %v; want %v", rooted, err, ref)
	}
	for i := range fields {
		value, err := collector.StructGet(ref, uint32(i))
		if err != nil || value.Bits != bits[i] {
			t.Fatalf("wide struct field %d = %+v, %v", i, value, err)
		}
	}
}

func TestStagedGCArrayGlobalSupportsOldLengthBoundaries(t *testing.T) {
	desc, err := gc.NewArrayDesc(0, gc.StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	collector, err := gc.NewCollector(gc.Config{}, []gc.TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer collector.Close()
	const length = 20
	bits := make([]uint64, length)
	for i := range bits {
		bits[i] = uint64(i + 1)
	}
	ref, _, err := instantiateGCArrayGlobal(collector, nil, []gc.TypeDesc{desc}, gcArrayGlobalInit{TypeID: 0, Length: length, Mode: gcArrayGlobalInitFixed, Bits: bits}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := collector.ArrayLen(ref); err != nil || got != length {
		t.Fatalf("wide array global length = %d, %v", got, err)
	}
	value, err := collector.ArrayGet(ref, length-1)
	if err != nil || value.Bits != length {
		t.Fatalf("wide array global last value = %+v, %v", value, err)
	}
}

func TestStagedGCArrayElementSegmentSupportsMoreThanTwoValues(t *testing.T) {
	desc, err := gc.NewArrayDesc(0, gc.StorageI8)
	if err != nil {
		t.Fatal(err)
	}
	collector, err := gc.NewCollector(gc.Config{}, []gc.TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer collector.Close()
	const count = 5
	values := make([]gcArrayElementValueInit, count)
	for i := range values {
		values[i] = gcArrayElementValueInit{Mode: gcArrayGlobalInitUniform, Length: 1, Bits: []uint64{uint64(i + 1)}}
	}
	descriptor := make([]byte, 16)
	state, err := instantiateGCArrayElementSegment(collector, nil, []gc.TypeDesc{desc}, &gcArrayElementInit{TypeID: 0, Count: count, Values: values}, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if state.Count != count || len(state.Refs) != count || binary.LittleEndian.Uint32(descriptor[8:]) != count {
		t.Fatalf("wide element state = %+v descriptor=%x", state, descriptor)
	}
	for i, ref := range state.Refs {
		value, err := collector.ArrayGet(ref, 0)
		if err != nil || value.Bits != uint64(i+1) {
			t.Fatalf("wide element value %d = %+v, %v", i, value, err)
		}
	}
	state.drop(collector)
}

func TestStagedGCRefTestRootTableSupportsMoreThanTwentySlots(t *testing.T) {
	desc, err := gc.NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	collector, err := gc.NewCollector(gc.Config{}, []gc.TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer collector.Close()
	const slots = 25
	descriptor := make([]byte, 8+slots*8)
	binary.LittleEndian.PutUint32(descriptor, slots)
	binary.LittleEndian.PutUint32(descriptor[4:], slots)
	state, err := newGCRefTestTableState(collector, [][]byte{descriptor}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.Count != slots || len(state.Slots) != slots {
		t.Fatalf("wide ref.test root table = count %d slots %d", state.Count, len(state.Slots))
	}
	state.drop(collector)
}
