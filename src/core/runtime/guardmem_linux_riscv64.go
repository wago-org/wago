//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

func NewJobMemoryGuarded(int, int) (*JobMemory, error)     { return nil, ErrUnsupported }
func AcquireJobMemoryGuarded(int, int) (*JobMemory, error) { return nil, ErrUnsupported }
func growGuardedHostView(j *JobMemory, _ int) error {
	if j != nil && j.reserveBase == 0 {
		return nil
	}
	return ErrUnsupported
}
