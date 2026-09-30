//go:build wago_guardpage && (linux || darwin || windows) && (amd64 || arm64)

package runtime

import "testing"

func TestGuardedJobMemoryRejectsInvalidSizes(t *testing.T) {
	tooLarge := int(uint64(maxClassicLinMemBytes) + 1)
	for _, test := range []struct {
		name string
		new  func() (*JobMemory, error)
	}{
		{"new initial", func() (*JobMemory, error) { return NewJobMemoryGuarded(-1, 0) }},
		{"new maximum", func() (*JobMemory, error) { return NewJobMemoryGuarded(0, -1) }},
		{"acquire initial", func() (*JobMemory, error) { return AcquireJobMemoryGuarded(-1, 0) }},
		{"acquire maximum", func() (*JobMemory, error) { return AcquireJobMemoryGuarded(0, -1) }},
		{"new initial over memory32", func() (*JobMemory, error) { return NewJobMemoryGuarded(tooLarge, tooLarge) }},
		{"new maximum over memory32", func() (*JobMemory, error) { return NewJobMemoryGuarded(0, tooLarge) }},
		{"acquire initial over memory32", func() (*JobMemory, error) { return AcquireJobMemoryGuarded(tooLarge, tooLarge) }},
		{"acquire maximum over memory32", func() (*JobMemory, error) { return AcquireJobMemoryGuarded(0, tooLarge) }},
		{"new unaligned initial", func() (*JobMemory, error) { return NewJobMemoryGuarded(1, 1<<16) }},
		{"new unaligned maximum", func() (*JobMemory, error) { return NewJobMemoryGuarded(0, 1) }},
		{"acquire unaligned initial", func() (*JobMemory, error) { return AcquireJobMemoryGuarded(1, 1<<16) }},
		{"acquire unaligned maximum", func() (*JobMemory, error) { return AcquireJobMemoryGuarded(0, 1) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			jm, err := test.new()
			if jm != nil {
				_ = jm.Close()
			}
			if err == nil {
				t.Fatal("invalid guarded-memory size was accepted")
			}
		})
	}
}
