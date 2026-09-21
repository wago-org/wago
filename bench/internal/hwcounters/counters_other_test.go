//go:build !linux

package hwcounters

import (
	"errors"
	"testing"
)

func TestUnsupportedHostFailsExplicitly(t *testing.T) {
	if group, err := Open(); group != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Open() = (%v, %v), want explicit unsupported error", group, err)
	}
}
