//go:build arm64

package arm64

import (
	"encoding/binary"
	"os"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestRepeatedStoreRegion(t *testing.T) {
	group := []uint32{0xf900061f, nopWord, 0xd2800031, 0xb9000611}
	makeCode := func() []byte {
		words := append(append(append([]uint32{}, group...), 0xb86c6b4c, 0x1100058c), group...)
		words = append(words, 0xd65f03c0)
		b := make([]byte, len(words)*4)
		for i, w := range words {
			binary.LittleEndian.PutUint32(b[i*4:], w)
		}
		return b
	}
	check := func(b []byte, entries []int, want bool) {
		t.Helper()
		got := repeatStoreRegionAt(b, 0, 24, 4, nativeConstantTargets(b), entries)
		if got != want {
			t.Fatalf("match=%v want=%v", got, want)
		}
	}
	b := makeCode()
	check(b, nil, true)
	for at := 24; at < 40; at += 4 {
		if rdWord(b, at) != nopWord {
			t.Fatal("second region not removed")
		}
	}
	b = makeCode()
	check(b, []int{20}, false)
	b = makeCode()
	wrWord(b, 16, 0xb9000200)
	check(b, nil, false)
	b = makeCode()
	wrWord(b, 16, 0xd2800010)
	check(b, nil, false)
	b = makeCode()
	wrWord(b, 16, 0x910043ff)
	check(b, nil, false) // Stack adjustments are not in the effect mask.
	b = makeCode()
	wrWord(b, 0, 0xf90003ff)
	wrWord(b, 24, 0xf90003ff)
	check(b, nil, false) // SP changes are not tracked by the register proof.
	b = makeCode()
	wrWord(b, 0, 0xb9000611)
	wrWord(b, 24, 0xb9000611)
	check(b, nil, false)
}

func TestRepeatedStoreRegionDiagnostic(t *testing.T) {
	path := os.Getenv("WAGO_REPEAT_STORE_DIAGNOSTIC_PATH")
	if path == "" {
		t.Skip("diagnostic only")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := repeatStoreRegionAt(b, 0x948, 0x9e8, 20, nativeConstantTargets(b), nil)
	t.Logf("real image matched=%v", got)
}

func TestRepeatedStoreLinearAddressAdmission(t *testing.T) {
	words := []uint32{0x8b130350, 0xf900061f, nopWord, 0xd2800031, 0xb9000611}
	code := make([]byte, len(words)*4)
	for i, w := range words {
		binary.LittleEndian.PutUint32(code[i*4:], w)
	}
	if !repeatLinearStoreRegion(code, 4, 4, nil, nil) {
		t.Fatal("linear-memory region rejected")
	}
	if repeatLinearStoreRegion(code, 4, 4, nil, []int{4}) {
		t.Fatal("accepted entry bypassing the linear address")
	}
	wrWord(code, 4, 0xf900065f) // STR XZR,[X18,#8]
	if repeatLinearStoreRegion(code, 4, 4, nil, nil) {
		t.Fatal("accepted unrelated native address")
	}
	wrWord(code, 4, 0xf900061f)
	wrWord(code, 12, 0xd2800030) // MOV X16,#1
	if repeatLinearStoreRegion(code, 4, 4, nil, nil) {
		t.Fatal("accepted clobbered linear address")
	}
}

func TestRepeatedStoreOrigins(t *testing.T) {
	words := []uint32{0xf900061f, nopWord, 0xd2800031, 0xb9000611, 0xf900061f, nopWord, 0xd2800031, 0xb9000611}
	code := make([]byte, len(words)*4)
	for i, w := range words {
		binary.LittleEndian.PutUint32(code[i*4:], w)
	}
	var origins [1]uint64
	for _, pc := range []int{0, 12, 16, 28} {
		branchTargetAdd(origins[:], pc, len(code))
	}
	if !repeatedStoreOriginsCover(code, 0, 16, 4, origins[:]) {
		t.Fatal("recorded Wasm stores rejected")
	}
	origins[0] &^= uint64(1) << (28 / 4)
	if repeatedStoreOriginsCover(code, 0, 16, 4, origins[:]) {
		t.Fatal("accepted an unrecorded native store")
	}
}

func TestRepeatedStoreOriginsDiscardedOnTruncate(t *testing.T) {
	f := fn{a: &a64.Asm{B: make([]byte, 64)}, sc: &scratch{repeatedStoreOriginsSet: true}}
	for _, pc := range []int{4, 12, 20} {
		branchTargetAdd(f.sc.repeatedStoreOrigins[:], pc, 64)
	}
	f.truncateNativeCode(12)
	if len(f.a.B) != 12 || !branchTargeted(f.sc.repeatedStoreOrigins[:], 4) || branchTargeted(f.sc.repeatedStoreOrigins[:], 12) || branchTargeted(f.sc.repeatedStoreOrigins[:], 20) {
		t.Fatal("truncation did not discard the discarded offsets")
	}
}

// Native offsets can be reused after an emitter rollback. No discarded store
// may retain its source provenance, including across bitmap-word boundaries.
func TestRepeatedStoreOriginsDiscardedAcrossWords(t *testing.T) {
	for _, cut := range []int{0, 4, 252, 256, 260, 16380, 16384} {
		f := fn{a: &a64.Asm{B: make([]byte, 16400)}, sc: &scratch{repeatedStoreOriginsSet: true}}
		for _, pc := range []int{0, 4, 248, 252, 256, 260, 16376, 16380} {
			branchTargetAdd(f.sc.repeatedStoreOrigins[:], pc, 16384)
		}
		f.truncateNativeCode(cut)
		for _, pc := range []int{0, 4, 248, 252, 256, 260, 16376, 16380} {
			if got := branchTargeted(f.sc.repeatedStoreOrigins[:], pc); got != (pc < cut) {
				t.Fatalf("cut=%d pc=%d retained=%v", cut, pc, got)
			}
		}
	}
}
