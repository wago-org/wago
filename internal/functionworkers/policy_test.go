package functionworkers

import (
	"runtime"
	"testing"
)

func TestResolve(t *testing.T) {
	old := runtime.GOMAXPROCS(8)
	defer runtime.GOMAXPROCS(old)

	for _, tc := range []struct {
		name                 string
		policy, funcs, bytes int
		want                 int
	}{
		{name: "empty", policy: 8, funcs: 0, want: 1},
		{name: "single", policy: 8, funcs: 1, bytes: 1 << 20, want: 1},
		{name: "auto tiny", funcs: 2, bytes: 9, want: 1},
		{name: "auto below threshold", funcs: 255, want: 1},
		{name: "auto at threshold", funcs: 256, want: 4},
		{name: "auto body threshold", funcs: 2, bytes: parallelThreshold, want: 2},
		{name: "auto many funcs", funcs: 301, bytes: 2053, want: 4},
		{name: "auto large bodies", funcs: 10, bytes: 1 << 20, want: 4},
		{name: "serial", policy: 1, funcs: 100, bytes: 1 << 20, want: 1},
		{name: "forced", policy: 3, funcs: 10, want: 3},
		{name: "function cap", policy: 8, funcs: 3, want: 3},
		{name: "gomaxprocs cap", policy: 32, funcs: 100, want: 8},
		{name: "invalid defensive", policy: -1, funcs: 100, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Resolve(tc.policy, tc.funcs, tc.bytes); got != tc.want {
				t.Fatalf("Resolve(%d, %d, %d) = %d, want %d", tc.policy, tc.funcs, tc.bytes, got, tc.want)
			}
		})
	}
}
