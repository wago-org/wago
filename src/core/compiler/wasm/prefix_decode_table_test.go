package wasm

import (
	"testing"
	"unsafe"
)

func TestPrefixLookupTablesStayCompactAndComplete(t *testing.T) {
	tables := []struct {
		name string
		data []InstrKind
		want int
	}{
		{name: "fc-no-immediate", data: fcNoImm[:], want: 8},
		{name: "fb-no-immediate", data: fbNoImm[:], want: 6},
		{name: "fe-memory", data: feMem[:], want: 17},
		{name: "fd-memory", data: fdMem[:], want: 22},
		{name: "fd-lane", data: fdLane[:], want: 14},
		{name: "fd-no-immediate", data: fdNoImm[:], want: 218},
	}
	for _, table := range tables {
		count := 0
		for sub, kind := range table.data {
			got, ok := lookupPrefixKind(table.data, uint32(sub))
			if kind == InstrInvalid {
				if ok || got != InstrInvalid {
					t.Errorf("%s subopcode %d invalid lookup = (%v, %v)", table.name, sub, got, ok)
				}
				continue
			}
			count++
			if !ok || got != kind {
				t.Errorf("%s subopcode %d lookup = (%v, %v), want (%v, true)", table.name, sub, got, ok, kind)
			}
		}
		if count != table.want {
			t.Errorf("%s populated entries = %d, want %d", table.name, count, table.want)
		}
		if got, ok := lookupPrefixKind(table.data, uint32(len(table.data))); ok || got != InstrInvalid {
			t.Errorf("%s out-of-range lookup = (%v, %v)", table.name, got, ok)
		}
	}
	if got := unsafe.Sizeof(fcNoImm) + unsafe.Sizeof(fbNoImm) + unsafe.Sizeof(feMem) + unsafe.Sizeof(fdMem) + unsafe.Sizeof(fdLane) + unsafe.Sizeof(fdNoImm); got != 948 {
		t.Fatalf("prefix lookup storage = %d bytes, want 948", got)
	}
}
