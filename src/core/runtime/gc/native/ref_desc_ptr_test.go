package gc

import (
	"encoding/binary"
	"testing"
)

func TestRefDescPtrReturnsCanonicalDescriptor(t *testing.T) {
	d, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{d})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.refDescPtr(r)
	if err != nil {
		t.Fatal(err)
	}
	if got != &c.types[0] {
		t.Fatal("object descriptor was copied")
	}
	got, object, err := c.refDescPtrBytes(r)
	if err != nil {
		t.Fatal(err)
	}
	if got != &c.types[0] || TypeID(binary.LittleEndian.Uint32(object)) != got.ID {
		t.Fatal("object bytes and descriptor disagree")
	}
	if _, err := c.refDescPtr(Null()); err == nil {
		t.Fatal("null reference accepted")
	}
}
