//go:build amd64

package amd64

import "testing"

func TestSelfTailEntryCompareDecoding(t *testing.T) {
	for _, tc := range []struct {
		code     []byte
		size     int
		reg      Reg
		wide, ok bool
	}{
		{[]byte{0x45, 0x85, 0xc9}, 3, R9, false, true},
		{[]byte{0x4d, 0x85, 0xc9}, 3, R9, true, true},
		{[]byte{0x4d, 0x39, 0xd1}, 3, regNone, true, true},
		{[]byte{0x41, 0x83, 0xf9, 0}, 4, R9, false, true},
		{[]byte{0x49, 0x81, 0xf9, 0, 0, 0, 0}, 7, R9, true, true},
		{[]byte{0x41, 0x83, 0xf9, 7}, 4, regNone, false, true},
		{[]byte{0x41, 0x85, 0x09}, 0, regNone, false, false},
		{[]byte{0x41, 0x81, 0xf9, 0}, 0, regNone, false, false},
		{[]byte{0x41}, 0, regNone, false, false},
		{[]byte{0x45, 0x01, 0xc9}, 0, regNone, false, false},
	} {
		size, reg, wide, ok := selfTailEntryCompare(tc.code)
		if size != tc.size || reg != tc.reg || wide != tc.wide || ok != tc.ok {
			t.Fatalf("%x got=%d,%v,%t,%t want=%d,%v,%t,%t", tc.code, size, reg, wide, ok, tc.size, tc.reg, tc.wide, tc.ok)
		}
	}
}

func TestSelfTailAssignmentZeroFlagsRequireWholeInstruction(t *testing.T) {
	for _, tc := range []struct {
		code       []byte
		wide, want bool
	}{
		{[]byte{0x41, 0x83, 0xe9, 1}, false, true},
		{[]byte{0x49, 0x83, 0xe9, 1}, true, true},
		{[]byte{0x41, 0x83, 0xc1, 1}, false, true},
		{[]byte{0x49, 0x81, 0xe9, 1, 0, 0, 0}, true, true},
		{[]byte{0x49, 0x83, 0xe9, 1}, false, false},
		{[]byte{0x41, 0x83, 0xe8, 1}, false, false},
		{[]byte{0x41, 0x83, 0xf9, 0}, false, false},
		{[]byte{0x41, 0xb9, 0x41, 0x83, 0xe9, 1}, false, false}, // MOV immediate contains a fake SUB suffix
		{[]byte{0x41, 0x83, 0xe9, 1, 0x45, 0x89, 0xc9}, false, false},
		{[]byte{0x41, 0x83, 0x29, 1}, false, false}, // memory destination
	} {
		if got := selfTailAssignmentSetsZero(tc.code, R9, tc.wide); got != tc.want {
			t.Fatalf("%x wide=%t got=%t want %t", tc.code, tc.wide, got, tc.want)
		}
	}
}
