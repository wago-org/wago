package shared

import "testing"

func TestStackArenaCapacity(t *testing.T) {
	if got := StackArenaCapacity(64, 0, 12); got != 19 {
		t.Fatalf("hinted capacity = %d, want 19", got)
	}
	if got := StackArenaCapacity(64, 12, 0); got != 68 {
		t.Fatalf("legacy capacity = %d, want 68", got)
	}
}

func TestModuleCodeCapacity(t *testing.T) {
	if got := ModuleCodeCapacity(100, 3, 5); got != 612 {
		t.Fatalf("capacity = %d, want 612", got)
	}
	if got := ModuleCodeCapacity(-1, 1, 5); got != 0 {
		t.Fatalf("negative capacity = %d, want 0", got)
	}
	if got := ModuleCodeCapacity(100, 3, 0); got != 0 {
		t.Fatalf("zero expansion capacity = %d, want 0", got)
	}
}
