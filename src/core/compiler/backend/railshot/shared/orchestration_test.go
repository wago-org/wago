package shared

import (
	"errors"
	"testing"
)

func TestResolveWorkers(t *testing.T) {
	for _, tc := range []struct {
		requested, functions, gomaxprocs int
		want                             int
	}{
		{0, 8, 8, 1},
		{1, 8, 8, 1},
		{8, 1, 8, 1},
		{8, 3, 8, 3},
		{8, 8, 3, 3},
		{4, 8, 8, 4},
		{4, 8, 0, 1},
	} {
		if got := ResolveWorkers(tc.requested, tc.functions, tc.gomaxprocs); got != tc.want {
			t.Errorf("ResolveWorkers(%d, %d, %d) = %d, want %d", tc.requested, tc.functions, tc.gomaxprocs, got, tc.want)
		}
	}
}

func TestPressureThreshold(t *testing.T) {
	if got := PressureThreshold(123, 800); got != 123 {
		t.Fatalf("explicit threshold = %d", got)
	}
	if got := PressureThreshold(0, 800); got != 700 {
		t.Fatalf("default threshold = %d, want 700", got)
	}
}

func TestFirstErrorIndex(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")
	errs := []error{nil, first, second}
	idx, err := FirstErrorIndex(len(errs), func(i int) error { return errs[i] })
	if idx != 1 || !errors.Is(err, first) {
		t.Fatalf("FirstErrorIndex = %d, %v", idx, err)
	}
}
