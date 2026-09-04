//go:build amd64

package amd64

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestPatchModuleRelocsValidatesTablesAndSites(t *testing.T) {
	var stubOffsets [gcSharedStubMax]int
	for i := range stubOffsets {
		stubOffsets[i] = -1
	}
	tests := []struct {
		name          string
		code          []byte
		entry         []int
		internalEntry []int
		relocs        [][]callReloc
		stubBase      int
		want          string
	}{
		{name: "entry table", code: make([]byte, 8), relocs: make([][]callReloc, 1), want: "entry table"},
		{name: "function entry", code: make([]byte, 8), entry: []int{9}, relocs: make([][]callReloc, 1), want: "invalid function 0 entry"},
		{name: "invalid site sentinel", code: make([]byte, 8), entry: []int{0}, relocs: [][]callReloc{{{at: invalidCallRelocField}}}, want: "invalid relocation site"},
		{name: "truncated site", code: make([]byte, 8), entry: []int{0}, relocs: [][]callReloc{{{at: 5}}}, want: "invalid relocation site"},
		{name: "invalid target sentinel", code: make([]byte, 8), entry: []int{0}, relocs: [][]callReloc{{{target: invalidCallRelocField}}}, want: "invalid call relocation target"},
		{name: "missing internal entry", code: make([]byte, 8), entry: []int{0}, relocs: [][]callReloc{{{at: 0, internal: true}}}, want: "missing internal entry"},
		{name: "missing shared stub", code: make([]byte, 8), entry: []int{0}, relocs: [][]callReloc{{{at: 0, gcStub: gcSharedStubResolveObject}}}, stubBase: -1, want: "missing shared GC stub"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := patchModuleRelocs(tc.code, tc.entry, tc.internalEntry, tc.relocs, tc.stubBase, stubOffsets)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("patch error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestPatchModuleRelocsPatchesOrdinaryInternalAndSharedCalls(t *testing.T) {
	code := make([]byte, 64)
	entry := []int{0, 32}
	internalEntry := []int{8, 40}
	var stubOffsets [gcSharedStubMax]int
	for i := range stubOffsets {
		stubOffsets[i] = -1
	}
	stubOffsets[gcSharedStubResolveObject] = 4
	relocs := [][]callReloc{
		{
			{at: 0, target: 1},
			{at: 4, target: 1, internal: true},
			{at: 8, gcStub: gcSharedStubResolveObject},
		},
		nil,
	}
	if err := patchModuleRelocs(code, entry, internalEntry, relocs, 48, stubOffsets); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		site, target int
	}{{0, 32}, {4, 40}, {8, 52}} {
		got := int32(binary.LittleEndian.Uint32(code[tc.site:]))
		want := int32(tc.target - (tc.site + 4))
		if got != want {
			t.Fatalf("site %#x delta = %d, want %d", tc.site, got, want)
		}
	}
}
