package arm64

import "testing"

func TestPatchBranch26RejectsOutOfRangeTarget(t *testing.T) {
	a := &Asm{B: make([]byte, 4)}
	if a.PatchBranch26(0, 1<<27) {
		t.Fatal("PatchBranch26 accepted +128 MiB target")
	}
	if a.PatchBranch26(0, -(1<<27)-4) {
		t.Fatal("PatchBranch26 accepted below -128 MiB target")
	}
}
