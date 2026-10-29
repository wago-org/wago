package wago

import (
	"testing"
)

func TestInternalEntryValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []int
		valid   bool
	}{
		{"legacy empty", nil, true},
		{"first byte", []int{0}, true},
		{"last byte", []int{1}, true},
		{"end", []int{2}, false},
		{"length", []int{0, 1}, false},
		{"negative", []int{-1}, false},
		{"compiler native bounded marker", []int{int(nativeScalarBoundedMask)}, true},
		{"compiler Go host dispatch marker", []int{int(goHostDispatchTagMask)}, true},
		{"compiler integer host context marker", []int{int(integerHostContextMask)}, true},
		{"compiler marker", []int{markDirectPreparedEntry(0)}, true},
		{"compiler native leaf marker", []int{int(directPreparedEntryMask >> 4)}, true},
		{"compiler invalid offset", []int{markDirectPreparedEntry(2)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Compiled{code: []byte{0, 0}, Entry: []int{0}, InternalEntry: tc.entries}
			if err := c.validateInternalEntries(); (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
}
