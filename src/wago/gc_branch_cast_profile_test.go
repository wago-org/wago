//go:build wago_profile && (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"
)

// Count completed Go helper entries, independently of generated call-site
// statistics. Timing benchmarks use an ordinary build without boundary tracing.
func TestGCBranchCastHelperEntries(t *testing.T) {
	requireCompleteCore3Backend(t)
	for _, sub := range []byte{24, 25} {
		for _, tc := range []struct {
			name   string
			heap   byte
			native bool
		}{
			{"base", 0, true}, {"final-subtype", 1, true}, {"array", 2, true},
			{"i31", 0x6c, true}, {"none", 0x71, true}, {"struct-fallback", 0x6b, false},
		} {
			t.Run(fmt.Sprintf("op%d/%s", sub, tc.name), func(t *testing.T) {
				session := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
				defer session.Close()
				config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCodeProfile(session)
				c, err := Compile(config, branchCastModule(sub, []byte{tc.heap}, true, 2, false, false))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				for selector := uint64(0); selector < 6; selector++ {
					if _, err := in.Invoke("run", selector, 7, 13); err != nil {
						t.Fatal(err)
					}
				}
				count := 0
				for _, span := range session.Spans() {
					if span.Kind == "gc-helper" && span.Function&0xff == gcStructRefTest {
						count++
					}
				}
				want := 6 * 13
				if tc.native {
					want = 0
				}
				if count != want {
					t.Fatalf("Go ref.test entries = %d, want %d", count, want)
				}
			})
		}
	}
}
