package wagocli

import "testing"

func TestIsRollingChannel(t *testing.T) {
	for _, v := range []string{"canary", "nightly"} {
		if !isRollingChannel(v) {
			t.Errorf("%q should be a rolling channel", v)
		}
	}
	for _, v := range []string{"1.2.3", "v1.0.0", "stable", "", "canaryx", "night"} {
		if isRollingChannel(v) {
			t.Errorf("%q should not be a rolling channel", v)
		}
	}
}
