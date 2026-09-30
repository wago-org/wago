package shared

import "testing"

func TestJoinedModuleCodeCapacity(t *testing.T) {
	const maxInt = int(^uint(0) >> 1)
	for _, tt := range []struct {
		name                               string
		estimate, emitted, functions, want int
	}{
		{"large overestimate", 1 << 20, 128 << 10, 100, (128 << 10) + 1600 + 4096},
		{"small estimate", 256, 80, 1, 256},
		{"underestimate retains growth", 10000, 20000, 3, 10000},
		{"negative emitted", 8000, -1, 1, 8000},
		{"negative functions", 8000, 100, -1, 8000},
		{"emitted overflow", 8000, maxInt, 1, 8000},
		{"function overflow", 8000, 100, maxInt, 8000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := JoinedModuleCodeCapacity(tt.estimate, tt.emitted, tt.functions); got != tt.want {
				t.Fatalf("joined capacity = %d, want %d", got, tt.want)
			}
		})
	}
}

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
