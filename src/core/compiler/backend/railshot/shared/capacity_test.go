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

func TestTaperedModuleCodeCapacity(t *testing.T) {
	if got := TaperedModuleCodeCapacity(100, 3, 32, 28, 1<<20); got != 512 {
		t.Fatalf("small capacity = %d, want 512", got)
	}
	wantLarge := (28 << 20) + (512 << 10) + 112
	if got := TaperedModuleCodeCapacity(8<<20, 3, 32, 28, 512<<10); got != wantLarge {
		t.Fatalf("large capacity = %d, want %d", got, wantLarge)
	}
	if got := TaperedModuleCodeCapacity(100, 3, 27, 28, 1); got != 0 {
		t.Fatalf("inverted expansion capacity = %d, want 0", got)
	}
}
