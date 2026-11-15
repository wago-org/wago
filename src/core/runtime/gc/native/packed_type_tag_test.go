package gc

import "testing"

// This is a new ABI invariant, not a claim that old-ABI inputs were broken.
func TestPackedTypeTagKeepsYoungMetadata(t *testing.T) {
	for _, tag := range []uint16{1, 127, 128, 129, 4095} {
		e := handleEntry{space: spaceNursery, class: 3}
		e.class |= (tag & 127) << 5
		e.space |= spaceKind((tag >> 7) << 3)
		if !e.young() {
			t.Fatalf("tag%d changed nursery identity", tag)
		}
		e.setYoungAge(2)
		got := ((e.class >> 5) & 127) | (uint16(e.space&0xf8) << 4)
		if got != tag || e.age() != 2 || e.class&handleClassMask != 3 {
			t.Fatalf("age update changed tag%d to%d", tag, got)
		}
		e.clearYoungAge()
		got = ((e.class >> 5) & 127) | (uint16(e.space&0xf8) << 4)
		if got != tag || e.class&handleClassMask != 3 {
			t.Fatalf("age clear changed tag%d to%d", tag, got)
		}
	}
}

func TestPackedTypeTagAllValuesAndInactive(t *testing.T) {
	for tag := uint16(0); tag < 4096; tag++ {
		for sp := spaceKind(0); sp < 8; sp++ {
			for _, class := range []uint16{0, 31, handleAbsorbedTailBit | 31, handleYoungBit | handleAgeMask | 31} {
				e := handleEntry{space: sp | spaceKind(tag>>7<<3), class: class | (tag&127)<<5}
				want := uint32(tag)
				if sp == spaceFree || sp >= NativeSpaceCount {
					want = 0
				}
				if e.logicalSpace() != sp || e.cachedTypeTag() != want {
					t.Fatalf("tag%d space%d class%x decoded%d/%d", tag, sp, class, e.logicalSpace(), e.cachedTypeTag())
				}
			}
		}
	}
}
