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

func TestRepeatedStoreRegionBranchesOverLongDeletion(t *testing.T) {
	group := []uint32{
		0xf900061f, nopWord, 0xd2800031, 0xb9000611,
		0xf9000a1f, nopWord, 0xd2800051, 0xb9000a11,
	}
	words := append(append(append([]uint32{}, group...), 0xb86c6b4c, 0x1100058c), group...)
	b := make([]byte, len(words)*4)
	for i, w := range words {
		binary.LittleEndian.PutUint32(b[i*4:], w)
	}
	second := (len(group) + 2) * 4
	if !repeatStoreRegionAt(b, 0, second, len(group), nativeConstantTargets(b), nil) {
		t.Fatal("long repeated region rejected")
	}
	if got, want := rdWord(b, second), uint32(0x14000000|len(group)); got != want {
		t.Fatalf("skip branch = %#x, want %#x", got, want)
	}
	for at := second + 4; at < second+4*len(group); at += 4 {
		if rdWord(b, at) != nopWord {
			t.Fatalf("word at %d was not cleared", at)
		}
	}
}

func TestRepeatedStoreRegionFindsDistantDuplicates(t *testing.T) {
	group := []uint32{
		0xf900061f, nopWord, 0xd2800031, 0xb9000611,
		0xf9000a1f, nopWord, 0xd2800051, 0xb9000a11,
	}
	words := append([]uint32{}, group...)
	for range 80 {
		words = append(words, 0x1100058c) // ADD W12,W12,#1; irrelevant to the store block.
	}
	second := len(words) * 4
	words = append(words, group...)
	for range 80 {
		words = append(words, 0x1100058c)
	}
	third := len(words) * 4
	words = append(words, group...)
	b := make([]byte, len(words)*4)
	for i, w := range words {
		binary.LittleEndian.PutUint32(b[i*4:], w)
	}
	targets := nativeConstantTargets(b)
	if !repeatedStoreRegionSafe(b, 0, second, len(group), targets, nil) {
		t.Fatal("distant duplicate rejected")
	}
	if !repeatedStoreRegionSafe(b, second, third, len(group), targets, nil) {
		t.Fatal("next link in distant duplicate chain rejected")
	}
	removeRepeatedStoreRegion(b, third, len(group))
	removeRepeatedStoreRegion(b, second, len(group))
	for _, at := range []int{second, third} {
		if got, want := rdWord(b, at), uint32(0x14000000|len(group)); got != want {
			t.Fatalf("skip branch at %d = %#x, want %#x", at, got, want)
		}
	}
}

func TestRepeatedStoreRegionDistanceBound(t *testing.T) {
	group := []uint32{0xf900061f, nopWord, 0xd2800031, 0xb9000611}
	second := len(group)*4 + maxRepeatedStoreDistance + 4
	b := make([]byte, second+len(group)*4)
	for i, w := range group {
		binary.LittleEndian.PutUint32(b[i*4:], w)
		binary.LittleEndian.PutUint32(b[second+i*4:], w)
	}
	if repeatStoreRegionAt(b, 0, second, len(group), nativeConstantTargets(b), nil) {
		t.Fatal("accepted duplicate beyond the bounded search distance")
	}
}

func TestRepeatedStoreChainBranchPrefix(t *testing.T) {
	const (
		words  = 4
		second = 32
		third  = 64
		branch = 80
	)
	b := make([]byte, 96)
	// This branch enters the final repeated region, so the first duplicate is
	// still removable but the final one is not.
	offset := int32((third + 4 - branch) / 4)
	wrWord(b, branch, 0x14000000|uint32(offset)&0x03ffffff)
	if got := repeatedStoreChainBranchPrefix(b, 0, []int{second, third}, words); got != 1 {
		t.Fatalf("safe prefix = %d, want 1", got)
	}
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

func TestScratchConstantReload(t *testing.T) {
	words := []uint32{
		0xd2800031, 0xb9000611, nopWord, // MOV X17,#1; STR W17,[X16,#4].
		0xd2800031, 0xb9000e11, // MOV X17,#1; STR W17,[X16,#12].
	}
	code := make([]byte, len(words)*4)
	for i, w := range words {
		binary.LittleEndian.PutUint32(code[i*4:], w)
	}
	if !scratchConstantReloadAt(code, 12, nil, nil) {
		t.Fatal("redundant scratch reload rejected")
	}
	if got := rdWord(code, 12); got != nopWord {
		t.Fatalf("reload = %#x, want nop", got)
	}
}

func TestScratchConstantReloadSafety(t *testing.T) {
	base := []uint32{
		0xd2800031, 0xb9000611, nopWord,
		0xd2800031, 0xb9000e11,
	}
	check := func(words []uint32, targets, entries []int, want bool) {
		t.Helper()
		code := make([]byte, len(words)*4)
		for i, w := range words {
			binary.LittleEndian.PutUint32(code[i*4:], w)
		}
		var targetBits [1]uint64
		for _, pc := range targets {
			branchTargetAdd(targetBits[:], pc, len(code))
		}
		if got := scratchConstantReloadAt(code, 12, targetBits[:], entries); got != want {
			t.Fatalf("match=%v, want %v", got, want)
		}
	}
	check(base, nil, nil, true)
	check(base, []int{12}, nil, false)
	check(base, nil, []int{12}, false)
	check(base, []int{8}, nil, false)
	check(base, nil, []int{8}, false)
	clobber := append([]uint32{}, base...)
	clobber[2] = 0xd2800051 // MOV X17,#2.
	check(clobber, nil, nil, false)
	different := append([]uint32{}, base...)
	different[3] = 0xd2800051 // Current materialization is MOV X17,#2.
	check(different, nil, nil, false)
	branch := append([]uint32{}, base...)
	branch[2] = 0x14000001 // B to the current MOVZ.
	check(branch, nil, nil, false)
}
