//go:build arm64

package arm64

import (
	"encoding/binary"
	"strings"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func testCallRelocTable(t testing.TB, functions ...[]callReloc) *callRelocTable {
	t.Helper()
	count := 0
	for _, relocs := range functions {
		count += len(relocs)
	}
	table := newCallRelocTable(len(functions), count)
	for i, relocs := range functions {
		if !table.appendFunction(i, relocs) {
			t.Fatalf("append function %d relocations", i)
		}
	}
	return &table
}

func TestGCObjectStubFarCallUsesHelper(t *testing.T) {
	for _, test := range []struct {
		name       string
		site, stub int
		delta      uint32
		want       uint32
	}{
		{"last BL displacement", 0, (1 << 27) - 4, 4, 0x95ffffff},
		{"first far displacement", 0, 1 << 27, 4, 0x14000001},
		{"moved function prefix", 8, 8 + 1<<27, 8, 0x14000002},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := a64.Asm{B: make([]byte, 20)}
			binary.LittleEndian.PutUint32(a.B[test.site:], 0x94000000)
			if !patchGCObjectCall(&a, test.site, test.stub, test.delta) {
				t.Fatal("valid native or helper branch rejected")
			}
			if got := binary.LittleEndian.Uint32(a.B[test.site:]); got != test.want {
				t.Fatalf("branch=%#x, want%#x", got, test.want)
			}
		})
	}
	for _, delta := range []uint32{0, 2, 8, invalidCallRelocField} {
		a := a64.Asm{B: make([]byte, 8)}
		binary.LittleEndian.PutUint32(a.B, 0x94000000)
		if patchGCObjectCall(&a, 0, 1<<27, delta) {
			t.Fatalf("accepted invalid helper delta%d", delta)
		}
	}
}

func TestPatchCallRelocsRangeChecks(t *testing.T) {
	t.Run("invalid site", func(t *testing.T) {
		err := patchCallRelocs(make([]byte, 4), []int{0}, []int{0}, testCallRelocTable(t, []callReloc{{at: invalidCallRelocField}}))
		if err == nil || !strings.Contains(err.Error(), "invalid relocation site") {
			t.Fatalf("invalid-site error = %v", err)
		}
	})
	t.Run("invalid target", func(t *testing.T) {
		err := patchCallRelocs(make([]byte, 4), []int{0}, []int{0}, testCallRelocTable(t, []callReloc{{target: invalidCallRelocField}}))
		if err == nil || !strings.Contains(err.Error(), "invalid call relocation target") {
			t.Fatalf("invalid-target error = %v", err)
		}
	})
	t.Run("forward", func(t *testing.T) {
		code := make([]byte, 8)
		binary.LittleEndian.PutUint32(code, 0x94000000) // BL placeholder
		err := patchCallRelocs(code, []int{0, 4}, []int{0, 4}, testCallRelocTable(t, []callReloc{{at: 0, target: 1}}, nil))
		if err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint32(code); got != 0x94000001 {
			t.Fatalf("patched forward BL = %#x, want %#x", got, uint32(0x94000001))
		}
	})
	t.Run("backward", func(t *testing.T) {
		code := make([]byte, 8)
		binary.LittleEndian.PutUint32(code[4:], 0x94000000)
		err := patchCallRelocs(code, []int{0, 4}, []int{0, 4}, testCallRelocTable(t, nil, []callReloc{{at: 0, target: 0}}))
		if err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint32(code[4:]); got != 0x97ffffff {
			t.Fatalf("patched backward BL = %#x, want %#x", got, uint32(0x97ffffff))
		}
	})
	t.Run("internal entry", func(t *testing.T) {
		code := make([]byte, 12)
		binary.LittleEndian.PutUint32(code, 0x94000000)
		err := patchCallRelocs(code, []int{0, 4}, []int{0, 8}, testCallRelocTable(t, []callReloc{{at: 0, target: 1, internal: true}}, nil))
		if err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint32(code); got != 0x94000002 {
			t.Fatalf("patched internal-entry BL = %#x, want %#x", got, uint32(0x94000002))
		}
	})
	t.Run("maximum forward displacement", func(t *testing.T) {
		code := make([]byte, 4)
		binary.LittleEndian.PutUint32(code, 0x94000000)
		target := (1 << 27) - 4
		err := patchCallRelocs(code, []int{0, target}, []int{0, target}, testCallRelocTable(t, []callReloc{{at: 0, target: 1}}, nil))
		if err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint32(code); got != 0x95ffffff {
			t.Fatalf("maximum-range BL = %#x, want %#x", got, uint32(0x95ffffff))
		}
	})
	t.Run("out of range", func(t *testing.T) {
		code := make([]byte, 4)
		binary.LittleEndian.PutUint32(code, 0x94000000)
		target := 1 << 27
		err := patchCallRelocs(code, []int{0, target}, []int{0, target}, testCallRelocTable(t, []callReloc{{at: 0, target: 1}}, nil))
		if err == nil || !strings.Contains(err.Error(), "exceeds BL range") {
			t.Fatalf("out-of-range relocation error = %v", err)
		}
	})
}

func TestCallRelocTableRequiresFunctionOrder(t *testing.T) {
	table := newCallRelocTable(2, 1)
	if table.appendFunction(1, nil) {
		t.Fatal("accepted out-of-order function")
	}
	if !table.appendFunction(0, []callReloc{{target: 1}}) || !table.appendFunction(1, nil) {
		t.Fatal("rejected ordered functions")
	}
	if got := table.serialFunction(0); len(got) != 1 || got[0].target != 1 {
		t.Fatalf("function 0 relocations = %+v", got)
	}
	if table.appendFunction(1, nil) {
		t.Fatal("accepted duplicate function")
	}
}

func TestGCObjectStubRelocation(t *testing.T) {
	code := make([]byte, 8)
	binary.LittleEndian.PutUint32(code, 0x94000000)
	table := testCallRelocTable(t, []callReloc{{at: 0, target: invalidCallRelocField, gcObject: true}})
	if !needsGCObjectStub(table) {
		t.Fatal("missing checked-object leaf")
	}
	if err := patchCallRelocs(code, []int{0}, []int{0}, table); err == nil {
		t.Fatal("accepted missing checked-object leaf")
	}
	table.gcObjectEntry = len(code)
	code = emitGCObjectStub(code)
	if len(code)-table.gcObjectEntry > 256 {
		t.Fatal("checked-object leaf exceeds reserved tail")
	}
	if err := patchCallRelocs(code, []int{0}, []int{0}, table); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(code); got != 0x94000002 {
		t.Fatalf("checked-object BL = %#x, want %#x", got, uint32(0x94000002))
	}
}
