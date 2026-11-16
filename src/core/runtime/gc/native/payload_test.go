//go:build amd64 && !tinygo && !wago_target_tinygo

package gc

import (
	"bytes"
	"encoding/binary"
	"testing"
	"unsafe"
)

func TestPayloadRelativeStoragePreservesLayouts(t *testing.T) {
	for _, mode := range []string{"throughput", "tiny", "direct"} {
		t.Run(mode, func(t *testing.T) {
			sd, _ := NewStructDesc(0, []StorageKind{StorageI32, StorageI64})
			sd.Final = true
			ad, _ := NewArrayDesc(1, StorageI16)
			ad.Final = true
			config := Config{}
			if mode == "tiny" {
				config.Profile = ProfileTiny
			}
			var c *Collector
			var err error
			if mode == "direct" {
				c, err = NewExperimentalDirectCollector(config, []TypeDesc{sd, ad})
			} else {
				c, err = NewCollector(config, []TypeDesc{sd, ad})
			}
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			r, err := c.NewStructWithRoots(0, []Value{I32Value(7), I64Value(99)}, EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			root := Root(r)
			ar, err := c.NewArrayFixedWithRoots(1, []Value{{Kind: StorageI16, Bits: 1}, {Kind: StorageI16, Bits: 2}, {Kind: StorageI16, Bits: 3}}, Slots{&root})
			if err != nil {
				t.Fatal(err)
			}
			arRoot := Root(ar)
			r = Ref(root)
			header := append([]byte{}, c.bytes(r)[:PayloadOffset]...)
			payload := c.payloadBytes(r)
			if &payload[0] != &c.bytes(r)[PayloadOffset] {
				t.Fatal("payload copied")
			}
			binary.LittleEndian.PutUint32(payload[sd.Fields[0].Offset:], 31)
			if !bytes.Equal(header, c.bytes(r)[:PayloadOffset]) {
				t.Fatal("payload write touched header")
			}
			if v, e := c.StructGet(r, 0); e != nil || v.Bits != 31 {
				t.Fatal(v, e)
			}
			if v, _, ok, e := c.StructGetTyped(r, 0, true, 1); e != nil || !ok || v.Bits != 99 {
				t.Fatal(v, ok, e)
			}
			if e := c.StructSet(r, 1, I64Value(123)); e != nil {
				t.Fatal(e)
			}
			if h, p, e := c.checkedHeaderPayload(r); e != nil || h.TypeID != 0 || &p[0] != &payload[0] {
				t.Fatal(h, e)
			}
			view, e := c.ArrayPayload(ar)
			if e != nil {
				t.Fatal(e)
			}
			if len(view.Bytes) != 6 || cap(view.Bytes) != 6 {
				t.Fatal("logical borrow widened")
			}
			binary.LittleEndian.PutUint16(view.Bytes[2:], 77)
			if e := c.ArrayCopy(ar, 1, ar, 0, 2); e != nil {
				t.Fatal(e)
			}
			if e := c.ArrayInitData(ar, 0, []byte{8, 0}, 0, 1); e != nil {
				t.Fatal(e)
			}
			if e := c.CollectFull(Slots{&root, &arRoot}); e != nil {
				t.Fatal(e)
			}
			for i, want := range []uint64{8, 1, 77} {
				v, e := c.ArrayGet(Ref(arRoot), uint32(i))
				if e != nil || v.Bits != want {
					t.Fatal(i, v, e)
				}
			}
			if v, e := c.StructGet(Ref(root), 1); e != nil || v.Bits != 123 {
				t.Fatal(v, e)
			}
			if a := testing.AllocsPerRun(20, func() { _, _ = c.StructGet(Ref(root), 0); _ = c.StructSet(Ref(root), 0, I32Value(31)) }); a != 0 {
				t.Fatalf("payload access allocations %v", a)
			}
			t.Logf("Collector bytes=%d", unsafe.Sizeof(*c))
		})
	}
}

func TestPayloadRelativeBoundsDoNotTouchHeader(t *testing.T) {
	b := make([]byte, 24)
	copy(b, []byte("header protected"))
	before := append([]byte{}, b[:16]...)
	if e := storeValueBytes(objectPayload(b), ^uint64(0), StorageI32, I32Value(1)); e == nil {
		t.Fatal("overflow accepted")
	}
	if _, e := loadValueBytes(objectPayload(b[:8]), 0, StorageI32); e == nil {
		t.Fatal("short header accepted")
	}
	if !bytes.Equal(before, b[:16]) {
		t.Fatal("header mutated")
	}
}

func TestPayloadViewRejectsShortHeaderEvenEmpty(t *testing.T) {
	d, _ := NewArrayDesc(0, StorageI8)
	c, e := NewCollector(Config{}, []TypeDesc{d})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	r, e := c.NewArrayDefault(0, 0)
	if e != nil {
		t.Fatal(e)
	}
	if v, e := c.ArrayPayload(r); e != nil || len(v.Bytes) != 0 {
		t.Fatal(v, e)
	}
	entry := c.entry(r)
	size := entry.size
	entry.size = 8
	defer func() { entry.size = size }()
	if _, e := c.ArrayPayload(r); e == nil {
		t.Fatal("short physical header accepted for empty logical array")
	}
}
