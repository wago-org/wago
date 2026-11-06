//go:build linux && amd64 && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

// The established-path admission tests select their emitter explicitly. Keep
// the same local-pressure and self-update bodies covered by the shared route,
// with independent semantic expectations and positive admission assertions.
func TestScalarRouteRegionAndSelfUpdateContracts(t *testing.T) {
	saved := sharedScalarEnabled
	t.Cleanup(func() { sharedScalarEnabled = saved })
	for _, shared := range []bool{false, true} {
		sharedScalarEnabled = shared
		for _, tc := range []struct {
			name   string
			module func(*testing.T) *wasm.Module
			args   []uint64
			want   uint64
		}{
			{"region", intervalRegionModule, nil, 210},
			{"self-update", commuteSelfUpdateModule, []uint64{0x55, 10, 20}, 0x55},
		} {
			t.Run(tc.name+map[bool]string{false: "/established", true: "/shared"}[shared], func(t *testing.T) {
				var stats ModuleStats
				cm, err := CompileModuleWith(tc.module(t), CompileOptions{Workers: 1, Stats: optionalTestStats(&stats)})
				if err != nil {
					t.Fatal(err)
				}
				defer cm.CodeImage.Close()
				if diagnosticsEnabled && stats.Funcs[0].SharedScalar != shared {
					t.Fatalf("shared admission=%v want=%v", stats.Funcs[0].SharedScalar, shared)
				}
				if got := runCompiledAmd64u(t, cm, tc.args...); got != tc.want {
					t.Fatalf("got=%d want=%d", got, tc.want)
				}
			})
		}
	}
}
