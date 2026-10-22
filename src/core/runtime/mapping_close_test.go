//go:build (linux || darwin || windows) && (amd64 || arm64 || riscv64)

package runtime

import "testing"

func TestClosedNativeOwnersDropMappings(t *testing.T) {
	t.Run("engine", func(t *testing.T) {
		e, err := NewEngine()
		if err != nil {
			t.Fatal(err)
		}
		if err := e.Close(); err != nil {
			t.Fatal(err)
		}
		if e.stack != nil || e.stackTop != 0 || e.StackBytes() != 0 || e.StackTop() != 0 {
			t.Fatal("closed engine retains a native stack address")
		}
		if err := e.Close(); err != nil {
			t.Fatalf("second close: %v", err)
		}
	})
	t.Run("arena", func(t *testing.T) {
		a, err := NewArena(4096)
		if err != nil {
			t.Fatal(err)
		}
		a.Alloc(8)
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		if a.mem != nil || a.off != 0 {
			t.Fatal("closed arena retains a native mapping")
		}
		if err := a.Close(); err != nil {
			t.Fatalf("second close: %v", err)
		}
	})
	t.Run("job-memory", func(t *testing.T) {
		j, err := NewJobMemory(65536)
		if err != nil {
			t.Fatal(err)
		}
		if err := j.Close(); err != nil {
			t.Fatal(err)
		}
		if j.mem != nil || j.linOff != 0 || j.linLen != 0 || j.reserveBase != 0 || j.reserveLen != 0 {
			t.Fatal("closed job memory retains a native mapping")
		}
		if len(j.LinearMemory()) != 0 {
			t.Fatal("closed job memory still exposes linear memory")
		}
		if err := j.Close(); err != nil {
			t.Fatalf("second close: %v", err)
		}
	})
}
