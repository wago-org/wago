//go:build tinygo || wago_target_tinygo

package gc

import "testing"

func TestParallelDisabledTargetAdmission(t *testing.T) {
	if err := ValidateConfig(Config{Workers: 2}); err == nil {
		t.Fatal("target admitted unsupported helpers")
	}
	c, err := NewCollector(Config{Workers: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.copyPromotionParallel(nil) || c.sweepParallel() {
		t.Fatal("target selected helpers")
	}
	if _, ok := c.drainParallel(false, false); ok {
		t.Fatal("target selected tracing helpers")
	}
	c.Close()
	c.Close()
}
