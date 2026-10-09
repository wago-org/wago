package wago

import (
	"strings"
	"testing"
)

func TestInternalEntryValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		entries  []int
		artifact bool
		valid    bool
	}{
		{"legacy empty", nil, true, true},
		{"first byte", []int{0}, true, true},
		{"last byte", []int{1}, true, true},
		{"end", []int{2}, true, false},
		{"length", []int{0, 1}, true, false},
		{"negative", []int{-1}, true, false},
		{"wire marker", []int{markDirectPreparedEntry(0)}, true, false},
		{"wire light marker", []int{int(directPreparedLightMask)}, true, false},
		{"wire bounded marker", []int{int(directPreparedBoundedMask)}, true, false},
		{"wire host segment marker", []int{int(directHostSegmentsMask)}, true, false},
		{"wire native bounded marker", []int{int(nativeScalarBoundedMask)}, true, false},
		{"compiler native bounded marker", []int{int(nativeScalarBoundedMask)}, false, true},
		{"wire Go host dispatch marker", []int{int(goHostDispatchTagMask)}, true, false},
		{"compiler Go host dispatch marker", []int{int(goHostDispatchTagMask)}, false, true},
		{"wire integer host context marker", []int{int(integerHostContextMask)}, true, integerHostContextMask == 0},
		{"compiler integer host context marker", []int{int(integerHostContextMask)}, false, true},
		{"wire native leaf marker", []int{int(nativeScalarLeafMask)}, true, false},
		{"compiler marker", []int{markDirectPreparedEntry(0)}, false, true},
		{"compiler native leaf marker", []int{int(directPreparedEntryMask >> 4)}, false, true},
		{"compiler invalid offset", []int{markDirectPreparedEntry(2)}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Compiled{code: []byte{0, 0}, Entry: []int{0}, InternalEntry: tc.entries}
			if err := c.validateInternalEntries(tc.artifact); (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
}

func TestMarshalRejectsInvalidInternalEntries(t *testing.T) {
	// Serialization requires explicit bounds even in a guard-page test build.
	c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(benchAddOneModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c = mutableCompiledFixture(c)
	c.InternalEntry = []int{len(c.code)}
	if _, err := c.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "InternalEntry") {
		t.Fatalf("MarshalBinary = %v", err)
	}
}
