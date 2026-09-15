package gc

import (
	"encoding/binary"
	"testing"
)

func TestArrayPayloadViewExposesOnlyLogicalPayload(t *testing.T) {
	desc, err := NewArrayDesc(0, StorageI16)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ref, err := c.NewArrayFixedWithRoots(0, []Value{
		{Kind: StorageI16, Bits: 0x1122},
		{Kind: StorageI16, Bits: 0x3344},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := c.ArrayPayload(ref)
	if err != nil {
		t.Fatal(err)
	}
	if view.Storage != StorageI16 || view.Length != 2 || len(view.Bytes) != 4 {
		t.Fatalf("payload view = storage %d length %d bytes %d", view.Storage, view.Length, len(view.Bytes))
	}
	if got := binary.LittleEndian.Uint16(view.Bytes[:2]); got != 0x1122 {
		t.Fatalf("payload[0] = %#x", got)
	}
	binary.LittleEndian.PutUint16(view.Bytes[2:], 0x5566)
	got, err := c.ArrayGet(ref, 1)
	if err != nil || got.Bits != 0x5566 {
		t.Fatalf("array.get after payload write = %+v, %v", got, err)
	}
}

func TestArrayPayloadViewRejectsNonArray(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ref, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ArrayPayload(ref); err == nil {
		t.Fatal("struct exposed an array payload view")
	}
}
